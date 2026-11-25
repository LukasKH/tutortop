package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"time"

	"github.com/LukasKH/tutortop/internal/update"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

//go:embed build/updater.pub
var updaterPublicKey []byte

// wailsUpdater adapts Wails' app.Updater to update.Engine so internal/update
// stays free of Wails (and GTK).
type wailsUpdater struct {
	wails *application.App
}

// newWailsUpdater initializes Wails' updater against GitHub Releases. If
// initialization fails, it returns a nil engine and ManualOnly support so the
// app still starts cleanly without offering broken in-app installs.
func newWailsUpdater(wails *application.App) (update.Engine, update.Support) {
	provider, err := endpoint.New(endpoint.Config{
		URL:        update.ManifestURL,
		HTTPClient: updaterHTTPClient(),
	})
	if err != nil {
		log.Printf("updater disabled: %v", err)
		return nil, update.ManualOnly("Automatic updates couldn't be initialized. Download updates from GitHub instead.")
	}
	if err := wails.Updater.Init(updater.Config{
		CurrentVersion: update.Version,
		Providers:      []updater.Provider{provider},
		PublicKey:      updaterPublicKey,
		Window:         updater.WindowNone,
	}); err != nil {
		log.Printf("updater disabled: %v", err)
		return nil, update.ManualOnly("Automatic updates couldn't be initialized. Download updates from GitHub instead.")
	}
	return &wailsUpdater{wails: wails}, updateSupport()
}

func (u *wailsUpdater) Check(ctx context.Context) (*update.Release, error) {
	rel, err := u.wails.Updater.Check(ctx)
	if err != nil || rel == nil {
		return nil, err
	}
	return &update.Release{Version: rel.Version, Notes: rel.Notes}, nil
}

func (u *wailsUpdater) DownloadAndInstall(ctx context.Context, onProgress func(written, total int64)) error {
	if onProgress != nil {
		off := u.wails.Event.On(updater.EventDownloadProgress, func(e *application.CustomEvent) {
			if p, ok := e.Data.(updater.Progress); ok {
				onProgress(p.Written, p.Total)
			}
		})
		defer off()
	}
	return u.wails.Updater.DownloadAndInstall(ctx)
}

func (u *wailsUpdater) Restart(ctx context.Context) error {
	return u.wails.Updater.Restart(ctx)
}

// updaterHTTPClient avoids Wails' default 30-second whole-request Timeout,
// which would abort large release downloads on slower links. Manifest checks
// are still bounded by their 30-second context, and headers time out via the
// transport.
func updaterHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}
