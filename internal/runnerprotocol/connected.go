package runnerprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"regexp"
	"slices"
	"time"
)

const (
	CapabilitiesSchema     = "readmit-runner-capabilities/v1"
	ConnectedRequestSchema = "readmit-runner-request/v2"
	ConnectedLeaseSchema   = "readmit-runner-lease/v2"
	ConnectedPolicySchema  = "readmit-runner-policy/v2"
	ConnectedCommandSchema = "readmit-runner-command/v1"
	MaxConnectedBytes      = 128 << 10
)

var capabilityName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
var capabilityVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/+\-]{0,127}$`)

// CapabilityPin names the implementation or exact installed dependency used by
// the connected engine. Contract, collector and SMART implementations can be
// version-only; packs, validators and worker images always bind exact bytes.
type CapabilityPin struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// Capabilities is an exact agreement, not a claim that a target or local worker
// is ready. The customer runner separately preflights every required facility.
type Capabilities struct {
	Schema string          `json:"schema"`
	Engine string          `json:"engine"`
	Pins   []CapabilityPin `json:"pins"`
}

func (c Capabilities) Validate() error {
	if c.Schema != CapabilitiesSchema || !capabilityVersion.MatchString(c.Engine) || len(c.Engine) > 64 || c.Pins == nil || len(c.Pins) > 128 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, p := range c.Pins {
		versionOnly := false
		switch p.Kind {
		case "contract", "collector", "smart":
			versionOnly = true
		case "profile", "terminology", "validator", "validator-worker":
		default:
			return ErrInvalid
		}
		key := p.Kind + "\x00" + p.ID + "\x00" + p.Version
		if !capabilityName.MatchString(p.ID) || !capabilityVersion.MatchString(p.Version) || seen[key] || !(validDigest(p.SHA256) || versionOnly && p.SHA256 == "") {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}

func DecodeCapabilities(data []byte) (Capabilities, error) {
	var c Capabilities
	if len(data) > MaxConnectedBytes || Exact(data, "schema", "engine", "pins") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil || c.Validate() != nil {
		return c, ErrInvalid
	}
	var raw struct {
		Pins []jsontext.Value `json:"pins"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return c, ErrInvalid
	}
	for _, p := range raw.Pins {
		if Exact(p, "kind", "id", "version", "sha256") != nil {
			return c, ErrInvalid
		}
	}
	return c, nil
}

func (c Capabilities) canonical() Capabilities {
	c.Pins = slices.Clone(c.Pins)
	slices.SortFunc(c.Pins, func(a, b CapabilityPin) int {
		if a.Kind != b.Kind {
			return compareNames(a.Kind, b.Kind)
		}
		if a.ID != b.ID {
			return compareNames(a.ID, b.ID)
		}
		if a.Version != b.Version {
			return compareNames(a.Version, b.Version)
		}
		return compareNames(a.SHA256, b.SHA256)
	})
	return c
}

