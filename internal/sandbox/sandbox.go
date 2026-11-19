// Package sandbox runs code in a Docker or Podman container: a task's tests
// ([Runner.Run]), the quality gate for new tasks ([Runner.Gate]) and the
// editor's language servers ([Runner.StartLanguageServer]). A [Toolchain]
// says how one language's image is built and its tests are read.
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"

	"github.com/LukasKH/tutortop/internal/text"
)

// Runner runs jobs in containers.
type Runner struct {
	Engine Engine

	builds   singleflight.Group // callers that need the same image share one build
	mu       sync.Mutex
	prepared map[string]bool
}

// Engine is the container CLI that runs the checks: Docker or Podman.
type Engine string

const (
	Docker Engine = "docker"
	Podman Engine = "podman"
)

// New uses Docker if it's installed, or else Podman. With neither installed it keeps "docker", and Available says
// how to get one.
func New() *Runner {
	return &Runner{Engine: detectEngine(), prepared: map[string]bool{}}
}

// Available reports whether the sandbox's engine is usable, with a fix-it
// hint if not.
func (r *Runner) Available(ctx context.Context) error {
	// Works for both engines, and for Podman's `docker` shim: it fails when
	// Docker's daemon or the Podman machine can't be reached.
	out, err := exec.CommandContext(ctx, string(r.Engine), "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		return engineHint(r.Engine, strings.TrimSpace(string(out)), errors.Is(err, exec.ErrNotFound))
	}
	return nil
}

