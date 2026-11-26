package workspace

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LukasKH/tutortop/internal/atomicfile"
	"github.com/LukasKH/tutortop/internal/sandbox"
)

// TaskSpec is what the authoring agent submits through the create_task tool.
type TaskSpec struct {
	ObjectiveID string        `json:"objectiveId" jsonschema:"id of the curriculum objective this task proves"`
	Title       string        `json:"title"`
	Size        TaskSize      `json:"size,omitempty" jsonschema:"small, medium or large; defaults to the curriculum's task_defaults.size"`
	Statement   Statement     `json:"statement" jsonschema:"the problem statement, in parts; the app renders it. It stands on its own: no feedback on earlier tasks or chat remarks."`
	Starter     sandbox.Files `json:"starter" jsonschema:"filename -> content. The files the learner edits. Must compile and fail some tests (e.g. return zero values)."`
	Reference   sandbox.Files `json:"reference" jsonschema:"filename -> content. A correct solution with the same filenames as starter. Never shown to the learner."`
	Tests       sandbox.Files `json:"tests" jsonschema:"filename -> content. Visible test files the learner can read (Go: *_test.go, Python: test_*.py, other languages: the toolchain's test_files glob). Every test, visible or hidden, needs its own name."`
	HiddenTests sandbox.Files `json:"hiddenTests" jsonschema:"filename -> content. Hidden test files with edge cases. Learner sees only names and failure messages."`
}

// check reports every problem with the spec's shape at once, for a task in
// lang run by tc.
func (spec *TaskSpec) check(tc sandbox.Toolchain, lang string) error {
	var p problems
	if strings.TrimSpace(spec.Title) == "" {
		p.addf("title is required")
	}
	if !spec.Size.valid() {
		p.addf("size must be small, medium or large")
	}
	spec.Statement.check(&p)
	spec.checkStarter(&p, tc)
	spec.checkTests(&p, tc, lang)
	return p.err("task")
}

// checkStarter checks the starter has files, the reference has the same, and
// none is a test.
func (spec *TaskSpec) checkStarter(p *problems, tc sandbox.Toolchain) {
	if len(spec.Starter) == 0 {
		p.addf("starter needs at least one file")
	}
	for name := range spec.Starter {
		if _, ok := spec.Reference[name]; !ok {
			p.addf("reference is missing %s (it must have the same files as starter)", name)
		}
		if tc.IsTestFile(name) {
			p.addf("starter file %s looks like a test file", name)
		}
	}
}

// checkTests checks every test file is named like one and belongs to one set
// only.
func (spec *TaskSpec) checkTests(p *problems, tc sandbox.Toolchain, lang string) {
	for _, set := range []sandbox.Files{spec.Tests, spec.HiddenTests} {
		for name := range set {
			if !tc.IsTestFile(name) {
				p.addf("%s isn't named like a %s test file", name, lang)
			}
			if _, clash := spec.Starter[name]; clash {
				p.addf("%s is both a test and a starter file", name)
			}
		}
	}
	for name := range spec.HiddenTests {
		if _, clash := spec.Tests[name]; clash {
			p.addf("%s is both a visible and a hidden test file", name)
		}
	}
}

// TaskInfo is task.yaml.
type TaskInfo struct {
	ID        string    `yaml:"id" json:"id"`
	Title     string    `yaml:"title" json:"title"`
	Objective string    `yaml:"objective" json:"objective"`
	Language  string    `yaml:"language" json:"language"`
	Size      TaskSize  `yaml:"size" json:"size"`
	Files     []string  `yaml:"files" json:"files"` // learner-editable files
	CreatedAt time.Time `yaml:"created_at" json:"createdAt"`
}

// TaskDetail is everything the task screen needs.
type TaskDetail struct {
	TaskInfo

	Statement   string        `json:"statement"`
	Tests       sandbox.Files `json:"tests"`
	Work        sandbox.Files `json:"work"`
	HiddenCount int           `json:"hiddenCount"`
	Passed      bool          `json:"passed"`
	HintLevel   int           `json:"hintLevel"` // highest hint level used so far, 0 if none
	// EditorLanguage is the Monaco language id for the task's files.
	EditorLanguage    string `json:"editorLanguage"`
	HasLanguageServer bool   `json:"hasLanguageServer"`
}

// ValidateSpec checks the spec's shape against the curriculum. It returns
// the spec with the plan's defaults filled in, and the task's toolchain.
func (w *Workspace) ValidateSpec(spec TaskSpec) (TaskSpec, sandbox.Toolchain, error) {
	cur, err := w.Curriculum()
	if err != nil {
		return spec, sandbox.Toolchain{}, err
	}
	if cur == nil {
		return spec, sandbox.Toolchain{}, errors.New("there is no approved curriculum yet; propose one and wait for the learner to approve it")
	}
	_, lang, ok := cur.Objective(spec.ObjectiveID)
	if !ok {
		return spec, sandbox.Toolchain{}, fmt.Errorf("objective %q isn't in the curriculum", spec.ObjectiveID)
	}
	if spec.Size == "" {
		spec.Size = cur.TaskDefaults.Size
	}
	tc, err := w.Toolchain(lang)
	if err != nil {
		return spec, tc, err
	}
	return spec, tc, spec.check(tc, lang)
}

