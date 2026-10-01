package observesource

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const DatabaseActionSchema = "readmit-network-database-action/v1"

type DatabaseActionContext struct{ Plan, Project, Environment, Revision, Endpoint, CredentialGeneration string }
type databaseActionDocument struct {
	Schema      string                `json:"schema"`
	Context     DatabaseActionContext `json:"context"`
	Source      Source                `json:"source"`
	Projection  dataset.Projection    `json:"projection"`
	Authorities []byte                `json:"authorities"`
}
type DatabaseAction struct {
	document       databaseActionDocument
	policy         sendpolicy.ScopedPolicy
	binding        networkaction.Binding
	raw, policyRaw []byte
}

func PrepareDatabaseAction(source Source, projection dataset.Projection, scope DatabaseActionContext, policyRaw []byte) (*DatabaseAction, error) {
	if source.Validate() != nil || source.Database == nil || projection.Validate() != nil || projection.Format != "database" || !runnerprotocol.ID(scope.Endpoint) || !runnerprotocol.ID(scope.CredentialGeneration) || !networkaction.ValidDigest(scope.Plan) {
		return nil, errors.New("invalid scoped database action")
	}
	policy, err := sendpolicy.DecodeScopedPolicy(policyRaw)
	if err != nil || policy.Project != scope.Project || policy.Environment != scope.Environment || policy.Revision != scope.Revision {
		return nil, errors.New("database policy differs")
	}
	ca, err := authorities(source.Database.CAFile)
	if err != nil {
		return nil, err
	}
	return databaseActionFromDocument(databaseActionDocument{Schema: DatabaseActionSchema, Context: scope, Source: source, Projection: projection, Authorities: ca}, policyRaw)
}
func databaseActionFromDocument(d databaseActionDocument, policyRaw []byte) (*DatabaseAction, error) {
	policy, err := sendpolicy.DecodeScopedPolicy(policyRaw)
	if err != nil || d.Schema != DatabaseActionSchema || d.Source.Validate() != nil || d.Source.Database == nil || d.Projection.Validate() != nil || d.Projection.Format != "database" || d.Context.Project != policy.Project || d.Context.Environment != policy.Environment || d.Context.Revision != policy.Revision || !runnerprotocol.ID(d.Context.CredentialGeneration) || !runnerprotocol.ID(d.Context.Endpoint) || !networkaction.ValidDigest(d.Context.Plan) {
		return nil, errors.New("invalid scoped database action")
	}
	raw, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	var owned databaseActionDocument
	if json.Unmarshal(raw, &owned, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("invalid database snapshot")
	}
	credential, _ := json.Marshal(struct {
		Credential DatabaseCredential `json:"credential"`
		Generation string             `json:"generation"`
	}{d.Source.Database.Credential, d.Context.CredentialGeneration}, json.Deterministic(true))
	b := networkaction.Binding{Plan: d.Context.Plan, Source: d.Source.Identity(), Project: d.Context.Project, Environment: d.Context.Environment, Revision: d.Context.Revision, Endpoint: d.Context.Endpoint, Operation: sendpolicy.ObservationRead, Configuration: networkaction.Digest(raw), Policy: networkaction.Digest(policyRaw), Credentials: networkaction.Digest(credential)}
	return &DatabaseAction{document: owned, policy: policy, binding: b, raw: raw, policyRaw: bytes.Clone(policyRaw)}, nil
}
func (p *DatabaseAction) Binding() networkaction.Binding { return p.binding }

var dbActionFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "scoped database action", RequiredFiles: []string{"action.json", "policy.json", "decision.json", "operation.json", "result.json", "identity.sha256"}, AllowFile: func(n string) bool {
	switch n {
	case "action.json", "policy.json", "decision.json", "operation.json", "result.json", "identity.sha256":
		return true
	}
	return false
}, MaxFiles: 6, MaxFileBytes: 2 << 20, MaxBytes: 4 << 20}, Seal: artifactdir.DirectoryHash(networkaction.ResultSchema)}

