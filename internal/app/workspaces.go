package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LukasKH/tutortop/internal/workspace"
)

// Workspaces: the list on Home, creating and deleting one, the state the
// workspace screen shows, and the learner's own changes to the plan.

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
	provider := a.Settings().Provider
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
	a.send(w, workspace.PlanThread, message{
		Role:  workspace.RoleNotice,
		Shown: "Workspace created. Your tutor is getting started.",
		Agent: fmt.Sprintf("[app] I want to learn: %s. This is a new workspace; start the interview.", title),
	})
	return w.ID, nil
}

// DeleteWorkspace stops the workspace's agents and language servers, then
// deletes its files for good.
func (a *App) DeleteWorkspace(id string) error {
	a.agents.endWorkspace(id)
	a.servers.stopWorkspace(id)
	a.mcp.forget(id)
	return a.store.Delete(id)
}

// WorkspaceSnapshot is everything the workspace screen shows, read at one
// moment. The frontend keeps it current with events until the next one.
type WorkspaceSnapshot struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	Icon       string                   `json:"icon"`
	Mission    string                   `json:"mission"`
	Curriculum *workspace.Curriculum    `json:"curriculum"`
	Proposal   *workspace.Curriculum    `json:"proposal"`
	Progress   workspace.Progress       `json:"progress"`
	Tasks      []workspace.TaskStatus   `json:"tasks"`
	Lessons    []workspace.LessonStatus `json:"lessons"`
	Questions  *workspace.Questions     `json:"questions"`
	Busy       map[string]bool          `json:"busy"` // thread -> its tutor or writer is working
	// Checks are writers' latest sandbox checks of new tasks, by objective.
	Checks map[string]SandboxCheck `json:"checks"`
	// Seq is when Busy and Checks were read, in the sequence that numbers
	// busy and check events: one with a higher Seq happened after.
	Seq uint64 `json:"seq"`
}

func (a *App) Workspace(id string) (WorkspaceSnapshot, error) {
	w, err := a.store.Open(id)
	if err != nil {
		return WorkspaceSnapshot{}, err
	}
	st := WorkspaceSnapshot{ID: w.ID, Title: w.Meta.Title, Icon: w.Meta.Icon, Mission: w.ReadDoc("MISSION.md"), Progress: w.Progress()}
	if st.Curriculum, err = w.Curriculum(); err != nil {
		return st, err
	}
	st.Proposal, _ = w.Proposal()
	st.Tasks, _ = w.TaskStatuses()
	st.Lessons, _ = w.LessonStatuses()
	st.Questions = w.PendingQuestions()
	st.Busy, st.Checks, st.Seq = a.agents.state(id)
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
	a.prepareImage(w, c.Language)
	post(w, workspace.ChatItem{
		Thread: workspace.PlanThread.String(), Role: workspace.RoleNotice, Mark: workspace.MarkPlanApproved,
		Text: fmt.Sprintf("You approved the plan (revision %d).", c.Revision),
	})
	a.keepReady(w, "The learner just approved the plan.")
	return nil
}

// prepareImage builds a language's sandbox image in the background, so the
// first test run is fast. Plans prepare theirs when approved, and workspaces
// when the app starts (ServiceStartup).
func (a *App) prepareImage(w *workspace.Workspace, language string) {
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
	a.send(w, workspace.PlanThread, message{
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
	a.send(w, workspace.PlanThread, message{Role: workspace.RoleUser, Shown: text, Agent: text})
	return nil
}
