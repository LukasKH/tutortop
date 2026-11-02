package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LukasKH/tutortop/internal/runner"
)

// Languages other than the built-in Go and Python are set up by the tutor per
// workspace and kept in .tutortop/toolchains/<language>.yaml.

// Toolchain returns how a language's code is checked in this workspace.
func (w *Workspace) Toolchain(language string) (runner.Toolchain, error) {
	if tc, ok := runner.Builtin(language); ok {
		return tc, nil
	}
	spec, err := w.toolchainSpec(language)
	if err != nil {
		return runner.Toolchain{}, err
	}
	return spec.Toolchain()
}

func (w *Workspace) toolchainSpec(language string) (runner.ToolchainSpec, error) {
	if !validName(language) {
		return runner.ToolchainSpec{}, fmt.Errorf("unsupported language %q", language)
	}
	spec, err := readYAML[runner.ToolchainSpec](w.path(".tutortop", "toolchains", language+".yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return spec, fmt.Errorf("unsupported language %q (built in: go, python; set up others with set_toolchain first)", language)
	}
	return spec, err
}

// SaveToolchain stores a language set up by the tutor. The spec must already
// be validated and its image built.
func (w *Workspace) SaveToolchain(spec runner.ToolchainSpec) error {
	if _, err := spec.Toolchain(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.path(".tutortop", "toolchains", spec.Language+".yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := writeYAML(p, spec); err != nil {
		return err
	}
	w.commit("Set up " + spec.Name)
	return nil
}

// ToolchainSpecs lists the languages the tutor has set up.
func (w *Workspace) ToolchainSpecs() []runner.ToolchainSpec {
	entries, _ := os.ReadDir(w.path(".tutortop", "toolchains"))
	out := []runner.ToolchainSpec{}
	for _, e := range entries {
		if lang, ok := strings.CutSuffix(e.Name(), ".yaml"); ok {
			if spec, err := w.toolchainSpec(lang); err == nil {
				out = append(out, spec)
			}
		}
	}
	return out
}

// languageCheck reports whether a language can be used in this workspace.
func (w *Workspace) languageCheck(language string) error {
	_, err := w.Toolchain(language)
	return err
}
