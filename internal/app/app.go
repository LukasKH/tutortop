// Package app ties workspaces, the sandbox, the MCP tools and the agent
// together, and exposes them to the frontend as Wails services, one per
// area.
package app

import (
	"cmp"
	"os"
	"path/filepath"

	"github.com/LukasKH/tutortop/internal/sandbox"
	"github.com/LukasKH/tutortop/internal/workspace"
	"github.com/adrg/xdg"
)

// Services are the app's Wails services, one per area. main.go registers
// each; their exported methods are the frontend's API.
type Services struct {
	Settings   *SettingsService
	Workspaces *WorkspaceService
	Tasks      *TaskService
	Lessons    *LessonService
	Chat       *ChatService
	Editor     *EditorService
}

// New builds the services on the user's workspaces and settings.
func New() (*Services, error) {
	settings := newSettingsStore(filepath.Join(xdg.ConfigHome, "tutortop", "settings.yaml"))
	// The workspaces folder: TUTORTOP_HOME, else the one chosen in the
	// settings, else ~/Tutortop. It's fixed for the run: running agents open
	// their workspace by id.
	store := workspace.NewStore(cmp.Or(os.Getenv("TUTORTOP_HOME"), settings.Load().WorkspacesFolder, workspace.DefaultRoot()))
	return newServices(store, settings, sandbox.New(), newLinkChecker(), installedCLI{})
}

// newServices builds the pieces several services share once, and the
// services on them: the workspaces in store, the settings, the sandbox, the
// link checker and the agent CLI it starts. Tests pass their own.
func newServices(store *workspace.Store, settings *settingsStore, runner *sandbox.Runner, links *linkChecker, cli agentCLI) (*Services, error) {
	tools, err := startMCP(runner, links)
	if err != nil {
		return nil, err
	}
	agents := newThreadAgents(settings, tools, cli)
	writers := newWriters(agents)
	servers := newLanguageServers()
	return &Services{
		Settings:   newSettingsService(settings, newModelCache(), runner, store.Root),
		Workspaces: newWorkspaceService(store, settings, runner, agents, writers, servers),
		Tasks:      newTaskService(store, runner, agents, writers),
		Lessons:    newLessonService(store),
		Chat:       newChatService(store, agents),
		Editor:     newEditorService(store, runner, servers),
	}, nil
}
