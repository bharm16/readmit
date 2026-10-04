package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/smartbackend"
	"github.com/bharm16/readmit/internal/testisolation"
)

// InputIdentity identifies the reviewed plan, exact selected configuration and
// every prepared runtime binding. It resolves no credentials and reads no
// grants. A fixed metadata instance prevents per-execution fixture owners and
// allocated identifiers from changing a suite's approved input pin.
func (p *PreparedFlow) InputIdentity() (string, error) {
	raw, err := p.InputSnapshot()
	if err != nil {
		return "", err
	}
	return networkaction.Digest(raw), nil
}

// RegistrySnapshot retains the exact approved registry encoding separately
// from the actual isolation plan's decoded registry. It resolves no provider.
func (p *PreparedFlow) RegistrySnapshot() ([]byte, error) {
	if p == nil || p.unchanged() != nil {
		return nil, invalid
	}
	return (artifactdir.Document{MaxBytes: 1 << 20}).Read(p.selection.Registry)
}

// InputSnapshot returns the exact canonical metadata InputIdentity hashes,
// for private manifest-linked offline inspection. It contains only plan,
// configuration and dependency digests plus value-free scoped bindings, never
// credential values, grant contents or execution-derived fixture identities.
func (p *PreparedFlow) InputSnapshot() ([]byte, error) {
	if p == nil || p.unchanged() != nil {
		return nil, invalid
	}
	metadata, err := PrepareFlow(p.planPath, p.configPath, "input-metadata")
	if err != nil {
		return nil, err
	}
	if validationIdentity(p) != validationIdentity(metadata) || !bytes.Equal(canonicalFlow(sourceIdentities(p)), canonicalFlow(sourceIdentities(metadata))) {
		return nil, invalid
	}
	registry, err := (artifactdir.Document{MaxBytes: 1 << 20}).Read(p.selection.Registry)
	if err != nil {
		return nil, err
	}
	policy, err := (artifactdir.Document{MaxBytes: 1 << 20}).Read(p.selection.Policy)
	if err != nil {
		return nil, err
	}
	input := struct {
		Plan          string                           `json:"plan"`
		Configuration string                           `json:"configuration"`
		Bindings      map[string]networkaction.Binding `json:"bindings"`
		Sources       map[string]string                `json:"sources"`
		Registry      string                           `json:"isolation_registry"`
		Policy        string                           `json:"isolation_policy"`
		Validation    string                           `json:"validation"`
	}{p.plan.Identity(), networkaction.Digest(p.raw), metadata.Bindings(), sourceIdentities(metadata), networkaction.Digest(registry), networkaction.Digest(policy), validationIdentity(metadata)}
	raw, err := json.Marshal(input, json.Deterministic(true))
	if err != nil {
		return nil, invalid
	}
	return raw, nil
}

