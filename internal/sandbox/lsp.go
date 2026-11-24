package sandbox

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/textproto"
	"os/exec"
	"strconv"
	"sync"
	"time"
	"uuid"

	"github.com/LukasKH/tutortop/internal/decode"
)

// The language server protocol frames each message as a Content-Length
// header, a blank line, then JSON. The editor relays them as they are.

// LanguageServer is a toolchain's language server running in a task's
// sandbox, which the app talks to over stdio.
type LanguageServer struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	// Root is the directory holding the task files, as the server sees it.
	Root string

	cmd       *exec.Cmd
	cleanup   func()
	closeOnce sync.Once
}

// StartLanguageServer runs the toolchain's language server with a snapshot of
// the task's files. Like checks, it runs without network and with capped
// resources. Edits reach it through the protocol, not the file system.
func (r *Runner) StartLanguageServer(ctx context.Context, tc Toolchain, files Files) (*LanguageServer, error) {
	if len(tc.LanguageServer) == 0 {
		return nil, errors.New("this language has no language server")
	}
	gen, err := r.prepare(ctx, tc)
	if err != nil {
		return nil, err
	}
	all := Files{}
	maps.Copy(all, tc.ExtraFiles)
	maps.Copy(all, files)
	archive, err := tarFiles(all)
	if err != nil {
		return nil, err
	}
	name := "tutortop-lsp-" + uuid.New().String()
	// A language server loads the same libraries as the checks, so it gets
	// at least as much memory.
	args := append([]string{"create", "-i", "--rm", "--name", name}, lockdownFlags(max(1024, tc.MemoryMB))...)
	args = append(append(args, tc.Image), tc.LanguageServer...)
	out, err := r.command(ctx, args...).CombinedOutput()
	if err != nil {
		rebuilt, rebuildErr := r.rebuildIfMissing(ctx, tc, gen)
		if rebuildErr != nil {
			return nil, rebuildErr
		}
		if rebuilt {
			out, err = r.command(ctx, args...).CombinedOutput()
		}
	}
	if err != nil {
		return nil, errors.New("starting the language server: " + string(bytes.TrimSpace(out)))
	}
	remove := func() { _ = r.command(context.Background(), "rm", "-f", name).Run() }
	cp := r.command(ctx, "cp", "-a", "-", name+":/work")
	cp.Stdin = bytes.NewReader(archive)
	if out, err := cp.CombinedOutput(); err != nil {
		remove()
		return nil, errors.New("copying files to the language server: " + string(bytes.TrimSpace(out)))
	}

	cmd := r.command(context.Background(), "start", "-a", "-i", name)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		remove()
		return nil, err
	}
	// An io.Pipe rather than StdoutPipe, so Wait can run while we still read.
	stdout, w := io.Pipe()
	cmd.Stdout = w
	if err := cmd.Start(); err != nil {
		remove()
		return nil, err
	}
	s := &LanguageServer{
		Stdin:  stdin,
		Stdout: stdout,
		Root:   "/work",
		cmd:    cmd,
		cleanup: func() {
			remove()
			_ = stdout.Close()
		},
	}
	// Reap the process when it exits on its own; the reader then sees EOF.
	go func() {
		err := cmd.Wait()
		_ = w.CloseWithError(cmp.Or(err, io.EOF))
		s.Close()
	}()
	return s, nil
}

// Close stops the server and removes its container.
func (s *LanguageServer) Close() {
	s.closeOnce.Do(func() {
		_ = s.Stdin.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		s.cleanup()
	})
}

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
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"processId": nil, "rootUri": "file://" + srv.Root, "capabilities": map[string]any{}},
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
			if err != nil || string(resp.ID) != "1" || (resp.Result == nil && resp.Error == nil) {
				continue // not the answer to initialize
			}
			if resp.Error != nil {
				done <- errors.New("initialize failed: " + resp.Error.Message)
				return
			}
			done <- nil
			return
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
