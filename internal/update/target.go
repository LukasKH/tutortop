package update

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// ManualOnly returns Support with in-app updates disabled and reason explaining
// how to update instead.
func ManualOnly(reason string) Support {
	return Support{ManualReason: reason}
}

// DetectSupport checks whether the running binary can replace itself in place.
func DetectSupport() Support {
	exe, err := os.Executable()
	if err != nil {
		return ManualOnly("Tutortop couldn't locate its own executable. Download updates from GitHub instead.")
	}
	return detectSupport(runtime.GOOS, exe, os.Getenv("APPIMAGE"), os.Getenv("APPDIR"), osProbe{})
}

type targetProbe interface {
	dirWritable(dir string) bool
	isRegularFile(name string) bool
}

type osProbe struct{}

func (osProbe) dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".tutortop-write-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()       // probe file only; removed immediately below
	_ = os.Remove(name) // best-effort cleanup of probe file
	return true
}

func (osProbe) isRegularFile(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular()
}

func detectSupport(goos, exe, appImage, appDir string, probe targetProbe) Support {
	switch goos {
	case "darwin":
		if strings.Contains(exe, "/AppTranslocation/") {
			return ManualOnly("Move Tutortop to Applications to enable automatic updates.")
		}
		bundle, ok := macAppBundle(exe)
		if !ok || strings.HasSuffix(bundle, ".dev.app") {
			return ManualOnly("Automatic updates are only available in the packaged macOS app.")
		}
		if !probe.dirWritable(path.Dir(bundle)) {
			return ManualOnly("Tutortop doesn't have write access to its install folder. Download the latest version from GitHub instead.")
		}
		return Support{CanSelfUpdate: true}

	case "windows":
		if !probe.dirWritable(filepath.Dir(exe)) {
			return ManualOnly("Tutortop doesn't have write access to its install folder. Download the latest installer from GitHub to enable automatic updates.")
		}
		return Support{CanSelfUpdate: true}

	case "linux":
		if isAppImage(exe, appImage, appDir, probe) {
			return ManualOnly("Installed as an AppImage. Download the latest AppImage from GitHub, or install GTK4/WebKitGTK and re-run install.sh for in-app updates.")
		}
		cleanExe := path.Clean(exe)
		if strings.HasPrefix(cleanExe, "/nix/store/") {
			return ManualOnly("Installed via Nix. Update with Nix, or download from GitHub.")
		}
		if strings.HasPrefix(cleanExe, "/usr/") || strings.HasPrefix(cleanExe, "/opt/") {
			return ManualOnly("Installed as a system package. Update with your package manager, or reinstall with install.sh for in-app updates.")
		}
		if !probe.dirWritable(path.Dir(cleanExe)) {
			return ManualOnly("Tutortop doesn't have write access to its install folder. Download the latest version from GitHub instead.")
		}
		return Support{CanSelfUpdate: true}

	default:
		return ManualOnly("Automatic updates aren't available on this platform. Download updates from GitHub instead.")
	}
}

// macAppBundle returns the enclosing .app bundle path when exe runs inside one,
// matching Wails' pkg/updater/updater_darwin.go. Uses POSIX path helpers so
// cross-platform tests pass on Windows too.
func macAppBundle(exe string) (string, bool) {
	parts := strings.Split(path.Clean(exe), "/")
	for i, part := range parts {
		if strings.HasSuffix(part, ".app") {
			return "/" + path.Join(parts[1:i+1]...), true
		}
	}
	return "", false
}

// isAppImage reports whether exe is running out of the mounted AppImage in
// appImage/appDir, matching Wails' pkg/updater/spawn.go.
func isAppImage(exe, appImage, appDir string, probe targetProbe) bool {
	if appImage == "" || appDir == "" {
		return false
	}
	cleanExe := path.Clean(exe)
	cleanDir := path.Clean(appDir)
	if !strings.HasPrefix(cleanExe, cleanDir+"/") {
		return false
	}
	return probe.isRegularFile(appImage)
}
