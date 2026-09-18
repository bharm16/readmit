// Command readmit-desktop is the native shell around the typed Go application
// facade in internal/desktop. It owns window, asset and dialog wiring only:
// every evidence decision belongs to the facade and the internal packages it
// calls, which are the same packages the readmit command line calls. Nothing
// here parses HL7, reads a bundle, or reimplements a command.
//
// This module is deliberately separate from the readmit module. Wails needs cgo
// and a platform webview, so the desktop dependency graph never reaches the
// static CGO_ENABLED=0 command-line release, which this build does not change.
package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"sync"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The window renders only these bundled files. The shell fetches nothing at
// run time and contacts no network service.
//
//go:embed all:frontend/dist
var assets embed.FS

// dialog presents the host's native folder picker. Wails supplies the
// application context after startup, so it is installed then and read under a
// mutex rather than assumed to be ready.
type dialog struct {
	mu  sync.Mutex
	ctx context.Context
}

func (d *dialog) start(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ctx = ctx
}

func (d *dialog) ChooseFolder(title string) (string, error) {
	d.mu.Lock()
	ctx := d.ctx
	d.mu.Unlock()
	if ctx == nil {
		return "", errors.New("the application window is not ready")
	}
	return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: title})
}

func main() {
	// The three files of local shell state, each named explicitly. None holds
	// evidence: the folders opened recently, the filters this person saved, and
	// the working session they have not stored, which is what the window
	// restores after an interruption.
	recent, err := desktop.DefaultRecentPath()
	filters, filtersErr := desktop.DefaultFiltersPath()
	session, sessionErr := desktop.DefaultSessionPath()
	if err != nil || filtersErr != nil || sessionErr != nil {
		log.Fatal("readmit: cannot resolve the user configuration directory")
	}
	folders := &dialog{}
	application := &options.App{
		Title:       "readmit",
		Width:       1100,
		Height:      760,
		MinWidth:    640,
		MinHeight:   480,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   folders.start,
		Bind:        []any{desktop.New(folders, recent, filters, session)},
		// The shell adds no logging of its own, reports no telemetry, no crash
		// reports and no update checks, and sends nothing to a network. The
		// window host is held to errors so it emits no routine output either.
		LogLevel: logger.ERROR,
	}
	if err := wails.Run(application); err != nil {
		log.Fatal("readmit: the desktop window could not be created")
	}
}
