package connectedtransport

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ReceiptSchema = "readmit-connected-transport/v1"
const ReceiptSchemaV2 = "readmit-connected-transport/v2"

// ReceiptSchemaV3 is a scheduled phase's transport: its retained sequence
// declares each occurrence's delay, and each intent records the delay and when
// the send began after the phase's sending began.
const ReceiptSchemaV3 = "readmit-connected-transport/v3"

type Receipt struct {
	Schema      string                    `json:"schema"`
	Instance    string                    `json:"instance"`
	Binding     Binding                   `json:"binding"`
	Actor       Actor                     `json:"actor"`
	Environment connectedtest.Environment `json:"environment"`
	RunIdentity string                    `json:"run_identity"`
	State       string                    `json:"state"`
	// Application evaluation belongs to retained downstream observations. Even
	// every matching AA is only a transport outcome, never a clinical verdict.
	ApplicationVerdict string `json:"application_verdict"`
}

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected transport", AllowedDirectories: []string{"configuration", "intents"}, Nested: []string{"plan", "run"}, RequiredFiles: []string{"started.json", "receipt.json", "decision.json", "operational.json", "identity.sha256"}, AllowFile: func(string) bool { return true }, MaxFiles: 20000, MaxFileBytes: 16 << 20, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(ReceiptSchema)}

func put(w *artifactdir.Writer, name string, v any) error {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return err
	}
	return w.WriteFile(name, b)
}

// Execute reserves immutable evidence before DNS or secrets. Each invocation
// requires a new output directory. There is deliberately no resume operation:
// an incomplete intent is uncertain and is never automatically resent.
func Execute(ctx context.Context, p *Prepared, authority Authority, instance, output string, resolve sendpolicy.Resolver) (Receipt, error) {
	if p == nil || authority == nil || !runnerprotocol.ID(instance) {
		return Receipt{}, refused
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.plan.Document().Test.Limits.DeadlineMS)*time.Millisecond)
	defer cancel()
	actor, err := authority.Check(ctx, p.binding)
	if err != nil || len(actor.EvidenceIdentity) != 64 || !actor.Expires.After(time.Now()) || !runnerprotocol.ID(actor.ID) || !runnerprotocol.ID(actor.Generation) || actor.Kind != "runner" && actor.Kind != "action-review" {
		return Receipt{}, refused
	}
	check := func(ctx context.Context) error {
		if ctx.Err() != nil || p.unchanged() != nil {
			return refused
		}
		current, err := authority.Check(ctx, p.binding)
		if err != nil || current != actor || !actor.Expires.After(time.Now()) {
			return refused
		}
		return nil
	}
	if check(ctx) != nil {
		return Receipt{}, refused
	}
	if !p.Holds(ctx) {
		return Receipt{}, refused
	}
	// The replay plan checks physical source containment before output creation.
	output, err = p.replay.ScopedDestination(output)
	if err != nil {
		return Receipt{}, refused
	}
	schema := ReceiptSchema
	if p.sequence {
		schema = ReceiptSchemaV2
	}
	if p.schedule != nil {
		schema = ReceiptSchemaV3
	}
	f := family
	f.Seal = artifactdir.DirectoryHash(schema)
	w, err := artifactdir.Create(output, f, artifactdir.Durable)
	if err != nil {
		return Receipt{}, refused
	}
	defer w.Close()
	r := Receipt{Schema: schema, Instance: instance, Binding: p.binding, Actor: actor, Environment: p.plan.Document().Environment, State: "incomplete", ApplicationVerdict: "not-evaluated"}
	if put(w, "started.json", r) != nil {
		return Receipt{}, refused
	}
	if err := p.plan.Write(ctx, filepath.Join(w.Path(), "plan")); err != nil {
		return Receipt{}, refused
	}
	if w.Mkdir("configuration") != nil || w.Mkdir("intents") != nil {
		return Receipt{}, refused
	}
	for name, raw := range p.retained {
		if w.WriteFile("configuration/"+name, raw) != nil {
			return Receipt{}, refused
		}
	}
	if w.Sync() != nil {
		return Receipt{}, refused
	}
	target := p.replay.Configuration()
	budget, _ := time.ParseDuration(target.ConnectTimeout)
	route, err := destination.AdmitScoped(ctx, destination.ScopedRequest{Policy: p.policy, Request: sendpolicy.ScopedRequest{Project: p.binding.Project, Environment: p.binding.Environment, Endpoint: p.binding.Endpoint, Classification: r.Environment.Classification, Address: target.Address, Operation: sendpolicy.V2Stimulus}, Budget: budget, Resolve: resolve, Authorize: check, Record: func(d sendpolicy.ScopedDecision) error {
		if err := put(w, "decision.json", d); err != nil {
			return err
		}
		return put(w, "operational.json", d.Redacted())
	}})
	if err != nil {
		return Receipt{}, refused
	}
	var security *destination.Security
	if target.Transport == "tls" {
		security = &destination.Security{ServerName: target.ServerName, Authorities: p.retained["authorities.pem"]}
		if p.credential != nil {
			if check(ctx) != nil {
				return Receipt{}, refused
			}
			key, err := p.key.Read(ctx)
			if err != nil {
				_ = retainClientRefusal(w, "client-key-unavailable")
				return Receipt{}, refused
			}
			cert, err := tls.X509KeyPair(p.retained["client.pem"], key.Expose())
			if err != nil {
				_ = retainClientRefusal(w, "client-key-pair-invalid")
				return Receipt{}, refused
			}
			security.Certificate = &cert
		}
	}
	if check(ctx) != nil {
		return Receipt{}, refused
	}
	observer := &intentObserver{writer: w, steps: p.plan.Document().Order, mappings: p.replay.Mappings(), schedule: p.schedule, began: time.Now()}
	run, err := replay.SendScoped(ctx, p.replay, filepath.Join(w.Path(), "run"), route, security, observer)
	if err != nil {
		return Receipt{}, refused
	}
	r.RunIdentity = run.Identity
	r.State = transportState(run)
	if put(w, "receipt.json", r) != nil {
		return Receipt{}, refused
	}
	if _, err := w.Seal(nil); err != nil {
		return Receipt{}, refused
	}
	return Open(w.Path())
}

