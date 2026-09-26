package desktop_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var cliBuildTags []string
var cliDirectory string

func TestMain(m *testing.M) {
	code := m.Run()
	if cliDirectory != "" {
		os.RemoveAll(cliDirectory)
	}
	os.Exit(code)
}

// Build once for the package, with the same durability mode as the facade.
// Every invocation still runs a fresh process over that test's own files.
var buildCLI = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "readmit-desktop-cli-")
	if err != nil {
		return "", err
	}
	cliDirectory = dir
	bin := filepath.Join(dir, "readmit")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	args := append([]string{"build", "-o", bin}, cliBuildTags...)
	build := exec.Command("go", append(args, "./cmd/readmit")...)
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build CLI: %w: %s", err, out)
	}
	return bin, nil
})

func cliExecutable(t *testing.T) string {
	t.Helper()
	bin, err := buildCLI()
	if err != nil {
		t.Fatal(err)
	}
	return bin
}
