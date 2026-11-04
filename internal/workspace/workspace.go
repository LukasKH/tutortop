// Package workspace stores one learning topic as plain files on disk.
//
// Layout (public, in ~/Tutortop/<id>):
//
//	MISSION.md, NOTES.md, GLOSSARY.md, RESOURCES.md, learning-records/   written by the agent
//	curriculum.yaml                                                         the approved plan
//	tasks/0001-slug/{task.yaml,README.md,starter/,tests/,work/}             tasks; work/ is the learner's code
//	.tutortop/{workspace.json,attempts.jsonl,proposal.yaml,questions.json}  app state
//	.tutortop/toolchains/<language>.yaml                                    languages the tutor set up (beyond the built-in ones)
//	.tutortop/chat/<thread>.jsonl                                           one chat per thread, by its name (Thread)
//
// Private files the learner and tutor shouldn't see (reference solutions,
// hidden tests, mutants) live outside the workspace in the app data dir.
//
// Progress is never stored. The app alone appends to attempts.jsonl
// (attempts.go), and progress and activity are derived from it, so nothing
// the agent writes can change them.
package workspace

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
)

type Store struct {
	Root        string // public workspaces, ~/Tutortop
	PrivateRoot string // ~/.local/share/tutortop/private

	locks sync.Map // workspace id -> *sync.Mutex, shared by every Open of that id
}

// NewStore keeps workspaces in root and their private files under the
// user's data folder.
func NewStore(root string) *Store {
	return &Store{Root: root, PrivateRoot: filepath.Join(xdg.DataHome, "tutortop", "private")}
}

// DefaultRoot is ~/Tutortop, the workspaces folder unless the learner picks another.
func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Tutortop")
}

func (s *Store) List() ([]Summary, error) {
	entries, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		w, err := s.Open(e.Name())
		if err != nil {
			continue // not a workspace
		}
		out = append(out, Summary{ID: w.ID, Title: w.Meta.Title, Icon: w.Meta.Icon, Provider: w.Meta.Provider, Progress: w.Progress(), Next: w.nextTask()})
	}
	return out, nil
}

