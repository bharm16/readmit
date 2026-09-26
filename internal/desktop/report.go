package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/report"
)

// The investigation-packet panels. This file connects what the window selects
// from actual retained evidence to the existing report operations, and adds no
// engine of its own: a preview reads the exact inputs assembly would copy and
// shows missing, mismatched and bounded ones before anything is written,
// assembly is the existing retained-packet operation into one new protected
// destination whose identity is read back from disk, export is the existing
// portable-review operation sealing the five inert offline renderings beside a
// byte-for-byte copy of the packet, and opening either artifact reopens it
// through the same verifiers the command line uses.
//
// Two rules hold across every method here. A missing baseline is stated as
// absent and never manufactured, and the exact historical specification a run
// retained is never substituted with the current editable one. And opening a
// packet or a review is a read-only mode: it acquires no send or mutation
// authority, and a successfully verified or rendered report is not a passing
// run, source authenticity, a disclosure approval or a regression-equivalence
// claim — those stay separate statements the views carry as given.

// packetOperation is the operation name the packet panels cancel through.
const packetOperation = "packet"

// packetOutputPrefix is the vocabulary assembly proposes fresh destinations
// with, so a generated name says what created it.
const packetOutputPrefix = "packet"

// PacketRequest names the actual evidence one packet assembles from. Every
// member but Output is one entry of the open workspace: the verified case, the
// exact historical specification the current result retained, the retained
// current execution — a run folder or one job inside a suite execution — and,
// never invented, an optional retained baseline execution with the case it was
// bound to when that differs. Output is the fresh workspace entry assembly
// writes into; empty asks for the next free generated name.
type PacketRequest struct {
	Workspace    string `json:"workspace"`
	Case         string `json:"case"`
	Spec         string `json:"spec"`
	Current      string `json:"current"`
	Baseline     string `json:"baseline,omitzero"`
	BaselineCase string `json:"baseline_case,omitzero"`
	Output       string `json:"output,omitzero"`
}

// PacketInputView is one named input as the preview verified it. Found is what
// a shared reader could open; Match members compare the input against what the
// current execution retained, so a historical specification that is not the
// exact one the run kept is visible before assembly instead of silently
// replaced. Problems are fixed sentences; none of them is ever resolved by
// substituting different evidence.
type PacketInputView struct {
	Entry             string   `json:"entry"`
	Found             bool     `json:"found"`
	Identity          string   `json:"identity,omitzero"`
	Provenance        string   `json:"provenance,omitzero"`
	Status            string   `json:"status,omitzero"`
	ErrorClass        string   `json:"error_class,omitzero"`
	Boundary          string   `json:"boundary,omitzero"`
	RunState          string   `json:"run_state,omitzero"`
	Durable           bool     `json:"durable,omitzero"`
	JournalIncomplete bool     `json:"journal_incomplete,omitzero"`
	DeliveryUncertain bool     `json:"delivery_uncertain,omitzero"`
	ResultIdentity    string   `json:"result_identity,omitzero"`
	SpecIdentity      string   `json:"spec_identity,omitzero"`
	CaseIdentity      string   `json:"case_identity,omitzero"`
	TargetIdentity    string   `json:"target_identity,omitzero"`
	SpecMatch         bool     `json:"spec_match,omitzero"`
	CaseMatch         bool     `json:"case_match,omitzero"`
	Problems          []string `json:"problems"`
}

