package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
)

const FHIRFlowConfigSchema = "readmit-connected-run-config/v4"
const FlowSchemaV4 = "readmit-connected-run/v4"
const PhaseSchemaV2 = "readmit-connected-phase/v2"

// FHIRAuthorization selects how one role reaches a FHIR server. "none" is the
// explicit laboratory mode; "smart" names a SMART Backend Services client and
// the separately provisioned grant for its token request. Neither is a fallback.
type FHIRAuthorization struct {
	Mode     string `json:"mode"`
	Client   string `json:"client,omitzero"`
	Metadata string `json:"metadata,omitzero"`
	Token    *Grant `json:"token,omitzero"`
}
type FHIRServerSelection struct {
	Authorities string            `json:"authorities"`
	ServerName  string            `json:"server_name"`
	Observation FHIRAuthorization `json:"observation"`
	Action      FHIRAuthorization `json:"action"`
}

// FHIRPhaseSelection holds a phase's runtime choices. A v2 phase names its
// case, target and send grant; FHIR steps, FHIR datasets and each server's
// capability preflight name separately provisioned grants in Grants.
type FHIRPhaseSelection struct {
	Case       string                     `json:"case,omitzero"`
	Target     string                     `json:"target,omitzero"`
	Credential string                     `json:"credential,omitzero"`
	Send       *Grant                     `json:"send,omitzero"`
	Sources    map[string]SourceSelection `json:"sources"`
	Barriers   map[string]SourceSelection `json:"barriers"`
	Grants     map[string]Grant           `json:"grants"`
}

// ValidationSelection names the optional installed validator capability.
// Engine "local" runs its worker; "none" retains validation as undecided.
type ValidationSelection struct {
	Capability string `json:"capability"`
	Engine     string `json:"engine"`
	Socket     string `json:"socket,omitzero"`
}
type FHIRFlowConfig struct {
	Schema     string                         `json:"schema"`
	Policy     string                         `json:"policy"`
	Servers    map[string]FHIRServerSelection `json:"servers"`
	Phases     map[string]FHIRPhaseSelection  `json:"phases"`
	Validation *ValidationSelection           `json:"validation,omitzero"`
	Isolation  IsolationSelection             `json:"isolation"`
	Seed       uint64                         `json:"seed"`
}

type fhirAuth struct {
	identity string
	client   *smartbackend.Client
	token    Grant
}
type fhirServer struct {
	id, base, serverName string
	authorities          []byte
	capability           []byte
	observation, action  fhirAuth
}
type fhirSource struct {
	definition  connectedtest.Dataset
	interval    observeinterval.Definition
	observation fhirobserve.Observation
	base, url   string
	plan        *fhirrest.Plan
	server      *fhirServer
	grant       Grant
	barrier     *sourcePlan
}
type fhirStep struct {
	id       string
	method   string
	server   *fhirServer
	template networkaction.Binding
	grant    Grant
}
type fhirPhase struct {
	plan      *connectedtest.Plan
	transport *connectedtransport.Prepared
	send      Grant
	sources   []sourcePlan
	fhir      []fhirSource
	steps     []fhirStep
	preflight map[string]*fhirrest.Plan
	grants    map[string]Grant
}
type fhirFlow struct {
	// lifecycle scopes every FHIR request to the v5 plan, so one reviewed
	// SMART client per role serves each of its phases.
	lifecycle  string
	policy     []byte
	servers    map[string]*fhirServer
	phases     map[string]*fhirPhase
	validation *fhirvalidator.Capability
	engine     string
	socket     string
}

