package desktop_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

const facadeAnyPolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func TestChooseCapturePathKinds(t *testing.T) {
	c := &chooser{folder: "/tmp/export", files: []string{"/usr/local/bin/transfer"}}
	app := newApp(t, c)
	folder := app.ChooseCapturePath("source-root")
	if folder.State != desktop.Completed || folder.Kind != "source-root" || folder.Paths[0] != "/tmp/export" {
		t.Fatalf("folder: %+v", folder)
	}
	program := app.ChooseCapturePath("transfer-program")
	if program.State != desktop.Completed || program.Paths[0] != "/usr/local/bin/transfer" {
		t.Fatalf("program: %+v", program)
	}
	c.files = nil
	dismissed := app.ChooseCapturePath(desktop.CertificatePath)
	if dismissed.State != desktop.Cancelled {
		t.Fatalf("dismissed: %+v", dismissed)
	}
	// Only the four kinds a source editor chooses are offered.
	if retired := app.ChooseCapturePath("policy"); retired.State != desktop.Failed {
		t.Fatalf("a retired kind: %+v", retired)
	}
}