// PacketPreview is what assembly would do, read from the inputs themselves.
// Limitations are the packet's own statements — observation boundary, absent
// baseline, and the separation of integrity from authenticity, disclosure
// approval and regression equivalence — carried so the window can show them
// before assembly instead of after.
type PacketPreview struct {
	Case                 *PacketInputView `json:"case,omitzero"`
	Spec                 *PacketInputView `json:"spec,omitzero"`
	Current              *PacketInputView `json:"current,omitzero"`
	Baseline             *PacketInputView `json:"baseline,omitzero"`
	BaselineCase         *PacketInputView `json:"baseline_case,omitzero"`
	BaselineSupplied     bool             `json:"baseline_supplied"`
	Destination          RunDestination   `json:"destination"`
	ExportPolicy         string           `json:"export_policy"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	Problems             []string         `json:"problems"`
	Limitations          []string         `json:"limitations"`
	Inventory            []string         `json:"inventory"`
}

// PacketPreviewResult carries one state. A preview is present whenever the
// request named a workspace, so missing and mismatched inputs are shown as
// input problems rather than hidden behind a single refusal.
type PacketPreviewResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Preview *PacketPreview `json:"preview,omitzero"`
}

func (r *PacketPreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// packetInputs resolves the entries one packet request names against an
// already-resolved workspace: the case and specification by one entry each,
// the executions through the retained-execution path (a run folder or one job
// inside a suite execution's runs), and the baseline's own case, which
// defaults to the current case. Preview and assembly resolve identically, so
// what the preview verified is what assembly copies.
type packetInputs struct {
	casePath, specPath, currentPath, baselinePath, baselineCasePath string
}

func resolvePacketInputs(root string, request PacketRequest) (packetInputs, string, bool) {
	invalid := func(reason string) (packetInputs, string, bool) { return packetInputs{}, reason, false }
	inputs := packetInputs{}
	if request.Case == "" || request.Spec == "" || request.Current == "" {
		return invalid("select the case, the exact retained specification and the current result")
	}
	var err error
	if inputs.casePath, err = runEntryPath(root, request.Case); err != nil {
		return invalid("the case must be one entry of the open workspace")
	}
	if inputs.specPath, err = artifactpath.File(root, request.Spec); err != nil {
		return invalid("the specification must be one entry of the open workspace")
	}
	if inputs.currentPath, err = runEvidencePath(root, request.Current); err != nil {
		return invalid("the current result must be one retained execution of the open workspace")
	}
	if request.Baseline != "" {
		if inputs.baselinePath, err = runEvidencePath(root, request.Baseline); err != nil {
			return invalid("the baseline result must be one retained execution of the open workspace")
		}
		name := request.BaselineCase
		if name == "" {
			name = request.Case
		}
		if inputs.baselineCasePath, err = runEntryPath(root, name); err != nil {
			return invalid("the baseline case must be one entry of the open workspace")
		}
	}
	return inputs, "", true
}

// PreviewPacket verifies the exact inputs one retained-packet assembly would
// copy through report's own assembly preview — the same readers and the same
// checks the assembly performs, and the packet layout the assembly writes —
// and reports what it found without writing anything. The facade adds only
// the entries the person selected and the destination it would create. It
// sends nothing, resets nothing and opens no network connection.
func (a *App) PreviewPacket(request PacketRequest) PacketPreviewResult {
	return run(a, false, false, func(ctx context.Context) PacketPreviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PacketPreviewResult{State: declined.state, Reason: declined.reason}
		}
		inputs, reason, ok := resolvePacketInputs(root, request)
		if !ok {
			return PacketPreviewResult{State: Failed, Reason: reason}
		}
		destination, refused := destinationFor(root, request.Output, packetOutputPrefix)
		if refused.state != "" {
			return PacketPreviewResult{State: refused.state, Reason: refused.reason}
		}
		preview, err := report.PreviewRetained(ctx, report.RetainedInput{
			Case: inputs.casePath, Spec: inputs.specPath, Current: inputs.currentPath,
			Baseline: inputs.baselinePath, BaselineCase: inputs.baselineCasePath,
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return PacketPreviewResult{State: Cancelled, Reason: cancelledRefusal.reason}
			}
			return PacketPreviewResult{State: Failed, Reason: err.Error()}
		}
		return PacketPreviewResult{State: Completed, Preview: packetPreview(request, destination, preview)}
	})
}

// packetPreview projects report's assembly preview for the shell: the entry
// name each verified input was selected by, the destination the shell would
// write, and the two statements every packet panel carries. The verified
// inputs, the problems, the planned inventory and the baseline statements are
// the report package's own; the facade adds none.
func packetPreview(request PacketRequest, destination RunDestination, preview *report.RetainedPreview) *PacketPreview {
	view := &PacketPreview{
		BaselineSupplied:     preview.BaselineSupplied,
		Destination:          destination,
		ExportPolicy:         preview.ExportPolicy,
		ContainsSourceValues: preview.ContainsSourceValues,
		Problems:             preview.Problems,
		Limitations:          preview.Limitations,
		Inventory:            preview.Inventory,
	}
	view.Case = packetInputView(request.Case, preview.Case)
	view.Spec = packetInputView(request.Spec, preview.Spec)
	view.Current = packetInputView(request.Current, preview.Current)
	if preview.Baseline != nil {
		view.Baseline = packetInputView(request.Baseline, preview.Baseline)
	}
	if preview.BaselineCase != nil {
		name := request.BaselineCase
		if name == "" {
			name = request.Case
		}
		view.BaselineCase = packetInputView(name, preview.BaselineCase)
	}
	view.Limitations = append(view.Limitations,
		"Hashes establish integrity, not source authenticity, disclosure approval or a regression-equivalence claim.",
		"Assembling, exporting and rendering a report never prove a passing run or an approved disclosure.")
	return view
}

// packetInputView names one verified input with the entry the person selected
// it by.
func packetInputView(entry string, from *report.RetainedInputView) *PacketInputView {
	if from == nil {
		return nil
	}
	return &PacketInputView{
		Entry:             entry,
		Found:             from.Found,
		Identity:          from.Identity,
		Provenance:        from.Provenance,
		Status:            from.Status,
		ErrorClass:        from.ErrorClass,
		Boundary:          from.Boundary,
		RunState:          from.RunState,
		Durable:           from.Durable,
		JournalIncomplete: from.JournalIncomplete,
		DeliveryUncertain: from.DeliveryUncertain,
		ResultIdentity:    from.ResultIdentity,
		SpecIdentity:      from.SpecIdentity,
		CaseIdentity:      from.CaseIdentity,
		TargetIdentity:    from.TargetIdentity,
		SpecMatch:         from.SpecMatch,
		CaseMatch:         from.CaseMatch,
		Problems:          from.Problems,
	}
}

// PacketRunView is one retained execution of a verified packet, as its own
// readers reevaluated it. Status is the result's verdict where one was
// finalized; RunState, the journal flags and the delivery flag are the durable
// lifecycle's separate facts. A retained execution error stays an error here
// exactly as it stays one in the packet.
type PacketRunView struct {
	Status            string `json:"status"`
	ErrorClass        string `json:"error_class,omitzero"`
	Boundary          string `json:"boundary"`
	CaseIdentity      string `json:"case_identity,omitzero"`
	CaseProvenance    string `json:"case_provenance,omitzero"`
	RunState          string `json:"run_state,omitzero"`
	JournalIncomplete bool   `json:"journal_incomplete"`
	DeliveryUncertain bool   `json:"delivery_uncertain"`
	ResultIdentity    string `json:"result_identity,omitzero"`
	SpecIdentity      string `json:"spec_identity,omitzero"`
	TargetIdentity    string `json:"target_identity,omitzero"`
}

// PacketFile is one indexed file of a sealed artifact.
type PacketFile struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

// PacketView is a verified retained packet read back from disk: the identity
// the seal records, both runs' separate facts, the complete file index and the
// packet's own limitations. It carries no message payload and no method; there
// is no operation behind it that could execute or change evidence.
type PacketView struct {
	Entry                string         `json:"entry"`
	Identity             string         `json:"identity"`
	Schema               string         `json:"schema"`
	State                string         `json:"state"`
	ExportPolicy         string         `json:"export_policy"`
	ContainsSourceValues bool           `json:"contains_source_values"`
	Current              PacketRunView  `json:"current"`
	Baseline             *PacketRunView `json:"baseline,omitzero"`
	Files                []PacketFile   `json:"files"`
	Limitations          []string       `json:"limitations"`
}

// PacketResult carries one state. Packet is present only when the operation
// verified a complete sealed packet; an assembly or verification refusal
// leaves any partial destination in place, explicitly incomplete.
type PacketResult struct {
	State  State       `json:"state"`
	Reason string      `json:"reason,omitzero"`
	Packet *PacketView `json:"packet,omitzero"`
}

func (r *PacketResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// AssemblePacket copies the selected actual evidence into one new protected
// destination through the existing retained-packet operation, then reads the
// sealed result back from disk. The exact historical specification must still
// be the one the current result retained when assembly runs; a changed or
// substituted one is refused and the partial destination is left explicitly
// incomplete. The CLI assembles free of operation admission, so this writes
// under no admission either; it sends nothing and contacts nothing.
func (a *App) AssemblePacket(request PacketRequest) PacketResult {
	return runNamed[PacketResult, *PacketResult](a, profiles["AssemblePacket"], func(ctx context.Context) PacketResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PacketResult{State: declined.state, Reason: declined.reason}
		}
		inputs, reason, ok := resolvePacketInputs(root, request)
		if !ok {
			return PacketResult{State: Failed, Reason: reason}
		}
		destination, refused := destinationFor(root, request.Output, packetOutputPrefix)
		if refused.state != "" {
			return PacketResult{State: refused.state, Reason: refused.reason}
		}
		packet, err := report.Assemble(ctx, report.RetainedInput{
			Case: inputs.casePath, Spec: inputs.specPath, Current: inputs.currentPath,
			Baseline: inputs.baselinePath, BaselineCase: inputs.baselineCasePath,
		}, filepath.Join(root, destination.Name))
		if err != nil {
			state, reason := packetRefusal(err)
			return PacketResult{State: state, Reason: reason}
		}
		return PacketResult{State: Completed, Packet: packetView(destination.Name, packet)}
	})
}

// OpenPacket verifies one sealed retained packet of the open workspace,
// read-only. It is the same offline verification `readmit report
// verify-retained` performs: no historical path is resolved, no endpoint is
// contacted, nothing is executed, and no admission of any kind is acquired.
func (a *App) OpenPacket(workspace, entry string) PacketResult {
	return run(a, false, false, func(ctx context.Context) PacketResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return PacketResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEvidencePath(root, entry)
		if err != nil {
			return PacketResult{State: Failed, Reason: "a retained packet is named by one entry of the open workspace"}
		}
		packet, err := report.OpenRetained(ctx, path)
		if err != nil {
			state, reason := packetRefusal(err)
			return PacketResult{State: state, Reason: reason}
		}
		return PacketResult{State: Completed, Packet: packetView(entry, packet)}
	})
}

