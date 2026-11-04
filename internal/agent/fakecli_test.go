package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fake CLI tests an adapter's protocol mapping without the real CLI.
// fakeBinary puts this test binary on PATH under the CLI's name. When the
// adapter starts it, TestMain sees fakeCLIEnv and, instead of running tests,
// relays the process's stdio to the test over a local socket. The test then
// plays the CLI: it reads what the adapter wrote (recv), writes the CLI's
// output (send), and can make it exit (exit). The adapter runs unchanged,
// through the real startProcess.
//
//	clis := fakeBinary(t, "mycli")
//	started := async(func() (err error) { sess, err = startMyCLI(ctx, opts, log.add); return })
//	cli := clis.next(t)
//	hello := cli.recv(t)      // the adapter's first message
//	cli.send(`{"id":1,"result":{}}`)
//	wait(t, started)
const fakeCLIEnv = "TUTORTOP_FAKE_CLI"

func TestMain(m *testing.M) {
	if addr := os.Getenv(fakeCLIEnv); addr != "" {
		os.Exit(runFakeCLI(addr))
	}
	os.Exit(m.Run())
}

// fakeProcesses are the fake CLI processes the adapter starts.
type fakeProcesses struct {
	started chan *fakeCLI
}

// fakeBinary makes binary, looked up on PATH, start the fake CLI, and returns
// the processes as they start.
func fakeBinary(t *testing.T, binary string) *fakeProcesses {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(dir, binary)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv(fakeCLIEnv, ln.Addr().String())

	ps := &fakeProcesses{started: make(chan *fakeCLI, 8)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			cli, err := newFakeCLI(conn)
			if err != nil {
				_ = conn.Close()
				continue
			}
			t.Cleanup(func() { _ = conn.Close() })
			ps.started <- cli
		}
	}()
	return ps
}

// next waits for the next process to start.
func (ps *fakeProcesses) next(t *testing.T) *fakeCLI {
	t.Helper()
	select {
	case cli := <-ps.started:
		return cli
	case <-time.After(fakeTimeout):
		t.Fatal("the adapter didn't start the CLI")
		return nil
	}
}

// fakeTimeout bounds every wait, so a test that the adapter leaves hanging
// fails instead of timing out the whole run.
const fakeTimeout = 10 * time.Second

// fakeCLI is one fake CLI process, as seen from the test.
type fakeCLI struct {
	Args []string // the arguments it was started with
	Dir  string   // its working directory
	Env  []string

	conn  net.Conn
	lines chan []byte // lines the adapter wrote to its stdin; closed on EOF
	wmu   sync.Mutex
}

func newFakeCLI(conn net.Conn) (*fakeCLI, error) {
	r := bufio.NewReader(conn)
	header, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	cli := &fakeCLI{conn: conn, lines: make(chan []byte, 64)}
	if err := json.Unmarshal(header, &fakeStart{&cli.Args, &cli.Dir, &cli.Env}); err != nil {
		return nil, err
	}
	go func() {
		defer close(cli.lines)
		sc := bufio.NewScanner(r)
		sc.Buffer(nil, 64*1024*1024)
		for sc.Scan() {
			cli.lines <- slices.Clone(sc.Bytes())
		}
	}()
	return cli, nil
}

// recv returns the next message the adapter wrote to the CLI's stdin.
func (c *fakeCLI) recv(t *testing.T) message {
	t.Helper()
	select {
	case line, ok := <-c.lines:
		if !ok {
			t.Fatal("the adapter closed the CLI's stdin")
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("the adapter wrote %q, not a JSON object: %v", line, err)
		}
		return m
	case <-time.After(fakeTimeout):
		t.Fatal("the adapter sent nothing")
		return nil
	}
}

// send writes lines to the CLI's stdout, for the adapter to read. A string
// is sent as it is, so recorded output can be pasted in; anything else is
// sent as JSON.
func (c *fakeCLI) send(t *testing.T, msgs ...any) {
	t.Helper()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	for _, m := range msgs {
		line, ok := m.(string)
		if !ok {
			b, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			line = string(b)
		}
		if strings.HasPrefix(line, fakeExit) || strings.Contains(line, "\n") {
			t.Fatalf("send takes one line of JSON at a time, not %q", line)
		}
		if _, err := io.WriteString(c.conn, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// exit makes the CLI print stderr and exit with code.
func (c *fakeCLI) exit(t *testing.T, code int, stderr string) {
	t.Helper()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.conn, "%s%d %s\n", fakeExit, code, strconv.Quote(stderr)); err != nil {
		t.Fatal(err)
	}
}

// fakeExit starts the line that tells the fake CLI to exit. Protocol lines
// are JSON, so they never start with it.
const fakeExit = "!exit "

// arg returns the value after flag in the CLI's arguments.
func (c *fakeCLI) arg(flag string) string {
	i := slices.Index(c.Args, flag)
	if i < 0 || i+1 >= len(c.Args) {
		return ""
	}
	return c.Args[i+1]
}

// message is one JSON message the adapter sent.
type message map[string]any

// get returns the value at a dotted path: "params.input.0.text".
func (m message) get(path string) any {
	var v any = map[string]any(m)
	for key := range strings.SplitSeq(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// str returns the string at path, or "".
func (m message) str(path string) string {
	s, _ := m.get(path).(string)
	return s
}

// fakeStart is the first line the fake CLI sends: how it was started.
type fakeStart struct {
	Args *[]string `json:"args"`
	Dir  *string   `json:"dir"`
	Env  *[]string `json:"env"`
}

// runFakeCLI is the fake CLI's process: it relays its stdin to the test and
// the test's lines to its stdout, until the test says to exit.
func runFakeCLI(addr string) int {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake CLI:", err)
		return 2
	}
	args, env := os.Args[1:], os.Environ()
	dir, _ := os.Getwd()
	if err := json.NewEncoder(conn).Encode(fakeStart{&args, &dir, &env}); err != nil {
		return 2
	}
	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		_ = conn.(*net.TCPConn).CloseWrite()
	}()
	sc := bufio.NewScanner(conn)
	sc.Buffer(nil, 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, fakeExit); ok {
			codeText, msg, _ := strings.Cut(rest, " ")
			code, _ := strconv.Atoi(codeText)
			stderr, _ := strconv.Unquote(msg)
			fmt.Fprint(os.Stderr, stderr)
			return code
		}
		if _, err := io.WriteString(os.Stdout, line+"\n"); err != nil {
			return 1
		}
	}
	return 0 // the test is done with it
}

// eventLog records a session's events.
type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) add(ev Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
}

// take returns the events so far and forgets them.
func (l *eventLog) take() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	evs := l.events
	l.events = nil
	return evs
}

// async runs f, which blocks, while the test plays the CLI.
func async(f func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	return done
}

// wait returns f's error once it's done.
func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(fakeTimeout):
		t.Fatal("still waiting for the adapter")
		return nil
	}
}
