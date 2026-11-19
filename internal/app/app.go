// Package app wires the app together and serves the workspaces, tasks and
// lessons to the frontend. Each area of the frontend's API is a Wails
// service: WorkspaceService, TaskService and LessonService here, the chats'
// threads.ChatService, the Settings screen's settings.Service and the
// editor's editor.Service and copilot.Service.
package app

import (
	"cmp"
	"os"
	"path/filepath"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/copilot"
	"github.com/LukasKH/tutortop/internal/editor"
	"github.com/LukasKH/tutortop/internal/mcp"
	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/settings"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/adrg/xdg"
)

// Services are the app's Wails services, one per area. main.go registers
// each; their exported methods are the frontend's API.
type Services struct {
	Settings   *settings.Service
	Workspaces *WorkspaceService
	Tasks      *TaskService
	Lessons    *LessonService
	Chat       *threads.ChatService
	Editor     *editor.Service
	Copilot    *copilot.Service
}

// New builds the services on the user's workspaces and settings, with
// thread agents on the agent CLIs installed on the computer.
func New() (*Services, error) {
	settingsStore := settings.NewStore(filepath.Join(xdg.ConfigHome, "tutortop", "settings.yaml"))
	// The workspaces folder: TUTORTOP_HOME, else the one chosen in the
	// settings, else ~/Tutortop. It's fixed for the run: running agents open
	// their workspace by id.
	store := workspace.NewStore(cmp.Or(os.Getenv("TUTORTOP_HOME"), settingsStore.Load().WorkspacesFolder, workspace.DefaultRoot()))
	runner := sandbox.New()
	tools, err := mcp.Start(runner, mcp.NewLinkChecker(nil))
	if err != nil {
		return nil, err
	}
	agents := threads.NewAgents(settingsStore, tools, threads.InstalledCLI{})
	return newServices(store, settingsStore, runner, agents), nil
}

// newServices builds the pieces several services share once, and the
// services on them: the workspaces in store, the settings, the sandbox
// runner and the thread agents. Tests pass agents on a fake CLI.
func newServices(store *workspace.Store, settingsStore *settings.Store, runner *sandbox.Runner, agents *threads.Agents) *Services {
	writers := threads.NewWriters(agents)
	servers := editor.NewServers()
	return &Services{
		Settings:   settings.NewService(settingsStore, runner, store.Root),
		Workspaces: newWorkspaceService(store, settingsStore, runner, agents, writers, servers),
		Tasks:      newTaskService(store, runner, agents, writers),
		Lessons:    newLessonService(store),
		Chat:       threads.NewChatService(store, agents),
		Editor:     editor.NewService(store, runner, servers),
		Copilot:    copilot.NewService(filepath.Join(xdg.DataHome, "tutortop", "copilot")),
	}
}
