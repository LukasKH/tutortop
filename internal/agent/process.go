package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// process is a long-running agent CLI with piped stdio.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr tailBuffer
	done   chan struct{}
	err    error

	wmu sync.Mutex // one message per line on stdin, from any goroutine
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

// decode unmarshals one JSON message, or a field of one, into a T. Like
// json.Unmarshal, it keeps the fields that decoded when another one doesn't:
// a tool call's file_path must survive an odd value elsewhere in its input,
// or the permission check would see no path.
func decode[T any](raw []byte) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
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

func startProcess(ctx context.Context, cwd string, env []string, binary string, args ...string) (*process, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%s isn't installed or isn't on PATH", binary)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	p := &process{cmd: cmd, done: make(chan struct{})}
	cmd.Stderr = &p.stderr
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if p.stdout, err = cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", binary, err)
	}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *process) Closed() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *process) Close() error {
	_ = p.stdin.Close()
	if !p.Closed() && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.done
	return nil
}

// exitError explains why the process stopped, using the tail of stderr.
func (p *process) exitError() error {
	msg := strings.TrimSpace(p.stderr.String())
	if msg == "" && p.err != nil {
		msg = p.err.Error()
	}
	if msg == "" {
		msg = "no output"
	}
	return fmt.Errorf("%w: %s", ErrStopped, msg)
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
