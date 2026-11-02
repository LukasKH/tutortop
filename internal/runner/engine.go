package runner

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// goos is runtime.GOOS, a variable so tests can check other platforms' hints.
var goos = runtime.GOOS

// engines in order of preference. On Linux, Podman works as soon as it's
// installed, while Docker also needs the user in the docker group, which no
// package manager can do for them. On macOS and Windows both run in a VM, and
// Docker Desktop is the easier one to set up.
func engines() []string {
	if goos == "linux" {
		return []string{"podman", "docker"}
	}
	return []string{"docker", "podman"}
}

// detectEngine returns the first engine that responds. If none does, it returns
// the first one installed, so Check can say what's wrong with it.
func detectEngine() string {
	installed := ""
	for _, engine := range engines() {
		if _, err := exec.LookPath(engine); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := ping(ctx, engine)
		cancel()
		if err == nil {
			return engine
		}
		installed = cmp.Or(installed, engine)
	}
	return cmp.Or(installed, engines()[0])
}

// ping asks the engine for its status. The error carries the engine's own
// explanation.
func ping(ctx context.Context, engine string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, engine, "info")
	cmd.Stderr = &stderr
	err := cmd.Run()
	switch {
	case err == nil, errors.Is(err, exec.ErrNotFound):
		return err
	case ctx.Err() != nil:
		return errors.New("it didn't respond")
	}
	return errors.New(cmp.Or(strings.TrimSpace(stderr.String()), err.Error()))
}

// Check reports whether the sandbox is usable, with a fix-it hint if not. On
// macOS and Windows it starts Docker Desktop if it's installed but not running.
func (r *Runner) Check(ctx context.Context) error {
	if r.Engine == "local" {
		return nil
	}
	err := ping(ctx, r.Engine)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, exec.ErrNotFound) && r.Engine == engines()[0]:
		return errors.New(installHint())
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Errorf("%s isn't installed", r.Engine)
	case r.Engine == "docker" && goos != "linux":
		return r.startDockerDesktop(err)
	case r.Engine == "docker" && strings.Contains(err.Error(), "permission denied"):
		return fmt.Errorf("your user isn't allowed to use Docker. The easiest fix is Podman, which needs no setup: install it with %s, then restart Tutortop. "+
			"Or join the docker group with `sudo usermod -aG docker $USER`, then log out and back in", podmanInstall())
	}
	return fmt.Errorf("can't reach %s: %v", r.Engine, err)
}

// startDockerDesktop starts Docker Desktop, which can take a minute. If it still
// isn't up two minutes later, it tries again.
func (r *Runner) startDockerDesktop(pingErr error) error {
	if last := r.desktopStarted.Load(); last == 0 || time.Since(time.Unix(0, last)) > 2*time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Older Docker Desktops lack this command, and so do setups like Colima
		// that use the docker CLI without Docker Desktop.
		if exec.CommandContext(ctx, "docker", "desktop", "start", "--detach").Run() != nil {
			return fmt.Errorf("can't reach Docker: %v. If you use Docker Desktop, open it", pingErr)
		}
		r.desktopStarted.Store(time.Now().UnixNano())
	}
	return errors.New("starting Docker Desktop, which takes a minute. If it asks you to accept its terms, do that")
}

// installHint says how to install the engine this platform prefers.
func installHint() string {
	switch goos {
	case "darwin":
		return "Docker Desktop isn't installed. Install it with `brew install --cask docker-desktop` or from docker.com, then restart Tutortop"
	case "windows":
		return "Docker Desktop isn't installed. Install it with `winget install Docker.DockerDesktop` or from docker.com, then restart Tutortop"
	}
	return "podman isn't installed. Install it with " + podmanInstall() + ", then restart Tutortop"
}

// podmanInstall is the command that installs Podman on this Linux system.
func podmanInstall() string {
	ids := linuxDistro()
	for _, pm := range []struct{ distro, command string }{
		{"arch", "sudo pacman -S podman"},
		{"debian", "sudo apt install podman"}, // Ubuntu and Mint list debian in ID_LIKE
		{"fedora", "sudo dnf install podman"}, // so do RHEL and CentOS
		{"suse", "sudo zypper install podman"},
	} {
		for _, id := range ids {
			if id == pm.distro {
				return "`" + pm.command + "`"
			}
		}
	}
	return "your package manager (the package is called podman)"
}

// linuxDistro lists the distribution and the ones it's based on, from
// /etc/os-release (ID=ubuntu, ID_LIKE=debian gives [ubuntu debian]).
func linuxDistro() []string {
	data, _ := os.ReadFile("/etc/os-release")
	var ids []string
	for line := range strings.Lines(string(data)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		if key == "ID" || key == "ID_LIKE" {
			ids = append(ids, strings.Fields(strings.Trim(value, `"'`))...)
		}
	}
	return ids
}

// qualifyImages writes Docker Hub images in FROM lines out in full
// (golang:1.27 becomes docker.io/library/golang:1.27). Podman refuses a short
// name unless it's on its alias list; Docker reads both the same way.
func qualifyImages(dockerfile string) string {
	stages := map[string]bool{} // names from `FROM image AS name`
	lines := strings.Split(dockerfile, "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.EqualFold(f[0], "FROM") {
			continue
		}
		at := 1
		for at < len(f)-1 && strings.HasPrefix(f[at], "--") { // --platform=...
			at++
		}
		f[at] = qualifyImage(f[at], stages)
		if at+2 < len(f) && strings.EqualFold(f[at+1], "AS") {
			stages[strings.ToLower(f[at+2])] = true
		}
		lines[i] = strings.Join(f, " ")
	}
	return strings.Join(lines, "\n")
}

func qualifyImage(ref string, stages map[string]bool) string {
	if stages[strings.ToLower(ref)] || ref == "scratch" || strings.Contains(ref, "$") {
		return ref
	}
	first, _, nested := strings.Cut(ref, "/")
	switch {
	case !nested:
		return "docker.io/library/" + ref
	case strings.ContainsAny(first, ".:") || first == "localhost": // already names a registry
		return ref
	}
	return "docker.io/" + ref
}
