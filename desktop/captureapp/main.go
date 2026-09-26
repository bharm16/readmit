//go:build darwin

// Native-only development host. It binds no evidence, filesystem, network or
// license facade: the production React components receive synthetic fixtures.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:dist
var assets embed.FS

// Catalog exposes only bounded capture operations in this development binary.
type Catalog struct {
	ctx    context.Context
	output string
	mu     sync.Mutex
	done   bool
	failed bool
}

var filename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,180}$`)

func validSnapshot(name string, x, y, width, height float64) bool {
	for _, value := range []float64{x, y, width, height} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return filename.MatchString(name) && width > 0 && height > 0 && x >= 0 && y >= 0 && x+width <= 1100 && y+height <= 760
}

func (c *Catalog) Mode() string { return os.Getenv("READMIT_CATALOG_ONLY") }

func (c *Catalog) Snapshot(name string, x, y, width, height float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed || !validSnapshot(name, x, y, width, height) {
		return fmt.Errorf("invalid snapshot bounds or name")
	}
	path := filepath.Join(c.output, name+".png")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return fmt.Errorf("snapshot destination already exists")
	}
	err := snapshot(path, x, y, width, height)
	if err != nil {
		c.failed = true
	}
	return err
}

func (c *Catalog) Finish(report string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || !json.Valid([]byte(report)) {
		return fmt.Errorf("invalid or repeated report")
	}
	f, err := os.OpenFile(filepath.Join(c.output, "capture.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(report)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	c.done = true
	runtime.Quit(c.ctx)
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: readmit-catalog OUTPUT_DIRECTORY")
		os.Exit(2)
	}
	output, err := filepath.Abs(os.Args[1])
	if err != nil {
		panic(err)
	}
	info, err := os.Stat(output)
	if err != nil || !info.IsDir() {
		panic("output directory must exist")
	}
	c := &Catalog{output: output}
	timer := time.AfterFunc(30*time.Minute, func() { fmt.Fprintln(os.Stderr, "capture timed out"); os.Exit(2) })
	defer timer.Stop()
	err = wails.Run(&options.App{Title: "Readmit — Screenshot catalog", Width: 1100, Height: 760, MinWidth: 1100, MinHeight: 760, MaxWidth: 1100, MaxHeight: 760, DisableResize: true, AssetServer: &assetserver.Options{Assets: assets}, OnStartup: func(ctx context.Context) { c.ctx = ctx }, Bind: []any{c}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.done {
		fmt.Fprintln(os.Stderr, "capture window closed before completion")
		os.Exit(2)
	}
}
