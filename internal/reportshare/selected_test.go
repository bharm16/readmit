package reportshare_test

import (
	"bytes"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/reportshare"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectedOriginalMessagesKeepExplicitOrderAndEveryOriginalByte(t *testing.T) {
	first := []byte("MSH|^~\\&|OWNED|||||||FIRST|T|2.5\rPID|1||A^^^OWNED\r")
	second := []byte("MSH|^~\\&|OWNED|||||||SECOND|T|2.5\nPID|1||B^^^OWNED\n")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	source, err := bundle.Write(filepath.Join(t.TempDir(), "case"), []bundle.Input{{Path: "first", Data: first, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.CR}}, {Path: "second", Data: second, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.LF}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reportshare.SelectOriginal(source, []string{source.Events[1].ID, source.Events[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(selected.Data, append(bytes.Clone(second), first...)) {
		t.Fatal("selection reordered or altered original bytes")
	}
	if len(selected.Occurrences) != 2 || selected.Occurrences[0].Occurrence != source.Events[1].ID || selected.Occurrences[0].Offset != 0 || selected.Occurrences[1].Offset != len(second) {
		t.Fatalf("wrong selected scope: %+v", selected.Occurrences)
	}
}
