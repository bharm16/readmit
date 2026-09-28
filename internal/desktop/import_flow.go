package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// Import (#552): one started flow turns chosen messages into one case of the
// open project. Probing proposes a declaration and never makes one; a preview
// names exactly what it read with a token; and Import writes the case inside
// the project's own area first, then moves it into place and registers it,
// so a case is listed only once the project registers it. A click is an
// intent: the same click again answers the case it made.

// The folders of the project's own area this flow writes in. Nothing under
// them is listed as an object of the project.
const (
	incomingFolder = "incoming"
	stagedFolder   = "staged-sources"
	// pastedName is what pasted content is called when the person names it
	// nothing.
	pastedName = "Pasted messages"
	// MaxPreviewRows bounds the rows one preview answers.
	MaxPreviewRows = 1000
)

// ImportProbeRequest names the inputs to probe: chosen locations, and pasted
// sources staged in the project named by Context.
type ImportProbeRequest struct {
	Context  RequestContext `json:"context,omitzero"`
	Files    []string       `json:"files,omitzero"`
	Folders  []string       `json:"folders,omitzero"`
	Archives []string       `json:"archives,omitzero"`
	Staged   []string       `json:"staged,omitzero"`
}

// ImportProbeResult is what probing proposed. Selected is set only when one
// format fits; a plan format then carries the whole plan to preview with.
type ImportProbeResult struct {
	State    State                  `json:"state"`
	Reason   string                 `json:"reason,omitzero"`
	Context  RequestContext         `json:"context"`
	Inputs   []ImportProbeInput     `json:"inputs"`
	Formats  []importer.ProbeFormat `json:"formats"`
	Selected *int                   `json:"selected"`
	Sample   *importer.ProbeSample  `json:"sample"`
}

