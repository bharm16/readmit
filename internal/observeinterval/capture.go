package observeinterval

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

const CaptureSourceSchema = "readmit-live-capture-source/v1"

type ScopeFilter struct {
	Selector string `json:"selector"`
	Equals   string `json:"equals"`
}

// The exact source declaration is pinned independently of an execution plan.
// RunSelector proves which runtime owns an incoming message; Include filters
// are declared before arming, and all excluded occurrences remain in the case.
type CaptureSource struct {
	Schema         string                    `json:"schema"`
	Address        string                    `json:"address"`
	ReceiverPolicy collection.Policy         `json:"receiver_policy"`
	Certificate    []byte                    `json:"certificate"`
	Authorities    []byte                    `json:"client_authorities"`
	PrivateKey     *networkaction.Credential `json:"private_key,omitzero"`
	TimeoutMS      int64                     `json:"timeout_ms"`
	MaxFrameBytes  int                       `json:"max_frame_bytes"`
	MaxBytes       int                       `json:"max_bytes"`
	MaxMessages    int                       `json:"max_messages"`
	MaxConnections int                       `json:"max_connections"`
	MaxSessions    int                       `json:"max_sessions"`
	RunSelector    string                    `json:"run_selector"`
	Include        []ScopeFilter             `json:"include"`
}

func DecodeCapture(raw []byte) (CaptureSource, error) {
	var s CaptureSource
	if len(raw) > 3<<20 || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != CaptureSourceSchema || len(s.Include) > 16 {
		return s, invalid
	}
	if _, err := hl7.ParseSelector(s.RunSelector); err != nil {
		return s, invalid
	}
	for _, f := range s.Include {
		if _, err := hl7.ParseSelector(f.Selector); err != nil || len(f.Equals) > 1024 {
			return s, invalid
		}
	}
	return s, nil
}
func PrepareCapture(raw, policy []byte, scope networkaction.Binding) (*networkaction.CapturePlan, error) {
	s, err := DecodeCapture(raw)
	if err != nil {
		return nil, err
	}
	v := networkaction.CaptureSpecV2{Schema: networkaction.CaptureActionSchemaV2, MaxConnections: s.MaxConnections, MaxSessions: s.MaxSessions, Definition: networkaction.CaptureSpec{Schema: networkaction.CaptureActionSchema, Plan: scope.Plan, Source: dataset.Digest(raw), Project: scope.Project, Environment: scope.Environment, Revision: scope.Revision, Endpoint: scope.Endpoint, Classification: "nonproduction", Address: s.Address, Policy: s.ReceiverPolicy, Certificate: s.Certificate, Authorities: s.Authorities, PrivateKey: s.PrivateKey, TimeoutMS: s.TimeoutMS, MaxFrameBytes: s.MaxFrameBytes, MaxBytes: s.MaxBytes, MaxMessages: s.MaxMessages}}
	encoded, _ := json.Marshal(v, json.Deterministic(true))
	return networkaction.PrepareCapture(encoded, policy)
}

type Capture struct {
	session    *networkaction.CaptureSession
	source     CaptureSource
	raw        []byte
	binding    dataset.Binding
	projection dataset.Projection
	output     string
	started    time.Time
	excluded   map[string]string
}

func ArmCapture(ctx context.Context, raw []byte, p *networkaction.CapturePlan, authority networkaction.Authority, binding dataset.Binding, projection dataset.Projection, output string, resolve sendpolicy.Resolver) (*Capture, error) {
	s, err := DecodeCapture(raw)
	if err != nil || p == nil || p.Binding().Source != dataset.Digest(raw) || binding.Source != dataset.Digest(raw) || projection.Format != "hl7" || projection.Validate() != nil {
		return nil, invalid
	}
	session, err := p.Start(ctx, authority, output, resolve)
	if err != nil {
		return nil, err
	}
	return &Capture{session: session, source: s, raw: slices.Clone(raw), binding: binding, projection: projection, output: output, started: time.Now().UTC(), excluded: map[string]string{}}, nil
}
func (c *Capture) Address() string { return c.session.Address() }
func (c *Capture) Poll(context.Context) (Observation, error) {
	progress := c.session.Progress()
	o := Observation{Binding: c.binding, Status: "healthy", Records: progress.Received, Bytes: progress.Bytes, Watermark: strconv.Itoa(progress.Received)}
	select {
	case <-c.session.Done():
		o.Status = "collector-stopped"
	default:
	}
	if progress.Stopped {
		o.Status = "collector-stopped"
	}
	return o, nil
}
func (c *Capture) Stop() { c.session.Stop() }

