package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"time"

	"github.com/LukasKH/tutortop/internal/decode"
)

// The language server protocol frames each message as a Content-Length
// header, a blank line, then JSON. The editor relays them as they are;
// ProbeLanguageServer checks a toolchain's server answers.

// ProbeLanguageServer checks that a toolchain's language server answers the
// LSP initialize request, so a broken language_server command is caught when
// the tutor sets it up.
func (r *Runner) ProbeLanguageServer(ctx context.Context, tc Toolchain, files Files) error {
	srv, err := r.StartLanguageServer(ctx, tc, files)
	if err != nil {
		return err
	}
	defer srv.Close()
	init, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"processId": nil, "rootUri": "file://" + srv.Root, "capabilities": map[string]any{}},
	})
	if err := WriteMessage(srv.Stdin, init); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		br := bufio.NewReader(srv.Stdout)
		for {
			msg, err := ReadMessage(br)
			if err != nil {
				done <- fmt.Errorf("the language server stopped before answering initialize: %w", err)
				return
			}
			resp, err := decode.JSON[lspResponse](msg)
			if err == nil && string(resp.ID) == "1" && (resp.Result != nil || resp.Error != nil) {
				if resp.Error != nil {
					done <- errors.New("initialize failed: " + resp.Error.Message)
				} else {
					done <- nil
				}
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeoutCause(ctx, 90*time.Second, errors.New("the language server didn't answer initialize within 90 seconds"))
	defer cancel()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// ReadMessage reads one language server protocol message from r.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	header, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(header.Get("Content-Length"))
	if err != nil || n < 0 || n > maxMessage {
		return nil, fmt.Errorf("bad Content-Length %q", header.Get("Content-Length"))
	}
	body := make([]byte, n)
	_, err = io.ReadFull(r, body)
	return body, err
}

// WriteMessage writes body to w as one language server protocol message.
func WriteMessage(w io.Writer, body []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

const maxMessage = 64 << 20

// lspResponse is the part of a JSON-RPC response ProbeLanguageServer reads.
type lspResponse struct {
	ID    json.RawMessage `json:"id"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Result json.RawMessage `json:"result"`
}