func (r *ImportProbeResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ImportLocation is which list of an import request a chosen location is in.
type ImportLocation string

// The lists an import request names its chosen locations in.
const (
	FileLocation    ImportLocation = "file"
	StagedLocation  ImportLocation = "staged"
	FolderLocation  ImportLocation = "folder"
	ArchiveLocation ImportLocation = "archive"
)

// ImportProbeInput is one row of a probe: a chosen file or pasted source, or
// one member of a chosen folder or archive (Member is its path there; a
// folder or archive holding none is one row without a member). Source and
// Index name the chosen location the row belongs to — its list in the
// request and its position there — so removing a row removes that location.
type ImportProbeInput struct {
	Source    ImportLocation `json:"source"`
	Index     int            `json:"index"`
	Member    string         `json:"member,omitzero"`
	Container int            `json:"container"`
	Name      string         `json:"name"`
	Kind      importer.Kind  `json:"kind"`
	Size      int64          `json:"size"`
	Accepted  bool           `json:"accepted"`
	Reason    string         `json:"reason,omitzero"`
}

// probeRows names the location each probed row belongs to. The probe numbers
// its containers files, then pasted sources (read as files), then folders,
// then archives.
func probeRows(request ImportProbeRequest, inputs []importer.ProbeInput) []ImportProbeInput {
	rows := make([]ImportProbeInput, 0, len(inputs))
	files, staged, folders := len(request.Files), len(request.Staged), len(request.Folders)
	for _, input := range inputs {
		row := ImportProbeInput{Member: input.Member, Container: input.Container, Name: input.Name, Kind: input.Kind, Size: input.Size,
			Accepted: input.Accepted, Reason: input.Reason}
		switch c := input.Container; {
		case c < files:
			row.Source, row.Index = FileLocation, c
		case c < files+staged:
			row.Source, row.Index = StagedLocation, c-files
		case c < files+staged+folders:
			row.Source, row.Index = FolderLocation, c-files-staged
		default:
			row.Source, row.Index = ArchiveLocation, c-files-staged-folders
		}
		rows = append(rows, row)
	}
	return rows
}

// ProbeImport reads at most the first 64 KiB of each chosen member and
// proposes the formats that read them all. It proposes a fully declared plan
// only when every member agrees on one reading, and never an engine export.
// It writes nothing.
func (a *App) ProbeImport(request ImportProbeRequest) ImportProbeResult {
	return runNamedRead[ImportProbeResult, *ImportProbeResult](a, profiles["ProbeImport"], func(ctx context.Context) ImportProbeResult {
		result := ImportProbeResult{Context: request.Context, Inputs: []ImportProbeInput{}, Formats: []importer.ProbeFormat{}}
		files, declined := a.stagedFiles(ctx, request.Context, "", request.Staged)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		probe, err := operation.ProbeImport(ctx, append(slices.Clone(request.Files), files...), request.Folders, request.Archives)
		if err != nil {
			if ctx.Err() != nil {
				result.refuse(Cancelled, cancelledRefusal.reason)
				return result
			}
			result.refuse(refusalState(err), err.Error())
			return result
		}
		result.State, result.Inputs, result.Formats, result.Selected, result.Sample = Completed, probeRows(request, probe.Inputs), probe.Formats, probe.Selected, probe.Sample
		return result
	})
}

// ImportPreviewRow is one parsed row of a preview: its position, the time
// and direction the declarations give it (null time and unknown direction
// when they give none), its declared message type, the source it is
// attributed to, the member it came from, and whether it parsed. Nothing in
// it is a message value.
type ImportPreviewRow struct {
	Index     int     `json:"index"`
	Time      *string `json:"time"`
	Type      string  `json:"type"`
	Source    string  `json:"source"`
	Direction string  `json:"direction"`
	Kind      string  `json:"kind"`
	Member    string  `json:"member"`
}

// ImportProblems counts what an import would not read as messages. Each
// count is present only when it is not zero.
type ImportProblems struct {
	Excluded int `json:"excluded,omitzero"`
	Unparsed int `json:"unparsed,omitzero"`
	Unmapped int `json:"unmapped,omitzero"`
}

// The kinds of a preview row.
const (
	rowMessage  = "message"
	rowUnparsed = "unparsed"
	rowUnmapped = "unmapped"
)

// importRead is one extraction of an import request and the token naming it.
type importRead struct {
	mode       string
	extraction *importer.Extraction
	engine     *operation.EngineExportPreview
	direction  string
	file       string
	token      string
	staged     []string
}

// withSchemas is request with the contract version of each declaration it
// carries filled in when it names none, so the window sends declarations
// without copying a schema literal, and one declaration reads the same with
// or without its version stated.
func withSchemas(request ImportRequest) ImportRequest {
	if request.Plan != nil && request.Plan.Schema == "" {
		plan := withPlanSchema(*request.Plan)
		request.Plan = &plan
	}
	if request.Recipe != nil && request.Recipe.Schema == "" {
		recipe := *request.Recipe
		recipe.Schema = importer.RecipeSchema
		request.Recipe = &recipe
	}
	if request.EnginePlan != nil && request.EnginePlan.Schema == "" {
		engine := *request.EnginePlan
		engine.Schema = engineexport.Schema
		request.EnginePlan = &engine
	}
	return request
}

// extractImport resolves the request's staged sources and reads every
// declared input under its declaration, writing nothing.
func (a *App) extractImport(ctx context.Context, request ImportRequest) (*importRead, refusal) {
	request = withSchemas(request)
	staged, declined := a.stagedFiles(ctx, request.Context, request.Project, request.Staged)
	if declined.state != "" {
		return nil, declined
	}
	files := append(slices.Clone(request.Files), staged...)
	read := &importRead{mode: request.Mode, staged: staged}
	failed := func(err error) (*importRead, refusal) {
		if ctx.Err() != nil {
			return nil, cancelledRefusal
		}
		return nil, refusal{refusalState(err), err.Error()}
	}
	var err error
	switch request.Mode {
	case "plan":
		if request.Plan == nil {
			return nil, refusal{Failed, "an import plan is required"}
		}
		if err := request.Plan.Validate(); err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		read.direction = string(request.Plan.Direction)
		read.extraction, err = importer.Extract(ctx, *request.Plan, files, request.Folders, request.Archives)
	case "recipe":
		if request.Recipe == nil {
			return nil, refusal{Failed, "a mapping recipe is required"}
		}
		if err := request.Recipe.Validate(); err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		read.extraction, err = importer.ExtractMapped(ctx, *request.Recipe, files, request.Folders, request.Archives)
	case "engine":
		if request.EnginePlan == nil {
			return nil, refusal{Failed, "an engine export adapter plan is required"}
		}
		if len(files) != 1 || len(request.Folders)+len(request.Archives) != 0 {
			return nil, refusal{Failed, "engine import requires exactly one input export file"}
		}
		read.file = filepath.Base(files[0])
		var preview operation.EngineExportPreview
		preview, err = operation.ImportEnginePreview(ctx, *request.EnginePlan, files[0])
		read.engine = &preview
	default:
		return nil, refusal{Failed, "unsupported import mode; choose plan, recipe, or engine"}
	}
	if err != nil {
		return failed(err)
	}
	read.token = previewToken(request, read)
	return read, refusal{}
}

// previewToken names the declarations of a request and the bytes they read.
func previewToken(request ImportRequest, read *importRead) string {
	digest := sha256.New()
	declared := request
	declared.Context, declared.Workspace, declared.Project = RequestContext{}, "", ""
	data, _ := json.Marshal(declared, json.Deterministic(true))
	digest.Write(data)
	if read.extraction != nil {
		for _, container := range read.extraction.Containers {
			digest.Write([]byte("\x00" + string(container.Kind) + "\x00" + container.SHA256))
			for _, member := range container.Members {
				digest.Write([]byte("\x00" + member.Name + "\x00" + member.SHA256))
			}
		}
	}
	if read.engine != nil {
		for _, record := range read.engine.Records {
			sum := sha256.Sum256(record.Payload)
			digest.Write([]byte("\x00" + strconv.Itoa(record.Offset) + "\x00" + record.Stage + "\x00" + hex.EncodeToString(sum[:])))
		}
	}
	return hex.EncodeToString(digest.Sum(nil))[:32]
}

// problems counts what would not be read as messages.
func (r *importRead) problems() ImportProblems {
	problems := ImportProblems{}
	if r.extraction != nil {
		problems.Excluded, problems.Unmapped = r.extraction.Totals.Excluded, r.extraction.UnmappedRecords
	}
	for _, unit := range r.units() {
		if unit.doc == nil {
			problems.Unparsed++
		}
	}
	return problems
}

// previewUnit is one extracted source parsed: the bytes, its document or nil
// when it did not parse, and what the declarations attribute to it.
type previewUnit struct {
	data      []byte
	doc       *hl7.Document
	member    string
	source    string
	direction string
	time      *string
	unmapped  bool
}

// units parses every extracted source once, in the order the case would hold
// them.
func (r *importRead) units() []previewUnit {
	units := []previewUnit{}
	if r.engine != nil {
		for _, record := range r.engine.Records {
			unit := previewUnit{data: record.Payload, member: r.file, source: record.Stage, direction: string(bundle.Unknown)}
			if doc, err := hl7.Parse(record.Payload, hl7.Options{Format: hl7.Raw, Terminator: hl7.Terminator(r.engine.Plan.Terminator)}); err == nil {
				unit.doc = doc
			}
			units = append(units, unit)
		}
		return units
	}
	members := []string{}
	for _, container := range r.extraction.Containers {
		for _, member := range container.Members {
			for range member.Records {
				members = append(members, member.Name)
			}
		}
	}
	for i, input := range r.extraction.Inputs {
		unit := previewUnit{data: input.Data, direction: r.direction}
		if i < len(members) {
			unit.member, unit.source = members[i], members[i]
		}
		if i < len(r.extraction.Mappings) {
			mapping := r.extraction.Mappings[i]
			unit.source, unit.direction, unit.unmapped = mapping.Source, string(mapping.Direction), mapping.State == importer.Unmapped
			if mapping.ObservedAt != nil {
				unit.time = stampedTime(*mapping.ObservedAt)
			}
		}
		if doc, err := hl7.Parse(input.Data, input.Options); err == nil {
			unit.doc = doc
		}
		units = append(units, unit)
	}
	return units
}

// previewRows lists the parsed rows, at most MaxPreviewRows, and how many
// there are.
func (r *importRead) previewRows() ([]ImportPreviewRow, int) {
	rows := []ImportPreviewRow{}
	total := 0
	for _, unit := range r.units() {
		row := ImportPreviewRow{Time: unit.time, Source: unit.source, Direction: unit.direction, Member: unit.member}
		if unit.doc == nil {
			row.Kind = rowUnparsed
			if unit.unmapped {
				row.Kind = rowUnmapped
			}
			row.Index = total
			if total < MaxPreviewRows {
				rows = append(rows, row)
			}
			total++
			continue
		}
		for message := range unit.doc.Messages {
			row.Index, row.Kind = total, rowMessage
			code, trigger := messageType(unit.doc, message)
			row.Type = strings.Trim(code+"^"+trigger, "^")
			if total < MaxPreviewRows {
				rows = append(rows, row)
			}
			total++
		}
	}
	return rows, total
}

// ImportInspectRequest inspects one row of a preview with the shared reader.
// Row is the row's index; the offsets are within that row's message.
type ImportInspectRequest struct {
	Context      RequestContext `json:"context,omitzero"`
	Source       ImportRequest  `json:"source"`
	PreviewToken string         `json:"preview_token"`
	Row          int            `json:"row"`
	Path         string         `json:"path"`
	NodeOffset   int            `json:"node_offset"`
	ByteOffset   int            `json:"byte_offset"`
	Reveal       bool           `json:"reveal"`
}

// InspectImportPreview reads one preview row through the same inspector a
// case occurrence and a standalone file use, values withheld until
// revealed. The inputs are read again, and a preview whose inputs or
// declarations changed is refused rather than inspected. Nothing is
// written.
func (a *App) InspectImportPreview(request ImportInspectRequest) InspectionResult {
	return runRead(a, false, func(ctx context.Context) InspectionResult {
		if request.NodeOffset < 0 || request.ByteOffset < -1 || request.Row < 0 {
			return InspectionResult{State: Failed, Reason: "inspector offsets must be in range"}
		}
		source := request.Source
		if source.Context.Project == "" {
			source.Context = request.Context
		}
		read, declined := a.extractImport(ctx, source)
		if read == nil {
			return InspectionResult{State: declined.state, Reason: declined.reason}
		}
		if read.token != request.PreviewToken {
			return InspectionResult{State: Failed, Reason: staleRefusal}
		}
		index := 0
		for _, unit := range read.units() {
			messages := 1
			if unit.doc != nil {
				messages = len(unit.doc.Messages)
			}
			if request.Row >= index+messages {
				index += messages
				continue
			}
			message := 0
			if unit.doc != nil {
				message = request.Row - index
			}
			view, reason := inspectDocument(unit.data, unit.doc, message, inspectorWindow{
				Path: request.Path, NodeOffset: request.NodeOffset, ByteOffset: request.ByteOffset, Reveal: request.Reveal,
			})
			if view == nil {
				return InspectionResult{State: Failed, Reason: reason}
			}
			view.Identity = read.token
			return InspectionResult{State: Completed, Inspection: view}
		}
		return InspectionResult{State: Failed, Reason: "the preview holds no such row"}
	})
}

// staleRefusal is what a request answered from an older preview is told.
const staleRefusal = "the sources or their format changed since this preview; preview again"

// ImportCaseRequest imports exactly one previewed selection into a new case
// of the project Context names. Name is the case's display name; the entry
// that holds its evidence is generated. PreviewToken is the preview this
// import was reviewed from, and IntentID the click that submitted it.
type ImportCaseRequest struct {
	Context      RequestContext `json:"context"`
	Name         string         `json:"name"`
	Source       ImportRequest  `json:"source"`
	PreviewToken string         `json:"preview_token"`
	IntentID     string         `json:"intent_id"`
}

// ImportCaseResult answers one import. Case is the registered case. Stale
// says the preview no longer matches its inputs; Replayed that this click
// was already answered with that case; Operation names import work kept for
// a retry under the same click.
type ImportCaseResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	Case      *ItemRef       `json:"case,omitzero"`
	Replayed  bool           `json:"replayed"`
	Stale     bool           `json:"stale,omitzero"`
	Operation string         `json:"operation,omitzero"`
}

