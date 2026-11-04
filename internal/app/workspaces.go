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
	a.send(w, workspace.PlanThread, message{
		Role:  workspace.RoleNotice,
		Shown: "Workspace created. Your tutor is getting started.",
		Agent: fmt.Sprintf("[app] I want to learn: %s. This is a new workspace; start the interview.", title),
	})
	return w.ID, nil
}

// DeleteWorkspace stops the workspace's tutors and language servers, then
// deletes its files for good.
func (a *App) DeleteWorkspace(id string) error {
	a.mu.Lock()
	var servers []*languageServer
	for key, s := range a.sessions {
		if key.ws == id {
			a.end(s)
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
	st.Busy, st.Checks = a.activity(id)
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
	a.post(w, workspace.ChatItem{Thread: workspace.PlanThread.String(), Role: workspace.RoleNotice, Text: fmt.Sprintf("You approved the plan (revision %d).", c.Revision)})
	a.keepReady(w, "The learner just approved the plan.")
	return nil
}

// prepare builds a language's sandbox image in the background, so the first
// check is fast. Plans prepare theirs when approved, and existing workspaces
// when the app starts (ServiceStartup).
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
