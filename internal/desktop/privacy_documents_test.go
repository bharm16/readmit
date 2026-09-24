package desktop_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/redact"
)

func TestDisclosureDocumentsSaveAsNewCanonicalEntriesAndReopen(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	policyRaw, err := os.ReadFile(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	inventoryRaw, err := os.ReadFile(filepath.Join(root, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	oldPolicy := app.ReadRedactPolicy(root, "policy.json")
	oldInventory := app.ReadRedactInventory(root, "inventory.json")
	if oldPolicy.State != desktop.Completed || oldPolicy.Policy == nil || oldInventory.State != desktop.Completed || oldInventory.Inventory == nil {
		t.Fatalf("existing hand-authored documents did not reopen: policy=%+v inventory=%+v", oldPolicy, oldInventory)
	}
	policy := app.SaveRedactPolicy(desktop.RedactPolicyRequest{Workspace: root, Output: "authored-policy.json", Policy: *oldPolicy.Policy})
	inventory := app.SaveRedactInventory(desktop.RedactInventoryRequest{Workspace: root, Output: "authored-inventory.json", Inventory: *oldInventory.Inventory})
	if policy.State != desktop.Completed || inventory.State != desktop.Completed {
		t.Fatalf("valid typed documents were refused: policy=%+v inventory=%+v", policy, inventory)
	}
	for entry, kind := range map[string]string{
		"authored-policy.json":    string(desktop.RedactPolicyArtifact),
		"authored-inventory.json": string(desktop.RedactInventoryArtifact),
	} {
		if got := listingKind(t, app, root, entry); got != kind {
			t.Fatalf("%s listed as %s, want %s", entry, got, kind)
		}
	}
	if got := app.ReadRedactPolicy(root, "authored-policy.json"); got.State != desktop.Completed || got.Policy == nil {
		t.Fatalf("saved policy did not reopen through redact decoder: %+v", got)
	}
	if got := app.ReadRedactInventory(root, "authored-inventory.json"); got.State != desktop.Completed || got.Inventory == nil {
		t.Fatalf("saved inventory did not reopen through redact decoder: %+v", got)
	}
	if got, err := os.ReadFile(filepath.Join(root, "policy.json")); err != nil || !bytes.Equal(got, policyRaw) {
		t.Fatal("saving an edited copy changed the original policy")
	}
	if got, err := os.ReadFile(filepath.Join(root, "inventory.json")); err != nil || !bytes.Equal(got, inventoryRaw) {
		t.Fatal("saving an edited copy changed the original inventory")
	}
	if again := app.SaveRedactPolicy(desktop.RedactPolicyRequest{Workspace: root, Output: "authored-policy.json", Policy: *oldPolicy.Policy}); again.State != desktop.Failed {
		t.Fatalf("existing policy destination was overwritten: %+v", again)
	}
}

func TestDisclosureDocumentReadersRefuseInvalidAndUnsupportedInputs(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	policy := app.ReadRedactPolicy(root, "policy.json")
	if policy.Policy == nil {
		t.Fatalf("fixture policy: %+v", policy)
	}
	policy.Policy.Fields[0].Policy = "unsupported/v1"
	if got := app.SaveRedactPolicy(desktop.RedactPolicyRequest{Workspace: root, Output: "invalid-policy.json", Policy: *policy.Policy}); got.State != desktop.Failed || got.Reason == "" {
		t.Fatalf("unsupported rule was accepted or unexplained: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "invalid-policy.json")); !os.IsNotExist(err) {
		t.Fatal("invalid policy was written")
	}
	inventory := app.ReadRedactInventory(root, "inventory.json")
	if inventory.Inventory == nil {
		t.Fatalf("fixture inventory: %+v", inventory)
	}
	inventory.Inventory.Artifacts = []redact.OriginalArtifact{{Kind: "unknown", Path: "run"}}
	if got := app.SaveRedactInventory(desktop.RedactInventoryRequest{Workspace: root, Output: "invalid-inventory.json", Inventory: *inventory.Inventory}); got.State != desktop.Failed || got.Reason == "" {
		t.Fatalf("unsupported artifact was accepted or unexplained: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "invalid-inventory.json")); !os.IsNotExist(err) {
		t.Fatal("invalid inventory was written")
	}
	writeDocument(t, root, "unsupported.json", `{"schema":"readmit-redact-policy/v2"}`)
	if got := app.ReadRedactPolicy(root, "unsupported.json"); got.State != desktop.Failed || got.Reason == "" {
		t.Fatalf("unsupported version was accepted or unexplained: %+v", got)
	}
	if got := app.ReadRedactInventory(root, "policy.json"); got.State != desktop.Failed || got.Reason == "" {
		t.Fatalf("wrong kind was accepted or unexplained: %+v", got)
	}
}

func TestHandWrittenInventoryKeepsItsOriginalDerivationRefusal(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	writeDocument(t, root, "inventory.json", `{"schema":"readmit-redact-inventory/v1","complete":true,"artifacts":[{"kind":"unknown","path":"spec.json"}],"residual_values":[]}`)
	// The existing decoder accepts the document's bounded shape; the derivation
	// still refuses its unsupported artifact when it reaches that declaration.
	read := app.ReadRedactInventory(root, "inventory.json")
	if read.State != desktop.Completed || read.Inventory == nil {
		t.Fatalf("the original inventory decoder changed its acceptance stage: %+v", read)
	}
	derived := app.DeriveExportReview(privacyRequest(root, "policy.json"))
	if derived.State != desktop.Failed || derived.Reason != "unsupported inventory artifact kind; no artifact was silently omitted" {
		t.Fatalf("hand-written inventory changed its derive refusal: %+v", derived)
	}
	// Structured authoring has the narrower obligation to refuse before a new
	// document is saved, without changing what the command line's reader does.
	authored := app.SaveRedactInventory(desktop.RedactInventoryRequest{Workspace: root, Output: "new-inventory.json", Inventory: *read.Inventory})
	if authored.State != desktop.Failed || authored.Reason != derived.Reason {
		t.Fatalf("new inventory was not refused before write: %+v", authored)
	}
}
