package workspace

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Curriculum is the plan: an ordered list of modules, each with objectives.
// It holds no progress; that is derived from attempts, so the agent can't fake it.
type Curriculum struct {
	Revision     int          `yaml:"revision" json:"revision,omitempty" jsonschema:"set by the app; leave out"`
	Language     string       `yaml:"language" json:"language" jsonschema:"default language for tasks: go, python, or one you set up with set_toolchain"`
	TaskDefaults TaskDefaults `yaml:"task_defaults" json:"taskDefaults,omitempty"`
	Modules      []Module     `yaml:"modules" json:"modules"`
}

type TaskDefaults struct {
	Size  string `yaml:"size" json:"size,omitempty" jsonschema:"small (~10 min), medium (~25 min) or large (~60 min)"`
	Shape string `yaml:"shape" json:"shape,omitempty" jsonschema:"kata (standalone function exercises) or project (grow one program over many tasks)"`
}

type Module struct {
	ID    string `yaml:"id" json:"id" jsonschema:"short kebab-case id, e.g. basics"`
	Title string `yaml:"title" json:"title"`
	// References are where the module's content comes from, like the
	// sources a school course lists, so the learner can read the originals.
	References []Source    `yaml:"references,omitempty" json:"references,omitempty" jsonschema:"1-4 official documentation pages, specifications or primary sources this module is based on, each a page you have read; the app checks every link opens. Title them so the learner knows what they'll find, e.g. 'Go blog: Working with Errors in Go 1.13'"`
	Objectives []Objective `yaml:"objectives" json:"objectives"`
}

type Source struct {
	Title string `json:"title" yaml:"title"`
	URL   string `json:"url" yaml:"url"`
}

// check reports what's wrong with a source, or "".
func (s Source) check() string {
	if strings.TrimSpace(s.Title) == "" {
		return "needs a title"
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "needs a full http(s) url"
	}
	return ""
}

type Objective struct {
	ID string `yaml:"id" json:"id" jsonschema:"dotted id, e.g. go.errors.wrap"`
	// Name is the objective in the curriculum menu; Title is the full goal.
	Name     string `yaml:"name,omitempty" json:"name,omitempty" jsonschema:"2-4 words shown in the curriculum menu, like a chapter heading: 'Wrapping errors'"`
	Title    string `yaml:"title" json:"title" jsonschema:"the full goal tests can prove: 'Return wrapped errors that callers can inspect with errors.Is'"`
	Verify   string `yaml:"verify,omitempty" json:"verify,omitempty" jsonschema:"how tasks for this objective are checked; only 'tests' is supported for now"`
	Language string `yaml:"language,omitempty" json:"language,omitempty" jsonschema:"overrides the curriculum language"`
}

// maxNameLen keeps objective names short enough for one line in the menu.
const maxNameLen = 28

var (
	moduleID    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	objectiveID = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*$`)
)

// Validate fills defaults and reports every problem at once, so the agent can
// fix them in one go. language reports whether a language is usable. Plans
// approved before references existed lack them; they're only checked here,
// when the tutor proposes a plan.
func (c *Curriculum) Validate(language func(string) error) error {
	var p problems

	if err := language(c.Language); err != nil {
		p.addf("language: %v", err)
	}
	if c.TaskDefaults.Size == "" {
		c.TaskDefaults.Size = "medium"
	}
	if c.TaskDefaults.Shape == "" {
		c.TaskDefaults.Shape = "kata"
	}
	switch c.TaskDefaults.Size {
	case "small", "medium", "large":
	default:
		p.addf("task_defaults.size must be small, medium or large")
	}
	switch c.TaskDefaults.Shape {
	case "kata", "project":
	default:
		p.addf("task_defaults.shape must be kata or project")
	}
	if len(c.Modules) == 0 {
		p.addf("the curriculum needs at least one module")
	}

	seen := map[string]bool{}
	for mi := range c.Modules {
		m := &c.Modules[mi]
		if !moduleID.MatchString(m.ID) {
			p.addf("module id %q must be kebab-case", m.ID)
		}
		if seen[m.ID] {
			p.addf("duplicate id %q", m.ID)
		}
		seen[m.ID] = true
		if len(m.Objectives) == 0 {
			p.addf("module %q has no objectives", m.ID)
		}
		if len(m.References) == 0 {
			p.addf("module %q needs references: 1-4 official documentation pages or primary sources it's based on", m.ID)
		}
		if len(m.References) > 4 {
			p.addf("module %q: keep it to 4 references at most, the most useful ones", m.ID)
		}
		for _, r := range m.References {
			if err := r.check(); err != "" {
				p.addf("module %q: reference %q: %s", m.ID, r.Title, err)
			}
		}
		for oi := range m.Objectives {
			o := &m.Objectives[oi]
			if !objectiveID.MatchString(o.ID) {
				p.addf("objective id %q must be lowercase, dotted or dashed", o.ID)
			}
			if seen[o.ID] {
				p.addf("duplicate id %q", o.ID)
			}
			seen[o.ID] = true
			switch n := len([]rune(o.Name)); {
			case n == 0:
				p.addf("objective %q needs a name: 2-4 words for the menu, like 'Wrapping errors'", o.ID)
			case n > maxNameLen:
				p.addf("objective %q: name %q is too long; keep it to 2-4 words (%d characters at most)", o.ID, o.Name, maxNameLen)
			}
			if o.Verify == "" {
				o.Verify = "tests"
			}
			if o.Verify != "tests" {
				p.addf("objective %q: verify must be 'tests' (other check kinds aren't supported yet). Reshape the objective so unit tests can prove it.", o.ID)
			}
			if o.Language != "" {
				if err := language(o.Language); err != nil {
					p.addf("objective %q: %v", o.ID, err)
				}
			}
		}
	}
	return p.err("curriculum")
}

// URLs lists every reference link in the plan.
func (c *Curriculum) URLs() []string {
	var out []string
	for _, m := range c.Modules {
		for _, r := range m.References {
			out = append(out, r.URL)
		}
	}
	return out
}

// Objective looks up an objective and its effective language.
func (c *Curriculum) Objective(id string) (Objective, string, bool) {
	for _, m := range c.Modules {
		for _, o := range m.Objectives {
			if o.ID == id {
				lang := o.Language
				if lang == "" {
					lang = c.Language
				}
				return o, lang, true
			}
		}
	}
	return Objective{}, "", false
}

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
