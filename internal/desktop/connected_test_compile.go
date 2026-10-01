package desktop

import (
	"bytes"
	stdcmp "cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
	"github.com/bharm16/readmit/internal/testisolation"
)

// Compiling a connected test turns its authoring document and the named
// objects it names into the readmit-connected-test/v5 lifecycle the connected
// runner executes, and the readmit-connected-run-config/v4 selection its
// preparation reads. Every value comes from a saved object: the environment's
// target or FHIR connection, approved addresses and typed isolation, the
// observations' saved sources, projections and completion, and the cases'
// verified bytes. The only values filled here are mechanical — the lifecycle's
// identifiers, the runtime grant locations and its execution bounds — never an
// expected value, a permission or an address. Compiling reads files and
// writes nothing; it contacts nothing and resolves no credential.

// The FHIR server and receiver endpoints a compiled lifecycle names.
const (
	connectedServer   = "fhir"
	connectedReceiver = "receiver"
)

// connectedLimits are the compiled lifecycle's execution bounds.
var connectedLimits = connectedtest.Limits{MaxSteps: 256, MaxBytes: 16 << 20, DeadlineMS: 600000}

// connectedRequestBudget bounds one reviewed FHIR request.
var connectedRequestBudget = fhirrest.Budget{Pages: 1, Rows: 1, Bytes: 1 << 20, TimeoutMS: 60000}

// connectedEnvironments names the environments a connected test is compiled
// against, by their identities in the project: the environment it links, and
// the FHIR environment its requests and FHIR observations reach when that is
// another one.
type connectedEnvironments struct {
	environment string
	server      string
	// promoted says a suite binds the test to environments in place of its
	// own: its FHIR observations then reach the bound FHIR server.
	promoted bool
}

// connectedCompiled is one connected test compiled against its environments:
// the sealed lifecycle and the selection preparing it reads. config builds
// the selection for a folder, given how a file of the project is named from
// that folder; files are the selection's own documents, by their names in it.
type connectedCompiled struct {
	plan   *connectedtest.FlowPlan
	flow   connectedtest.FlowTest
	files  map[string][]byte
	config func(place func(string) string) connectedrun.FHIRFlowConfig
}

// connectedObservationRead is one saved version of a connected observation as
// a connected test reads it.
type connectedObservationRead struct {
	ref        ItemRef
	name       string
	setup      ConnectedObservation
	paths      map[string]string
	kind       string
	source     string
	projection string
	columns    []ConnectedColumn
	member     []byte
	interval   []byte
	definition observeinterval.Definition
	fhir       *fhirobserve.Observation
	typed      *observesource.Source
}

// connectedObservation reads one saved version of a named observation, or
// the current one when revision is empty.
func (c *loadedCatalog) connectedObservation(id, revision string) (*connectedObservationRead, error) {
	index := c.document.Find(id)
	if index < 0 || c.document.Items[index].Kind != string(ObservationItem) || c.removed(c.document.Items[index]) {
		return nil, errors.New("the project holds no such observation")
	}
	item := c.document.Items[index]
	if revision == "" {
		revision = item.RevisionLabel()
	}
	paths, availability, reason := c.revisionBacking(item, revision)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	read := &connectedObservationRead{ref: ItemRef{Kind: ObservationItem, ID: id, Revision: revision}, name: c.read(item).Name, paths: paths}
	if _, held := paths["setup"]; !held {
		return read, errors.New("only an observation with typed fields and a completion rule can be read by a connected test")
	}
	if err := verifyConnectedObservation(paths); err != nil {
		return read, err
	}
	raw, err := savedFile.Read(paths["setup"])
	if err == nil && json.Unmarshal(raw, &read.setup, json.RejectUnknownMembers(true)) != nil {
		err = errors.New("the observation's setup cannot be read")
	}
	if err != nil {
		return read, err
	}
	if read.interval, err = savedFile.Read(paths["interval"]); err != nil {
		return read, err
	}
	if read.definition, err = observeinterval.Decode(read.interval); err != nil {
		return read, err
	}
	if read.setup.FHIR != nil {
		raw, err := savedFile.Read(paths["source"])
		if err != nil {
			return read, err
		}
		o, err := fhirobserve.Decode(raw)
		if err != nil {
			return read, err
		}
		read.kind, read.fhir, read.member, read.source, read.projection = "fhir", &o, raw, o.Identity(), o.ProjectionIdentity()
		for _, column := range o.TypedColumns() {
			read.columns = append(read.columns, ConnectedColumn{Name: column.Name, Type: column.Type, CodeSystem: column.CodeSystem, Key: column.Key, Required: column.Required, Repeated: column.Repeated, States: fhirStates})
		}
		return read, nil
	}
	raw, err = savedFile.Read(paths["projection"])
	if err != nil {
		return read, err
	}
	projection, err := dataset.DecodeProjection(raw)
	if err != nil {
		return read, err
	}
	sourceRaw, err := savedFile.Read(paths["source"])
	if err != nil {
		return read, err
	}
	source, err := observesource.DecodeSource(sourceRaw)
	if err != nil {
		return read, err
	}
	read.kind, read.typed, read.member, read.source, read.projection = "typed", &source, raw, source.Identity(), projection.Identity()
	for _, column := range projection.Columns {
		read.columns = append(read.columns, ConnectedColumn{Name: column.Name, Type: column.Type, CodeSystem: column.CodeSystem, Key: column.Key, Required: column.Required, Repeated: column.Repeated, States: hl7States})
	}
	return read, nil
}

