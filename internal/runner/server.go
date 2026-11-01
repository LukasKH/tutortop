package runner

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"uuid"
)

// Server is a long-running program in a task's sandbox that the app talks to
// over stdio, such as a language server.
type Server struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	// Root is the directory holding the task files, as the server sees it.
	Root string

	cmd       *exec.Cmd
	cleanup   func()
	closeOnce sync.Once
}

// Close stops the server and removes its container or temp directory.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		_ = s.Stdin.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		s.cleanup()
	})
}

// StartLanguageServer runs the toolchain's language server with a snapshot of
// the task's files. Like checks, it runs without network and with capped
// resources. Edits reach it through the protocol, not the file system.
func (r *Runner) StartLanguageServer(ctx context.Context, tc Toolchain, files Files) (*Server, error) {
	if len(tc.LanguageServer) == 0 {
		return nil, errors.New("this language has no language server")
	}
	all := Files{}
	maps.Copy(all, tc.ExtraFiles)
	maps.Copy(all, files)

	if r.Engine == "local" {
		dir, err := os.MkdirTemp("", "tutortop-lsp-")
		if err != nil {
			return nil, err
		}
		for name, content := range all {
			p := filepath.Join(dir, filepath.FromSlash(name))
			err := os.MkdirAll(filepath.Dir(p), 0o755)
			if err == nil {
				err = os.WriteFile(p, []byte(content), 0o644)
			}
			if err != nil {
				os.RemoveAll(dir)
				return nil, err
			}
		}
		cmd := exec.Command(tc.LanguageServer[0], tc.LanguageServer[1:]...)
		cmd.Dir = dir
		return startServer(cmd, dir, func() { os.RemoveAll(dir) })
	}

	if err := r.Prepare(ctx, tc); err != nil {
		return nil, err
	}
	archive, err := tarFiles(all)
	if err != nil {
		return nil, err
	}
	name := "tutortop-lsp-" + uuid.New().String()
	args := append([]string{"create", "-i", "--rm", "--name", name}, sandboxFlags("1g")...)
	args = append(append(args, tc.Image), tc.LanguageServer...)
	if out, err := exec.CommandContext(ctx, r.Engine, args...).CombinedOutput(); err != nil {
		return nil, errors.New("starting the language server: " + string(bytes.TrimSpace(out)))
	}
	remove := func() { _ = exec.Command(r.Engine, "rm", "-f", name).Run() }
	cp := exec.CommandContext(ctx, r.Engine, "cp", "-a", "-", name+":/work")
	cp.Stdin = bytes.NewReader(archive)
	if out, err := cp.CombinedOutput(); err != nil {
		remove()
		return nil, errors.New("copying files to the language server: " + string(bytes.TrimSpace(out)))
	}
	return startServer(exec.Command(r.Engine, "start", "-a", "-i", name), "/work", remove)
}

func startServer(cmd *exec.Cmd, root string, cleanup func()) (*Server, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, err
	}
	// An io.Pipe rather than StdoutPipe, so Wait can run while we still read.
	stdout, w := io.Pipe()
	cmd.Stdout = w
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	s := &Server{Stdin: stdin, Stdout: stdout, Root: root, cmd: cmd, cleanup: func() {
		cleanup()
		_ = stdout.Close()
	}}
	// Reap the process when it exits on its own; the reader then sees EOF.
	go func() {
		err := cmd.Wait()
		_ = w.CloseWithError(cmp.Or(err, io.EOF))
		s.Close()
	}()
	return s, nil
}
