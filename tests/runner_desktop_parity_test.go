package tests

// The runner configuration the window writes and the staged-update check it
// offers, against the command line's runner. The window builds a
// readmit-runner/v1 document through the strict reader `readmit runner`
// applies, so the command line reads exactly the bytes the window previewed
// and saved; a document either one refuses, the other refuses too. The
// window's check is customerrunner.VerifyUpdate over the same configuration,
// so it admits exactly the candidates `readmit runner verify-update` admits,
// refuses the rest with the command's own sentence, and runs none of them.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
)

// runnerForm is a complete structured configuration form whose paths are all
// inside dir. Nothing these commands do resolves the credential references.
func runnerForm(dir, updateKey, output string) desktop.RunnerConfigRequest {
	reader := filepath.Join(dir, "customer-secret-reader")
	return desktop.RunnerConfigRequest{
		Hub: "https://hub.example:8443", Project: "alpha", Environment: "lab",
		Root: filepath.Join(dir, "runs"), CA: filepath.Join(dir, "ca.pem"), Certificate: filepath.Join(dir, "client.pem"),
		Key:       desktop.RunnerReferenceInput{Command: reader, Arguments: []string{"runner-key"}},
		Token:     desktop.RunnerReferenceInput{Command: reader, Arguments: []string{"runner-token"}},
		UpdateKey: updateKey, UpdateEngine: "next-approved-build", Output: output,
	}
}

// runnerHost is a folder holding the private runner root a configuration
// names, as the administrator creates it on the runner host.
func runnerHost(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "runs"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The configuration the window previews is the one it saves, byte for byte,
// and the command line's runner reads that file as its own. A hand edit the
// runner refuses is refused by the window's reader too, and neither writes.
func TestTheRunnerConfigurationTheWindowSavesIsTheOneTheCommandLinesRunnerReads(t *testing.T) {
	dir := runnerHost(t)
	app := desktopApp(t, t.TempDir())
	form := runnerForm(dir, base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)), filepath.Join(dir, "runner.json"))

	invalid := form
	invalid.Hub = "http://hub.example:8443"
	if refused := app.PreviewRunnerConfig(invalid); refused.State != desktop.Failed || refused.Document != "" ||
		!strings.Contains(refused.Reason, "the hub must be an HTTPS origin") {
		t.Fatalf("an HTTP hub was previewed: %+v", refused)
	}
	if refused := app.SaveRunnerConfig(invalid); refused.State != desktop.Failed {
		t.Fatalf("an HTTP hub was saved: %+v", refused)
	}
	preview := app.PreviewRunnerConfig(form)
	if preview.State != desktop.Completed || preview.Document == "" {
		t.Fatalf("preview: %+v", preview)
	}
	if _, err := os.Lstat(form.Output); !os.IsNotExist(err) {
		t.Fatalf("a preview or a refused save wrote the destination: %v", err)
	}
	saved := app.SaveRunnerConfig(form)
	written := mustRead(t, form.Output)
	sum := sha256.Sum256(written)
	if saved.State != desktop.Completed || string(written) != preview.Document || saved.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the saved configuration is not the previewed one: %+v\n%s", saved, written)
	}

	status, _ := runJSON[customerrunner.Status](t, "runner", "status", "--config", form.Output)
	if status != (customerrunner.Status{Schema: "readmit-runner-status/v1", State: "idle", Jobs: 0}) {
		t.Fatalf("the command line's runner read the window's configuration as %+v", status)
	}
	if read := app.ReadRunnerConfig(form.Output); read.State != desktop.Completed || read.Health == nil || *read.Health != status {
		t.Fatalf("the window read its configuration back as %+v", read)
	}
	if again := app.SaveRunnerConfig(form); again.State != desktop.Failed || !strings.Contains(again.Reason, "never replaced") ||
		!bytes.Equal(mustRead(t, form.Output), written) {
		t.Fatalf("a second save over the configuration: %+v", again)
	}

	edits := map[string]struct {
		content []byte
		mode    os.FileMode
		reason  string
	}{
		"an HTTP hub":        {bytes.Replace(written, []byte(`"https://hub.example:8443"`), []byte(`"http://hub.example:8443"`), 1), 0600, "could not be read through its own strict reader"},
		"an unknown member":  {bytes.Replace(written, []byte(`{`), []byte(`{"extra": true, `), 1), 0600, "could not be read through its own strict reader"},
		"a file others read": {written, 0644, "must be a private regular file"},
		"a later contract":   {bytes.Replace(written, []byte(`readmit-runner/v1`), []byte(`readmit-runner/v2`), 1), 0600, "could not be read through its own strict reader"},
		"a relative CA path": {bytes.Replace(written, []byte(filepath.Join(dir, "ca.pem")), []byte("ca.pem"), 1), 0600, "could not be read through its own strict reader"},
	}
	for name, edit := range edits {
		if edit.mode&0077 != 0 && runtime.GOOS == "windows" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "edited.json")
			if err := os.WriteFile(path, edit.content, edit.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, edit.mode); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := run(t, "runner", "status", "--config", path)
			if exitCode(t, err) != 1 || stdout != "" || stderr != "readmit: "+customerrunner.ErrRefused.Error()+"\n" {
				t.Fatalf("the command line's runner did not refuse: %d %q %q", exitCode(t, err), stdout, stderr)
			}
			if read := app.ReadRunnerConfig(path); read.State != desktop.Failed || !strings.Contains(read.Reason, edit.reason) {
				t.Fatalf("the window read the refused configuration as %+v", read)
			}
			if !bytes.Equal(mustRead(t, path), edit.content) {
				t.Fatal("a refused configuration was rewritten")
			}
		})
	}
}

