package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/casegen"
)

func TestScenarioGenerateCasePublicInterface(t *testing.T) {
	root := "../testdata/casegen/"
	destination := filepath.Join(t.TempDir(), "generated")
	out, stderr, err := run(t, "scenario", "generate-case", root+"request-book-reschedule-cancel.json", root+"owned/profile-siu.json", root+"owned/pack.json", "--output", destination)
	if err != nil || stderr != "" || !strings.Contains(out, "Generated 9 cases, 27 messages, HL7 2.5.1 SIU.") || !strings.Contains(out, "Nothing was sent") {
		t.Fatalf("generate-case: %q %q %v", out, stderr, err)
	}
	if strings.Contains(out, "APPT-0001") || strings.Contains(out, "DOE") {
		t.Fatal("generation output leaked values")
	}
	raw, err := os.ReadFile(filepath.Join(destination, "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	record, err := casegen.ReadRecord(raw)
	if err != nil || len(record.Cases) != 9 || record.Cases[0].Entry != "plain-baseline" {
		t.Fatalf("%v %+v", err, record.Cases)
	}
	if _, err := os.Stat(filepath.Join(destination, "plain-baseline", "manifest.json")); err != nil {
		t.Fatal("the case bundle is missing")
	}
	if _, _, err := run(t, "scenario", "generate-case", root+"request-book-reschedule-cancel.json", root+"owned/profile-siu.json", root+"owned/pack.json", "--output", destination); err == nil {
		t.Fatal("an existing generation was overwritten")
	}
	after, err := os.ReadFile(filepath.Join(destination, "generation.json"))
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("a refused repeat changed the generation")
	}

	// An event the pinned pack does not declare is listed, and nothing is written.
	pack, err := os.ReadFile(root + "owned/pack.json")
	if err != nil {
		t.Fatal(err)
	}
	without := bytes.Replace(pack, []byte(`"structure": "SIU_S13"`), []byte(`"structure": "SIU_S99"`), 1)
	request, err := os.ReadFile(root + "request-book-reschedule-cancel.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pack)
	moved := sha256.Sum256(without)
	request = bytes.Replace(request, []byte(hex.EncodeToString(sum[:])), []byte(hex.EncodeToString(moved[:])), 1)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), without, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), request, 0o600); err != nil {
		t.Fatal(err)
	}
	unsupported := filepath.Join(dir, "never")
	out, _, err = run(t, "scenario", "generate-case", filepath.Join(dir, "request.json"), root+"owned/profile-siu.json", filepath.Join(dir, "pack.json"), "--output", unsupported)
	if err == nil || !strings.Contains(out, "S13") || !strings.Contains(out, "the pack declares no SIU^S13 message in HL7 2.5.1") || !strings.Contains(out, "S12     S12  supported") {
		t.Fatalf("unsupported event: %q %v", out, err)
	}
	if _, err := os.Stat(unsupported); err == nil {
		t.Fatal("an unsupported generation wrote output")
	}
	for _, args := range [][]string{{"scenario", "generate-case"}, {"scenario", "generate-case", root + "request-siu.json", root + "owned/profile-siu.json", root + "owned/pack.json"}} {
		if _, _, err := run(t, args...); err == nil {
			t.Fatal("missing required input accepted")
		}
	}
}
