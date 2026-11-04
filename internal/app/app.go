// Package app ties workspaces, the check runner, the MCP tools and the agent
// together, and exposes them to the frontend as a Wails service.
package app

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/agent"
	"github.com/LukasKH/tutortop/internal/runner"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/adrg/xdg"
)

// The tutor's instructions: shared rules plus one role per kind of thread.
var (
	//go:embed prompts/tutor.md
	tutorPrompt string
	//go:embed prompts/plan.md
	planRole string
	//go:embed prompts/task.md
	taskRole string
	//go:embed prompts/lesson.md
	lessonRole string
	//go:embed prompts/writer.md
	writerRole string
)

type App struct {
	store  *workspace.Store
	runner *runner.Runner
	mcp    *mcpHost

	// startAgent is agent.Start; tests replace it.
	startAgent func(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)
	// linkCheck is checkLinks; tests replace it so they don't need the network.
	linkCheck func(ctx context.Context, urls []string) []string

	mu          sync.Mutex
	sessions    map[threadKey]*agentSession // live tutor per chat thread
	lastResults map[threadKey]runner.Result // latest check per task, for tutor context
	checks      map[threadKey]Gate          // a writer's latest sandbox check, by objective, while it works
	servers     map[string]*languageServer  // running language servers by id

	settingsMu   sync.Mutex
	settingsPath string
	models       map[string]modelList // provider -> recently listed models
}

func New() (*App, error) {
	a := &App{runner: runner.New(), sessions: map[threadKey]*agentSession{}, lastResults: map[threadKey]runner.Result{}, checks: map[threadKey]Gate{}, servers: map[string]*languageServer{}, startAgent: agent.Start, linkCheck: checkLinks,
		settingsPath: filepath.Join(xdg.ConfigHome, "tutortop", "settings.yaml"), models: map[string]modelList{}}
	// The workspaces folder: TUTORTOP_HOME, else the one chosen in the
	// settings, else ~/Tutortop. It's fixed for the run: live sessions open
	// their workspace by id.
	a.store = workspace.NewStore(cmp.Or(os.Getenv("TUTORTOP_HOME"), a.loadSettings().WorkspacesFolder, workspace.DefaultRoot()))
	h, err := startMCP(a)
	if err != nil {
		return nil, err
	}
	a.mcp = h
	return a, nil
}

// ServiceShutdown is called by Wails on exit and stops all agent processes
// and language servers.
func (a *App) ServiceShutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.sessions {
		s.close()
	}
	for _, ls := range a.servers {
		ls.srv.Close()
	}
	return nil
}

// ---- environment ----

type ProviderStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

type Environment struct {
	Providers   []ProviderStatus `json:"providers"`
	SandboxOK   bool             `json:"sandboxOk"`
	SandboxHint string           `json:"sandboxHint,omitempty"`
	// Root is the workspaces folder in use. A folder chosen in the settings
	// is used from the next start; TUTORTOP_HOME overrides it (RootFromEnv).
	Root        string `json:"root"`
	DefaultRoot string `json:"defaultRoot"`
	RootFromEnv bool   `json:"rootFromEnv"`
}

func (a *App) Environment() Environment {
	env := Environment{Root: a.store.Root, DefaultRoot: workspace.DefaultRoot(), RootFromEnv: os.Getenv("TUTORTOP_HOME") != ""}
	for _, p := range agent.Providers() {
		path, err := exec.LookPath(p.Binary)
		env.Providers = append(env.Providers, ProviderStatus{ID: p.ID, Name: p.Name, Installed: err == nil, Path: path})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.runner.Check(ctx); err != nil {
		env.SandboxHint = err.Error()
	} else {
		env.SandboxOK = true
	}
	return env
}

// ---- workspaces ----

func (a *App) ListWorkspaces() ([]workspace.Summary, error) { return a.store.List() }

// Activity is what the learner finished each day across all workspaces, with
// their streak, for the activity panel on Home.
func (a *App) Activity() (workspace.Activity, error) {
	list, err := a.store.List()
	if err != nil {
		return workspace.Activity{}, err
	}
	var all []workspace.Completion
	for _, s := range list {
		w, err := a.store.Open(s.ID)
		if err != nil {
			continue
		}
		cs, _ := w.Completions()
		all = append(all, cs...)
	}
	return workspace.Summarize(all, time.Now()), nil
}

// CreateWorkspace starts a workspace tutored by the agent in the settings.
// The icon is "" (the tutor picks one during the interview) or one of
// workspace.Icons, as when the topic comes from an idea.
func (a *App) CreateWorkspace(title, icon string) (string, error) {
	provider := a.Settings().Agent
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("give the topic a name")
	}
	if icon != "" && !slices.Contains(workspace.Icons, icon) {
		return "", fmt.Errorf("unknown icon %q", icon)
	}
	existing, _ := a.store.List()
	for _, e := range existing {
		if strings.EqualFold(e.Title, title) {
			return "", fmt.Errorf("you already have a workspace called “%s”; pick another name", e.Title)
		}
	}
	w, err := a.store.Create(title, provider)
	if err != nil {
		return "", err
	}
	if err := w.SetIcon(icon); err != nil {
		return "", err
	}
	// The tutor starts the interview right away.
	go a.send(w, workspace.PlanThread, message{
		Role:  workspace.RoleNotice,
		Shown: "Workspace created. Your tutor is getting started.",
		Agent: fmt.Sprintf("[app] I want to learn: %s. This is a new workspace; start the interview.", title),
	})
	return w.ID, nil
}

