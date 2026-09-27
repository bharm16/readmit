package networkaction

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"net"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

const CaptureActionSchema = "readmit-network-capture-action/v1"
const CaptureActionSchemaV2 = "readmit-network-capture-action/v2"

// CaptureSpecV2 removes the historical one-connection lifetime. Its limits
// remain safety ceilings, never a business completion rule.
type CaptureSpecV2 struct {
	Schema         string      `json:"schema"`
	Definition     CaptureSpec `json:"definition"`
	MaxConnections int         `json:"max_connections"`
	MaxSessions    int         `json:"max_sessions"`
}

type CaptureSpec struct {
	Schema         string            `json:"schema"`
	Plan           string            `json:"plan"`
	Source         string            `json:"source"`
	Project        string            `json:"project"`
	Environment    string            `json:"environment"`
	Revision       string            `json:"revision"`
	Endpoint       string            `json:"endpoint"`
	Classification string            `json:"classification"`
	Address        string            `json:"address"`
	Policy         collection.Policy `json:"receiver_policy"`
	Certificate    []byte            `json:"certificate"`
	Authorities    []byte            `json:"client_authorities"`
	PrivateKey     *Credential       `json:"private_key,omitzero"`
	TimeoutMS      int64             `json:"timeout_ms"`
	MaxFrameBytes  int               `json:"max_frame_bytes"`
	MaxBytes       int               `json:"max_bytes"`
	MaxMessages    int               `json:"max_messages"`
}
type CapturePlan struct {
	maxConnections, maxSessions int
	continuous                  bool
	spec                        CaptureSpec
	policy                      sendpolicy.ScopedPolicy
	binding                     Binding
	raw, policyRaw              []byte
}

func PrepareCapture(raw, policyRaw []byte) (*CapturePlan, error) {
	var s CaptureSpec
	original := bytes.Clone(raw)
	maxConnections, maxSessions := 1, 1
	continuous := false
	var head struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return nil, refused
	}
	if head.Schema == CaptureActionSchemaV2 {
		var v CaptureSpecV2
		if len(raw) > 3<<20 || json.Unmarshal(raw, &v, json.RejectUnknownMembers(true)) != nil || v.MaxConnections < 1 || v.MaxConnections > receiver.MaxConnections || v.MaxSessions < 1 || v.MaxSessions > bundle.MaxSources {
			return nil, refused
		}
		raw, _ = json.Marshal(v.Definition, json.Deterministic(true))
		maxConnections, maxSessions = v.MaxConnections, v.MaxSessions
		continuous = true
	}
	if len(raw) > 3<<20 || json.Unmarshal(raw, &s, json.RejectUnknownMembers(true)) != nil || s.Schema != CaptureActionSchema || !hash.MatchString(s.Plan) || !hash.MatchString(s.Source) || !identifier.MatchString(s.Endpoint) || s.Classification != "nonproduction" || s.TimeoutMS < 1 || s.TimeoutMS > 300000 || s.MaxFrameBytes < 1 || s.MaxFrameBytes > 16<<20 || s.MaxBytes < s.MaxFrameBytes+(16<<10) || s.MaxBytes > 32<<20 || s.MaxMessages < 1 || s.MaxMessages > receiver.MaxMessages {
		return nil, refused
	}
	policy, err := sendpolicy.DecodeScopedPolicy(policyRaw)
	if err != nil || policy.Project != s.Project || policy.Environment != s.Environment || policy.Revision != s.Revision {
		return nil, refused
	}
	if _, _, err := net.SplitHostPort(s.Address); err != nil {
		return nil, refused
	}
	policyBytes, err := json.Marshal(s.Policy)
	if err != nil {
		return nil, refused
	}
	if _, err := collection.DecodePolicy(policyBytes); err != nil {
		return nil, refused
	}
	if s.Policy.Enhanced != nil && s.Policy.Enhanced.ApplicationDelivery == collection.SeparateEndpoint {
		return nil, refused
	}
	if (len(s.Certificate) > 0) != (s.PrivateKey != nil) || len(s.Certificate) > 1<<20 || len(s.Authorities) > 1<<20 || len(s.Authorities) > 0 && s.PrivateKey == nil {
		return nil, refused
	}
	if c := s.PrivateKey; c != nil {
		if c.Endpoint != s.Address || c.Purpose != sendpolicy.CaptureListen || !identifier.MatchString(c.Generation) || c.Header != "" || c.Prefix != "" || (secret.Locator{Command: c.Locator.Command, Arguments: c.Locator.Arguments}).Validate() != nil {
			return nil, refused
		}
	}
	raw, err = json.Marshal(s, json.Deterministic(true))
	if err != nil {
		return nil, refused
	}
	if continuous {
		var v CaptureSpecV2
		_ = json.Unmarshal(original, &v)
		raw, _ = json.Marshal(v, json.Deterministic(true))
	}
	credential, _ := json.Marshal(s.PrivateKey, json.Deterministic(true))
	return &CapturePlan{maxConnections: maxConnections, maxSessions: maxSessions, continuous: continuous, spec: s, policy: policy, raw: raw, policyRaw: bytes.Clone(policyRaw), binding: Binding{Plan: s.Plan, Source: s.Source, Configuration: Digest(raw), Policy: Digest(policyRaw), Credentials: Digest(credential), Project: s.Project, Environment: s.Environment, Revision: s.Revision, Endpoint: s.Endpoint, Operation: sendpolicy.CaptureListen}}, nil
}
func (p *CapturePlan) Binding() Binding { return p.binding }

type CaptureSession struct {
	progress func() receiver.CaptureProgress
	done     chan struct{}
	cancel   context.CancelFunc
	result   *bundle.Bundle
	err      error
	address  string
}

