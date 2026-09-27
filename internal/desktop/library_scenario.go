package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// A scenario's preview is its generated messages, produced in memory by the
// generator exactly as a case would be generated from the same plan, and read
// through the same message reader a case is. Nothing is written and no
// receiver starts. Creating a case generates once, into the project, and
// registers the case with its synthetic provenance.

// SyntheticOrigin is the origin every generated message and case is marked
// with.
const SyntheticOrigin = "synthetic"

// ScenarioPreviewMessage is one generated message, in the order the plan's
// intended arrivals give: which row and variant stream it belongs to, the
// step and lifecycle event it was generated for and the outcome that step
// expects, its MSH-9 code and trigger, the instant it declares and its offset
// from the base time, and whether it is a retransmission.
type ScenarioPreviewMessage struct {
	Index        int    `json:"index"`
	Row          string `json:"row"`
	Variant      string `json:"variant"`
	Step         string `json:"step"`
	Event        string `json:"event"`
	Expect       string `json:"expect,omitzero"`
	MessageCode  string `json:"message_code"`
	TriggerEvent string `json:"trigger_event"`
	At           string `json:"at"`
	After        string `json:"after"`
	Duplicate    bool   `json:"duplicate"`
	Origin       string `json:"origin"`
}

// ScenarioPlanPreviewResult is one plan's generated messages. PreviewID names
// them for InspectScenarioPreview while this process holds them; the same
// plan is the same preview. Problems are why a plan cannot be generated.
type ScenarioPlanPreviewResult struct {
	State            State                    `json:"state"`
	Reason           string                   `json:"reason,omitzero"`
	Context          RequestContext           `json:"context"`
	Problems         []FieldProblem           `json:"problems"`
	PreviewID        string                   `json:"preview_id,omitzero"`
	Origin           string                   `json:"origin,omitzero"`
	Seed             uint64                   `json:"seed"`
	BaseTime         string                   `json:"base_time,omitzero"`
	GeneratorVersion string                   `json:"generator_version,omitzero"`
	Profile          string                   `json:"profile,omitzero"`
	Streams          int                      `json:"streams"`
	Messages         []ScenarioPreviewMessage `json:"messages"`
}

func (r *ScenarioPlanPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// heldPreviews is how many previews this process holds at once.
const heldPreviews = 4

// preview is one plan's generated streams, parsed, and where each listed
// message is among them.
type preview struct {
	id      string
	streams [][]byte
	parsed  []*hl7.Document
	at      [][2]int
}

// previewStore holds the latest previews, in this process only.
type previewStore struct {
	mu   sync.Mutex
	held []*preview
}

func (s *previewStore) hold(held *preview) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = slices.DeleteFunc(s.held, func(p *preview) bool { return p.id == held.id })
	s.held = append(s.held, held)
	if len(s.held) > heldPreviews {
		s.held = s.held[len(s.held)-heldPreviews:]
	}
}

func (s *previewStore) find(id string) *preview {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, held := range s.held {
		if held.id == id {
			return held
		}
	}
	return nil
}

