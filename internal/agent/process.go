package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// process is a long-running agent CLI with piped stdio. Adapters embed it:
// startProcess finds and starts the CLI, send writes one line of JSON, await
// waits for a reply unless the process exits first, and exitError and abort
// turn an exit into ErrExited with the end of stderr.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr tailBuffer
	done   chan struct{}
	err    error
	cancel context.CancelFunc // stops the process: an interrupt, then a kill after WaitDelay

	wmu sync.Mutex // one message per line on stdin, from any goroutine
}

func startProcess(ctx context.Context, cwd string, env []string, binary string, args ...string) (*process, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%s isn't installed or isn't on PATH", binary)
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	// Ask the CLI to stop first, so it can save its session. Windows has no
	// interrupt signal, so there it's killed at once.
	cmd.Cancel = func() error {
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	p := &process{cmd: cmd, done: make(chan struct{}), cancel: cancel}
	cmd.Stderr = &p.stderr
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		cancel()
		return nil, err
	}
	// An io.Pipe rather than StdoutPipe, so Wait can run while we still read.
	stdout, w := io.Pipe()
	p.stdout, cmd.Stdout = stdout, w
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("starting %s: %w", binary, err)
	}
	go func() {
		p.err = cmd.Wait()
		_ = w.CloseWithError(cmp.Or(p.err, io.EOF))
		cancel()
		close(p.done)
	}()
	return p, nil
}

// send writes v to stdin as one line of JSON. Both CLIs speak newline-delimited
// JSON: Claude's stream-json and Codex's app-server JSON-RPC.
func (p *process) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// await waits for a reply, the process exiting, or ctx.
func (p *process) await[T any](ctx context.Context, reply <-chan T) (T, error) {
	select {
	case v := <-reply:
		return v, nil
	case <-p.done:
		return *new(T), p.exitError()
	case <-ctx.Done():
		return *new(T), ctx.Err()
	}
}

func (p *process) Closed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// Close stops the process without waiting for it to exit, since callers may
// hold the app's lock. Closing stdout also frees a write the reader never
// took, so Wait can't hang on it.
func (p *process) Close() error {
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	p.cancel()
	return nil
}

// exitError explains why the process stopped, using the tail of stderr.
// A failed write can call it while the process is still exiting, so it reads
// the exit status only once Wait has set it.
func (p *process) exitError() error {
	msg := strings.TrimSpace(p.stderr.String())
	if msg == "" && p.Closed() && p.err != nil {
		msg = p.err.Error()
	}
	if msg == "" {
		msg = "no output"
	}
	return fmt.Errorf("%w: %s", ErrExited, msg)
}

// abort stops a process whose setup failed. If it had already exited, its
// stderr explains why better than err does.
func (p *process) abort(err error) error {
	if p.Closed() {
		err = p.exitError()
	}
	_ = p.Close()
	return err
}

// tailBuffer keeps the last few KB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > 8192 {
		t.buf = t.buf[len(t.buf)-8192:]
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
