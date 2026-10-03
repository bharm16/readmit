package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/operationguard"
)

// A person's action that meets a read the window started on its own waits for
// that read to end and then runs, instead of being refused busy; reads that
// arrive while it waits yield to it; and an action that meets another action
// is still refused busy.
func TestAnActionWaitsForAReadHoldingTheSlotAndReadsYieldToIt(t *testing.T) {
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	reading := make(chan struct{})
	finish := make(chan struct{})
	readDone := make(chan CatalogResult)
	go func() {
		readDone <- runRead(app, false, func(context.Context) CatalogResult {
			close(reading)
			<-finish
			return CatalogResult{State: Completed}
		})
	}()
	<-reading

	actionDone := make(chan CatalogResult)
	go func() {
		actionDone <- operate(app, operationguard.Profile{Name: "waiting-action", Interruptible: true}, false, nil, func(context.Context) CatalogResult { return CatalogResult{State: Completed} })
	}()
	// Once the action waits, a new read yields to it.
	deadline := time.Now().Add(time.Second)
	for !app.readsYield() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if later := runRead(app, false, func(context.Context) CatalogResult { return CatalogResult{State: Completed} }); later.State != Busy {
		t.Fatalf("a read that arrived while an action waited: %+v", later)
	}
	app.Cancel("unrelated")
	close(finish)
	if read := <-readDone; read.State != Completed {
		t.Fatalf("the read: %+v", read)
	}
	if action := <-actionDone; action.State != Completed {
		t.Fatalf("the action waited for the read and then ran: %+v", action)
	}

	// An action holding the slot still refuses another action at once.
	holding := make(chan struct{})
	release := make(chan struct{})
	go run(app, false, false, func(context.Context) CatalogResult {
		close(holding)
		<-release
		return CatalogResult{State: Completed}
	})
	<-holding
	if other := run(app, false, false, func(context.Context) CatalogResult { return CatalogResult{State: Completed} }); other.State != Busy {
		t.Fatalf("an action met by another action: %+v", other)
	}
	close(release)
}

func TestStopWhileWaitingForAReadPreventsTheAction(t *testing.T) {
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	reading, finish := make(chan struct{}), make(chan struct{})
	readDone := make(chan CatalogResult, 1)
	go func() {
		readDone <- runRead(app, false, func(context.Context) CatalogResult {
			close(reading)
			<-finish
			return CatalogResult{State: Completed}
		})
	}()
	<-reading
	actionDone := make(chan CatalogResult, 1)
	worked := make(chan struct{}, 1)
	go func() {
		actionDone <- operate(app, operationguard.Profile{Name: "waiting-import", Interruptible: true}, false, nil, func(context.Context) CatalogResult {
			worked <- struct{}{}
			return CatalogResult{State: Completed}
		})
	}()
	deadline := time.Now().Add(time.Second)
	for !app.readsYield() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !app.readsYield() {
		close(finish)
		t.Fatal("action never waited")
	}
	app.Cancel("waiting-import")
	select {
	case result := <-actionDone:
		if result.State != Cancelled {
			t.Errorf("stopped action: %+v", result)
		}
	case <-time.After(time.Second):
		close(finish)
		t.Fatal("Stop waited for the read to finish")
	}
	close(finish)
	if result := <-readDone; result.State != Completed {
		t.Errorf("Stop cancelled the unrelated read: %+v", result)
	}
	select {
	case <-worked:
		t.Fatal("stopped action executed")
	default:
	}
	if app.readsYield() {
		t.Fatal("cancelled waiter still blocks reads")
	}
}

func TestBusyCatalogReadRetainsItsRequestContext(t *testing.T) {
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		run(app, false, false, func(context.Context) CatalogResult {
			close(started)
			<-release
			return CatalogResult{State: Completed}
		})
		close(done)
	}()
	<-started
	defer func() { close(release); <-done }()
	request := RequestContext{Project: t.TempDir(), Generation: 17}
	answer := app.ListCatalog(CatalogQuery{Context: request, Kind: CaseItem})
	if answer.State != Busy || answer.Context != request {
		t.Fatalf("busy answer lost its caller: %+v", answer)
	}
}

// Saving an explicit view is a single local mutation: it waits for automatic
// navigation reads without retrying the write or admitting a competing action.
func TestSaveViewWaitsForBackgroundReadBeforeWritingOnce(t *testing.T) {
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	reading, finish := make(chan struct{}), make(chan struct{})
	readDone := make(chan CatalogResult, 1)
	go func() {
		readDone <- runRead(app, false, func(context.Context) CatalogResult {
			close(reading)
			<-finish
			return CatalogResult{State: Completed}
		})
	}()
	<-reading
	done := make(chan ViewsResult, 1)
	workspace := t.TempDir()
	go func() { done <- app.SaveView(workspace, "Retained selection", grid.Query{}) }()
	deadline := time.Now().Add(time.Second)
	for !app.readsYield() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !app.readsYield() {
		close(finish)
		<-readDone
		t.Fatalf("SaveView bypassed owned read waiting: %+v", <-done)
	}
	close(finish)
	<-readDone
	if saved := <-done; saved.State != Completed || len(saved.Views) != 1 {
		t.Fatalf("saved view: %+v", saved)
	}
	if saved := app.ListViews(workspace); saved.State != Completed || len(saved.Views) != 1 || saved.Views[0].Name != "Retained selection" {
		t.Fatalf("retained view: %+v", saved)
	}
}
