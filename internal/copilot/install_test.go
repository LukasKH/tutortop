package copilot

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnpackReplacesTheInstall(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "copilot")
	archive := filepath.Join(root, "release.zip")
	writeZip(t, archive, map[string]string{
		"package.json":           "{}",
		binaryName(runtime.GOOS): "new server",
	})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath(dir), []byte("old server"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := unpack(archive, dir, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if v := installedVersion(dir); v != "1.2.3" {
		t.Errorf("version %q, want 1.2.3", v)
	}
	b, err := os.ReadFile(binaryPath(dir))
	if err != nil || string(b) != "new server" {
		t.Errorf("server %q (%v), want the new one", b, err)
	}
	if fi, err := os.Stat(binaryPath(dir)); err == nil && runtime.GOOS != "windows" && fi.Mode()&0o100 == 0 {
		t.Errorf("server mode %v, want executable", fi.Mode())
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Errorf("left %d entries beside the install, want only the zip", len(entries)-1)
	}
}

func TestUnpackWithoutTheServerKeepsTheOldOne(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "copilot")
	archive := filepath.Join(root, "release.zip")
	writeZip(t, archive, map[string]string{"README.md": "nothing here"})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath(dir), []byte("old server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version"), []byte("1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := unpack(archive, dir, "1.2.3"); err == nil {
		t.Fatal("unpacked a zip without the server")
	}
	if v := installedVersion(dir); v != "1.0.0" {
		t.Errorf("version %q, want the old 1.0.0", v)
	}
}

func TestDownloadChecksTheDigest(t *testing.T) {
	body := []byte("the release zip")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	sum := sha256.Sum256(body)
	good := "sha256:" + hex.EncodeToString(sum[:])

	tests := []struct {
		name, digest string
		ok           bool
	}{
		{"matching digest", good, true},
		{"other digest", "sha256:" + hex.EncodeToString(make([]byte, 32)), false},
		{"no digest", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path, err := download(t.Context(), asset{Name: "a.zip", URL: srv.URL, Digest: tt.digest}, dir)
			if tt.ok != (err == nil) {
				t.Fatalf("download error %v, want ok %v", err, tt.ok)
			}
			entries, _ := os.ReadDir(dir)
			if !tt.ok && len(entries) != 0 {
				t.Errorf("left %d files after a failed download", len(entries))
			}
			if tt.ok {
				if b, _ := os.ReadFile(path); string(b) != string(body) {
					t.Errorf("saved %q, want %q", b, body)
				}
			}
		})
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct{ goos, goarch, want string }{
		{"linux", "amd64", "copilot-language-server-linux-x64-1.5.0.zip"},
		{"darwin", "arm64", "copilot-language-server-darwin-arm64-1.5.0.zip"},
		{"windows", "amd64", "copilot-language-server-win32-x64-1.5.0.zip"},
	}
	for _, tt := range tests {
		if got := assetName(tt.goos, tt.goarch, "1.5.0"); got != tt.want {
			t.Errorf("assetName(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
