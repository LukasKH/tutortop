package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LukasKH/tutortop/internal/sandbox"
)

// Languages other than the built-in ones (sandbox.Builtin) are set up by the
// tutor per workspace and kept in .tutortop/toolchains/<language>.yaml.

// Toolchain returns how a language's code is checked in this workspace. The
// workspace's own toolchain comes first: one the tutor set up before the app
// shipped the language keeps checking the tasks that passed the quality gate
// with it. New ones can't be set up for a built-in language (set_toolchain).
func (w *Workspace) Toolchain(language string) (sandbox.Toolchain, error) {
	spec, err := w.toolchainSpec(language)
	if err == nil {
		return spec.Toolchain()
	}
	if tc, ok := sandbox.Builtin(language); ok {
		return tc, nil
	}
	return sandbox.Toolchain{}, err
}

func (w *Workspace) toolchainSpec(language string) (sandbox.ToolchainSpec, error) {
	if !validName(language) {
		return sandbox.ToolchainSpec{}, fmt.Errorf("unsupported language %q", language)
	}
	spec, err := readYAML[sandbox.ToolchainSpec](w.path(".tutortop", "toolchains", language+".yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return spec, fmt.Errorf("unsupported language %q (built in: %s; set up others with set_toolchain first)", language, strings.Join(sandbox.BuiltinLanguages(), ", "))
	}
	return spec, err
}

// SaveToolchain stores a language set up by the tutor. The spec must already
// be validated and its image built.
func (w *Workspace) SaveToolchain(spec sandbox.ToolchainSpec) error {
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
func (w *Workspace) ToolchainSpecs() []sandbox.ToolchainSpec {
	entries, _ := os.ReadDir(w.path(".tutortop", "toolchains"))
	out := []sandbox.ToolchainSpec{}
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