// offer is how the editor shows one observation it can read.
func (r *connectedObservationRead) offer(err error) ConnectedObservationOffer {
	offer := ConnectedObservationOffer{Ref: r.ref, Name: r.name, Protocol: r.kind, Phases: []string{}, Environment: r.setup.Environment, Columns: []ConnectedColumn{}, Projection: r.projection, Readable: err == nil,
		HorizonMS: r.definition.HorizonMS, Barrier: r.definition.Barrier != nil || r.setup.BarrierObservation != ""}
	if r.setup.Phase != "" {
		offer.Phases = []string{r.setup.Phase}
	}
	if r.columns != nil {
		offer.Columns = r.columns
	}
	if err != nil {
		offer.Reason = err.Error()
	}
	return offer
}

// connectedEnvironmentRead is one named environment as a connected test uses
// it: its current revision, name and members.
type connectedEnvironmentRead struct {
	ref     ItemRef
	name    string
	members *environmentMembers
}

func (c *loadedCatalog) connectedEnvironment(id string) (*connectedEnvironmentRead, error) {
	index := c.document.Find(id)
	if id == "" || index < 0 || c.document.Items[index].Kind != string(EnvironmentItem) || c.removed(c.document.Items[index]) {
		return nil, errors.New("the project holds no such environment")
	}
	item := c.document.Items[index]
	members, err := c.environmentOf(item)
	if err != nil {
		return nil, errors.New("that environment cannot be read: " + err.Error())
	}
	return &connectedEnvironmentRead{ref: ItemRef{Kind: EnvironmentItem, ID: id, Revision: item.RevisionLabel()}, name: c.read(item).Name, members: members}, nil
}

// connectedSourceRead is the bytes one step sends, read from its case.
type connectedSourceRead struct {
	path     string
	identity string
	protocol string
	raw      []byte
	request  *fhirevidence.RequestDeclaration
	resource string
}

// connectedSource reads one step's evidence: the case it names must still be
// the case of the project with the evidence it was chosen from.
func (c *loadedCatalog) connectedSource(source ConnectedSource) (*connectedSourceRead, error) {
	index := c.document.Find(source.Case.ID)
	if index < 0 || (c.document.Items[index].Kind != string(CaseItem) && c.document.Items[index].Kind != string(VariantItem)) || c.removed(c.document.Items[index]) || c.document.Items[index].Entry == "" {
		return nil, errors.New("the case this input sends is no longer in the project")
	}
	entry := c.document.Items[index].Entry
	path, err := artifactpath.Child(c.root, entry)
	if err != nil {
		return nil, errors.New("the case this input sends cannot be read")
	}
	read := &connectedSourceRead{path: path}
	if regular(filepath.Join(path, fhirevidence.ManifestName)) {
		opened, err := fhirevidence.Open(context.Background(), path)
		if err != nil {
			return nil, errors.New("the FHIR evidence this input sends cannot be verified")
		}
		read.identity, read.protocol, read.request = opened.Identity, "fhir", opened.Manifest.Declaration.Request
		if opened.Request != nil {
			read.resource = opened.Request.Resource
		}
		if opened.Document != nil {
			resources := opened.Document.Resources()
			at := slices.IndexFunc(resources, func(r fhirr4.Resource) bool { return r.Occurrence == source.Occurrence })
			if at < 0 {
				return nil, errors.New("the FHIR evidence holds no such resource")
			}
			if resources[at].Pointer == "" {
				read.raw = opened.Raw()
			} else if read.raw, err = opened.Document.ResourceBytes(source.Occurrence); err != nil {
				return nil, errors.New("the FHIR evidence holds no such resource")
			}
			read.resource = resources[at].Type
		} else if source.Occurrence != "request" {
			return nil, errors.New("the FHIR evidence holds no such resource")
		}
	} else {
		opened, err := bundle.Open(path)
		if err != nil {
			return nil, errors.New("the case this input sends cannot be verified")
		}
		read.identity, read.protocol = opened.Identity, "v2"
		if read.raw, err = opened.Raw(source.Occurrence); err != nil {
			return nil, errors.New("the case holds no such message")
		}
	}
	if read.identity != source.Identity {
		return read, errors.New("the case this input sends changed since it was chosen; choose its evidence again")
	}
	return read, nil
}