func prepareFHIRFlow(plan *connectedtest.FlowPlan, planPath, configPath, instance string, raw []byte) (*PreparedFlow, error) {
	var c FHIRFlowConfig
	d := plan.Document().Test
	if json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != FHIRFlowConfigSchema || len(c.Phases) != len(d.Phases) || len(c.Servers) != len(d.Servers) {
		return nil, invalid
	}
	root, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	anchor := func(s string) string {
		if s == "" {
			return ""
		}
		return artifactpath.JoinReference(root, s)
	}
	anchorGrant := func(g Grant) Grant { g.Path = anchor(g.Path); return g }
	selection := anchorIsolation(c.Isolation, anchor)
	policy, err := (artifactdir.Document{MaxBytes: sendpolicy.MaxPolicyBytes}).Read(anchor(c.Policy))
	if err != nil || dataset.Digest(policy) != d.Environment.AddressPolicyIdentity {
		return nil, invalid
	}
	isolation, err := prepareFlowIsolation(plan, selection, instance, c.Seed)
	if err != nil {
		return nil, err
	}
	f := &fhirFlow{lifecycle: plan.Identity(), policy: policy, servers: map[string]*fhirServer{}, phases: map[string]*fhirPhase{}}
	for _, server := range d.Servers {
		chosen, ok := c.Servers[server.ID]
		if !ok || chosen.ServerName == "" {
			return nil, invalid
		}
		authorities, err := destination.ReadAuthorities(anchor(chosen.Authorities))
		if err != nil || len(authorities) == 0 {
			return nil, invalid
		}
		s := &fhirServer{id: server.ID, base: server.Base, serverName: chosen.ServerName, authorities: authorities}
		s.capability, _, _ = plan.Phase(d.Phases[0].ID).ServerCapability(server.ID)
		if s.observation, err = prepareFHIRAuth(chosen.Observation, server.Base, "observer", policy, anchor); err != nil {
			return nil, err
		}
		if s.action, err = prepareFHIRAuth(chosen.Action, server.Base, "setup", policy, anchor); err != nil {
			return nil, err
		}
		f.servers[server.ID] = s
	}
	if v := c.Validation; v != nil {
		if v.Engine != "local" && v.Engine != "none" || v.Engine == "none" && v.Socket != "" {
			return nil, invalid
		}
		f.validation, err = fhirvalidator.OpenCapability(anchor(v.Capability))
		if err != nil {
			return nil, err
		}
		f.engine, f.socket = v.Engine, anchor(v.Socket)
	}
	p := &PreparedFlow{plan: plan, phases: map[string]*Prepared{}, isolation: isolation, selection: selection, instance: instance, planPath: planPath, configPath: configPath, raw: bytes.Clone(raw), fhir: f}
	if err = p.prepareTransitions(); err != nil {
		return nil, err
	}
	for _, phase := range d.Phases {
		chosen, ok := c.Phases[phase.ID]
		if !ok {
			return nil, invalid
		}
		child := plan.Phase(phase.ID)
		fp := &fhirPhase{plan: child, preflight: map[string]*fhirrest.Plan{}, grants: map[string]Grant{}}
		for name, g := range chosen.Grants {
			fp.grants[name] = anchorGrant(g)
		}
		used := map[string]bool{}
		grant := func(name string) (Grant, error) {
			g, ok := fp.grants[name]
			if !ok {
				return Grant{}, invalid
			}
			used[name] = true
			return g, nil
		}
		v2 := child.Document().Test.Steps[0].V2 != nil
		if v2 {
			if chosen.Send == nil {
				return nil, invalid
			}
			fp.transport, err = connectedtransport.PrepareSequence(child, connectedtransport.Selection{Case: anchor(chosen.Case), Target: anchor(chosen.Target), Policy: anchor(c.Policy), Credential: anchor(chosen.Credential)})
			if err != nil {
				return nil, err
			}
			fp.send = anchorGrant(*chosen.Send)
		} else if chosen.Send != nil || chosen.Case != "" || chosen.Target != "" || chosen.Credential != "" {
			return nil, invalid
		}
		servers := map[string]bool{}
		for _, s := range child.Document().Test.Steps {
			if s.Interaction == nil {
				continue
			}
			server := f.servers[s.Interaction.Server]
			servers[server.id] = true
			g, err := grant("step:" + s.ID)
			if err != nil {
				return nil, err
			}
			fp.steps = append(fp.steps, fhirStep{id: s.ID, method: s.Interaction.Method, server: server, template: stepTemplate(f.lifecycle, child, s, server, policy), grant: g})
		}
		for _, id := range slices.Sorted(func(yield func(string) bool) {
			for id := range servers {
				if !yield(id) {
					return
				}
			}
		}) {
			server := f.servers[id]
			if _, err := grant("preflight:" + id); err != nil {
				return nil, err
			}
			spec := fhirSpec(f.lifecycle, child.Document().Environment, server, server.observation, "preflight-"+id, id, sendpolicy.FHIRMetadata, fhirrest.Budget{Pages: 1, Rows: 1, Bytes: 4 << 20, TimeoutMS: 30000}, fhirrest.Retry{MaxAttempts: 1})
			spec.HTTP.HTTP.Method, spec.HTTP.HTTP.URL = "GET", server.base+"/metadata"
			if fp.preflight[id], err = fhirrest.Prepare(encodeSpec(spec), policy); err != nil {
				return nil, err
			}
		}
		typedRows := 0
		for _, ds := range child.Document().Test.Datasets {
			if ds.Kind == "fhir-resources" {
				source, err := prepareFHIRSource(child, ds, f, policy)
				if err != nil {
					return nil, err
				}
				if source.grant, err = grant("dataset:" + ds.ID); err != nil {
					return nil, err
				}
				if source.interval.Barrier != nil {
					barrier, err := prepareBarrier(child, ds, source.interval, chosen.Barriers, anchor, policy)
					if err != nil {
						return nil, err
					}
					source.barrier = barrier
				}
				fp.fhir = append(fp.fhir, source)
				continue
			}
			typedRows++
			selected, ok := chosen.Sources[ds.ID]
			if !ok {
				return nil, invalid
			}
			item, err := prepareTypedSource(child, ds, selected, chosen.Barriers, anchor, policy)
			if err != nil {
				return nil, err
			}
			fp.sources = append(fp.sources, item)
		}
		barriers := 0
		for _, s := range fp.sources {
			if s.barrier != nil {
				barriers++
			}
		}
		for _, s := range fp.fhir {
			if s.barrier != nil {
				barriers++
			}
		}
		if typedRows != len(chosen.Sources) || barriers != len(chosen.Barriers) || len(used) != len(fp.grants) {
			return nil, invalid
		}
		p.fhir.phases[phase.ID] = fp
	}
	if d.Boundary == "application-state" {
		business := false
		for _, fp := range f.phases {
			for _, s := range fp.fhir {
				business = business || s.definition.Phase == "after"
			}
			for _, s := range fp.sources {
				business = business || s.definition.Phase == "after"
			}
		}
		if !business {
			return nil, invalid
		}
	}
	return p, nil
}

