// Package workspace stores one learning topic as plain files on disk.
//
// Layout (public, in ~/Tutortop/<id>):
//
//	MISSION.md, NOTES.md, GLOSSARY.md, RESOURCES.md, learning-records/   written by the agent
//	curriculum.yaml                                                         the approved plan
//	tasks/0001-slug/{task.yaml,README.md,starter/,tests/,work/}             tasks; work/ is the learner's code
//	.tutortop/{workspace.json,attempts.jsonl,proposal.yaml,questions.json}  app state
//	.tutortop/toolchains/<language>.yaml                                    languages the tutor set up (beyond Go and Python)
//	.tutortop/chat/<thread>.jsonl                                           one chat per thread: "plan" or a task id
//
// Private files the learner and tutor shouldn't see (reference solutions,
// hidden tests, mutants) live outside the workspace in the app data dir.
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

// PlanThread is the planning conversation: interview, mission and
// curriculum. Every task also gets its own thread, named by its task id.
const PlanThread = "plan"

// WriterThread is the background conversation that writes one objective's
// lesson and exercise. The learner doesn't chat in it; it ends when it's done.
func WriterThread(objectiveID string) string { return "write-" + objectiveID }

// ObjectiveOfWriter returns the objective a writer thread writes for, if the
// thread is a writer.
func ObjectiveOfWriter(thread string) (string, bool) { return strings.CutPrefix(thread, "write-") }

// IsWriterThread reports whether a thread is an objective's writer.
func IsWriterThread(thread string) bool { _, ok := ObjectiveOfWriter(thread); return ok }

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

// ---- curriculum ----

// Curriculum returns the approved plan, or nil if there isn't one yet.
func (w *Workspace) Curriculum() (*Curriculum, error) {
	return readCurriculum(w.path("curriculum.yaml"))
}

// Proposal returns the plan waiting for the learner's approval, if any.
func (w *Workspace) Proposal() (*Curriculum, error) {
	return readCurriculum(w.path(".tutortop", "proposal.yaml"))
}

func readCurriculum(p string) (*Curriculum, error) {
	c, err := readYAML[Curriculum](p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	return &c, nil
}

// Propose validates a plan and stores it until the learner approves it.
func (w *Workspace) Propose(c Curriculum) error {
	if err := c.Validate(w.languageCheck); err != nil {
		return err
	}
	return writeYAML(w.path(".tutortop", "proposal.yaml"), c)
}

// ApproveProposal turns the pending proposal into the curriculum.
func (w *Workspace) ApproveProposal() (*Curriculum, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, err := w.Proposal()
	if err != nil || p == nil {
		return nil, errors.Join(err, errors.New("no proposal to approve"))
	}
	if cur, _ := w.Curriculum(); cur != nil {
		p.Revision = cur.Revision + 1
	} else {
		p.Revision = 1
	}
	if err := writeYAML(w.path("curriculum.yaml"), p); err != nil {
		return nil, err
	}
	_ = os.Remove(w.path(".tutortop", "proposal.yaml"))
	w.commit(fmt.Sprintf("Curriculum revision %d", p.Revision))
	return p, nil
}

func (w *Workspace) DiscardProposal() error {
	return removeIfExists(w.path(".tutortop", "proposal.yaml"))
}

// ---- questions ----

type Question struct {
	ID          string   `json:"id" jsonschema:"short key for the answer"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty" jsonschema:"2-4 suggested answers; the learner can always write their own"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

// Questions is a form from the tutor that waits for the learner's answers.
type Questions struct {
	Thread    string     `json:"thread,omitempty" jsonschema:"set by the app; leave out"`
	Intro     string     `json:"intro,omitempty" jsonschema:"one sentence shown above the questions"`
	Questions []Question `json:"questions" jsonschema:"2-4 focused questions"`
}

func (w *Workspace) SetQuestions(q *Questions) error {
	p := w.path(".tutortop", "questions.json")
	if q == nil {
		return removeIfExists(p)
	}
	return writeJSON(p, q)
}

// PendingQuestions returns the unanswered form, or nil.
func (w *Workspace) PendingQuestions() *Questions {
	q, err := readJSON[Questions](w.path(".tutortop", "questions.json"))
	if err != nil {
		return nil
	}
	return &q
}

// ---- git ----

func (w *Workspace) git(args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = w.Dir
	// The workspace is its own repo. Variables like GIT_DIR and GIT_INDEX_FILE,
	// which git sets for hooks, would point the commands at another one.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	_ = cmd.Run() // git is a convenience; the app works without it
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

// ---- chat transcript ----

// Chat item roles.
const (
	RoleUser      = "user"      // the learner's words
	RoleAssistant = "assistant" // the tutor's reply
	RoleTool      = "tool"      // a tutor action worth showing, e.g. "Updated your mission"
	RoleNotice    = "notice"    // an app event, e.g. "Stopped" or "You passed …"
)

// ChatItem is one entry in the conversation shown in the UI.
type ChatItem struct {
	ID     string    `json:"id"`
	Thread string    `json:"thread"`
	Role   string    `json:"role"` // user | assistant | tool | notice
	Text   string    `json:"text"`
	Status string    `json:"status,omitempty"` // for tools: running | done | failed
	At     time.Time `json:"at"`
}

func (w *Workspace) AppendChat(item ChatItem) error {
	if !validName(item.Thread) {
		return fmt.Errorf("invalid chat thread %q", item.Thread)
	}
	// A tutor still winding down after its workspace was deleted mustn't
	// bring the folder back.
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := os.Stat(w.path(".tutortop", "workspace.json")); err != nil {
		return err
	}
	if err := os.MkdirAll(w.path(".tutortop", "chat"), 0o755); err != nil {
		return err
	}
	return appendJSONL(w.path(".tutortop", "chat", item.Thread+".jsonl"), item)
}

// Chat returns one thread's transcript.
func (w *Workspace) Chat(thread string) ([]ChatItem, error) {
	if !validName(thread) {
		return nil, fmt.Errorf("invalid chat thread %q", thread)
	}
	return readJSONL[ChatItem](w.path(".tutortop", "chat", thread+".jsonl"))
}
