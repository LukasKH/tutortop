// Package copilot serves GitHub Copilot's suggestions to the task editor
// (Service). Copilot is optional: the learner installs GitHub's Copilot
// language server from Settings and signs in with a GitHub account, which may
// be on Copilot Free.
//
// Unlike the rest of the app, the language server runs outside the sandbox,
// as it has to reach GitHub: it sends the file being edited there.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/sync/singleflight"
)

// Browser opens URLs in the user's web browser, for GitHub's sign-in page.
// A nil Browser skips opening them, as in tests.
type Browser interface {
	OpenURL(url string) error
}

// Service installs Copilot's language server, signs in to GitHub and asks it
// for suggestions. One server runs for the whole app, started on first use.
type Service struct {
	dir     string  // where Install puts the language server
	browser Browser // opens GitHub's sign-in page; nil in tests

	mu         sync.Mutex
	server     *server
	signIn     *command // finishes the sign-in SignIn started
	installing bool

	// Separate from mu so Setup's GitHub lookup doesn't wait while running()
	// starts and initializes the language server.
	releaseMu sync.Mutex
	latest    string             // the newest release's version, as last looked up or installed; "" if GitHub didn't answer
	latestAt  time.Time          // when it was looked up
	lookups   singleflight.Group // one GitHub lookup at a time, shared by concurrent Setups
}

// NewService keeps the language server in dir, which needn't exist yet.
func NewService(dir string, browser Browser) *Service {
	return &Service{dir: dir, browser: browser}
}

// ServiceShutdown is called by Wails on exit and stops the language server.
func (s *Service) ServiceShutdown() error {
	s.stop()
	return nil
}

// Setup reports whether the language server is installed, who is signed
// in, and whether GitHub has a newer release. It starts the server to ask.
// While an install runs it says so, and asks nothing: the server is stopped.
func (s *Service) Setup(ctx context.Context) (Setup, error) {
	version := installedVersion(s.dir)
	if s.isInstalling() {
		return Setup{Installing: true, Installed: version != "", Version: version}, nil
	}
	if version == "" {
		return Setup{}, nil
	}
	// GitHub is asked about a newer release while the server starts and
	// answers, so the lookup adds no wait of its own.
	newerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	update := make(chan string, 1)
	go func() { update <- s.newer(newerCtx, version) }()
	setup, err := s.status(ctx)
	setup.Installed, setup.Version = true, version
	if err != nil {
		return setup, err
	}
	setup.Update = <-update
	return setup, nil
}

// Install downloads the latest Copilot language server for this computer,
// about 120 MB, replacing any installed one. When it ends, it emits
// events.CopilotInstalled too, for a Settings screen opened in the meantime.
func (s *Service) Install(ctx context.Context) (Setup, error) {
	if err := s.replace(ctx); err != nil {
		return Setup{}, err
	}
	return s.Setup(ctx)
}

// SignIn starts signing in to GitHub. The learner enters the returned code on
// GitHub's page, which FinishSignIn opens. An empty code means someone is
// signed in already.
func (s *Service) SignIn(ctx context.Context) (DeviceCode, error) {
	srv, err := s.running()
	if err != nil {
		return DeviceCode{}, err
	}
	res, err := call[signInResult](ctx, srv, "signIn", struct{}{})
	if err != nil || res.Status == authAlreadySignedIn {
		return DeviceCode{}, err
	}
	s.mu.Lock()
	s.signIn = res.Command
	s.mu.Unlock()
	return DeviceCode{UserCode: res.UserCode, VerificationURI: res.VerificationURI}, nil
}

