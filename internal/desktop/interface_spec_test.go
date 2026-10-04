package desktop_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/interfacespec"
)

func TestInterfaceSpecificationPinsExactProfileRevisionAndKeepsDocumentsDescriptive(t *testing.T) {
	app, context, pack := profileProject(t)
	profile := localProfile(t)
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: desktop.ItemDraft{Name: "Owned scheduling requirements", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}, IntentID: "spec-profile-one"})
	if saved.Saved == nil {
		t.Fatal(saved)
	}
	text := "Owned partner narrative. It is not an executable requirement."
	sum := sha256.Sum256([]byte(text))
	request := desktop.InterfaceSpecSaveRequest{Context: context, IntentID: "spec-one", Name: "Owned interface Spec", Profile: *saved.Saved, Documents: []interfacespec.Documentation{{Name: "partner.md", Text: text, SHA256: hex.EncodeToString(sum[:])}}}
	spec := app.SaveInterfaceSpec(request)
	if spec.State != desktop.Completed || spec.Spec == nil || spec.Ref == nil || spec.Spec.Profile.Revision != "1" || spec.Spec.Profile.ID != profile.Identity.ID {
		t.Fatalf("specification did not pin profile: %+v", spec)
	}
	listed := app.ListInterfaceSpecs(context)
	if listed.State != desktop.Completed || len(listed.Items) != 1 || listed.Items[0].Ref != *spec.Ref || slices.Contains(listed.Items[0].Capabilities, desktop.RenameAction) || slices.Contains(listed.Items[0].Capabilities, desktop.SaveAction) {
		t.Fatalf("catalog advertised unsupported generic editor or wrong specification: %+v", listed)
	}
	source := writeCase(t, context.Project, "spec-case", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	item := cases.Page.Items[0]
	field := app.ReadInterfaceRequirements(desktop.InterfaceRequirementsRequest{Context: context, Source: item.Ref, Identity: source.Identity, Occurrence: "s0001-e000001", Selector: "MSH-9", Spec: *spec.Ref})
	if field.State != desktop.Completed || field.Applicability != "declared_match" || field.Resolution == nil || field.Profile == nil || field.Profile.Revision != "1" {
		t.Fatalf("context not exact profile: %+v", field)
	}
	caseLevel := app.ReadInterfaceRequirements(desktop.InterfaceRequirementsRequest{Context: context, Source: item.Ref, Identity: source.Identity, Spec: *spec.Ref})
	if caseLevel.State != desktop.Completed || caseLevel.Applicability != "unspecified" || caseLevel.Profile == nil || caseLevel.Resolution != nil {
		t.Fatalf("case-level scope invented a field: %+v", caseLevel)
	}
	invalid := app.ReadInterfaceRequirements(desktop.InterfaceRequirementsRequest{Context: context, Source: item.Ref, Identity: source.Identity, Occurrence: "missing", Spec: *spec.Ref})
	if invalid.State != desktop.Failed {
		t.Fatal("explicit missing occurrence was treated as case-level context")
	}

	profile.Identity.Version = "2"
	updated := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: saved.Saved.ID, BaseRevision: "1", Draft: desktop.ItemDraft{Name: "Owned scheduling requirements", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}, IntentID: "spec-profile-two"})
	if updated.Saved == nil {
		t.Fatal(updated)
	}
	held := app.ReadInterfaceSpec(desktop.ItemRequest{Context: context, Ref: *spec.Ref})
	if held.Spec == nil || held.Spec.Profile.Revision != "1" || held.Spec.Documents[0].Text != text {
		t.Fatal("profile edit silently moved specification pin")
	}
	request.Base = spec.Ref
	request.Profile = *updated.Saved
	request.IntentID = "spec-two"
	newSpec := app.SaveInterfaceSpec(request)
	if newSpec.State != desktop.Completed || newSpec.Ref.Revision != "2" || newSpec.Spec.Profile.Revision != "2" {
		t.Fatalf("explicit revision update failed: %+v", newSpec)
	}
	old := app.ReadInterfaceSpec(desktop.ItemRequest{Context: context, Ref: *spec.Ref})
	if old.Spec.Profile.Revision != "1" {
		t.Fatal("old specification revision rewritten")
	}
	conflict := app.SaveInterfaceSpec(request)
	if conflict.State != desktop.Completed {
		t.Fatalf("same-intent repeat not idempotent: %+v", conflict)
	}
	request.IntentID = "spec-stale"
	if stale := app.SaveInterfaceSpec(request); stale.State != desktop.Failed {
		t.Fatal("conflicting whole-object save published")
	}
}

func TestInterfaceSpecificationDocumentChooserCopiesBoundedUTF8WithoutPublication(t *testing.T) {
	_, request := namedProject(t)
	file := filepath.Join(t.TempDir(), "owned.md")
	content := []byte("Owned documentation. <script>inert</script>")
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
	choice := &chooser{files: []string{file}}
	app := newApp(t, choice)
	selected := app.ChooseInterfaceSpecDocument(request)
	if selected.State != desktop.Completed || selected.Document == nil || selected.Document.Text != string(content) || selected.Document.SHA256 != func() string { sum := sha256.Sum256(content); return hex.EncodeToString(sum[:]) }() {
		t.Fatalf("document copy: %+v", selected)
	}
	if len(app.ListInterfaceSpecs(request).Items) != 0 {
		t.Fatal("chooser published a specification")
	}
	for _, raw := range [][]byte{{0xff}, make([]byte, interfacespec.MaxDocumentBytes+1)} {
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if result := app.ChooseInterfaceSpecDocument(request); result.State != desktop.Failed || result.Document != nil {
			t.Fatalf("invalid UTF8 or oversized document admitted: %+v", result)
		}
	}
	choice.files = nil
	if result := app.ChooseInterfaceSpecDocument(request); result.State != desktop.Cancelled {
		t.Fatalf("cancel not explicit: %+v", result)
	}
}
