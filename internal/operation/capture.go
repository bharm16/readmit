// Package operation — capture helpers shared by the command line and desktop.
// Presentation and state mapping stay in those adapters; bind, serve, diagnose,
// collect and journal recovery live here once beside the domain packages.
package operation

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// maxCertificateBytes bounds each PEM file a listener is configured with,
// matching the bound a target configuration applies to its own CA file.
const maxCertificateBytes = 1 << 20

// The bounds a capture runs under when its adapter was not told otherwise:
// the command line's flag defaults, and what the window uses for a bound its
// request left out. A capture configuration states every bound it runs
// under, so an operation never replaces a stated one, zero included.
const (
	DefaultMaxFrameBytes      = 1 << 20
	DefaultIdleTimeout        = 30 * time.Second
	DefaultApplicationTimeout = 10 * time.Second
)

// ErrCollectionReceiptExists and ErrAccessReportExists refuse a source run's
// receipt or diagnosis destination that is already taken, before the source
// is reached.
var (
	ErrCollectionReceiptExists = errors.New("the collection receipt destination must be a new file")
	ErrAccessReportExists      = errors.New("the access diagnosis destination must be a new file")
)

// ErrListenerTLSIncomplete refuses a listener that declared part of its
// transport security: a certificate chain, the reference to its private key
// and the store holding it are declared together, or not at all.
var ErrListenerTLSIncomplete = errors.New("a TLS listener requires certificate, key reference and secrets together")

// SourceSave writes a validated evidence-source declaration atomically and
// reads it back, so a GUI save and a CLI-authored file share one writer.
func SourceSave(path string, source evidencesource.Source) (evidencesource.Source, error) {
	if path == "" {
		return evidencesource.Source{}, errors.New("source destination must not be empty")
	}
	if source.Schema == "" {
		source.Schema = evidencesource.Schema
	}
	if err := source.Validate(); err != nil {
		return evidencesource.Source{}, err
	}
	data, err := json.Marshal(source, json.Deterministic(true))
	if err != nil {
		return evidencesource.Source{}, errors.New("cannot encode evidence source")
	}
	data = append(data, '\n')
	if _, err := evidencesource.Decode(data); err != nil {
		return evidencesource.Source{}, err
	}
	if err := atomicWrite(path, data); err != nil {
		return evidencesource.Source{}, err
	}
	return evidencesource.ReadSource(path)
}

// SourceRead opens one declared evidence source.
func SourceRead(path string) (evidencesource.Source, error) {
	return evidencesource.ReadSource(path)
}

// SourceOptions is the declaration a source run reaches its source under: the
// import plan, and the approved-destination policy when one is named. Both
// entry points read it here, so neither diagnoses nor collects under a policy
// the other would refuse, and neither repeats the policy's path.
func SourceOptions(plan importer.Plan, policyPath string) (evidencesource.Options, error) {
	options := evidencesource.Options{Plan: plan}
	if policyPath == "" {
		return options, nil
	}
	data, err := ReadInputFile(policyPath, sendpolicy.MaxPolicyBytes)
	if err != nil {
		return evidencesource.Options{}, errors.New("cannot read the approved-destination policy")
	}
	policy, err := sendpolicy.DecodePolicy(data)
	if err != nil {
		return evidencesource.Options{}, err
	}
	options.Policy = &policy
	return options, nil
}

// SourceDiagnose reports what access to a declared source was actually
// available, collecting nothing. With a report destination, the
// readmit-source-access/v1 document is also retained there: the destination
// is refused before the source is reached unless it is new, its missing
// folders are created, and the document is written before it is returned.
func SourceDiagnose(ctx context.Context, source evidencesource.Source, options evidencesource.Options, report string) (evidencesource.Access, error) {
	if report != "" {
		if err := checkNewDocument(report, ErrAccessReportExists); err != nil {
			return evidencesource.Access{}, err
		}
	}
	access, err := evidencesource.Diagnose(ctx, source, options)
	if err != nil || report == "" {
		return access, err
	}
	encoded, err := evidencesource.EncodeAccess(access)
	if err != nil {
		return evidencesource.Access{}, err
	}
	if err := writeNewDocument(report, encoded,
		"cannot create the access diagnosis; destination must be new and writable",
		"cannot write the access diagnosis"); err != nil {
		return evidencesource.Access{}, err
	}
	return access, nil
}