func prepareFHIRAuth(a FHIRAuthorization, base, role string, policy []byte, anchor func(string) string) (fhirAuth, error) {
	switch a.Mode {
	case "none":
		if a.Client != "" || a.Metadata != "" || a.Token != nil {
			return fhirAuth{}, invalid
		}
		return fhirAuth{}, nil
	case "smart":
		if a.Client == "" || a.Token == nil {
			return fhirAuth{}, invalid
		}
		raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(anchor(a.Client))
		if err != nil {
			return fhirAuth{}, invalid
		}
		var declared smartbackend.Config
		if json.Unmarshal(raw, &declared) != nil || declared.FHIRBase != base || declared.Role != role {
			return fhirAuth{}, invalid
		}
		var metadata []byte
		if a.Metadata != "" {
			if metadata, err = (artifactdir.Document{MaxBytes: 2 << 20}).Read(anchor(a.Metadata)); err != nil {
				return fhirAuth{}, invalid
			}
		}
		client, err := smartbackend.Prepare(raw, policy, metadata)
		if err != nil {
			return fhirAuth{}, err
		}
		token := *a.Token
		token.Path = anchor(token.Path)
		return fhirAuth{identity: client.Identity(), client: client, token: token}, nil
	}
	return fhirAuth{}, invalid
}

// fhirSpec is the one derivation of a FHIR request's transport declaration;
// the reader derives it again from the plan and retained runtime evidence.
func fhirSpec(lifecycle string, env connectedtest.Environment, server *fhirServer, auth fhirAuth, source, endpoint string, operation sendpolicy.Operation, budget fhirrest.Budget, retry fhirrest.Retry) fhirrest.Spec {
	maxBytes := min(budget.Bytes, 16<<20)
	return fhirrest.Spec{Schema: fhirrest.PlanSchema, Base: server.base, Capability: server.capability, Budget: budget, Retry: retry, HTTP: networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, Authorization: auth.identity, Accept: "application/fhir+json", HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: lifecycle, Source: networkaction.Digest([]byte(source)), Project: env.Project, Environment: env.ID, Revision: env.Revision, Endpoint: endpoint, Classification: "nonproduction", Operation: operation, ServerName: server.serverName, Authorities: server.authorities, TimeoutMS: min(budget.TimeoutMS, 300000), MaxBytes: maxBytes}}}
}
func encodeSpec(s fhirrest.Spec) []byte { b, _ := json.Marshal(s, json.Deterministic(true)); return b }

