package desktop_test

// The started Import flow (#552): probing proposes a declaration only when
// every member agrees on one reading, a preview names what it read, Import
// writes one registered case per click, and pasted content is its own staged
// source, removed once imported.

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/project"
)

// sourceFile writes one file into its own folder and returns its path.
func sourceFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const secondImportHL7 = "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120500||SIU^S12|MSG002|P|2.5.1\rSCH|1\r"

func TestProbeImportSelectsOnlyUnambiguousFormats(t *testing.T) {
	app, context := namedProject(t)
	probe := func(request desktop.ImportProbeRequest) desktop.ImportProbeResult {
		t.Helper()
		request.Context = context
		result := app.ProbeImport(request)
		if result.State != desktop.Completed {
			t.Fatalf("probe: %+v", result)
		}
		return result
	}

	// One raw HL7 message is read one way: a whole plan, direction unknown,
	// that previews the file as it is.
	single := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "appointments.hl7", sampleImportHL7)}})
	if single.Selected == nil || len(single.Formats) != 1 || single.Formats[0].Plan == nil {
		t.Fatalf("a single message: %+v", single)
	}
	plan := single.Formats[0].Plan
	if plan.Framing != importer.RawFraming || plan.Terminator != hl7.CR || plan.Encoding != importer.UTF8 || plan.Direction != bundle.Unknown {
		t.Fatalf("the proposed plan: %+v", plan)
	}
	if !single.Inputs[0].Accepted || single.Inputs[0].Name != "appointments.hl7" || single.Inputs[0].Size != int64(len(sampleImportHL7)) {
		t.Fatalf("the input: %+v", single.Inputs)
	}

	// Two messages in one file divide at their headers.
	batch := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "two.hl7", sampleImportHL7+secondImportHL7)}})
	if batch.Selected == nil || batch.Formats[0].Plan.Framing != importer.BatchFraming || batch.Formats[0].Plan.BatchBoundary != importer.SegmentStart {
		t.Fatalf("two messages: %+v", batch.Formats)
	}

	// A file ending its segments two ways has no one terminator: the choice
	// is offered, not made.
	mixed := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "mixed.hl7", "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|1|P|2.5.1\rPID|1\nPV1|1\r")}})
	if mixed.Selected != nil || len(mixed.Formats) != 1 || mixed.Formats[0].Plan != nil {
		t.Fatalf("mixed terminators: %+v", mixed)
	}

	// Comma-separated lines are CSV or a text log: both offered, neither
	// chosen, and the header's names are the pickers' columns.
	csv := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "export.csv", "time,payload\n2026-01-01T12:00:00Z,x\n")}})
	if csv.Selected != nil || len(csv.Formats) != 2 || csv.Formats[0].Envelope != importer.CSVEnvelope || csv.Formats[1].Envelope != importer.TextEnvelope {
		t.Fatalf("csv: %+v", csv)
	}
	if csv.Sample == nil || csv.Sample.Fields != 2 || !slices.Equal(csv.Sample.Columns, []string{"time", "payload"}) {
		t.Fatalf("csv sample: %+v", csv.Sample)
	}

	// A JSON document is one envelope; its keys, never its values, fill the
	// mapping's tree picker.
	json := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "export.json", `{"messages":[{"payload":"MSH|secret","at":"2026"}]}`)}})
	if json.Selected == nil || json.Formats[0].Mode != "recipe" || json.Formats[0].Envelope != importer.JSONEnvelope || json.Formats[0].Plan != nil {
		t.Fatalf("json: %+v", json)
	}
	if json.Sample == nil || !slices.ContainsFunc(json.Sample.Paths, func(path []string) bool { return slices.Equal(path, []string{"messages", "payload"}) }) {
		t.Fatalf("json sample: %+v", json.Sample)
	}
	for _, path := range json.Sample.Paths {
		if slices.Contains(path, "MSH|secret") {
			t.Fatalf("a sample carried a value: %+v", json.Sample)
		}
	}

	// HL7 beside a JSON document is two readings: nothing is chosen.
	both := probe(desktop.ImportProbeRequest{Files: []string{sourceFile(t, "a.hl7", sampleImportHL7), sourceFile(t, "b.json", `{"a":1}`)}})
	if both.Selected != nil || len(both.Formats) != 2 {
		t.Fatalf("hl7 beside json: %+v", both)
	}

	// A folder's note beside its messages is excluded by the plan's suffixes.
	folder := t.TempDir()
	writeDocument(t, folder, "one.hl7", sampleImportHL7)
	writeDocument(t, folder, "README.txt", "exported for QA\n")
	withNote := probe(desktop.ImportProbeRequest{Folders: []string{folder}})
	if withNote.Selected == nil || !slices.Equal(withNote.Formats[0].Plan.Members, []string{".hl7"}) {
		t.Fatalf("a folder with a note: %+v", withNote)
	}
	for _, input := range withNote.Inputs {
		if input.Name == "README.txt" && (input.Accepted || input.Reason != importer.ReasonSuffix) {
			t.Fatalf("the note: %+v", input)
		}
	}
}

