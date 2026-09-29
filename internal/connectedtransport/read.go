package connectedtransport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Open verifies retained bindings and transport evidence locally. Missing final
// identity is incomplete evidence; reopening never contacts an endpoint.
type Evidence struct {
	Receipt  Receipt
	Identity string
	// Schedule is a scheduled phase's declared delays in the plan's order,
	// each verified against the intent of every occurrence that was sent.
	Schedule []time.Duration
}

func Open(directory string) (Receipt, error) {
	e, err := OpenEvidence(directory)
	return e.Receipt, err
}

// OpenEvidence returns identity from the same bytes whose receipt is verified.
func OpenEvidence(directory string) (Evidence, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Evidence{}, refused
	}
	var r, start Receipt
	if json.Unmarshal(files["receipt.json"], &r, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || (r.Schema != ReceiptSchema && r.Schema != ReceiptSchemaV2 && r.Schema != ReceiptSchemaV3) || r.ApplicationVerdict != "not-evaluated" || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(r.Schema, files) {
		return Evidence{}, refused
	}

	want := r
	want.State = "incomplete"
	want.RunIdentity = ""
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(start)
	if !bytes.Equal(a, b) {
		return Evidence{}, refused
	}
	plan, err := connectedtest.OpenPlan(filepath.Join(directory, "plan"))
	if err != nil || r.Binding.Plan != plan.Identity() || !artifactdir.MatchesSubtree(files, "plan", plan.Document().Schema, artifactdir.Identity(plan.Document().Schema, plan.Files())) {
		return Evidence{}, refused
	}
	env := plan.Document().Environment
	a, _ = json.Marshal(env)
	b, _ = json.Marshal(r.Environment)
	if !bytes.Equal(a, b) || r.Binding.Project != env.Project || r.Binding.Environment != env.ID || r.Binding.Revision != env.Revision || r.Binding.Endpoint != env.Endpoint || r.Binding.Operation != sendpolicy.V2Stimulus || !runnerprotocol.ID(r.Instance) || !runnerprotocol.ID(r.Actor.ID) || !runnerprotocol.ID(r.Actor.Generation) || r.Actor.Kind != "runner" && r.Actor.Kind != "action-review" {
		return Evidence{}, refused
	}
	config := map[string][]byte{}
	for n, v := range files {
		if strings.HasPrefix(n, "configuration/") {
			config[strings.TrimPrefix(n, "configuration/")] = v
		}
	}
	if artifactdir.Identity("readmit-connected-configuration/v1", config) != r.Binding.Configuration || connectedtest.Digest(config["policy.json"]) != r.Binding.Policy || connectedtest.Digest(config["credential.json"]) != r.Binding.Credentials {
		return Evidence{}, refused
	}
	policy, err := sendpolicy.DecodeScopedPolicy(config["policy.json"])
	if err != nil || policy.Project != env.Project || policy.Environment != env.ID || policy.Revision != env.Revision || r.Binding.Policy != env.AddressPolicyIdentity {
		return Evidence{}, refused
	}
	run, err := replay.Open(filepath.Join(directory, "run"))
	if err != nil || !artifactdir.MatchesSubtree(files, "run", run.Manifest.Schema, run.Identity) || run.Identity != r.RunIdentity || run.Manifest.SourceBundleIdentity != r.Binding.Source || run.Manifest.Target.Identity() != env.TargetIdentity || len(run.Events) != len(plan.Document().Order) || transportState(run) != r.State {
		return Evidence{}, refused
	}
	expectedRunSchema := replay.Schema
	if r.Schema == ReceiptSchemaV2 || r.Schema == ReceiptSchemaV3 {
		expectedRunSchema = replay.SequenceSchema
	}
	if run.Manifest.Schema != expectedRunSchema {
		return Evidence{}, refused
	}
	target, decodeErr := replay.DecodeTarget(config["target.json"], "")
	if decodeErr != nil || target.Schema != replay.TargetSchemaV3 || target.Name != env.Name || string(target.Classification) != env.Classification || target.ServerName != env.TLS.ServerName {
		return Evidence{}, refused
	}
	expectedNames := []string{"target.json", "policy.json"}
	var schedule []time.Duration
	if r.Schema == ReceiptSchemaV2 || r.Schema == ReceiptSchemaV3 {
		expectedNames = append(expectedNames, "sequence.json")
		var sequence phaseSequence
		scheduled := r.Schema == ReceiptSchemaV3
		if json.Unmarshal(config["sequence.json"], &sequence, json.RejectUnknownMembers(true)) != nil || sequence.Schema != r.Schema || !slices.Equal(sequence.Order, plan.Document().Order) || len(sequence.Occurrences) != len(run.Events) || scheduled != (sequence.DelaysMS != nil) || scheduled && len(sequence.DelaysMS) != len(run.Events) {
			return Evidence{}, refused
		}
		for i, event := range run.Events {
			if sequence.Occurrences[i] != event.SourceOccurrence || scheduled && sequence.DelaysMS[i] < 0 {
				return Evidence{}, refused
			}
			if scheduled {
				schedule = append(schedule, time.Duration(sequence.DelaysMS[i])*time.Millisecond)
			}
		}
	}

	if target.CAFile != "" {
		expectedNames = append(expectedNames, "authorities.pem")
	}
	if target.ClientCertificate != "" {
		expectedNames = append(expectedNames, "client.pem", "credential.json", "secrets.json")
		if _, _, err := bindCredentialSnapshot(config["credential.json"], config["secrets.json"], env, target); err != nil {
			return Evidence{}, refused
		}
	}
	if len(config) != len(expectedNames) {
		return Evidence{}, refused
	}
	for _, n := range expectedNames {
		if _, ok := config[n]; !ok {
			return Evidence{}, refused
		}
	}
	mode := target.Transport
	if target.ClientCertificate != "" {
		mode = "mtls"
	}
	if mode != env.TLS.Mode {
		return Evidence{}, refused
	}
	record := replay.TargetRecord{Address: target.Address, Transport: target.Transport, TestEndpoint: target.TestEndpoint, ApprovedTransport: target.ApprovedTransport, ConnectTimeout: target.ConnectTimeout, MessageTimeout: target.MessageTimeout, MaxACKBytes: target.MaxACKBytes}
	if len(config["authorities.pem"]) > 0 {
		record.CASHA256 = connectedtest.Digest(config["authorities.pem"])
	}
	if record != run.Manifest.Target {
		return Evidence{}, refused
	}
	var decision sendpolicy.ScopedDecision
	if json.Unmarshal(files["decision.json"], &decision, json.RejectUnknownMembers(true)) != nil {
		return Evidence{}, refused
	}
	candidates := []netip.Addr{}
	for _, candidate := range decision.Candidates {
		ip, err := netip.ParseAddr(candidate)
		if err != nil {
			return Evidence{}, refused
		}
		candidates = append(candidates, ip)
	}
	expected := sendpolicy.DecideScoped(context.Background(), policy, sendpolicy.ScopedRequest{Project: env.Project, Environment: env.ID, Endpoint: env.Endpoint, Operation: sendpolicy.V2Stimulus, Classification: env.Classification, Address: target.Address}, func(context.Context, string) ([]netip.Addr, error) { return candidates, nil })
	a, _ = json.Marshal(expected)
	b, _ = json.Marshal(decision)
	var operational sendpolicy.OperationalDecision
	if json.Unmarshal(files["operational.json"], &operational, json.RejectUnknownMembers(true)) != nil || operational != decision.Redacted() {
		return Evidence{}, refused
	}
	if !expected.Allowed || !bytes.Equal(a, b) {
		return Evidence{}, refused
	}
	var transport TLSReceipt
	if raw, exists := files["transport.json"]; exists {
		if json.Unmarshal(raw, &transport, json.RejectUnknownMembers(true)) != nil || transport.Mode != target.Transport {
			return Evidence{}, refused
		}
		if target.Transport == "tls" {
			if !transport.Verified || transport.Version < tls.VersionTLS12 || transport.ServerName != target.ServerName || len(transport.PeerCertificates) == 0 {
				return Evidence{}, refused
			}
			for _, der := range transport.PeerCertificates {
				if _, err := x509.ParseCertificate(der); err != nil {
					return Evidence{}, refused
				}
			}
		}
	} else if r.State == "settled" || r.State == "uncertain" {
		return Evidence{}, refused
	}
	d := plan.Document()
	inputs := plan.Files()
	for i, e := range run.Events {
		id := d.Order[i]
		j := slices.IndexFunc(d.Test.Steps, func(s connectedtest.Step) bool { return s.ID == id })
		raw, err := run.Raw(e.Intended)
		if err != nil || d.Test.Steps[j].V2 == nil || e.SourceOccurrence != d.Test.Steps[j].V2.Occurrence || !bytes.Equal(raw, frame(inputs["inputs/"+id+".hl7"])) {
			return Evidence{}, refused
		}
		if e.Delivery != "not_sent" {
			// A scheduled send records its delay and never started before it.
			var in intent
			if json.Unmarshal(files["intents/"+e.OutboundOccurrence+".json"], &in, json.RejectUnknownMembers(true)) != nil || in.Step != id || in.Occurrence != e.OutboundOccurrence || in.State != "uncertain-until-settled" || (schedule != nil) != (in.DeclaredDelayMS != nil) || (schedule != nil) != (in.StartedAfterMS != nil) {
				return Evidence{}, refused
			}
			if schedule != nil && (*in.DeclaredDelayMS != schedule[i].Milliseconds() || *in.StartedAfterMS < *in.DeclaredDelayMS) {
				return Evidence{}, refused
			}
		}
	}
	return Evidence{Receipt: r, Identity: strings.TrimSpace(string(files["identity.sha256"])), Schedule: schedule}, nil
}
