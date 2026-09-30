package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
)

const fhirCheckSchema = "readmit-fhir-connection-check/v1"

type FHIRConnectionReview struct {
	Protocol       string   `json:"protocol"`
	Authentication string   `json:"authentication"`
	ClientID       string   `json:"client_id,omitzero"`
	Scopes         []string `json:"scopes"`
	KeyReference   string   `json:"key_reference,omitzero"`
	Effect         string   `json:"effect"`
}

type fhirCheckRecord struct {
	Schema     string              `json:"schema"`
	Item       string              `json:"item"`
	Action     ActionID            `json:"action"`
	Check      FHIRCapabilityCheck `json:"check"`
	Capability []byte              `json:"capability,omitzero"`
}

var fhirCheckFile = artifactdir.Document{MaxBytes: 2 << 20}

func fhirCheckPath(root, item string, action ActionID) string {
	return filepath.Join(root, catalog.Folder, checksFolder, item+"-"+string(action)+".json")
}
func readFHIRCheck(root, item string, action ActionID) (fhirCheckRecord, error) {
	raw, err := fhirCheckFile.Read(fhirCheckPath(root, item, action))
	var record fhirCheckRecord
	if err != nil || json.Unmarshal(raw, &record, json.RejectUnknownMembers(true)) != nil || record.Schema != fhirCheckSchema || record.Item != item || record.Action != action {
		return record, errors.New("no retained check")
	}
	return record, nil
}
func retainFHIRCheck(root string, record fhirCheckRecord) error {
	path := fhirCheckPath(root, record.Item, record.Action)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := encodeMember(record)
	if err != nil {
		return err
	}
	return fhirCheckFile.Replace(path, raw)
}

type fhirCheckBinding struct {
	observation *fhirrest.Plan
	root        string
	ref         ItemRef
	connection  FHIRConnection
	policy      sendpolicy.ScopedPolicy
	policyRaw   []byte
	http        *networkaction.RuntimeHTTPPlan
	client      *smartbackend.Client
	authorities []byte
}

func httpsAddress(address string) string {
	u, _ := url.Parse(address)
	if u.Port() == "" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return u.Host
}

func (c FHIRConnection) scopedPolicy(project string, ref ItemRef, policy *sendpolicy.Policy) sendpolicy.ScopedPolicy {
	destinations := []string{}
	if policy != nil {
		destinations = policy.ApprovedDestinations
	}
	p := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: project, Environment: ref.ID, Revision: ref.Revision, Rules: []sendpolicy.ScopeRule{}}
	add := func(endpoint string, operation sendpolicy.Operation, address string) {
		_, port, _ := net.SplitHostPort(httpsAddress(address))
		n, _ := strconv.Atoi(port)
		p.Rules = append(p.Rules, sendpolicy.ScopeRule{Endpoint: endpoint, Operation: operation, Port: n, Destinations: destinations, Selection: "single-address"})
	}
	add("fhir", sendpolicy.FHIRMetadata, c.Base)
	add("fhir", sendpolicy.FHIRSearch, c.Base)
	add("fhir", sendpolicy.ObservationRead, c.Base)
	if c.Authentication == "smart" {
		add("token", sendpolicy.SMARTToken, c.TokenEndpoint)
	}
	return p
}