// packetView projects a verified packet for the shell, deriving the packet's
// own limitation statements from the manifest facts the verification just
// reestablished.
func packetView(entry string, packet *report.RetainedPacket) *PacketView {
	view := &PacketView{
		Entry:                entry,
		Identity:             packet.Identity,
		Schema:               packet.Manifest.Schema,
		State:                packet.Manifest.State,
		ExportPolicy:         packet.Manifest.ExportPolicy,
		ContainsSourceValues: packet.Manifest.ContainsSourceValues,
		Files:                []PacketFile{},
		Limitations:          []string{},
	}
	view.Current = packetRunStatus(packet.Manifest.Current)
	if packet.Manifest.Baseline != nil {
		baseline := packetRunStatus(*packet.Manifest.Baseline)
		view.Baseline = &baseline
	} else {
		view.Limitations = append(view.Limitations,
			"No observed baseline was supplied. This single-run report proves no before/after improvement or regression.")
	}
	for _, file := range packet.Manifest.Files {
		view.Files = append(view.Files, PacketFile{Path: file.Path, Size: file.Size, SHA256: file.SHA256})
	}
	view.Limitations = append(view.Limitations,
		"Customer-local only: contains original source values and historical paths/configuration. No disclosure approval or de-identification is implied.",
		"Hashes establish integrity, not source authenticity, disclosure approval or a regression-equivalence claim.")
	return view
}

