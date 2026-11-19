package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// server is a running Copilot language server, which the app talks to over
// stdio. The protocol is documented in
// https://github.com/github/copilot-language-server-release.
type server struct {
	cmd  *exec.Cmd
	conn *jsonrpc2.Conn

	mu      sync.Mutex
	docs    map[string]*document // by URI
	message string               // what the server last said is wrong
}

// start runs the language server at path and initializes it. It doesn't
// take the caller's context: a suggestion cancelled by the next key mustn't
// stop a server that's still starting.
func start(path string) (*server, error) {
	cmd := exec.Command(path, "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &server{cmd: cmd, docs: map[string]*document{}}
	stream := jsonrpc2.NewBufferedStream(stdio{stdout, stdin}, jsonrpc2.VSCodeObjectCodec{})
	// Async, as answering showDocument mustn't hold up the call that caused it.
	s.conn = jsonrpc2.NewConn(context.Background(), stream, jsonrpc2.AsyncHandler(jsonrpc2.HandlerWithError(s.handle)))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.initialize(ctx); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *server) initialize(ctx context.Context) error {
	info := map[string]string{"name": "Tutortop", "version": "1"}
	if _, err := call[json.RawMessage](ctx, s, "initialize", map[string]any{
		"processId": nil,
		"capabilities": map[string]any{
			// GitHub's sign-in page opens through the app (handle).
			"window": map[string]any{"showDocument": map[string]any{"support": true}},
		},
		"initializationOptions": map[string]any{"editorInfo": info, "editorPluginInfo": info},
	}); err != nil {
		return err
	}
	if err := s.conn.Notify(ctx, "initialized", struct{}{}); err != nil {
		return err
	}
	// No settings to give, but the server waits for them. Like GitHub's own
	// plugins, this leaves telemetry at the server's default.
	return s.conn.Notify(ctx, "workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{}})
}

// alive reports whether the server is still running.
func (s *server) alive() bool {
	select {
	case <-s.conn.DisconnectNotify():
		return false
	default:
		return true
	}
}

// close asks the server to shut down and exit, as the protocol says, and
// kills it if it hasn't within a few seconds.
func (s *server) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := call[json.RawMessage](ctx, s, "shutdown", nil); err == nil {
		_ = s.conn.Notify(ctx, "exit", nil) // killed below if this fails
	}
	select {
	case <-s.conn.DisconnectNotify(): // it exited, closing its output
	case <-ctx.Done():
	}
	_ = s.conn.Close()       // stopping anyway
	_ = s.cmd.Process.Kill() // gone already, unless it ignored exit
	_ = s.cmd.Wait()         // the exit status says nothing here
}

// sync sends the server a file's text, opening it the first time, and
// returns the version the server now has.
func (s *server) sync(uri, languageID, text string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	d := s.docs[uri]
	if d == nil {
		d = &document{version: 1, text: text}
		s.docs[uri] = d
		return d.version, s.conn.Notify(ctx, "textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": languageID, "version": d.version, "text": text},
		})
	}
	if d.text == text {
		return d.version, nil
	}
	d.version++
	d.text = text
	// A change without a range replaces the whole text; fine for task-sized files.
	return d.version, s.conn.Notify(ctx, "textDocument/didChange", map[string]any{
		"textDocument":   versionedDocument{URI: uri, Version: d.version},
		"contentChanges": []map[string]string{{"text": text}},
	})
}

// focus tells the server which file is in front, if it has that file open.
func (s *server) focus(ctx context.Context, uri string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.docs[uri] == nil {
		return nil
	}
	return s.conn.Notify(ctx, "textDocument/didFocus", map[string]any{"textDocument": map[string]string{"uri": uri}})
}

// closeAll closes every file the server has open.
func (s *server) closeAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for uri := range s.docs {
		errs = append(errs, s.conn.Notify(ctx, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}}))
	}
	clear(s.docs)
	return errors.Join(errs...)
}

// notice is what the server last said is wrong, or "".
func (s *server) notice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