// PreviewScenarioDraft generates a scenario draft's messages in memory, as
// the generator would generate a case from it: the same seed, base time and
// generator version are the same messages. It writes nothing, starts no
// receiver and runs no test.
func (a *App) PreviewScenarioDraft(request DraftRequest) ScenarioPlanPreviewResult {
	return run(a, true, false, func(ctx context.Context) ScenarioPlanPreviewResult {
		result := ScenarioPlanPreviewResult{Context: request.Context, Problems: []FieldProblem{}, Messages: []ScenarioPreviewMessage{}}
		if request.Kind != ScenarioItem || request.Draft.Scenario == nil {
			result.refuse(Failed, "a scenario draft is previewed")
			return result
		}
		staged, held, problems := validateScenarioDraft(request.Draft, "")
		if len(problems) > 0 {
			result.State, result.Problems = Failed, problems
			result.Reason = "the plan has problems to fix; nothing was generated"
			return result
		}
		data := staged[0].Data
		family, err := scenariogen.Produce(ctx, data)
		if err != nil {
			if ctx.Err() != nil {
				result.refuse(Cancelled, cancelledRefusal.reason)
				return result
			}
			result.refuse(Failed, err.Error())
			return result
		}
		workflow, err := scenario.DecodeDocument(held.Plan.Template)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		steps := map[string]scenario.Step{}
		for _, step := range workflow.Steps {
			steps[step.ID] = step
		}
		sum := sha256.Sum256(data)
		kept := &preview{id: hex.EncodeToString(sum[:16])}
		for i, stream := range family.Manifest.Streams {
			bytes := family.Stream(i)
			document, err := hl7.Parse(bytes, hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR})
			if err != nil || len(document.Messages) != len(stream.IntendedArrivals) {
				result.refuse(Failed, "the generated messages could not be read back")
				return result
			}
			kept.streams, kept.parsed = append(kept.streams, bytes), append(kept.parsed, document)
			for m, arrival := range stream.IntendedArrivals {
				offset, _ := time.ParseDuration(arrival.After)
				code, trigger := messageType(document, m)
				step := steps[arrival.Step]
				result.Messages = append(result.Messages, ScenarioPreviewMessage{Index: len(result.Messages), Row: stream.Row, Variant: stream.Variant,
					Step: arrival.Step, Event: string(step.Event), Expect: string(step.Expect), MessageCode: code, TriggerEvent: trigger,
					At: workflow.BaseTime.UTC().Add(offset).Format(time.RFC3339), After: arrival.After, Duplicate: arrival.Duplicate, Origin: SyntheticOrigin})
				kept.at = append(kept.at, [2]int{i, m})
			}
		}
		a.previews.hold(kept)
		result.State, result.PreviewID, result.Origin, result.Streams = Completed, kept.id, SyntheticOrigin, len(family.Manifest.Streams)
		result.Seed, result.BaseTime, result.GeneratorVersion, result.Profile = held.Plan.Seed, workflow.BaseTime.UTC().Format(time.RFC3339), held.Plan.GeneratorVersion, string(workflow.Profile)
		return result
	})
}

// ScenarioPreviewInspectRequest inspects one previewed message with the
// inspector a case occurrence is inspected with. Message is its index in the
// preview; offsets are within its generated stream.
type ScenarioPreviewInspectRequest struct {
	PreviewID  string `json:"preview_id"`
	Message    int    `json:"message"`
	Path       string `json:"path"`
	NodeOffset int    `json:"node_offset"`
	ByteOffset int    `json:"byte_offset"`
	Reveal     bool   `json:"reveal"`
}

// InspectScenarioPreview reads one message of a preview this process holds.
// No file is involved. A preview no longer held is refused, and previewing
// the plan again holds it again.
func (a *App) InspectScenarioPreview(request ScenarioPreviewInspectRequest) InspectionResult {
	return run(a, false, false, func(context.Context) InspectionResult {
		if request.NodeOffset < 0 || request.ByteOffset < -1 {
			return InspectionResult{State: Failed, Reason: "inspector offsets must be in range"}
		}
		held := a.previews.find(request.PreviewID)
		if held == nil {
			return InspectionResult{State: Failed, Reason: "this preview is no longer held; preview the scenario again"}
		}
		if request.Message < 0 || request.Message >= len(held.at) {
			return InspectionResult{State: Failed, Reason: "the selected message is not one this preview holds"}
		}
		place := held.at[request.Message]
		view, reason := inspectDocument(held.streams[place[0]], held.parsed[place[0]], place[1], inspectorWindow{
			Path: request.Path, NodeOffset: request.NodeOffset, ByteOffset: request.ByteOffset, Reveal: request.Reveal})
		if view == nil {
			return InspectionResult{State: Failed, Reason: reason}
		}
		view.Identity, view.Occurrence, view.SourceID = held.id, strconv.Itoa(request.Message), SyntheticOrigin
		return InspectionResult{State: Completed, Inspection: view}
	})
}

// ScenarioCaseRequest creates one case from a saved scenario at one revision.
// IntentID is allocated once when the person submits and reused for every
// retry of that submission.
type ScenarioCaseRequest struct {
	Context  RequestContext `json:"context"`
	Scenario ItemRef        `json:"scenario"`
	IntentID string         `json:"intent_id"`
}

