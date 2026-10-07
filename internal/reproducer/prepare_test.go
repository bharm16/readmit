package reproducer_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/reproducer"
)

func TestPreparedDerivationMatchesOrdinaryWriterWithoutPreparationWrites(t *testing.T) {
	sourcePath, source := capture(t, "synth-v1-regression.mllp")
	authored := plan(t, source.Identity, selectOne(source.Events[0].ID), reproducer.Step{Operator: reproducer.SetField, Occurrence: source.Events[0].ID, Selector: "MSH-10", Value: "runtime-one"})
	before, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := reproducer.Prepare(sourcePath, authored)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil || len(before) != len(after) {
		t.Fatal("pure preparation wrote output", err)
	}
	actual, err := prepared.Case()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := actual.Raw(actual.Events[0].ID)
	if err != nil || !bytes.Contains(raw, []byte("|runtime-one|")) {
		t.Fatal("declared field not derived", err)
	}
	original, err := source.Raw(source.Events[0].ID)
	if err != nil || bytes.Contains(original, []byte("|runtime-one|")) {
		t.Fatal("source was rewritten", err)
	}
	ordinary := filepath.Join(t.TempDir(), "ordinary")
	written, err := reproducer.Create(source, sourcePath, authored, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	if written.Derived.Identity != prepared.Identity() || !reflect.DeepEqual(*written, prepared.Manifest()) {
		t.Fatal("pure preparation differs from established writer")
	}
	materialized := filepath.Join(t.TempDir(), "retained")
	if err := prepared.Write(t.Context(), materialized); err != nil {
		t.Fatal(err)
	}
	reopened, err := reproducer.Open(materialized)
	if err != nil || !reflect.DeepEqual(*reopened, *written) {
		t.Fatal("prepared artifact did not reopen identically", err)
	}
	detached, err := reproducer.PrepareFiles(prepared.OriginalFiles(), authored)
	if err != nil || !reflect.DeepEqual(detached.Files(), prepared.Files()) {
		t.Fatal("offline derivation differs", err)
	}
	authored.Steps[1].Value = "caller-mutated"
	if prepared.Manifest().Plan.Steps[1].Value != "runtime-one" {
		t.Fatal("caller mutated prepared derivation plan")
	}
	actual.Manifest.Provenance.Derivation = "caller-mutated"
	fresh, err := prepared.Case()
	if err != nil || fresh.Manifest.Provenance.Derivation != reproducer.Derivation {
		t.Fatal("caller mutated prepared bundle metadata", err)
	}
	if detached.VerifyUnchanged() == nil {
		t.Fatal("detached evidence acquired live disk authority")
	}
	mutated := prepared.Files()
	mutated["case/payloads/"+actual.Events[0].ID+".bin"][0] ^= 1
	if _, err := bundle.Verify(prepared.CaseFiles()); err != nil {
		t.Fatal("caller mutated prepared case", err)
	}
	if err := prepared.VerifyUnchanged(); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(sourcePath, source.Events[0].Payload.Path)
	if err := os.WriteFile(payload, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if prepared.VerifyUnchanged() == nil {
		t.Fatal("changed original source retained execution eligibility")
	}
	if err := os.WriteFile(payload, original, 0600); err != nil {
		t.Fatal(err)
	}
	backup := sourcePath + "-original"
	if err := os.Rename(sourcePath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(sourcePath, os.DirFS(backup)); err != nil {
		t.Fatal(err)
	}
	if prepared.VerifyUnchanged() == nil {
		t.Fatal("byte-identical replacement directory retained original physical authority")
	}

}