func TestProbeImportNeverInfersAnEngine(t *testing.T) {
	app, context := namedProject(t)
	export := `<list><message><messageId>1</messageId><connectorMessages><entry><int>0</int><connectorMessage>` +
		`<raw><content>MSH|^~\&amp;|A|B|C|D|20260101||ADT^A01|1|P|2.5.1</content><dataType>HL7V2</dataType></raw>` +
		`</connectorMessage></entry></connectorMessages></message></list>`
	result := app.ProbeImport(desktop.ImportProbeRequest{Context: context, Files: []string{sourceFile(t, "channel-export.xml", export)}})
	if result.State != desktop.Completed {
		t.Fatalf("probe: %+v", result)
	}
	for _, format := range result.Formats {
		if format.Mode == "engine" || format.Plan != nil {
			t.Fatalf("an engine export was inferred from its bytes: %+v", result.Formats)
		}
	}
	if len(result.Formats) != 1 || result.Formats[0].Envelope != importer.XMLEnvelope {
		t.Fatalf("an XML export is an XML envelope until declared otherwise: %+v", result.Formats)
	}
}

// previewed previews one file under the plan probing proposed.
func importPreviewed(t *testing.T, app *desktop.App, context desktop.RequestContext, source desktop.ImportRequest) desktop.ImportPreviewResult {
	t.Helper()
	source.Context = context
	preview := app.PreviewImport(source)
	if preview.State != desktop.Completed || preview.PreviewToken == "" {
		t.Fatalf("preview: %+v", preview)
	}
	return preview
}

// registeredCases are the cases the project registers now.
func registeredCases(t *testing.T, root string) []project.Case {
	t.Helper()
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return opened.Document.Cases
}