// role is the authorization a reviewed request runs under: reads and searches
// use the read-only observation client; only writes use the action client.
func (s *fhirServer) role(method string) fhirAuth {
	if method == "GET" {
		return s.observation
	}
	return s.action
}
func roleName(method string) string {
	if method == "GET" {
		return "observation"
	}
	return "action"
}

// stepOperation is the scoped purpose a reviewed request needs. A delete is
// setup authority; reads and searches never borrow write authority.
func stepOperation(method string) sendpolicy.Operation {
	switch method {
	case "GET":
		return sendpolicy.FHIRSearch
	case "DELETE":
		return sendpolicy.SetupAction
	}
	return sendpolicy.FHIRAction
}

// stepTemplate is the pre-provisionable binding of a reviewed request whose
// exact address may depend on an earlier response. The derived request is
// admitted only under this binding and only as the offline derivation states.
func stepTemplate(lifecycle string, plan *connectedtest.Plan, s connectedtest.Step, server *fhirServer, policy []byte) networkaction.Binding {
	env := plan.Document().Environment
	body := plan.Files()["inputs/"+s.ID+".fhir"]
	configuration := dataset.Digest(canonicalFlow(struct {
		Step        connectedtest.Step `json:"step"`
		Body        []byte             `json:"body"`
		Base        string             `json:"base"`
		ServerName  string             `json:"server_name"`
		Authorities []byte             `json:"authorities"`
		Auth        string             `json:"authorization"`
	}{s, body, server.base, server.serverName, server.authorities, server.role(s.Interaction.Method).identity}))
	return networkaction.Binding{Plan: lifecycle, Configuration: configuration, Policy: dataset.Digest(policy), Credentials: server.role(s.Interaction.Method).identity, Source: networkaction.Digest([]byte("step-" + s.ID)), Project: env.Project, Environment: env.ID, Revision: env.Revision, Endpoint: s.Interaction.Server, Operation: stepOperation(s.Interaction.Method)}
}

func prepareFHIRSource(plan *connectedtest.Plan, ds connectedtest.Dataset, f *fhirFlow, policy []byte) (fhirSource, error) {
	o, base, address, err := plan.ObservationURL(ds.ID)
	if err != nil || ds.Completion.Policy == nil {
		return fhirSource{}, invalid
	}
	definition, err := observeinterval.Decode(plan.Files()["dependencies/"+ds.Completion.Policy.SHA256])
	if err != nil || !definition.Enabled {
		return fhirSource{}, invalid
	}
	server := f.servers[o.Server]
	spec := fhirSpec(f.lifecycle, plan.Document().Environment, server, server.observation, o.Identity(), ds.ID, sendpolicy.FHIRSearch, o.Budget, o.Retry)
	spec.HTTP.HTTP.Source = o.Identity()
	spec.HTTP.HTTP.Method, spec.HTTP.HTTP.URL = "GET", address
	prepared, err := fhirrest.Prepare(encodeSpec(spec), policy)
	if err != nil {
		return fhirSource{}, err
	}
	return fhirSource{definition: ds, interval: definition, observation: o, base: base, url: address, plan: prepared, server: server}, nil
}
func prepareTypedSource(plan *connectedtest.Plan, ds connectedtest.Dataset, selected SourceSelection, barriers map[string]SourceSelection, anchor func(string) string, policy []byte) (sourcePlan, error) {
	if ds.Projection == nil || ds.Completion.Policy == nil {
		return sourcePlan{}, invalid
	}
	projection, err := dataset.DecodeProjection(plan.Files()["dependencies/"+ds.Projection.SHA256])
	if err != nil {
		return sourcePlan{}, err
	}
	definition, err := observeinterval.Decode(plan.Files()["dependencies/"+ds.Completion.Policy.SHA256])
	if err != nil || !definition.Enabled || definition.Mode != "snapshots" {
		return sourcePlan{}, invalid
	}
	item, err := prepareSource(plan, ds, projection, selected, anchor, policy)
	if err != nil || item.source.Capture != nil {
		return sourcePlan{}, invalid
	}
	item.interval = &definition
	if definition.Barrier != nil {
		if item.barrier, err = prepareBarrier(plan, ds, definition, barriers, anchor, policy); err != nil {
			return sourcePlan{}, err
		}
	}
	return item, nil
}
func prepareBarrier(plan *connectedtest.Plan, ds connectedtest.Dataset, definition observeinterval.Definition, barriers map[string]SourceSelection, anchor func(string) string, policy []byte) (*sourcePlan, error) {
	chosen, ok := barriers[ds.ID]
	if ds.Phase != "after" || !ok {
		return nil, invalid
	}
	bd := connectedtest.Dataset{ID: ds.ID + "-barrier", Source: definition.Barrier.Source, Namespace: "processing-barrier", Phase: "after", Kind: "typed-rows"}
	prepared, err := prepareSource(plan, bd, definition.Barrier.Projection, chosen, anchor, policy)
	if err != nil || prepared.source.Capture != nil {
		return nil, invalid
	}
	return &prepared, nil
}