func bindFHIRCheck(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != EnvironmentItem || request.Destination != nil {
		return nil, refusal{Failed, "Choose one saved FHIR connection"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	members, err := loaded.environmentOf(records[0])
	if err != nil || members.fhir == nil {
		return nil, refusal{Failed, "Choose a FHIR connection"}
	}
	c := *members.fhir
	display := FHIRConnectionReview{Protocol: "FHIR R4 4.0.1", Authentication: c.Authentication, ClientID: c.ClientID, Scopes: c.Scopes, KeyReference: c.KeyReference}
	switch request.Action {
	case CheckFHIRConnectionAction:
		display.Effect = "Opens and closes one verified TLS connection; sends no HTTP or clinical message."
	case CheckFHIRCapabilitiesAction:
		display.Effect = "Reads the server's CapabilityStatement; records dated claims, not workflow compatibility."
	case CheckFHIRAuthorizationAction:
		display.Effect = "Reads the registered signing key and requests a short-lived token; changes no application state."
	default:
		return nil, refusal{Failed, "Unknown connection check"}
	}
	fixed := &fhirCheckBinding{root: loaded.root, ref: items[0].Ref, connection: c}
	fixed.policy = c.scopedPolicy(loaded.document.Project.ID, items[0].Ref, members.policy)
	fixed.policyRaw, _ = encodeMember(fixed.policy)
	raw, _ := encodeMember(c)
	identity := networkaction.Digest(append(raw, []byte(request.Action)...))
	bound := &boundAction{action: request.Action, origin: request, fhirCheck: fixed, review: ActionReview{Items: items, Ready: true, FHIRCheck: &display, Destination: ReviewDestination{Name: items[0].Name, Classification: string(c.Classification), Address: c.Base}}}
	deny := func(reason string) { bound.review.Ready = false; bound.review.Refusal = reason }
	if c.Classification != replay.Nonproduction {
		deny("Classify this environment as Nonproduction before reaching it")
	}
	if c.CAFile != "" {
		fixed.authorities, err = (artifactdir.Document{MaxBytes: 1 << 20}).Read(c.CAFile)
		if err != nil {
			deny("Choose a readable CA certificate")
		}
	}
	http := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: identity, Source: networkaction.Digest(raw), Project: fixed.policy.Project, Environment: fixed.policy.Environment, Revision: fixed.policy.Revision, Endpoint: "fhir", Classification: string(c.Classification), Operation: sendpolicy.FHIRMetadata, Method: "GET", URL: c.Base + "/metadata", ServerName: c.ServerName, Authorities: fixed.authorities, TimeoutMS: 5000, MaxBytes: 1 << 20}
	authorization := ""
	if request.Action != CheckFHIRConnectionAction && c.Authentication == "smart" {
		fixed.client, err = prepareConnectionClient(loaded.root, c, http, fixed.policyRaw)
		if err != nil {
			deny(err.Error())
		} else {
			authorization = fixed.client.Identity()
		}
	}
	if request.Action == CheckFHIRAuthorizationAction && c.Authentication != "smart" {
		deny("Choose SMART authentication and register the client before testing authorization")
	}
	transport := networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, Authorization: authorization, HTTP: http, Accept: "application/fhir+json"}
	httpRaw, _ := encodeMember(transport)
	fixed.http, err = networkaction.PrepareRuntimeHTTPV2(httpRaw, fixed.policyRaw)
	if err != nil && bound.review.Ready {
		deny("The scoped connection configuration is unavailable")
	}
	if !held && !a.admissionPreview(ctx).Admitted {
		deny("Execution admission is required to check this connection")
	}
	clientIdentity := ""
	if fixed.client != nil {
		clientIdentity = fixed.client.Identity()
	}
	bound.binding = binding(string(request.Action), loaded.root, items[0].Ref.ID, items[0].Ref.Revision, string(raw), string(fixed.policyRaw), string(fixed.authorities), clientIdentity, a.reviewer(), a.policyBinding(ctx, true, held))
	return bound, refusal{}
}

func prepareConnectionClient(root string, c FHIRConnection, http networkaction.HTTPSpec, policy []byte) (*smartbackend.Client, error) {
	keys, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(c.PublicKeysFile)
	var public smartbackend.JWKS
	if err != nil || json.Unmarshal(keys, &public, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("Choose the registered public keys")
	}
	references, err := secret.ReadStore(filepath.Join(root, ProjectSecrets))
	if err != nil {
		return nil, errors.New("Register the named signing key reference")
	}
	key, err := secret.Find(references, c.KeyReference)
	if err != nil || key.Purpose != secret.SourceEndpoint || key.Address != httpsAddress(c.TokenEndpoint) {
		return nil, errors.New("Register a signing key reference scoped to this token endpoint")
	}
	u, _ := url.Parse(c.TokenEndpoint)
	token := http
	token.Endpoint = "token"
	token.URL = c.TokenEndpoint
	token.ServerName = u.Hostname()
	token.Operation = sendpolicy.SMARTToken
	token.Method = "POST"
	token.MaxBytes = 64 << 10
	config := smartbackend.Config{Schema: smartbackend.ConfigSchema, FHIRBase: c.Base, TokenEndpoint: c.TokenEndpoint, ClientID: c.ClientID, Audience: c.TokenEndpoint, Algorithm: c.Algorithm, Role: "observer", Scopes: c.Scopes, Key: smartbackend.KeyReference{Kid: c.KeyID, Generation: "generation-" + strconv.Itoa(key.Generation), Locator: networkaction.Provider{Command: key.Command, Arguments: key.Arguments}, JWKS: public}, Token: token}
	raw, _ := encodeMember(config)
	client, err := smartbackend.Prepare(raw, policy, nil)
	if err != nil {
		return nil, errors.New("The client registration, scopes or public key are unsupported; check their registered values")
	}
	return client, nil
}

type fhirReviewAuthority struct {
	app   *App
	bound *boundAction
	actor networkaction.Actor
}