func TestImportCaseRegistersOneCaseAtomicallyAndOnlyOncePerIntent(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	plan := validDesktopPlan()
	source := desktop.ImportRequest{Mode: "plan", Plan: &plan, Files: []string{sourceFile(t, "appointments.hl7", sampleImportHL7)}}
	preview := importPreviewed(t, app, context, source)
	if len(preview.Rows) != 1 || preview.Rows[0].Type != "ADT^A01" || preview.Rows[0].Direction != "inbound" || preview.Rows[0].Time != nil ||
		preview.Rows[0].Kind != "message" || preview.Problems != (desktop.ImportProblems{}) {
		t.Fatalf("preview rows: %+v", preview)
	}
	inspected := app.InspectImportPreview(desktop.ImportInspectRequest{Context: context, Source: source, PreviewToken: preview.PreviewToken, Row: 0})
	if inspected.State != desktop.Completed || inspected.Inspection == nil || inspected.Inspection.Revealed {
		t.Fatalf("inspect the preview row: %+v", inspected)
	}
	before := entries(t, root)

	// A registration that fails leaves no case in the project: the written
	// case goes back into the project's own area, kept for the retry.
	writeDocument(t, root, project.RevisionsDocumentName, "{")
	failed := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Morning appointments", Source: source, PreviewToken: preview.PreviewToken, IntentID: "import-1"})
	if failed.State != desktop.Failed || failed.Case != nil || failed.Operation != "import-1" {
		t.Fatalf("a failed registration: %+v", failed)
	}
	if now := entries(t, root); !slices.Equal(now, append(slices.Clone(before), project.RevisionsDocumentName)) && !slices.Equal(now, before) {
		t.Fatalf("a failed registration left entries: %v, was %v", now, before)
	}
	if len(registeredCases(t, root)) != 0 {
		t.Fatal("a failed registration registered a case")
	}
	if err := os.Remove(filepath.Join(root, project.RevisionsDocumentName)); err != nil {
		t.Fatal(err)
	}

	// The retry under the same click publishes the case it wrote.
	imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Morning appointments", Source: source, PreviewToken: preview.PreviewToken, IntentID: "import-1"})
	if imported.State != desktop.Completed || imported.Case == nil || imported.Case.Kind != desktop.CaseItem || imported.Replayed {
		t.Fatalf("import: %+v", imported)
	}
	cases := registeredCases(t, root)
	if len(cases) != 1 || cases[0].Title != "Morning appointments" {
		t.Fatalf("registered: %+v", cases)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 || listed.Page.Items[0].Ref.ID != imported.Case.ID || listed.Page.Items[0].Name != "Morning appointments" {
		t.Fatalf("listed: %+v", listed)
	}

	// Its folder in the project's import area keeps only the record that
	// answers the same click again.
	incoming, err := filepath.Glob(filepath.Join(root, ".readmit", "incoming", "*", "*"))
	if err != nil || len(incoming) != 1 || filepath.Base(incoming[0]) != "intent.json" {
		t.Fatalf("the import area after the import: %v %v", incoming, err)
	}

	// The same click again answers the same case and writes nothing.
	written := entries(t, root)
	again := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Morning appointments", Source: source, PreviewToken: preview.PreviewToken, IntentID: "import-1"})
	if again.State != desktop.Completed || !again.Replayed || again.Case == nil || again.Case.ID != imported.Case.ID {
		t.Fatalf("a repeated click: %+v", again)
	}
	if now := entries(t, root); !slices.Equal(now, written) || len(registeredCases(t, root)) != 1 {
		t.Fatalf("a repeated click wrote again: %v", now)
	}
	// A different submission under that click is refused.
	if reused := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Other", Source: source, PreviewToken: preview.PreviewToken, IntentID: "import-1"}); reused.State != desktop.Failed || reused.Case != nil {
		t.Fatalf("another submission under one click: %+v", reused)
	}
}

func TestImportCaseRefusesAStalePreview(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	plan := validDesktopPlan()
	file := sourceFile(t, "appointments.hl7", sampleImportHL7)
	source := desktop.ImportRequest{Mode: "plan", Plan: &plan, Files: []string{file}}
	preview := importPreviewed(t, app, context, source)
	before := entries(t, root)

	// The file changed after it was previewed.
	if err := os.WriteFile(file, []byte(secondImportHL7), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Appointments", Source: source, PreviewToken: preview.PreviewToken, IntentID: "import-stale"})
	if stale.State != desktop.Failed || !stale.Stale || stale.Case != nil {
		t.Fatalf("a changed file: %+v", stale)
	}
	// So did the mapping: another plan is another preview.
	other := validDesktopPlan()
	other.Direction = bundle.Outbound
	changed := desktop.ImportRequest{Mode: "plan", Plan: &other, Files: []string{file}}
	if refused := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Appointments", Source: changed, PreviewToken: preview.PreviewToken, IntentID: "import-changed"}); !refused.Stale {
		t.Fatalf("a changed plan: %+v", refused)
	}
	if inspected := app.InspectImportPreview(desktop.ImportInspectRequest{Context: context, Source: source, PreviewToken: preview.PreviewToken}); inspected.State != desktop.Failed {
		t.Fatalf("a stale preview row was inspected: %+v", inspected)
	}
	if now := entries(t, root); !slices.Equal(now, before) || len(registeredCases(t, root)) != 0 {
		t.Fatalf("a stale import wrote: %v", now)
	}
}