func packetRunStatus(run report.RetainedRun) PacketRunView {
	return PacketRunView{
		Status:            run.Status,
		ErrorClass:        run.ErrorClass,
		Boundary:          run.Boundary,
		CaseIdentity:      run.CaseIdentity,
		CaseProvenance:    run.CaseProvenance,
		RunState:          run.RunState,
		JournalIncomplete: run.JournalIncomplete,
		DeliveryUncertain: run.DeliveryUncertain,
		ResultIdentity:    run.Identity,
		SpecIdentity:      run.SpecIdentity,
		TargetIdentity:    run.TargetIdentity,
	}
}

// packetRefusal maps the report operations' own errors to one state and one
// sentence. The report package's errors are operator-facing sentences that
// disclose no path and already say what an incomplete output means, so they
// carry unchanged; cancellation is named as itself.
func packetRefusal(err error) (State, string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled, "the packet operation was cancelled; any partial destination remains incomplete and cannot be verified as complete"
	}
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return Failed, "the evidence was evaluated by a version this release cannot read; it has not been changed"
	}
	return Failed, err.Error()
}

// PacketPathResult is one new folder named natively for a portable review.
type PacketPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *PacketPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChoosePacketExportPath presents the host's native save dialog to name the
// new folder a portable review is sealed into. The choice is a destination
// only; naming it creates, exports and contacts nothing.
func (a *App) ChoosePacketExportPath() PacketPathResult {
	return run(a, true, false, func(ctx context.Context) PacketPathResult {
		folder, declined := a.chooseDestination(ctx, "Choose a new folder for the portable review")
		if folder == "" {
			return PacketPathResult{State: declined.state, Reason: declined.reason}
		}
		return PacketPathResult{State: Completed, Path: folder}
	})
}

