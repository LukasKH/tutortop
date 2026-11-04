package main

import (
	"embed"
	"log"
	"time"

	"github.com/LukasKH/tutortop/internal/app"
	"github.com/LukasKH/tutortop/internal/shellpath"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Before anything looks up claude, codex, docker or git.
	shellpath.Fix()

	services, err := app.New()
	if err != nil {
		log.Fatal(err)
	}

	wails := application.New(application.Options{
		Name:        "Tutortop",
		Description: "Learn anything verifiable, with your own AI tutor",
		// Wails starts these in this order and shuts them down in reverse.
		// No service needs another started first or stopped last: Workspaces
		// builds sandbox images in the background, Chat ends the agents and
		// Editor stops the language servers, each on its own.
		Services: []application.Service{
			application.NewService(services.Settings),
			application.NewService(services.Workspaces),
			application.NewService(services.Tasks),
			application.NewService(services.Lessons),
			application.NewService(services.Chat),
			application.NewService(services.Editor),
		},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		// Used by server mode (`wails3 task run:server`): checks can take a
		// while the first time a sandbox image is pulled.
		Server: application.ServerOptions{ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute},
	})

	wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Tutortop",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(10, 10, 10),
		URL:              "/",
	})

	if err := wails.Run(); err != nil {
		log.Fatal(err)
	}
}