// Create makes a new workspace. The caller checks the provider is known.
func (s *Store) Create(title, provider string) (*Workspace, error) {
	base := slugify(title)
	id := base
	for i := 2; ; i++ {
		// Private files are kept by id across workspaces folders, so a new
		// workspace mustn't take an id whose private files are still around.
		_, errDir := os.Stat(filepath.Join(s.Root, id))
		_, errPrivate := os.Stat(filepath.Join(s.PrivateRoot, id))
		if errors.Is(errDir, os.ErrNotExist) && errors.Is(errPrivate, os.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	w := s.workspace(id)
	if err := os.MkdirAll(filepath.Join(w.Dir, ".tutortop"), 0o755); err != nil {
		return nil, err
	}
	w.Meta = Meta{Title: title, Provider: provider, CreatedAt: time.Now()}
	if err := w.saveMeta(); err != nil {
		return nil, err
	}
	_ = os.WriteFile(filepath.Join(w.Dir, ".gitignore"), []byte(".tutortop/chat/\n"), 0o644)
	w.git("init", "-q")
	w.Commit("Create workspace")
	return w, nil
}

func (s *Store) Open(id string) (*Workspace, error) {
	if !validName(id) {
		return nil, fmt.Errorf("invalid workspace id %q", id)
	}
	w := s.workspace(id)
	var err error
	if w.Meta, err = readJSON[Meta](w.path(".tutortop", "workspace.json")); err != nil {
		return nil, err
	}
	return w, nil
}

// Delete removes a workspace for good: its folder, with the learner's code,
// notes and learning records, and its private task files.
func (s *Store) Delete(id string) error {
	w, err := s.Open(id)
	if err != nil {
		return fmt.Errorf("workspace %s not found", id)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.RemoveAll(w.Dir); err != nil {
		return err
	}
	return os.RemoveAll(w.PrivateDir)
}

func (s *Store) workspace(id string) *Workspace {
	mu, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return &Workspace{ID: id, Dir: filepath.Join(s.Root, id), PrivateDir: filepath.Join(s.PrivateRoot, id), mu: mu.(*sync.Mutex)}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "topic"
	}
	return s
}

type Summary struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Icon     string   `json:"icon"`
	Provider string   `json:"provider"`
	Progress Progress `json:"progress"`
	Next     string   `json:"next"` // title of the first unsolved task, "" if none
}

type Meta struct {
	Title string `json:"title"`
	// Icon names the workspace's picture, one of Icons; "" until one is picked.
	Icon string `json:"icon,omitempty"`
	// Provider is the agent the saved sessions belong to: "claude" or "codex".
	// The agent in use comes from the app settings.
	Provider string `json:"provider"`
	// Sessions maps a chat thread to the agent's session id, so each thread
	// resumes its own conversation.
	Sessions  map[string]string `json:"sessions,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

type Workspace struct {
	ID         string
	Dir        string
	PrivateDir string
	Meta       Meta

	// mu serialises changes that read-then-write (task numbering, plan
	// revisions) and git commits. It's shared by every Workspace value with
	// the same id, since Store.Open returns a fresh value each time.
	mu *sync.Mutex
}

// path is a file in the public workspace.
func (w *Workspace) path(parts ...string) string {
	return filepath.Join(append([]string{w.Dir}, parts...)...)
}

// private is a file outside the workspace that the learner and tutor don't see.
func (w *Workspace) private(parts ...string) string {
	return filepath.Join(append([]string{w.PrivateDir}, parts...)...)
}

// validName reports whether s can name a workspace, task or chat thread: a
// single path element that isn't "." or "..".
func validName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

func (w *Workspace) saveMeta() error {
	return writeJSON(w.path(".tutortop", "workspace.json"), w.Meta)
}

// updateMeta re-reads the metadata under the workspace lock, since several
// threads change it at once, and saves it if change reports a change.
func (w *Workspace) updateMeta(change func(m *Meta) bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	meta, err := readJSON[Meta](w.path(".tutortop", "workspace.json"))
	if err != nil {
		return err
	}
	changed := change(&meta)
	w.Meta = meta
	if !changed {
		return nil
	}
	return w.saveMeta()
}

// UseProvider switches the workspace to another agent. Sessions belong to one
// agent, so switching forgets them: threads start fresh conversations, and
// the tutor catches up from the workspace files, as it does for new tasks.
// It returns the session ids to resume from.
func (w *Workspace) UseProvider(provider string) (map[string]string, error) {
	var sessions map[string]string
	err := w.updateMeta(func(m *Meta) bool {
		switched := m.Provider != provider
		if switched {
			m.Provider, m.Sessions = provider, nil
		}
		sessions = maps.Clone(m.Sessions)
		return switched
	})
	return sessions, err
}

// SetSession records a thread's agent session id. An id from an agent the
// workspace has since switched away from is dropped.
func (w *Workspace) SetSession(provider, thread, sessionID string) error {
	return w.updateMeta(func(m *Meta) bool {
		if m.Provider != provider || m.Sessions[thread] == sessionID {
			return false
		}
		if m.Sessions == nil {
			m.Sessions = map[string]string{}
		}
		m.Sessions[thread] = sessionID
		return true
	})
}

// ReadDoc returns a top-level markdown file like MISSION.md, or "" if missing.
func (w *Workspace) ReadDoc(name string) string {
	b, _ := os.ReadFile(w.path(filepath.Base(name)))
	return string(b)
}

// SaveMission stores the learner's own edit of their mission. The curriculum
// isn't edited by hand: the tutor proposes changes to it, which the learner
// reviews. It reports whether the mission changed.
func (w *Workspace) SaveMission(mission string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mission = strings.TrimSpace(mission)
	if mission == strings.TrimSpace(w.ReadDoc("MISSION.md")) {
		return false, nil
	}
	if err := WriteFileAtomic(w.path("MISSION.md"), []byte(mission+"\n")); err != nil {
		return false, err
	}
	w.commit("Edit mission by hand")
	return true, nil
}

// Commit snapshots the workspace so every change to the plan has history.
func (w *Workspace) Commit(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.commit(msg)
}

// commit is Commit for callers that already hold w.mu.
func (w *Workspace) commit(msg string) {
	w.git("add", "-A")
	w.git("-c", "user.name=Tutortop", "-c", "user.email=tutortop@localhost", "commit", "-q", "-m", msg)
}

func (w *Workspace) git(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = w.Dir
	// The workspace is its own repo. Variables like GIT_DIR and GIT_INDEX_FILE,
	// which git sets for hooks, would point the commands at another one.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	_ = cmd.Run() // git is a convenience; the app works without it
}