// PrepareObjective has a writer write an objective's lesson and task now,
// because the learner opened it ahead of the suggested order. It returns
// once the writer has started; the lesson and task arrive as events.
func (a *App) PrepareObjective(wsID, objectiveID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	c, err := w.Curriculum()
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("there's no plan yet")
	}
	o, _, ok := c.Objective(objectiveID)
	if !ok {
		return fmt.Errorf("%q isn't in the plan", objectiveID)
	}
	return a.writeObjective(w, o, "The learner opened it and is waiting for it, so be quick. Write the lesson first, as they read it first.")
}

// DeleteWorkspace stops the workspace's tutors and language servers, then
// deletes its files for good.
func (a *App) DeleteWorkspace(id string) error {
	a.mu.Lock()
	var servers []*languageServer
	for key, s := range a.sessions {
		if key.ws == id {
			delete(a.sessions, key)
			s.close()
		}
	}
	for key := range a.lastResults {
		if key.ws == id {
			delete(a.lastResults, key)
		}
	}
	for sid, ls := range a.servers {
		if ls.key.ws == id {
			delete(a.servers, sid)
			servers = append(servers, ls)
		}
	}
	a.mu.Unlock()
	for _, ls := range servers {
		ls.srv.Close()
	}
	a.mcp.forget(id)
	return a.store.Delete(id)
}

// WorkspaceState is everything the workspace screen shows.
type WorkspaceState struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	Icon       string                   `json:"icon"`
	Provider   string                   `json:"provider"`
	Mission    string                   `json:"mission"`
	Curriculum *workspace.Curriculum    `json:"curriculum"`
	Proposal   *workspace.Curriculum    `json:"proposal"`
	Progress   workspace.Progress       `json:"progress"`
	Tasks      []workspace.TaskStatus   `json:"tasks"`
	Lessons    []workspace.LessonStatus `json:"lessons"`
	Questions  *workspace.Questions     `json:"questions"`
	Busy       map[string]bool          `json:"busy"` // thread -> its tutor or writer is working
	// Checks are writers' latest sandbox checks of new tasks, by objective.
	Checks map[string]Gate `json:"checks"`
}

func (a *App) Workspace(id string) (WorkspaceState, error) {
	w, err := a.store.Open(id)
	if err != nil {
		return WorkspaceState{}, err
	}
	st := WorkspaceState{ID: w.ID, Title: w.Meta.Title, Icon: w.Meta.Icon, Provider: w.Meta.Provider, Mission: w.ReadDoc("MISSION.md"), Progress: w.Progress()}
	if st.Curriculum, err = w.Curriculum(); err != nil {
		return st, err
	}
	st.Proposal, _ = w.Proposal()
	st.Tasks, _ = w.TaskStatuses()
	st.Lessons, _ = w.LessonStatuses()
	st.Questions = w.PendingQuestions()
	st.Busy = map[string]bool{}
	st.Checks = map[string]Gate{}
	a.mu.Lock()
	for key, s := range a.sessions {
		if key.ws == id && s.busy {
			st.Busy[key.thread] = true
		}
	}
	for key, c := range a.checks {
		if key.ws == id {
			st.Checks[key.thread] = c
		}
	}
	a.mu.Unlock()

	if st.Curriculum != nil {
		a.prepare(w, st.Curriculum.Language)
	}
	return st, nil
}

func (a *App) ApproveProposal(wsID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	c, err := w.ApproveProposal()
	if err != nil {
		return err
	}
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	a.prepare(w, c.Language)
	// The tutor panel hides the plan thread up to the first of these notices
	// (afterSetup in frontend/src/lib/chat.ts); keep the wording's start.
	a.post(w, workspace.ChatItem{Thread: workspace.PlanThread, Role: workspace.RoleNotice, Text: fmt.Sprintf("You approved the plan (revision %d).", c.Revision)})
	a.keepReady(w, "The learner just approved the plan.")
	return nil
}