// SourceCollect stages the declared source's evidence into a new directory and
// writes the collection receipt. The receipt destination is refused before
// anything is collected unless it is new, so an unusable one cannot leave
// staged evidence behind that nothing describes; its missing folders are
// created when it is written.
func SourceCollect(ctx context.Context, source evidencesource.Source, output, receipt string, options evidencesource.Options) (evidencesource.Collection, error) {
	if output == "" || receipt == "" {
		return evidencesource.Collection{}, errors.New("source collect requires a new output directory and a new receipt file")
	}
	if err := checkNewDocument(receipt, ErrCollectionReceiptExists); err != nil {
		return evidencesource.Collection{}, err
	}
	collection, collectErr := evidencesource.Collect(ctx, source, output, options)
	if collectErr != nil && !errors.Is(collectErr, evidencesource.ErrIncomplete) {
		return evidencesource.Collection{}, collectErr
	}
	encoded, err := evidencesource.EncodeCollection(collection)
	if err != nil {
		return evidencesource.Collection{}, err
	}
	if err := writeNewDocument(receipt, encoded,
		"the evidence was staged but its receipt was not; the receipt destination must be new and writable",
		"the evidence was staged but its receipt was not; retry the collection with new destinations"); err != nil {
		return evidencesource.Collection{}, err
	}
	if collectErr != nil {
		return collection, collectErr
	}
	return collection, nil
}

// ReceiverPolicySave writes a validated receiver policy atomically and reads it back.
func ReceiverPolicySave(path string, policy collection.Policy) (collection.Policy, error) {
	if path == "" {
		return collection.Policy{}, errors.New("receiver policy destination must not be empty")
	}
	data, err := collection.EncodePolicy(policy)
	if err != nil {
		return collection.Policy{}, err
	}
	data = append(data, '\n')
	if err := atomicWrite(path, data); err != nil {
		return collection.Policy{}, err
	}
	return ReceiverPolicyRead(path)
}

// ReceiverPolicyRead opens one declared receiver policy with the reader
// `readmit collect --policy` uses, so a policy the window reopens is read and
// refused exactly as the command line reads and refuses it.
func ReceiverPolicyRead(path string) (collection.Policy, error) {
	data, err := ReadInputFile(path, collection.MaxPolicyBytes)
	if err != nil {
		return collection.Policy{}, err
	}
	return collection.DecodePolicy(data)
}

// CapturePreview is the value-free description of a capture the operator has
// configured but not yet started. It names declarations and counts, never an
// endpoint value beyond the address the operator typed, and never a credential.
type CapturePreview struct {
	Kind              string `json:"kind"` // "collect" or "listen"
	Address           string `json:"address"`
	ApprovedBind      bool   `json:"approved_bind"`
	PolicyName        string `json:"policy_name,omitzero"`
	PolicySchema      string `json:"policy_schema,omitzero"`
	SourceLabel       string `json:"source_label,omitzero"`
	Acknowledgement   string `json:"acknowledgement,omitzero"`
	Enhanced          string `json:"enhanced,omitzero"`
	FixtureMode       string `json:"fixture_mode,omitzero"`
	FixtureLabel      string `json:"fixture_label,omitzero"`
	Transport         string `json:"transport"`
	ClientCertificate bool   `json:"client_certificate"`
	KeyReference      string `json:"key_reference,omitzero"`
	MaxConnections    int    `json:"max_connections,omitzero"`
	MaxMessages       int    `json:"max_messages,omitzero"`
	MaxSessions       int    `json:"max_sessions,omitzero"`
	MaxCaptureBytes   int    `json:"max_capture_bytes,omitzero"`
	MaxFrameBytes     int    `json:"max_frame_bytes"`
	IdleTimeout       string `json:"idle_timeout"`
	JournalEnabled    bool   `json:"journal_enabled"`
	OutputName        string `json:"output_name,omitzero"`
	ObservationName   string `json:"observation_name,omitzero"`
}

