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

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Open verifies retained bindings and transport evidence locally. Missing final
// identity is incomplete evidence; reopening never contacts an endpoint.
func Open(directory string) (Receipt, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Receipt{}, refused
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ReceiptSchema, files) {
		return Receipt{}, refused
	}
	var r, start Receipt
	if json.Unmarshal(files["receipt.json"], &r, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || r.Schema != ReceiptSchema || r.ApplicationVerdict != "not-evaluated" {
		return Receipt{}, refused
	}
	want := r
	want.State = "incomplete"
	want.RunIdentity = ""
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(start)
	if !bytes.Equal(a, b) {
		return Receipt{}, refused
	}
	plan, err := connectedtest.OpenPlan(filepath.Join(directory, "plan"))
	if err != nil || r.Binding.Plan != plan.Identity() {
		return Receipt{}, refused
	}
	env := plan.Document().Environment
	a, _ = json.Marshal(env)
	b, _ = json.Marshal(r.Environment)
	if !bytes.Equal(a, b) || r.Binding.Project != env.Project || r.Binding.Environment != env.ID || r.Binding.Revision != env.Revision || r.Binding.Endpoint != env.Endpoint || r.Binding.Operation != sendpolicy.V2Stimulus || !runnerprotocol.ID(r.Instance) || !runnerprotocol.ID(r.Actor.ID) || !runnerprotocol.ID(r.Actor.Generation) || r.Actor.Kind != "runner" && r.Actor.Kind != "action-review" {
		return Receipt{}, refused
	}
	config := map[string][]byte{}
	for n, v := range files {
		if strings.HasPrefix(n, "configuration/") {
			config[strings.TrimPrefix(n, "configuration/")] = v
		}
	}
	if artifactdir.Identity("readmit-connected-configuration/v1", config) != r.Binding.Configuration || connectedtest.Digest(config["policy.json"]) != r.Binding.Policy || connectedtest.Digest(config["credential.json"]) != r.Binding.Credentials {
		return Receipt{}, refused
	}
	policy, err := sendpolicy.DecodeScopedPolicy(config["policy.json"])
	if err != nil || policy.Project != env.Project || policy.Environment != env.ID || policy.Revision != env.Revision || r.Binding.Policy != env.AddressPolicyIdentity {
		return Receipt{}, refused
	}
	run, err := replay.Open(filepath.Join(directory, "run"))
	if err != nil || run.Identity != r.RunIdentity || run.Manifest.SourceBundleIdentity != r.Binding.Source || run.Manifest.Target.Identity() != env.TargetIdentity || len(run.Events) != len(plan.Document().Order) || transportState(run) != r.State {
		return Receipt{}, refused
	}
	target, decodeErr := replay.DecodeTarget(config["target.json"], "")
	if decodeErr != nil || target.Schema != replay.TargetSchemaV3 || target.Name != env.Name || string(target.Classification) != env.Classification || target.ServerName != env.TLS.ServerName {
		return Receipt{}, refused
	}
	expectedNames := []string{"target.json", "policy.json"}
	if target.CAFile != "" {
		expectedNames = append(expectedNames, "authorities.pem")
	}
	if target.ClientCertificate != "" {
		expectedNames = append(expectedNames, "client.pem", "credential.json", "secrets.json")
		if _, _, err := bindCredentialSnapshot(config["credential.json"], config["secrets.json"], env, target); err != nil {
			return Receipt{}, refused
		}
	}
	if len(config) != len(expectedNames) {
		return Receipt{}, refused
	}
	for _, n := range expectedNames {
		if _, ok := config[n]; !ok {
			return Receipt{}, refused
		}
	}
	mode := target.Transport
	if target.ClientCertificate != "" {
		mode = "mtls"
	}
	if mode != env.TLS.Mode {
		return Receipt{}, refused
	}
	record := replay.TargetRecord{Address: target.Address, Transport: target.Transport, TestEndpoint: target.TestEndpoint, ApprovedTransport: target.ApprovedTransport, ConnectTimeout: target.ConnectTimeout, MessageTimeout: target.MessageTimeout, MaxACKBytes: target.MaxACKBytes}
	if len(config["authorities.pem"]) > 0 {
		record.CASHA256 = connectedtest.Digest(config["authorities.pem"])
	}
	if record != run.Manifest.Target {
		return Receipt{}, refused
	}
	var decision sendpolicy.ScopedDecision
	if json.Unmarshal(files["decision.json"], &decision, json.RejectUnknownMembers(true)) != nil {
		return Receipt{}, refused
	}
	candidates := []netip.Addr{}
	for _, candidate := range decision.Candidates {
		ip, err := netip.ParseAddr(candidate)
		if err != nil {
			return Receipt{}, refused
		}
		candidates = append(candidates, ip)
	}
	expected := sendpolicy.DecideScoped(context.Background(), policy, sendpolicy.ScopedRequest{Project: env.Project, Environment: env.ID, Endpoint: env.Endpoint, Operation: sendpolicy.V2Stimulus, Classification: env.Classification, Address: target.Address}, func(context.Context, string) ([]netip.Addr, error) { return candidates, nil })
	a, _ = json.Marshal(expected)
	b, _ = json.Marshal(decision)
	var operational sendpolicy.OperationalDecision
	if json.Unmarshal(files["operational.json"], &operational, json.RejectUnknownMembers(true)) != nil || operational != decision.Redacted() {
		return Receipt{}, refused
	}
	if !expected.Allowed || !bytes.Equal(a, b) {
		return Receipt{}, refused
	}
	var transport TLSReceipt
	if raw, exists := files["transport.json"]; exists {
		if json.Unmarshal(raw, &transport, json.RejectUnknownMembers(true)) != nil || transport.Mode != target.Transport {
			return Receipt{}, refused
		}
		if target.Transport == "tls" {
			if !transport.Verified || transport.Version < tls.VersionTLS12 || transport.ServerName != target.ServerName || len(transport.PeerCertificates) == 0 {
				return Receipt{}, refused
			}
			for _, der := range transport.PeerCertificates {
				if _, err := x509.ParseCertificate(der); err != nil {
					return Receipt{}, refused
				}
			}
		}
	} else if r.State == "settled" || r.State == "uncertain" {
		return Receipt{}, refused
	}
	d := plan.Document()
	inputs := plan.Files()
	for i, e := range run.Events {
		id := d.Order[i]
		j := slices.IndexFunc(d.Test.Steps, func(s connectedtest.Step) bool { return s.ID == id })
		raw, err := run.Raw(e.Intended)
		if err != nil || d.Test.Steps[j].V2 == nil || e.SourceOccurrence != d.Test.Steps[j].V2.Occurrence || !bytes.Equal(raw, frame(inputs["inputs/"+id+".hl7"])) {
			return Receipt{}, refused
		}
		if e.Delivery != "not_sent" {
			var in intent
			if json.Unmarshal(files["intents/"+e.OutboundOccurrence+".json"], &in, json.RejectUnknownMembers(true)) != nil || in.Step != id || in.Occurrence != e.OutboundOccurrence || in.State != "uncertain-until-settled" {
				return Receipt{}, refused
			}
		}
	}
	return r, nil
}