func (s *CaptureSession) Progress() receiver.CaptureProgress { return s.progress() }
func (s *CaptureSession) Done() <-chan struct{}              { return s.done }
func (s *CaptureSession) Address() string                    { return s.address }
func (s *CaptureSession) Stop()                              { s.cancel() }
func (s *CaptureSession) Wait() (*bundle.Bundle, error)      { <-s.done; return s.result, s.err }

var captureFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "network capture action", Nested: []string{"case", "journal"}, RequiredFiles: []string{"action.json", "policy.json", "decision.json", "operation.json", "result.json", "identity.sha256"}, AllowFile: func(n string) bool {
	switch n {
	case "action.json", "policy.json", "intent.json", "decision.json", "operation.json", "result.json", "identity.sha256":
		return true
	}
	return false
}, MaxFiles: 20000, MaxFileBytes: 32 << 20, MaxBytes: 128 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

func (p *CapturePlan) Start(ctx context.Context, authority Authority, output string, resolve sendpolicy.Resolver) (*CaptureSession, error) {
	if p == nil || authority == nil {
		return nil, refused
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.spec.TimeoutMS)*time.Millisecond)
	failed := true
	defer func() {
		if failed {
			cancel()
		}
	}()
	actor, err := authority.Check(ctx, p.binding)
	if err != nil || !validActor(actor) {
		return nil, refused
	}
	check := func(ctx context.Context) error {
		a, err := authority.Check(ctx, p.binding)
		if err != nil || a != actor || !validActor(a) || ctx.Err() != nil {
			return refused
		}
		return nil
	}
	w, err := artifactdir.Create(output, captureFamily, artifactdir.Durable)
	if err != nil {
		return nil, refused
	}
	defer func() {
		if failed {
			w.Close()
		}
	}()
	if w.WriteFile("action.json", p.raw) != nil || w.WriteFile("policy.json", p.policyRaw) != nil {
		return nil, refused
	}
	s := p.spec
	route, err := destination.AdmitScoped(ctx, destination.ScopedRequest{Policy: p.policy, Request: sendpolicy.ScopedRequest{Project: s.Project, Environment: s.Environment, Endpoint: s.Endpoint, Address: s.Address, Classification: s.Classification, Operation: sendpolicy.CaptureListen}, Budget: time.Duration(s.TimeoutMS) * time.Millisecond, Resolve: resolve, Authorize: check, Record: func(d sendpolicy.ScopedDecision) error {
		if err := put(w, "decision.json", d); err != nil {
			return err
		}
		if err := put(w, "operation.json", d.Redacted()); err != nil {
			return err
		}
		return w.Sync()
	}})
	if err != nil {
		return nil, refused
	}
	var tlsConfig *tls.Config
	if c := s.PrivateKey; c != nil {
		if check(ctx) != nil {
			return nil, refused
		}
		key, err := (secret.Locator{Command: c.Locator.Command, Arguments: c.Locator.Arguments}).Read(ctx)
		if err != nil {
			return nil, refused
		}
		tlsConfig, err = transportsecurity.ServerConfig(s.Certificate, key.Expose(), s.Authorities)
		if err != nil {
			return nil, refused
		}
	}
	collector, err := receiver.NewCollector(receiver.CollectorConfig{Policy: s.Policy, OutputPath: filepath.Join(w.Path(), "case"), JournalPath: filepath.Join(w.Path(), "journal"), MaxFrameBytes: s.MaxFrameBytes, IdleTimeout: time.Duration(s.TimeoutMS) * time.Millisecond, ApplicationTimeout: time.Duration(s.TimeoutMS) * time.Millisecond, MaxMessages: s.MaxMessages, MaxConnections: p.maxConnections, MaxSessions: p.maxSessions, MaxCaptureBytes: s.MaxBytes, TLS: tlsConfig != nil, ClientCertificate: len(s.Authorities) > 0})
	if err != nil {
		return nil, refused
	}
	if put(w, "intent.json", p.binding) != nil || w.Sync() != nil {
		return nil, refused
	}
	listener, err := route.Listen(ctx)
	if err != nil {
		return nil, refused
	}
	listener = &authorizedListener{Listener: listener, ctx: ctx, check: check}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	session := &CaptureSession{progress: collector.Progress, done: make(chan struct{}), cancel: cancel, address: listener.Addr().String()}
	failed = false
	go func() {
		defer close(session.done)
		defer cancel()
		defer w.Close()
		defer listener.Close()
		captured, err := collector.Serve(ctx, listener)
		session.result = captured
		result := Result{Schema: ResultSchema, Binding: p.binding, Actor: actor, State: "incomplete"}
		if err == nil && captured != nil {
			result.State = "captured"
			result.ResponseDigest = captured.Identity
		}
		if err != nil {
			if p.continuous && captured != nil {
				result.ResponseDigest = captured.Identity
			}
			session.err = refused
		}
		if finish(w, result) != nil {
			session.err = refused
		}
	}()
	return session, nil
}

type authorizedListener struct {
	net.Listener
	ctx   context.Context
	check func(context.Context) error
}

func (l *authorizedListener) Accept() (net.Conn, error) {
	if err := l.check(l.ctx); err != nil {
		return nil, err
	}
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err = l.check(l.ctx); err != nil {
		c.Close()
		return nil, err
	}
	return &authorizedConnection{Conn: c, ctx: l.ctx, check: l.check}, nil
}

type authorizedConnection struct {
	net.Conn
	ctx   context.Context
	check func(context.Context) error
}

func (c *authorizedConnection) Read(b []byte) (int, error) {
	if err := c.check(c.ctx); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(b)
	if check := c.check(c.ctx); check != nil {
		return 0, check
	}
	return n, err
}
func (c *authorizedConnection) Write(b []byte) (int, error) {
	if err := c.check(c.ctx); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}