// intent is retained before an occurrence's write. A scheduled phase's intent
// also records the delay the occurrence declared and how long after the
// phase's sending began its send started.
type intent struct {
	Step            string `json:"step"`
	Occurrence      string `json:"occurrence"`
	State           string `json:"state"`
	DeclaredDelayMS *int64 `json:"declared_delay_ms,omitzero"`
	StartedAfterMS  *int64 `json:"started_after_ms,omitzero"`
}
type intentObserver struct {
	writer   *artifactdir.Writer
	steps    []string
	mappings []replay.Mapping
	schedule []time.Duration
	began    time.Time
}

func (o *intentObserver) index(id string) int {
	return slices.IndexFunc(o.mappings, func(m replay.Mapping) bool { return m.OutboundOccurrence == id })
}

// Await holds a scheduled occurrence until its delay after the phase's sending
// began, on the monotonic clock. Replay records a wait the execution ends as
// the occurrence not sent.
func (o *intentObserver) Await(ctx context.Context, id string) error {
	if o.schedule == nil {
		return nil
	}
	i := o.index(id)
	if i < 0 {
		return refused
	}
	wait := time.Until(o.began.Add(o.schedule[i]))
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (o *intentObserver) BeforeSend(id string) error {
	i := o.index(id)
	if i < 0 {
		return refused
	}
	v := intent{Step: o.steps[i], Occurrence: id, State: "uncertain-until-settled"}
	if o.schedule != nil {
		declared, started := o.schedule[i].Milliseconds(), time.Since(o.began).Milliseconds()
		v.DeclaredDelayMS, v.StartedAfterMS = &declared, &started
	}
	if err := put(o.writer, "intents/"+id+".json", v); err != nil {
		return err
	}
	return o.writer.Sync()
}
func (o *intentObserver) Sent(string, []byte) error   { return nil }
func (o *intentObserver) Recorded(replay.Event) error { return nil }
func transportState(r *replay.Run) string {
	state := "settled"
	for _, e := range r.Events {
		if e.Delivery == "uncertain" {
			return "uncertain"
		}
		if e.Delivery != "acknowledged" {
			state = "incomplete"
		}
	}
	return state
}

// TLSReceipt records the successful connection's negotiated facts separately
// from configured expectations, without retaining private-key material.
type TLSReceipt struct {
	Mode             string   `json:"mode"`
	Version          uint16   `json:"version"`
	CipherSuite      uint16   `json:"cipher_suite"`
	ServerName       string   `json:"server_name"`
	Verified         bool     `json:"verified"`
	PeerCertificates [][]byte `json:"peer_certificates_der"`
}

func (o *intentObserver) Connected(c *destination.Connection) error {
	receipt := TLSReceipt{Mode: "plain", PeerCertificates: [][]byte{}}
	if state, ok := c.TLS(); ok {
		receipt.Mode = "tls"
		receipt.Version = state.Version
		receipt.CipherSuite = state.CipherSuite
		receipt.ServerName = state.ServerName
		receipt.Verified = len(state.VerifiedChains) > 0
		for _, cert := range state.PeerCertificates {
			receipt.PeerCertificates = append(receipt.PeerCertificates, cert.Raw)
		}
	}
	return put(o.writer, "transport.json", receipt)
}