// handle answers the server's requests and notifications.
func (s *server) handle(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
	var params json.RawMessage
	if req.Params != nil {
		params = *req.Params
	}
	switch req.Method {
	case "window/showDocument":
		p, err := decode.JSON[struct {
			URI string `json:"uri"`
		}](params)
		if err != nil {
			return nil, err
		}
		if wails := application.Get(); wails != nil {
			if err := wails.Browser.OpenURL(p.URI); err != nil {
				return map[string]bool{"success": false}, nil
			}
		}
		return map[string]bool{"success": true}, nil
	case "didChangeStatus":
		// Normal and Inactive (a file Copilot ignores) are fine; Warning and
		// Error say why suggestions stopped.
		p, err := decode.JSON[struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		}](params)
		if err != nil {
			return nil, nil
		}
		message := ""
		if p.Kind == "Warning" || p.Kind == "Error" {
			message = p.Message
		}
		s.setNotice(message)
	case "window/showMessage", "window/showMessageRequest":
		// Account and billing news, such as a used-up monthly allowance.
		p, err := decode.JSON[struct {
			Message string `json:"message"`
		}](params)
		if err == nil {
			s.setNotice(p.Message)
		}
	case "workspace/configuration":
		p, err := decode.JSON[struct {
			Items []json.RawMessage `json:"items"`
		}](params)
		if err != nil {
			return nil, err
		}
		return make([]any, len(p.Items)), nil // defaults for every item
	}
	// Anything else, such as registering capabilities, needs no answer.
	return nil, nil
}

// setNotice keeps what the server says is wrong; "" means nothing is.
func (s *server) setNotice(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}

// call sends a request and waits for its result.
func call[T any](ctx context.Context, s *server, method string, params any) (T, error) {
	var zero T
	var raw json.RawMessage
	if err := s.conn.Call(ctx, method, params, &raw); err != nil {
		return zero, fmt.Errorf("copilot %s: %w", method, err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return zero, nil
	}
	return decode.JSON[T](raw)
}

// document is a file the server has open.
type document struct {
	version int
	text    string
}

// stdio is the server's stdout and stdin as one stream.
type stdio struct {
	io.ReadCloser
	io.WriteCloser
}

func (p stdio) Close() error {
	return p.WriteCloser.Close()
}

// command is a command the server gives the client to run, such as the one
// finishing sign-in.
type command struct {
	Command   string `json:"command"`
	Title     string `json:"title,omitempty"`
	Arguments []any  `json:"arguments"`
}

// authStatus is the answer to checkStatus, signOut and finishing sign-in.
type authStatus struct {
	Status authState `json:"status"`
	User   string    `json:"user"`
}

// describe returns who is signed in, and why they get no suggestions if
// they don't.
func (a authStatus) describe() (user, problem string) {
	switch a.Status {
	case authOK, authMaybeOK, authAlreadySignedIn:
		return a.User, ""
	case authNotAuthorized:
		return a.User, "This GitHub account doesn't have Copilot. Turn on Copilot Free at github.com/settings/copilot, then check again."
	case authNotSignedIn, authPromptDeviceFlow:
		return "", ""
	case authFailedToGetToken:
		return a.User, "Copilot couldn't reach GitHub. Check your connection, then check again."
	case authNoTelemetryConsent:
		return a.User, "GitHub needs you to accept Copilot's telemetry terms. Check your Copilot settings at github.com/settings/copilot, then check again."
	}
	return a.User, fmt.Sprintf("Copilot reports %q.", a.Status)
}

type authState string

const (
	authOK               authState = "OK"
	authMaybeOK          authState = "MaybeOK"
	authAlreadySignedIn  authState = "AlreadySignedIn"
	authNotAuthorized    authState = "NotAuthorized"
	authNotSignedIn      authState = "NotSignedIn"
	authPromptDeviceFlow authState = "PromptUserDeviceFlow"
	authFailedToGetToken authState = "FailedToGetToken"
	// Reported by GitHub's other plugins (copilot.lua) for accounts that
	// haven't accepted the telemetry terms.
	authNoTelemetryConsent authState = "NoTelemetryConsent"
)

// signInResult is the answer to signIn.
type signInResult struct {
	Status          authState `json:"status"`
	UserCode        string    `json:"userCode"`
	VerificationURI string    `json:"verificationUri"`
	Command         *command  `json:"command"`
}

type versionedDocument struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type inlineCompletionParams struct {
	TextDocument      versionedDocument       `json:"textDocument"`
	Position          Position                `json:"position"`
	Context           inlineCompletionContext `json:"context"`
	FormattingOptions formattingOptions       `json:"formattingOptions"`
}

type inlineCompletionContext struct {
	TriggerKind int `json:"triggerKind"`
}

// triggerAutomatic is the trigger kind for suggestions asked for while typing.
const triggerAutomatic = 2

// codeNotSignedIn is the error code of a request that needs a GitHub sign-in.
const codeNotSignedIn = 1000

type formattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type completionList struct {
	Items []json.RawMessage `json:"items"` // kept as sent, for Suggestion.Item
}

type completionItem struct {
	InsertText string   `json:"insertText"`
	Range      Range    `json:"range"`
	Command    *command `json:"command"` // run when the learner accepts it all
}