// signedUpdate is a readmit-runner-update/v1 manifest over claims, signed by
// key as the customer's deployment authority signs one; a nil key leaves it
// unsigned.
func signedUpdate(t *testing.T, claims customerrunner.Update, key ed25519.PrivateKey) []byte {
	t.Helper()
	claims.Signature = ""
	unsigned, err := json.Marshal(claims, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if key != nil {
		claims.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, unsigned))
	}
	manifest, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// The window's staged-update check admits the candidate the command line
// admits and refuses, in the command's own sentence, every candidate it
// refuses: an unsigned manifest, another authority's signature, another
// platform, another build, a manifest others can read and bytes changed after
// signing. The candidate is a program that marks its folder if it ever runs,
// and neither entry point runs it.
func TestTheWindowVerifiesAStagedRunnerUpdateAsTheCommandLineDoes(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, foreign, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := runnerHost(t)
	app := desktopApp(t, t.TempDir())
	form := runnerForm(dir, base64.StdEncoding.EncodeToString(public), filepath.Join(dir, "runner.json"))
	if saved := app.SaveRunnerConfig(form); saved.State != desktop.Completed {
		t.Fatalf("configuration: %+v", saved)
	}

	staged := filepath.Join(dir, "staged")
	if err := os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	candidate, ran := filepath.Join(staged, "readmit"), filepath.Join(staged, "ran")
	program := []byte("#!/bin/sh\n: > '" + ran + "'\n")
	if err := os.WriteFile(candidate, program, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(program)
	approved := customerrunner.Update{Schema: "readmit-runner-update/v1", Engine: "next-approved-build", OS: runtime.GOOS, Arch: runtime.GOARCH, SHA256: hex.EncodeToString(digest[:])}
	otherPlatform, otherBuild := approved, approved
	otherPlatform.OS = "linux"
	if runtime.GOOS == "linux" {
		otherPlatform.OS = "darwin"
	}
	otherBuild.Engine = "another-build"
	cases := []struct {
		name     string
		manifest []byte
		mode     os.FileMode
		verified bool
	}{
		{"the approved candidate", signedUpdate(t, approved, private), 0600, true},
		{"an unsigned manifest", signedUpdate(t, approved, nil), 0600, false},
		{"another authority's signature", signedUpdate(t, approved, foreign), 0600, false},
		{"another platform", signedUpdate(t, otherPlatform, private), 0600, false},
		{"a build the configuration does not approve", signedUpdate(t, otherBuild, private), 0600, false},
		{"a manifest others can read", signedUpdate(t, approved, private), 0644, false},
	}
	check := func(t *testing.T, manifest string, verified bool) {
		t.Helper()
		window := app.VerifyRunnerUpdate(form.Output, manifest, candidate)
		stdout, stderr, err := run(t, "runner", "verify-update", manifest, candidate, "--config", form.Output)
		if verified {
			if window != (desktop.RunnerUpdateResult{State: desktop.Completed, Engine: "next-approved-build"}) {
				t.Fatalf("the window refused the approved candidate: %+v", window)
			}
			if err != nil || stderr != "" || stdout != `{"schema":"readmit-runner-update-check/v1","verified":true}`+"\n" {
				t.Fatalf("the command line refused the approved candidate: %v %q %q", err, stdout, stderr)
			}
			return
		}
		if window != (desktop.RunnerUpdateResult{State: desktop.Failed, Reason: "the staged candidate is not the approved update; " + customerrunner.ErrRefused.Error()}) {
			t.Fatalf("the window did not refuse: %+v", window)
		}
		if exitCode(t, err) != 1 || stdout != "" || stderr != "readmit: "+customerrunner.ErrRefused.Error()+"\n" {
			t.Fatalf("the command line did not refuse in the window's words: %d %q %q", exitCode(t, err), stdout, stderr)
		}
	}
	for _, c := range cases {
		if c.mode&0077 != 0 && runtime.GOOS == "windows" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			manifest := filepath.Join(t.TempDir(), "update.json")
			if err := os.WriteFile(manifest, c.manifest, c.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(manifest, c.mode); err != nil {
				t.Fatal(err)
			}
			check(t, manifest, c.verified)
		})
	}

	// Bytes changed after the authority signed them are not the approved
	// update, whoever asks.
	manifest := filepath.Join(t.TempDir(), "update.json")
	if err := os.WriteFile(manifest, signedUpdate(t, approved, private), 0600); err != nil {
		t.Fatal(err)
	}
	check(t, manifest, true)
	altered := append(bytes.Clone(program), "# altered\n"...)
	if err := os.WriteFile(candidate, altered, 0700); err != nil {
		t.Fatal(err)
	}
	check(t, manifest, false)
	if !bytes.Equal(mustRead(t, candidate), altered) {
		t.Fatal("verification changed the staged candidate")
	}
	if _, err := os.Lstat(ran); !os.IsNotExist(err) {
		t.Fatalf("a staged candidate was run while it was verified: %v", err)
	}
}
