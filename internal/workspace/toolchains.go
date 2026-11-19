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

// ToolchainSpecs lists the languages the tutor has set up, leaving out any
// it can't read.
func (w *Workspace) ToolchainSpecs() []sandbox.ToolchainSpec {
	specs, _ := w.toolchainSpecs()
	return specs
}

// CheckLanguage reports whether a language can be used in this workspace.
func (w *Workspace) CheckLanguage(language string) error {
	_, err := w.Toolchain(language)
	return err
}

// toolchainSpecs reads the languages the tutor has set up. The error joins
// those of the files it couldn't read; the specs are the rest.
func (w *Workspace) toolchainSpecs() ([]sandbox.ToolchainSpec, error) {
	out := []sandbox.ToolchainSpec{} // [] in JSON, not null
	entries, err := os.ReadDir(w.path(".tutortop", "toolchains"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var errs []error
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok {
			continue
		}
		spec, err := w.toolchainSpec(lang)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, spec)
	}
	return out, errors.Join(errs...)
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

// ToolchainsInUse lists the toolchains the tutor set up in the workspaces
// the learner hasn't completed, so their sandbox images can stay. It fails
// if a workspace or its toolchains can't be read, as they would then look
// unused.
func (s *Store) ToolchainsInUse() ([]sandbox.Toolchain, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []sandbox.Toolchain
	for _, e := range entries {
		if !e.IsDir() || !validName(e.Name()) {
			continue
		}
		w, err := s.Open(e.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue // not a workspace
		}
		if err != nil {
			return nil, err
		}
		if w.Progress().Complete() {
			continue
		}
		specs, err := w.toolchainSpecs()
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {
			// One that no longer validates can't run checks, so it needs no image.
			if tc, err := spec.Toolchain(); err == nil {
				out = append(out, tc)
			}
		}
	}
	return out, nil
}