// compileConnected compiles one connected test, identified in its lifecycle by
// id and revision, against its environments. Every problem is reported at
// the field it is about.
func (c *loadedCatalog) compileConnected(d ConnectedTestDraft, id, revision string, bind connectedEnvironments) (*connectedCompiled, []FieldProblem) {
	problems := []FieldProblem{}
	add := func(field, problem string) {
		if !slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Field == field && p.Problem == problem }) {
			problems = append(problems, FieldProblem{Field: field, Problem: problem})
		}
	}
	project := c.document.Project.ID
	files := map[string][]byte{}
	ref := func(name, schema, file string, raw []byte) connectedtest.Reference {
		files[file] = raw
		return connectedtest.Reference{Project: project, ID: name, Schema: schema, File: file, SHA256: dataset.Digest(raw)}
	}
	usesV2, usesFHIR := false, false
	for _, s := range d.Steps {
		usesV2, usesFHIR = usesV2 || s.V2 != nil, usesFHIR || s.FHIR != nil
	}
	for _, phase := range d.Phases {
		for _, observed := range phase.Observations {
			if read, err := c.connectedObservation(observed.Observation.ID, observed.Observation.Revision); err == nil && read.kind == "fhir" {
				usesFHIR = true
			}
		}
	}

	primary, err := c.connectedEnvironment(bind.environment)
	if err != nil {
		add("test.environment", "choose the environment this test runs against")
		return nil, problems
	}
	serverID := stdcmp.Or(bind.server, primary.ref.ID)
	var server *connectedEnvironmentRead
	if usesFHIR {
		if server, err = c.connectedEnvironment(serverID); err != nil || server.members.fhir == nil {
			add("connected.server", "choose the FHIR environment this test's requests and FHIR observations reach")
			server = nil
		}
	}
	if usesV2 && primary.members.fhir != nil {
		add("test.environment", "this test sends v2 messages; choose a v2 environment and reach FHIR through its FHIR server")
	}
	if primary.members.isolation == nil {
		add("test.environment", "set up typed isolation in this environment's Reset before a connected test runs against it")
		return nil, problems
	}
	classification := primary.members.target.Classification
	if primary.members.fhir != nil {
		classification = primary.members.fhir.Classification
	}
	contract, registry, isolationPolicy, err := isolationConfiguration(project, *primary.members.isolation, classification, primary.members.policy)
	if err != nil {
		add("test.environment", "this environment's isolation is not authorized for this project: "+err.Error())
		return nil, problems
	}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: project, Environment: contract.Environment, Revision: contract.Revision, Rules: []sendpolicy.ScopeRule{}}
	allow := func(field, endpoint, address string, approved *sendpolicy.Policy, operations ...sendpolicy.Operation) {
		_, port, err := net.SplitHostPort(address)
		number, _ := strconv.Atoi(port)
		if err != nil || number < 1 {
			add(field, "this environment's address has no port a connected test can reach")
			return
		}
		if approved == nil || len(approved.ApprovedDestinations) == 0 {
			add(field, "approve the addresses this environment may reach before a connected test uses it")
			return
		}
		for _, operation := range operations {
			if slices.ContainsFunc(policy.Rules, func(r sendpolicy.ScopeRule) bool { return r.Endpoint == endpoint && r.Operation == operation }) {
				continue
			}
			policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: endpoint, Operation: operation, Port: number, Destinations: slices.Clone(approved.ApprovedDestinations), Selection: "single-address"})
		}
	}
	environment := connectedtest.Environment{Project: project, ID: contract.Environment, Revision: contract.Revision, Name: primary.name, Classification: "nonproduction", TargetRevision: connectedtest.TargetRevision{Provenance: "unknown"}}
	if classification != replay.Nonproduction {
		add("test.environment", "classify this environment as Nonproduction before a connected test runs against it")
	}
	var target replay.Target
	if primary.members.fhir == nil {
		target = primary.members.target
		environment.Name, environment.Endpoint = target.Name, connectedReceiver
		environment.TLS = connectedtest.TLS{Mode: target.Transport, ServerName: target.ServerName}
		if target.ClientCertificate != "" || target.Credential.Declared() {
			add("test.environment", "a connected test reaches this environment without a client certificate; choose a plain or TLS connection")
		}
		allow("test.environment", connectedReceiver, target.Address, primary.members.policy, sendpolicy.V2Stimulus)
	} else {
		raw, _ := encodeMember(*primary.members.fhir)
		environment.Endpoint, environment.TargetIdentity = connectedServer, dataset.Digest(raw)
		environment.TLS = connectedtest.TLS{Mode: "tls", ServerName: primary.members.fhir.ServerName}
	}
	var connection *FHIRConnection
	var capability []byte
	if server != nil {
		connection = server.members.fhir
		record, err := readFHIRCheck(c.root, server.ref.ID, CheckFHIRCapabilitiesAction)
		switch {
		case err != nil || len(record.Capability) == 0:
			add("connected.server", "check this FHIR environment's capabilities before a connected test reaches it")
		case record.Check.Revision != server.ref.Revision:
			add("connected.server", "this FHIR environment changed since its capabilities were checked; check them again")
		default:
			capability = record.Capability
		}
		address := httpsAddress(connection.Base)
		allow("connected.server", connectedServer, address, server.members.policy, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.SetupAction)
		if connection.Authentication == "smart" {
			allow("connected.server", "token", httpsAddress(connection.TokenEndpoint), server.members.policy, sendpolicy.SMARTToken)
		}
		if connection.CAFile == "" {
			add("connected.server", "choose the certificate authority this FHIR connection trusts")
		}
		if connection.Authentication == "smart" {
			references, err := secret.ReadStore(filepath.Join(c.root, ProjectSecrets))
			if err == nil {
				_, err = secret.Find(references, connection.KeyReference)
			}
			if err != nil {
				add("connected.server", "register this FHIR connection's signing key reference before a connected test authorizes through it")
			}
		}
	}

	flow := connectedtest.FlowTest{Schema: connectedtest.FHIRFlowTestSchema, Project: project, ID: id, Revision: revision, Boundary: d.Boundary, Limits: connectedLimits, Variables: []connectedtest.Variable{}, Profiles: []connectedtest.Reference{}, Steps: []connectedtest.Step{}, Phases: []connectedtest.FlowPhase{}}
	declared := map[string]string{}
	for _, v := range d.Variables {
		flow.Variables = append(flow.Variables, connectedtest.Variable{ID: v.ID, Kind: v.Kind, Namespace: v.Namespace, Value: v.Value, OffsetMS: v.OffsetMS})
		declared[v.ID] = v.Kind
	}
	if server != nil && capability != nil {
		flow.Servers = []connectedtest.FHIRServer{{ID: connectedServer, Base: connection.Base, Capability: ref("capability", "fhir-r4-json", "capability.json", capability)}}
	}
	cases := map[string]string{}
	for i, s := range d.Steps {
		field := connectedField("steps", i, "source")
		read, err := c.connectedSource(s.Source)
		if err != nil {
			add(field, err.Error())
			continue
		}
		step := connectedtest.Step{ID: s.ID, After: slices.Clone(s.After), BusinessKeys: []connectedtest.BusinessKey{}}
		if step.After == nil {
			step.After = []string{}
		}
		switch {
		case s.V2 != nil:
			if read.protocol != "v2" {
				add(field, "this input sends a v2 message; choose a message of a v2 case")
				continue
			}
			step.Endpoint = connectedReceiver
			step.V2 = &connectedtest.V2Stimulus{Input: ref(s.ID+"-input", "hl7", "inputs/"+s.ID+".hl7", read.raw), Occurrence: s.Source.Occurrence, Assignments: []connectedtest.Assignment{}}
			cases[s.ID] = read.path
		case s.FHIR != nil:
			if read.protocol != "fhir" {
				add(field, "this input sends a FHIR request; choose a resource of FHIR evidence")
				continue
			}
			interaction := connectedtest.FHIRInteraction{Server: connectedServer, Method: s.FHIR.Method, Path: s.FHIR.path(), Headers: s.FHIR.headers(), Bind: slices.Clone(s.FHIR.Bind), Budget: connectedRequestBudget, Retry: fhirrest.Retry{MaxAttempts: 1}}
			if interaction.Bind == nil {
				interaction.Bind = []connectedtest.ResponseBinding{}
			}
			if s.FHIR.Method == "POST" || s.FHIR.Method == "PUT" {
				if len(read.raw) == 0 {
					add(field, "a create or update sends a resource; choose a resource of the evidence")
					continue
				}
				if read.resource != s.FHIR.Resource {
					add(connectedField("steps", i, "fhir"), "the request addresses "+s.FHIR.Resource+" but sends a "+read.resource)
				}
				raw := read.raw
				if s.FHIR.Method == "PUT" && s.FHIR.Target.Kind == "instance" {
					// FHIR requires an update's resource to carry the identity it
					// addresses: the same bound variable, never a stored value.
					if raw, err = withIdentity(read.raw, s.FHIR.Target.Variable); err != nil {
						add(field, "the resource this update sends cannot carry the identity it addresses")
						continue
					}
				}
				body := ref(s.ID+"-body", "fhir-r4-json", "inputs/"+s.ID+".json", raw)
				interaction.Body = &body
				read.raw = raw
			}
			step.Endpoint, step.Interaction = connectedServer, &interaction
			if capability != nil {
				shape := map[string]string{}
				for name, kind := range declared {
					shape[name] = "id-0"
					if kind == "literal" {
						shape[name] = d.variable(name).Value
					}
				}
				if err := fhirrest.Admits(connection.Base, capability, fhirrest.Interaction{Method: interaction.Method, URL: connection.Base + "/" + substituteShape(interaction.Path, shape), ContentType: contentType(interaction.Body), Body: []byte(substituteShape(string(read.raw), shape)), Headers: networkaction.HTTPHeadersV2{IfMatch: substituteShape(interaction.Headers.IfMatch, shape), IfNoneExist: substituteShape(interaction.Headers.IfNoneExist, shape), Prefer: interaction.Headers.Prefer}}); err != nil {
					add(connectedField("steps", i, "fhir"), "the FHIR environment's checked capabilities do not support this request")
				}
			}
		}
		flow.Steps = append(flow.Steps, step)
	}

	observations := map[string]*connectedObservationRead{}
	sources := map[string]map[string]string{}
	for p, phase := range d.Phases {
		compiled := connectedtest.FlowPhase{ID: phase.ID, Steps: slices.Clone(phase.Steps), After: slices.Clone(phase.After), When: phase.When, Datasets: []connectedtest.Dataset{}}
		if compiled.After == nil {
			compiled.After = []connectedtest.PhaseDependency{}
		}
		set := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{}, Assertions: []assertion.DatasetAssertion{}}
		sources[phase.ID] = map[string]string{}
		budget := 0
		for o, observed := range phase.Observations {
			field := connectedField("phases", p, "observations", o)
			read, err := c.connectedObservation(observed.Observation.ID, observed.Observation.Revision)
			if err != nil {
				add(field, "this observation cannot be read: "+err.Error())
				continue
			}
			observations[read.ref.ID+"@"+read.ref.Revision] = read
			if read.definition.Barrier != nil || read.setup.BarrierObservation != "" {
				add(field, "this observation completes on a processing barrier, which a connected test in this release does not wait for; choose a full-horizon observation")
				continue
			}
			name := phase.ID + "-" + observed.Dataset
			interval := ref(name+"-window", observeinterval.Schema, "observations/"+name+"-window.json", read.interval)
			ds := connectedtest.Dataset{ID: observed.Dataset, Namespace: read.setup.Namespace, Phase: observed.When, Source: read.source, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: read.definition.HorizonMS, MaxRecords: read.definition.MaxRecords, MaxBytes: read.definition.MaxBytes, Policy: &interval}}
			budget += read.definition.MaxBytes
			if read.kind == "fhir" {
				if server == nil || !bind.promoted && read.setup.Environment != server.ref.ID {
					add(field, "this FHIR observation reaches another environment than this test's FHIR server")
					continue
				}
				for _, variable := range read.fhir.Variables() {
					if kind := declared[variable]; kind == "" || kind == "response" {
						add(field, "declare the variable "+variable+" this observation's search uses")
					}
				}
				projection := ref(name, fhirobserve.Schema, "observations/"+name+".json", read.member)
				ds.Kind, ds.Projection = "fhir-resources", &projection
				allow("connected.server", observed.Dataset, httpsAddress(connection.Base), server.members.policy, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch)
			} else {
				if read.typed.Observes.Kind != observesource.FileExport || read.typed.File == nil {
					add(field, "this observation reads its source through a collector that needs its own authority; a connected test in this release reads file exports and FHIR observations")
					continue
				}
				projection := ref(name, dataset.ProjectionSchema, "observations/"+name+".json", read.member)
				ds.Kind, ds.Projection = "typed-rows", &projection
				sources[phase.ID][observed.Dataset] = read.paths["source"]
			}
			compiled.Datasets = append(compiled.Datasets, ds)
			set.Bindings = append(set.Bindings, assertion.DatasetBinding{ProjectionIdentity: read.projection, Name: observed.Dataset, Namespace: read.setup.Namespace, Phase: observed.When, Source: read.source})
		}
		if budget > connectedLimits.MaxBytes {
			add(connectedField("phases", p, "observations"), "the observations this phase reads may collect more than one test holds; lower their byte limits")
		}
		for _, check := range phase.Checks {
			set.Assertions = append(set.Assertions, check.Check)
		}
		// An imported clause is decided exactly as it was declared.
		wireClauses := []assertion.Assertion{}
		for c, clause := range phase.Unsupported {
			field := connectedField("phases", p, "unsupported", c)
			var err error
			switch clause.Kind {
			case "check":
				var check assertion.DatasetAssertion
				if err = json.Unmarshal([]byte(clause.Text), &check, json.RejectUnknownMembers(true)); err == nil {
					set.Assertions = append(set.Assertions, check)
				}
			case "acknowledgement":
				var check assertion.Assertion
				if err = json.Unmarshal([]byte(clause.Text), &check, json.RejectUnknownMembers(true)); err == nil {
					wireClauses = append(wireClauses, check)
				}
			case "validation":
				var check connectedtest.ValidationCheck
				if err = json.Unmarshal([]byte(clause.Text), &check, json.RejectUnknownMembers(true)); err == nil {
					compiled.Validations = append(compiled.Validations, check)
				}
			}
			if err != nil {
				add(field, "this imported check cannot be read")
			}
		}
		if len(set.Bindings) > 0 && len(set.Assertions) > 0 {
			raw, _ := encodeMember(set)
			if _, err := assertion.DecodeDatasets(raw); err != nil {
				add(connectedField("phases", p, "checks"), "these checks cannot be decided as written")
			}
			checks := ref(phase.ID+"-checks", assertion.DatasetSchema, "checks/"+phase.ID+".json", raw)
			compiled.Checks = checks
		}
		for _, check := range phase.Responses {
			compiled.Responses = append(compiled.Responses, check.Check)
		}
		for _, check := range phase.Validations {
			compiled.Validations = append(compiled.Validations, connectedValidation(check.ID, check.Step))
		}
		if len(phase.Acks) > 0 || len(wireClauses) > 0 {
			wire := assertion.Set{Schema: assertion.Schema, Name: "Acknowledgements", Assertions: []assertion.Assertion{}}
			for _, check := range phase.Acks {
				code := check.Code
				occurrence := ""
				if i := slices.IndexFunc(d.Steps, func(s ConnectedStep) bool { return s.ID == check.Step }); i >= 0 {
					occurrence = d.Steps[i].Source.Occurrence
				}
				wire.Assertions = append(wire.Assertions, assertion.Assertion{ID: check.ID, Operator: assertion.FieldEquals, Subject: assertion.Subject{Field: &assertion.FieldRef{Scope: assertion.ObservedMessages, Message: occurrence, Selector: "MSA-1"}}, Expected: assertion.Expected{Field: &assertion.FieldValue{State: "present", Text: &code}}})
			}
			wire.Assertions = append(wire.Assertions, wireClauses...)
			raw, _ := encodeMember(wire)
			compiled.Wire = &connectedtest.WireChecks{Set: ref(phase.ID+"-acknowledgements", assertion.Schema, "checks/"+phase.ID+"-acknowledgements.json", raw), Observed: "transport-acks"}
		}
		flow.Phases = append(flow.Phases, compiled)
	}
	validations := slices.ContainsFunc(d.Phases, func(p ConnectedPhase) bool {
		return len(p.Validations) > 0 || slices.ContainsFunc(p.Unsupported, func(c ConnectedClause) bool { return c.Kind == "validation" })
	})
	if validations && connection != nil && connection.validatorState() != "capability-installed-worker-not-checked" {
		add("connected.server", "this FHIR environment has no installed local validator; choose one in its connection, or remove the validation checks")
	}

	policyRaw, _ := encodeMember(policy)
	if len(policy.Rules) > 0 {
		if _, err := sendpolicy.DecodeScopedPolicy(policyRaw); err != nil {
			add("test.environment", "the addresses this environment approves cannot scope a connected test")
		}
	}
	environment.Grants = []connectedtest.Reference{ref("address-policy", sendpolicy.ScopedPolicySchema, "address-policy.json", policyRaw)}
	environment.AddressPolicyIdentity = dataset.Digest(policyRaw)
	if primary.members.fhir == nil && len(cases) > 0 {
		occurrences := []string{}
		var casePath string
		for _, s := range d.Steps {
			if s.V2 != nil && cases[s.ID] != "" {
				casePath = cases[s.ID]
				occurrences = append(occurrences, s.Source.Occurrence)
				break
			}
		}
		prepared, err := replay.PrepareScopedSequence(casePath, target, replay.Options{Occurrences: occurrences})
		if err != nil {
			add("test.environment", "this environment's target cannot send these messages: "+err.Error())
		} else {
			environment.TargetIdentity = prepared.Target().Identity()
		}
	} else if primary.members.fhir == nil {
		raw, _ := encodeMember(target)
		environment.TargetIdentity = dataset.Digest(raw)
	}
	flow.Environment = environment
	contractRaw, _ := encodeMember(contract)
	flow.Isolation = ref("isolation", testisolation.ContractSchema, "isolation.json", contractRaw)
	if len(problems) > 0 {
		return nil, problems
	}
	raw, err := encodeMember(flow)
	if err != nil {
		add("connected", "this test cannot be encoded within its bounds")
		return nil, problems
	}
	plan, err := connectedtest.CompileFlow(raw, files, d.Generation)
	if err != nil {
		add("connected", "this test cannot be compiled as written: "+err.Error())
		return nil, problems
	}

	registryRaw, _ := encodeMember(registry)
	isolationRaw, _ := encodeMember(isolationPolicy)
	own := map[string][]byte{"address-policy.json": policyRaw, "isolation-registry.json": registryRaw, "isolation-policy.json": isolationRaw}
	var targetFile string
	if primary.members.fhir == nil {
		copied := target
		if copied.CAFile != "" && !filepath.IsAbs(copied.CAFile) {
			copied.CAFile = filepath.Join(c.root, copied.CAFile)
		}
		raw, _ := json.Marshal(copied, json.Deterministic(true))
		own["target.json"], targetFile = append(raw, '\n'), "target.json"
	}
	servers := map[string]connectedrun.FHIRServerSelection{}
	var validation *connectedrun.ValidationSelection
	if server != nil {
		authorities, err := (artifactdir.Document{MaxBytes: 1 << 20}).Read(connection.CAFile)
		if err != nil {
			add("connected.server", "the certificate authority this FHIR connection trusts cannot be read")
			return nil, problems
		}
		own["authorities.pem"] = authorities
		selection := connectedrun.FHIRServerSelection{Authorities: "authorities.pem", ServerName: connection.ServerName, Observation: connectedrun.FHIRAuthorization{Mode: "none"}, Action: connectedrun.FHIRAuthorization{Mode: "none"}}
		if connection.Authentication == "smart" {
			for _, role := range []struct{ name, file, grant string }{{"observer", "smart-observer.json", "token:" + connectedServer + ":observation"}, {"setup", "smart-setup.json", "token:" + connectedServer + ":action"}} {
				client, err := c.smartClient(*connection, role.name, plan.Identity(), authorities, policyRaw)
				if err != nil {
					add("connected.server", err.Error())
					return nil, problems
				}
				own[role.file] = client
				grant := connectedGrant(role.grant)
				authorization := connectedrun.FHIRAuthorization{Mode: "smart", Client: role.file, Token: &grant}
				if role.name == "observer" {
					selection.Observation = authorization
				} else {
					selection.Action = authorization
				}
			}
		}
		servers[connectedServer] = selection
		if validations {
			validation = &connectedrun.ValidationSelection{Capability: connection.Validation.Capability, Engine: connection.Validation.Engine, Socket: connection.Validation.Socket}
		}
	}
	compiledPlan := plan
	return &connectedCompiled{plan: plan, flow: flow, files: own, config: func(place func(string) string) connectedrun.FHIRFlowConfig {
		config := connectedrun.FHIRFlowConfig{Schema: connectedrun.FHIRFlowConfigSchema, Policy: "address-policy.json", Servers: servers, Phases: map[string]connectedrun.FHIRPhaseSelection{}, Validation: validation, Seed: d.Generation.Seed,
			Isolation: connectedrun.IsolationSelection{Registry: "isolation-registry.json", Policy: "isolation-policy.json", Read: connectedGrant("isolation:read"), Setup: connectedGrant("isolation:setup"), Cleanup: connectedGrant("isolation:cleanup")}}
		if validation != nil {
			copied := *validation
			copied.Capability = place(copied.Capability)
			if copied.Socket != "" {
				copied.Socket = place(copied.Socket)
			}
			config.Validation = &copied
		}
		document := compiledPlan.Document()
		for _, phase := range document.Test.Phases {
			selection := connectedrun.FHIRPhaseSelection{Sources: map[string]connectedrun.SourceSelection{}, Barriers: map[string]connectedrun.SourceSelection{}, Grants: map[string]connectedrun.Grant{}}
			requests := false
			for _, id := range phase.Steps {
				if path := cases[id]; path != "" {
					send := connectedGrant(phase.ID + ":stimulus")
					selection.Case, selection.Target, selection.Send = place(path), targetFile, &send
					continue
				}
				selection.Grants["step:"+id] = connectedGrant(phase.ID + ":step:" + id)
				requests = true
			}
			if requests {
				selection.Grants["preflight:"+connectedServer] = connectedGrant(phase.ID + ":preflight:" + connectedServer)
			}
			for _, ds := range phase.Datasets {
				if ds.Kind == "fhir-resources" {
					selection.Grants["dataset:"+ds.ID] = connectedGrant(phase.ID + ":dataset:" + ds.ID)
					continue
				}
				selection.Sources[ds.ID] = connectedrun.SourceSelection{Path: place(sources[phase.ID][ds.ID])}
			}
			config.Phases[phase.ID] = selection
		}
		return config
	}}, nil
}

