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
	"sync"
	"unicode/utf16"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Service installs Copilot's language server, signs in to GitHub and asks it
// for suggestions. One server runs for the whole app, started on first use.
type Service struct {
	dir string // where Install puts the language server

	mu     sync.Mutex
	server *server
	signIn *command // finishes the sign-in SignIn started
}

var _ application.ServiceShutdown = (*Service)(nil)

// NewService keeps the language server in dir, which needn't exist yet.
func NewService(dir string) *Service {
	return &Service{dir: dir}
}

// ServiceShutdown is called by Wails on exit and stops the language server.
func (s *Service) ServiceShutdown() error {
	s.stop()
	return nil
}

// Setup reports whether the language server is installed and who is signed
// in. It starts the server to ask.
func (s *Service) Setup(ctx context.Context) (Setup, error) {
	version := installedVersion(s.dir)
	if version == "" {
		return Setup{}, nil
	}
	setup := Setup{Installed: true, Version: version}
	srv, err := s.running()
	if err != nil {
		return setup, err
	}
	st, err := call[authStatus](ctx, srv, "checkStatus", struct{}{})
	if err != nil {
		return setup, err
	}
	setup.User, setup.Problem = st.describe()
	return setup, nil
}

// Install downloads the latest Copilot language server for this computer,
// about 120 MB, replacing any installed one.
func (s *Service) Install(ctx context.Context) (Setup, error) {
	s.stop()
	if err := install(ctx, s.dir); err != nil {
		return Setup{}, fmt.Errorf("installing Copilot: %w", err)
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
	_, err = call[authStatus](ctx, srv, "signOut", struct{}{})
	return err
}

// Complete asks Copilot how to go on at the cursor. Each suggestion is cut to
// the rest of the cursor's line (oneLine), so it helps with syntax rather
// than writing the solution.
func (s *Service) Complete(ctx context.Context, req CompletionRequest) (Completion, error) {
	srv, err := s.running()
	if err != nil {
		return Completion{}, err
	}
	version, err := srv.sync(req.URI, req.LanguageID, req.Text)
	if err != nil {
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
	if s.server != nil && s.server.alive() {
		return s.server, nil
	}
	if installedVersion(s.dir) == "" {
		return nil, errors.New("copilot isn't installed")
	}
	srv, err := start(binaryPath(s.dir))
	if err != nil {
		return nil, fmt.Errorf("starting Copilot: %w", err)
	}
	s.server = srv
	return srv, nil
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
	// User is the GitHub account signed in, or "" if none is.
	User string `json:"user,omitempty"`
	// Problem says why a signed-in account gets no suggestions, such as
	// having no Copilot plan.
	Problem string `json:"problem,omitempty"`
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