// prepare builds a language's sandbox image in the background, so the first
// check is fast.
func (a *App) prepare(w *workspace.Workspace, language string) {
	if tc, err := w.Toolchain(language); err == nil {
		go a.runner.Prepare(context.Background(), tc)
	}
}

// SaveMission saves the learner's own edit of their mission, then asks the
// planning tutor to propose curriculum changes if the plan no longer fits it.
// The curriculum itself only changes through a proposal the learner approves.
func (a *App) SaveMission(wsID, mission string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	changed, err := w.SaveMission(mission)
	if err != nil || !changed {
		return err
	}
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	go a.send(w, workspace.PlanThread, message{
		Role:  workspace.RoleNotice,
		Shown: "You edited your mission.",
		Agent: "[app] I edited my mission by hand. Read it; it's mine now, so don't change it back. " +
			"If the curriculum no longer fits it, propose a revision that does, keeping the ids of objectives that stay. Otherwise reply in one sentence.",
	})
	return nil
}

func (a *App) RejectProposal(wsID, feedback string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	if err := w.DiscardProposal(); err != nil {
		return err
	}
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	text := "I'd like changes to the proposed curriculum: " + feedback
	go a.send(w, workspace.PlanThread, message{Role: workspace.RoleUser, Shown: text, Agent: text})
	return nil
}

// ---- lessons ----

func (a *App) Lesson(wsID, lessonID string) (workspace.LessonDetail, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.LessonDetail{}, err
	}
	return w.Lesson(lessonID)
}

// AnswerQuiz checks one quiz answer and returns the feedback. Only the first
// answer to a question is recorded.
func (a *App) AnswerQuiz(wsID, lessonID string, question, choice int) (workspace.QuizResult, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.QuizResult{}, err
	}
	r, err := w.AnswerQuiz(lessonID, question, choice)
	if err == nil {
		emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	}
	return r, err
}

// ---- tasks ----

func (a *App) Task(wsID, taskID string) (workspace.TaskDetail, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return workspace.TaskDetail{}, err
	}
	return w.Task(taskID)
}

func (a *App) SaveWork(wsID, taskID string, files runner.Files) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.SaveWork(taskID, files)
}

func (a *App) ResetWork(wsID, taskID string) error {
	w, err := a.store.Open(wsID)
	if err != nil {
		return err
	}
	return w.ResetWork(taskID)
}

// RunChecks runs the visible tests, or all tests when submit is true.
// A passing submission tells the planning tutor, and writers prepare what
// comes next.
func (a *App) RunChecks(wsID, taskID string, files runner.Files, submit bool) (runner.Result, error) {
	w, err := a.store.Open(wsID)
	if err != nil {
		return runner.Result{}, err
	}
	if err := w.SaveWork(taskID, files); err != nil {
		return runner.Result{}, err
	}
	job, err := w.Job(taskID, submit)
	if err != nil {
		return runner.Result{}, err
	}
	res, err := a.runner.Run(context.Background(), job)
	if err != nil {
		return res, err
	}
	a.mu.Lock()
	a.lastResults[threadKey{wsID, taskID}] = res
	a.mu.Unlock()
	kind := workspace.KindRun
	if submit {
		kind = workspace.KindSubmit
	}
	p, t := res.Counts()
	firstPass := submit && res.Status == runner.StatusPassed && !w.TaskPassed(taskID)
	_ = w.RecordAttempt(workspace.Attempt{TaskID: taskID, Kind: kind, Status: res.Status, Passed: p, Total: t})
	emit(workspaceChanged, WorkspaceEvent{Workspace: wsID})
	if firstPass {
		w.Commit("Pass " + taskID)
		d, _ := w.Task(taskID)
		shown := fmt.Sprintf("You passed “%s” (%d/%d tests).", d.Title, p, t)
		a.post(w, workspace.ChatItem{Thread: taskID, Role: workspace.RoleNotice, Text: shown})
		// Writers keep the next objectives ready, so the learner never waits;
		// the planning tutor records what was learned.
		a.keepReady(w, fmt.Sprintf("The learner just passed “%s”.", d.Title))
		go a.send(w, workspace.PlanThread, message{
			Role:  workspace.RoleNotice,
			Shown: shown,
			Agent: fmt.Sprintf("[app] I passed task %s on submit (%d/%d tests). %s Record what I learned if there's evidence worth recording. Writers are preparing what comes next, so don't write lessons or tasks. Reply in one sentence at most.", taskID, p, t, hintSummary(w, taskID)),
		})
	}
	return res, nil
}

// hintSummary states exactly how many hints the learner used on a task, so
// the tutor doesn't have to guess from the chat.
func hintSummary(w *workspace.Workspace, taskID string) string {
	h := w.Hints(taskID)
	if h.Count == 0 {
		return "I used no hints."
	}
	return fmt.Sprintf("I used %d hint(s) on this task, highest level %d.", h.Count, h.Highest)
}
