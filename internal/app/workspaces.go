package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/editor"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
)

// WorkspaceService serves the workspaces: the list on Home, creating and
// deleting one, the state the workspace screen shows, and the learner's own
// changes to the plan. It builds the plans' sandbox images ahead.
type WorkspaceService struct {
	store    *workspace.Store
	settings *settings.Store // the agent a new workspace starts with
	runner   *sandbox.Runner
	agents   *threads.Agents  // a new workspace's interview, the plan's notices, and ending a deleted workspace's agents
	writers  *threads.Writers // write objectives once the plan is approved or opened ahead
	servers  *editor.Servers
}

func newWorkspaceService(store *workspace.Store, settingsStore *settings.Store, runner *sandbox.Runner, agents *threads.Agents, writers *threads.Writers, servers *editor.Servers) *WorkspaceService {
	return &WorkspaceService{store: store, settings: settingsStore, runner: runner, agents: agents, writers: writers, servers: servers}
}

func (s *WorkspaceService) ListWorkspaces() ([]workspace.Summary, error) { return s.store.List() }

// Activity is what the learner finished each day across all workspaces, with
// their streak, for the activity panel on Home.
func (s *WorkspaceService) Activity() (workspace.Activity, error) {
	list, err := s.store.List()
	if err != nil {
		return workspace.Activity{}, err
	}
	var all []workspace.Completion
	for _, sum := range list {
		w, err := s.store.Open(sum.ID)
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
// workspace.Icons(), as when the topic comes from an idea.
func (s *WorkspaceService) CreateWorkspace(title, icon string) (string, error) {
	provider := s.settings.Load().Provider
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("give the topic a name")
	}
	if !workspace.ValidIcon(icon) {
		return "", fmt.Errorf("unknown icon %q", icon)
	}
	existing, _ := s.store.List()
	for _, e := range existing {
		if strings.EqualFold(e.Title, title) {
			return "", fmt.Errorf("you already have a workspace called “%s”; pick another name", e.Title)
		}
	}
	w, err := s.store.Create(title, provider)
	if err != nil {
		return "", err
	}
	if err := w.SetIcon(icon); err != nil {
		return "", err
	}
	// The tutor starts the interview right away.
	s.agents.Send(w, workspace.PlanThread, threads.Message{
		Role:  workspace.RoleNotice,
		Shown: "Workspace created. Your tutor is getting started.",
		Agent: fmt.Sprintf("[app] I want to learn: %s. This is a new workspace; start the interview.", title),
	})
	return w.ID, nil
}

// DeleteWorkspace stops the workspace's agents and language servers, then
// deletes its files and its runs' cache for good.
func (s *WorkspaceService) DeleteWorkspace(id string) error {
	s.agents.EndWorkspace(id)
	s.servers.StopWorkspace(id)
	go s.removeCache(id)
	return s.store.Delete(id)
}

// WorkspaceSnapshot is everything the workspace screen shows, read at one
// moment. The frontend keeps it current with events until the next one.
type WorkspaceSnapshot struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	Icon       string                   `json:"icon"`
	Mission    string                   `json:"mission"`
	Notes      string                   `json:"notes"`     // NOTES.md: how the learner likes to learn
	Known      string                   `json:"known"`     // what the mission says the learner already knows, like "Python"; "" if it doesn't say
	Resources  string                   `json:"resources"` // RESOURCES.md: the sources the plan and its lessons draw on
	Curriculum *workspace.Curriculum    `json:"curriculum"`
	Proposal   *workspace.Curriculum    `json:"proposal"`
	Progress   workspace.Progress       `json:"progress"`
	Tasks      []workspace.TaskStatus   `json:"tasks"`
	Lessons    []workspace.LessonStatus `json:"lessons"`
	Questions  *workspace.Questions     `json:"questions"`
	Busy       map[string]bool          `json:"busy"` // thread -> its tutor or writer is working
	// Checks are writers' latest sandbox checks of new tasks, by objective.
	Checks map[string]events.SandboxCheck `json:"checks"`
	// Seq is when Busy and Checks were read, in the sequence that numbers
	// busy and check events: one with a higher Seq happened after.
	Seq uint64 `json:"seq"`
}

func (s *WorkspaceService) Workspace(id string) (WorkspaceSnapshot, error) {
	w, err := s.store.Open(id)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	st := WorkspaceSnapshot{
		ID:        w.ID,
		Title:     w.Meta.Title,
		Icon:      w.Meta.Icon,
		Mission:   w.ReadDoc("MISSION.md"),
		Notes:     w.ReadDoc("NOTES.md"),
		Known:     w.PriorKnowledge(),
		Resources: w.ReadDoc("RESOURCES.md"),
		Progress:  w.Progress(),
	}
	if st.Curriculum, err = w.Curriculum(); err != nil {
		return st, err
	}
	st.Proposal, _ = w.Proposal()
	st.Tasks, _ = w.TaskStatuses()
	st.Lessons, _ = w.LessonStatuses()
	st.Questions = w.PendingQuestions()
	st.Busy, st.Checks, st.Seq = s.agents.State(id)
	return st, nil
}

