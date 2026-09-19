// Package runnerprotocol defines customer-hub admission, separate from evidence.
package runnerprotocol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid runner contract")
var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func ID(s string) bool { return identifier.MatchString(s) }

// Request carries approved metadata only, never spec paths or evidence.
type Request struct {
	Schema      string `json:"schema"`
	Environment string `json:"environment"`
	Instance    string `json:"instance"`
	Job         string `json:"job"`
	Engine      string `json:"engine"`
	Spec        string `json:"spec"`
	Profile     string `json:"profile"`
}
type Lease struct {
	Schema     string    `json:"schema"`
	Expires    time.Time `json:"expires_at"`
	MaxSeconds int       `json:"max_seconds"`
	MaxJobs    int       `json:"max_jobs"`
}
type Policy struct {
	Schema  string  `json:"schema"`
	Runners []Grant `json:"runners"`
}
type Grant struct {
	Project     string `json:"project"`
	Subject     string `json:"subject"`
	Environment string `json:"environment"`
	Engine      string `json:"engine"`
	Spec        string `json:"spec"`
	Profile     string `json:"profile"`
	MaxSeconds  int    `json:"max_seconds"`
	MaxJobs     int    `json:"max_jobs"`
}

// Exact rejects absent and null fields before the typed strict read.
func Exact(data []byte, names ...string) error {
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil || len(members) != len(names) {
		return ErrInvalid
	}
	for _, name := range names {
		if len(members[name]) == 0 || string(members[name]) == "null" {
			return ErrInvalid
		}
	}
	return nil
}
func DecodeRequest(data []byte) (Request, error) {
	var v Request
	if len(data) > 4096 || Exact(data, "schema", "environment", "engine", "spec", "profile", "instance", "job") != nil || json.Unmarshal(data, &v, json.RejectUnknownMembers(true)) != nil || v.Schema != "readmit-runner-request/v1" || !ID(v.Environment) || !ID(v.Instance) || !ID(v.Job) || v.Engine == "" || len(v.Engine) > 64 || v.Spec != "readmit-test/v1" || v.Profile != "readmit-siu-v1" {
		return v, ErrInvalid
	}
	return v, nil
}
func DecodeLease(data []byte) (Lease, error) {
	var v Lease
	if len(data) > 4096 || Exact(data, "schema", "expires_at", "max_seconds", "max_jobs") != nil || json.Unmarshal(data, &v, json.RejectUnknownMembers(true)) != nil || v.Schema != "readmit-runner-lease/v1" || v.Expires.IsZero() || v.MaxSeconds < 1 || v.MaxSeconds > 3600 || v.MaxJobs < 1 || v.MaxJobs > 10000 {
		return v, ErrInvalid
	}
	return v, nil
}
func DecodePolicy(data []byte) (Policy, error) {
	var v Policy
	if len(data) > 1<<20 || Exact(data, "schema", "runners") != nil || json.Unmarshal(data, &v, json.RejectUnknownMembers(true)) != nil || v.Schema != "readmit-runner-policy/v1" || len(v.Runners) > 4096 {
		return v, ErrInvalid
	}
	var raw struct {
		Runners []jsontext.Value `json:"runners"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return v, ErrInvalid
	}
	seen := map[string]bool{}
	for i, g := range v.Runners {
		key := g.Project + "\x00" + g.Environment
		if Exact(raw.Runners[i], "project", "subject", "environment", "engine", "spec", "profile", "max_seconds", "max_jobs") != nil || !ID(g.Project) || g.Subject == "" || len(g.Subject) > 256 || seen[key] || g.MaxSeconds < 1 || g.MaxSeconds > 3600 || g.MaxJobs < 1 || g.MaxJobs > 10000 {
			return v, ErrInvalid
		}
		b, _ := json.Marshal(Request{"readmit-runner-request/v1", g.Environment, "policy-validation", "policy-job", g.Engine, g.Spec, g.Profile})
		if _, err := DecodeRequest(b); err != nil {
			return v, err
		}
		seen[key] = true
	}
	return v, nil
}
