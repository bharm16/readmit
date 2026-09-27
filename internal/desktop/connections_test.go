package desktop_test

// The connection inventory lists the external access actually configured and
// what this window is reaching now, from saved configuration and runtime state
// alone. These tests hold it to that: listing looks up no name, opens no
// connection and runs no declared program; a check that ran is Checked at its
// time and never Connected; and an operation reaching outside stays listed
// even when another window removes its configuration while it runs.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// connectionsProject is a named project of an activated window whose file
// dialog answers with the commercial destinations file given.
func connectionsProject(t *testing.T, files ...string) (*desktop.App, desktop.RequestContext) {
	t.Helper()
	app := activatedApp(t, &chooser{folder: t.TempDir(), files: files}, t.TempDir())
	app.ChooseProjectLocation()
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	if created.State != desktop.Completed {
		t.Fatalf("create: %+v", created)
	}
	return app, created.Context
}

func connectionRows(t *testing.T, app *desktop.App, context desktop.RequestContext) map[string]desktop.ConnectionRow {
	t.Helper()
	listed := app.ListConnections(context)
	if listed.State != desktop.Completed || listed.ProjectReason != "" {
		t.Fatalf("list connections: %+v", listed)
	}
	rows := map[string]desktop.ConnectionRow{}
	for _, row := range listed.Rows {
		rows[row.Ref] = row
	}
	return rows
}

// countLookups replaces the system resolver for the test with one that
// counts every name lookup and answers none.
func countLookups(t *testing.T) *atomic.Int64 {
	t.Helper()
	var lookups atomic.Int64
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		lookups.Add(1)
		return nil, errors.New("lookup refused")
	}}
	t.Cleanup(func() { net.DefaultResolver = previous })
	return &lookups
}

func TestListConnectionsReadsSavedConfigurationAndRuntimeStateWithoutContactingAnything(t *testing.T) {
	lookups := countLookups(t)
	destinations := filepath.Join(t.TempDir(), "destinations.json")
	writeDocument(t, filepath.Dir(destinations), filepath.Base(destinations),
		`{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test"}`)
	app, context := connectionsProject(t, destinations)

	// Every configured destination would be seen if it were reached: the
	// environment's and the hub's listeners count connections, a name would
	// be looked up through the counting resolver, and the hub's key command
	// leaves a marker when it runs.
	environment := newCountingEndpoint(t, false)
	hub := newCountingEndpoint(t, false)
	local := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "local",
		Draft: desktop.ItemDraft{Name: "Local fixture", Environment: environmentDraft(environment.address, "nonproduction", "plain"),
			SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}}})
	named := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "named",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: environmentDraft("lab.example.test:2575", "nonproduction", "plain"),
			SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}}})
	source := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, IntentID: "source",
		Draft: desktop.ItemDraft{Name: "Appointments export", Observation: observationDraft(t, "appointments")}})
	if source.Saved == nil {
		t.Fatalf("save source: %+v", source)
	}
	folder := t.TempDir()
	marker := filepath.Join(folder, "key-command-ran")
	program := filepath.Join(folder, "read-key.sh")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(folder, "hub-client.json")
	writeDocument(t, folder, "hub-client.json", fmt.Sprintf(`{"schema":"readmit-hub-client/v1","hub":"https://%s","ca":%q,"certificate":%q,`+
		`"key":{"command":%q,"arguments":[]},`+
		`"idp":{"issuer":"https://idp.example.test","client_id":"desktop-app","audience":"hub-aud","authorize_endpoint":"https://idp.example.test/auth","token_endpoint":"https://idp.example.test/token","scopes":["evidence.read"]},`+
		`"projects":["alpha"]}`, hub.address, filepath.Join(folder, "ca.pem"), filepath.Join(folder, "cert.pem"), program))
	if selected := app.SelectHubConfig(config); selected.State != desktop.Completed {
		t.Fatalf("hub configuration: %+v", selected)
	}
	if chosen := app.ChooseCommercialDestinations(); chosen.State != desktop.Completed {
		t.Fatalf("commercial destinations: %+v", chosen)
	}
	lookups.Store(0)

	// Listing does not wait for the slot: an operation holding it does not
	// make the inventory busy.
	release, held := desktop.HoldSlotForTest(app, "")
	if !held {
		t.Fatal("the slot was not free")
	}
	rows := connectionRows(t, app, context)
	release()

	for ref, want := range map[string]struct {
		kind        desktop.ConnectionKind
		name        string
		destination string
		state       desktop.ConnectionState
	}{
		"environment:" + local.ID:        {"environment", "Local fixture", environment.address, "not-checked"},
		"environment:" + named.ID:        {"environment", "Scheduling lab", "lab.example.test:2575", "not-checked"},
		"observation:" + source.Saved.ID: {"source", "Appointments export", "", "not-checked"},
		"hub:client":                     {"team", "Team hub", hub.address, "not-checked"},
		"portal":                         {"portal", "Customer portal", "Browser", "not-checked"},
	} {
		row, ok := rows[ref]
		if !ok {
			t.Errorf("no %s row: %+v", ref, rows)
			continue
		}
		if row.Kind != want.kind || row.Name != want.name || row.State != want.state || want.destination != "" && row.Destination != want.destination {
			t.Errorf("%s: %+v, want %+v", ref, row, want)
		}
		if row.CheckedAt != nil || len(row.Actions) != 1 || row.Actions[0] != "edit" || row.Disclosure == "" {
			t.Errorf("%s: a row nothing reached, whose only action is Edit: %+v", ref, row)
		}
	}
	if owner := rows["environment:"+local.ID].Owner; owner.Kind != "environment" || owner.ObjectID != local.ID {
		t.Errorf("the environment's owner: %+v", owner)
	}
	if len(rows) != 5 {
		t.Errorf("the inventory lists what is not configured: %+v", rows)
	}
	if environment.accepted.Load() != 0 || hub.accepted.Load() != 0 || lookups.Load() != 0 {
		t.Fatalf("listing reached a destination: %d environment and %d hub connections, %d lookups",
			environment.accepted.Load(), hub.accepted.Load(), lookups.Load())
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("listing ran the hub's key command")
	}

	// While a hub request holds the slot and runs a declared program, the
	// hub row is active and the program is listed as a Program, first; no
	// row offers Disconnect, because no session is connected.
	release, held = desktop.HoldSlotForTest(app, "hub")
	if !held {
		t.Fatal("the slot was not free")
	}
	ended := desktop.DeclaredProgramRunningForTest(app)
	active := app.ListConnections(context)
	ended()
	release()
	if len(active.Rows) < 2 || active.Rows[0].State != "active" || active.Rows[1].State != "active" {
		t.Fatalf("active rows are not first: %+v", active.Rows)
	}
	byRef := map[string]desktop.ConnectionRow{}
	for _, row := range active.Rows {
		byRef[row.Ref] = row
		for _, action := range row.Actions {
			if action == "disconnect" {
				t.Errorf("%s offers Disconnect with no connected session", row.Ref)
			}
		}
	}
	if hubRow := byRef["hub:client"]; hubRow.State != "active" || hubRow.Detail.Operation != "hub" {
		t.Errorf("the hub while a hub request runs: %+v", hubRow)
	}
	if program := byRef["program"]; program.State != "active" || program.Kind != "program" || program.Destination != "Program" {
		t.Errorf("the running declared program: %+v", program)
	}
	if after := connectionRows(t, app, context); after["hub:client"].State != "not-checked" || len(after) != 5 {
		t.Errorf("after the request the inventory still reads it: %+v", after)
	}
}

