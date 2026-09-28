package desktop

import (
	"encoding/json/v2"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Capture sources and mapping presets (#552) are named objects of a project,
// saved whole through SaveItem like every other editor's object. A capture
// source is one of the ways this release collects: a local folder or a
// transfer program, read as an evidence source under an import plan, or an
// MLLP listener with the responder that answers it. Its members are
// published as one revision, so a listener is never started with a responder
// from another save.

// CaptureSourceType is what kind of source a capture reads.
type CaptureSourceType string

// The capture source types.
const (
	LocalFolderSource  CaptureSourceType = "local-folder"
	TransferSource     CaptureSourceType = "transfer"
	MLLPListenerSource CaptureSourceType = "mllp-listener"
	APISource          CaptureSourceType = "api"
)

// captureSourceTypes are the types a source editor offers, in its order,
// and whether this release can save and start each one.
var captureSourceTypes = []CaptureSourceTypeChoice{
	{Type: LocalFolderSource, Available: true},
	{Type: TransferSource, Available: true},
	{Type: MLLPListenerSource, Available: true},
	{Type: APISource, Available: false},
}

// CaptureSourceTypeChoice is one capture source type and whether this
// release can save and start a source of it.
type CaptureSourceTypeChoice struct {
	Type      CaptureSourceType `json:"type"`
	Available bool              `json:"available"`
}

// ListenerSchema is the contract of a saved MLLP listener's settings.
const ListenerSchema = "readmit-capture-listener/v1"

// ListenerTransport is how an MLLP listener accepts connections.
type ListenerTransport string

// The listener transports, in the order a person is offered them.
const (
	PlainTransport     ListenerTransport = "plain"
	TLSTransport       ListenerTransport = "tls"
	MutualTLSTransport ListenerTransport = "mutual-tls"
)

var listenerTransports = []ListenerTransport{PlainTransport, TLSTransport, MutualTLSTransport}

// AckCode is the original-mode acknowledgement code a listener answers with.
type AckCode string

// The acknowledgement codes a listener answers with, in the order a person
// is offered them.
const (
	AcceptAck           AckCode = collection.AcceptCode
	ApplicationErrorAck AckCode = collection.ApplicationErrorCode
	RejectAck           AckCode = collection.RejectCode
)

var ackCodes = []AckCode{AcceptAck, ApplicationErrorAck, RejectAck}

// apiUnavailable is why an API source cannot be saved or started.
const apiUnavailable = "API sources are unavailable: this release has no supported API collection path"

// CaptureSourceDraft is the whole of one capture source. A local folder or
// transfer program carries its evidence source and the import plan its
// collection is read under; an MLLP listener carries its listener settings
// and, optionally, its responder, which a save composes from the ACK code
// when it is left out.
type CaptureSourceDraft struct {
	Type      CaptureSourceType      `json:"type"`
	Evidence  *evidencesource.Source `json:"evidence,omitzero"`
	Plan      *importer.Plan         `json:"plan,omitzero"`
	Listener  *ListenerSettings      `json:"listener,omitzero"`
	Responder *collection.Policy     `json:"responder,omitzero"`
	// ResponderChoices is the listener's responder as the Responder sheet's
	// controls express it; a save composes the published responder from
	// them, and OpenItemDraft answers them for a saved source. When a draft
	// carries them, Responder is ignored.
	ResponderChoices *ResponderChoices `json:"responder_choices,omitzero"`
}

// ResponderChoices is what a person chose for a listener's responder: its
// name and label (the listener's own by default), the message types it
// acknowledges (any by default), whether it answers enhanced mode with the
// fixed codes, and one simulated fault step (Fault is "none" or empty for
// none) with its delay. The acknowledgement code is the listener's ack_code,
// and a fault is held to the listener's exact bind address and port, a
// loopback test receiver; neither is chosen here.
type ResponderChoices struct {
	Name                 string                     `json:"name,omitzero"`
	SourceLabel          string                     `json:"source_label,omitzero"`
	AcceptedMessageTypes collection.MessageTypeRule `json:"accepted_message_types,omitzero"`
	Enhanced             bool                       `json:"enhanced"`
	Fault                string                     `json:"fault,omitzero"`
	FaultDelayMS         int                        `json:"fault_delay_ms,omitzero"`
}

// responderChoices is what the Responder sheet shows for a saved responder,
// decided as ReadReceiverPolicy decides a policy's controls.
func responderChoices(policy collection.Policy) *ResponderChoices {
	decided := policyChoices(policy)
	return &ResponderChoices{Name: decided.Name, SourceLabel: decided.SourceLabel, AcceptedMessageTypes: decided.AcceptedMessageTypes,
		Enhanced: decided.Enhanced, Fault: decided.Fault, FaultDelayMS: decided.FaultDelayMS}
}

// composeResponder composes the responder the choices describe for listener,
// through the composer the older Responder panel saves with. saved is the
// responder of the revision this save edits, if any: while the choices still
// say what it declares, what the controls cannot express is kept.
func composeResponder(listener ListenerSettings, chosen ResponderChoices, saved *collection.Policy) (collection.Policy, *FieldProblem) {
	fault := chosen.Fault
	if fault == "" {
		fault = noFault
	}
	if fault != noFault {
		if !slices.ContainsFunc(collection.FaultActions(), func(known collection.FaultAction) bool { return known.Action == fault }) {
			return collection.Policy{}, &FieldProblem{Field: "source.responder.faults", Problem: "choose a simulated fault this release offers, or none"}
		}
		if ip, err := netip.ParseAddr(listener.BindAddress); err != nil || !ip.IsLoopback() || listener.Port == 0 {
			return collection.Policy{}, &FieldProblem{Field: "source.responder.faults", Problem: "a simulated fault answers only on a loopback test listener with a fixed port"}
		}
	}
	defaults := responderFor(listener)
	choices := ReceiverPolicyChoices{
		Name: cmpOr(chosen.Name, defaults.Name), SourceLabel: cmpOr(chosen.SourceLabel, defaults.SourceLabel),
		Acknowledgement: defaults.Acknowledgement, AcceptedMessageTypes: chosen.AcceptedMessageTypes,
		Enhanced: chosen.Enhanced, Fault: fault, FaultDelayMS: chosen.FaultDelayMS, Endpoint: listener.Address(), Opened: saved,
	}
	if choices.AcceptedMessageTypes.Operator == "" {
		choices.AcceptedMessageTypes = defaults.AcceptedMessageTypes
	}
	if choices.AcceptedMessageTypes.Values == nil {
		choices.AcceptedMessageTypes.Values = []string{}
	}
	if saved != nil && saved.Acknowledgement.Code == string(listener.AckCode) {
		choices.Acknowledgement = saved.Acknowledgement
	}
	return choices.policy(), nil
}

// ListenerSettings are an MLLP listener's saved settings. The address is the
// bind address and port; a bind beyond this machine needs AllowRemote. TLS
// names project files and a credential reference, never a key.
// MessageLimit 0 runs until stopped; ConnectionLimit 0 is one at a time.
type ListenerSettings struct {
	Schema          string            `json:"schema"`
	BindAddress     string            `json:"bind_address"`
	Port            int               `json:"port"`
	Transport       ListenerTransport `json:"transport"`
	MessageLimit    int               `json:"message_limit"`
	ConnectionLimit int               `json:"connection_limit"`
	IdleTimeout     string            `json:"idle_timeout"`
	AckCode         AckCode           `json:"ack_code"`
	AllowRemote     bool              `json:"allow_remote"`
	TLSCertificate  string            `json:"tls_certificate,omitzero"`
	TLSKeyReference string            `json:"tls_key_reference,omitzero"`
	SecretsFile     string            `json:"secrets_file,omitzero"`
	ClientCA        string            `json:"client_ca,omitzero"`
}

// Address is where the listener binds.
func (l ListenerSettings) Address() string {
	return net.JoinHostPort(l.BindAddress, strconv.Itoa(l.Port))
}

// defaultListener is what a new MLLP listener starts from: loopback, a port
// the system chooses, plain MLLP and accept.
func defaultListener() ListenerSettings {
	return ListenerSettings{Schema: ListenerSchema, BindAddress: "127.0.0.1", Port: 0, Transport: PlainTransport,
		ConnectionLimit: 1, IdleTimeout: operation.DefaultIdleTimeout.String(), AckCode: collection.AcceptCode}
}

// defaultCaptureSource is what a new capture source of a type starts from.
func defaultCaptureSource() *CaptureSourceDraft {
	listener := defaultListener()
	return &CaptureSourceDraft{Type: MLLPListenerSource, Listener: &listener}
}

// Where a new folder or transfer source starts: bounded reads, each entry read
// once, and the default import plan. What has no default — the source's name
// and scope, the folder it reads, the transfer program and the address it
// reaches — is left empty for the person to choose; a transfer source's class
// starts unclassified, which claims nothing.
var (
	startQuota = evidencesource.Quota{MaxEntries: 64, MaxEntryBytes: 4 << 20, MaxTotalBytes: 32 << 20}
	startRetry = evidencesource.Retry{Attempts: 1, Backoff: "250ms"}
)

// captureSourceStarts is where the source editor starts each type it can
// save, in the order it offers them: a local folder, a transfer program and
// an MLLP listener with the responder its ACK code composes. An API source is
// not here: it cannot be saved or started in this release.
func captureSourceStarts() []CaptureSourceDraft {
	folder := evidencesource.Source{Schema: evidencesource.Schema, Kind: evidencesource.Directory, Quota: startQuota, Retry: startRetry}
	transfer := evidencesource.Source{Schema: evidencesource.Schema, Kind: evidencesource.Transfer, Quota: startQuota, Retry: startRetry,
		Classification: evidencesource.Unclassified}
	folderPlan, transferPlan := operation.DefaultImportPlan(), operation.DefaultImportPlan()
	listener := defaultListener()
	return []CaptureSourceDraft{
		{Type: LocalFolderSource, Evidence: &folder, Plan: &folderPlan},
		{Type: TransferSource, Evidence: &transfer, Plan: &transferPlan},
		{Type: MLLPListenerSource, Listener: &listener, ResponderChoices: responderChoices(responderFor(listener))},
	}
}

// decodeListener reads saved listener settings strictly.
func decodeListener(data []byte) (ListenerSettings, error) {
	var listener ListenerSettings
	if err := json.Unmarshal(data, &listener, json.RejectUnknownMembers(true)); err != nil {
		return ListenerSettings{}, errors.New("invalid listener settings")
	}
	if listener.Schema != ListenerSchema {
		return ListenerSettings{}, errors.New("unsupported listener settings version")
	}
	if problems := listenerProblems(listener); len(problems) > 0 {
		return ListenerSettings{}, errors.New(problems[0].Problem)
	}
	return listener, nil
}

// listenerProblems is every reason listener settings cannot be served.
func listenerProblems(listener ListenerSettings) []FieldProblem {
	problems := []FieldProblem{}
	add := func(field, problem string) {
		problems = append(problems, FieldProblem{Field: "source.listener." + field, Problem: problem})
	}
	if listener.BindAddress == "" {
		add("bind_address", "enter the address to listen on")
	}
	if listener.Port < 0 || listener.Port > 65535 {
		add("port", "a port is 0 to 65535")
	}
	if listener.BindAddress != "" && listener.Port >= 0 && listener.Port <= 65535 {
		switch err := sendpolicy.BindAddress(listener.Address(), listener.AllowRemote); {
		case errors.Is(err, sendpolicy.ErrNonloopbackBind):
			add("allow_remote", "listening beyond this machine requires Allow remote connections")
		case err != nil:
			add("bind_address", "enter an IP address, or localhost")
		}
	}
	switch listener.Transport {
	case PlainTransport:
		if listener.TLSCertificate != "" || listener.TLSKeyReference != "" || listener.SecretsFile != "" || listener.ClientCA != "" {
			add("transport", "plain MLLP carries no TLS settings")
		}
	case TLSTransport, MutualTLSTransport:
		if listener.TLSCertificate == "" {
			add("tls_certificate", "choose the listener certificate")
		}
		if listener.TLSKeyReference == "" || listener.SecretsFile == "" {
			add("tls_key_reference", "choose the credential reference of the private key")
		}
		if listener.Transport == MutualTLSTransport && listener.ClientCA == "" {
			add("client_ca", "choose the client certificate authority")
		}
		if listener.Transport == TLSTransport && listener.ClientCA != "" {
			add("client_ca", "only mutual TLS names a client certificate authority")
		}
	default:
		add("transport", "choose Plain MLLP, TLS or Mutual TLS")
	}
	if listener.MessageLimit < 0 {
		add("message_limit", "a message limit is 0 or more")
	}
	if listener.ConnectionLimit < 0 || listener.ConnectionLimit > 64 {
		add("connection_limit", "a connection limit is 0 to 64")
	}
	if listener.IdleTimeout != "" {
		if d, err := time.ParseDuration(listener.IdleTimeout); err != nil || d <= 0 {
			add("idle_timeout", "an idle timeout is a positive duration such as 30s")
		}
	}
	if listener.AckCode != collection.AcceptCode && listener.AckCode != collection.ApplicationErrorCode && listener.AckCode != collection.RejectCode {
		add("ack_code", "choose AA, AE or AR")
	}
	return problems
}

// projectEntry is the entry name of a file directly in the project folder,
// named by that name or by a path to it, as the file dialog answers: a
// regular file, not a link, and never one inside a folder of the project or
// outside it.
func (s draftScope) projectEntry(field, value string) (string, *FieldProblem) {
	problem := &FieldProblem{Field: field, Problem: "choose a file directly in the project folder"}
	if s.root == "" {
		return "", problem
	}
	name := value
	if filepath.IsAbs(value) {
		folder, err := filepath.EvalSymlinks(filepath.Dir(value))
		root, rootErr := filepath.EvalSymlinks(s.root)
		if err != nil || rootErr != nil || folder != root {
			return "", problem
		}
		name = filepath.Base(value)
	}
	if _, err := artifactpath.File(s.root, name); err != nil {
		return "", problem
	}
	return name, nil
}

// listenerCredential checks the credential a TLS listener presents its
// certificate's private key from, as the listener binds it when it starts
// (secret.Bind): a reference the project registers, for an MLLP endpoint,
// scoped to the exact address the listener binds.
func (s draftScope) listenerCredential(listener ListenerSettings) *FieldProblem {
	if listener.TLSKeyReference == "" {
		return nil
	}
	problem := func(text string) *FieldProblem {
		return &FieldProblem{Field: "source.listener.tls_key_reference", Problem: text}
	}
	if filepath.Clean(listener.SecretsFile) != ProjectSecrets {
		return problem("the private key is named by one of this project's credentials")
	}
	if s.root == "" {
		return problem("the project registers no credentials")
	}
	document, err := operation.ReadSecrets(filepath.Join(s.root, ProjectSecrets))
	if err != nil {
		return problem("the project registers no credentials")
	}
	reference, err := secret.Find(document, listener.TLSKeyReference)
	switch {
	case err != nil:
		return problem(err.Error())
	case reference.Purpose != secret.MLLPEndpoint:
		return problem("that credential is registered for another purpose; a listener presents an MLLP endpoint credential")
	case reference.Address != listener.Address():
		return problem("that credential is scoped to " + reference.Address + ", not the address this listener binds")
	}
	return nil
}

// responderFor composes the responder a listener without one answers with:
// its fixed ACK code to any message type.
func responderFor(listener ListenerSettings) collection.Policy {
	return collection.Policy{Schema: collection.PolicySchemaV1, Name: "listener", SourceLabel: "listener",
		Acknowledgement:      collection.AckRule{Operator: collection.FixedCodeOperator, Code: string(listener.AckCode)},
		AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}
}

// validateCaptureSource validates a capture source draft and answers the
// members a save stages, the normalized draft, or every problem.
func validateCaptureSource(scope draftScope, draft *CaptureSourceDraft) ([]catalog.Staged, *CaptureSourceDraft, []FieldProblem) {
	problems := []FieldProblem{}
	switch draft.Type {
	case APISource:
		return nil, nil, []FieldProblem{{Field: "source.type", Problem: apiUnavailable}}
	case LocalFolderSource, TransferSource:
		if draft.Listener != nil || draft.Responder != nil {
			problems = append(problems, FieldProblem{Field: "source.type", Problem: "a folder or transfer source carries no listener or responder"})
		}
		if draft.Evidence == nil {
			return nil, nil, append(problems, FieldProblem{Field: "source.evidence", Problem: "a folder or transfer source declares where it reads"})
		}
		evidence := *draft.Evidence
		if evidence.Schema == "" {
			evidence.Schema = evidencesource.Schema
		}
		want := evidencesource.Directory
		if draft.Type == TransferSource {
			want = evidencesource.Transfer
		}
		if evidence.Kind != want {
			problems = append(problems, FieldProblem{Field: "source.evidence.kind", Problem: "the source's kind must be " + string(want) + " for this type"})
		}
		var sourceData []byte
		if err := evidence.Validate(); err != nil {
			problems = append(problems, FieldProblem{Field: "source.evidence", Problem: err.Error()})
		} else if data, err := json.Marshal(evidence, json.Deterministic(true)); err != nil {
			problems = append(problems, FieldProblem{Field: "source.evidence", Problem: "the source cannot be encoded"})
		} else if _, err := evidencesource.Decode(data); err != nil {
			problems = append(problems, FieldProblem{Field: "source.evidence", Problem: err.Error()})
		} else {
			sourceData = append(data, '\n')
		}
		plan := operation.DefaultImportPlan()
		if draft.Plan != nil {
			plan = *draft.Plan
		}
		if plan.Schema == "" {
			plan.Schema = importer.PlanSchema
		}
		var planData []byte
		if err := plan.Validate(); err != nil {
			problems = append(problems, FieldProblem{Field: "source.plan", Problem: err.Error()})
		} else if data, err := json.Marshal(plan, json.Deterministic(true)); err != nil {
			problems = append(problems, FieldProblem{Field: "source.plan", Problem: "the format cannot be encoded"})
		} else if _, err := importer.DecodePlan(data); err != nil {
			problems = append(problems, FieldProblem{Field: "source.plan", Problem: err.Error()})
		} else {
			planData = append(data, '\n')
		}
		if len(problems) > 0 {
			return nil, nil, problems
		}
		return []catalog.Staged{{Role: "source", File: "source.json", Data: sourceData}, {Role: "plan", File: "plan.json", Data: planData}},
			&CaptureSourceDraft{Type: draft.Type, Evidence: &evidence, Plan: &plan}, problems
	case MLLPListenerSource:
		if draft.Evidence != nil || draft.Plan != nil {
			problems = append(problems, FieldProblem{Field: "source.type", Problem: "an MLLP listener carries no evidence source or plan"})
		}
		if draft.Listener == nil {
			return nil, nil, append(problems, FieldProblem{Field: "source.listener", Problem: "an MLLP listener declares where it listens"})
		}
		listener := *draft.Listener
		listener.Schema = ListenerSchema
		if listener.IdleTimeout == "" {
			listener.IdleTimeout = operation.DefaultIdleTimeout.String()
		}
		// The private key is one of the project's named credentials, as an
		// environment's is: the reference names it in the project's secrets
		// entry, which a draft need not name.
		if listener.TLSKeyReference != "" && listener.SecretsFile == "" {
			listener.SecretsFile = ProjectSecrets
		}
		// A certificate and a client authority are files directly in the
		// project folder, recorded by their entry name; a path the file
		// dialog chose is taken as the entry it names there.
		for _, file := range []struct {
			field string
			value *string
		}{{"tls_certificate", &listener.TLSCertificate}, {"client_ca", &listener.ClientCA}} {
			if *file.value == "" {
				continue
			}
			entry, problem := scope.projectEntry("source.listener."+file.field, *file.value)
			if problem != nil {
				problems = append(problems, *problem)
				continue
			}
			*file.value = entry
		}
		problems = append(problems, listenerProblems(listener)...)
		if problem := scope.listenerCredential(listener); problem != nil {
			problems = append(problems, *problem)
		}
		responder := responderFor(listener)
		if draft.ResponderChoices != nil {
			composed, problem := composeResponder(listener, *draft.ResponderChoices, scope.savedResponder())
			if problem != nil {
				return nil, nil, append(problems, *problem)
			}
			responder = composed
		} else if draft.Responder != nil {
			responder = *draft.Responder
			if responder.Acknowledgement.Code != string(listener.AckCode) {
				problems = append(problems, FieldProblem{Field: "source.responder.acknowledgement", Problem: "the responder's ACK code must be the listener's"})
			}
		}
		responderData, err := collection.EncodePolicy(responder)
		if err == nil {
			_, err = collection.DecodePolicy(responderData)
		}
		if err != nil {
			field := "source.responder"
			if responder.Faults != nil {
				field = "source.responder.faults"
			}
			problems = append(problems, FieldProblem{Field: field, Problem: err.Error()})
		} else if responder.Faults != nil && len(problems) == 0 {
			if err := responder.Faults.ApproveEndpoint(listener.Address()); err != nil {
				problems = append(problems, FieldProblem{Field: "source.responder.faults", Problem: err.Error()})
			}
		}
		listenerData, encodeErr := json.Marshal(listener, json.Deterministic(true))
		if encodeErr != nil {
			problems = append(problems, FieldProblem{Field: "source.listener", Problem: "the listener cannot be encoded"})
		}
		if len(problems) > 0 {
			return nil, nil, problems
		}
		return []catalog.Staged{{Role: "listener", File: "listener.json", Data: append(listenerData, '\n')},
				{Role: "responder", File: "responder.json", Data: append(responderData, '\n')}},
			&CaptureSourceDraft{Type: draft.Type, Listener: &listener, Responder: &responder, ResponderChoices: responderChoices(responder)}, problems
	}
	return nil, nil, []FieldProblem{{Field: "source.type", Problem: "choose Local folder, Transfer program or MLLP listener"}}
}

// savedResponder is the responder of the capture source this save edits, as
// its current revision holds it, or nil for a new one.
func (s draftScope) savedResponder() *collection.Policy {
	if s.loaded == nil || s.item == "" {
		return nil
	}
	index := s.loaded.document.Find(s.item)
	if index < 0 {
		return nil
	}
	paths, availability, _ := s.loaded.backing(s.loaded.document.Items[index])
	if availability != ItemAvailable {
		return nil
	}
	source, err := readCaptureSource(paths)
	if err != nil || source.Responder == nil {
		return nil
	}
	return source.Responder
}

// verifyCaptureSource reads a staged capture source through the readers a
// capture reads it with.
func verifyCaptureSource(files map[string]string) error {
	_, err := readCaptureSource(files)
	return err
}

// readCaptureSource reads a saved capture source's members.
func readCaptureSource(files map[string]string) (*CaptureSourceDraft, error) {
	if path, held := files["listener"]; held {
		data, err := boundedFile(path, catalog.MaxMemberBytes)
		if err != nil {
			return nil, err
		}
		listener, err := decodeListener(data)
		if err != nil {
			return nil, err
		}
		responder, err := operation.ReceiverPolicyRead(files["responder"])
		if err != nil {
			return nil, err
		}
		return &CaptureSourceDraft{Type: MLLPListenerSource, Listener: &listener, Responder: &responder, ResponderChoices: responderChoices(responder)}, nil
	}
	source, err := operation.SourceRead(files["source"])
	if err != nil {
		return nil, err
	}
	data, err := boundedFile(files["plan"], importer.MaxPlanBytes)
	if err != nil {
		return nil, err
	}
	plan, err := importer.DecodePlan(data)
	if err != nil {
		return nil, err
	}
	kind := LocalFolderSource
	if source.Kind == evidencesource.Transfer {
		kind = TransferSource
	}
	return &CaptureSourceDraft{Type: kind, Evidence: &source, Plan: &plan}, nil
}

// validateMapping validates a mapping preset and answers the member a save
// stages.
func validateMapping(recipe *importer.Recipe) ([]catalog.Staged, []FieldProblem) {
	declared := *recipe
	if declared.Schema == "" {
		declared.Schema = importer.RecipeSchema
	}
	if err := declared.Validate(); err != nil {
		return nil, []FieldProblem{{Field: "mapping", Problem: err.Error()}}
	}
	data, err := json.Marshal(declared, json.Deterministic(true))
	if err == nil {
		_, err = importer.DecodeRecipe(data)
	}
	if err != nil {
		return nil, []FieldProblem{{Field: "mapping", Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: "recipe", File: "mapping.json", Data: append(data, '\n')}}, nil
}

// readMapping reads a saved mapping preset.
func readMapping(path string) (importer.Recipe, error) {
	data, err := boundedFile(path, importer.MaxRecipeBytes)
	if err != nil {
		return importer.Recipe{}, err
	}
	return importer.DecodeRecipe(data)
}

// MappingSummary is a mapping preset's envelope.
type MappingSummary struct {
	Envelope string `json:"envelope"`
}

// SourceSummary is a capture source's type and, for a listener, where it
// binds and its transport.
type SourceSummary struct {
	Type      CaptureSourceType `json:"type"`
	Address   string            `json:"address,omitzero"`
	Transport ListenerTransport `json:"transport,omitzero"`
}

func readMappingItem(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
	recipe, err := readMapping(paths["recipe"])
	if err != nil {
		return view{}, err
	}
	return view{name: recipe.Name, summary: ItemSummary{Mapping: &MappingSummary{Envelope: string(recipe.Envelope)}}}, nil
}

func readSourceItem(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
	source, err := readCaptureSource(paths)
	if err != nil {
		return view{}, err
	}
	summary := &SourceSummary{Type: source.Type}
	name := ""
	if source.Listener != nil {
		summary.Address, summary.Transport = source.Listener.Address(), source.Listener.Transport
	} else {
		name = source.Evidence.Name
	}
	return view{name: name, summary: ItemSummary{Source: summary}}, nil
}