// ScenarioCaseResult is the case one submission generated, or found it had
// already generated: its reference, the project entry that holds it, its
// evidence identity, its synthetic provenance and the generator inputs it
// records.
type ScenarioCaseResult struct {
	State            State          `json:"state"`
	Reason           string         `json:"reason,omitzero"`
	Context          RequestContext `json:"context"`
	Case             *ItemRef       `json:"case,omitzero"`
	Entry            string         `json:"entry,omitzero"`
	Identity         string         `json:"identity,omitzero"`
	Provenance       string         `json:"provenance,omitzero"`
	Replayed         bool           `json:"replayed"`
	Streams          int            `json:"streams"`
	Seed             uint64         `json:"seed"`
	BaseTime         string         `json:"base_time,omitzero"`
	GeneratorVersion string         `json:"generator_version,omitzero"`
	ProfileVersion   string         `json:"profile_version,omitzero"`
}

func (r *ScenarioCaseResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// maxCaseAttempts bounds how many interrupted attempts one submission's names
// step past.
const maxCaseAttempts = 16

// CreateScenarioCase generates one case from the saved plan of a scenario, at
// the revision named, into the project, and registers it with its synthetic
// provenance. The entries it writes are named from the scenario and its plan,
// never by the person: the generator makes the same case from the same plan,
// so a submission arriving again, or another submission of the same revision,
// answers the case already generated and generates nothing. An attempt an
// interruption left incomplete is retained and a retry writes beside it.
func (a *App) CreateScenarioCase(request ScenarioCaseRequest) ScenarioCaseResult {
	return run(a, true, true, func(ctx context.Context) ScenarioCaseResult {
		result := ScenarioCaseResult{Context: request.Context}
		if !catalog.ValidToken(request.IntentID) {
			result.refuse(Failed, "a case is created by one submission")
			return result
		}
		if request.Scenario.Kind != ScenarioItem {
			result.refuse(Failed, "a case is created from a saved scenario")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Scenario, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record, held := itemAt(loaded.document.Items[loaded.document.Find(item.Ref.ID)], request.Scenario.Revision)
		if !held {
			result.refuse(Failed, "the scenario has no revision "+request.Scenario.Revision)
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		if !declares(paths[scenarioRole], scenariogen.Schema) {
			result.refuse(Failed, "a case is generated from a scenario saved as a generator plan; save this scenario first")
			return result
		}
		data, err := boundedFile(paths[scenarioRole], scenariogen.MaxBytes)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		root := loaded.root
		source := ItemRef{Kind: ScenarioItem, ID: record.ID, Revision: record.RevisionLabel()}
		// The same plan generates the same case, so the entries are named from
		// the scenario and the plan: any submission of one revision answers the
		// case it already generated.
		key := sha256.Sum256(append([]byte(record.ID+"\x00"), data...))
		base := "synthetic-" + hex.EncodeToString(key[:6])
		var caseEntry, generation string
		for attempt := 1; attempt <= maxCaseAttempts; attempt++ {
			suffix := ""
			if attempt > 1 {
				suffix = "-" + strconv.Itoa(attempt)
			}
			caseEntry, generation = base+suffix+"-case", base+suffix+"-generation"
			if _, err := os.Lstat(filepath.Join(root, caseEntry)); err == nil {
				if facts, _, err := operation.VerifiedCase(root, caseEntry); err == nil {
					return a.registeredScenarioCase(ctx, request, source, item.Name, caseEntry, facts.Identity, true, result)
				}
				continue
			}
			if _, err := os.Lstat(filepath.Join(root, generation)); err == nil {
				continue
			}
			break
		}
		written, err := scenariogen.WriteCase(ctx, data, filepath.Join(root, generation), filepath.Join(root, caseEntry))
		switch {
		case ctx.Err() != nil:
			result.refuse(Cancelled, "the case was not created; anything an interrupted generation wrote is retained and a retry writes beside it")
			return result
		case errors.Is(err, fs.ErrPermission):
			result.refuse(PermissionDenied, "this account cannot write a generated case into the project")
			return result
		case err != nil:
			result.refuse(Failed, err.Error())
			return result
		}
		result.Streams, result.Seed, result.GeneratorVersion, result.ProfileVersion = written.Streams, written.Seed, written.GeneratorVersion, written.ProfileVersion
		result.BaseTime = written.BaseTime.UTC().Format(time.RFC3339)
		return a.registeredScenarioCase(ctx, request, source, item.Name, caseEntry, written.Identity, false, result)
	})
}

// registeredScenarioCase registers a generated case, unless the project
// already does, and answers it by its catalog reference.
func (a *App) registeredScenarioCase(ctx context.Context, request ScenarioCaseRequest, source ItemRef, name, entry, identity string, replayed bool, result ScenarioCaseResult) ScenarioCaseResult {
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	if !loaded.registered(entry) {
		if _, err := operation.RegisterCase(loaded.root, entry, operation.CaseRegistration{Title: name}); err != nil {
			result.refuse(Failed, "the case was generated and could not be registered: "+err.Error())
			return result
		}
	}
	loaded, declined = a.loadCatalog(ctx, request.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	index := loaded.document.ByEntry(string(CaseItem), entry)
	if index < 0 {
		result.refuse(Failed, "the generated case is not listed in the project")
		return result
	}
	if manifest, err := bundle.Describe(filepath.Join(loaded.root, entry)); err == nil && manifest.Provenance.Generator != nil {
		inputs := manifest.Provenance.Generator
		result.Seed, result.GeneratorVersion, result.ProfileVersion = inputs.Seed, inputs.GeneratorVersion, inputs.ProfileVersion
		result.BaseTime = inputs.BaseTime.UTC().Format(time.RFC3339)
	}
	// The case records the scenario revision it came from, once; a saved
	// scenario has revisions, a discovered plan has none to name.
	if revision, err := strconv.Atoi(source.Revision); err == nil {
		if _, err := loaded.store.RecordOrigin(catalog.Origin{Item: loaded.document.Items[index].ID, Kind: catalog.OriginScenario, Source: source.ID, Revision: revision}, a.now()); err != nil {
			result.refuse(Failed, "the case was generated and registered, and where it came from could not be recorded: "+err.Error())
			return result
		}
	}
	ref := loaded.read(loaded.document.Items[index]).Ref
	result.State, result.Case, result.Entry, result.Identity, result.Replayed = Completed, &ref, entry, identity, replayed
	result.Provenance = provenanceMarker(string(bundle.Generated))
	return result
}

// SampleFixtureRequest starts the built-in SIU fixture: its mode, fixed or
// defective, the loopback address it listens on, and its bounds.
type SampleFixtureRequest struct {
	Context     RequestContext `json:"context"`
	Mode        string         `json:"mode"`
	Address     string         `json:"address"`
	MaxMessages int            `json:"max_messages,omitzero"`
	IdleTimeout string         `json:"idle_timeout,omitzero"`
}

// SampleFixtureResult is one fixture session: where it listened, what it
// received, and the synthetic case and ledger it recorded into the project.
type SampleFixtureResult struct {
	State            State          `json:"state"`
	Reason           string         `json:"reason,omitzero"`
	Context          RequestContext `json:"context"`
	Phase            CapturePhase   `json:"phase,omitzero"`
	BoundAddress     string         `json:"bound_address,omitzero"`
	Case             *ItemRef       `json:"case,omitzero"`
	CaseEntry        string         `json:"case_entry,omitzero"`
	ObservationEntry string         `json:"observation_entry,omitzero"`
	Received         int            `json:"received"`
	Ledger           *FixtureLedger `json:"ledger,omitzero"`
	Origin           string         `json:"origin"`
}

func (r *SampleFixtureResult) refuse(state State, reason string) {
	r.State, r.Reason, r.Origin = state, reason, SyntheticOrigin
}

func (r *SampleFixtureResult) refuseExecution(state State, reason string) {
	r.State, r.Reason, r.Phase, r.Origin = state, reason, CaptureFailed, SyntheticOrigin
	if state == Cancelled {
		r.Phase = CaptureStopped
	}
}

// SampleFixtureMessages is the bound a fixture session takes when its request
// states none.
const SampleFixtureMessages = 100

// SyntheticTag is the tag a fixture's recorded case is registered with.
const SyntheticTag = "synthetic"

// StartSampleFixture starts the built-in SIU fixture on a loopback address,
// under the capture's own name, so the capture's Cancel stops it. Its case
// and observation ledger are named by the application, recorded into the
// project and registered as synthetic. An address that is not loopback is
// refused before anything is bound.
func (a *App) StartSampleFixture(request SampleFixtureRequest) SampleFixtureResult {
	return runNamed[SampleFixtureResult, *SampleFixtureResult](a, profiles["StartSampleFixture"], func(ctx context.Context) SampleFixtureResult {
		result := SampleFixtureResult{Context: request.Context, Origin: SyntheticOrigin}
		fail := func(reason string) SampleFixtureResult {
			result.refuse(Failed, reason)
			result.Phase = CaptureFailed
			return result
		}
		if err := sendpolicy.BindAddress(request.Address, false); err != nil {
			return fail("the SIU fixture listens only on a loopback address, such as 127.0.0.1:0")
		}
		mode := observation.Mode(request.Mode)
		if mode != observation.Fixed && mode != observation.Defective {
			return fail("the SIU fixture runs fixed or defective")
		}
		messages := request.MaxMessages
		if messages == 0 {
			messages = SampleFixtureMessages
		}
		if messages < 1 || messages > receiver.MaxMessages {
			return fail("a fixture session receives from 1 to " + strconv.Itoa(receiver.MaxMessages) + " messages")
		}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		stamp := a.now().UTC().Format("20060102T150405")
		caseEntry := ""
		for attempt := 1; ; attempt++ {
			caseEntry = "siu-fixture-" + stamp
			if attempt > 1 {
				caseEntry += "-" + strconv.Itoa(attempt)
			}
			_, caseErr := os.Lstat(filepath.Join(root, caseEntry))
			_, ledgerErr := os.Lstat(filepath.Join(root, caseEntry+"-observation.json"))
			if caseErr != nil && ledgerErr != nil {
				break
			}
		}
		observationEntry := caseEntry + "-observation.json"
		defer a.endCaptureProgress()
		capture := CaptureRequest{Address: request.Address, FixtureMode: string(mode), IdleTimeout: request.IdleTimeout, MaxMessages: messages}
		config := listenConfig(capture, filepath.Join(root, caseEntry), filepath.Join(root, observationEntry))
		config.Listening = a.reportCaptureProgress("listen")
		served, serveErr := operation.StartListen(ctx, config)
		session := a.finishCapture(ctx, CaptureSessionResult{BoundAddress: served.BoundAddress, CasePath: caseEntry, ObservationPath: observationEntry}, served.Bundle, serveErr, CaptureListening)
		result.State, result.Reason, result.Phase, result.BoundAddress, result.Ledger = session.State, session.Reason, session.Phase, session.BoundAddress, session.Ledger
		if served.Observation != nil {
			result.Received = len(served.Observation.Processed)
		}
		if served.Bundle == nil {
			return result
		}
		result.CaseEntry, result.ObservationEntry = caseEntry, observationEntry
		if _, err := operation.RegisterCase(root, caseEntry, operation.CaseRegistration{Title: "SIU fixture " + string(mode), Tags: []string{SyntheticTag}}); err != nil {
			result.Reason = cmp.Or(result.Reason, "the fixture's case was recorded and could not be registered: "+err.Error())
			return result
		}
		if loaded, _ := a.loadCatalog(context.WithoutCancel(ctx), request.Context, true); loaded != nil {
			if index := loaded.document.ByEntry(string(CaseItem), caseEntry); index >= 0 {
				// The fixture's case and the ledger beside it are recorded as
				// synthetic, whatever mode the bundle was captured in.
				if _, err := loaded.store.RecordOrigin(catalog.Origin{Item: loaded.document.Items[index].ID, Kind: catalog.OriginFixture, Mode: string(mode),
					Observation: observationEntry}, a.now()); err != nil {
					result.Reason = cmp.Or(result.Reason, "the fixture's case was registered and could not be recorded as synthetic: "+err.Error())
				}
				ref := loaded.read(loaded.document.Items[index]).Ref
				result.Case = &ref
			}
		}
		return result
	})
}
