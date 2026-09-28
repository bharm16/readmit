//go:build !windows

package desktop_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

const (
	importDiskFullChild   = "READMIT_DESKTOP_IMPORT_DISK_FULL_CHILD"
	importDiskFullProject = "READMIT_DESKTOP_IMPORT_DISK_FULL_PROJECT"
	importDiskFullSource  = "READMIT_DESKTOP_IMPORT_DISK_FULL_SOURCE"
)

// An import the disk refused part way through writing its case is a visible
// failure, never a completed import: the case is retained incomplete, where the
// reader refuses it, no receipt claims it, and the project registers nothing.
// Once the disk has room again, importing into a new destination completes. The
// disk is full in the one way a test can make it full portably — the
// account's own file size limit — in a child process, so the limit starves
// nothing else.
func TestADiskRefusalDuringAnImportRegistersNothingAndAcceptsNoPartialCase(t *testing.T) {
	if os.Getenv(importDiskFullChild) == "1" {
		importDiskFullChildRun(t)
		return
	}
	folder := importProject(t)
	source := mllpSource(t, 300)
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), importDiskFullChild+"=1", importDiskFullProject+"="+folder, importDiskFullSource+"="+source)
	output, _ := child.CombinedOutput()
	if !strings.Contains(string(output), "RESULT:"+string(desktop.Failed)+" registered=false") {
		t.Fatalf("the child did not report a refused import: %s", output)
	}

	app := workspaceApp(t)
	if opened := app.OpenProject(folder); opened.State != desktop.Empty || opened.Project == nil || len(opened.Project.Cases) != 0 {
		t.Fatalf("the project registered an import the disk refused: %+v", opened)
	}
	if listed := entries(t, folder); len(listed) != 2 {
		t.Fatalf("a refused import added project entries: %v", listed)
	}
	again := app.ImportCase(previewedImport(t, app, folder, "Imported", mllpSource(t, 3), "imported"))
	if again.State != desktop.Completed || again.Case == nil {
		t.Fatalf("importing again once the disk had room did not complete: %+v", again)
	}
}

// importDiskFullChildRun writes its license fixture and previews before the
// limit drops, then imports a case whose event record is past the account's file size limit
// — the operation clock record admission writes is far below it — and reports
// what the facade answered.
func importDiskFullChildRun(t *testing.T) {
	folder := os.Getenv(importDiskFullProject)
	app := workspaceApp(t)
	request := previewedImport(t, app, folder, "Refused", os.Getenv(importDiskFullSource), "refused")
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Printf("RESULT:getrlimit failed\n")
		return
	}
	limit.Cur = 16 << 10
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		fmt.Printf("RESULT:setrlimit failed\n")
		return
	}
	result := app.ImportCase(request)
	fmt.Printf("RESULT:%s registered=%t\n", result.State, result.Case != nil)
}
