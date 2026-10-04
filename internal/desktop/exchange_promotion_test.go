package desktop_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/desktop"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRetainedExchangePromotionCreatesOrderedIncompleteDraftWithoutObservedOracle(t *testing.T) {
	target := newReplayReceiver(t)
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, target.address)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	source := exchangeSource(t, app, context, port)
	request := exchangeRequest(context, incident, lab, source)
	review := prepared(t, app, request)
	actual := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "promotion-exchange"})
	if actual.Exchange == nil || actual.Exchange.Inputs == nil || actual.Exchange.Identity == "" {
		t.Fatalf("exact promotion origins missing: %+v", actual)
	}
	origin := actual.Exchange
	before := target.reached()
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: origin.Inputs.Case, Messages: origin.Inputs.Messages, Exchange: &desktop.ExchangeOrigin{ID: origin.ID, Identity: origin.Identity}}})
	if draft.Draft == nil || draft.Draft.ConnectedTest == nil || draft.Draft.TestLinks == nil || draft.Draft.TestLinks.Source == nil || draft.Draft.TestLinks.Source.Exchange == nil {
		t.Fatalf("promotion did not enter named connected draft: %+v", draft)
	}
	held := draft.Draft.ConnectedTest
	order := []string{}
	for _, step := range held.Steps {
		order = append(order, step.Source.Occurrence)
		if step.Source.Identity != origin.InputIdentity {
			t.Fatal("original identity changed")
		}
	}
	if !reflect.DeepEqual(order, origin.Inputs.Messages) || len(held.Phases) != 1 || len(held.Phases[0].Checks) != 0 || len(held.Phases[0].Acks) != 0 || len(held.Phases[0].Observations) != 0 || len(held.Variables) != 0 {
		t.Fatalf("observed evidence became expected truth: %+v", held)
	}
	content, err := json.Marshal(desktop.TestEditorDraft{Schema: desktop.TestEditorDraftSchemaV3, Mode: "new", Step: "setup", Case: &origin.Inputs.Case, Draft: *draft.Draft})
	if err != nil {
		t.Fatal(err)
	}
	retained := app.SaveEditorDraft(desktop.EditorDraft{Kind: desktop.TestEditorDraftKind, Workspace: context.Project, ContentSchema: desktop.TestEditorDraftSchemaV3, Content: jsontext.Value(content)})
	if retained.State != desktop.Completed || len(retained.Drafts) != 1 {
		t.Fatalf("incomplete exchange draft not retained: %+v", retained)
	}
	reopened := app.EditorDrafts()
	if len(reopened.Drafts) != 1 || string(reopened.Drafts[0].Content) != string(retained.Drafts[0].Content) {
		t.Fatalf("ordered origin did not reopen: %+v", reopened)
	}
	for _, legacy := range []string{desktop.TestEditorDraftSchema, desktop.TestEditorDraftSchemaV2} {
		old := strings.Replace(string(content), desktop.TestEditorDraftSchemaV3, legacy, 1)
		refused := app.SaveEditorDraft(desktop.EditorDraft{Kind: desktop.TestEditorDraftKind, Workspace: context.Project, ContentSchema: legacy, Content: jsontext.Value(old)})
		if refused.State != desktop.Failed {
			t.Fatalf("legacy schema reinterpreted exchange: %+v", refused)
		}
	}
	if target.reached() != before {
		t.Fatal("promotion sent input")
	}
	stale := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: origin.Inputs.Case, Exchange: &desktop.ExchangeOrigin{ID: origin.ID, Identity: "different-snapshot"}}})
	if stale.State != desktop.Failed {
		t.Fatalf("changed exchange rebound: %+v", stale)
	}
}

func TestRetainedPartialExchangePromotesMissingConfigurationButRefusesChangedTarget(t *testing.T) {
	for _, mutation := range []string{"partial", "missing", "changed", "receiver", "input"} {
		t.Run(mutation, func(t *testing.T) {
			target := newReplayReceiver(t)
			app, context := namedProject(t)
			incident, lab := sendProject(t, app, context, target.address)
			probe, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()
			port := probe.Addr().(*net.TCPAddr).Port
			if mutation != "partial" {
				probe.Close()
			}
			source := exchangeSource(t, app, context, port)
			review := prepared(t, app, exchangeRequest(context, incident, lab, source))
			actual := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "partial-promotion"})
			if actual.Exchange == nil {
				t.Fatal("retained intent absent")
			}
			history := app.ListExchanges(context)
			if len(history.Exchanges) != 1 {
				t.Fatalf("partial history: %+v", history)
			}
			origin := &history.Exchanges[0]
			if mutation == "missing" {
				// Removing the object through its ordinary owner preserves its retained context.
				removed := app.RemoveItem(desktop.ItemRequest{Context: context, Ref: lab})
				if removed.State != desktop.Completed {
					t.Fatalf("target removal: %+v", removed)
				}
			}
			if mutation == "changed" {
				opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: lab})
				opened.Draft.Environment.MessageTimeout = "59s"
				changed := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: lab.ID, BaseRevision: lab.Revision, IntentID: "changed-target", Draft: *opened.Draft})
				if changed.Saved == nil {
					t.Fatal("target not changed")
				}
			}
			if mutation == "receiver" {
				opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: source})
				opened.Draft.Source.Listener.Port = 65531
				changed := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SourceItem, Item: source.ID, BaseRevision: source.Revision, IntentID: "changed-receiver", Draft: *opened.Draft})
				if changed.Saved == nil {
					t.Fatal("receiver not changed")
				}
			}
			if mutation == "input" {
				listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
				entry := ""
				for _, item := range listed.Page.Items {
					if item.Ref.ID == incident.ID {
						entry = item.Summary.Case.Entry
					}
				}
				if entry == "" {
					t.Fatal("original case missing")
				}
				path := filepath.Join(context.Project, entry, "payloads", "s0001-e000001.bin")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if os.WriteFile(path, append(raw, ' '), 0o600) != nil {
					t.Fatal("input witness did not mutate")
				}
			}
			got := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: origin.Inputs.Case, Exchange: &desktop.ExchangeOrigin{ID: origin.ID, Identity: origin.Identity}}})
			if mutation == "changed" || mutation == "receiver" || mutation == "input" {
				if got.State != desktop.Failed {
					t.Fatalf("changed target rebound: %+v", got)
				}
				return
			}
			if got.Draft == nil || got.Draft.ConnectedTest == nil {
				t.Fatalf("valid incomplete draft refused: %+v", got)
			}
			provenance := got.Draft.TestLinks.Source.Exchange
			if mutation == "partial" && provenance.Coverage != "incomplete" {
				t.Fatal("partial was credited")
			}
			if mutation == "missing" && !strings.Contains(provenance.ConfigurationState, "unavailable") {
				t.Fatal("missing target was credited")
			}
		})
	}
}