func (s *WorkspaceService) ApproveProposal(wsID string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	c, err := w.ApproveProposal()
	if err != nil {
		return err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	s.prepareImage(w, c.Language)
	s.agents.SupersedeTurn(wsID, workspace.PlanThread)
	threads.Post(w, workspace.ChatItem{
		Thread: workspace.PlanThread.String(),
		Role:   workspace.RoleNotice,
		Mark:   workspace.MarkPlanApproved,
		Text:   fmt.Sprintf("You approved the plan (revision %d).", c.Revision),
	})
	s.writers.KeepReady(w, "The learner just approved the plan.")
	return nil
}

// SaveMission saves the learner's own edit of their mission, then asks the
// planning tutor to propose curriculum changes if the plan no longer fits it.
// The curriculum itself only changes through a proposal the learner approves.
func (s *WorkspaceService) SaveMission(wsID, mission string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	changed, err := w.SaveMission(mission)
	if err != nil || !changed {
		return err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	s.agents.Send(w, workspace.PlanThread, threads.Message{
		Role:  workspace.RoleNotice,
		Shown: "You edited your mission.",
		Agent: "[app] I edited my mission by hand. Read it; it's mine now, so don't change it back. " +
			"If the curriculum no longer fits it, propose a revision that does, keeping the ids of objectives that stay. Otherwise reply in one sentence.",
	})
	return nil
}

// SaveNotes saves the learner's own edit of how they like to learn, and tells
// the planning tutor, who keeps the notes current from their feedback.
func (s *WorkspaceService) SaveNotes(wsID, notes string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	changed, err := w.SaveNotes(notes)
	if err != nil || !changed {
		return err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	s.agents.Send(w, workspace.PlanThread, threads.Message{
		Role:  workspace.RoleNotice,
		Shown: "You edited how you like to learn.",
		Agent: "[app] I edited my notes on how I like to learn by hand. Read them and keep what I wrote; build on it when I give feedback. " +
			"If I changed how big tasks should be, update task_defaults to match. Reply in one sentence.",
	})
	return nil
}

func (s *WorkspaceService) RejectProposal(wsID, feedback string) error {
	w, err := s.store.Open(wsID)
	if err != nil {
		return err
	}
	if err := w.DiscardProposal(); err != nil {
		return err
	}
	events.Emit(events.WorkspaceChanged, events.Workspace{Workspace: wsID})
	text := "I'd like changes to the proposed curriculum: " + feedback
	s.agents.SupersedeTurn(wsID, workspace.PlanThread)
	s.agents.Send(w, workspace.PlanThread, threads.Message{Role: workspace.RoleUser, Shown: text, Agent: text})
	return nil
}

// WriteObjectiveNow has a writer write an objective's lesson and task now,
// because the learner opened it ahead of the suggested order. It returns
// once the writer is asked; the lesson and task arrive as events, and a
// writer that fails posts why in its thread.
func (s *WorkspaceService) WriteObjectiveNow(wsID, objectiveID string) error {
	w, err := s.store.Open(wsID)
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
	s.writers.Write(w, o, "The learner opened it and is waiting for it, so be quick. Write the lesson first, as they read it first.")
	return nil
}

// removeCache deletes the workspace's cache volumes in the background, so a
// slow or stopped Docker doesn't hold up the delete. A cache left behind only
// takes disk space, so a failure is dropped.
func (s *WorkspaceService) removeCache(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_ = s.runner.RemoveCache(ctx, id)
}

// warmUp builds the sandbox images of the unfinished workspaces' plans in the
// background, so the first check in each is fast, then removes the images no
// unfinished workspace uses. A completed course needs none: its lessons need
// no sandbox, and a check run there or a new plan builds the image again.
// Reading a workspace has no such side effect.
func (s *WorkspaceService) warmUp() {
	go func() {
		var builds sync.WaitGroup
		list, _ := s.store.List()
		for _, sum := range list {
			w, err := s.store.Open(sum.ID)
			if err != nil || w.Progress().Complete() {
				continue
			}
			c, _ := w.Curriculum()
			if c == nil {
				continue
			}
			if tc, err := w.Toolchain(c.Language); err == nil {
				// A failed warm-up shows at the first run, which prepares the image again.
				builds.Go(func() { _ = s.runner.Prepare(context.Background(), tc) })
			}
		}
		// After the builds, so the old images' layers can serve them as cache.
		builds.Wait()
		s.removeUnusedImages()
	}()
}

// prepareImage builds a language's sandbox image in the background, so the
// first test run is fast. Plans prepare theirs when approved; warmUp prepares
// the unfinished workspaces'.
func (s *WorkspaceService) prepareImage(w *workspace.Workspace, language string) {
	if tc, err := w.Toolchain(language); err == nil {
		// A failed warm-up shows at the first run, which prepares the image again.
		go func() { _ = s.runner.Prepare(context.Background(), tc) }()
	}
}

// removeUnusedImages removes the sandbox images that neither a built-in
// language nor an unfinished workspace uses. If a workspace can't be read,
// it removes nothing; leftover images only cost disk space until the next
// start.
func (s *WorkspaceService) removeUnusedImages() {
	used, err := s.store.ToolchainsInUse()
	if err != nil {
		return
	}
	for _, lang := range sandbox.BuiltinLanguages() {
		tc, _ := sandbox.Builtin(lang)
		used = append(used, tc)
	}
	_ = s.runner.RemoveUnusedImages(context.Background(), used)
}