func databaseScopedReader(ctx context.Context, p *DatabaseAction, a networkaction.Authority, output string, resolve sendpolicy.Resolver) (reader, error) {
	refused := errors.New("scoped database action refused")
	if p == nil || a == nil {
		return nil, refused
	}
	actor, err := a.Check(ctx, p.binding)
	if err != nil || !networkaction.CurrentActor(actor) {
		return nil, refused
	}
	check := func(ctx context.Context) error {
		current, err := a.Check(ctx, p.binding)
		if err != nil || current != actor || !networkaction.CurrentActor(current) || ctx.Err() != nil {
			return refused
		}
		return nil
	}
	w, err := artifactdir.Create(output, dbActionFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	closeWriter := sync.OnceFunc(w.Close)
	failed := true
	defer func() {
		if failed {
			closeWriter()
		}
	}()
	if w.WriteFile("action.json", p.raw) != nil || w.WriteFile("policy.json", p.policyRaw) != nil {
		return nil, refused
	}
	d := *p.document.Source.Database
	timeout, _ := time.ParseDuration(d.limits().Timeout)
	route, err := destination.AdmitScoped(ctx, destination.ScopedRequest{Policy: p.policy, Request: sendpolicy.ScopedRequest{Project: p.binding.Project, Environment: p.binding.Environment, Endpoint: p.binding.Endpoint, Operation: sendpolicy.ObservationRead, Address: d.Address, Classification: d.Classification}, Budget: timeout, Resolve: resolve, Authorize: check, Record: func(decision sendpolicy.ScopedDecision) error {
		raw, _ := json.Marshal(decision, json.Deterministic(true))
		if w.WriteFile("decision.json", raw) != nil {
			return refused
		}
		raw, _ = json.Marshal(decision.Redacted(), json.Deterministic(true))
		if w.WriteFile("operation.json", raw) != nil {
			return refused
		}
		return w.Sync()
	}})
	if err != nil {
		return nil, refused
	}
	config, err := route.ClientConfig(destination.Security{ServerName: d.ServerName, Authorities: p.document.Authorities})
	if err != nil {
		return nil, refused
	}
	maxAge, _ := time.ParseDuration(p.document.Source.Freshness.MaxAge)
	r := &databaseReader{declaration: d, route: route, tls: config, maxAge: maxAge, check: check, cleanup: closeWriter}
	r.complete = func(taken attempt) error {
		defer closeWriter()
		result := networkaction.Result{Schema: networkaction.ResultSchema, Binding: p.binding, Actor: actor, State: "incomplete"}
		if taken.status == observewindow.Observed {
			result.State = "responded"
			result.ResponseRetained = true
			result.ResponseDigest = networkaction.Digest(taken.evidence["dataset-database.json"])
		}
		raw, _ := json.Marshal(result, json.Deterministic(true))
		if w.WriteFile("result.json", raw) != nil {
			return refused
		}
		_, err := w.Seal(nil)
		return err
	}
	failed = false
	return r, nil
}
func OpenDatabaseAction(directory string) (networkaction.Result, error) {
	files, err := artifactdir.Read(directory, dbActionFamily.Layout)
	if err != nil {
		return networkaction.Result{}, err
	}
	return VerifyDatabaseAction(files)
}

// VerifyDatabaseAction verifies exactly the caller's retained byte snapshot.
// It neither rereads a path nor opens a database/credential provider.
func VerifyDatabaseAction(files map[string][]byte) (networkaction.Result, error) {
	total := 0
	if len(files) > dbActionFamily.Layout.MaxFiles {
		return networkaction.Result{}, errors.New("database action exceeds bounds")
	}
	for name, raw := range files {
		if !dbActionFamily.Layout.AllowFile(name) || len(raw) > dbActionFamily.Layout.MaxFileBytes {
			return networkaction.Result{}, errors.New("invalid database action member")
		}
		total += len(raw)
	}
	if total > dbActionFamily.Layout.MaxBytes {
		return networkaction.Result{}, errors.New("database action exceeds bounds")
	}
	for _, name := range dbActionFamily.Layout.RequiredFiles {
		if _, ok := files[name]; !ok {
			return networkaction.Result{}, errors.New("missing database action member")
		}
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(networkaction.ResultSchema, files) {
		return networkaction.Result{}, errors.New("database action identity mismatch")
	}
	var doc databaseActionDocument
	var result networkaction.Result
	var decision sendpolicy.ScopedDecision
	if json.Unmarshal(files["action.json"], &doc, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["result.json"], &result, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["decision.json"], &decision, json.RejectUnknownMembers(true)) != nil {
		return result, errors.New("invalid database action")
	}
	p, err := databaseActionFromDocument(doc, files["policy.json"])
	if err != nil || p.binding != result.Binding {
		return result, errors.New("database binding differs")
	}
	if !networkaction.VerifyDecision(p.policy, sendpolicy.ScopedRequest{Project: p.binding.Project, Environment: p.binding.Environment, Endpoint: p.binding.Endpoint, Operation: p.binding.Operation, Address: doc.Source.Database.Address, Classification: doc.Source.Database.Classification}, decision) || !decision.Allowed {
		return result, errors.New("database decision differs")
	}
	expected, _ := json.Marshal(decision.Redacted(), json.Deterministic(true))
	if !bytes.Equal(expected, files["operation.json"]) {
		return result, errors.New("database operation differs")
	}
	if result.Schema != networkaction.ResultSchema || !networkaction.RecordedActor(result.Actor) || result.HTTPStatus != 0 || result.State != "responded" && result.State != "incomplete" || result.State == "responded" && (!result.ResponseRetained || !networkaction.ValidDigest(result.ResponseDigest)) || result.State == "incomplete" && (result.ResponseRetained || result.ResponseDigest != "") {
		return result, errors.New("invalid database outcome")
	}
	return result, nil
}
func databaseActionMatches(r DatasetRequest) bool {
	return r.DatabaseNetwork != nil && r.Network == nil && r.Policy == nil && r.NetworkAuthority != nil && r.Source.Identity() == r.DatabaseNetwork.binding.Source && r.Projection.Identity() == r.DatabaseNetwork.document.Projection.Identity()
}
func networkResultFor(path string, kind string) (networkaction.Result, error) {
	if kind == "database" {
		return OpenDatabaseAction(filepath.Join(path, "network"))
	}
	return networkaction.OpenHTTP(filepath.Join(path, "network"))
}

func networkResultFromFiles(files map[string][]byte, kind string) (networkaction.Result, error) {
	if kind == "database" {
		return VerifyDatabaseAction(artifactdir.Subtree(files, "network"))
	}
	return networkaction.VerifyHTTP(artifactdir.Subtree(files, "network"))
}
