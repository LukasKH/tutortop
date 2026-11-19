package copilot

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/LukasKH/tutortop/internal/decode"
)

// GitHub publishes each version of the language server as a zip per
// platform, holding one executable.
const latestRelease = "https://api.github.com/repos/github/copilot-language-server-release/releases/latest"

// install downloads the latest language server for this computer into dir,
// replacing what's there.
func install(ctx context.Context, dir string) error {
	rel, err := fetchRelease(ctx)
	if err != nil {
		return err
	}
	name := assetName(runtime.GOOS, runtime.GOARCH, rel.Tag)
	i := slices.IndexFunc(rel.Assets, func(a asset) bool { return a.Name == name })
	if i < 0 {
		return fmt.Errorf("GitHub has no Copilot language server for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	archive, err := download(ctx, rel.Assets[i], parent)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archive) }() // a temporary file
	return unpack(archive, dir, rel.Tag)
}

// installedVersion is the version of the language server in dir, or "" if
// none is installed there.
func installedVersion(dir string) string {
	if _, err := os.Stat(binaryPath(dir)); err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func binaryPath(dir string) string {
	return filepath.Join(dir, binaryName(runtime.GOOS))
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "copilot-language-server.exe"
	}
	return "copilot-language-server"
}

// assetName is the release file for a platform, named as Node names it.
func assetName(goos, goarch, version string) string {
	platform := map[string]string{"windows": "win32"}[goos]
	if platform == "" {
		platform = goos
	}
	arch := map[string]string{"amd64": "x64"}[goarch]
	if arch == "" {
		arch = goarch
	}
	return fmt.Sprintf("copilot-language-server-%s-%s-%s.zip", platform, arch, version)
}

type release struct {
	Tag    string  `json:"tag_name"`
	Assets []asset `json:"assets"`
}

// asset is a file of a release. GitHub publishes its SHA-256 as the digest.
type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // "sha256:<hex>"
}

func fetchRelease(ctx context.Context) (release, error) {
	res, err := get(ctx, latestRelease)
	if err != nil {
		return release{}, err
	}
	defer func() { _ = res.Body.Close() }() // read only: nothing to lose
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return release{}, err
	}
	return decode.JSON[release](b)
}

// download saves a release file to a temporary file in dir and returns its
// path, once its SHA-256 matches the one GitHub published.
func download(ctx context.Context, a asset, dir string) (string, error) {
	want, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok {
		return "", fmt.Errorf("GitHub published no SHA-256 for %s", a.Name)
	}
	res, err := get(ctx, a.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }() // read only: nothing to lose
	f, err := os.CreateTemp(dir, "copilot-*.zip")
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, sum), res.Body)
	err = errors.Join(err, f.Close())
	if err == nil && hex.EncodeToString(sum.Sum(nil)) != want {
		err = fmt.Errorf("%s doesn't match the SHA-256 GitHub published", a.Name)
	}
	if err != nil {
		_ = os.Remove(f.Name()) // incomplete or corrupt
		return "", err
	}
	return f.Name(), nil
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close() // read only: nothing to lose
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return res, nil
}

// unpack puts the executable from the release zip at archive into dir, with
// a file saying its version. It fills a new folder and swaps it in, so a
// failed install leaves the old one working.
func unpack(archive, dir, version string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".copilot-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // gone already if the swap worked
	if err := extract(archive, binaryPath(tmp)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "version"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// extract writes the zip's executable to path.
func extract(archive, path string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }() // read only: nothing to lose
	name := filepath.Base(path)
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }() // read only: nothing to lose
		dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		return errors.Join(err, dst.Close())
	}
	return fmt.Errorf("the download has no %s", name)
}
