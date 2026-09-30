package desktop_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestFHIRCollectionRefusalShowsSelectedEndpointWithoutReadingSource(t *testing.T) {
	app, context := namedProject(t)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
		t.Error("a refused collection review reached its source")
	}))
	defer server.Close()
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{
		Draft: desktop.ItemDraft{Name: "FHIR review QA", FHIR: &desktop.FHIRConnection{
			Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: server.URL + "/fhir",
			ServerName: "example.com", Authentication: "none", Classification: replay.Nonproduction,
		}, SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}},
		IntentID: "review-endpoint-environment",
	})
	vocabulary := app.Shell().Shell.Vocabulary.Connected
	setup := vocabulary.Observation
	search := vocabulary.Search
	search.Boundary = "reference-fhir-store"
	search.Criteria = []desktop.FHIRCriterion{{Parameter: "identifier", Type: "token", System: "urn:lab:appointment", Value: "qa"}}
	search.Fields = []desktop.FHIRFieldProjection{{Name: "identity", Field: "resource-identity", Key: true, Required: true}, {Name: "status", Field: "status", Required: true}}
	setup.Environment = environment.ID
	setup.FHIR = &search
	setup.BusinessKeys = []desktop.BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft:    desktop.ItemDraft{Name: "FHIR review state", Observation: &desktop.ObservationDraft{Connected: &setup}},
		IntentID: "review-endpoint-observation",
	})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save observation: %+v", saved)
	}
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}})
	if review.Review == nil || review.Review.Ready || !strings.Contains(review.Review.Refusal, "Check capabilities") {
		t.Fatalf("missing capabilities should preserve a refused review: %+v", review)
	}
	if review.Review.Collect == nil || !strings.HasPrefix(review.Review.Collect.Destination, server.URL+"/fhir/Appointment?") || review.Review.Destination.Address != review.Review.Collect.Destination {
		t.Fatalf("FHIR review must name its selected search endpoint, not a legacy file: %+v", review.Review)
	}
	if requests.Load() != 0 {
		t.Fatal("saving or reviewing an unavailable FHIR source requested network authority")
	}
}
