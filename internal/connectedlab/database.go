package connectedlab

import (
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// DatabaseDataset authors a typed-rows observation of a PostgreSQL database
// named "application" at address, serving TLS for example.com under the PEM
// authority ca, through the real driver and collector: its "observed" view's
// columns named, text typed, key the first, filtered to status = filter, over
// a full horizon, read as the least-privilege role "observer". It writes the
// source definition and selects it with a separately provisioned grant, as an
// operator does.
func (h *Harness) DatabaseDataset(phase, id, when, address string, ca []byte, filter string, columns ...string) connectedtest.Dataset {
	h.T.Helper()
	caPath := filepath.Join(h.Root, id+"-database-ca.pem")
	if err := os.WriteFile(caPath, ca, 0o600); err != nil {
		h.T.Fatal(err)
	}
	provider := filepath.Join(h.Root, id+"-database-credential.sh")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'lab-observer-secret'\n"), 0o700); err != nil {
		h.T.Fatal(err)
	}
	source := observesource.Source{Schema: observesource.SchemaDatabase, Observes: observewindow.Source{Kind: observesource.DatabaseQuery, Identity: "application-db-" + id, Scope: id}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "10s"},
		Database: &observesource.Database{Driver: "postgresql", Address: address, Classification: "nonproduction", Name: "application", Username: "observer", CAFile: caPath, ServerName: "example.com",
			Credential: observesource.DatabaseCredential{Store: "customer-managed", Address: address, Purpose: "database-observation", Command: provider, Arguments: []string{}},
			View:       []string{"public", "observed"}, RecordKey: columns[0], KeyType: "text", Filters: []observesource.DatabaseFilter{{Column: "status", Value: filter}}, Limits: &observesource.DatabaseLimits{Timeout: "5s", MaxRows: 20, MaxBytes: 65536}}}
	sourcePath := id + "-source.json"
	if err := observesource.WriteSource(filepath.Join(h.Root, sourcePath), source); err != nil {
		h.T.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: id, Format: "database", Order: "unordered", Columns: []dataset.Column{}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 5000}}
	for i, c := range columns {
		projection.Columns = append(projection.Columns, dataset.Column{Name: c, Type: "text", Locator: importer.Locator{c}, Key: i == 0, Required: true})
	}
	h.projections[source.Identity()] = projection.Identity()
	name := phase + "-" + id + "-" + when
	ref := h.RefJSON(name+"-projection", dataset.ProjectionSchema, name+"-projection.json", projection)
	interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: source.Identity(), Namespace: id, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: h.HorizonMS, SampleMS: 25, MaxGapMS: 30000, MaxSamples: 400, MaxRecords: 20, MaxBytes: 65536}
	window := h.RefJSON(name+"-window", observeinterval.Schema, name+"-window.json", interval)
	h.allow(id, address, sendpolicy.ObservationRead)
	h.Sources[id] = connectedrun.SourceSelection{Path: sourcePath, Grant: &connectedrun.Grant{Path: GrantFile(phase + ":dataset:" + id), Actor: "runner", Generation: "1"}, CredentialGeneration: "1"}
	return connectedtest.Dataset{ID: id, Kind: "typed-rows", Namespace: id, Phase: when, Source: source.Identity(), Projection: &ref, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: h.HorizonMS, MaxRecords: 20, MaxBytes: 65536, Policy: &window}}
}