func TestPastedMessagesAreAStagedSourceCleanedAfterImport(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	before := entries(t, root)
	staged := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7})
	if staged.State != desktop.Completed || staged.StagedID == "" || staged.Name != "Pasted messages" {
		t.Fatalf("stage: %+v", staged)
	}
	folder := filepath.Join(root, ".readmit", "staged-sources", staged.StagedID)
	if data, err := os.ReadFile(filepath.Join(folder, "Pasted messages")); err != nil || string(data) != sampleImportHL7 {
		t.Fatalf("the staged bytes: %q %v", data, err)
	}
	// Staging lists nothing in the project.
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("staging wrote project entries: %v", now)
	}

	probed := app.ProbeImport(desktop.ImportProbeRequest{Context: context, Staged: []string{staged.StagedID}})
	if probed.Selected == nil || probed.Inputs[0].Name != "Pasted messages" {
		t.Fatalf("probe: %+v", probed)
	}
	source := desktop.ImportRequest{Mode: "plan", Plan: probed.Formats[0].Plan, Staged: []string{staged.StagedID}}
	preview := importPreviewed(t, app, context, source)
	if len(preview.Rows) != 1 || preview.Rows[0].Source != "Pasted messages" || preview.Rows[0].Direction != "unknown" {
		t.Fatalf("preview: %+v", preview.Rows)
	}
	imported := app.ImportCase(desktop.ImportCaseRequest{Context: context, Name: "Pasted messages", Source: source, PreviewToken: preview.PreviewToken, IntentID: "paste-1"})
	if imported.State != desktop.Completed || imported.Case == nil {
		t.Fatalf("import: %+v", imported)
	}
	if _, err := os.Lstat(folder); !os.IsNotExist(err) {
		t.Fatalf("the staged source was kept after import: %v", err)
	}
	// Refused content is refused before anything is staged.
	if refused := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Name: "../outside", Content: "x"}); refused.State != desktop.Failed {
		t.Fatalf("a name with a slash: %+v", refused)
	}
}

func TestAMappingPresetIsPublishedOnlyBySave(t *testing.T) {
	app, context := namedProject(t)
	recipe := validDesktopRecipe()
	validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.MappingItem, Draft: desktop.ItemDraft{Name: "Scheduling export", Mapping: &recipe}})
	if validated.State != desktop.Completed || len(validated.Problems) != 0 || validated.Projection == nil || validated.Projection.Mapping == nil {
		t.Fatalf("validate: %+v", validated)
	}
	// Validating and previewing publish nothing.
	if listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.MappingItem}); listed.State != desktop.Empty {
		t.Fatalf("a mapping was published before Save: %+v", listed)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.MappingItem, Draft: desktop.ItemDraft{Name: "Scheduling export", Mapping: &recipe}, IntentID: "save-mapping"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.Revision != "1" {
		t.Fatalf("save: %+v", saved)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.MappingItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 || listed.Page.Items[0].Name != "Scheduling export" || listed.Page.Items[0].Summary.Mapping == nil ||
		listed.Page.Items[0].Summary.Mapping.Envelope != "csv" {
		t.Fatalf("listed: %+v", listed)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Mapping == nil || opened.Draft.Mapping.Name != recipe.Name {
		t.Fatalf("open: %+v", opened)
	}
	// An invalid recipe is a problem at its field, and nothing is saved.
	invalid := recipe
	invalid.Envelope = "yaml"
	if refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.MappingItem, Draft: desktop.ItemDraft{Name: "Bad", Mapping: &invalid}, IntentID: "save-bad"}); refused.Outcome != desktop.InvalidOutcome || refused.Problems[0].Field != "mapping" {
		t.Fatalf("an invalid mapping: %+v", refused)
	}
}

