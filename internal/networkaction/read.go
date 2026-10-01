package networkaction

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// OpenHTTP verifies a retained action without resolving a provider or endpoint.
func OpenHTTP(directory string) (Result, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Result{}, err
	}
	return VerifyHTTP(files)
}

// VerifyHTTP validates one retained byte snapshot without re-reading files.
// It applies the same finite layout and semantic checks as OpenHTTP.
func VerifyHTTP(files map[string][]byte) (Result, error) {
	if len(files) > family.Layout.MaxFiles {
		return Result{}, refused
	}
	total := 0
	for name, raw := range files {
		if !family.Layout.AllowFile(name) || len(raw) > family.Layout.MaxFileBytes {
			return Result{}, refused
		}
		total += len(raw)
	}
	if total > family.Layout.MaxBytes {
		return Result{}, refused
	}
	for _, name := range family.Layout.RequiredFiles {
		if _, ok := files[name]; !ok {
			return Result{}, refused
		}
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ResultSchema, files) {
		return Result{}, refused
	}
	var r Result
	if json.Unmarshal(files["result.json"], &r, json.RejectUnknownMembers(true)) != nil || r.Schema != ResultSchema {
		return Result{}, refused
	}
	p, err := PrepareHTTP(files["action.json"], files["policy.json"])
	if err != nil || p.Binding() != r.Binding {
		return Result{}, refused
	}
	if !RecordedActor(r.Actor) {
		return Result{}, refused
	}
	var d sendpolicy.ScopedDecision
	if json.Unmarshal(files["decision.json"], &d, json.RejectUnknownMembers(true)) != nil || d.Operation != r.Binding.Operation || d.Endpoint != r.Binding.Endpoint {
		return Result{}, refused
	}
	u, _ := url.Parse(p.spec.URL)
	address := u.Host
	if u.Port() == "" {
		address = net.JoinHostPort(u.Hostname(), "443")
	}
	if !VerifyDecision(p.policy, sendpolicy.ScopedRequest{Project: p.spec.Project, Environment: p.spec.Environment, Endpoint: p.spec.Endpoint, Classification: p.spec.Classification, Address: address, Operation: p.spec.Operation}, d) {
		return Result{}, refused
	}
	if r.State == "refused" {
		if r.HTTPStatus != 0 || r.ResponseRetained {
			return Result{}, refused
		}
	} else {
		if !d.Allowed || r.State != "responded" && r.State != "uncertain" {
			return Result{}, refused
		}
		var intent struct {
			Binding Binding `json:"binding"`
			State   string  `json:"state"`
		}
		if json.Unmarshal(files["intent.json"], &intent, json.RejectUnknownMembers(true)) != nil || intent.Binding != r.Binding || intent.State != "uncertain-until-settled" {
			return Result{}, refused
		}
		if r.State == "responded" && (r.HTTPStatus < 100 || r.HTTPStatus > 599) || r.State == "uncertain" && (r.HTTPStatus != 0 || r.ResponseRetained) {
			return Result{}, refused
		}
	}
	body, exists := files["response.bin"]
	if exists && r.ResponseDigest != Digest(body) || !exists && r.ResponseDigest != "" || exists != r.ResponseRetained || len(body) > p.spec.MaxBytes || p.spec.Operation == sendpolicy.SMARTToken && exists {
		return Result{}, refused
	}
	expected, _ := json.Marshal(d.Redacted(), json.Deterministic(true))
	if !bytes.Equal(expected, files["operation.json"]) {
		return Result{}, refused
	}
	return r, nil
}