// Prepare builds the language image. The first call can take a few minutes.
// A caller can give up waiting; the build carries on for the others.
func (r *Runner) Prepare(ctx context.Context, tc Toolchain) error {
	r.mu.Lock()
	done := r.prepared[tc.Image]
	r.mu.Unlock()
	if done {
		return nil
	}
	build := r.builds.DoChan(tc.Image, func() (any, error) {
		// Its own deadline, so a stuck build doesn't hold up every later caller.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), buildTimeout)
		defer cancel()
		return nil, r.build(ctx, tc)
	})
	select {
	case res := <-build:
		return res.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RemoveCache deletes the volumes that keep the toolchain caches of runs
// that share cache (Job.Cache), as when its workspace is deleted.
func (r *Runner) RemoveCache(ctx context.Context, cache string) error {
	out, err := exec.CommandContext(ctx, string(r.Engine), "volume", "ls", "-q").Output()
	if err != nil {
		return err
	}
	prefix := cacheVolumePrefix(cache)
	var names []string
	for _, name := range strings.Fields(string(out)) {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return exec.CommandContext(ctx, string(r.Engine), append([]string{"volume", "rm", "-f"}, names...)...).Run()
}

// RemoveUnusedImages deletes the toolchain images that none of used needs:
// those of languages whose Dockerfile changed, of toolchains the tutor tried
// and dropped, and of deleted workspaces. An image a container still runs on
// stays until the next call.
func (r *Runner) RemoveUnusedImages(ctx context.Context, used []Toolchain) error {
	out, err := exec.CommandContext(ctx, string(r.Engine), "image", "ls", "--filter", "reference=tutortop-*", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return err
	}
	for _, image := range unusedImages(strings.Fields(string(out)), used) {
		// Fails only for an image in use, which the next call removes.
		_ = exec.CommandContext(ctx, string(r.Engine), "image", "rm", image).Run()
		r.mu.Lock()
		delete(r.prepared, strings.TrimPrefix(image, podmanLocal))
		r.mu.Unlock()
	}
	return nil
}

func detectEngine() Engine {
	if _, err := exec.LookPath(string(Docker)); err == nil {
		return Docker
	}
	if _, err := exec.LookPath(string(Podman)); err == nil {
		return Podman
	}
	return Docker
}

// buildTimeout only stops a stuck build. A healthy first build can take a
// while: it pulls a base image of a gigabyte or more, and a custom toolchain
// may compile tools.
const buildTimeout = time.Hour

func (r *Runner) build(ctx context.Context, tc Toolchain) error {
	if exec.CommandContext(ctx, string(r.Engine), "image", "inspect", tc.Image).Run() != nil {
		cmd := exec.CommandContext(ctx, string(r.Engine), "build", "-t", tc.Image, "-")
		cmd.Stdin = strings.NewReader(tc.Dockerfile)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("preparing the %s sandbox: %v\n%s", tc.Name, err, text.Tail(string(out), 1500))
		}
	}
	r.mu.Lock()
	r.prepared[tc.Image] = true
	r.mu.Unlock()
	return nil
}

// exec unpacks the files into a throwaway container and runs the script
// there. Runs that share cache keep the toolchain's cache folder in a volume.
func (r *Runner) exec(ctx context.Context, tc Toolchain, script string, files Files, cache string, out *cappedBuffer) error {
	archive, err := tarFiles(files)
	if err != nil {
		return err
	}
	// The images run as an unprivileged user with a build cache baked in,
	// so nothing is shared between runs but the cache volume, if any.
	name := "tutortop-" + uuid.New().String()
	args := append([]string{"run", "--rm", "-i", "--name", name}, lockdownFlags(tc.MemoryMB)...)
	if volume := cacheVolume(cache, tc); volume != "" {
		// A new volume starts as a copy of the image's folder, so it
		// keeps the image's warm cache and its owner.
		args = append(args, "-v", volume+":"+tc.cache)
	}
	cmd := exec.CommandContext(ctx, string(r.Engine), append(args, tc.Image, "sh", "-c", "tar -xf - && "+script)...)
	// Killing the docker client doesn't stop the container, so remove it too.
	cmd.Cancel = func() error {
		_ = exec.Command(string(r.Engine), "rm", "-f", name).Run()
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = bytes.NewReader(archive)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// cacheVolume names the volume that keeps tc's cache folder for the runs
// that share cache, or returns "" when there's nothing to keep.
func cacheVolume(cache string, tc Toolchain) string {
	if cache == "" || tc.cache == "" {
		return ""
	}
	return cacheVolumePrefix(cache) + nameSafe(tc.Language)
}

// cacheVolumePrefix starts the names of cache's volumes, one per language.
// The hash has a fixed length, so no other cache's names start the same way.
func cacheVolumePrefix(cache string) string {
	sum := sha256.Sum256([]byte(cache))
	return "tutortop-cache-" + hex.EncodeToString(sum[:6]) + "-"
}

// engineHint turns a failed `<engine> version` into a fix the learner can act on.
func engineHint(engine Engine, output string, notFound bool) error {
	desktop := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	switch {
	case notFound && desktop:
		return errors.New("install Docker Desktop (or Podman Desktop), then restart Tutortop")
	case notFound:
		return errors.New("install docker or podman with your package manager, then restart Tutortop")
	case engine == Podman && desktop:
		return fmt.Errorf("can't reach Podman. Start it with `podman machine start`: %s", output)
	case strings.Contains(output, "permission denied") && runtime.GOOS == "linux":
		return errors.New("can't reach Docker (permission denied). Add yourself to the docker group: `sudo usermod -aG docker $USER`, then log out and back in")
	case desktop:
		return fmt.Errorf("can't reach Docker. Start Docker Desktop: %s", output)
	case engine == Docker:
		return fmt.Errorf("can't reach Docker. Start it with `sudo systemctl start docker`: %s", output)
	default:
		return fmt.Errorf("can't reach %s: %s", engine, output)
	}
}

// lockdownFlags lock a container down: no network, no capabilities, and
// capped memory, CPU and processes.
func lockdownFlags(memoryMB int) []string {
	return []string{
		"--network", "none",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", fmt.Sprintf("%dm", memoryMB), "--cpus", "1", "--pids-limit", "256",
	}
}

// unusedImages picks the listed images that none of used is built as.
func unusedImages(listed []string, used []Toolchain) []string {
	keep := map[string]bool{}
	for _, tc := range used {
		keep[tc.Image] = true
	}
	var unused []string
	for _, image := range listed {
		name := strings.TrimPrefix(image, podmanLocal)
		// Podman's filter matches a name's last part, so it also lists
		// images pulled from a registry, which the app didn't build.
		if !strings.Contains(name, "/") && !keep[name] {
			unused = append(unused, image)
		}
	}
	return unused
}

// podmanLocal starts the names Podman lists images built here under.
const podmanLocal = "localhost/"

func tarFiles(files Files) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		content := files[name]
		// Owned by the sandbox user (uid 1000), for `docker cp -a`.
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Uid: 1000, Gid: 1000}); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	// Close before taking the bytes: it writes the final padding and the
	// end-of-archive blocks.
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
