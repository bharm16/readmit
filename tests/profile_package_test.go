package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilePackagePublicMigrationAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "package.json")
	imported := filepath.Join(dir, "imported")
	args := []string{"profile", "export", "../testdata/fixtures/local-profile.json", "--pack", "../testdata/fixtures/profile-pack.json", "--version", "../testdata/fixtures/profile-version.json", "--origin", "../testdata/fixtures/profile-origin.json", "--output", output}
	if _, _, err := run(t, args...); err == nil {
		t.Fatal("export without explicit metadata review succeeded")
	}
	args = append(args, "--reviewed")
	stdout, stderr, err := run(t, args...)
	if err != nil {
		t.Fatalf("export: %v %s", err, stderr)
	}
	if strings.Contains(stdout+stderr, "fixture-local-siu") {
		t.Fatal("output disclosed contract content")
	}
	if _, stderr, err = run(t, "profile", "import", output, "--output", imported); err != nil {
		t.Fatalf("import: %v %s", err, stderr)
	}
	again := filepath.Join(dir, "again.json")
	if _, stderr, err = run(t, "profile", "export", filepath.Join(imported, "profile.json"), "--pack", filepath.Join(imported, "pack.json"), "--version", filepath.Join(imported, "version.json"), "--origin", filepath.Join(imported, "origin.json"), "--output", again, "--reviewed"); err != nil {
		t.Fatalf("re-export: %v %s", err, stderr)
	}
	first, _ := os.ReadFile(output)
	second, _ := os.ReadFile(again)
	if !bytes.Equal(first, second) {
		t.Fatal("migration round trip changed package")
	}
	if _, _, err = run(t, "profile", "import", output, "--output", imported); err == nil {
		t.Fatal("import overwrote existing destination")
	}
	if _, _, err = run(t, args...); err == nil {
		t.Fatal("export overwrote package")
	}
}