// CollectConfig is what one MLLP collector serve needs after the operator
// previewed and authorized it.
type CollectConfig struct {
	Address      string
	ApprovedBind bool
	// Policy is the receiver policy the collector serves under. Without one,
	// PolicyPath names the document it is read from, only once the address is
	// approved, so a bind beyond this machine is refused before anything else
	// whichever entry point asked.
	Policy             *collection.Policy
	PolicyPath         string
	OutputPath         string
	JournalPath        string
	MaxFrameBytes      int
	IdleTimeout        time.Duration
	ApplicationTimeout time.Duration
	MaxMessages        int
	MaxConnections     int
	MaxSessions        int
	MaxCaptureBytes    int
	TLSCertificatePath string
	TLSKeyReference    string
	SecretsFile        string
	ClientCAPath       string
	// Listening, when set, is told the address the collector bound once it is
	// ready to accept, and the policy it serves under, the point at which
	// `readmit collect` prints its `Listening:` line. A port of 0 is only
	// known from here. An error it returns stops the collector before it
	// serves anyone.
	Listening func(bound string, policy collection.Policy) error
}

// ListenConfig is what one SIU fixture serve needs after preview and approval.
type ListenConfig struct {
	Address         string
	ApprovedBind    bool
	Mode            observation.Mode
	OutputPath      string
	ObservationPath string
	MaxFrameBytes   int
	IdleTimeout     time.Duration
	MaxMessages     int
	// Listening, when set, is told the address the fixture bound once its
	// initial observation is installed, the point at which `readmit listen`
	// prints its `Listening:` line. A port of 0 is only known from here. An
	// error it returns stops the fixture before it serves anyone.
	Listening func(bound string) error
}

// ErrReceiverPolicyRequired refuses a collector configured with no receiver
// policy at all.
var ErrReceiverPolicyRequired = errors.New("a receiver policy is required")

// PreviewCollect validates a collector configuration without binding a socket.
func PreviewCollect(cfg CollectConfig) (CapturePreview, error) {
	policy, err := approveCollect(cfg)
	if err != nil {
		return CapturePreview{}, err
	}
	if cfg.OutputPath == "" {
		return CapturePreview{}, errors.New("collector requires a new case destination")
	}
	transport := "plain"
	clientCert := cfg.ClientCAPath != ""
	if cfg.TLSCertificatePath != "" || cfg.TLSKeyReference != "" || cfg.SecretsFile != "" {
		if cfg.TLSCertificatePath == "" || cfg.TLSKeyReference == "" || cfg.SecretsFile == "" {
			return CapturePreview{}, ErrListenerTLSIncomplete
		}
		transport = "tls"
	}
	enhanced := "unsupported"
	if policy.Enhanced != nil {
		enhanced = policy.Enhanced.Operator
	}
	connections := cfg.MaxConnections
	if connections <= 0 {
		connections = 1
	}
	return CapturePreview{
		Kind:              "collect",
		Address:           cfg.Address,
		ApprovedBind:      cfg.ApprovedBind,
		PolicyName:        policy.Name,
		PolicySchema:      policy.Schema,
		SourceLabel:       policy.SourceLabel,
		Acknowledgement:   policy.Acknowledgement.Operator + " " + policy.Acknowledgement.Code,
		Enhanced:          enhanced,
		Transport:         transport,
		ClientCertificate: clientCert,
		KeyReference:      cfg.TLSKeyReference,
		MaxConnections:    connections,
		MaxMessages:       cfg.MaxMessages,
		MaxSessions:       cfg.MaxSessions,
		MaxCaptureBytes:   cfg.MaxCaptureBytes,
		MaxFrameBytes:     cfg.MaxFrameBytes,
		IdleTimeout:       cfg.IdleTimeout.String(),
		JournalEnabled:    cfg.JournalPath != "",
		OutputName:        baseName(cfg.OutputPath),
	}, nil
}

