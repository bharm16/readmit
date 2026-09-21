package runresult_test

import (
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

func TestOpenDirectResultClassifiesItOnce(t *testing.T) {
	opened, err := runresult.Open(filepath.Join("..", "..", "testdata", "acceptance", "native-109", "baseline"))
	if err != nil {
		t.Fatal(err)
	}
	if opened.Durable || opened.Artifact == nil || opened.Spec == nil || opened.Run == nil || len(opened.Assertions) == 0 {
		t.Fatalf("incomplete classification: %+v", opened)
	}
	if usable, reason := opened.Usable(); !usable || reason != durablerun.UsableResult {
		t.Fatalf("usable=%t reason=%v", usable, reason)
	}
}

func TestUsableOwnsDurableLifecyclePolicy(t *testing.T) {
	tests := []struct {
		name    string
		summary durablerun.Summary
		usable  bool
		reason  durablerun.Usability
	}{
		{name: "passed", summary: durablerun.Summary{State: durablerun.Passed, ResultIdentity: "result"}, usable: true},
		{name: "no result", summary: durablerun.Summary{State: durablerun.Interrupted}, reason: durablerun.UsabilityNoResult},
		{name: "journal incomplete", summary: durablerun.Summary{State: durablerun.Passed, ResultIdentity: "result", JournalIncomplete: true}, reason: durablerun.UsabilityJournalIncomplete},
		{name: "delivery uncertain", summary: durablerun.Summary{State: durablerun.DeliveryUncertain, ResultIdentity: "result", DeliveryUncertain: true}, reason: durablerun.UsabilityDeliveryUncertain},
		{name: "nonterminal", summary: durablerun.Summary{State: durablerun.Running, ResultIdentity: "result"}, reason: durablerun.UsabilityUndecided},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := runresult.Result{Durable: true, Lifecycle: test.summary}
			if test.summary.ResultIdentity != "" {
				opened.Artifact = &testrunner.Artifact{Identity: test.summary.ResultIdentity}
			}
			usable, reason := opened.Usable()
			if usable != test.usable || reason != test.reason {
				t.Fatalf("usable=%t reason=%q", usable, reason)
			}
		})
	}
}