func TestAnExplicitCheckIsCheckedNeverConnected(t *testing.T) {
	app, context := connectionsProject(t)
	endpoint := newCountingEndpoint(t, false)
	saved := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "first",
		Draft: desktop.ItemDraft{Name: "Local fixture", Environment: environmentDraft(endpoint.address, "nonproduction", "plain"),
			SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}}})
	checked := app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: saved})
	if checked.CheckedAt == "" || endpoint.accepted.Load() != 1 {
		t.Fatalf("the check: %+v", checked)
	}
	rows := connectionRows(t, app, context)
	row := rows["environment:"+saved.ID]
	if row.State != "checked" || row.CheckedAt == nil || *row.CheckedAt != checked.CheckedAt || row.Detail.Outcome != checked.Report.Outcome || row.Detail.Revision != "1" {
		t.Fatalf("a checked environment: %+v", row)
	}
	for _, listed := range rows {
		if listed.State == "connected" || listed.State == "active" {
			t.Fatalf("a finished check reads %s: %+v", listed.State, listed)
		}
	}
	if endpoint.accepted.Load() != 1 {
		t.Fatalf("listing reached the checked environment again: %d", endpoint.accepted.Load())
	}
}

func TestListConnectionsKeepsAnActiveOperationAfterItsConfigurationIsRemoved(t *testing.T) {
	// The check's one name lookup waits until the test releases it, so the
	// check is reaching outside for as long as the test needs.
	started, released := make(chan struct{}, 1), make(chan struct{})
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-released:
		case <-ctx.Done():
		}
		return nil, errors.New("lookup refused")
	}}
	defer func() { net.DefaultResolver = previous }()
	var once atomic.Bool
	releaseLookup := func() {
		if once.CompareAndSwap(false, true) {
			close(released)
		}
	}
	defer releaseLookup()

	app, context := connectionsProject(t)
	draft := environmentDraft("lab.example.test:2575", "nonproduction", "plain")
	draft.ConnectTimeout = "30s"
	saved := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "lab",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: draft,
			SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}}})
	done := make(chan desktop.EnvironmentCheckResult, 1)
	go func() { done <- app.CheckEnvironment(desktop.ItemRequest{Context: context, Ref: saved}) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the check never reached its destination")
	}

	// Another window removes the environment while the check runs.
	other := activatedApp(t, &chooser{}, t.TempDir())
	if removed := other.RemoveItem(desktop.ItemRequest{Context: context, Ref: saved}); removed.State != desktop.Completed {
		t.Fatalf("remove the environment: %+v", removed)
	}
	listed := app.ListConnections(context)
	if listed.State != desktop.Completed || len(listed.Rows) == 0 {
		t.Fatalf("list while the check runs: %+v", listed)
	}
	row := listed.Rows[0]
	if row.Ref != "environment:"+saved.ID || row.State != "active" || row.Name != "Scheduling lab" ||
		row.Destination != "lab.example.test:2575" || row.Detail.Operation != "target-check" || row.Owner.ObjectID != saved.ID {
		t.Fatalf("the active check is not listed first as itself: %+v", listed.Rows)
	}

	releaseLookup()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the check did not end")
	}
	for _, after := range app.ListConnections(context).Rows {
		if after.Ref == "environment:"+saved.ID || after.State == "active" {
			t.Fatalf("a finished check of a removed environment is still listed: %+v", after)
		}
	}
}