func (r fhirReviewAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	deny := errors.New("the connection review is no longer authorized")
	if ctx.Err() != nil || r.bound.executionReview == nil || r.bound.fhirCheck == nil {
		return networkaction.Actor{}, deny
	}
	fixed := r.bound.fhirCheck
	if fixed.http == nil || b != fixed.http.Binding() && (fixed.client == nil || b != fixed.client.TokenBinding()) {
		if fixed.observation == nil || b != fixed.observation.Binding() {
			return networkaction.Actor{}, deny
		}
	}
	lease := r.bound.executionReview
	store := &r.app.reviews
	store.mu.Lock()
	review := store.reviews[lease.token]
	valid := review != nil && !review.withdrawn && review.consumed == lease.intent && store.running == lease.intent && r.app.now().Before(review.expires)
	store.mu.Unlock()
	if !valid {
		return networkaction.Actor{}, deny
	}
	guard, _ := r.app.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return networkaction.Actor{}, deny
	}
	var fresh *boundAction
	var declined refusal
	if r.bound.action == CollectObservationAction {
		fresh, declined = bindCollect(r.app, ctx, r.bound.origin, true)
	} else {
		fresh, declined = bindFHIRCheck(r.app, ctx, r.bound.origin, true)
	}
	if fresh == nil || declined.reason != "" || fresh.binding != r.bound.binding {
		return networkaction.Actor{}, deny
	}
	return r.actor, nil
}

func executeFHIRCheck(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{State: Failed, Outcome: ActionRefused}
	fixed := bound.fhirCheck
	lease := bound.executionReview
	if fixed == nil || lease == nil || fixed.http == nil {
		result.Reason = "A fresh connection review is required"
		return result
	}
	actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "review-" + binding(lease.token)[:16], EvidenceIdentity: bound.binding, Expires: lease.expires}
	authority := fhirReviewAuthority{app: a, bound: bound, actor: actor}
	a.reach(reachingTarget{ref: "environment:" + fixed.ref.ID, name: bound.review.Items[0].Name, destination: fixed.connection.Base, kind: ConnectionEnvironment})
	check := FHIRCapabilityCheck{Revision: fixed.ref.Revision, CheckedAt: catalog.Stamp(a.now()), Outcome: "unavailable"}
	record := fhirCheckRecord{Schema: fhirCheckSchema, Item: fixed.ref.ID, Action: bound.action, Check: check}
	var err error
	var session *smartbackend.Session
	if fixed.client != nil {
		session = fixed.client.Session(authority, nil)
		defer session.Disconnect()
	}
	switch bound.action {
	case CheckFHIRConnectionAction:
		route, e := destination.AdmitScoped(ctx, destination.ScopedRequest{Policy: fixed.policy, Request: sendpolicy.ScopedRequest{Project: fixed.policy.Project, Environment: fixed.ref.ID, Endpoint: "fhir", Classification: string(fixed.connection.Classification), Address: httpsAddress(fixed.connection.Base), Operation: sendpolicy.FHIRMetadata}, Budget: 5 * time.Second, Record: func(sendpolicy.ScopedDecision) error { return nil }, Authorize: func(ctx context.Context) error { _, err := authority.Check(ctx, fixed.http.Binding()); return err }})
		err = e
		if err == nil {
			connection, e := route.Open(ctx, &destination.Security{ServerName: fixed.connection.ServerName, Authorities: fixed.authorities})
			err = e
			if connection != nil {
				_ = connection.Close()
			}
		}
		if err == nil {
			check.Outcome = "reachable"
		}
	case CheckFHIRAuthorizationAction:
		if session == nil {
			err = errors.New("authentication is not configured")
		} else {
			_, err = session.Material(ctx, networkaction.RuntimeTarget{URL: fixed.connection.Base + "/metadata", Method: "GET", Operation: sendpolicy.FHIRMetadata, Binding: fixed.http.Binding(), Actor: actor, Contract: networkaction.RuntimeHTTPSchemaV2, Check: func(ctx context.Context) error { _, err := authority.Check(ctx, fixed.http.Binding()); return err }})
		}
		if err == nil {
			check.Outcome = "authorized"
		}
	case CheckFHIRCapabilitiesAction:
		var provider networkaction.RuntimeProvider
		if session != nil {
			provider = session
		}
		response, _, e := fixed.http.Execute(ctx, authority, nil, provider)
		err = e
		if err == nil && response.Status != 200 {
			err = errors.New("the server did not return capabilities")
		}
		if err == nil {
			raw := response.Body.Expose()
			document, e := fhirr4.Decode(ctx, raw, fhirr4.Context{Version: fhirr4.Version, Base: fixed.connection.Base, MediaType: "application/fhir+json"})
			err = e
			if err == nil && len(document.Resources()) == 1 {
				claims, e := document.Capabilities(document.Resources()[0].Occurrence)
				err = e
				if err == nil {
					check.Outcome = "recorded"
					check.Claims = &claims
					record.Capability = raw
				}
			} else {
				err = errors.New("the server did not return one supported R4 CapabilityStatement")
			}
		}
	}
	if err == nil {
		result.State = Completed
		result.Outcome = ActionCompleted
	} else {
		result.Reason = "The explicit check could not complete. Verify TLS, allowed destinations, registered authentication and server support."
	}
	if ctx.Err() != nil {
		result.State = Cancelled
		result.Outcome = ActionCancelled
		check.Outcome = "cancelled"
	}
	record.Check = check
	result.FHIRCheck = &check
	if err := retainFHIRCheck(fixed.root, record); err != nil {
		result.State = Failed
		result.Reason = "The check could not be retained; check local storage"
	}
	return result
}