// fhirBindings are the exact grant scopes a runner must provision for v5.
func (f *fhirFlow) bindings(store *RecoveryStore) map[string]networkaction.Binding {
	out := map[string]networkaction.Binding{}
	for id, phase := range f.phases {
		if phase.transport != nil {
			out[id+":stimulus"] = scopeStore(phase.transport.Binding(), store)
		}
		for _, s := range phase.sources {
			for role, b := range map[string]networkaction.Binding{"dataset:": sourceBinding(s), "barrier:": barrierBinding(s.barrier)} {
				if b != (networkaction.Binding{}) {
					out[id+":"+role+s.definition.ID] = scopeStore(b, store)
				}
			}
		}
		for _, s := range phase.fhir {
			out[id+":dataset:"+s.definition.ID] = scopeStore(s.plan.Binding(), store)
			if b := barrierBinding(s.barrier); b != (networkaction.Binding{}) {
				out[id+":barrier:"+s.definition.ID] = scopeStore(b, store)
			}
		}
		for _, s := range phase.steps {
			out[id+":step:"+s.id] = scopeStore(s.template, store)
		}
		for server, plan := range phase.preflight {
			out[id+":preflight:"+server] = scopeStore(plan.Binding(), store)
		}
	}
	for id, server := range f.servers {
		if server.observation.client != nil {
			out["token:"+id+":observation"] = scopeStore(server.observation.client.TokenBinding(), store)
		}
		if server.action.client != nil {
			out["token:"+id+":action"] = scopeStore(server.action.client.TokenBinding(), store)
		}
	}
	return out
}
func sourceBinding(s sourcePlan) networkaction.Binding {
	if s.http != nil {
		return s.http.Binding()
	}
	if s.database != nil {
		return s.database.Binding()
	}
	return networkaction.Binding{}
}
func barrierBinding(s *sourcePlan) networkaction.Binding {
	if s == nil {
		return networkaction.Binding{}
	}
	return sourceBinding(*s)
}

// provider returns the runtime credential adapter for one role. The SMART
// session lives only for one phase and is disconnected when it ends.
func (a fhirAuth) provider(p *PreparedFlow) (networkaction.RuntimeProvider, func()) {
	if a.client == nil {
		return nil, func() {}
	}
	session := a.client.Session(flowAuthority{flow: p, authority: a.token.authority()}, nil)
	return session, session.Disconnect
}

// flowAuthority rechecks the complete selected configuration before any
// grant is consulted, as liveAuthority does for a v2 phase.
type flowAuthority struct {
	flow      *PreparedFlow
	authority networkaction.Authority
}

func (a flowAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if ctx.Err() != nil || a.flow.unchanged() != nil {
		return networkaction.Actor{}, invalid
	}
	return a.authority.Check(ctx, scopeStore(b, a.flow.store))
}

// templateAuthority admits exactly one derived request under a step's
// provisioned template grant; any other binding is refused before effects.
type templateAuthority struct {
	root              networkaction.Authority
	template, derived networkaction.Binding
}

func (a templateAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if b != a.derived {
		return networkaction.Actor{}, invalid
	}
	return a.root.Check(ctx, a.template)
}