// SaveTask writes a task that already passed the quality gate, with the
// names of its tests as the gate saw them.
func (w *Workspace) SaveTask(spec TaskSpec, lang string, names sandbox.TestNames) (TaskInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	tasks, err := w.Tasks()
	if err != nil {
		return TaskInfo{}, err
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	info := TaskInfo{
		ID:        fmt.Sprintf("%04d-%s", nextNumber(ids), slugify(spec.Title)),
		Title:     spec.Title,
		Objective: spec.ObjectiveID,
		Language:  lang,
		Size:      spec.Size,
		Files:     slices.Sorted(maps.Keys(spec.Starter)),
		CreatedAt: time.Now(),
	}
	pub := w.path("tasks", info.ID)
	priv := w.private("tasks", info.ID)
	type write struct {
		dir   string
		files sandbox.Files
	}
	writes := []write{
		{filepath.Join(pub, "starter"), spec.Starter},
		{filepath.Join(pub, "work"), spec.Starter},
		{filepath.Join(pub, "tests"), spec.Tests},
		// Grading reads this copy, so editing the public tests can't change results.
		{filepath.Join(priv, "tests"), spec.Tests},
		{filepath.Join(priv, "reference"), spec.Reference},
		{filepath.Join(priv, "hidden"), spec.HiddenTests},
	}
	for _, wr := range writes {
		if err := writeFiles(wr.dir, wr.files); err != nil {
			return TaskInfo{}, err
		}
	}
	if err := atomicfile.Write(filepath.Join(pub, "README.md"), []byte(spec.Statement.Markdown(spec.Title, lang))); err != nil {
		return TaskInfo{}, err
	}
	if err := os.MkdirAll(priv, 0o755); err != nil {
		return TaskInfo{}, err
	}
	if err := writeYAML(filepath.Join(priv, "test_names.yaml"), names); err != nil {
		return TaskInfo{}, err
	}
	if err := writeYAML(filepath.Join(pub, "task.yaml"), info); err != nil {
		return TaskInfo{}, err
	}
	w.commit("Add task " + info.ID)
	return info, nil
}

// Tasks lists the tasks in order: names start with the task number.
func (w *Workspace) Tasks() ([]TaskInfo, error) {
	return readEach[TaskInfo](w.path("tasks"), "task.yaml")
}

func (w *Workspace) Task(id string) (TaskDetail, error) {
	info, err := w.taskInfo(id)
	if err != nil {
		return TaskDetail{}, err
	}
	d := TaskDetail{TaskInfo: info}
	b, _ := os.ReadFile(w.path("tasks", id, "README.md"))
	d.Statement = string(b)
	if d.Tests, err = readFiles(w.path("tasks", id, "tests")); err != nil {
		return d, err
	}
	if d.Work, err = w.Work(id); err != nil {
		return d, err
	}
	tc, err := w.Toolchain(info.Language)
	if err != nil {
		return d, err
	}
	d.EditorLanguage = tc.EditorLanguage
	d.HasLanguageServer = len(tc.LanguageServer) > 0
	d.HiddenCount = len(w.testNames(id).Hidden)
	h := w.history()
	d.Passed, d.HintLevel = h.passed[id], h.hints[id].Highest
	return d, nil
}

// Work returns the learner's current code.
func (w *Workspace) Work(id string) (sandbox.Files, error) {
	return readFiles(w.path("tasks", id, "work"))
}

// SaveWork stores the learner's code. Only the task's editable files are accepted.
func (w *Workspace) SaveWork(id string, files sandbox.Files) error {
	info, err := w.taskInfo(id)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, f := range info.Files {
		allowed[f] = true
	}
	for name := range files {
		if !allowed[name] {
			return fmt.Errorf("%s isn't an editable file of task %s", name, id)
		}
	}
	return writeFiles(w.path("tasks", id, "work"), files)
}

// ResetWork restores the starter code.
func (w *Workspace) ResetWork(id string) error {
	if _, err := w.taskInfo(id); err != nil {
		return err
	}
	starter, err := readFiles(w.path("tasks", id, "starter"))
	if err != nil {
		return err
	}
	return writeFiles(w.path("tasks", id, "work"), starter)
}

// Job builds a check run for the learner's current code. A submission also
// runs the hidden tests.
func (w *Workspace) Job(id string, submit bool) (sandbox.Job, error) {
	info, err := w.taskInfo(id)
	if err != nil {
		return sandbox.Job{}, err
	}
	// The workspace's runs share their toolchain's cache, so a run doesn't
	// recompile what the last one did.
	job := sandbox.Job{Names: w.testNames(id), Cache: w.ID}
	if job.Toolchain, err = w.Toolchain(info.Language); err != nil {
		return job, err
	}
	if job.Solution, err = w.Work(id); err != nil {
		return job, err
	}
	// The private copy, so edits to the public tests/ folder can't change results.
	if job.Tests, err = readFiles(w.private("tasks", id, "tests")); err != nil {
		return job, err
	}
	if submit {
		if job.HiddenTests, err = readFiles(w.private("tasks", id, "hidden")); err != nil {
			return job, err
		}
	}
	return job, nil
}

func (w *Workspace) taskInfo(id string) (TaskInfo, error) {
	if !validName(id) {
		return TaskInfo{}, fmt.Errorf("invalid task id %q", id)
	}
	info, err := readYAML[TaskInfo](w.path("tasks", id, "task.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return info, fmt.Errorf("task %s not found", id)
	}
	return info, err
}

// testNames are a task's tests by name, kept private since hidden test names
// can give away edge cases. A missing file gives none.
func (w *Workspace) testNames(id string) sandbox.TestNames {
	names, _ := readYAML[sandbox.TestNames](w.private("tasks", id, "test_names.yaml"))
	return names
}

func writeFiles(dir string, files sandbox.Files) error {
	for name, content := range files {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("file path %q must stay inside the task folder", name)
		}
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := atomicfile.Write(p, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func readFiles(dir string) (sandbox.Files, error) {
	files := sandbox.Files{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if errors.Is(err, os.ErrNotExist) {
		return files, nil
	}
	return files, err
}