// Capabilities names the engine and contracts actually selected by this
// verified plan and configuration. It is a requirement/pin handshake only;
// installed worker readiness and live server preflight remain separate checks.
func (p *PreparedFlow) Capabilities() (runnerprotocol.Capabilities, error) {
	if _, err := p.InputIdentity(); err != nil {
		return runnerprotocol.Capabilities{}, err
	}
	if err := p.validationRequirements(); err != nil {
		return runnerprotocol.Capabilities{}, err
	}
	capabilities := runnerprotocol.Capabilities{Schema: runnerprotocol.CapabilitiesSchema, Engine: engine.Version(), Pins: []runnerprotocol.CapabilityPin{}}
	pins := map[runnerprotocol.CapabilityPin]bool{}
	add := func(pin runnerprotocol.CapabilityPin) { pins[pin] = true }
	contract := func(schema string) {
		if schema == "" {
			return
		}
		id, version, ok := strings.Cut(schema, "/v")
		if ok {
			add(runnerprotocol.CapabilityPin{Kind: "contract", ID: id, Version: "v" + version})
		}
	}
	d := p.plan.Document()
	contract(d.Schema)
	contract(d.Test.Schema)
	contract(flowSchemaFor(p.plan))
	contract(d.Test.Isolation.Schema)
	contract(testisolation.RegistrySchema)
	contract(testisolation.ProtocolSchema)
	if p.transitions != nil {
		contract(testisolation.TransitionPolicySchema)
	}
	var config struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(p.raw, &config) != nil {
		return runnerprotocol.Capabilities{}, invalid
	}
	contract(config.Schema)
	source := func(source sourcePlan) error {
		if source.capture != nil {
			capture, err := observeinterval.DecodeCapture(source.captureRaw)
			if err != nil {
				return invalid
			}
			contract(capture.Schema)
			add(runnerprotocol.CapabilityPin{Kind: "collector", ID: "live-mllp-capture", Version: engine.Version()})
			return nil
		}
		contract(source.source.Schema)
		kind := source.source.Observes.Kind
		version := engine.Version()
		if source.source.Database != nil {
			kind = source.source.Database.Driver
			var err error
			version, err = compiledDriverVersion(kind)
			if err != nil {
				return err
			}
		}
		add(runnerprotocol.CapabilityPin{Kind: "collector", ID: kind, Version: version})
		return nil
	}
	for _, phase := range d.Test.Phases {
		child := p.plan.Phase(phase.ID).Document()
		contract(child.Schema)
		contract(child.Test.Schema)
		contract(child.Test.OperatorVersion)
		contract(phase.Checks.Schema)
		if phase.Wire != nil {
			contract(phase.Wire.Set.Schema)
		}
		for _, dataset := range phase.Datasets {
			if dataset.Projection != nil {
				contract(dataset.Projection.Schema)
			}
			if dataset.Completion.Policy != nil {
				contract(dataset.Completion.Policy.Schema)
			}
		}
		if prepared := p.phases[phase.ID]; prepared != nil {
			for _, s := range prepared.sources {
				if err := source(s); err != nil {
					return runnerprotocol.Capabilities{}, err
				}
				if s.barrier != nil {
					if err := source(*s.barrier); err != nil {
						return runnerprotocol.Capabilities{}, err
					}
				}
			}
		}
		if p.fhir != nil {
			prepared := p.fhir.phases[phase.ID]
			for _, s := range prepared.sources {
				if err := source(s); err != nil {
					return runnerprotocol.Capabilities{}, err
				}
				if s.barrier != nil {
					if err := source(*s.barrier); err != nil {
						return runnerprotocol.Capabilities{}, err
					}
				}
			}
			if len(prepared.fhir) > 0 {
				contract(fhirobserve.Schema)
				add(runnerprotocol.CapabilityPin{Kind: "collector", ID: "fhir-resources", Version: engine.Version()})
			}
			for _, s := range prepared.fhir {
				if s.barrier != nil {
					if err := source(*s.barrier); err != nil {
						return runnerprotocol.Capabilities{}, err
					}
				}
			}
		}
	}
	profiles := []profileeval.ProfileV2{}
	packs := []profileeval.PackV2{}
	profileFiles := p.plan.Phase(d.Test.Phases[0].ID).Files()
	for _, ref := range d.Test.Profiles {
		contract(ref.Schema)
		if profileeval.AcceptsProfile(ref.Schema) {
			profile, err := profileeval.DecodeProfile(profileFiles["dependencies/"+ref.SHA256])
			if err != nil {
				return runnerprotocol.Capabilities{}, err
			}
			id := profile.Definition.Identity
			add(runnerprotocol.CapabilityPin{Kind: "profile", ID: "local/" + id.ID, Version: id.Version, SHA256: ref.SHA256})
			if len(profile.Definition.Terminology) > 0 {
				add(runnerprotocol.CapabilityPin{Kind: "terminology", ID: "local/" + id.ID, Version: id.Version, SHA256: ref.SHA256})
			}
			profiles = append(profiles, profile)
		} else if profileeval.AcceptsPack(ref.Schema) {
			pack, err := profileeval.DecodePack(profileFiles["dependencies/"+ref.SHA256])
			if err != nil {
				return runnerprotocol.Capabilities{}, err
			}
			id := pack.Metadata.Identity
			add(runnerprotocol.CapabilityPin{Kind: "profile", ID: "pack/" + id.ID, Version: id.Version, SHA256: ref.SHA256})
			if pack.Schema == profileeval.PackSchemaV5 {
				add(runnerprotocol.CapabilityPin{Kind: "terminology", ID: "pack/" + id.ID, Version: id.Version, SHA256: ref.SHA256})
			}
			packs = append(packs, pack)
		}
	}
	for _, profile := range profiles {
		for _, pack := range packs {
			if profile.Definition.Base.Pack != pack.Metadata.Identity {
				continue
			}
			operator := profileeval.OperatorVersion
			if profile.Schema == profileeval.ProfileSchemaV3 || pack.Schema == profileeval.PackSchemaV3 {
				operator = profileeval.ComponentOperatorVersion
			}
			if pack.Schema == profileeval.PackSchemaV4 {
				operator = profileeval.ChoiceOperatorVersion
			}
			if pack.Schema == profileeval.PackSchemaV5 {
				operator = profileeval.UsageOperatorVersion
			}
			contract(operator)
		}
	}
	if p.fhir != nil {
		contract(fhirrest.PlanSchema)
		contract(fhirrest.ResultSchema)
		contract(networkaction.RuntimeHTTPSchemaV2)
		modes, err := p.smartCapabilities()
		if err != nil {
			return runnerprotocol.Capabilities{}, err
		}
		for _, mode := range modes {
			add(mode)
		}
		if slices.ContainsFunc(modes, func(pin runnerprotocol.CapabilityPin) bool { return pin.ID != "none" }) {
			contract(smartbackend.ConfigSchema)
		}
	}
	if p.needsValidation() {
		contract(fhirvalidator.CapabilitySchema)
		contract(fhirvalidator.RequestSchema)
		contract(fhirvalidator.WorkerRequestSchema)
		contract(fhirvalidator.Policy)
		manifest := p.fhir.validation.Manifest()
		add(runnerprotocol.CapabilityPin{Kind: "validator", ID: "readmit-fhir-validator-capability", Version: "v1", SHA256: p.fhir.validation.Identity()})
		add(runnerprotocol.CapabilityPin{Kind: "validator", ID: "hl7-validator", Version: manifest.Validator.Version, SHA256: manifest.Validator.SHA256})
		add(runnerprotocol.CapabilityPin{Kind: "validator", ID: "java-runtime", Version: manifest.Runtime.Version, SHA256: manifest.Runtime.SHA256})
		add(runnerprotocol.CapabilityPin{Kind: "validator-worker", ID: "readmit-fhir-worker", Version: "v1", SHA256: strings.TrimPrefix(manifest.Image, "sha256:")})
		for _, pack := range manifest.Packages {
			kind := "profile"
			if strings.Contains(pack.ID, ".terminology.") {
				kind = "terminology"
			}
			add(runnerprotocol.CapabilityPin{Kind: kind, ID: pack.ID, Version: pack.Version, SHA256: pack.SHA256})
		}
		// The sealed inventory includes every profile/terminology canonical,
		// exact package closure, adapter, launcher and platform pin. Runtime
		// meta.profile values can select only members of that pinned inventory.
		add(runnerprotocol.CapabilityPin{Kind: "terminology", ID: "offline-validator-inventory", Version: "v1", SHA256: p.fhir.validation.Identity()})
	}
	for pin := range pins {
		capabilities.Pins = append(capabilities.Pins, pin)
	}
	slices.SortFunc(capabilities.Pins, func(a, b runnerprotocol.CapabilityPin) int {
		return strings.Compare(a.Kind+"/"+a.ID+"/"+a.Version+"/"+a.SHA256, b.Kind+"/"+b.ID+"/"+b.Version+"/"+b.SHA256)
	})
	if err := capabilities.Validate(); err != nil {
		return runnerprotocol.Capabilities{}, err
	}
	return capabilities, nil
}

