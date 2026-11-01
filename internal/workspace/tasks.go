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

	"github.com/LukasKH/tutortop/internal/runner"
)

// TaskSpec is what the authoring agent submits through the create_task tool.
type TaskSpec struct {
	ObjectiveID string          `json:"objectiveId" jsonschema:"id of the curriculum objective this task proves"`
	Title       string          `json:"title"`
	Size        string          `json:"size,omitempty" jsonschema:"small, medium or large; defaults to the curriculum's task_defaults.size"`
	Statement   Statement       `json:"statement" jsonschema:"the problem statement, in parts; the app renders it. It stands on its own: no feedback on earlier tasks or chat remarks."`
	Starter     runner.Files    `json:"starter" jsonschema:"filename -> content. The files the learner edits. Must compile and fail some tests (e.g. return zero values)."`
	Reference   runner.Files    `json:"reference" jsonschema:"filename -> content. A correct solution with the same filenames as starter. Never shown to the learner."`
	Tests       runner.Files    `json:"tests" jsonschema:"filename -> content. Visible test files the learner can read (Go: *_test.go, Python: test_*.py, other languages: the toolchain's test_files pattern)."`
	HiddenTests runner.Files    `json:"hiddenTests" jsonschema:"filename -> content. Hidden test files with edge cases. Learner sees only names and failure messages."`
	Mutants     []runner.Mutant `json:"mutants" jsonschema:"at least 2 plausible wrong solutions (same filenames as reference). Each must compile and be caught by the tests."`
}

// TaskInfo is task.yaml.
type TaskInfo struct {
	ID        string    `yaml:"id" json:"id"`
	Title     string    `yaml:"title" json:"title"`
	Objective string    `yaml:"objective" json:"objective"`
	Language  string    `yaml:"language" json:"language"`
	Size      string    `yaml:"size" json:"size"`
	Files     []string  `yaml:"files" json:"files"` // learner-editable files
	CreatedAt time.Time `yaml:"created_at" json:"createdAt"`
}

// TaskDetail is everything the task screen needs.
type TaskDetail struct {
	TaskInfo
	Statement   string       `json:"statement"`
	Tests       runner.Files `json:"tests"`
	Work        runner.Files `json:"work"`
	HiddenCount int          `json:"hiddenCount"`
	Passed      bool         `json:"passed"`
	HintLevel   int          `json:"hintLevel"` // highest hint level used so far, 0 if none
	// EditorLanguage is the Monaco language id for the task's files.
	EditorLanguage    string `json:"editorLanguage"`
	HasLanguageServer bool   `json:"hasLanguageServer"`
}

// ValidateSpec checks the spec's shape against the curriculum and returns the
// task's toolchain.
func (w *Workspace) ValidateSpec(spec *TaskSpec) (runner.Toolchain, error) {
	var tc runner.Toolchain
	cur, err := w.Curriculum()
	if err != nil {
		return tc, err
	}
	if cur == nil {
		return tc, errors.New("there is no approved curriculum yet; propose one and wait for the learner to approve it")
	}
	_, lang, ok := cur.Objective(spec.ObjectiveID)
	if !ok {
		return tc, fmt.Errorf("objective %q isn't in the curriculum", spec.ObjectiveID)
	}
	if spec.Size == "" {
		spec.Size = cur.TaskDefaults.Size
	}
	if tc, err = w.Toolchain(lang); err != nil {
		return tc, err
	}

	var p problems
	if strings.TrimSpace(spec.Title) == "" {
		p.addf("title is required")
	}
	spec.Statement.check(&p)
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
	for _, set := range []runner.Files{spec.Tests, spec.HiddenTests} {
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
	for _, m := range spec.Mutants {
		for name := range m.Files {
			if _, ok := spec.Starter[name]; !ok {
				p.addf("mutant %q changes %s, which isn't a solution file", m.Name, name)
			}
		}
	}
	if err := p.err("task"); err != nil {
		return tc, err
	}
	return tc, nil
}

// SaveTask writes a task that already passed the quality gate.
func (w *Workspace) SaveTask(spec TaskSpec, lang string) (TaskInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	tasks, err := w.Tasks()
	if err != nil {
		return TaskInfo{}, err
	}
	info := TaskInfo{
		ID:        fmt.Sprintf("%04d-%s", len(tasks)+1, slugify(spec.Title)),
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
		files runner.Files
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
	for _, m := range spec.Mutants {
		writes = append(writes, write{filepath.Join(priv, "mutants", slugify(m.Name)), m.Files})
	}
	for _, wr := range writes {
		if err := writeFiles(wr.dir, wr.files); err != nil {
			return TaskInfo{}, err
		}
	}
	if err := writeFileAtomic(filepath.Join(pub, "README.md"), []byte(spec.Statement.Markdown(spec.Title, lang))); err != nil {
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
	hidden, _ := readFiles(w.private("tasks", id, "hidden"))
	for _, src := range hidden {
		d.HiddenCount += tc.CountTests(src)
	}
	h := w.history()
	d.Passed, d.HintLevel = h.passed[id], h.hints[id].Highest
	return d, nil
}

// Work returns the learner's current code.
func (w *Workspace) Work(id string) (runner.Files, error) {
	return readFiles(w.path("tasks", id, "work"))
}

// SaveWork stores the learner's code. Only the task's editable files are accepted.
func (w *Workspace) SaveWork(id string, files runner.Files) error {
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
	starter, err := readFiles(w.path("tasks", id, "starter"))
	if err != nil {
		return err
	}
	return writeFiles(w.path("tasks", id, "work"), starter)
}

// Job builds a check run for the learner's current code. A submission also
// runs the hidden tests.
func (w *Workspace) Job(id string, submit bool) (runner.Job, error) {
	info, err := w.taskInfo(id)
	if err != nil {
		return runner.Job{}, err
	}
	job := runner.Job{}
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

func writeFiles(dir string, files runner.Files) error {
	for name, content := range files {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("file path %q must stay inside the task folder", name)
		}
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := writeFileAtomic(p, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func readFiles(dir string) (runner.Files, error) {
	files := runner.Files{}
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