// connectedGrant is where the runner's operator provisions the grant of one
// binding, beside the configuration: a location, never an authority.
func connectedGrant(name string) connectedrun.Grant {
	return connectedrun.Grant{Path: "grants/" + strings.ReplaceAll(name, ":", "-") + ".json", Actor: "runner", Generation: "1"}
}

// smartClient is one role's SMART Backend Services client of a FHIR
// connection, as the connection registers it: its client, scopes and the key
// reference the project's credential registry names. No key is read.
func (c *loadedCatalog) smartClient(connection FHIRConnection, role, plan string, authorities, policy []byte) ([]byte, error) {
	keys, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(connection.PublicKeysFile)
	var public smartbackend.JWKS
	if err != nil || json.Unmarshal(keys, &public, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("choose this FHIR connection's registered public keys")
	}
	references, err := secret.ReadStore(filepath.Join(c.root, ProjectSecrets))
	if err != nil {
		return nil, errors.New("register this FHIR connection's signing key reference")
	}
	key, err := secret.Find(references, connection.KeyReference)
	if err != nil || key.Purpose != secret.SourceEndpoint || key.Address != httpsAddress(connection.TokenEndpoint) {
		return nil, errors.New("register a signing key reference scoped to this FHIR connection's token endpoint")
	}
	u, _ := url.Parse(connection.TokenEndpoint)
	token := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: plan, Source: networkaction.Digest([]byte("connected-smart-" + role)), Project: c.document.Project.ID, Endpoint: "token", Classification: string(connection.Classification), Operation: sendpolicy.SMARTToken, Method: "POST", URL: connection.TokenEndpoint, ServerName: u.Hostname(), Authorities: authorities, TimeoutMS: 60000, MaxBytes: 64 << 10}
	var scoped sendpolicy.ScopedPolicy
	if json.Unmarshal(policy, &scoped) == nil {
		token.Environment, token.Revision = scoped.Environment, scoped.Revision
	}
	config := smartbackend.Config{Schema: smartbackend.ConfigSchema, FHIRBase: connection.Base, TokenEndpoint: connection.TokenEndpoint, ClientID: connection.ClientID, Audience: connection.TokenEndpoint, Algorithm: connection.Algorithm, Role: role, Scopes: connection.Scopes,
		Key: smartbackend.KeyReference{Kid: connection.KeyID, Generation: "generation-" + strconv.Itoa(key.Generation), Locator: networkaction.Provider{Command: key.Command, Arguments: key.Arguments}, JWKS: public}, Token: token}
	raw, _ := encodeMember(config)
	if _, err := smartbackend.Prepare(raw, policy, nil); err != nil {
		return nil, errors.New("this FHIR connection's SMART registration does not authorize a separate " + role + " role for a connected test; register its client and scopes for that role")
	}
	return raw, nil
}

