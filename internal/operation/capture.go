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
	"os"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
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

const maxCertificateBytes = 1 << 20

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

// SourceDiagnose reports what access to a declared source was actually
// available, collecting nothing.
func SourceDiagnose(ctx context.Context, source evidencesource.Source, options evidencesource.Options) (evidencesource.Access, error) {
	return evidencesource.Diagnose(ctx, source, options)
}

// SourceCollect stages the declared source's evidence into a new directory and
// writes the collection receipt beside it. The receipt destination must be new.
func SourceCollect(ctx context.Context, source evidencesource.Source, output, receipt string, options evidencesource.Options) (evidencesource.Collection, error) {
	if output == "" || receipt == "" {
		return evidencesource.Collection{}, errors.New("source collect requires a new output directory and a new receipt file")
	}
	if err := reserveNewFile(receipt, "the collection receipt destination must be a new file"); err != nil {
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
	if err := writeNewReceipt(receipt, encoded); err != nil {
		return evidencesource.Collection{}, errors.New("the evidence was staged but its receipt was not; the receipt destination must be new and writable")
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
	Address            string
	ApprovedBind       bool
	Policy             collection.Policy
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
	// ready to accept, the point at which `readmit collect` prints its
	// `Listening:` line. A port of 0 is only known from here.
	Listening func(bound string)
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
	// prints its `Listening:` line. A port of 0 is only known from here.
	Listening func(bound string)
}

// PreviewCollect validates a collector configuration without binding a socket.
func PreviewCollect(cfg CollectConfig) (CapturePreview, error) {
	if err := sendpolicy.BindAddress(cfg.Address, cfg.ApprovedBind); err != nil {
		return CapturePreview{}, err
	}
	if err := cfg.Policy.Validate(); err != nil {
		return CapturePreview{}, err
	}
	// A fault policy names the test endpoints it may be served at. The address
	// is held to them before anything binds, as `readmit collect` holds it, so
	// a collector never listens where its policy did not approve.
	if cfg.Policy.Faults != nil {
		if err := cfg.Policy.Faults.ApproveEndpoint(cfg.Address); err != nil {
			return CapturePreview{}, err
		}
	}
	if cfg.OutputPath == "" {
		return CapturePreview{}, errors.New("collector requires a new case destination")
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = 1 << 20
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	transport := "plain"
	clientCert := cfg.ClientCAPath != ""
	if cfg.TLSCertificatePath != "" || cfg.TLSKeyReference != "" || cfg.SecretsFile != "" {
		if cfg.TLSCertificatePath == "" || cfg.TLSKeyReference == "" || cfg.SecretsFile == "" {
			return CapturePreview{}, errors.New("a TLS listener requires certificate, key reference and secrets together")
		}
		transport = "tls"
	}
	enhanced := "unsupported"
	if cfg.Policy.Enhanced != nil {
		enhanced = cfg.Policy.Enhanced.Operator
	}
	connections := cfg.MaxConnections
	if connections <= 0 {
		connections = 1
	}
	return CapturePreview{
		Kind:              "collect",
		Address:           cfg.Address,
		ApprovedBind:      cfg.ApprovedBind,
		PolicyName:        cfg.Policy.Name,
		PolicySchema:      cfg.Policy.Schema,
		SourceLabel:       cfg.Policy.SourceLabel,
		Acknowledgement:   cfg.Policy.Acknowledgement.Operator + " " + cfg.Policy.Acknowledgement.Code,
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

// PreviewListen validates a SIU fixture configuration without binding.
func PreviewListen(cfg ListenConfig) (CapturePreview, error) {
	if err := sendpolicy.BindAddress(cfg.Address, cfg.ApprovedBind); err != nil {
		return CapturePreview{}, err
	}
	if cfg.Mode != observation.Fixed && cfg.Mode != observation.Defective {
		return CapturePreview{}, errors.New("receiver mode must be fixed or defective")
	}
	if cfg.OutputPath == "" || cfg.ObservationPath == "" {
		return CapturePreview{}, errors.New("fixture requires new case and observation destinations")
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = 1 << 20
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
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
// when the process stops mid-flight.
func StartCollect(ctx context.Context, cfg CollectConfig) (CollectResult, error) {
	preview, err := PreviewCollect(cfg)
	if err != nil {
		return CollectResult{}, err
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = preview.MaxFrameBytes
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	if cfg.ApplicationTimeout <= 0 {
		cfg.ApplicationTimeout = 10 * time.Second
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
		Policy:             cfg.Policy,
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
		cfg.Listening(bound)
	}
	b, serveErr := collector.Serve(ctx, listener)
	out := CollectResult{BoundAddress: bound, Bundle: b, Journal: collector.Journal()}
	return out, serveErr
}

// StartListen binds the SIU fixture receiver and serves until a bound,
// cancellation or error.
func StartListen(ctx context.Context, cfg ListenConfig) (ListenResult, error) {
	preview, err := PreviewListen(cfg)
	if err != nil {
		return ListenResult{}, err
	}
	if cfg.MaxFrameBytes <= 0 {
		cfg.MaxFrameBytes = preview.MaxFrameBytes
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
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
		cfg.Listening(bound)
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

func listenerTLSConfig(ctx context.Context, cfg CollectConfig) (*tls.Config, error) {
	if cfg.TLSCertificatePath == "" && cfg.TLSKeyReference == "" && cfg.SecretsFile == "" && cfg.ClientCAPath == "" {
		return nil, nil
	}
	if cfg.TLSCertificatePath == "" || cfg.TLSKeyReference == "" || cfg.SecretsFile == "" {
		return nil, errors.New("a TLS listener requires certificate, key reference and secrets together")
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

func reserveNewFile(path, taken string) error {
	reserved, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(reserved); !os.IsNotExist(err) {
		return errors.New(taken)
	}
	return nil
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