// FinishSignIn opens GitHub's sign-in page and waits until the learner has
// entered the code there, or the code expires.
func (s *Service) FinishSignIn(ctx context.Context) (Setup, error) {
	s.mu.Lock()
	cmd := s.signIn
	s.mu.Unlock()
	if cmd == nil {
		return Setup{}, errors.New("start signing in first")
	}
	srv, err := s.running()
	if err != nil {
		return Setup{}, err
	}
	// Forget the previous account's plan before signing in, so the new
	// account's, which may come during sign-in, is kept.
	srv.resetPlan()
	if _, err := call[authStatus](ctx, srv, "workspace/executeCommand", cmd); err != nil {
		return Setup{}, err
	}
	s.mu.Lock()
	s.signIn = nil
	s.mu.Unlock()
	return s.Setup(ctx)
}

// SignOut signs out of GitHub, in every editor that shares Copilot's sign-in.
func (s *Service) SignOut(ctx context.Context) error {
	srv, err := s.running()
	if err != nil {
		return err
	}
	if _, err := call[authStatus](ctx, srv, "signOut", struct{}{}); err != nil {
		return err
	}
	srv.resetPlan()
	return nil
}

// Complete asks Copilot how to go on at the cursor. Each suggestion is cut to
// the rest of the cursor's line (oneLine), so it helps with syntax rather
// than writing the solution.
func (s *Service) Complete(ctx context.Context, req CompletionRequest) (Completion, error) {
	if isCancelled(ctx, nil) {
		return Completion{Suggestions: []Suggestion{}}, nil
	}
	if err := ctx.Err(); err != nil {
		return Completion{}, err
	}
	srv, err := s.running()
	if err != nil {
		return Completion{}, err
	}
	version, err := srv.sync(ctx, req.URI, req.LanguageID, req.Text)
	if err != nil {
		if isCancelled(ctx, err) {
			return Completion{Suggestions: []Suggestion{}}, nil
		}
		return Completion{}, err
	}
	list, err := call[completionList](ctx, srv, "textDocument/inlineCompletion", inlineCompletionParams{
		TextDocument: versionedDocument{URI: req.URI, Version: version},
		Position:     req.Position,
		Context:      inlineCompletionContext{TriggerKind: triggerAutomatic},
		FormattingOptions: formattingOptions{
			TabSize:      req.TabSize,
			InsertSpaces: req.InsertSpaces,
		},
	})
	if isCancelled(ctx, err) {
		return Completion{Suggestions: []Suggestion{}}, nil
	}
	if rpcErr, ok := errors.AsType[*jsonrpc2.Error](err); ok && rpcErr.Code == codeNotSignedIn {
		return Completion{Suggestions: []Suggestion{}, Notice: "Not signed in to GitHub. Sign in under Settings."}, nil
	}
	if err != nil {
		return Completion{}, err
	}
	line := lineAt(req.Text, req.Position.Line)
	out := Completion{Suggestions: []Suggestion{}, Notice: srv.notice()}
	for _, raw := range list.Items {
		item, err := decode.JSON[completionItem](raw)
		if err != nil {
			continue
		}
		if sg, ok := oneLine(item, req.Position, line); ok {
			sg.Item = raw
			out.Suggestions = append(out.Suggestions, sg)
		}
	}
	return out, nil
}

// Shown tells Copilot a suggestion is on screen, as GitHub's own plugins do.
func (s *Service) Shown(ctx context.Context, sg Suggestion) error {
	srv := s.current()
	if srv == nil {
		return nil
	}
	return srv.conn.Notify(ctx, "textDocument/didShowCompletion", map[string]any{"item": sg.Item})
}

// Accepted tells Copilot the learner took a suggestion: the whole of it, or
// only its first line, as oneLine cut it.
func (s *Service) Accepted(ctx context.Context, sg Suggestion) error {
	srv := s.current()
	if srv == nil {
		return nil
	}
	item, err := decode.JSON[completionItem](sg.Item)
	if err != nil {
		return err
	}
	if sg.Text != item.InsertText {
		return srv.conn.Notify(ctx, "textDocument/didPartiallyAcceptCompletion", map[string]any{
			"item":           sg.Item,
			"acceptedLength": len(utf16.Encode([]rune(sg.Text))), // from the start of insertText
		})
	}
	if item.Command == nil {
		return nil
	}
	_, err = call[json.RawMessage](ctx, srv, "workspace/executeCommand", item.Command)
	return err
}

