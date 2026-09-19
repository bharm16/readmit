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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engine"
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

// reportsVersion reports whether this invocation asks for the build identity
// the shell was stamped with instead of a window. An installed application is
// checked on a machine that has no terminal open and, in a packaging check, no
// display at all, so the identity has to be answerable without creating one.
func reportsVersion(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "--version" || argument == "-version" {
			return true
		}
	}
	return false
}

func main() {
	if err := runShell(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func runShell(arguments []string) error {
	// The shell is stamped with the same engine identity as the command line,
	// so an installed package and a release archive report one build rather
	// than two. Answering it opens no window and reads no evidence.
	if reportsVersion(arguments) {
		fmt.Printf("readmit-desktop version %s\n", engine.Version())
		return nil
	}
	// The three files of local shell state, each named explicitly. None holds
	// evidence: the folders opened recently, the filters this person saved, and
	// the working session they have not stored, which is what the window
	// restores after an interruption.
	startupCheck := len(arguments) == 1 && arguments[0] == "--startup-check"
	var recent, filters, session string
	if startupCheck {
		directory, err := os.MkdirTemp("", "readmit-startup-check-")
		if err != nil {
			return errors.New("readmit: startup check storage unavailable")
		}
		defer os.RemoveAll(directory)
		recent, filters, session = filepath.Join(directory, "recent.json"), filepath.Join(directory, "filters.json"), filepath.Join(directory, "session.json")
	} else {
		var err, filtersErr, sessionErr error
		recent, err = desktop.DefaultRecentPath()
		filters, filtersErr = desktop.DefaultFiltersPath()
		session, sessionErr = desktop.DefaultSessionPath()
		if err != nil || filtersErr != nil || sessionErr != nil {
			return errors.New("readmit: cannot resolve the user configuration directory")
		}
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
	var ready atomic.Bool
	if startupCheck {
		// Check the real native webview, without restoring any user's workspace.
		// A wedged native startup must never become a successful CI install.
		timer := time.AfterFunc(30*time.Second, func() {
			fmt.Fprintln(os.Stderr, "readmit: native startup check timed out")
			os.Exit(2)
		})
		defer timer.Stop()
		application.OnDomReady = func(ctx context.Context) {
			ready.Store(true)
			runtime.Quit(ctx)
		}
	}
	if err := wails.Run(application); err != nil {
		return errors.New("readmit: the desktop window could not be created")
	}
	if startupCheck {
		if !ready.Load() {
			return errors.New("readmit: native webview did not become ready")
		}
		fmt.Println("readmit-desktop native webview ready")
	}
	return nil
}