// CapabilityStatus is a value-free admission refusal the runner can retain as
// actionable result metadata without including configuration or credentials.
type CapabilityStatus struct {
	State       string `json:"state"`
	Requirement string `json:"requirement"`
}

func (s CapabilityStatus) Error() string { return "connected runner " + s.State + "; " + s.Requirement }

func compiledDriverVersion(driver string) (string, error) {
	module := map[string]string{"postgresql": "github.com/jackc/pgx/v5", "sqlserver": "github.com/microsoft/go-mssqldb", "oracle": "github.com/sijms/go-ora/v2"}[driver]
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, dependency := range info.Deps {
			if dependency.Path != module {
				continue
			}
			if dependency.Replace != nil {
				dependency = dependency.Replace
			}
			if dependency.Version != "" && dependency.Version != "(devel)" {
				return dependency.Version, nil
			}
		}
	}
	return "", CapabilityStatus{State: "collector-unavailable", Requirement: "install a runner with exact compiled database-driver version metadata"}
}

func (p *PreparedFlow) smartCapabilities() ([]runnerprotocol.CapabilityPin, error) {
	var c FHIRFlowConfig
	if json.Unmarshal(p.raw, &c, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	root, err := filepath.Abs(filepath.Dir(p.configPath))
	if err != nil {
		return nil, err
	}
	read := func(path string) ([]byte, error) {
		return (artifactdir.Document{MaxBytes: 2 << 20}).Read(artifactpath.JoinReference(root, path))
	}
	pins := []runnerprotocol.CapabilityPin{}
	for id, selection := range c.Servers {
		for _, role := range []struct {
			selection FHIRAuthorization
			prepared  fhirAuth
		}{{selection.Observation, p.fhir.servers[id].observation}, {selection.Action, p.fhir.servers[id].action}} {
			if role.prepared.client == nil {
				pins = append(pins, runnerprotocol.CapabilityPin{Kind: "smart", ID: "none", Version: "v1"})
				continue
			}
			raw, err := read(role.selection.Client)
			if err != nil {
				return nil, err
			}
			var metadata []byte
			if role.selection.Metadata != "" {
				metadata, err = read(role.selection.Metadata)
				if err != nil {
					return nil, err
				}
			}
			client, err := smartbackend.Prepare(raw, p.fhir.policy, metadata)
			if err != nil || client.Identity() != role.prepared.client.Identity() {
				return nil, CapabilityStatus{State: "smart-configuration-changed", Requirement: "review the current selected SMART authorization configuration"}
			}
			var declared smartbackend.Config
			if json.Unmarshal(raw, &declared, json.RejectUnknownMembers(true)) != nil {
				return nil, invalid
			}
			pins = append(pins, runnerprotocol.CapabilityPin{Kind: "smart", ID: "backend-services/" + declared.Algorithm, Version: "2.2"})
		}
	}
	return pins, nil
}

func (p *PreparedFlow) needsValidation() bool {
	for _, phase := range p.plan.Document().Test.Phases {
		if len(phase.Validations) != 0 {
			return true
		}
	}
	return false
}

func (p *PreparedFlow) validationRequirements() error {
	if !p.needsValidation() {
		return nil
	}
	if p.fhir == nil || p.fhir.validation == nil || p.fhir.engine != "local" {
		return fhirvalidator.Status{State: "worker-missing", Requirement: "install and enable the selected local validator before running this suite"}
	}
	if err := p.fhir.validation.Check(); err != nil {
		return err
	}
	manifest := p.fhir.validation.Manifest()
	for _, phase := range p.plan.Document().Test.Phases {
		for _, check := range phase.Validations {
			for _, profile := range check.Profiles {
				if !slices.Contains(manifest.Profiles, profile) {
					return fhirvalidator.Status{State: "profile-unavailable", Requirement: "stage the exact requested validator profile revision before running this suite"}
				}
			}
		}
	}
	return nil
}

// CheckCapabilities performs the required local worker preflight under the
// caller's execution authority before setup. A test without validation needs
// no worker. This does not probe a target or upgrade its reviewed baseline to
// a claim about its current capability; the existing flow owns that preflight.
func (p *PreparedFlow) CheckCapabilities(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := p.Capabilities(); err != nil {
		return err
	}
	if !p.needsValidation() {
		return nil
	}
	engine, err := fhirvalidator.LocalEngine(p.fhir.socket)
	if err != nil {
		return err
	}
	defer engine.Close()
	status := engine.Check(ctx, p.fhir.validation)
	if status.State != "ready" {
		return status
	}
	return nil
}

// Resources identifies registered physical target and isolation lease domains,
// not this plan or occurrence. A second test using the same domain conflicts;
// tests with distinct targets and fixture leases remain independently runnable.
// Only digests leave this boundary; no address, name or namespace is returned.
func (p *PreparedFlow) Resources() []string {
	if p == nil {
		return nil
	}
	resources := map[string]bool{}
	add := func(kind, value string) {
		if value != "" {
			resources[networkaction.Digest(canonicalFlow(struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}{kind, value}))] = true
		}
	}
	origin := func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
	}
	var source func(sourcePlan)
	source = func(s sourcePlan) {
		switch {
		case s.capture != nil:
			if capture, err := observeinterval.DecodeCapture(s.captureRaw); err == nil {
				add("tcp", strings.ToLower(capture.Address))
			}
		case s.source.HTTP != nil:
			add("https", origin(s.source.HTTP.URL))
		case s.source.Database != nil:
			add("database", strings.ToLower(s.source.Database.Address)+"/"+s.source.Database.Name)
		case s.source.File != nil:
			add("export", filepath.Clean(s.source.File.Path))
		case s.source.Capture != nil:
			add("capture", filepath.Clean(s.source.Capture.Path))
		}
		if s.barrier != nil {
			source(*s.barrier)
		}
	}
	scope := p.isolation.Review("read").Scope
	add("isolation-lease", string(canonicalFlow(struct {
		Project     string `json:"project"`
		Environment string `json:"environment"`
		Lease       string `json:"lease"`
	}{scope.Project, scope.Environment, scope.LeaseKey})))
	for _, phase := range p.phases {
		target, _ := phase.transport.Preview()
		add("tcp", strings.ToLower(target.Address))
		for _, s := range phase.sources {
			source(s)
		}
	}
	if p.fhir != nil {
		for _, server := range p.fhir.servers {
			add("https", origin(server.base))
		}
		for _, phase := range p.fhir.phases {
			if phase.transport != nil {
				target, _ := phase.transport.Preview()
				add("tcp", strings.ToLower(target.Address))
			}
			for _, s := range phase.sources {
				source(s)
			}
			for _, s := range phase.fhir {
				if s.barrier != nil {
					source(*s.barrier)
				}
			}
		}
	}
	out := make([]string, 0, len(resources))
	for key := range resources {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func validationIdentity(p *PreparedFlow) string {
	if p.fhir != nil && p.fhir.validation != nil {
		return p.fhir.validation.Identity()
	}
	return ""
}

func sourceIdentities(p *PreparedFlow) map[string]string {
	out := map[string]string{}
	add := func(phase string, sources []sourcePlan) {
		for _, source := range sources {
			identity := source.source.Identity()
			if source.capture != nil {
				identity = networkaction.Digest(source.captureRaw)
			}
			out[phase+":dataset:"+source.definition.ID] = identity
			if source.barrier != nil {
				out[phase+":barrier:"+source.definition.ID] = source.barrier.source.Identity()
			}
		}
	}
	for id, phase := range p.phases {
		add(id, phase.sources)
	}
	if p.fhir != nil {
		for id, phase := range p.fhir.phases {
			add(id, phase.sources)
			for _, source := range phase.fhir {
				if source.barrier != nil {
					out[id+":barrier:"+source.definition.ID] = source.barrier.source.Identity()
				}
			}
		}
	}
	return out
}