// PacketExportRequest names one verified packet of the open workspace and the
// new destination folder, chosen natively, the portable review is sealed into.
type PacketExportRequest struct {
	Workspace   string `json:"workspace"`
	Packet      string `json:"packet"`
	Destination string `json:"destination"`
}

// PacketExportResult carries one state. Formats names the five inert offline
// renderings the existing export operation writes; the review they sit in
// inherits the packet's sensitivity, and sealing it grants no disclosure
// approval and performs no upload.
type PacketExportResult struct {
	State                State    `json:"state"`
	Reason               string   `json:"reason,omitzero"`
	Review               string   `json:"review,omitzero"`
	Identity             string   `json:"identity,omitzero"`
	PacketIdentity       string   `json:"packet_identity,omitzero"`
	Formats              []string `json:"formats,omitzero"`
	ExportPolicy         string   `json:"export_policy,omitzero"`
	ContainsSourceValues bool     `json:"contains_source_values,omitzero"`
}

func (r *PacketExportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExportPacketReview seals the existing portable review — the complete packet
// copied byte for byte beside the five locally rendered offline reports — into
// the chosen new destination through the existing export operation. The
// command line exports free of operation admission, so this writes under none
// either. A cancelled or failed export leaves the destination explicitly
// incomplete; nothing is transmitted and no original evidence is changed.
func (a *App) ExportPacketReview(request PacketExportRequest) PacketExportResult {
	return runNamed[PacketExportResult, *PacketExportResult](a, profiles["ExportPacketReview"], func(ctx context.Context) PacketExportResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PacketExportResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEvidencePath(root, request.Packet)
		if err != nil {
			return PacketExportResult{State: Failed, Reason: "a retained packet is named by one entry of the open workspace"}
		}
		if request.Destination == "" {
			return PacketExportResult{State: Failed, Reason: "choose a new folder for the portable review first"}
		}
		review, err := report.ExportReview(ctx, path, request.Destination)
		if err != nil {
			state, reason := packetRefusal(err)
			return PacketExportResult{State: state, Reason: reason}
		}
		return PacketExportResult{
			State:                Completed,
			Review:               filepath.Base(request.Destination),
			Identity:             review.Identity,
			PacketIdentity:       review.Manifest.PacketIdentity,
			Formats:              []string{"offline HTML", "PDF", "Markdown", "strict JSON", "JUnit"},
			ExportPolicy:         review.Manifest.ExportPolicy,
			ContainsSourceValues: review.Manifest.ContainsSourceValues,
		}
	})
}