func (r *ImportCaseResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// importIntent is what one click's import recorded in its incoming folder:
// the click, what it submitted, and the entry its case is published at.
type importIntent struct {
	Intent string `json:"intent"`
	Digest string `json:"digest"`
	Entry  string `json:"entry"`
}

const intentDocument = "intent.json"

// ImportCase writes one case from exactly the previewed inputs and
// declarations, and registers it on the project with its name, together or
// not at all. The case is written inside the project's own area, moved into
// a generated entry and registered; a registration that fails moves it back,
// so the project never lists an unregistered import. A preview that no longer
// matches is refused as stale. The same click again answers the same case,
// and a different submission under it is refused. Staged pasted sources are
// removed once the case is registered.
func (a *App) ImportCase(request ImportCaseRequest) ImportCaseResult {
	return runNamed[ImportCaseResult, *ImportCaseResult](a, profiles["ImportCase"], func(ctx context.Context) ImportCaseResult {
		result := ImportCaseResult{Context: request.Context}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		name := strings.TrimSpace(request.Name)
		if !catalog.ValidName(name) {
			result.refuse(Failed, nameRule)
			return result
		}
		if !catalog.ValidToken(request.IntentID) {
			result.refuse(Failed, "an import names the click it was submitted by")
			return result
		}
		source := withSchemas(request.Source)
		source.Context, source.Workspace, source.Project = request.Context, "", ""
		digest := submissionOf(name, source, request.PreviewToken)
		incoming := filepath.Join(root, catalog.Folder, incomingFolder, intentFolder(request.IntentID))
		if held, present := readIntent(incoming); present {
			if held.Digest != digest || held.Intent != request.IntentID {
				result.refuse(Failed, "this submission was already used for different content; nothing was imported")
				return result
			}
			return a.completeImport(ctx, result, root, incoming, held, name, source)
		}
		// The inputs are read and matched to the preview before anything is
		// written, so a refused import leaves the project as it was.
		read, declined := a.extractImport(ctx, source)
		if read == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if read.token != request.PreviewToken {
			result.refuse(Failed, staleRefusal)
			result.Stale = true
			return result
		}
		// Nothing was published under this click: whatever an interrupted
		// attempt left in its folder is its own and is written again.
		incoming, err := managedFolder(root, catalog.Folder, incomingFolder, intentFolder(request.IntentID))
		if err == nil {
			err = clearFolder(incoming)
		}
		if err != nil {
			result.refuse(Failed, "the project's import area cannot be written")
			return result
		}
		casePath, receiptPath := filepath.Join(incoming, "case"), filepath.Join(incoming, "receipt.json")
		files := append(slices.Clone(source.Files), read.staged...)
		switch source.Mode {
		case "plan":
			_, _, err = operation.ImportPlanCommit(ctx, *source.Plan, files, source.Folders, source.Archives, casePath, receiptPath)
		case "recipe":
			_, _, err = operation.ImportRecipeCommit(ctx, *source.Recipe, files, source.Folders, source.Archives, casePath, receiptPath)
		case "engine":
			_, err = operation.ImportEngineCommit(ctx, *source.EnginePlan, files[0], casePath)
		}
		if err != nil {
			if ctx.Err() != nil {
				result.refuse(Cancelled, cancelledRefusal.reason)
				return result
			}
			result.refuse(refusalState(err), err.Error())
			return result
		}
		destination, declined := caseEntryRule.destination(root, "")
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		held := importIntent{Intent: request.IntentID, Digest: digest, Entry: destination.Name}
		if err := writeIntent(incoming, held); err != nil {
			result.refuse(Failed, "the import could not be recorded in the project's import area")
			return result
		}
		return a.completeImport(ctx, result, root, incoming, held, name, source)
	})
}

// caseEntryRule names the entry a new case is published at, with its
// receipt beside it.
var caseEntryRule = outputRule{prefix: "case", beside: "-receipt.json",
	invalid:   "the case must be one new entry of the project",
	exhausted: "the project holds more generated case entries than this release proposes",
	taken:     "the case entry already exists"}

// completeImport publishes the case one click wrote, or answers the case it
// already published, and then removes the pasted sources it read.
func (a *App) completeImport(ctx context.Context, result ImportCaseResult, root, incoming string, held importIntent, name string, source ImportRequest) ImportCaseResult {
	replayed, err := publishCase(root, incoming, held.Entry, name)
	if err != nil {
		result.refuse(Failed, "the case was written but not registered, and it stays out of the project: "+err.Error())
		result.Operation = held.Intent
		return result
	}
	ref, declined := a.caseRef(ctx, result.Context, held.Entry, true)
	if ref == nil {
		result.refuse(declined.state, "the case was imported and registered, and "+declined.reason)
		return result
	}
	a.removeStaged(root, source.Staged)
	keepOnlyIntent(incoming)
	result.State, result.Case, result.Replayed = Completed, ref, replayed
	return result
}

// keepOnlyIntent removes everything a published import left in its folder
// but the intent record, which is what answers the same click again.
func keepOnlyIntent(incoming string) {
	entries, err := os.ReadDir(incoming)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() != intentDocument {
			_ = os.RemoveAll(filepath.Join(incoming, entry.Name()))
		}
	}
}

