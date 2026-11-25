// Package update coordinates in-app update checks, downloads and restarts,
// keeping the update state machine free of Wails so it can be linted and
// tested without GTK. main.go plugs Wails' app.Updater in through Engine.
package update

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	// Version is the running Tutortop version. release-please updates it on
	// each release.
	Version = "0.2.6" // x-release-please-version

	// ReleasesURL is the GitHub Releases page for manual downloads.
	ReleasesURL = "https://github.com/LukasKH/tutortop/releases"

	// ManifestURL is the signed Wails update manifest attached to the latest
	// GitHub release.
	ManifestURL = ReleasesURL + "/latest/download/manifest.json"

	defaultStartupDelay  = 5 * time.Second
	defaultCheckInterval = 6 * time.Hour
	checkTimeout         = 30 * time.Second
)

// Phase is where the updater is in the update lifecycle.
type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseChecking    Phase = "checking"
	PhaseUpToDate    Phase = "up-to-date"
	PhaseAvailable   Phase = "available"
	PhaseDownloading Phase = "downloading"
	PhaseReady       Phase = "ready"
	PhaseRestarting  Phase = "restarting"
)

// State is a snapshot of the updater's current phase and release metadata,
// returned by Status and pushed to the frontend on every change.
type State struct {
	CurrentVersion string `json:"currentVersion"`
	Phase          Phase  `json:"phase"`
	CanSelfUpdate  bool   `json:"canSelfUpdate"`
	// ManualReason explains why in-app updates aren't available when
	// CanSelfUpdate is false (for example, a Linux system package).
	ManualReason string `json:"manualReason,omitempty"`
	// Version, Notes and ReleaseURL describe the newer release once found.
	// ReleaseURL defaults to the latest releases page before a version is known.
	Version    string `json:"version,omitempty"`
	Notes      string `json:"notes,omitempty"`
	ReleaseURL string `json:"releaseUrl"`
	// Written and Total track download progress in bytes while downloading.
	Written int64 `json:"written,omitempty"`
	Total   int64 `json:"total,omitempty"`
	// Error explains why the last manual check, download or restart failed.
	// Kept orthogonal to Phase so the user can retry from the same phase.
	Error string `json:"error,omitempty"`
}

// Release describes a newer version returned by Engine.Check.
type Release struct {
	Version string
	Notes   string
}

// Engine checks for, downloads, verifies and applies updates. main.go adapts
// Wails' app.Updater; tests supply a fake.
type Engine interface {
	Check(ctx context.Context) (*Release, error)
	DownloadAndInstall(ctx context.Context, onProgress func(written, total int64)) error
	Restart(ctx context.Context) error
}

// Support describes whether the running binary can replace itself in place.
type Support struct {
	CanSelfUpdate bool
	ManualReason  string
}

// Start runs a quiet update check shortly after startup and periodically while
// the app stays open. Safe to call more than once; does nothing without an
// engine.
func Start(s *Service) {
	s.start()
}

// Service exposes update checks, downloads and restarts to the frontend as a
// Wails service. Downloads outlive the Settings dialog that starts them.
type Service struct {
	engine        Engine
	onChange      func(State)
	startupDelay  time.Duration
	checkInterval time.Duration

	// checkMu serialises engine.Check calls so a background check and a manual
	// check never race against the underlying updater.
	checkMu sync.Mutex

	mu         sync.Mutex
	state      State
	startOnce  sync.Once
	stopCtx    context.Context
	stopCancel context.CancelFunc
	wg         sync.WaitGroup
}

// NewService builds an update service for engine. When engine is nil (as in
// tests or when updater initialization failed), checks are no-ops. onChange is
// called in strict state-transition order whenever State changes.
func NewService(engine Engine, support Support, onChange func(State)) *Service {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	return &Service{
		engine:        engine,
		onChange:      onChange,
		startupDelay:  defaultStartupDelay,
		checkInterval: defaultCheckInterval,
		state: State{
			CurrentVersion: Version,
			Phase:          PhaseIdle,
			CanSelfUpdate:  support.CanSelfUpdate,
			ManualReason:   support.ManualReason,
			ReleaseURL:     releaseURL(""),
		},
		stopCtx:    stopCtx,
		stopCancel: stopCancel,
	}
}

// ServiceShutdown stops background checks and any download in progress.
func (s *Service) ServiceShutdown() error {
	s.stopCancel()
	s.wg.Wait()
	return nil
}

// Status returns the current updater state.
func (s *Service) Status() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Check asks the update feed for a newer release and updates the state. If a
// check, download or restart is already running, or an update is already
// staged, Check returns the current state without disturbing it.
func (s *Service) Check(ctx context.Context) State {
	if s.engine == nil {
		return s.Status()
	}

	var prev Phase
	started := false
	cur := s.mutate(func(st *State) bool {
		if !canCheck(st.Phase) {
			return false
		}
		prev = st.Phase
		st.Phase = PhaseChecking
		st.Error = ""
		started = true
		return true
	})
	if !started {
		return cur
	}

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	s.checkMu.Lock()
	rel, err := s.engine.Check(checkCtx)
	s.checkMu.Unlock()

	return s.mutate(func(st *State) bool {
		if err != nil {
			st.Phase = prev
			st.Error = err.Error()
			return true
		}
		applyRelease(st, rel)
		return true
	})
}

