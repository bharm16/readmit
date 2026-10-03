package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observeinterval"
)

// ConnectedCaptureObservation explicitly selects the saved listener and the
// runtime marker selector. A business key never substitutes for this marker.
type ConnectedCaptureObservation struct {
	Source            ItemRef                       `json:"source"`
	InputKeySelector  string                        `json:"input_key_selector"`
	OutputKeySelector string                        `json:"output_key_selector"`
	RunSelector       string                        `json:"run_selector"`
	Include           []observeinterval.ScopeFilter `json:"include"`
}

func (c *loadedCatalog) connectedCaptureSource(ref ItemRef, completion observeinterval.Definition, selector, outputKey string, include []observeinterval.ScopeFilter) (observeinterval.CaptureSource, ItemRef, error) {
	invalid := errors.New("choose the exact saved plain loopback MLLP listener; remote/TLS capture combinations are not admitted by this connected editor")
	i := c.document.Find(ref.ID)
	if i < 0 || ref.Kind != SourceItem || c.removed(c.document.Items[i]) {
		return observeinterval.CaptureSource{}, ref, invalid
	}
	item := c.document.Items[i]
	if ref.Revision != "" && ref.Revision != item.RevisionLabel() {
		return observeinterval.CaptureSource{}, ref, errors.New("the selected capture source changed; save its observation and review again")
	}
	paths, availability, _ := c.backing(item)
	if availability != ItemAvailable {
		return observeinterval.CaptureSource{}, ref, invalid
	}
	source, err := readCaptureSource(paths)
	if err != nil || source.Type != MLLPListenerSource || source.Listener == nil || source.Responder == nil {
		return observeinterval.CaptureSource{}, ref, invalid
	}
	listener := source.Listener
	ip := net.ParseIP(listener.BindAddress)
	if listener.Transport != PlainTransport || ip == nil || !ip.IsLoopback() || listener.Port < 1 {
		return observeinterval.CaptureSource{}, ref, invalid
	}
	if _, err := hl7.ParseSelector(selector); err != nil {
		return observeinterval.CaptureSource{}, ref, errors.New("choose an explicit runtime marker selector")
	}
	idle, err := time.ParseDuration(listener.IdleTimeout)
	if err != nil || idle <= 0 {
		return observeinterval.CaptureSource{}, ref, invalid
	}

	maximum := listener.MessageLimit
	if maximum == 0 {
		maximum = completion.MaxRecords
	}
	maxFrame := min(64<<10, completion.MaxBytes-(16<<10))
	raw := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchemaV2, OutputKeySelector: outputKey, Address: listener.Address(), ReceiverPolicy: *source.Responder, TimeoutMS: max(completion.HorizonMS+30000, idle.Milliseconds()), MaxFrameBytes: maxFrame, MaxBytes: completion.MaxBytes, MaxMessages: min(maximum, completion.MaxRecords), MaxConnections: listener.ConnectionLimit, MaxSessions: 128, RunSelector: selector, Include: include}
	encoded, _ := encodeMember(raw)
	if _, err := observeinterval.DecodeCapture(encoded); err != nil {
		return raw, ref, err
	}
	return raw, ItemRef{Kind: SourceItem, ID: item.ID, Revision: item.RevisionLabel()}, nil
}
func validateConnectedCapture(scope draftScope, setup *ConnectedObservation) ([]catalog.Staged, string, []FieldProblem) {
	fail := func(reason string) ([]catalog.Staged, string, []FieldProblem) {
		return nil, "", []FieldProblem{{Field: "observation.connected.capture", Problem: reason}}
	}
	if scope.loaded == nil || setup.FHIR != nil || setup.Projection == nil || setup.Projection.Format != "hl7" || setup.Phase != "after" || setup.Completion.Mode != "stream" || setup.BarrierObservation != "" {
		return fail("a live HL7 capture needs an explicit after-phase stream, typed HL7 projection and full horizon")
	}
	if _, err := hl7.ParseSelector(setup.Capture.InputKeySelector); err != nil {
		return fail("choose the original input identity HL7 selector for phase matching")
	}
	if err := setup.Projection.Validate(); err != nil {
		return fail(err.Error())
	}
	source, ref, err := scope.loaded.connectedCaptureSource(setup.Capture.Source, setup.Completion, setup.Capture.RunSelector, setup.Capture.OutputKeySelector, setup.Capture.Include)
	if err != nil {
		return fail(err.Error())
	}
	setup.Capture.Source = ref
	raw, _ := encodeMember(source)
	projection, _ := encodeMember(*setup.Projection)
	return []catalog.Staged{{Role: "source", File: "source.json", Data: raw}, {Role: "projection", File: "projection.json", Data: projection}}, dataset.Digest(raw), nil
}

const connectedRuntimeReservationSchema = "readmit-connected-runtime-marker-reservation/v1"

type connectedRuntimeReservation struct {
	Schema  string `json:"schema"`
	Project string `json:"project"`
	Marker  string `json:"marker"`
	Run     string `json:"run"`
	Input   string `json:"input"`
	Plan    string `json:"plan"`
}

func needsConnectedRuntimeMarker(draft ConnectedTestDraft) bool {
	for _, step := range draft.Steps {
		if step.V2 != nil && step.V2.RuntimeMarkerSelector != "" {
			return true
		}
	}
	return false
}
func (a *App) connectedRuntimeMarker(ctx context.Context, request RequestContext, marker string, held bool, loaded *loadedCatalog) (string, exchangeRuntimeMarker, error) {
	invalid := errors.New("issue a fresh project runtime marker before reviewing this explicit derived-input capture run")
	if !held || marker == "" {
		return a.unusedRuntimeMarker(ctx, request, marker)
	}
	a.reviews.mu.Lock()
	var bound *boundAction
	for _, review := range a.reviews.reviews {
		if a.reviews.running != "" && review.consumed == a.reviews.running && !review.withdrawn && a.now().Before(review.expires) {
			bound = review.bound
			break
		}
	}
	a.reviews.mu.Unlock()
	if bound == nil {
		return a.unusedRuntimeMarker(ctx, request, marker)
	}
	if bound.run == nil || bound.run.lifecycle == nil || bound.run.lifecycle.runtimeMarker != marker || loaded == nil || bound.run.root != loaded.root || bound.run.lifecycle.compiled.flow.Project != loaded.document.Project.ID {
		return "", exchangeRuntimeMarker{}, invalid
	}
	project := loaded.document.Project.ID
	root := loaded.root
	for _, part := range []string{catalog.Folder, runtimeMarkersFolder, marker} {
		next, err := artifactpath.Child(root, part)
		if err != nil {
			return "", exchangeRuntimeMarker{}, invalid
		}
		root = next
	}
	raw, err := exchangeFile.Read(filepath.Join(root, "reservation.json"))
	var reserved connectedRuntimeReservation
	if err != nil || json.Unmarshal(raw, &reserved, json.RejectUnknownMembers(true)) != nil || reserved.Schema != connectedRuntimeReservationSchema || reserved.Project != project || reserved.Marker != marker || reserved.Run != bound.run.output || reserved.Input != bound.run.lifecycle.input || reserved.Plan != bound.run.lifecycle.compiled.plan.Identity() {
		return "", exchangeRuntimeMarker{}, invalid
	}
	return root, exchangeRuntimeMarker{Project: project, Marker: marker}, nil
}
