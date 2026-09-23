package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
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
// copy, through the same readers assembly verifies them with, and reports what
// it found without writing anything. It sends nothing, resets nothing and
// opens no network connection.
func (a *App) PreviewPacket(request PacketRequest) PacketPreviewResult {
	return run(a, false, false, func(context.Context) PacketPreviewResult {
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
		preview := &PacketPreview{
			BaselineSupplied:     request.Baseline != "",
			Destination:          destination,
			ExportPolicy:         "customer-local-only",
			ContainsSourceValues: true,
			Problems:             []string{},
			Limitations:          []string{},
			Inventory:            []string{},
		}
		preview.Case = packetCaseView(inputs.casePath, request.Case)
		current, currentView := packetRunView(inputs.currentPath, request.Current)
		preview.Current = currentView
		if current != nil && current.Artifact != nil && preview.Case.Found {
			preview.Case.CaseMatch = preview.Case.Identity == current.Artifact.Result.InputBundleIdentity
			if !preview.Case.CaseMatch {
				preview.Case.Problems = append(preview.Case.Problems, "the case is not the case the current result retained")
			}
		}
		preview.Spec = packetSpecView(inputs.specPath, request.Spec, current)
		preview.Inventory = packetInventory(preview, current)
		if request.Baseline != "" {
			baseline, baselineView := packetRunView(inputs.baselinePath, request.Baseline)
			preview.Baseline = baselineView
			baselineCaseName := request.BaselineCase
			if baselineCaseName == "" {
				baselineCaseName = request.Case
			}
			preview.BaselineCase = packetCaseView(inputs.baselineCasePath, baselineCaseName)
			if baseline != nil && current != nil {
				if baseline.Artifact != nil && current.Artifact != nil && baseline.Artifact.Identity == current.Artifact.Identity {
					preview.Problems = append(preview.Problems, "the baseline and the current result are the same retained execution; a baseline must be a distinct retained execution")
				}
				if baseline.Artifact != nil && preview.BaselineCase.Found {
					preview.BaselineCase.CaseMatch = preview.BaselineCase.Identity == baseline.Artifact.Result.InputBundleIdentity
					if !preview.BaselineCase.CaseMatch {
						preview.BaselineCase.Problems = append(preview.BaselineCase.Problems, "the baseline case is not the case the baseline execution retained")
					}
				}
			}
			preview.Inventory = append(preview.Inventory, packetBaselineInventory(preview, baseline)...)
			preview.Limitations = append(preview.Limitations, packetBaselineBoundaries(baseline, current)...)
		} else {
			preview.Limitations = append(preview.Limitations,
				"No observed baseline was supplied. This single-run report proves no before/after improvement or regression.")
		}
		preview.Problems = append(preview.Problems, packetInputProblems(preview.Case, preview.Spec, preview.Current)...)
		if preview.Baseline != nil {
			preview.Problems = append(preview.Problems, packetInputProblems(preview.Baseline)...)
		}
		if preview.BaselineCase != nil {
			preview.Problems = append(preview.Problems, packetInputProblems(preview.BaselineCase)...)
		}
		preview.Limitations = append(preview.Limitations,
			"Hashes establish integrity, not source authenticity, disclosure approval or a regression-equivalence claim.",
			"Assembling, exporting and rendering a report never prove a passing run or an approved disclosure.")
		return PacketPreviewResult{State: Completed, Preview: preview}
	})
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
	return runNamed[PacketResult, *PacketResult](a, packetOperation, true, false, func(ctx context.Context) PacketResult {
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

// PacketPathResult is one native folder choice for a portable review.
type PacketPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *PacketPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChoosePacketExportPath presents the host's native folder dialog for the new
// folder a portable review is sealed into. The choice is a destination only;
// choosing it exports nothing and contacts nothing.
func (a *App) ChoosePacketExportPath() PacketPathResult {
	return run(a, true, false, func(ctx context.Context) PacketPathResult {
		folder, declined := a.chooseFolder(ctx, "Choose a new folder for the portable review")
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
	return runNamed[PacketExportResult, *PacketExportResult](a, packetOperation, true, false, func(ctx context.Context) PacketExportResult {
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

// packetCaseView verifies one case entry through the shared reader.
func packetCaseView(path, entry string) *PacketInputView {
	view := &PacketInputView{Entry: entry, Problems: []string{}}
	opened, err := operation.OpenCase(path)
	if err != nil {
		view.Problems = append(view.Problems, "the case is not a case bundle this release verifies")
		return view
	}
	view.Found = true
	view.Identity = opened.Identity
	view.Provenance = string(opened.Manifest.Provenance.Mode)
	return view
}

// packetSpecView reads and decodes one specification entry and compares its
// identity with the specification the current execution retained, so the exact
// historical bytes are what assembly sees — never the current editable test.
func packetSpecView(path, entry string, current *runresult.Result) *PacketInputView {
	view := &PacketInputView{Entry: entry, Problems: []string{}}
	raw, err := readBoundedEntry(path, testrunner.MaxSpecBytes)
	if err != nil {
		view.Problems = append(view.Problems, "the specification is not a bounded regular file")
		return view
	}
	if _, err := testrunner.DecodeSpec(raw); err != nil {
		view.Problems = append(view.Problems, "the specification is not one this release assembles")
		return view
	}
	view.Found = true
	view.Identity = digestOf(raw)
	if current != nil && current.Artifact != nil {
		view.SpecMatch = view.Identity == current.Artifact.Result.SpecIdentity
		if !view.SpecMatch {
			view.Problems = append(view.Problems, "the specification is not the exact one the current result retained; assembly never substitutes the current editable test for a historical one")
		}
	}
	return view
}

// packetRunView opens one retained execution and reports its separate facts.
func packetRunView(path, entry string) (*runresult.Result, *PacketInputView) {
	view := &PacketInputView{Entry: entry, Problems: []string{}}
	retained, err := runresult.Open(path)
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		view.Problems = append(view.Problems, "the retained execution was evaluated by a version this release cannot read")
		return nil, view
	}
	if err != nil {
		view.Problems = append(view.Problems, "the entry is not a retained execution this release verifies")
		return nil, view
	}
	view.Found = true
	view.Durable = retained.Durable
	if retained.Durable {
		view.RunState = string(retained.Lifecycle.State)
		view.JournalIncomplete = retained.Lifecycle.JournalIncomplete
		view.DeliveryUncertain = retained.Lifecycle.DeliveryUncertain
	}
	if retained.Artifact == nil {
		view.Problems = append(view.Problems, "the retained execution has no finalized result; assembly refuses an incomplete job")
		return retained, view
	}
	if usable, _ := retained.Usable(); !usable {
		view.Problems = append(view.Problems, "the retained execution is incomplete or its delivery is uncertain; assembly refuses it")
	}
	artifact := retained.Artifact
	view.Status = string(artifact.Result.Status)
	view.ErrorClass = artifact.Result.ErrorClass
	view.Boundary = artifact.Result.ObservationBoundary
	view.ResultIdentity = artifact.Identity
	view.SpecIdentity = artifact.Result.SpecIdentity
	view.CaseIdentity = artifact.Result.InputBundleIdentity
	view.TargetIdentity = artifact.Result.TargetIdentity
	return retained, view
}

// packetInputProblems collects the input views' own sentences into the
// preview's one problem list.
func packetInputProblems(views ...*PacketInputView) []string {
	problems := []string{}
	for _, view := range views {
		if view == nil {
			continue
		}
		problems = append(problems, view.Problems...)
	}
	return problems
}

// packetBaselineBoundaries states what a supplied baseline does and does not
// establish, from the two runs' own verified facts.
func packetBaselineBoundaries(baseline, current *runresult.Result) []string {
	limitations := []string{}
	if baseline == nil || baseline.Artifact == nil || current == nil || current.Artifact == nil {
		return limitations
	}
	sameCase := baseline.Artifact.Result.InputBundleIdentity == current.Artifact.Result.InputBundleIdentity
	sameTarget := baseline.Artifact.Result.TargetIdentity == current.Artifact.Result.TargetIdentity
	if sameCase && sameTarget {
		limitations = append(limitations, "Baseline and current share one input and one target configuration identity; their outcomes differ only as the retained evidence records.")
	} else if sameCase {
		limitations = append(limitations, "The baseline input identity matches the current one; the target configuration identity changed between the two executions.")
	} else {
		limitations = append(limitations, "The baseline was executed against a different case identity than the current result; the comparison spans changed inputs.")
	}
	return limitations
}

// packetInventory names the sections the packet will hold, with the counts the
// verified evidence itself reports. Every evidence file is copied byte for
// byte; the assembled packet's manifest is the complete index.
func packetInventory(preview *PacketPreview, current *runresult.Result) []string {
	inventory := []string{}
	if preview.Case != nil && preview.Case.Found {
		inventory = append(inventory, "case/ — the verified case bundle, copied byte for byte")
	}
	inventory = append(inventory, "spec.json — the exact historical specification bytes")
	if current != nil && current.Artifact != nil && preview.Current != nil {
		summary := "current/ — the retained execution, byte for byte"
		summary += " (" + string(current.Artifact.Result.Status) + ", boundary " + current.Artifact.Result.ObservationBoundary
		if current.Spec != nil {
			summary += fmt.Sprintf(", %d selected messages", len(current.Spec.Input.Messages))
		}
		summary += ")"
		inventory = append(inventory, summary)
	}
	inventory = append(inventory, "SUMMARY.md and RERUN.md — regenerated outcomes, limitations and rerun instructions")
	return inventory
}

// packetBaselineInventory names the sections a supplied baseline adds.
func packetBaselineInventory(preview *PacketPreview, baseline *runresult.Result) []string {
	inventory := []string{}
	if preview.Baseline != nil && preview.Baseline.Found {
		summary := "baseline/ — the retained baseline execution, byte for byte"
		if baseline != nil && baseline.Artifact != nil {
			summary += " (" + string(baseline.Artifact.Result.Status) + ")"
		}
		inventory = append(inventory, summary)
	}
	if preview.BaselineCase != nil && preview.BaselineCase.Found {
		inventory = append(inventory, "baseline-case/ — the baseline's own source case, byte for byte")
	}
	return inventory
}
