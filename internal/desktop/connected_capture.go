package desktop

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
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
	invalid := errors.New("Choose an approved saved MLLP listener with a fixed port and valid transport settings.")
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
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || !ip.IsLoopback() && !listener.AllowRemote || listener.Port < 1 {
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
	raw := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchemaV2, OutputKeySelector: outputKey, Address: listener.Address(), ReceiverPolicy: *source.Responder, TimeoutMS: max(completion.HorizonMS+30000, idle.Milliseconds()), MaxFrameBytes: maxFrame, MaxBytes: completion.MaxBytes, MaxMessages: min(maximum, completion.MaxRecords), MaxConnections: max(1, listener.ConnectionLimit), MaxSessions: 128, RunSelector: selector, Include: include}
	if listener.Transport != PlainTransport || !ip.IsLoopback() {
		raw.Schema = observeinterval.CaptureSourceSchemaV3
		for _, role := range []string{"listener", "responder"} {
			data, err := savedFile.Read(paths[role])
			if err != nil {
				return raw, ref, err
			}
			raw.Inputs = append(raw.Inputs, observeinterval.CaptureInput{Role: role, Path: paths[role], SHA256: dataset.Digest(data)})
		}
	}
	if listener.Transport != PlainTransport {
		// Read public material and the registered locator only. The shared
		// executor resolves the private key after separate capture admission.
		read := func(name, role string) ([]byte, error) {
			path, err := artifactpath.File(c.root, name)
			if err != nil {
				return nil, err
			}
			data, err := savedFile.Read(path)
			if err == nil && !captureCertificates(data) {
				return nil, invalid
			}
			if err == nil {
				raw.Inputs = append(raw.Inputs, observeinterval.CaptureInput{Role: role, Path: path, SHA256: dataset.Digest(data)})
			}
			return data, err
		}
		if raw.Certificate, err = read(listener.TLSCertificate, "certificate"); err != nil {
			return raw, ref, err
		}
		if listener.Transport == MutualTLSTransport {
			if raw.Authorities, err = read(listener.ClientCA, "client-authorities"); err != nil {
				return raw, ref, err
			}
		}
		path, err := artifactpath.File(c.root, listener.SecretsFile)
		if err != nil {
			return raw, ref, err
		}
		store, err := savedFile.Read(path)
		if err != nil {
			return raw, ref, err
		}
		document, err := secret.Decode(store)
		if err != nil {
			return raw, ref, err
		}
		credential, err := secret.Bind(document, listener.TLSKeyReference, secret.MLLPEndpoint, listener.Address())
		if err != nil {
			return raw, ref, err
		}
		if credential.Generation < 1 || credential.Rotation(time.Now()) == secret.RotationOverdue {
			return raw, ref, invalid
		}
		registered, _ := json.Marshal(credential, json.Deterministic(true))
		raw.Inputs = append(raw.Inputs, observeinterval.CaptureInput{Role: "credential", Path: path, Credential: credential.Name, SHA256: dataset.Digest(registered)})
		raw.PrivateKey = &networkaction.Credential{Endpoint: listener.Address(), Purpose: sendpolicy.CaptureListen, Generation: strconv.Itoa(credential.Generation), Locator: networkaction.Provider{Command: credential.Command, Arguments: append([]string(nil), credential.Arguments...)}}
	}
	encoded, _ := encodeMember(raw)
	if _, err := observeinterval.DecodeCapture(encoded); err != nil {
		return raw, ref, err
	}
	return raw, ItemRef{Kind: SourceItem, ID: item.ID, Revision: item.RevisionLabel()}, nil
}

// Only public certificate PEM blocks may enter retained source definitions.
// A misplaced private-key file must never become an observation dependency.
func captureCertificates(raw []byte) bool {
	remaining := bytes.TrimSpace(raw)
	if len(remaining) == 0 {
		return false
	}
	for len(remaining) > 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return false
		}
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return false
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return false
		}
		remaining = bytes.TrimSpace(rest)
	}
	return true
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