// publishCase moves a case written under folder into the project at entry,
// with its receipt beside it, and registers it with its name. A case the
// project already registers there is answered as published before. A
// registration that fails moves the case back under folder.
func publishCase(root, folder, entry, name string) (bool, error) {
	if entryRegistered(root, entry) {
		return true, nil
	}
	staged, published := filepath.Join(folder, "case"), filepath.Join(root, entry)
	receipt, besideReceipt := filepath.Join(folder, "receipt.json"), filepath.Join(root, entry+"-receipt.json")
	_, stagedErr := os.Lstat(staged)
	if _, err := os.Lstat(published); err == nil && stagedErr == nil {
		// Something else took the entry after it was chosen: the written
		// case stays where it is rather than registering what is there.
		return false, errors.New("the case's entry was taken by something else")
	} else if errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Lstat(staged); err != nil {
			return false, errors.New("the written case is no longer in the project's import area")
		}
		if err := os.Rename(staged, published); err != nil {
			return false, errors.New("the case could not be moved into the project")
		}
		if _, err := os.Lstat(receipt); err == nil {
			if _, taken := os.Lstat(besideReceipt); taken == nil {
				_ = os.Rename(published, staged)
				return false, errors.New("the case's receipt entry was taken by something else")
			}
			if err := os.Rename(receipt, besideReceipt); err != nil {
				_ = os.Rename(published, staged)
				return false, errors.New("the case's receipt could not be moved into the project")
			}
		}
	}
	if _, err := operation.RegisterCase(root, entry, operation.CaseRegistration{Title: name}); err != nil {
		_ = os.Rename(published, staged)
		if _, statErr := os.Lstat(besideReceipt); statErr == nil {
			_ = os.Rename(besideReceipt, receipt)
		}
		return false, err
	}
	return false, nil
}