// approveCollect approves the address first, then reads the policy the
// configuration names when it carries none, and holds a fault policy to the
// test endpoints it approves. It returns the policy the collector serves
// under. Nothing binds before it answers.
func approveCollect(cfg CollectConfig) (collection.Policy, error) {
	if err := sendpolicy.BindAddress(cfg.Address, cfg.ApprovedBind); err != nil {
		return collection.Policy{}, err
	}
	var policy collection.Policy
	switch {
	case cfg.Policy != nil:
		policy = *cfg.Policy
	case cfg.PolicyPath != "":
		read, err := ReceiverPolicyRead(cfg.PolicyPath)
		if err != nil {
			return collection.Policy{}, err
		}
		policy = read
	default:
		return collection.Policy{}, ErrReceiverPolicyRequired
	}
	if err := policy.Validate(); err != nil {
		return collection.Policy{}, err
	}
	// A fault policy names the test endpoints it may be served at. The address
	// is held to them before anything binds, as `readmit collect` holds it, so
	// a collector never listens where its policy did not approve.
	if policy.Faults != nil {
		if err := policy.Faults.ApproveEndpoint(cfg.Address); err != nil {
			return collection.Policy{}, err
		}
	}
	return policy, nil
}

// PreviewListen validates a SIU fixture configuration without binding.
func PreviewListen(cfg ListenConfig) (CapturePreview, error) {
	if err := sendpolicy.BindAddress(cfg.Address, cfg.ApprovedBind); err != nil {
		return CapturePreview{}, err
	}
	if cfg.Mode != observation.Fixed && cfg.Mode != observation.Defective {
		return CapturePreview{}, errors.New("receiver mode must be fixed or defective")
	}
	if cfg.OutputPath == "" || cfg.ObservationPath == "" {
		return CapturePreview{}, errors.New("receiver requires new case and observation destinations")
	}
	return CapturePreview{
		Kind:            "listen",
		Address:         cfg.Address,
		ApprovedBind:    cfg.ApprovedBind,
		FixtureMode:     string(cfg.Mode),
		FixtureLabel:    "built-in synthetic SIU fixture (readmit-siu-v1)",
		Transport:       "plain",
		MaxMessages:     cfg.MaxMessages,
		MaxFrameBytes:   cfg.MaxFrameBytes,
		IdleTimeout:     cfg.IdleTimeout.String(),
		JournalEnabled:  false,
		OutputName:      baseName(cfg.OutputPath),
		ObservationName: baseName(cfg.ObservationPath),
	}, nil
}

// CollectResult is what one collector serve produced.
type CollectResult struct {
	BoundAddress string
	Bundle       *bundle.Bundle
	Journal      *capturejournal.Summary
}

// ListenResult is what one SIU fixture serve produced.
type ListenResult struct {
	BoundAddress string
	Bundle       *bundle.Bundle
	Observation  *observation.Snapshot
}

// StartCollect binds the collector, serves until a bound, cancellation or error,
// and returns the sealed case when one was written. It never invents completion
// when the process stops mid-flight. It refuses in the order `readmit collect`
// always has: the address, the policy, the transport security, the bind, and
// then the collector's own bounds and destinations; an adapter that wants the
// destinations refused before anything binds previews first.
func StartCollect(ctx context.Context, cfg CollectConfig) (CollectResult, error) {
	policy, err := approveCollect(cfg)
	if err != nil {
		return CollectResult{}, err
	}
	secured, err := listenerTLSConfig(ctx, cfg)
	if err != nil {
		return CollectResult{}, err
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return CollectResult{}, errors.New("cannot bind collector address")
	}
	defer listener.Close()
	bound := listener.Addr().String()
	if secured != nil {
		listener = tls.NewListener(listener, secured)
	}
	collector, err := receiver.NewCollector(receiver.CollectorConfig{
		Policy:             policy,
		OutputPath:         cfg.OutputPath,
		JournalPath:        cfg.JournalPath,
		MaxFrameBytes:      cfg.MaxFrameBytes,
		IdleTimeout:        cfg.IdleTimeout,
		ApplicationTimeout: cfg.ApplicationTimeout,
		MaxMessages:        cfg.MaxMessages,
		MaxConnections:     cfg.MaxConnections,
		MaxSessions:        cfg.MaxSessions,
		MaxCaptureBytes:    cfg.MaxCaptureBytes,
		TLS:                secured != nil,
		ClientCertificate:  cfg.ClientCAPath != "",
	})
	if err != nil {
		return CollectResult{}, err
	}
	if cfg.Listening != nil {
		if err := cfg.Listening(bound, policy); err != nil {
			return CollectResult{BoundAddress: bound}, err
		}
	}
	b, serveErr := collector.Serve(ctx, listener)
	out := CollectResult{BoundAddress: bound, Bundle: b, Journal: collector.Journal()}
	return out, serveErr
}

