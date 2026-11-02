//go:build !windows

package shellenv

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestLoadPath(t *testing.T) {
	// A login shell whose rc files print noise around the PATH.
	shell := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\necho 'welcome!'\nprintf '__TUTORTOP_PATH__/shell/bin:/usr/bin__TUTORTOP_PATH__'\necho bye\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/usr/bin:/bin")

	LoadPath()

	path := filepath.SplitList(os.Getenv("PATH"))
	if !slices.Equal(path[:3], []string{"/usr/bin", "/bin", "/shell/bin"}) {
		t.Errorf("PATH starts %q, want the app's PATH, then the shell's without duplicates", path[:3])
	}
	if !slices.Contains(path, "/usr/local/bin") {
		t.Errorf("PATH %q lacks /usr/local/bin", path)
	}
}

// Regression test: under `wails3 dev` the app runs in a terminal's background,
// and an interactive shell started in the app's session stopped them both.
func TestShellRunsInItsOwnSession(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	// Prints /own-session if the shell leads a new session (session ID = PID).
	shell := filepath.Join(t.TempDir(), "shell")
	script := `#!/bin/sh
read -r stat </proc/$$/stat
set -- $stat
[ "$6" = "$$" ] && dir=/own-session || dir=/app-session
printf '__TUTORTOP_PATH__%s__TUTORTOP_PATH__' "$dir"
`
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := shellPath(shell); got != "/own-session" {
		t.Errorf("shell ran in %s, want /own-session", got)
	}
}

func TestLoadPathWithBrokenShell(t *testing.T) {
	t.Setenv("SHELL", "/does/not/exist")
	t.Setenv("PATH", "/usr/bin")

	LoadPath()

	path := filepath.SplitList(os.Getenv("PATH"))
	if path[0] != "/usr/bin" || !slices.Contains(path, "/opt/homebrew/bin") {
		t.Errorf("PATH = %q, want the app's PATH plus the usual folders", path)
	}
}