// entryRegistered reports whether the project registers a case at entry.
func entryRegistered(root, entry string) bool {
	opened, declined := openProjectFolder(root)
	if opened == nil || declined.state != "" {
		return false
	}
	return slices.ContainsFunc(opened.Document.Cases, func(registered project.Case) bool { return registered.Name == entry })
}

// caseRef is the catalog's reference to the case at entry, recording the
// catalog first when record asks. Import and capture resolve entries they
// just wrote; a read resolves without recording.
func (a *App) caseRef(ctx context.Context, request RequestContext, entry string, record bool) (*ItemRef, refusal) {
	loaded, declined := a.loadCatalog(ctx, request, record)
	if loaded == nil {
		return nil, declined
	}
	index := loaded.document.ByEntry(string(CaseItem), entry)
	if index < 0 {
		// The recording path says "yet": the catalog may still record the
		// case. The read path records nothing, so it does not.
		if record {
			return nil, refusal{Failed, "the project's catalog does not list the case yet"}
		}
		return nil, refusal{Failed, "the project's catalog does not list the case"}
	}
	item := loaded.read(loaded.document.Items[index])
	return &item.Ref, refusal{}
}

// intentFolder is the folder one click writes in: a digest of the click, so
// any token names one plain folder.
func intentFolder(intent string) string {
	sum := sha256.Sum256([]byte("readmit-import-intent\x00" + intent))
	return hex.EncodeToString(sum[:])[:24]
}

