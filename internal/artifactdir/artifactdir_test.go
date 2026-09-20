package artifactdir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

func TestIdentityUsesDomainAndLengthDelimitedSortedFiles(t *testing.T) {
	files := map[string][]byte{
		"z.bin":           {0x00, 0xff},
		"manifest.json":   []byte("{}\n"),
		"identity.sha256": []byte("completion marker is never identity input\n"),
	}
	const want = "49ab70cd4ae408257f176dcccc1d195fbc02aca83cbc53757b497f09b6dbf52d"
	if got := artifactdir.Identity("readmit-example/v1", files); got != want {
		t.Fatalf("identity = %s, want %s", got, want)
	}
}

func TestWriteFileIsExclusiveAndReadRefusesUnexpectedOrLinkedFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.Mkdir("payloads", 0700); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "manifest.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "payloads/one.bin", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := artifactdir.WriteFile(root, "manifest.json", []byte("changed")); err == nil {
		t.Fatal("exclusive write replaced an existing file")
	}

	layout := artifactdir.Layout{
		Noun:               "example",
		AllowedDirectories: []string{"payloads"},
		RequiredFiles:      []string{"manifest.json"},
		AllowFile: func(name string) bool {
			return name == "manifest.json" || name == "payloads/one.bin"
		},
		MaxFiles:     3,
		MaxFileBytes: 8,
		MaxBytes:     16,
	}
	files, err := artifactdir.Read(dir, layout)
	if err != nil || string(files["payloads/one.bin"]) != "one" {
		t.Fatalf("read: files=%v err=%v", files, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactdir.Read(dir, layout); err == nil {
		t.Fatal("unexpected file accepted")
	}
	if err := os.Remove(filepath.Join(dir, "unexpected")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("manifest.json", filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	layout.AllowFile = func(name string) bool {
		return name == "manifest.json" || name == "payloads/one.bin" || name == "linked"
	}
	if _, err := artifactdir.Read(dir, layout); err == nil {
		t.Fatal("symbolic link accepted")
	}
}

func TestWriteCreatesIdentityLastArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	files := map[string][]byte{"manifest.json": []byte("{}\n"), "payloads/one.bin": []byte("one")}
	identity, err := artifactdir.Write(path, artifactdir.WriteOptions{Domain: "readmit-example/v1", Directories: []string{"payloads"}}, files)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(path, "identity.sha256"))
	if err != nil || string(marker) != identity+"\n" {
		t.Fatalf("marker=%q err=%v", marker, err)
	}
	if _, err := artifactdir.Write(path, artifactdir.WriteOptions{Domain: "readmit-example/v1"}, files); err == nil {
		t.Fatal("existing artifact overwritten")
	}
}