// Focus tells Copilot which file the learner is looking at.
func (s *Service) Focus(ctx context.Context, uri string) error {
	srv := s.current()
	if srv == nil {
		return nil
	}
	return srv.focus(ctx, uri)
}

// CloseFiles closes every file Copilot has open, as the learner leaves a
// task, so it doesn't draw on other tasks' code.
func (s *Service) CloseFiles(ctx context.Context) error {
	srv := s.current()
	if srv == nil {
		return nil
	}
	return srv.closeAll(ctx)
}

// current returns the language server if it's running, else nil.
func (s *Service) isInstalling() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installing
}

func (s *Service) current() *server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil && s.server.alive() {
		return s.server
	}
	return nil
}

// running returns the language server, starting it if it isn't running.
func (s *Service) running() (*server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installing {
		return nil, errors.New("copilot is installing")
	}
	if s.server != nil {
		if s.server.alive() {
			return s.server, nil
		}
		// Reap the exited server before starting a fresh one.
		s.server.close()
		s.server = nil
	}
	if installedVersion(s.dir) == "" {
		return nil, errors.New("copilot isn't installed")
	}
	srv, err := start(binaryPath(s.dir), s.browser)
	if err != nil {
		return nil, fmt.Errorf("starting Copilot: %w", err)
	}
	s.server = srv
	return srv, nil
}

// status asks the running server who is signed in, and their plan.
func (s *Service) status(ctx context.Context) (Setup, error) {
	var setup Setup
	srv, err := s.running()
	if err != nil {
		return setup, err
	}
	st, err := call[authStatus](ctx, srv, "checkStatus", struct{}{})
	if err != nil {
		return setup, err
	}
	setup.User, setup.Problem = st.describe()
	if setup.User != "" && setup.Problem == "" {
		setup.Plan = srv.awaitPlan(ctx, 2*time.Second)
	}
	return setup, nil
}

// newer returns the newest release's version if it's newer than the
// installed one, else "". It asks GitHub at most every few hours. If it
// can't, it offers no update, and waits a while before asking again, so being
// offline doesn't slow every Setup.
func (s *Service) newer(ctx context.Context, installed string) string {
	s.releaseMu.Lock()
	latest, asked := s.latest, s.latestAt
	s.releaseMu.Unlock()
	if time.Since(asked) > 6*time.Hour || latest == "" && time.Since(asked) > 10*time.Minute {
		// Shared by concurrent callers, so it mustn't stop when one gives up.
		lookup := s.lookups.DoChan("latest", func() (any, error) {
			return s.lookUpLatest(context.WithoutCancel(ctx)), nil
		})
		select {
		case r := <-lookup:
			latest, _ = r.Val.(string) // always a string
		case <-ctx.Done():
			return ""
		}
	}
	if !isNewer(latest, installed) {
		return ""
	}
	return latest
}

// lookUpLatest asks GitHub for the newest release's version and keeps it;
// "" if GitHub doesn't answer.
func (s *Service) lookUpLatest(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	latest := ""
	if rel, err := fetchRelease(ctx); err == nil {
		latest = rel.Tag
	}
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	s.latest, s.latestAt = latest, time.Now()
	return latest
}

// replace stops any running server and installs the latest one into s.dir,
// preventing concurrent callers from restarting the old binary mid-install.
func (s *Service) replace(ctx context.Context) (err error) {
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return errors.New("copilot is installing already")
	}
	s.installing = true
	srv := s.server
	s.server = nil
	s.signIn = nil
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
		var done events.Install
		if err != nil {
			done.Error = err.Error()
		}
		events.Emit(events.CopilotInstalled, done)
	}()
	if srv != nil {
		srv.close()
	}
	tag, err := install(ctx, s.dir)
	if err != nil {
		return fmt.Errorf("installing Copilot: %w", err)
	}
	// The installed release is the newest known, so an older one looked up
	// before isn't offered as an update.
	s.releaseMu.Lock()
	s.latest, s.latestAt = tag, time.Now()
	s.releaseMu.Unlock()
	return nil
}

