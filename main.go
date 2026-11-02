package main

import (
	"embed"
	"log"
	"time"

	"github.com/LukasKH/tutortop/internal/app"
	"github.com/LukasKH/tutortop/internal/shellenv"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[app.Event]("tutortop")
}

func main() {
	// Before anything looks for docker, podman, claude or codex.
	shellenv.LoadPath()

	var wails *application.App
	svc, err := app.New(func(name string, data any) {
		if wails != nil {
			wails.Event.Emit(name, data)
		}
	})
	if err != nil {
		log.Fatal(err)
	}

	wails = application.New(application.Options{
		Name:        "Tutortop",
		Description: "Learn anything verifiable, with your own AI coach",
		Services:    []application.Service{application.NewService(svc)},
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
