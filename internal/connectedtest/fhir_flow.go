package connectedtest

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
)

const FHIRFlowTestSchema = "readmit-connected-test/v5"
const FHIRFlowPlanSchema = "readmit-execution-plan/v5"

// FHIRServer pins one FHIR R4 base and the reviewed CapabilityStatement its
// interactions and observations were authored against.
type FHIRServer struct {
	ID         string    `json:"id"`
	Base       string    `json:"base"`
	Capability Reference `json:"capability"`
}
type FHIRHeaders struct {
	IfMatch     string `json:"if_match,omitzero"`
	IfNoneExist string `json:"if_none_exist,omitzero"`
	Prefer      string `json:"prefer,omitzero"`
}

// ResponseBinding assigns a server-assigned logical ID or version from this
// step's actual response to a declared response variable. Exactly one value
// must exist; it is used only to address later requests, never as an expected
// clinical value. Scope "phase" limits its use to the binding step's phase;
// "lifecycle" lets later phases that depend on it use it too.
type ResponseBinding struct {
	Variable     string `json:"variable"`
	From         string `json:"from"`
	Multiplicity string `json:"multiplicity"`
	Scope        string `json:"scope"`
}

// FHIRInteraction is one reviewed request. Path, headers and body may name
// variables as {id}: compiled variables are substituted at preparation;
// response variables only at execution, from the named earlier response.
type FHIRInteraction struct {
	Server  string            `json:"server"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    *Reference        `json:"body,omitzero"`
	Headers FHIRHeaders       `json:"headers"`
	Bind    []ResponseBinding `json:"bind"`
	Budget  fhirrest.Budget   `json:"budget"`
	Retry   fhirrest.Retry    `json:"retry"`
}

// ResponseCheck validates a step's HTTP outcome class. It is response evidence
// and is reported separately from downstream workflow checks.
type ResponseCheck struct {
	ID      string `json:"id"`
	Step    string `json:"step"`
	Outcome string `json:"outcome"`
}

// ValidationCheck asks the optional local validator to check a step's returned
// resource. Without an installed capability it is retained as undecided.
type ValidationCheck struct {
	ID             string                     `json:"id"`
	Step           string                     `json:"step"`
	Profiles       []fhirvalidator.Canonical  `json:"profiles"`
	Requirements   fhirvalidator.Requirements `json:"requirements"`
	TimeoutMS      int64                      `json:"timeout_ms"`
	MaxOutputBytes int64                      `json:"max_output_bytes"`
}

var placeholderPattern = regexp.MustCompile(`\{([a-z][a-z0-9-]{0,63})\}`)
var serverAssigned = regexp.MustCompile(`^[A-Za-z0-9\-.]{1,64}$`)

// ServerAssignedID reports whether a response value is a FHIR logical id or
// version, the only values a response variable can hold.
func ServerAssignedID(v string) bool { return serverAssigned.MatchString(v) && v != "." && v != ".." }

// ResponseOutcomes are the finite HTTP outcome classes a response check may name.
var ResponseOutcomes = []string{"succeeded", "not-modified", "conflict", "not-found", "pending", "rejected", "rejected-transaction", "partial-failure", "unauthorized", "forbidden", "throttled", "unavailable"}

// FHIRCall is one resolved request, ready for the shared FHIR executor.
type FHIRCall struct {
	Server      string
	Base        string
	Method      string
	URL         string
	ContentType string
	Body        []byte
	Headers     networkaction.HTTPHeadersV2
	Budget      fhirrest.Budget
	Retry       fhirrest.Retry
	Bind        []ResponseBinding
}

// ResponseVariables names the response variables a step's request needs,
// including any its authored body names.
func ResponseVariables(s Step, variables []Variable, body []byte) []string {
	out := []string{}
	if s.Interaction == nil {
		return out
	}
	runtime := map[string]bool{}
	for _, v := range variables {
		runtime[v.ID] = v.Kind == "response"
	}
	texts := []string{s.Interaction.Path, s.Interaction.Headers.IfMatch, s.Interaction.Headers.IfNoneExist, s.Interaction.Headers.Prefer, string(body)}
	for _, text := range texts {
		for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
			if runtime[m[1]] && !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// FHIRCall resolves a compiled step with values bound from actual earlier
// responses. Every response variable must be supplied and be a FHIR id.
func (p *Plan) FHIRCall(step string, bound map[string]string) (FHIRCall, error) {
	d := p.document.Test
	i := slices.IndexFunc(d.Steps, func(s Step) bool { return s.ID == step })
	if i < 0 || d.Steps[i].Interaction == nil {
		return FHIRCall{}, invalid
	}
	body := p.files["inputs/"+step+".fhir"]
	values := map[string]string{}
	for k, v := range p.document.Resolution {
		values[k] = v
	}
	for _, v := range d.Variables {
		if v.Kind != "response" {
			continue
		}
		if value, ok := bound[v.ID]; ok {
			if !ServerAssignedID(value) {
				return FHIRCall{}, invalid
			}
			values[v.ID] = value
		}
	}
	return resolveCall(d, d.Steps[i], body, values)
}

// FHIRCallShape resolves a step with a syntactically valid id for every
// response variable, so its capability needs are checked before any value
// exists. It is never sent.
func (p *Plan) FHIRCallShape(step string) (FHIRCall, error) {
	return p.FHIRCall(step, exampleResponses(p.document.Test.Variables))
}
func exampleResponses(variables []Variable) map[string]string {
	out := map[string]string{}
	for _, v := range variables {
		if v.Kind == "response" {
			out[v.ID] = "id-0"
		}
	}
	return out
}

// Interaction is the call's request shape for the shared capability checks.
func (c FHIRCall) Interaction() fhirrest.Interaction {
	return fhirrest.Interaction{Method: c.Method, URL: c.URL, ContentType: c.ContentType, Body: c.Body, Headers: c.Headers}
}

func resolveCall(d Test, s Step, body []byte, values map[string]string) (FHIRCall, error) {
	in := s.Interaction
	j := slices.IndexFunc(d.Servers, func(x FHIRServer) bool { return x.ID == in.Server })
	if j < 0 {
		return FHIRCall{}, invalid
	}
	base := d.Servers[j].Base
	pathPart, query, hasQuery := strings.Cut(in.Path, "?")
	path, err := substitute(pathPart, values, nil, url.PathEscape)
	if err != nil {
		return FHIRCall{}, err
	}
	if hasQuery {
		q, e := substitute(query, values, nil, url.QueryEscape)
		if e != nil {
			return FHIRCall{}, e
		}
		path += "?" + q
	}
	header := func(v string) (string, error) {
		out, e := substitute(v, values, nil, func(s string) string { return s })
		if e != nil || strings.ContainsAny(out, "\r\n") {
			return "", invalid
		}
		return out, nil
	}
	c := FHIRCall{Server: in.Server, Base: base, Method: in.Method, URL: base + "/" + path, Budget: in.Budget, Retry: in.Retry, Bind: slices.Clone(in.Bind)}
	if c.Headers.IfMatch, err = header(in.Headers.IfMatch); err != nil {
		return FHIRCall{}, err
	}
	if c.Headers.IfNoneExist, err = header(in.Headers.IfNoneExist); err != nil {
		return FHIRCall{}, err
	}
	if c.Headers.Prefer, err = header(in.Headers.Prefer); err != nil {
		return FHIRCall{}, err
	}
	if in.Body != nil {
		text, e := substitute(string(body), values, nil, jsonString)
		if e != nil {
			return FHIRCall{}, e
		}
		c.Body = []byte(text)
		c.ContentType = "application/fhir+json"
		if in.Method == "PATCH" {
			c.ContentType = "application/json-patch+json"
		}
	}
	return c, nil
}

// substitute replaces {id} placeholders; names in keep stay for execution.
func substitute(text string, values map[string]string, keep map[string]bool, escape func(string) string) (string, error) {
	var err error
	out := placeholderPattern.ReplaceAllStringFunc(text, func(m string) string {
		name := m[1 : len(m)-1]
		if keep[name] {
			return m
		}
		v, ok := values[name]
		if !ok || strings.ContainsAny(v, "{}") {
			err = invalid
			return m
		}
		return escape(v)
	})
	return out, err
}
func jsonString(v string) string {
	raw, _ := json.Marshal(v)
	return string(raw[1 : len(raw)-1])
}

// compileInteraction checks one reviewed request offline against its server's
// pinned CapabilityStatement, substituting compiled variables into the body.
func compileInteraction(p *Plan, d Test, s Step, runtime map[string]bool, resolve func(Reference, string) ([]byte, error)) error {
	in := s.Interaction
	j := slices.IndexFunc(d.Servers, func(x FHIRServer) bool { return x.ID == in.Server })
	if j < 0 || s.Endpoint != in.Server || s.FHIR != nil || s.V2 != nil || !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, in.Method) || in.Path == "" || len(in.Path) > 4096 || strings.HasPrefix(in.Path, "/") || strings.Contains(in.Path, "..") || strings.ContainsAny(in.Path, "#\\\r\n ") || strings.Contains(in.Path, "://") {
		return invalid
	}
	if !in.Budget.Valid() || !in.Retry.Valid() {
		return invalid
	}
	if (in.Body != nil) != (in.Method == "POST" || in.Method == "PUT" || in.Method == "PATCH") {
		return invalid
	}
	body := []byte(nil)
	if in.Body != nil {
		schema := "fhir-r4-json"
		if in.Method == "PATCH" {
			schema = "json-patch"
		}
		raw, err := resolve(*in.Body, schema)
		if err != nil {
			return err
		}
		text, err := substitute(string(raw), p.document.Resolution, runtime, jsonString)
		if err != nil {
			return err
		}
		body = []byte(text)
		p.files["inputs/"+s.ID+".fhir"] = body
	}
	seen := map[string]bool{}
	for _, b := range in.Bind {
		if !runtime[b.Variable] || seen[b.Variable] || b.From != "logical-id" && b.From != "version-id" || b.Multiplicity != "exactly-one" || b.Scope != "phase" && b.Scope != "lifecycle" {
			return invalid
		}
		seen[b.Variable] = true
	}
	// A response variable placeholder resolves to a concrete FHIR id at
	// execution; check the request shape with a syntactically equivalent id.
	example := exampleResponses(d.Variables)
	for k, v := range p.document.Resolution {
		example[k] = v
	}
	call, err := resolveCall(d, s, body, example)
	if err != nil {
		return err
	}
	if call.Body != nil {
		var value map[string]jsontext.Value
		var patch []map[string]jsontext.Value
		if in.Method == "PATCH" && json.Unmarshal(call.Body, &patch) != nil || in.Method != "PATCH" && (json.Unmarshal(call.Body, &value) != nil || value["resourceType"] == nil) {
			return invalid
		}
	}
	capability := p.files["dependencies/"+d.Servers[j].Capability.SHA256]
	return fhirrest.Admits(call.Base, capability, call.Interaction())
}

// compileServers pins every declared base and its reviewed capability bytes.
func compileServers(d Test, resolve func(Reference, string) ([]byte, error)) error {
	seen := map[string]bool{}
	for _, server := range d.Servers {
		if !identifier.MatchString(server.ID) || seen[server.ID] || !fhirrequest.ValidBase(server.Base) {
			return invalid
		}
		seen[server.ID] = true
		raw, err := resolve(server.Capability, "fhir-r4-json")
		if err != nil {
			return err
		}
		if fhirrest.Admits(server.Base, raw, fhirrest.Interaction{Method: "GET", URL: server.Base + "/metadata"}) != nil {
			return invalid
		}
	}
	return nil
}

// compileObservation pins one FHIR dataset: its reviewed search, typed columns
// and interval definition. Query variables must be compiled variables.
func compileObservation(d Test, ds Dataset, raw []byte, definition observeinterval.Definition, resolution map[string]string) (fhirobserve.Observation, error) {
	o, err := fhirobserve.Decode(raw)
	if err != nil || !slices.ContainsFunc(d.Servers, func(s FHIRServer) bool { return s.ID == o.Server }) || ds.Source != o.Identity() || !identifier.MatchString(ds.Namespace) || definition.Mode != "snapshots" || definition.Freshness != "snapshot-only" || o.MaxRows > ds.Completion.MaxRecords || o.Budget.Bytes > ds.Completion.MaxBytes {
		return o, invalid
	}
	for _, v := range o.Variables() {
		if _, ok := resolution[v]; !ok {
			return o, invalid
		}
	}
	return o, nil
}

// ObservationURL is the exact compiled search address of a FHIR dataset.
func (p *Plan) ObservationURL(id string) (fhirobserve.Observation, string, string, error) {
	d := p.document.Test
	i := slices.IndexFunc(d.Datasets, func(x Dataset) bool { return x.ID == id })
	if i < 0 || d.Datasets[i].Kind != "fhir-resources" || d.Datasets[i].Projection == nil {
		return fhirobserve.Observation{}, "", "", invalid
	}
	o, err := fhirobserve.Decode(p.files["dependencies/"+d.Datasets[i].Projection.SHA256])
	if err != nil {
		return o, "", "", err
	}
	j := slices.IndexFunc(d.Servers, func(s FHIRServer) bool { return s.ID == o.Server })
	if j < 0 {
		return o, "", "", invalid
	}
	address, err := o.URL(d.Servers[j].Base, p.document.Resolution)
	return o, d.Servers[j].Base, address, err
}

// ServerCapability is a server's pinned reviewed CapabilityStatement.
func (p *Plan) ServerCapability(id string) ([]byte, string, bool) {
	for _, s := range p.document.Test.Servers {
		if s.ID == id {
			return slices.Clone(p.files["dependencies/"+s.Capability.SHA256]), s.Base, true
		}
	}
	return nil, "", false
}

func validateFHIRChecks(d Test) error {
	interactions := map[string]bool{}
	for _, s := range d.Steps {
		interactions[s.ID] = s.Interaction != nil
	}
	ids := map[string]bool{}
	for _, c := range d.Responses {
		if !identifier.MatchString(c.ID) || ids["response:"+c.ID] || !interactions[c.Step] || !slices.Contains(ResponseOutcomes, c.Outcome) {
			return invalid
		}
		ids["response:"+c.ID] = true
	}
	for _, c := range d.Validations {
		r := c.Requirements
		if !identifier.MatchString(c.ID) || ids["validation:"+c.ID] || !interactions[c.Step] || len(c.Profiles) > 32 || c.TimeoutMS < 1 || c.TimeoutMS > 300000 || c.MaxOutputBytes < 1024 || c.MaxOutputBytes > 16<<20 || !slices.Contains([]string{"required", "not-requested"}, r.Terminology) || !slices.Contains([]string{"required", "not-requested"}, r.Invariants) || !slices.Contains(r.FailSeverities, "fatal") || !slices.Contains(r.FailSeverities, "error") || len(r.FailSeverities) > 4 {
			return invalid
		}
		for _, s := range r.FailSeverities {
			if !slices.Contains([]string{"fatal", "error", "warning", "information"}, s) {
				return invalid
			}
		}
		ids["validation:"+c.ID] = true
	}
	return nil
}