func compareNames(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func contractIdentity(v any) (string, error) {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return "", ErrInvalid
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (c Capabilities) Identity() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return contractIdentity(c.canonical())
}

// ConnectedRequest contains only approved metadata and value-free resource
// digests. Job identifies one occurrence forever; another instance cannot
// recycle it. No target value, credential or evidence travels to the hub.
type ConnectedRequest struct {
	Schema       string       `json:"schema"`
	Environment  string       `json:"environment"`
	Instance     string       `json:"instance"`
	Job          string       `json:"job"`
	Engine       string       `json:"engine"`
	Capabilities Capabilities `json:"capabilities"`
	Resources    []string     `json:"resources"`
	Input        string       `json:"input"`
}

func (r ConnectedRequest) Validate() error {
	if r.Schema != ConnectedRequestSchema || !ID(r.Environment) || !ID(r.Instance) || !ID(r.Job) || r.Capabilities.Validate() != nil || r.Engine != r.Capabilities.Engine || !validDigest(r.Input) || r.Resources == nil || len(r.Resources) > 128 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, key := range r.Resources {
		if !validDigest(key) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	return nil
}

func DecodeConnectedRequest(data []byte) (ConnectedRequest, error) {
	var r ConnectedRequest
	if len(data) > MaxConnectedBytes || Exact(data, "schema", "environment", "instance", "job", "engine", "capabilities", "resources", "input") != nil || json.Unmarshal(data, &r, json.RejectUnknownMembers(true)) != nil || r.Validate() != nil {
		return r, ErrInvalid
	}
	var raw struct {
		Capabilities jsontext.Value `json:"capabilities"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return r, ErrInvalid
	}
	if _, err := DecodeCapabilities(raw.Capabilities); err != nil {
		return r, err
	}
	return r, nil
}

func (r ConnectedRequest) Identity() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	r.Capabilities = r.Capabilities.canonical()
	r.Resources = slices.Clone(r.Resources)
	slices.Sort(r.Resources)
	return contractIdentity(r)
}

type ConnectedLease struct {
	Schema     string    `json:"schema"`
	Expires    time.Time `json:"expires_at"`
	MaxSeconds int       `json:"max_seconds"`
	MaxJobs    int       `json:"max_jobs"`
	Generation uint64    `json:"generation"`
}

func DecodeConnectedLease(data []byte) (ConnectedLease, error) {
	var l ConnectedLease
	if len(data) > 4096 || Exact(data, "schema", "expires_at", "max_seconds", "max_jobs", "generation") != nil || json.Unmarshal(data, &l, json.RejectUnknownMembers(true)) != nil || l.Schema != ConnectedLeaseSchema || l.Expires.IsZero() || l.MaxSeconds < 1 || l.MaxSeconds > 3600 || l.MaxJobs < 1 || l.MaxJobs > 10000 || l.Generation < 1 {
		return l, ErrInvalid
	}
	return l, nil
}

// ConnectedPolicy is explicitly selected beside the frozen v1 policy; no v1
// reader silently accepts this different authority contract.
type ConnectedPolicy struct {
	Schema  string           `json:"schema"`
	Runners []ConnectedGrant `json:"runners"`
}

type ConnectedGrant struct {
	Project     string `json:"project"`
	Subject     string `json:"subject"`
	Environment string `json:"environment"`
	Engine      string `json:"engine"`
	Capability  string `json:"capability"`
	MaxSeconds  int    `json:"max_seconds"`
	MaxJobs     int    `json:"max_jobs"`
}

func DecodeConnectedPolicy(data []byte) (ConnectedPolicy, error) {
	var p ConnectedPolicy
	if len(data) > 1<<20 || Exact(data, "schema", "runners") != nil || json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != ConnectedPolicySchema || p.Runners == nil || len(p.Runners) > 4096 {
		return p, ErrInvalid
	}
	var raw struct {
		Runners []jsontext.Value `json:"runners"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return p, ErrInvalid
	}
	seen := map[string]bool{}
	for i, g := range p.Runners {
		key := g.Project + "\x00" + g.Environment + "\x00" + g.Subject + "\x00" + g.Capability
		if Exact(raw.Runners[i], "project", "subject", "environment", "engine", "capability", "max_seconds", "max_jobs") != nil || !ID(g.Project) || !ID(g.Environment) || g.Subject == "" || len(g.Subject) > 256 || !capabilityVersion.MatchString(g.Engine) || len(g.Engine) > 64 || !validDigest(g.Capability) || g.MaxSeconds < 1 || g.MaxSeconds > 3600 || g.MaxJobs < 1 || g.MaxJobs > 10000 || seen[key] {
			return p, ErrInvalid
		}
		seen[key] = true
	}
	return p, nil
}

// ConnectedCommand carries the exact admitted request and fence on every
// renewal, attempted effect, and terminal settlement. Empty Step and Outcome
// mean renewal; one Step means an effect; one Outcome means settlement.
type ConnectedCommand struct {
	Schema     string           `json:"schema"`
	Request    ConnectedRequest `json:"request"`
	Generation uint64           `json:"generation"`
	Step       string           `json:"step"`
	Outcome    string           `json:"outcome"`
}

func DecodeConnectedCommand(data []byte) (ConnectedCommand, error) {
	var c ConnectedCommand
	if len(data) > MaxConnectedBytes || Exact(data, "schema", "request", "generation", "step", "outcome") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != ConnectedCommandSchema || c.Generation < 1 || c.Request.Validate() != nil || c.Step != "" && (!ID(c.Step) || c.Outcome != "") || c.Outcome != "" && c.Outcome != "settled" && c.Outcome != "uncertain" {
		return c, ErrInvalid
	}
	var raw struct {
		Request jsontext.Value `json:"request"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return c, ErrInvalid
	}
	if _, err := DecodeConnectedRequest(raw.Request); err != nil {
		return c, err
	}
	return c, nil
}
