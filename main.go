package main

import (
	"embed"
	"log"
	"time"

	"github.com/LukasKH/tutortop/internal/agent/threads"
	"github.com/LukasKH/tutortop/internal/app"
	"github.com/LukasKH/tutortop/internal/copilot"
	"github.com/LukasKH/tutortop/internal/editor"
	"github.com/LukasKH/tutortop/internal/events"
	"github.com/LukasKH/tutortop/internal/shellpath"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// Services whose resources must be stopped on exit implement Wails'
// ServiceShutdown. Asserted here so internal packages stay free of Wails.
var (
	_ application.ServiceShutdown = (*threads.ChatService)(nil)
	_ application.ServiceShutdown = (*editor.Service)(nil)
	_ application.ServiceShutdown = (*copilot.Service)(nil)
)

func main() {
	// Before anything looks up claude, codex, docker or git.
	shellpath.Fix()

	services, err := app.New(wailsBrowser{})
	if err != nil {
		log.Fatal(err)
	}

	registerEvents()

	wails := application.New(application.Options{
		Name:        "Tutortop",
		Description: "Learn anything verifiable, with your own AI tutor",
		// Wails shuts these down in reverse order. No service needs another
		// stopped last: Chat ends the agents, and Editor and Copilot stop
		// their language servers, each on its own.
		Services: []application.Service{
			application.NewService(services.Settings),
			application.NewService(services.Workspaces),
			application.NewService(services.Tasks),
			application.NewService(services.Lessons),
			application.NewService(services.Chat),
			application.NewService(services.Editor),
			application.NewService(services.Copilot),
		},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		// Used by server mode (`wails3 task run:server`): checks can take a
		// while the first time a sandbox image is pulled.
		Server: application.ServerOptions{ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute},
	})
	events.SetEmitter(func(name string, data any) { wails.Event.Emit(name, data) })
	services.Start()

	wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Tutortop",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(10, 10, 10),
		URL:              "/",
		// Paint the title bar in the app's colours from globals.css instead of
		// the system's white.
		Windows: application.WindowsWindow{
			Theme: application.Dark,
			CustomTheme: application.ThemeSettings{
				DarkModeActive: &application.WindowTheme{
					TitleBarColour:  application.NewRGBPtr(10, 10, 10),    // --background
					TitleTextColour: application.NewRGBPtr(237, 237, 237), // --foreground
					BorderColour:    application.NewRGBPtr(31, 31, 31),    // --border
				},
				DarkModeInactive: &application.WindowTheme{
					TitleBarColour:  application.NewRGBPtr(10, 10, 10),    // --background
					TitleTextColour: application.NewRGBPtr(133, 133, 133), // --muted-foreground
					BorderColour:    application.NewRGBPtr(31, 31, 31),    // --border
				},
			},
		},
	})

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}

// registerEvents tells Wails each event's name and payload type, so the
// generated bindings type them in TypeScript and Wails rejects emits with the
// wrong type.
func registerEvents() {
	application.RegisterEvent[events.Chat](string(events.ChatMessage))
	application.RegisterEvent[events.Busy](string(events.ThreadBusy))
	application.RegisterEvent[events.Working](string(events.ThreadWorking))
	application.RegisterEvent[events.Check](string(events.CheckStatus))
	application.RegisterEvent[events.Task](string(events.TaskPublished))
	application.RegisterEvent[events.Lesson](string(events.LessonPublished))
	application.RegisterEvent[events.Questions](string(events.QuestionsAsked))
	application.RegisterEvent[events.LSP](string(events.LSPMessage))
	application.RegisterEvent[events.Workspace](string(events.WorkspaceChanged))
	application.RegisterEvent[events.Workspace](string(events.CurriculumProposed))
}

// wailsBrowser opens URLs through Wails so internal/copilot stays free of it.
type wailsBrowser struct{}

func (wailsBrowser) OpenURL(url string) error {
	if wails := application.Get(); wails != nil {
		return wails.Browser.OpenURL(url)
	}
	return nil
}