// StartListen binds the SIU fixture receiver and serves until a bound,
// cancellation or error. It refuses in the order `readmit listen` always
// has: the address, the bind, and then the receiver's own mode, bounds and
// destinations; an adapter that wants those refused before anything binds
// previews first.
func StartListen(ctx context.Context, cfg ListenConfig) (ListenResult, error) {
	if err := sendpolicy.BindAddress(cfg.Address, cfg.ApprovedBind); err != nil {
		return ListenResult{}, err
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return ListenResult{}, errors.New("cannot bind receiver address")
	}
	defer listener.Close()
	bound := listener.Addr().String()
	r, err := receiver.New(receiver.Config{
		Mode:            cfg.Mode,
		OutputPath:      cfg.OutputPath,
		ObservationPath: cfg.ObservationPath,
		MaxFrameBytes:   cfg.MaxFrameBytes,
		IdleTimeout:     cfg.IdleTimeout,
		MaxMessages:     cfg.MaxMessages,
	})
	if err != nil {
		return ListenResult{}, err
	}
	if cfg.Listening != nil {
		if err := cfg.Listening(bound); err != nil {
			return ListenResult{BoundAddress: bound}, err
		}
	}
	b, serveErr := r.Serve(ctx, listener)
	out := ListenResult{BoundAddress: bound, Bundle: b}
	if snap, readErr := observation.Read(cfg.ObservationPath); readErr == nil {
		out.Observation = &snap
	}
	return out, serveErr
}

// CaptureJournalStatus recovers an interrupted capture read-only; it never
// sends, resends or resumes.
func CaptureJournalStatus(path string) (capturejournal.Summary, error) {
	return capturejournal.Open(path)
}

// DefaultImportPlan is the framing a collected folder is imported under when
// the operator kept the source collect plan's declarations.
func DefaultImportPlan() importer.Plan {
	return importer.Plan{
		Schema:     importer.PlanSchema,
		Framing:    importer.RawFraming,
		Terminator: hl7.CR,
		Encoding:   importer.UTF8,
		Direction:  bundle.Inbound,
		Members:    []string{".hl7", ".mllp"},
	}
}

// listenerTLSConfig builds the collector's TLS configuration, or nil when the
// operator declared none. The certificate chain and the client authority are
// configuration files; the private key is a credential, read from the store
// its reference names for this one process and never written anywhere.
func listenerTLSConfig(ctx context.Context, cfg CollectConfig) (*tls.Config, error) {
	if cfg.TLSCertificatePath == "" && cfg.TLSKeyReference == "" && cfg.SecretsFile == "" && cfg.ClientCAPath == "" {
		return nil, nil
	}
	if cfg.TLSCertificatePath == "" || cfg.TLSKeyReference == "" || cfg.SecretsFile == "" {
		return nil, ErrListenerTLSIncomplete
	}
	chain, err := readBoundedFile(cfg.TLSCertificatePath, maxCertificateBytes)
	if err != nil {
		return nil, errors.New("cannot read the configured listener certificate")
	}
	var authorities []byte
	if cfg.ClientCAPath != "" {
		if authorities, err = readBoundedFile(cfg.ClientCAPath, maxCertificateBytes); err != nil {
			return nil, errors.New("cannot read the configured client certificate authority")
		}
	}
	store, err := secret.ReadStore(cfg.SecretsFile)
	if err != nil {
		return nil, err
	}
	// The reference is bound to this listener's own endpoint before it is read,
	// so a credential registered for another address is refused rather than
	// presented here.
	reference, err := secret.Bind(store, cfg.TLSKeyReference, secret.MLLPEndpoint, cfg.Address)
	if err != nil {
		return nil, err
	}
	value, err := secret.Resolve(ctx, reference)
	if err != nil {
		return nil, errors.New("the private key the listener certificate's reference names did not resolve from its declared store")
	}
	return transportsecurity.ServerConfig(chain, value.Expose(), authorities)
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