// Close releases the active socket and waits for its evidence writer. It never
// removes the spool or resumes a capture.
func (c *Capture) Close() error { c.session.Stop(); _, err := c.session.Wait(); return err }
func (c *Capture) Finalize(ctx context.Context) (Observation, error) {
	finalizing := time.Now().UTC()
	stoppedBefore := c.session.Progress().Stopped
	c.session.Stop()
	captured, err := c.session.Wait()
	o := Observation{Binding: c.binding, Status: "healthy"}
	if err != nil || captured == nil {
		return o, invalid
	}
	_, verified, err := networkaction.OpenCapture(c.output)
	if err != nil || verified.Identity != captured.Identity {
		return o, invalid
	}
	material, excluded, status, err := captureMaterial(captured, c.source, c.binding)
	if err != nil {
		return o, err
	}
	o.Status = status
	journal, err := capturejournal.Open(filepath.Join(c.output, "journal"))
	if err != nil {
		return o, err
	}
	if journal.JournalIncomplete || journal.DeliveryUncertain || journal.Unsent > 0 {
		o.Status = "lost-coverage"
	}
	if journal.StopReason != durablerun.Cancelled {
		o.Status = "collector-stopped"
	}
	o.Excluded = excluded
	o.CapturePath = "capture"
	progress := c.session.Progress()
	if stoppedBefore || progress.Received >= c.source.MaxMessages || progress.Bytes >= c.source.MaxBytes || len(captured.Manifest.Sources) >= c.source.MaxSessions {
		o.Status = "safety-limit"
	}
	snapshot, err := dataset.Build(ctx, c.binding, c.projection, dataset.Acquisition{Kind: "capture", Status: "complete", StartedAt: finalizing, Facts: &dataset.AcquisitionFacts{ObservedFrom: c.started, Status: "observed"}, CompletedAt: time.Now().UTC(), SourceConfiguration: c.raw, Completion: "snapshot"}, material)
	if err != nil {
		return o, err
	}
	// Exclusions accompany the immutable network capture, outside its sealed case.
	// The interval snapshot contains only the predeclared scope, never changed
	// retrospectively after an assertion is evaluated.
	o.Snapshot = snapshot
	o.Records = len(snapshot.Document().Rows)
	o.Bytes = len(material)
	o.Watermark = captured.Identity
	return o, nil
}
func (c *Capture) EvidencePath() string { return filepath.Join(c.output, "case") }

func captureMaterial(captured *bundle.Bundle, source CaptureSource, binding dataset.Binding) ([]byte, map[string]string, string, error) {
	excluded := map[string]string{}
	status := "healthy"
	bytes := 0
	for _, s := range captured.Manifest.Sources {
		bytes += s.Size
	}
	if captured.Collection == nil {
		return nil, nil, "", invalid
	}
	if len(captured.Collection.Received) >= source.MaxMessages || len(captured.Manifest.Sources) >= source.MaxSessions || bytes >= source.MaxBytes {
		status = "safety-limit"
	}
	record := dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: captured.Identity, Rows: []dataset.CaptureRow{}}
	for _, event := range captured.Events {
		if event.Direction != bundle.Inbound {
			continue
		}
		if event.Kind == bundle.Unparsed {
			status = "lost-coverage"
			continue
		}
		raw, err := captured.Raw(event.ID)
		if err != nil {
			return nil, nil, "", err
		}
		doc, err := captured.Document(event)
		if err != nil || len(doc.Messages) != 1 {
			status = "lost-coverage"
			continue
		}
		runSelector, _ := hl7.ParseSelector(source.RunSelector)
		run, err := doc.Read(0, runSelector, hl7.EnforceMSH18)
		if err != nil || run.State != hl7.Present || run.Reason != "" || string(run.Decoded) != binding.Run {
			excluded[event.ID] = "wrong-run"
			status = "wrong-scope"
			continue
		}
		included := true
		exclusion := "declared-filter"
		for _, filter := range source.Include {
			sel, _ := hl7.ParseSelector(filter.Selector)
			value, err := doc.Read(0, sel, hl7.EnforceMSH18)
			if err != nil || value.Reason != "" || value.State != hl7.Present {
				status = "unknown-scope"
				exclusion = "unreadable-filter"
				included = false
				break
			}
			if value.State != hl7.Present || string(value.Decoded) != filter.Equals {
				included = false
			}
		}
		if !included {
			excluded[event.ID] = exclusion
			continue
		}
		record.Rows = append(record.Rows, dataset.CaptureRow{Occurrence: event.ID, Raw: raw})
	}
	material, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return nil, nil, "", err
	}
	return material, excluded, status, nil
}