// stop stops the language server, if it's running.
func (s *Service) stop() {
	s.mu.Lock()
	srv := s.server
	s.server = nil
	s.signIn = nil
	s.mu.Unlock()
	if srv != nil {
		srv.close()
	}
}

// Setup is how far Copilot is set up, for the Settings screen.
type Setup struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	// Installing is true while Install downloads a release. The other fields
	// then only say what was installed before.
	Installing bool `json:"installing,omitempty"`
	// User is the GitHub account signed in, or "" if none is.
	User string `json:"user,omitempty"`
	// Problem says why a signed-in account gets no suggestions, such as
	// having no Copilot plan.
	Problem string `json:"problem,omitempty"`
	// Plan is the signed-in account's plan, or nil until the server says.
	Plan *Plan `json:"plan,omitempty"`
	// Update is a newer release's version, which Install would bring; ""
	// when the installed one is the latest, or GitHub couldn't be asked.
	Update string `json:"update,omitempty"`
}

// Plan is a Copilot plan and its allowance of suggestions.
type Plan struct {
	Name string `json:"name"` // "Copilot Free", "Copilot Pro"
	// Unlimited plans have no allowance. Otherwise Used of Allowance
	// suggestions are spent until ResetDate (RFC 3339); 0 if not known.
	Unlimited bool   `json:"unlimited"`
	Allowance int    `json:"allowance,omitempty"`
	Used      int    `json:"used,omitempty"`
	ResetDate string `json:"resetDate,omitempty"`
}

// DeviceCode is what the learner enters on GitHub's sign-in page.
type DeviceCode struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
}

// CompletionRequest is the file being edited and where the cursor is.
type CompletionRequest struct {
	// URI names the file; Copilot uses its extension too.
	URI          string   `json:"uri"`
	LanguageID   string   `json:"languageId"`
	Text         string   `json:"text"`
	Position     Position `json:"position"`
	TabSize      int      `json:"tabSize"`
	InsertSpaces bool     `json:"insertSpaces"`
}

// Completion is Copilot's suggestions, and what it last said went wrong,
// such as a used-up monthly allowance.
type Completion struct {
	Suggestions []Suggestion `json:"suggestions"`
	Notice      string       `json:"notice,omitempty"`
}

// Suggestion is text to put in place of Range, on the cursor's line.
type Suggestion struct {
	Text  string `json:"text"`
	Range Range  `json:"range"`
	// Item is the suggestion as Copilot sent it, uncut, which Shown and
	// Accepted pass back.
	Item json.RawMessage `json:"item"`
}

// Range and Position are as in the language server protocol: lines and
// UTF-16 characters, counted from 0.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// isCancelled reports whether a completion request was cancelled because the
// learner typed again or closed the file, which is normal and not an error.
func isCancelled(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	rpcErr, ok := errors.AsType[*jsonrpc2.Error](err)
	return ok && rpcErr.Code == codeRequestCancelled
}

// isNewer reports whether version a comes after b, comparing their
// dot-separated numbers, as in GitHub's release tags ("1.391.0"). A version
// that isn't such numbers counts as older than any that is.
func isNewer(a, b string) bool {
	return slices.Compare(versionNumbers(a), versionNumbers(b)) > 0
}

// versionNumbers splits a version such as "1.391.0" into its numbers, or
// returns nil if it isn't one.
func versionNumbers(v string) []int {
	var nums []int
	for part := range strings.SplitSeq(strings.TrimPrefix(v, "v"), ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		nums = append(nums, n)
	}
	return nums
}
