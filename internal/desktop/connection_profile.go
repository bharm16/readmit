package desktop

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// FHIRConnectionSchema is a named connection's local adapter contract. It is
// distinct from the frozen MLLP target and grants no authority. Its members
// name public TLS/JWK files and a registered key reference, never key material.
const FHIRConnectionSchema = "readmit-fhir-connection/v1"

type FHIRConnection struct {
	Validation     *ConnectionValidation `json:"validation,omitzero"`
	Schema         string                `json:"schema"`
	Base           string                `json:"base"`
	Version        string                `json:"version"`
	Classification replay.Classification `json:"classification"`
	Authentication string                `json:"authentication"`
	ServerName     string                `json:"server_name"`
	CAFile         string                `json:"ca_file,omitzero"`
	ClientID       string                `json:"client_id,omitzero"`
	TokenEndpoint  string                `json:"token_endpoint,omitzero"`
	Algorithm      string                `json:"algorithm,omitzero"`
	Scopes         []string              `json:"scopes,omitzero"`
	KeyReference   string                `json:"key_reference,omitzero"`
	KeyID          string                `json:"key_id,omitzero"`
	PublicKeysFile string                `json:"public_keys_file,omitzero"`
}

// ConnectionValidation selects an installed local worker capability. This is
// a local validation boundary, never a remote connection or a worker session.
type ConnectionValidation struct {
	Capability string `json:"capability"`
	Engine     string `json:"engine"`
	Socket     string `json:"socket,omitzero"`
}

func (c FHIRConnection) validatorState() string {
	if c.Validation == nil || c.Validation.Capability == "" || c.Validation.Engine == "none" {
		return "not-configured"
	}
	if _, err := fhirvalidator.OpenCapability(c.Validation.Capability); err != nil {
		return "capability-unavailable"
	}
	return "capability-installed-worker-not-checked"
}

// FHIRCapabilityCheck is a dated recorded claim, bound to the revision that
// was explicitly checked. Reading it neither contacts the server nor refreshes
// authorization. A revision mismatch is exposed as stale by the view.
type FHIRCapabilityCheck struct {
	Revision  string               `json:"revision"`
	CheckedAt string               `json:"checked_at"`
	Outcome   string               `json:"outcome"`
	Claims    *fhirr4.Capabilities `json:"claims,omitzero"`
}

func (c FHIRConnection) problems() []FieldProblem {
	problems := []FieldProblem{}
	add := func(field, reason string) {
		problems = append(problems, FieldProblem{Field: "fhir." + field, Problem: reason})
	}
	if c.Schema != FHIRConnectionSchema || c.Version != fhirr4.Version {
		add("version", "Choose FHIR R4 4.0.1")
	}
	if !fhirrequest.ValidBase(c.Base) {
		add("base", "Choose an HTTPS FHIR base URL without a query or trailing slash")
	}
	if !slices.Contains([]replay.Classification{replay.Unclassified, replay.Nonproduction, replay.Production}, c.Classification) {
		add("classification", "Choose a classification")
	}
	if c.ServerName == "" || len(c.ServerName) > 253 || strings.ContainsAny(c.ServerName, " /\\\r\n") {
		add("server_name", "Enter the TLS server name")
	}
	if !slices.Contains([]string{"none", "smart"}, c.Authentication) {
		add("authentication", "Choose an authentication mode")
	}
	if c.Authentication == "smart" {
		if c.ClientID == "" || len(c.ClientID) > 256 || strings.ContainsAny(c.ClientID, "\r\n\x00") {
			add("client_id", "Enter the registered client ID")
		}
		if !fhirrequest.ValidBase(c.TokenEndpoint) {
			add("token_endpoint", "Enter the HTTPS token endpoint registered for this client")
		}
		if !slices.Contains([]string{"RS384", "ES384"}, c.Algorithm) {
			add("algorithm", "Choose RS384 or ES384")
		}
		if c.KeyReference == "" || c.KeyID == "" || c.PublicKeysFile == "" {
			add("key_reference", "Choose a named signing key reference and its public keys")
		}
		if len(c.Scopes) < 1 || len(c.Scopes) > 64 {
			add("scopes", "Declare the required system scopes")
		}
	}
	if c.Validation != nil && !slices.Contains([]string{"local", "none"}, c.Validation.Engine) {
		add("validation", "Choose local offline validation or no worker")
	}
	return problems
}

func readFHIRConnectionFile(path string) (FHIRConnection, error) {
	raw, err := savedFile.Read(path)
	var c FHIRConnection
	if err != nil || json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || len(c.problems()) > 0 {
		return c, errors.New("the FHIR connection cannot be read")
	}
	return c, nil
}

func validateFHIRConnectionDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	connection := *draft.FHIR
	if connection.Schema == "" {
		connection.Schema = FHIRConnectionSchema
	}
	if connection.Version == "" {
		connection.Version = fhirr4.Version
	}
	normalized := ItemDraft{Name: draft.Name, FHIR: &connection}
	problems := connection.problems()
	if draft.Environment != nil {
		problems = append(problems, FieldProblem{Field: "fhir", Problem: "Choose one connection protocol"})
	}
	name, problem := scope.environmentName(draft.Name, "")
	if problem != nil {
		problems = append(problems, *problem)
	}
	raw, err := encodeMember(connection)
	if err != nil {
		return nil, normalized, append(problems, FieldProblem{Field: "fhir", Problem: err.Error()})
	}
	staged := []catalog.Staged{{Role: "target", File: "connection.json", Data: raw}}
	if draft.SendPolicy != nil {
		policy := *draft.SendPolicy
		if policy.Schema == "" {
			policy.Schema = sendpolicy.PolicySchema
		}
		if policy.ApprovedDestinations == nil {
			policy.ApprovedDestinations = []string{}
		}
		raw, err := encodeMember(policy)
		if err == nil {
			policy, err = sendpolicy.DecodePolicy(raw)
		}
		if err != nil {
			problems = append(problems, FieldProblem{Field: "policy", Problem: err.Error()})
		} else {
			normalized.SendPolicy = &policy
			staged = append(staged, catalog.Staged{Role: "policy", File: "policy.json", Data: raw})
		}
	}
	links := EnvironmentLinks{Schema: EnvironmentLinksSchema}
	if draft.Links != nil {
		links = *draft.Links
		links.Schema = EnvironmentLinksSchema
	}
	if links.Observation != "" && !scope.holdsObservation(links.Observation) {
		problems = append(problems, FieldProblem{Field: "links.observation", Problem: "the project holds no such observation"})
	}
	if draft.ResetPlan != nil {
		plan, found := normalizedPlan(*draft.ResetPlan, name, name, links.ActionNames)
		found = append(found, scope.namedObservationSources(&plan)...)
		problems = append(problems, found...)
		raw, err := encodeMember(plan)
		if err == nil {
			plan, err = fixturereset.DecodePlan(raw)
		}
		if err != nil {
			problems = append(problems, FieldProblem{Field: "reset", Problem: err.Error()})
		} else {
			normalized.ResetPlan = &plan
			staged = append(staged, catalog.Staged{Role: "reset", File: "reset.json", Data: raw})
		}
	}
	if draft.Links != nil {
		raw, err := encodeMember(links)
		if err == nil {
			links, err = decodeLinks(raw)
		}
		if err != nil {
			problems = append(problems, FieldProblem{Field: "links", Problem: err.Error()})
		} else {
			normalized.Links = &links
			staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: raw})
		}
	}
	return staged, normalized, problems
}

func verifyFHIRConnection(files map[string]string) error {
	if path, held := files["isolation"]; held {
		if _, err := readEnvironmentIsolation(path); err != nil {
			return err
		}
	}
	if _, err := readFHIRConnectionFile(files["target"]); err != nil {
		return err
	}
	if path, held := files["policy"]; held {
		if _, err := operation.ReadSendPolicy(path); err != nil {
			return err
		}
	}
	if path, held := files["reset"]; held {
		if _, err := operation.ReadResetPlan(path); err != nil {
			return err
		}
	}
	if path, held := files["links"]; held {
		if _, err := readLinks(path); err != nil {
			return err
		}
	}
	return nil
}

func (c *loadedCatalog) fhirEnvironmentOf(paths map[string]string) (*environmentMembers, error) {
	if err := verifyFHIRConnection(paths); err != nil {
		return nil, err
	}
	connection, err := readFHIRConnectionFile(paths["target"])
	if err != nil {
		return nil, err
	}
	members := &environmentMembers{fhir: &connection, paths: paths}
	if path, held := paths["isolation"]; held {
		isolation, err := readEnvironmentIsolation(path)
		if err != nil {
			return nil, err
		}
		members.isolation = &isolation
	}
	if path, held := paths["policy"]; held {
		policy, err := operation.ReadSendPolicy(path)
		if err != nil {
			return nil, err
		}
		members.policy = &policy
	}
	if path, held := paths["reset"]; held {
		reset, err := operation.ReadResetPlan(path)
		if err != nil {
			return nil, err
		}
		members.reset = &reset
	}
	if path, held := paths["links"]; held {
		if members.links, err = readLinks(path); err != nil {
			return nil, err
		}
	}
	return members, nil
}

func readFHIRConnection(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	members, err := c.fhirEnvironmentOf(paths)
	if err != nil {
		return view{}, err
	}
	fhir := members.fhir
	summary := &EnvironmentSummary{Protocol: "fhir-r4", Version: fhir.Version, Authentication: fhir.Authentication, Classification: string(fhir.Classification), Address: fhir.Base, Transport: "https", HasPolicy: members.policy != nil, ResetName: members.links.ResetName}
	summary.Validator = fhir.validatorState()
	if members.isolation != nil {
		summary.IsolationName = members.isolation.Name
		if state, err := readIsolationState(c.root, item.ID); err == nil {
			summary.IsolationOutcome = state.Outcome
		}
	}
	if members.reset != nil {
		summary.ResetActions = len(members.reset.Actions)
	}
	if record, err := readFHIRCheck(c.root, item.ID, CheckFHIRCapabilitiesAction); err == nil {
		summary.Capabilities = &record.Check
	}
	if record, err := readFHIRCheck(c.root, item.ID, CheckFHIRAuthorizationAction); err == nil {
		summary.Authorization = &record.Check
	}
	if record, err := readFHIRCheck(c.root, item.ID, CheckFHIRConnectionAction); err == nil {
		summary.LastCheckedAt = &record.Check.CheckedAt
		summary.LastCheckRevision = record.Check.Revision
		summary.LastCheckOutcome = record.Check.Outcome
	}
	if index := c.document.Find(members.links.Observation); members.links.Observation != "" && index >= 0 && !c.removed(c.document.Items[index]) {
		linked := c.document.Items[index]
		summary.Observation = &ItemRef{Kind: ObservationItem, ID: linked.ID, Revision: linked.RevisionLabel()}
		summary.ObservationName = linked.Name
	}
	return view{name: item.Name, summary: ItemSummary{Environment: summary}}, nil
}
