// Command bindgen writes frontend/src/bindings.gen.ts: the TypeScript
// declarations of the two Go objects the desktop shell binds, generated from
// their Go types so the window's declarations cannot drift from what crosses
// Wails' boundary. Run it from the desktop module:
//
//	go run ./bindgen
//
// TestGeneratedBindingsAreCurrent fails while the committed file differs from
// what this writes.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/desktop/hubadmin"
	"github.com/bharm16/readmit/internal/desktop"
)

// output is the generated file, relative to the desktop module.
const output = "frontend/src/bindings.gen.ts"

// bound are the objects the shell binds, as desktop/main.go binds them.
var bound = []object{
	{value: &desktop.App{}, facade: "Facade"},
	{value: &hubadmin.Admin{}, facade: "HubAdminFacade"},
}

func main() {
	module, err := desktopModule()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bindgen:", err)
		os.Exit(1)
	}
	generated, err := generate(filepath.Dir(module), bound, facadeNames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bindgen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(module, filepath.FromSlash(output)), generated, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "bindgen:", err)
		os.Exit(1)
	}
}

// desktopModule finds the desktop module's directory from the working
// directory, which is inside it for "go run ./bindgen" and "go test".
func desktopModule() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if module, err := modulePathOf(filepath.Join(dir, "go.mod")); err == nil && module == modulePath+"/desktop" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run bindgen inside the desktop module")
		}
		dir = parent
	}
}

func modulePathOf(goMod string) (string, error) {
	file, err := os.Open(goMod)
	if err != nil {
		return "", err
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		if module, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "module "); ok {
			return strings.TrimSpace(module), nil
		}
	}
	return "", errors.New(goMod + " declares no module")
}