// VerifyDecision replays only retained DNS answers. It never resolves a name.
func VerifyDecision(policy sendpolicy.ScopedPolicy, request sendpolicy.ScopedRequest, record sendpolicy.ScopedDecision) bool {
	if record.Schema != sendpolicy.ScopedDecisionSchema || record.Address != request.Address || record.Endpoint != request.Endpoint || record.Operation != request.Operation {
		return false
	}
	candidates := []netip.Addr{}
	for _, value := range record.Candidates {
		ip, err := netip.ParseAddr(value)
		if err != nil {
			return false
		}
		candidates = append(candidates, ip)
	}
	expected := sendpolicy.DecideScoped(context.Background(), policy, request, func(context.Context, string) ([]netip.Addr, error) { return candidates, nil })
	if !record.Allowed && (record.Reason == "resolution-failed" || record.Reason == "resolution-cancelled" || record.Reason == "unapproved-address" && len(candidates) == 0) {
		return len(candidates) == 0 && record.SelectedAddress == ""
	}
	a, _ := json.Marshal(expected, json.Deterministic(true))
	b, _ := json.Marshal(record, json.Deterministic(true))
	return bytes.Equal(a, b)
}

// CaptureEvidence binds returned objects to exactly the outer bytes verified
// by this read. It is readback metadata, never a new serialized Result member.
type CaptureEvidence struct {
	Result   Result
	Capture  *bundle.Bundle
	Identity string
}

func OpenCapture(directory string) (Result, *bundle.Bundle, error) {
	readback, err := OpenCaptureEvidence(directory)
	return readback.Result, readback.Capture, err
}
func OpenCaptureEvidence(directory string) (CaptureEvidence, error) {
	result, capture, identity, err := readCapture(directory)
	return CaptureEvidence{Result: result, Capture: capture, Identity: identity}, err
}
func readCapture(directory string) (Result, *bundle.Bundle, string, error) {
	files, err := artifactdir.Read(directory, captureFamily.Layout)
	if err != nil {
		return Result{}, nil, "", err
	}
	return verifyCapture(files)
}

// VerifyCaptureEvidence checks the captured action and case without path reads.
func VerifyCaptureEvidence(captured map[string][]byte) (CaptureEvidence, error) {
	files, err := artifactdir.Snapshot(captured, captureFamily.Layout)
	if err != nil {
		return CaptureEvidence{}, err
	}
	result, capture, identity, err := verifyCapture(files)
	return CaptureEvidence{Result: result, Capture: capture, Identity: identity}, err
}
func verifyCapture(files map[string][]byte) (Result, *bundle.Bundle, string, error) {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ResultSchema, files) {
		return Result{}, nil, "", refused
	}
	p, err := PrepareCapture(files["action.json"], files["policy.json"])
	if err != nil {
		return Result{}, nil, "", err
	}
	var r Result
	var d sendpolicy.ScopedDecision
	if json.Unmarshal(files["result.json"], &r, json.RejectUnknownMembers(true)) != nil || r.Schema != ResultSchema || r.Binding != p.binding || json.Unmarshal(files["decision.json"], &d, json.RejectUnknownMembers(true)) != nil {
		return Result{}, nil, "", refused
	}
	s := p.spec
	if !VerifyDecision(p.policy, sendpolicy.ScopedRequest{Project: s.Project, Environment: s.Environment, Endpoint: s.Endpoint, Classification: s.Classification, Address: s.Address, Operation: sendpolicy.CaptureListen}, d) || !d.Allowed {
		return Result{}, nil, "", refused
	}
	var intent Binding
	if json.Unmarshal(files["intent.json"], &intent, json.RejectUnknownMembers(true)) != nil || intent != p.binding {
		return Result{}, nil, "", refused
	}
	if !RecordedActor(r.Actor) || r.State != "captured" && r.State != "incomplete" {
		return Result{}, nil, "", refused
	}
	expected, _ := json.Marshal(d.Redacted(), json.Deterministic(true))
	if !bytes.Equal(expected, files["operation.json"]) {
		return Result{}, nil, "", refused
	}
	capture, err := bundle.Verify(artifactdir.Subtree(files, "case"))
	if err != nil {
		if r.State == "incomplete" {
			return r, nil, strings.TrimSpace(string(files["identity.sha256"])), nil
		}
		return Result{}, nil, "", err
	}
	if r.ResponseDigest != capture.Identity || !artifactdir.MatchesSubtree(files, "case", capture.Manifest.Schema, capture.Identity) {
		return Result{}, nil, "", refused
	}
	return r, capture, strings.TrimSpace(string(files["identity.sha256"])), nil
}