var intentFile = artifactdir.Document{MaxBytes: 64 << 10}

func readIntent(folder string) (importIntent, bool) {
	data, err := intentFile.Read(filepath.Join(folder, intentDocument))
	if err != nil {
		return importIntent{}, false
	}
	var held importIntent
	if err := json.Unmarshal(data, &held); err != nil || held.Entry == "" {
		return importIntent{}, false
	}
	return held, true
}

func writeIntent(folder string, held importIntent) error {
	data, err := json.Marshal(held, json.Deterministic(true))
	if err != nil {
		return err
	}
	return intentFile.Replace(filepath.Join(folder, intentDocument), append(data, '\n'))
}

// managedFolder is the folder at parts below root, each one created when it
// is absent and otherwise required to be a real folder, never a link.
func managedFolder(root string, parts ...string) (string, error) {
	folder := root
	for _, part := range parts {
		folder = filepath.Join(folder, part)
		info, err := os.Lstat(folder)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := os.Mkdir(folder, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return "", err
			}
		case err != nil:
			return "", err
		case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
			return "", errors.New("the project's own area holds something other than a folder there")
		}
	}
	return folder, nil
}

// clearFolder removes everything a folder holds and keeps the folder.
func clearFolder(folder string) error {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(folder, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// stagePasted stages pasted content inside the project's own area under a
// generated identity, as its own declared source.
func (a *App) stagePasted(ctx context.Context, request PastedSourceRequest) PastedSourceResult {
	root, declined := a.projectRoot(ctx, request.Context)
	if root == "" {
		return PastedSourceResult{State: declined.state, Reason: declined.reason}
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = pastedName
	}
	if !catalog.ValidName(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return PastedSourceResult{State: Failed, Reason: "a pasted source is named by 1 to 200 printable characters without a slash"}
	}
	id, err := catalog.NewID()
	if err != nil {
		return PastedSourceResult{State: Failed, Reason: err.Error()}
	}
	folder, err := managedFolder(root, catalog.Folder, stagedFolder)
	if err != nil {
		return PastedSourceResult{State: Failed, Reason: "the project's staging area cannot be written"}
	}
	pruneStaged(folder, a.now())
	data := []byte(request.Content)
	if _, err := operation.StagePastedContent(filepath.Join(folder, id), name, data); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return PastedSourceResult{State: PermissionDenied, Reason: "this account cannot write staged sources into the project"}
		}
		return PastedSourceResult{State: Failed, Reason: err.Error()}
	}
	encoding := request.Encoding
	if encoding == "" {
		encoding = "utf-8"
	}
	return PastedSourceResult{State: Completed, StagedID: id, Name: name, Size: len(data), SHA256: digestOf(data), Encoding: encoding}
}