// Install downloads, verifies and stages the available update in the
// background so closing Settings does not interrupt it. Progress and
// completion arrive through the update:changed event.
func (s *Service) Install() error {
	if s.engine == nil {
		return errors.New("updater is not configured")
	}

	var startErr error
	started := false
	s.mutate(func(st *State) bool {
		if !st.CanSelfUpdate {
			startErr = errors.New("automatic updates are not supported for this installation")
			return false
		}
		switch st.Phase {
		case PhaseDownloading, PhaseReady, PhaseRestarting:
			return false
		case PhaseIdle, PhaseChecking, PhaseUpToDate:
			startErr = errors.New("no update available")
			return false
		case PhaseAvailable:
			st.Phase = PhaseDownloading
			st.Written = 0
			st.Total = 0
			st.Error = ""
			started = true
			return true
		}
		return false
	})
	if startErr != nil {
		return startErr
	}
	if !started {
		return nil
	}

	s.wg.Go(s.runInstall)
	return nil
}

// Restart hands the staged update to the swap helper and quits Tutortop so it
// relaunches into the new version.
func (s *Service) Restart(ctx context.Context) error {
	if s.engine == nil {
		return errors.New("updater is not configured")
	}

	started := false
	s.mutate(func(st *State) bool {
		if st.Phase != PhaseReady {
			return false
		}
		st.Phase = PhaseRestarting
		st.Error = ""
		started = true
		return true
	})
	if !started {
		return errors.New("no update is ready to install")
	}

	if err := s.engine.Restart(ctx); err != nil {
		// Wails reclaims the staging dir when helper startup fails, so the
		// update stays ready for another Restart attempt.
		s.mutate(func(st *State) bool {
			st.Phase = PhaseReady
			st.Error = err.Error()
			return true
		})
		return err
	}
	return nil
}

func (s *Service) start() {
	if s.engine == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Go(s.backgroundLoop)
	})
}

func (s *Service) backgroundLoop() {
	timer := time.NewTimer(s.startupDelay)
	defer timer.Stop()
	for {
		select {
		case <-s.stopCtx.Done():
			return
		case <-timer.C:
			s.checkSilent()
			if s.checkInterval <= 0 {
				return
			}
			timer.Reset(s.checkInterval)
		}
	}
}

// checkSilent checks in the background without moving the UI into checking or
// surfacing errors, so going offline never interrupts the user.
func (s *Service) checkSilent() {
	if !canCheck(s.Status().Phase) {
		return
	}

	ctx, cancel := context.WithTimeout(s.stopCtx, checkTimeout)
	defer cancel()

	s.checkMu.Lock()
	rel, err := s.engine.Check(ctx)
	s.checkMu.Unlock()
	if err != nil {
		return
	}

	s.mutate(func(st *State) bool {
		if !canCheck(st.Phase) {
			return false
		}
		applyRelease(st, rel)
		return true
	})
}

func (s *Service) runInstall() {
	err := s.engine.DownloadAndInstall(s.stopCtx, func(written, total int64) {
		s.mutate(func(st *State) bool {
			if st.Phase != PhaseDownloading {
				return false
			}
			st.Written = written
			st.Total = total
			return true
		})
	})

	s.mutate(func(st *State) bool {
		if err != nil {
			if s.stopCtx.Err() != nil {
				return false
			}
			st.Phase = PhaseAvailable
			st.Written = 0
			st.Total = 0
			st.Error = err.Error()
			return true
		}
		st.Phase = PhaseReady
		st.Error = ""
		return true
	})
}

// mutate updates s.state under s.mu and emits while still holding the lock, so
// concurrent progress events can never be delivered out of order after Ready.
func (s *Service) mutate(change func(*State) bool) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if change(&s.state) && s.onChange != nil {
		s.onChange(s.state)
	}
	return s.state
}

func applyRelease(st *State, rel *Release) {
	st.Written = 0
	st.Total = 0
	st.Error = ""
	if rel == nil {
		st.Phase = PhaseUpToDate
		st.Version = ""
		st.Notes = ""
		st.ReleaseURL = releaseURL("")
		return
	}
	ver := strings.TrimPrefix(strings.TrimSpace(rel.Version), "v")
	st.Phase = PhaseAvailable
	st.Version = ver
	st.Notes = strings.TrimSpace(rel.Notes)
	st.ReleaseURL = releaseURL(ver)
}

func canCheck(p Phase) bool {
	switch p {
	case PhaseIdle, PhaseUpToDate, PhaseAvailable:
		return true
	case PhaseChecking, PhaseDownloading, PhaseReady, PhaseRestarting:
		return false
	}
	return false
}

func releaseURL(version string) string {
	if version == "" {
		return ReleasesURL + "/latest"
	}
	return ReleasesURL + "/tag/v" + version
}
