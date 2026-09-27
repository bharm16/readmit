package connectedtransport

import (
	"bytes"
	"encoding/json/v2"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"

	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const CredentialSchema = "readmit-connected-credential/v1"

type Credential struct {
	Schema      string               `json:"schema"`
	Project     string               `json:"project"`
	Environment string               `json:"environment"`
	Endpoint    string               `json:"endpoint"`
	Operation   sendpolicy.Operation `json:"operation"`
	Address     string               `json:"address"`
	Generation  int                  `json:"generation"`
	Reference   string               `json:"reference"`
}
type Selection struct{ Case, Target, Policy, Credential string }
type Prepared struct {
	key        secret.Locator
	plan       *connectedtest.Plan
	replay     *replay.Plan
	binding    Binding
	policy     sendpolicy.ScopedPolicy
	credential *Credential
	retained   map[string][]byte
	selected   map[string]string
}

func (p *Prepared) Binding() Binding { return p.binding }

// Prepare performs bounded local reads only. No resolver, secret provider or
// network dependency is accepted. A source case must contain the exact compiled
// stimuli in order; transport does not quietly regenerate or transform them.
func Prepare(plan *connectedtest.Plan, s Selection) (*Prepared, error) {
	if plan == nil {
		return nil, refused
	}
	p := &Prepared{plan: plan, retained: map[string][]byte{}, selected: map[string]string{}}
	read := func(name, path string) ([]byte, error) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, refused
		}
		b, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(absolute)
		if err != nil {
			return nil, refused
		}
		p.retained[name] = b
		p.selected[name] = absolute
		return b, nil
	}
	targetRaw, err := read("target.json", s.Target)
	if err != nil {
		return nil, err
	}
	target, err := replay.DecodeTarget(targetRaw, filepath.Dir(p.selected["target.json"]))
	if err != nil || target.Schema != replay.TargetSchemaV3 || target.Credential.Declared() && target.ClientCertificate == "" || target.Classification != replay.Nonproduction {
		return nil, refused
	}
	policyRaw, err := read("policy.json", s.Policy)
	if err != nil {
		return nil, err
	}
	p.policy, err = sendpolicy.DecodeScopedPolicy(policyRaw)
	if err != nil {
		return nil, refused
	}
	d := plan.Document()
	env := d.Environment
	mode := target.Transport
	if target.ClientCertificate != "" {
		mode = "mtls"
	}
	if env.Classification != "nonproduction" || env.Name != target.Name || env.TLS.Mode != mode || env.TLS.ServerName != target.ServerName || env.AddressPolicyIdentity != connectedtest.Digest(policyRaw) || p.policy.Project != env.Project || p.policy.Environment != env.ID || p.policy.Revision != env.Revision {
		return nil, refused
	}
	if d.Test.Setup.Kind != "operator-declared" || d.Test.Setup.Plan != nil || d.Test.Setup.Cleanup != nil {
		return nil, refused
	}
	ids := []string{}
	for _, id := range d.Order {
		i := slices.IndexFunc(d.Test.Steps, func(s connectedtest.Step) bool { return s.ID == id })
		step := d.Test.Steps[i]
		if step.V2 == nil || step.Endpoint != env.Endpoint {
			return nil, refused
		}
		ids = append(ids, step.V2.Occurrence)
	}
	p.replay, err = replay.PrepareScoped(s.Case, target, replay.Options{Occurrences: ids})
	if err != nil || p.replay.Target().Identity() != env.TargetIdentity || p.replay.Count() != len(ids) {
		return nil, refused
	}
	files := plan.Files()
	for i, m := range p.replay.Mappings() {
		wire, err := p.replay.Outbound(m.OutboundOccurrence)
		if err != nil || m.SourceOccurrence != ids[i] || !bytes.Equal(wire, frame(files["inputs/"+d.Order[i]+".hl7"])) {
			return nil, refused
		}
	}
	if target.CAFile != "" {
		if _, err := read("authorities.pem", target.CAFile); err != nil {
			return nil, err
		}
	}
	if target.ClientCertificate != "" {
		if target.Transport != "tls" || s.Credential == "" {
			return nil, refused
		}
		if _, err := read("client.pem", target.ClientCertificate); err != nil {
			return nil, err
		}
		raw, err := read("credential.json", s.Credential)
		if err != nil {
			return nil, err
		}
		storeRaw, err := read("secrets.json", target.Credential.SecretsFile)
		if err != nil {
			return nil, err
		}
		c, key, err := bindCredentialSnapshot(raw, storeRaw, env, target)
		if err != nil {
			return nil, err
		}
		p.key = key
		p.credential = &c
	} else if s.Credential != "" {
		return nil, refused
	}
	// Bind exact configuration, CA/certificate and purpose-specific locator bytes.
	config := artifactdir.Identity("readmit-connected-configuration/v1", p.retained)
	p.binding = Binding{Plan: plan.Identity(), Configuration: config, Policy: connectedtest.Digest(policyRaw), Credentials: connectedtest.Digest(p.retained["credential.json"]), Source: p.replay.SourceIdentity(), Project: env.Project, Environment: env.ID, Revision: env.Revision, Endpoint: env.Endpoint, Operation: sendpolicy.V2Stimulus}
	// Also verify the independently loaded authority bytes used by replay.
	ca, err := destination.ReadAuthorities(target.CAFile)
	if err != nil || !bytes.Equal(ca, p.retained["authorities.pem"]) {
		return nil, refused
	}
	return p, nil
}
func frame(b []byte) []byte {
	d, err := hl7.Parse(b, hl7.Options{})
	if err == nil && d.Format == hl7.Raw {
		return mllp.Frame(b)
	}
	return b
}
func (p *Prepared) unchanged() error {
	for name, path := range p.selected {
		b, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(path)
		if err != nil || !bytes.Equal(b, p.retained[name]) {
			return refused
		}
	}
	return nil
}

// Derive the executable locator only from the retained snapshots. Reading a
// second store here could authorize one revision and execute another.
func bindCredentialSnapshot(raw, storeRaw []byte, env connectedtest.Environment, target replay.Target) (Credential, secret.Locator, error) {
	var c Credential
	if json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != CredentialSchema || c.Project != env.Project || c.Environment != env.ID || c.Endpoint != env.Endpoint || c.Address != target.Address || c.Operation != sendpolicy.V2Stimulus || c.Generation < 1 || c.Reference != target.Credential.Reference {
		return Credential{}, secret.Locator{}, refused
	}
	store, err := secret.Decode(storeRaw)
	if err != nil {
		return Credential{}, secret.Locator{}, refused
	}
	ref, err := secret.Bind(store, c.Reference, secret.MLLPEndpoint, target.Address)
	if err != nil || ref.Generation != c.Generation {
		return Credential{}, secret.Locator{}, refused
	}
	return c, ref.Locator(), nil
}

type MessagePreview struct {
	Source, Outbound string
	WireBytes        int
}

// Preview returns display metadata from the sealed plan without effects.
func (p *Prepared) Preview() (replay.Target, []MessagePreview) {
	messages := []MessagePreview{}
	for _, m := range p.replay.Mappings() {
		wire, _ := p.replay.Outbound(m.OutboundOccurrence)
		messages = append(messages, MessagePreview{Source: m.SourceOccurrence, Outbound: m.OutboundOccurrence, WireBytes: len(wire)})
	}
	return p.replay.Configuration(), messages
}
