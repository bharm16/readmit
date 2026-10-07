package connectedtransport

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/reproducer"

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
	sequence   bool
	schedule   []time.Duration
	key        secret.Locator
	plan       *connectedtest.Plan
	replay     *replay.Plan
	binding    Binding
	policy     sendpolicy.ScopedPolicy
	credential *Credential
	retained   map[string][]byte
	selected   map[string]string
	derivation *reproducer.Prepared
}

func (p *Prepared) Binding() Binding { return p.binding }

// Prepare performs bounded local reads only. No resolver, secret provider or
// network dependency is accepted. A source case must contain the exact compiled
// stimuli in order; transport does not quietly regenerate or transform them.
func Prepare(plan *connectedtest.Plan, s Selection) (*Prepared, error) {
	return prepare(plan, s, false, nil, nil)
}

// PrepareSequence is selected only by the v4 and v5 lifecycles' phase plans,
// never old standalone plans.
func PrepareSequence(plan *connectedtest.Plan, s Selection) (*Prepared, error) {
	return prepare(plan, s, true, nil, nil)
}

// PrepareScheduled is PrepareSequence for a phase of a scheduled lifecycle:
// schedule holds, in the plan's order, the delay each occurrence waits after
// the phase's sending begins. It is retained with the configuration, so a
// grant authorizes this schedule and no other.
func PrepareScheduled(plan *connectedtest.Plan, s Selection, schedule []time.Duration) (*Prepared, error) {
	if plan == nil || schedule == nil || len(schedule) != len(plan.Document().Order) {
		return nil, refused
	}
	for _, d := range schedule {
		if d < 0 || d%time.Millisecond != 0 {
			return nil, refused
		}
	}
	return prepare(plan, s, true, slices.Clone(schedule), nil)
}

// PrepareDerivedSequence prepares already-bound concrete phase inputs against
// the ordinary derived case. It writes nothing and never grants authority.
func PrepareDerivedSequence(plan *connectedtest.Plan, s Selection, derivation *reproducer.Prepared) (*Prepared, error) {
	if derivation == nil || derivation.VerifyUnchanged() != nil {
		return nil, refused
	}
	return prepare(plan, s, true, nil, derivation)
}
func prepare(plan *connectedtest.Plan, s Selection, sequence bool, schedule []time.Duration, derivation *reproducer.Prepared) (*Prepared, error) {
	phase := plan != nil && (plan.Document().Schema == connectedtest.PhasePlanSchema || plan.Document().Schema == connectedtest.PhasePlanSchemaV2)
	if plan == nil || sequence != phase {
		return nil, refused
	}
	p := &Prepared{derivation: derivation, sequence: sequence, schedule: schedule, plan: plan, retained: map[string][]byte{}, selected: map[string]string{}}
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
	if derivation != nil {
		p.replay, err = replay.PrepareScopedDerivedSequence(s.Case, target, replay.Options{Occurrences: ids}, derivation)
	} else if sequence {
		p.replay, err = replay.PrepareScopedSequence(s.Case, target, replay.Options{Occurrences: ids})
	} else {
		p.replay, err = replay.PrepareScoped(s.Case, target, replay.Options{Occurrences: ids})
	}
	if err != nil || p.replay.Target().Identity() != env.TargetIdentity || p.replay.Count() != len(ids) {
		return nil, refused
	}
	// A generated case is sent only on the schedule its generation declared,
	// which a scheduled lifecycle names; its bytes alone cannot say which of
	// the schedules sharing them applies. The replay plan read its provenance.
	if schedule == nil && p.replay.ScenarioTiming() != "" {
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
	if sequence {
		retained := phaseSequence{Schema: ReceiptSchemaV2, Order: d.Order, Occurrences: ids}
		if schedule != nil {
			retained.Schema, retained.DelaysMS = ReceiptSchemaV3, []int64{}
			for _, d := range schedule {
				retained.DelaysMS = append(retained.DelaysMS, d.Milliseconds())
			}
		}
		p.retained["sequence.json"], _ = json.Marshal(retained, json.Deterministic(true))
	}
	config := artifactdir.Identity("readmit-connected-configuration/v1", p.retained)
	p.binding = Binding{Plan: plan.Identity(), Configuration: config, Policy: connectedtest.Digest(policyRaw), Credentials: connectedtest.Digest(p.retained["credential.json"]), Source: p.replay.SourceIdentity(), Project: env.Project, Environment: env.ID, Revision: env.Revision, Endpoint: env.Endpoint, Operation: sendpolicy.V2Stimulus}
	// Also verify the independently loaded authority bytes used by replay.
	ca, err := destination.ReadAuthorities(target.CAFile)
	if err != nil || !bytes.Equal(ca, p.retained["authorities.pem"]) {
		return nil, refused
	}
	return p, nil
}

// phaseSequence is a lifecycle phase's retained order. A scheduled phase
// (v3) adds the delay, in whole milliseconds, each occurrence waits after the
// phase's sending begins.
type phaseSequence struct {
	Schema      string   `json:"schema"`
	Order       []string `json:"order"`
	Occurrences []string `json:"occurrences"`
	DelaysMS    []int64  `json:"delays_ms,omitzero"`
}

// Scheduled reports whether the phase sends on a declared schedule.
func (p *Prepared) Scheduled() bool { return p.schedule != nil }

// Holds reports whether the budget left in ctx holds the phase's schedule:
// its longest delay and then one message and its acknowledgement. A phase that
// does not hold is refused before anything is sent; a delay is never
// shortened to fit. An unscheduled phase, or a context without a deadline,
// always holds.
func (p *Prepared) Holds(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	if p.schedule == nil || !ok {
		return true
	}
	window, _ := time.ParseDuration(p.replay.Configuration().MessageTimeout)
	longest := time.Duration(0)
	for _, d := range p.schedule {
		longest = max(longest, d)
	}
	return time.Until(deadline) > longest+window
}

func frame(b []byte) []byte {
	d, err := hl7.Parse(b, hl7.Options{})
	if err == nil && d.Format == hl7.Raw {
		return mllp.Frame(b)
	}
	return b
}
func (p *Prepared) unchanged() error {
	if p.derivation != nil && p.derivation.VerifyUnchanged() != nil {
		return refused
	}
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
