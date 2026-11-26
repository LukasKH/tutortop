package update

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEngine struct {
	mu       sync.Mutex
	check    func(ctx context.Context) (*Release, error)
	download func(ctx context.Context, onProgress func(written, total int64)) error
	restart  func(ctx context.Context) error
	checks   int
}

func (f *fakeEngine) Check(ctx context.Context) (*Release, error) {
	f.mu.Lock()
	f.checks++
	fn := f.check
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil, nil
}

func (f *fakeEngine) DownloadAndInstall(ctx context.Context, onProgress func(written, total int64)) error {
	f.mu.Lock()
	fn := f.download
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, onProgress)
	}
	return nil
}

func (f *fakeEngine) Restart(ctx context.Context) error {
	f.mu.Lock()
	fn := f.restart
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

type fakeProbe struct {
	writable bool
	regular  bool
}

func (p fakeProbe) dirWritable(string) bool   { return p.writable }
func (p fakeProbe) isRegularFile(string) bool { return p.regular }

func TestCheckAndInstallLifecycle(t *testing.T) {
	var emitted []State
	var mu sync.Mutex
	record := func(s State) {
		mu.Lock()
		emitted = append(emitted, s)
		mu.Unlock()
	}

	downloadReady := make(chan struct{})
	downloadDone := make(chan struct{})

	eng := &fakeEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{Version: "v0.3.0", Notes: "Bug fixes"}, nil
		},
		download: func(_ context.Context, onProgress func(written, total int64)) error {
			onProgress(50, 100)
			close(downloadReady)
			<-downloadDone
			return nil
		},
	}

	svc := NewService(eng, Support{CanSelfUpdate: true}, record)
	t.Cleanup(func() { _ = svc.ServiceShutdown() })

	if got := svc.Status(); got.ReleaseURL != ReleasesURL+"/latest" {
		t.Fatalf("initial ReleaseURL: got %q", got.ReleaseURL)
	}

	st := svc.Check(context.Background())
	if st.Phase != PhaseAvailable || st.Version != "0.3.0" || st.ReleaseURL != ReleasesURL+"/tag/v0.3.0" {
		t.Fatalf("after Check: got %+v", st)
	}

	if err := svc.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	<-downloadReady
	if got := svc.Status(); got.Phase != PhaseDownloading || got.Written != 50 || got.Total != 100 {
		t.Fatalf("mid-install: got %+v", got)
	}

	// A second Install while running is a no-op, and Check leaves it alone.
	if err := svc.Install(); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if got := svc.Check(context.Background()); got.Phase != PhaseDownloading {
		t.Fatalf("Check during install: got %+v", got)
	}

	close(downloadDone)
	waitForPhase(t, svc, PhaseReady)

	// Failed Restart keeps PhaseReady so the user can retry without re-downloading.
	eng.restart = func(context.Context) error { return errors.New("helper timed out") }
	if err := svc.Restart(context.Background()); err == nil {
		t.Fatal("expected Restart error")
	}
	if got := svc.Status(); got.Phase != PhaseReady || got.Error != "helper timed out" {
		t.Fatalf("after failed Restart: got %+v", got)
	}

	// Retry Restart succeeds.
	eng.restart = func(context.Context) error { return nil }
	if err := svc.Restart(context.Background()); err != nil {
		t.Fatalf("retry Restart: %v", err)
	}
	if got := svc.Status(); got.Phase != PhaseRestarting || got.Error != "" {
		t.Fatalf("after Restart: got %+v", got)
	}

	mu.Lock()
	count := len(emitted)
	mu.Unlock()
	if count == 0 {
		t.Fatal("expected state changes to be emitted")
	}
}

func TestErrorsPreserveRetryPhase(t *testing.T) {
	eng := &fakeEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{Version: "0.3.0"}, nil
		},
		download: func(context.Context, func(int64, int64)) error {
			return errors.New("signature mismatch")
		},
	}
	svc := NewService(eng, Support{CanSelfUpdate: true}, nil)
	t.Cleanup(func() { _ = svc.ServiceShutdown() })

	svc.Check(context.Background())
	if err := svc.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	waitForPhase(t, svc, PhaseAvailable)
	if got := svc.Status(); got.Version != "0.3.0" || got.Error != "signature mismatch" {
		t.Fatalf("after failed download: got %+v", got)
	}

	// A failed manual re-check keeps the known update intact with the new error.
	eng.mu.Lock()
	eng.check = func(context.Context) (*Release, error) {
		return nil, errors.New("network unreachable")
	}
	eng.mu.Unlock()

	st := svc.Check(context.Background())
	if st.Phase != PhaseAvailable || st.Version != "0.3.0" || st.Error != "network unreachable" {
		t.Fatalf("after failed re-check: got %+v", st)
	}
}

func TestManualOnlyRefusesInstall(t *testing.T) {
	eng := &fakeEngine{
		check: func(context.Context) (*Release, error) {
			return &Release{Version: "0.4.0"}, nil
		},
	}
	svc := NewService(eng, ManualOnly("system package"), nil)
	t.Cleanup(func() { _ = svc.ServiceShutdown() })

	st := svc.Check(context.Background())
	if st.Phase != PhaseAvailable || st.CanSelfUpdate || st.ManualReason != "system package" || st.ReleaseURL != ReleasesURL+"/tag/v0.4.0" {
		t.Fatalf("after Check: %+v", st)
	}
	if err := svc.Install(); err == nil {
		t.Fatal("expected Install to refuse manual-only installation")
	}
}

