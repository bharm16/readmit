//go:build unix

package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/secret"
)

// credentialLocator is a locator program that prints value, outside the
// project.
func credentialLocator(t *testing.T, value string) string {
	t.Helper()
	program := filepath.Join(t.TempDir(), "locator")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s' '"+value+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return program
}

// Checking a reference and recording its rotation run its locator
// deliberately and keep nothing it printed; the answer says only that it
// resolved.
func TestACredentialCheckResolvesWithoutKeepingTheValue(t *testing.T) {
	app, context := namedProject(t)
	const value = "test-only-credential-5c1e"
	if created := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged,
		Address: "127.0.0.1:2575", Command: credentialLocator(t, value)}); created.State != desktop.Completed {
		t.Fatalf("%+v", created)
	}
	checked := app.CheckCredential(desktop.CredentialRequest{Context: context, Name: "lab-mllp"})
	answered, _ := json.Marshal(checked)
	if checked.State != desktop.Completed || !checked.Resolved || strings.Contains(string(answered), value) {
		t.Fatalf("a credential check: %s", answered)
	}
	rotated := app.RecordCredentialRotation(desktop.CredentialRequest{Context: context, Name: "lab-mllp"})
	answered, _ = json.Marshal(rotated)
	if rotated.State != desktop.Completed || len(rotated.Credentials) != 1 || rotated.Credentials[0].Generation != 2 || strings.Contains(string(answered), value) {
		t.Fatalf("a recorded rotation: %s", answered)
	}
	if missing := app.CheckCredential(desktop.CredentialRequest{Context: context, Name: "not-registered"}); missing.State != desktop.Failed || missing.Resolved {
		t.Fatalf("a check of no reference: %+v", missing)
	}
}

// A scan's review lists exactly the files it reads — the project's secrets
// entry and the files its environments and observations were saved as — and
// the scan reads those and nothing else.
func TestAReviewedScanListsItsExactFiles(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	const value = "test-only-credential-9a7d"
	if created := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "lab-mllp", Purpose: secret.MLLPEndpoint, Store: secret.CustomerManaged,
		Address: "127.0.0.1:2575", Command: credentialLocator(t, value)}); created.State != desktop.Completed {
		t.Fatalf("%+v", created)
	}
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab",
		Draft: desktop.ItemDraft{Name: "Lab", Environment: environmentDraft("127.0.0.1:2575", "nonproduction", "plain")}})
	// A file outside the configuration scope holds the value; the scan does
	// not claim to have looked there.
	writeDocument(t, root, "notes.txt", "pasted "+value+"\n")

	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ScanSecretsAction, Items: []desktop.ItemRef{}})
	if prepared.Review == nil || !prepared.Review.Ready || prepared.Review.Scan == nil ||
		!slices.Equal(prepared.Review.Scan.Files, []string{desktop.ProjectSecrets, memberEntry(t, root, environment, "target")}) {
		t.Fatalf("a scan review: %+v", prepared)
	}
	scanned := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "scan-1"})
	answered, _ := json.Marshal(scanned)
	if scanned.State != desktop.Completed || scanned.Scan == nil || !slices.Equal(scanned.Scan.Files, prepared.Review.Scan.Files) ||
		strings.Contains(string(answered), "notes.txt") || strings.Contains(string(answered), value) {
		t.Fatalf("a scan: %s", answered)
	}
}
