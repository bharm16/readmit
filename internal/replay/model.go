// Package replay prepares explicit local replay plans, sends one outstanding
// message at a time, and retains immutable customer-local transport evidence.
package replay

import (
	"bytes"
	"errors"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

const (
	Schema = "readmit-run/v1"
	// TargetSchema is the contract a run manifest's recorded transport is read
	// back under. TargetSchemaV2 adds the credential reference below and
	// TargetSchemaV3 adds the named environment; every version is read
	// unchanged and none is migrated into another.
	TargetSchema   = "readmit-target/v1"
	TargetSchemaV2 = "readmit-target/v2"
	TargetSchemaV3 = "readmit-target/v3"
	MaxMessages    = 4000
	maxRunBytes    = 96 << 20
	maxFileBytes   = 16 << 20
)

// Classification is the class of environment an operator recorded for a named
// endpoint. It is a claim written down, never a property readmit established:
// labelling an endpoint nonproduction is not proof that the address is safe to
// send to. readmit displays it wherever a target is shown and never reads it as
// permission to send anywhere.
//
// It can only ever refuse. Production refuses every replay, in Prepare, before
// a plan exists. Nonproduction grants nothing on its own: what a send may
// actually reach is decided against the addresses the configuration resolves to
// at the moment of the send, in internal/sendpolicy.
type Classification string

const (
	Nonproduction Classification = "nonproduction"
	Production    Classification = "production"
	// Unclassified is an environment whose class nobody recorded. It is
	// reported as its own answer rather than assumed to be nonproduction: an
	// absent claim is not a passing one.
	Unclassified Classification = "unclassified"
)

var classifications = []Classification{Nonproduction, Production, Unclassified}

// Target is explicitly selected configuration, never a discovered endpoint.
// Timeouts are positive Go duration strings, bounded to at most five minutes.
type Target struct {
	Schema            string     `json:"schema"`
	TestEndpoint      bool       `json:"test_endpoint"`
	Address           string     `json:"address"`
	Transport         string     `json:"transport"`
	ApprovedTransport bool       `json:"approved_transport"`
	CAFile            string     `json:"ca_file,omitzero"`
	ConnectTimeout    string     `json:"connect_timeout"`
	MessageTimeout    string     `json:"message_timeout"`
	MaxACKBytes       int        `json:"max_ack_bytes"`
	Credential        Credential `json:"credential,omitzero"`
	// The four members below belong to readmit-target/v3 alone. Name and
	// Classification make the environment a configuration describes visible;
	// ServerName and ClientCertificate complete its TLS setup. A version that
	// never declared them refuses them rather than reading them as its own.
	Name              string         `json:"name,omitzero"`
	Classification    Classification `json:"classification,omitzero"`
	ServerName        string         `json:"server_name,omitzero"`
	ClientCertificate string         `json:"client_certificate,omitzero"`
}

// Environment is the named environment a configuration describes: the name a
// person gave it and the class they recorded for it. The two travel together
// because a class means nothing without the environment it is a class of, and
// a verdict produced against an environment is never separated from it.
type Environment struct {
	Name           string
	Classification Classification
}

// Environment reports what a target records about the environment itself. A
// version that carries no classification member reads as Unclassified, so an
// absent claim is never reported as a nonproduction one.
func (t Target) Environment() Environment {
	recorded := t.Classification
	if recorded == "" {
		recorded = Unclassified
	}
	return Environment{Name: t.Name, Classification: recorded}
}

// Credential names the secret reference this endpoint presents. It carries the
// document that holds the reference and the name of the reference in it, never
// a credential: a target configuration is shared as written, so a value in one
// would be a value in every copy of it. Only readmit-target/v2 may declare one.
type Credential struct {
	SecretsFile string `json:"secrets_file"`
	Reference   string `json:"reference"`
}

// Declared reports whether this target names a credential at all.
func (c Credential) Declared() bool { return c != Credential{} }

// TargetRecord describes the transport used without retaining a CA source path.
type TargetRecord struct {
	Address           string `json:"address"`
	Transport         string `json:"transport"`
	TestEndpoint      bool   `json:"test_endpoint"`
	ApprovedTransport bool   `json:"approved_transport"`
	CASHA256          string `json:"ca_sha256,omitzero"`
	ConnectTimeout    string `json:"connect_timeout"`
	MessageTimeout    string `json:"message_timeout"`
	MaxACKBytes       int    `json:"max_ack_bytes"`
}

// Transformation supports only the named operators documented in docs/replay.md.
type Transformation struct {
	Name  string `json:"name"`
	Shift string `json:"shift,omitzero"`
}

type Options struct {
	Occurrences     []string
	Transformations []Transformation
}

type Change struct {
	Transformation     string    `json:"transformation"`
	SourceOccurrence   string    `json:"source_occurrence"`
	OutboundOccurrence string    `json:"outbound_occurrence"`
	Selector           string    `json:"selector"`
	OldState           hl7.State `json:"old_state"`
	NewState           hl7.State `json:"new_state"`
	Old                []byte    `json:"old_base64"`
	New                []byte    `json:"new_base64"`
}

func cloneChanges(changes []Change) []Change {
	result := make([]Change, len(changes))
	for i, change := range changes {
		result[i] = change
		result[i].Old = bytes.Clone(change.Old)
		result[i].New = bytes.Clone(change.New)
	}
	return result
}

type Mapping struct {
	SourceOccurrence   string `json:"source_occurrence"`
	OutboundOccurrence string `json:"outbound_occurrence"`
	SourceSHA256       string `json:"source_sha256"`
}

// Plan is sealed by Prepare. Inspect it through methods returning private copies;
// caller edits cannot change the validated endpoint or bytes Execute will use.
type Plan struct {
	sourcePath     string
	sourceInfo     os.FileInfo
	sourceIdentity string
	target         Target
	ca             []byte
	options        Options
	messages       []plannedMessage
	changes        []Change
}

type plannedMessage struct {
	mapping   Mapping
	wire      []byte
	source    []byte
	controlID []byte
}

func (p *Plan) Count() int             { return len(p.messages) }
func (p *Plan) SourceIdentity() string { return p.sourceIdentity }
func (p *Plan) Target() TargetRecord   { return targetRecord(p.target, p.ca) }

// Environment is the named environment this plan is pointed at. A run's own
// evidence records the transport it used under the frozen readmit-run/v1
// contract and carries no environment name or classification, so a console
// reads this to state what a send is aimed at.
func (p *Plan) Environment() Environment { return p.target.Environment() }

func (p *Plan) Mappings() []Mapping {
	result := make([]Mapping, len(p.messages))
	for i, message := range p.messages {
		result[i] = message.mapping
	}
	return result
}
func (p *Plan) Outbound(id string) ([]byte, error) {
	for _, message := range p.messages {
		if message.mapping.OutboundOccurrence == id {
			return bytes.Clone(message.wire), nil
		}
	}
	return nil, errors.New("unknown outbound occurrence")
}

type Manifest struct {
	Schema               string           `json:"schema"`
	State                string           `json:"state"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	ExportPolicy         string           `json:"export_policy"`
	SourceBundleIdentity string           `json:"source_bundle_identity"`
	Target               TargetRecord     `json:"target"`
	StartedAt            time.Time        `json:"started_at"`
	CompletedAt          time.Time        `json:"completed_at"`
	MessageCount         int              `json:"message_count"`
	Mappings             []Mapping        `json:"mappings"`
	Transformations      []Transformation `json:"transformations"`
	Changes              []Change         `json:"changes"`
}

type Outcome string

const (
	Accepted          Outcome = "application_accepted"
	ApplicationError  Outcome = "application_error"
	Rejected          Outcome = "application_rejected"
	Timeout           Outcome = "timeout"
	ConnectionRefused Outcome = "connection_refused"
	Disconnect        Outcome = "disconnect"
	TLSError          Outcome = "tls_error"
	ProtocolError     Outcome = "protocol_error"
	Cancelled         Outcome = "cancelled"
	NetworkError      Outcome = "network_error"
	NotAttempted      Outcome = "not_attempted"
)

type TransportError struct {
	Phase string `json:"phase"`
	Class string `json:"class"`
}

// ACK correlation concerns this single outstanding request. It never claims an
// identifier uniquely identifies an occurrence across the source bundle.
type ACK struct {
	Correlation string `json:"correlation"`
	Code        string `json:"code"`
	ControlID   []byte `json:"control_id_base64"`
}

type Event struct {
	SourceOccurrence   string          `json:"source_occurrence"`
	OutboundOccurrence string          `json:"outbound_occurrence"`
	Source             bundle.Payload  `json:"source"`
	Intended           bundle.Payload  `json:"intended"`
	Sent               bundle.Payload  `json:"sent"`
	Received           bundle.Payload  `json:"received"`
	ControlID          []byte          `json:"control_id_base64"`
	Outcome            Outcome         `json:"outcome"`
	Delivery           string          `json:"delivery"`
	ACK                ACK             `json:"ack"`
	ElapsedNS          int64           `json:"elapsed_ns"`
	TransportError     *TransportError `json:"transport_error"`
}

type Run struct {
	Manifest Manifest
	Events   []Event
	Identity string
	payloads map[string][]byte
}

// Raw returns exact bytes referenced by a validated payload descriptor.
func (r *Run) Raw(payload bundle.Payload) ([]byte, error) {
	raw, ok := r.payloads[payload.Path]
	if !ok || len(raw) != payload.Size || digest(raw) != payload.SHA256 {
		return nil, errors.New("unknown run payload")
	}
	return bytes.Clone(raw), nil
}

func (r *Run) Successful() bool {
	if len(r.Events) == 0 {
		return false
	}
	for _, event := range r.Events {
		if event.Outcome != Accepted {
			return false
		}
	}
	return true
}

// Configuration returns the actual sealed configuration, including references
// (never secret values), for versioned durable execution records.
func (p *Plan) Configuration() Target { return p.target }