func TestBackgroundCheckSilentErrorAndNewerRelease(t *testing.T) {
	// First check fails silently without surfacing an error.
	firstDone := make(chan struct{})
	eng := &fakeEngine{
		check: func(context.Context) (*Release, error) {
			defer close(firstDone)
			return nil, errors.New("offline")
		},
	}
	svc := NewService(eng, Support{CanSelfUpdate: true}, nil)
	svc.startupDelay = time.Millisecond
	svc.checkInterval = 0
	t.Cleanup(func() { _ = svc.ServiceShutdown() })

	Start(svc)
	<-firstDone
	_ = svc.ServiceShutdown()

	if got := svc.Status(); got.Phase != PhaseIdle || got.Error != "" {
		t.Fatalf("silent background error should not surface: got %+v", got)
	}

	// A newer release published while PhaseAvailable is picked up on the next check.
	versions := []string{"0.3.0", "0.3.1"}
	idx := 0
	eng2 := &fakeEngine{
		check: func(context.Context) (*Release, error) {
			v := versions[idx]
			if idx < len(versions)-1 {
				idx++
			}
			return &Release{Version: v}, nil
		},
	}
	svc2 := NewService(eng2, Support{CanSelfUpdate: true}, nil)
	svc2.startupDelay = time.Millisecond
	svc2.checkInterval = 5 * time.Millisecond
	t.Cleanup(func() { _ = svc2.ServiceShutdown() })

	Start(svc2)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc2.Status().Version == "0.3.1" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected background check to pick up 0.3.1, got %+v", svc2.Status())
}

func TestDetectSupport(t *testing.T) {
	tests := []struct {
		name       string
		goos       string
		exe        string
		appImage   string
		appDir     string
		probe      fakeProbe
		want       bool
		wantReason string
	}{
		{
			name:  "macOS packaged writable",
			goos:  "darwin",
			exe:   "/Applications/Tutortop.app/Contents/MacOS/tutortop",
			probe: fakeProbe{writable: true},
			want:  true,
		},
		{
			name:  "macOS packaged read-only",
			goos:  "darwin",
			exe:   "/Applications/Tutortop.app/Contents/MacOS/tutortop",
			probe: fakeProbe{writable: false},
			want:  false,
		},
		{
			name:  "macOS translocated",
			goos:  "darwin",
			exe:   "/private/var/folders/xx/AppTranslocation/abc/d/Tutortop.app/Contents/MacOS/tutortop",
			probe: fakeProbe{writable: true},
			want:  false,
		},
		{
			name:  "macOS dev bundle",
			goos:  "darwin",
			exe:   "/tmp/bin/tutortop.dev.app/Contents/MacOS/tutortop",
			probe: fakeProbe{writable: true},
			want:  false,
		},
		{
			name:  "macOS bare binary",
			goos:  "darwin",
			exe:   "/tmp/bin/tutortop",
			probe: fakeProbe{writable: true},
			want:  false,
		},
		{
			name:  "Windows user install",
			goos:  "windows",
			exe:   `C:\Users\ada\AppData\Local\Programs\Tutortop\tutortop.exe`,
			probe: fakeProbe{writable: true},
			want:  true,
		},
		{
			name:  "Windows Program Files",
			goos:  "windows",
			exe:   `C:\Program Files\LukasKH\Tutortop\tutortop.exe`,
			probe: fakeProbe{writable: false},
			want:  false,
		},
		{
			name:  "Linux user-local binary writable",
			goos:  "linux",
			exe:   "/home/ada/.local/bin/tutortop",
			probe: fakeProbe{writable: true},
			want:  true,
		},
		{
			name:       "Linux user-local binary read-only",
			goos:       "linux",
			exe:        "/home/ada/.local/bin/tutortop",
			probe:      fakeProbe{writable: false},
			want:       false,
			wantReason: "write access",
		},
		{
			name:       "Linux system package",
			goos:       "linux",
			exe:        "/usr/bin/tutortop",
			probe:      fakeProbe{writable: true},
			want:       false,
			wantReason: "system package",
		},
		{
			name:       "Linux opt install",
			goos:       "linux",
			exe:        "/opt/tutortop/tutortop",
			probe:      fakeProbe{writable: true},
			want:       false,
			wantReason: "system package",
		},
		{
			name:       "Linux nix store",
			goos:       "linux",
			exe:        "/nix/store/abc-tutortop/bin/tutortop",
			probe:      fakeProbe{writable: true},
			want:       false,
			wantReason: "Nix",
		},
		{
			name:       "Linux AppImage",
			goos:       "linux",
			exe:        "/tmp/.mount_tut123/usr/bin/tutortop",
			appImage:   "/home/ada/.local/bin/tutortop",
			appDir:     "/tmp/.mount_tut123",
			probe:      fakeProbe{writable: true, regular: true},
			want:       false,
			wantReason: "AppImage",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectSupport(tc.goos, tc.exe, tc.appImage, tc.appDir, tc.probe)
			if got.CanSelfUpdate != tc.want {
				t.Fatalf("CanSelfUpdate = %v, want %v (reason: %q)", got.CanSelfUpdate, tc.want, got.ManualReason)
			}
			if !tc.want && got.ManualReason == "" {
				t.Fatal("expected a ManualReason when CanSelfUpdate is false")
			}
			if tc.wantReason != "" && !strings.Contains(got.ManualReason, tc.wantReason) {
				t.Fatalf("ManualReason = %q, want substring %q", got.ManualReason, tc.wantReason)
			}
		})
	}
}

func waitForPhase(t *testing.T, svc *Service, want Phase) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.Status().Phase == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for phase %q (got %+v)", want, svc.Status())
}
