// Package connectedrun owns the typed v2 stimulus/observation execution path.
// It composes existing sender, collectors and evaluator without subprocesses.
package connectedrun

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ConfigSchema = "readmit-connected-run-config/v1"
const Schema = "readmit-connected-run/v1"

var invalid = errors.New("connected execution configuration or retained evidence is invalid")

type Grant struct {
	Path       string `json:"path"`
	Actor      string `json:"actor"`
	Generation string `json:"generation"`
}
type SourceSelection struct {
	Path                 string `json:"path"`
	Grant                *Grant `json:"grant,omitzero"`
	CredentialGeneration string `json:"credential_generation,omitzero"`
}
type Config struct {
	Schema     string                     `json:"schema"`
	Case       string                     `json:"case"`
	Target     string                     `json:"target"`
	Policy     string                     `json:"policy"`
	Credential string                     `json:"credential,omitzero"`
	Send       Grant                      `json:"send"`
	Sources    map[string]SourceSelection `json:"sources"`
}
type sourcePlan struct {
	definition connectedtest.Dataset
	source     observesource.Source
	projection dataset.Projection
	http       *networkaction.HTTPPlan
	database   *observesource.DatabaseAction
	grant      Grant
}
type Prepared struct {
	plan                 *connectedtest.Plan
	transport            *connectedtransport.Prepared
	send                 Grant
	sources              []sourcePlan
	planPath, configPath string
	configRaw            []byte
}

func Prepare(planPath, configPath string) (*Prepared, error) {
	plan, err := connectedtest.OpenPlan(planPath)
	if err != nil || plan.Document().Schema != connectedtest.PlanSchemaV2 {
		return nil, invalid
	}
	raw, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(configPath)
	if err != nil {
		return nil, invalid
	}
	var config Config
	if json.Unmarshal(raw, &config, json.RejectUnknownMembers(true)) != nil || config.Schema != ConfigSchema {
		return nil, invalid
	}
	root, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return nil, invalid
	}
	anchor := func(s string) string {
		if s == "" {
			return ""
		}
		return artifactpath.JoinReference(root, s)
	}
	config.Send.Path = anchor(config.Send.Path)
	transport, err := connectedtransport.Prepare(plan, connectedtransport.Selection{Case: anchor(config.Case), Target: anchor(config.Target), Policy: anchor(config.Policy), Credential: anchor(config.Credential)})
	if err != nil {
		return nil, err
	}
	policy, err := (artifactdir.Document{MaxBytes: sendpolicy.MaxPolicyBytes}).Read(anchor(config.Policy))
	if err != nil {
		return nil, invalid
	}
	p := &Prepared{plan: plan, transport: transport, send: config.Send, planPath: planPath, configPath: configPath, configRaw: bytes.Clone(raw)}
	doc := plan.Document()
	if len(config.Sources) != len(doc.Test.Datasets) {
		return nil, invalid
	}
	files := plan.Files()
	// This executor deliberately accepts final-state before/after datasets only.
	// Unsupported setup, cleanup and protocol schedules are refused by preparation.
	for _, d := range doc.Test.Datasets {
		selected, ok := config.Sources[d.ID]
		if !ok || d.Kind != "typed-rows" || d.Projection == nil || d.Completion.Kind != "bounded-horizon" {
			return nil, invalid
		}
		source, err := observesource.ReadSource(anchor(selected.Path))
		if err != nil || source.Identity() != d.Source || !source.Enabled {
			return nil, invalid
		}
		projection, err := dataset.DecodeProjection(files["dependencies/"+d.Projection.SHA256])
		if err != nil {
			return nil, invalid
		}
		item := sourcePlan{definition: d, source: source, projection: projection}
		if source.HTTP != nil || source.Database != nil {
			if selected.Grant == nil || selected.CredentialGeneration == "" {
				return nil, invalid
			}
			item.grant = *selected.Grant
			item.grant.Path = anchor(item.grant.Path)
			if source.HTTP != nil {
				u, err := url.Parse(source.HTTP.URL)
				if err != nil {
					return nil, invalid
				}
				name := source.HTTP.ServerName
				if name == "" {
					name = u.Hostname()
				}
				var ca []byte
				if source.HTTP.CAFile != "" {
					ca, err = (artifactdir.Document{MaxBytes: 1 << 20}).Read(source.HTTP.CAFile)
					if err != nil {
						return nil, invalid
					}
				}
				spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: plan.Identity(), Source: source.Identity(), Project: doc.Environment.Project, Environment: doc.Environment.ID, Revision: doc.Environment.Revision, Endpoint: d.ID, Classification: source.HTTP.Classification, Operation: sendpolicy.ObservationRead, Method: "GET", URL: source.HTTP.URL, ServerName: name, Authorities: ca, TimeoutMS: projection.Limits.TimeoutMS, MaxBytes: projection.Limits.MaxBytes}
				if c := source.HTTP.Credential; c != nil {
					spec.Credential = &networkaction.Credential{Endpoint: c.Address, Purpose: sendpolicy.ObservationRead, Generation: selected.CredentialGeneration, Header: c.Header, Locator: networkaction.Provider{Command: c.Command, Arguments: c.Arguments}}
				}
				b, _ := json.Marshal(spec, json.Deterministic(true))
				item.http, err = networkaction.PrepareHTTP(b, policy)
				if err != nil {
					return nil, err
				}
			} else {
				item.database, err = observesource.PrepareDatabaseAction(source, projection, observesource.DatabaseActionContext{Plan: plan.Identity(), Project: doc.Environment.Project, Environment: doc.Environment.ID, Revision: doc.Environment.Revision, Endpoint: d.ID, CredentialGeneration: selected.CredentialGeneration}, policy)
				if err != nil {
					return nil, err
				}
			}
		} else if selected.Grant != nil || selected.CredentialGeneration != "" {
			return nil, invalid
		}
		p.sources = append(p.sources, item)
	}
	return p, nil
}

// Bindings are machine configuration for separately provisioned runner grants;
// preparing them resolves no secret and contacts no source.
func (p *Prepared) Bindings() map[string]networkaction.Binding {
	out := map[string]networkaction.Binding{"stimulus": p.transport.Binding()}
	for _, s := range p.sources {
		if s.http != nil {
			out["dataset:"+s.definition.ID] = s.http.Binding()
		}
		if s.database != nil {
			out["dataset:"+s.definition.ID] = s.database.Binding()
		}
	}
	return out
}
func (p *Prepared) unchanged() error {
	raw, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(p.configPath)
	if err != nil || !bytes.Equal(raw, p.configRaw) {
		return invalid
	}
	fresh, err := Prepare(p.planPath, p.configPath)
	if err != nil {
		return invalid
	}
	a, _ := json.Marshal(p.Bindings(), json.Deterministic(true))
	b, _ := json.Marshal(fresh.Bindings(), json.Deterministic(true))
	if !bytes.Equal(a, b) || fresh.plan.Identity() != p.plan.Identity() {
		return invalid
	}
	for i, s := range p.sources {
		if s.source.Identity() != fresh.sources[i].source.Identity() {
			return invalid
		}
	}
	return nil
}
func (g Grant) authority() networkaction.FileAuthority {
	return networkaction.FileAuthority{Path: g.Path, Actor: g.Actor, Generation: g.Generation}
}
func duration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }
func safeID(s string) bool {
	if s == "" || len(s) > 64 || strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		return false
	}
	return s[0] >= 'a' && s[0] <= 'z'
}