// path is the address a request's typed choices compile to.
func (f ConnectedFHIR) path() string {
	switch f.Target.Kind {
	case "instance":
		return f.Resource + "/{" + f.Target.Variable + "}"
	case "conditional":
		return f.Resource + "?identifier=" + url.QueryEscape(f.Target.Identifier.System+"|"+f.Target.Identifier.Value)
	}
	return f.Resource
}

// headers are the conditions a request's typed choices compile to.
func (f ConnectedFHIR) headers() connectedtest.FHIRHeaders {
	h := connectedtest.FHIRHeaders{Prefer: f.Prefer}
	if f.IfNone != nil {
		h.IfNoneExist = "identifier=" + f.IfNone.System + "|" + f.IfNone.Value
	}
	if f.IfMatch != "" {
		h.IfMatch = `W/"{` + f.IfMatch + `}"`
	}
	return h
}

func (d ConnectedTestDraft) variable(id string) ConnectedVariable {
	for _, v := range d.Variables {
		if v.ID == id {
			return v
		}
	}
	return ConnectedVariable{}
}

// substituteShape fills every {variable} placeholder with an example of its
// shape, so a request is checked against the capabilities before any value
// exists.
func substituteShape(text string, values map[string]string) string {
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func contentType(body *connectedtest.Reference) string {
	if body == nil {
		return ""
	}
	return "application/fhir+json"
}

// writeConnected writes a compiled test into folder: its sealed lifecycle as
// plan and its selection as config.json beside its own documents. Files of
// the project it reads in place are named from the folder.
func (compiled *connectedCompiled) write(ctx context.Context, folder, root string) (string, string, error) {
	plan := filepath.Join(folder, "plan")
	if err := compiled.plan.Write(ctx, plan); err != nil {
		return "", "", err
	}
	config := filepath.Join(folder, "config")
	if err := os.MkdirAll(filepath.Join(config, "grants"), 0o700); err != nil {
		return "", "", err
	}
	for name, raw := range compiled.files {
		if err := (artifactdir.Document{MaxBytes: catalog.MaxMemberBytes}).Create(filepath.Join(config, name), raw); err != nil {
			return "", "", err
		}
	}
	inside := func(path string) bool {
		relative, err := filepath.Rel(root, path)
		return root != "" && err == nil && filepath.IsLocal(relative)
	}
	selection := compiled.config(func(path string) string {
		if inside(path) && inside(config) {
			if relative, err := filepath.Rel(config, path); err == nil {
				return relative
			}
		}
		return path
	})
	raw, err := encodeMember(selection)
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(config, "config.json")
	if err := (artifactdir.Document{MaxBytes: 2 << 20}).Create(path, raw); err != nil {
		return "", "", err
	}
	return plan, path, nil
}

// prepareConnected proves a compiled test prepares exactly as the connected
// runner prepares it, in a private folder removed afterwards: the lifecycle,
// its selection and every file they name. Preparation reads files only.
func (compiled *connectedCompiled) prepare(ctx context.Context) error {
	folder, err := os.MkdirTemp("", "readmit-connected-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(folder)
	plan, config, err := compiled.write(ctx, folder, "")
	if err != nil {
		return err
	}
	_, err = connectedrun.PrepareFlow(plan, config, "editor-preview")
	return err
}

// withIdentity is a resource's original JSON with its top-level identity
// replaced by one variable's placeholder; every other member keeps its bytes
// and order.
func withIdentity(raw []byte, variable string) ([]byte, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		return nil, errors.New("not a resource")
	}
	identity, _ := jsontext.AppendQuote(nil, "{"+variable+"}")
	out := []byte{'{'}
	placed := false
	member := func(name string, value []byte) {
		if len(out) > 1 {
			out = append(out, ',')
		}
		out, _ = jsontext.AppendQuote(out, name)
		out = append(append(out, ':'), value...)
	}
	for decoder.PeekKind() != '}' {
		token, err := decoder.ReadToken()
		if err != nil || token.Kind() != '"' {
			return nil, errors.New("not a resource")
		}
		name := token.String()
		value, err := decoder.ReadValue()
		if err != nil {
			return nil, err
		}
		if name == "id" {
			continue
		}
		member(name, value)
		if name == "resourceType" && !placed {
			member("id", identity)
			placed = true
		}
	}
	if !placed {
		member("id", identity)
	}
	return append(out, '}'), nil
}