// stagedFiles resolves staged source identities to the one file each holds,
// in the project the context names, or project when the context names none.
func (a *App) stagedFiles(ctx context.Context, request RequestContext, project string, staged []string) ([]string, refusal) {
	if len(staged) == 0 {
		return nil, refusal{}
	}
	if request.Project == "" {
		request.Project = project
	}
	root, declined := a.projectRoot(ctx, request)
	if root == "" {
		return nil, declined
	}
	files := []string{}
	for _, id := range staged {
		if !stagedID(id) {
			return nil, refusal{Failed, "a staged source is named by the identity staging gave it"}
		}
		folder := filepath.Join(root, catalog.Folder, stagedFolder, id)
		entries, err := os.ReadDir(folder)
		if err != nil || len(entries) != 1 || !entries[0].Type().IsRegular() {
			return nil, refusal{Failed, "a staged source is no longer held by the project"}
		}
		files = append(files, filepath.Join(folder, entries[0].Name()))
	}
	return files, refusal{}
}

// stagedID reports an identity staging could have given.
func stagedID(id string) bool {
	if len(id) != 24 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// StagedSourceRetention is how long a pasted source no import has read yet
// stays staged. Staging another one removes those older than this: an
// import draft left that long names a source it would have to be pasted into
// again, and ImportCase then says it is no longer held.
const StagedSourceRetention = 7 * 24 * time.Hour

// pruneStaged removes the staged sources in folder staged before
// StagedSourceRetention ago, by their folder's time. Only folders staging
// named are touched, and a removal that fails is left for the next time.
func pruneStaged(folder string, now time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !stagedID(entry.Name()) {
			continue
		}
		if info, err := entry.Info(); err == nil && now.Sub(info.ModTime()) > StagedSourceRetention {
			_ = os.RemoveAll(filepath.Join(folder, entry.Name()))
		}
	}
}

// removeStaged removes the staged sources an import read once its case is
// registered.
func (a *App) removeStaged(root string, staged []string) {
	for _, id := range staged {
		if stagedID(id) {
			_ = os.RemoveAll(filepath.Join(root, catalog.Folder, stagedFolder, id))
		}
	}
}