// PacketReviewRequest opens one portable review of the open workspace,
// read-only. Reveal is the deliberate local reveal of the canonical report
// text, which carries the actual expected and observed content; without it the
// text stays behind and the view says so.
type PacketReviewRequest struct {
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	Reveal    bool   `json:"reveal"`
}

// PacketReviewView is a verified portable review. It exposes verification
// metadata and, under the deliberate reveal, the canonical report lines the
// five renderings carry. There is no operation behind this view that could
// execute, send, reset or modify anything: opening a review acquires no
// authority at all.
type PacketReviewView struct {
	Entry                string       `json:"entry"`
	Identity             string       `json:"identity"`
	Schema               string       `json:"schema"`
	State                string       `json:"state"`
	PacketIdentity       string       `json:"packet_identity"`
	ExportPolicy         string       `json:"export_policy"`
	ContainsSourceValues bool         `json:"contains_source_values"`
	Renderings           []PacketFile `json:"renderings"`
	Files                int          `json:"files"`
	Current              string       `json:"current"`
	Baseline             string       `json:"baseline,omitzero"`
	VersionRequirements  []string     `json:"version_requirements"`
	Lines                []string     `json:"lines"`
	Revealed             bool         `json:"revealed"`
}

// PacketReviewResult carries one state.
type PacketReviewResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	Review *PacketReviewView `json:"review,omitzero"`
}

func (r *PacketReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenPacketReview verifies one portable review of the open workspace through
// the same read-only verifier `readmit report review` runs, which re-derives
// every rendering from the sealed packet and refuses altered or invented
// content even when hashes are recomputed. It writes nothing, resolves no
// historical path and acquires no admission: a reviewer without any operation
// policy can open a review, because reading never grants authority.
func (a *App) OpenPacketReview(request PacketReviewRequest) PacketReviewResult {
	return run(a, false, false, func(ctx context.Context) PacketReviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PacketReviewResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEvidencePath(root, request.Entry)
		if err != nil {
			return PacketReviewResult{State: Failed, Reason: "a portable review is named by one entry of the open workspace"}
		}
		review, err := report.OpenReview(ctx, path)
		if err != nil {
			state, reason := packetRefusal(err)
			return PacketReviewResult{State: state, Reason: reason}
		}
		view := &PacketReviewView{
			Entry:                request.Entry,
			Identity:             review.Identity,
			Schema:               review.Manifest.Schema,
			State:                review.Manifest.State,
			PacketIdentity:       review.Manifest.PacketIdentity,
			ExportPolicy:         review.Manifest.ExportPolicy,
			ContainsSourceValues: review.Manifest.ContainsSourceValues,
			Renderings:           []PacketFile{},
			Files:                len(review.Manifest.Files),
			Current:              "unknown",
			VersionRequirements: []string{
				report.ReviewSchema + " (review manifest)",
				report.ReportSchema + " (strict JSON report)",
				report.RetainedSchema + " (sealed packet)",
			},
			Lines:    []string{},
			Revealed: request.Reveal,
		}
		for _, file := range review.Manifest.Files {
			switch file.Path {
			case "report.html", "report.pdf", "report.md", "report.json", "junit.xml":
				view.Renderings = append(view.Renderings, PacketFile{Path: file.Path, Size: file.Size, SHA256: file.SHA256})
			}
		}
		// The run statuses are the sealed packet's own manifest facts, read
		// back through the packet's verifier. They are labels, not verdicts
		// about anyone's production system.
		if packet, err := report.OpenRetained(ctx, filepath.Join(path, "packet")); err == nil {
			view.Current = packet.Manifest.Current.Status
			if packet.Manifest.Baseline != nil {
				view.Baseline = packet.Manifest.Baseline.Status
			}
		}
		if request.Reveal {
			rendered, err := review.Render("json")
			if err != nil {
				return PacketReviewResult{State: Failed, Reason: "the portable review could not be rendered"}
			}
			var document struct {
				Lines []string `json:"lines"`
			}
			if json.Unmarshal(rendered, &document) != nil {
				return PacketReviewResult{State: Failed, Reason: "the portable report could not be read back"}
			}
			view.Lines = document.Lines
		}
		return PacketReviewResult{State: Completed, Review: view}
	})
}
