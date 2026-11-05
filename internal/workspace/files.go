package workspace

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/LukasKH/tutortop/internal/atomicfile"
	"github.com/LukasKH/tutortop/internal/decode"
	"gopkg.in/yaml.v3"
)

func readJSON[T any](p string) (T, error) {
	return readFile(p, decode.JSON[T])
}

func readYAML[T any](p string) (T, error) {
	return readFile(p, decode.YAML[T])
}

// readFile reads file p with parse, or returns the zero T, not a
// half-decoded one.
func readFile[T any](p string, parse func([]byte) (T, error)) (T, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return *new(T), err
	}
	v, err := parse(b)
	if err != nil {
		return *new(T), err
	}
	return v, nil
}

// readEach reads <dir>/<entry>/<file> for every entry of dir, in name order,
// skipping entries without a readable file.
func readEach[T any](dir, file string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []T{}
	for _, e := range entries {
		if v, err := readYAML[T](filepath.Join(dir, e.Name(), file)); err == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(p, b)
}

func writeYAML(p string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return atomicfile.Write(p, b)
}

func removeIfExists(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func appendJSONL(p string, v any) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func readJSONL[T any](p string) ([]T, error) {
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []T{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	var v T
	for sc.Scan() {
		v = *new(T) // or the previous line's fields leak into this one
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			out = append(out, v)
		}
	}
	return out, sc.Err()
}
