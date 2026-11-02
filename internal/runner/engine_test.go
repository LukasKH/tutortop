package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQualifyImages(t *testing.T) {
	tests := []struct{ in, want string }{
		{"FROM golang:1.27-alpine", "FROM docker.io/library/golang:1.27-alpine"},
		{"from python:3.14-slim AS base", "from docker.io/library/python:3.14-slim AS base"},
		{"FROM rocker/r-ver:4", "FROM docker.io/rocker/r-ver:4"},
		{"FROM golang@sha256:abc", "FROM docker.io/library/golang@sha256:abc"},
		{"FROM --platform=linux/amd64 node:24", "FROM --platform=linux/amd64 docker.io/library/node:24"},
		{"FROM ghcr.io/owner/image:1", "FROM ghcr.io/owner/image:1"},
		{"FROM localhost:5000/image", "FROM localhost:5000/image"},
		{"FROM localhost/image", "FROM localhost/image"},
		{"FROM scratch", "FROM scratch"},
		{"FROM ${BASE}", "FROM ${BASE}"},
		{"# FROM golang", "# FROM golang"},
		{"RUN echo FROM golang", "RUN echo FROM golang"},
		// A later stage that builds on an earlier one keeps the stage's name.
		{"FROM golang:1 AS build\nRUN go build\nFROM build", "FROM docker.io/library/golang:1 AS build\nRUN go build\nFROM build"},
	}
	for _, tt := range tests {
		if got := qualifyImages(tt.in); got != tt.want {
			t.Errorf("qualifyImages(%q)\n got %q\nwant %q", tt.in, got, tt.want)
		}
	}
}

// Fake engine commands: shell scripts that answer `info`.
const (
	works   = "exit 0"
	down    = "echo 'cannot connect' >&2; exit 1"
	noGroup = "echo 'permission denied while trying to connect to the docker API' >&2; exit 1"
	// Docker Desktop that's installed but stopped; `docker desktop start`
	// leaves a file next to the script.
	desktop = `if [ "$1" = desktop ]; then : >"$0.started"; exit 0; fi; echo 'cannot connect' >&2; exit 1`
)

// fakeEngines makes the fakes the only commands on PATH, so engines missing
// from the map aren't installed. It returns their folder.
func fakeEngines(t *testing.T, scripts map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses shell scripts")
	}
	dir := t.TempDir()
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

// onPlatform makes the engine code behave as on the given OS.
func onPlatform(t *testing.T, os string) {
	old := goos
	goos = os
	t.Cleanup(func() { goos = old })
}

func TestDetectEngine(t *testing.T) {
	tests := []struct {
		name    string
		os      string
		engines map[string]string
		want    string
	}{
		{"linux prefers podman", "linux", map[string]string{"podman": works, "docker": works}, "podman"},
		{"falls back to a working docker", "linux", map[string]string{"podman": down, "docker": works}, "docker"},
		{"reports the first installed when none works", "linux", map[string]string{"podman": down, "docker": down}, "podman"},
		{"docker only", "linux", map[string]string{"docker": down}, "docker"},
		{"nothing installed on linux", "linux", map[string]string{}, "podman"},
		{"macos prefers docker", "darwin", map[string]string{"podman": works, "docker": works}, "docker"},
		{"falls back to a working podman", "darwin", map[string]string{"podman": works, "docker": down}, "podman"},
		{"nothing installed on windows", "windows", map[string]string{}, "docker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			onPlatform(t, tt.os)
			fakeEngines(t, tt.engines)
			if got := detectEngine(); got != tt.want {
				t.Errorf("detectEngine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckHints(t *testing.T) {
	tests := []struct {
		name    string
		os      string
		engine  string
		engines map[string]string
		want    string
	}{
		{"works", "linux", "podman", map[string]string{"podman": works}, ""},
		{"podman missing", "linux", "podman", map[string]string{}, "podman isn't installed. Install it with"},
		{"docker group", "linux", "docker", map[string]string{"docker": noGroup}, "easiest fix is Podman"},
		{"engine's own error", "linux", "podman", map[string]string{"podman": down}, "can't reach podman: cannot connect"},
		{"docker desktop missing on macos", "darwin", "docker", map[string]string{}, "brew install --cask docker-desktop"},
		{"docker desktop missing on windows", "windows", "docker", map[string]string{}, "winget install Docker.DockerDesktop"},
		{"docker without docker desktop", "darwin", "docker", map[string]string{"docker": down}, "If you use Docker Desktop, open it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			onPlatform(t, tt.os)
			fakeEngines(t, tt.engines)
			err := (&Runner{Engine: tt.engine}).Check(context.Background())
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("Check() = %v, want no error", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("Check() = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestCheckStartsDockerDesktop(t *testing.T) {
	onPlatform(t, "darwin")
	dir := fakeEngines(t, map[string]string{"docker": desktop})
	started := filepath.Join(dir, "docker.started")
	r := &Runner{Engine: "docker"}

	err := r.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "starting Docker Desktop") {
		t.Fatalf("Check() = %v, want it to say it is starting Docker Desktop", err)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatal("Check didn't run `docker desktop start`")
	}

	// While it starts, checking again doesn't start it again.
	os.Remove(started)
	if err := r.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "starting Docker Desktop") {
		t.Fatalf("second Check() = %v, want it to still be starting", err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Fatal("Check started Docker Desktop twice")
	}
}