// Every probed row names the chosen location it belongs to — its list in the
// request and its position there — and a row inside a folder or archive names
// its member, so removing a row removes that location; a folder that holds
// nothing is still one row.
func TestEveryProbedRowNamesTheChosenLocationItBelongsTo(t *testing.T) {
	app, context := namedProject(t)
	file := sourceFile(t, "one.hl7", sampleImportHL7)
	staged := app.StagePastedContent(desktop.PastedSourceRequest{Context: context, Content: sampleImportHL7})
	if staged.State != desktop.Completed {
		t.Fatalf("stage: %+v", staged)
	}
	folder, empty := t.TempDir(), t.TempDir()
	writeDocument(t, folder, "a.hl7", sampleImportHL7)
	writeDocument(t, folder, "b.hl7", sampleImportHL7)
	archive := filepath.Join(t.TempDir(), "week.zip")
	written, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zipped := zip.NewWriter(written)
	member, err := zipped.Create("inside/c.hl7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte(sampleImportHL7)); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(zipped.Close(), written.Close()); err != nil {
		t.Fatal(err)
	}
	probed := app.ProbeImport(desktop.ImportProbeRequest{Context: context, Files: []string{file}, Staged: []string{staged.StagedID},
		Folders: []string{folder, empty}, Archives: []string{archive}})
	if probed.State != desktop.Completed {
		t.Fatalf("probe: %+v", probed)
	}
	type row struct {
		source desktop.ImportLocation
		index  int
		member string
	}
	got := []row{}
	for _, input := range probed.Inputs {
		got = append(got, row{input.Source, input.Index, input.Member})
	}
	want := []row{
		{desktop.FileLocation, 0, ""},
		{desktop.StagedLocation, 0, ""},
		{desktop.FolderLocation, 0, "a.hl7"},
		{desktop.FolderLocation, 0, "b.hl7"},
		{desktop.FolderLocation, 1, ""},
		{desktop.ArchiveLocation, 0, "inside/c.hl7"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("probed rows: %+v, want %+v", got, want)
	}
	if last := probed.Inputs[4]; last.Accepted || last.Reason != "it holds no files" {
		t.Fatalf("an empty folder's row: %+v", last)
	}
}

// The window declares an import without copying a contract version: a plan,
// a recipe or an engine plan that names none reads as the one that names the
// current version, under the same preview token.
func TestAnImportDeclarationWithoutItsSchemaReadsAsTheCurrentVersion(t *testing.T) {
	app, context := namedProject(t)
	file := sourceFile(t, "one.hl7", sampleImportHL7)
	plan := validDesktopPlan()
	unversioned := plan
	unversioned.Schema = ""
	engine := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.2", Format: engineexport.RawFormat, Terminator: hl7.CR}
	unversionedEngine := engine
	unversionedEngine.Schema = ""
	recipe := validDesktopRecipe()
	unversionedRecipe := recipe
	unversionedRecipe.Schema = ""
	csv := sourceFile(t, "data.csv", "time,payload,source,channel\n2026-01-01T12:00:00Z,\""+sampleImportHL7+"\",sys,ch\n")
	for name, pair := range map[string][2]desktop.ImportRequest{
		"plan":   {{Context: context, Mode: "plan", Plan: &plan, Files: []string{file}}, {Context: context, Mode: "plan", Plan: &unversioned, Files: []string{file}}},
		"engine": {{Context: context, Mode: "engine", EnginePlan: &engine, Files: []string{file}}, {Context: context, Mode: "engine", EnginePlan: &unversionedEngine, Files: []string{file}}},
		"recipe": {{Context: context, Mode: "recipe", Recipe: &recipe, Files: []string{csv}}, {Context: context, Mode: "recipe", Recipe: &unversionedRecipe, Files: []string{csv}}},
	} {
		versioned, stated := app.PreviewImport(pair[0]), app.PreviewImport(pair[1])
		if versioned.State != desktop.Completed || stated.State != desktop.Completed || stated.PreviewToken != versioned.PreviewToken {
			t.Errorf("%s: %s %q, without its schema %s %q", name, versioned.State, versioned.Reason, stated.State, stated.Reason)
		}
	}
	stream := unversioned
	stream.Members = []string{}
	if scanned := app.ScanCorpus(desktop.CorpusScanRequest{File: file, Plan: stream}); scanned.State != desktop.Completed {
		t.Errorf("a corpus scan without the plan's schema: %+v", scanned)
	}
}
