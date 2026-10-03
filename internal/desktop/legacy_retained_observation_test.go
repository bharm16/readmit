package desktop_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runresult"
)

func TestLegacyRetainedObservationReadsActualImmutableSnapshotWithExactScopeAndReveal(t *testing.T) {
	app, workspace := ledgerWorkspace(t)
	listener, fixture := ledgerReceiver(t, workspace)
	defer listener.Close()
	spec := authorLedgerSpec(t, app, workspace)
	ready := app.PreflightRun(desktop.RunPreflightRequest{Workspace: workspace, Spec: spec})
	if ready.Preflight == nil {
		t.Fatalf("preflight unavailable: %+v", ready)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := fixture.Serve(ctx, listener); done <- err }()
	actual := app.StartDurableRun(desktop.DurableRunRequest{Workspace: workspace, Spec: spec, Output: ready.Preflight.Destination.Name, Expected: ready.Preflight.Identity})
	if actual.Run == nil || actual.Run.State != durablerun.Passed {
		t.Fatalf("actual fixture run absent: %+v", actual)
	}
	// Import the actual sealed execution into a named project, preserving its
	// immutable engine/result/snapshot bytes and leaving publication unlinked.
	original := filepath.Join(workspace, ready.Preflight.Destination.Name)
	viewer, requestContext := namedProject(t)
	entry := "actual-legacy-evidence"
	if err := os.CopyFS(filepath.Join(requestContext.Project, entry), os.DirFS(original)); err != nil {
		t.Fatal(err)
	}
	app = viewer
	runs := app.ListCatalog(desktop.CatalogQuery{Context: requestContext, Kind: desktop.RunItem})
	if runs.Page == nil {
		t.Fatalf("catalog refused actual scope: %+v", runs)
	}
	var ref desktop.ItemRef
	for _, item := range runs.Page.Items {
		if item.Summary.Run != nil && item.Summary.Run.Entry == entry {
			ref = item.Ref
		}
	}
	if ref.ID == "" {
		t.Fatal("actual run not discovered")
	}
	opened := app.OpenRun(desktop.RunRequest{Context: requestContext, Run: ref})
	if opened.Run == nil || opened.Run.RetainedObservation == nil || !opened.Run.RetainedObservation.Available {
		t.Fatalf("actual snapshot not offered: %+v", opened)
	}
	pin := opened.Run.RetainedObservation
	archived, err := runresult.Open(filepath.Join(requestContext.Project, entry))
	if err != nil {
		t.Fatal(err)
	}
	if archived.Artifact == nil || archived.Artifact.FinalObservation == nil || len(archived.Artifact.FinalObservation.Records) != 1 {
		t.Fatal("independent retained fixture snapshot missing")
	}
	if pin.Identity != archived.Artifact.Result.FinalObservation.SHA256 || pin.SourceIdentity != archived.Artifact.Result.InputBundleIdentity {
		t.Fatal("snapshot/input pins disagree with immutable owner")
	}
	stop()
	<-done
	request := desktop.ConnectedObservationRequest{Context: requestContext, Run: ref, Family: pin.Family, Phase: pin.Phase, Dataset: pin.Dataset, Identity: pin.Identity, SourceIdentity: pin.SourceIdentity, Limit: 1}
	hidden := app.ReadConnectedObservation(request)
	if !hidden.Available || hidden.Total != 1 || len(hidden.Rows) != 1 || !hidden.Hidden {
		t.Fatalf("bounded masked read unavailable: %+v", hidden)
	}
	for _, value := range hidden.Rows[0].Values {
		if value.Text != "" || len(value.Items) != 0 {
			t.Fatal("masked preview leaked a retained value")
		}
	}
	request.Reveal = true
	shown := app.ReadConnectedObservation(request)
	if !shown.Available || shown.Rows[0].Values[0].Text != archived.Artifact.FinalObservation.Records[0].RecordID {
		t.Fatal("reveal changed or recreated original values")
	}
	for _, field := range []string{"identity", "source", "phase", "dataset", "family", "job"} {
		next := request
		switch field {
		case "identity":
			next.Identity = "foreign"
		case "source":
			next.SourceIdentity = "foreign"
		case "phase":
			next.Phase = "foreign"
		case "dataset":
			next.Dataset = "foreign"
		case "family":
			next.Family = "foreign"
		case "job":
			next.Job = "../foreign"
		}
		refused := app.ReadConnectedObservation(next)
		if refused.Available || refused.State != desktop.Failed {
			t.Fatalf("borrowed %s scope read: %+v", field, refused)
		}
	}
	request.Offset = math.MaxInt
	empty := app.ReadConnectedObservation(request)
	if !empty.Available || len(empty.Rows) != 0 || empty.Total != 1 {
		t.Fatalf("oversized offset overflowed evidence page: %+v", empty)
	}
	file := filepath.Join(archived.ResultPath, "observation.json")
	held := file + ".held"
	if err := os.Rename(file, held); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(held, file)
	missing := app.ReadConnectedObservation(request)
	if missing.Available || len(missing.Rows) != 0 {
		t.Fatal("missing snapshot became an available zero")
	}
}
