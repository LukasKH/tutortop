package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/sourcegraph/jsonrpc2"
)

func TestCallCancelsOnServer(t *testing.T) {
	received := make(chan jsonrpc2.ID, 1)
	cancelled := make(chan uint64, 1)
	srv := fakeServer(t, func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) {
		switch req.Method {
		case "textDocument/inlineCompletion":
			received <- req.ID // leave unanswered so the client waits until cancelled
		case "$/cancelRequest":
			p, err := decode.JSON[struct {
				ID uint64 `json:"id"`
			}](*req.Params)
			if err == nil {
				cancelled <- p.ID
			}
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := call[completionList](ctx, srv, "textDocument/inlineCompletion", struct{}{})
		done <- err
	}()

	var reqID jsonrpc2.ID
	select {
	case reqID = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the request")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return after cancel")
	}

	select {
	case gotID := <-cancelled:
		if gotID != reqID.Num {
			t.Errorf("cancelled id = %d, want %d", gotID, reqID.Num)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received $/cancelRequest")
	}
}

func TestCancelledContextSkipsSyncAndCall(t *testing.T) {
	seen := make(chan string, 2)
	srv := fakeServer(t, func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) {
		seen <- req.Method
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := srv.sync(ctx, "file:///t1/work/main.go", "go", "stale"); !errors.Is(err, context.Canceled) {
		t.Fatalf("sync error = %v, want context.Canceled", err)
	}
	if _, err := call[completionList](ctx, srv, "textDocument/inlineCompletion", struct{}{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("call error = %v, want context.Canceled", err)
	}
	if len(srv.docs) != 0 {
		t.Errorf("sync recorded %d docs for a cancelled context", len(srv.docs))
	}
	select {
	case m := <-seen:
		t.Errorf("sent %q for an already-cancelled context", m)
	default:
	}
}

func TestSyncSendsDidFocus(t *testing.T) {
	type event struct{ method, uri string }
	events := make(chan event, 16)
	srv := fakeServer(t, func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) {
		p, err := decode.JSON[struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}](*req.Params)
		if err == nil {
			events <- event{req.Method, p.TextDocument.URI}
		}
	})
	want := func(method, uri string) {
		t.Helper()
		select {
		case got := <-events:
			if got != (event{method, uri}) {
				t.Fatalf("event = %+v, want {%s %s}", got, method, uri)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s %s", method, uri)
		}
	}

	ctx := t.Context()
	a, b := "file:///t1/work/a.go", "file:///t1/work/b.go"

	// First sync opens and focuses a; second sync changes a without a duplicate didFocus.
	if _, err := srv.sync(ctx, a, "go", "v1"); err != nil {
		t.Fatal(err)
	}
	want("textDocument/didOpen", a)
	want("textDocument/didFocus", a)

	if _, err := srv.sync(ctx, a, "go", "v2"); err != nil {
		t.Fatal(err)
	}
	want("textDocument/didChange", a)

	// Focus before b is open is skipped; syncing b opens and focuses it.
	if err := srv.focus(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.sync(ctx, b, "go", "v1"); err != nil {
		t.Fatal(err)
	}
	want("textDocument/didOpen", b)
	want("textDocument/didFocus", b)

	// Switching back to already-open a focuses it immediately.
	if err := srv.focus(ctx, a); err != nil {
		t.Fatal(err)
	}
	want("textDocument/didFocus", a)

	// Closing resets focus so reopening a focuses it again.
	if err := srv.closeAll(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case got := <-events:
			if got.method != "textDocument/didClose" {
				t.Fatalf("event = %+v, want didClose", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for didClose")
		}
	}
	if _, err := srv.sync(ctx, a, "go", "v1"); err != nil {
		t.Fatal(err)
	}
	want("textDocument/didOpen", a)
	want("textDocument/didFocus", a)
}

func TestShowDocumentOpensBrowser(t *testing.T) {
	params := json.RawMessage(`{"uri":"https://github.com/login/device"}`)
	req := &jsonrpc2.Request{Method: "window/showDocument", Params: &params}

	var opened []string
	srv := &server{browser: browserFunc(func(u string) error {
		opened = append(opened, u)
		return nil
	})}
	got, err := srv.handle(t.Context(), nil, req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if m, _ := got.(map[string]bool); !m["success"] {
		t.Errorf("result = %v, want success=true", got)
	}
	if len(opened) != 1 || opened[0] != "https://github.com/login/device" {
		t.Errorf("opened = %v, want [https://github.com/login/device]", opened)
	}

	failSrv := &server{browser: browserFunc(func(string) error { return errors.New("boom") })}
	got, err = failSrv.handle(t.Context(), nil, req)
	if err != nil {
		t.Fatalf("handle on error: %v", err)
	}
	if m, _ := got.(map[string]bool); m["success"] {
		t.Errorf("result on browser error = %v, want success=false", got)
	}

	nilSrv := &server{}
	got, err = nilSrv.handle(t.Context(), nil, req)
	if err != nil {
		t.Fatalf("handle with nil browser: %v", err)
	}
	if m, _ := got.(map[string]bool); !m["success"] {
		t.Errorf("result with nil browser = %v, want success=true", got)
	}
}

func fakeServer(t *testing.T, handle func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request)) *server {
	t.Helper()
	c1, c2 := net.Pipe()
	clientConn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewBufferedStream(c1, jsonrpc2.VSCodeObjectCodec{}), handlerFunc(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {}))
	serverConn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewBufferedStream(c2, jsonrpc2.VSCodeObjectCodec{}), handlerFunc(handle))
	t.Cleanup(func() {
		_ = clientConn.Close() // test cleanup
		_ = serverConn.Close() // test cleanup
	})
	return &server{conn: clientConn, docs: map[string]*document{}}
}

type browserFunc func(string) error

func (f browserFunc) OpenURL(url string) error { return f(url) }

type handlerFunc func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request)

func (f handlerFunc) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	f(ctx, conn, req)
}
