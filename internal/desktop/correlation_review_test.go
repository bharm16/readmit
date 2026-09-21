package desktop_test

import (
	"fmt"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorrelationReviewRetainsMachineAndInvalidatesPriorMapping(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry}
	initial := app.OpenCorrelationReview(req)
	if initial.State != desktop.Completed || initial.View == nil || len(initial.View.Links) == 0 {
		t.Fatalf("initial: %+v", initial)
	}
	req.Mapping = initial.View.Mapping
	req.Decision = correlate.Decision{Action: "reject", Link: initial.View.Links[0].ID, Actor: "local analyst", Reason: "separate observation"}
	req.Output = "review-1"
	saved := app.DecideCorrelation(req)
	if saved.State != desktop.Completed || saved.View == nil {
		t.Fatalf("saved: %+v", saved)
	}
	if saved.View.Links[0].Status != "rejected" || saved.View.Mapping == initial.View.Mapping {
		t.Fatalf("decision not reflected: %+v", saved.View)
	}
	original, err := os.ReadFile(filepath.Join(root, "review-1", "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Previous = "review-1"
	// An earlier derived view cannot be reused against the new mapping.
	if got := app.OpenCorrelationReview(req); got.State != desktop.Failed || got.View != nil {
		t.Fatalf("stale derived view accepted: %+v", got)
	}
	req.Mapping = saved.View.Mapping
	req.Output = "review-2"
	req.Decision.Action = "accept"
	accepted := app.DecideCorrelation(req)
	if accepted.State != desktop.Completed || accepted.View.Links[0].Status != "accepted" {
		t.Fatalf("accept: %+v", accepted)
	}
	current, err := os.ReadFile(filepath.Join(root, "review-2", "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != string(current) {
		t.Fatal("human decision rewrote machine finding")
	}
	req.Previous = "review-2"
	req.Mapping = ""
	reopened := app.OpenCorrelationReview(req)
	if reopened.State != desktop.Completed || len(reopened.View.History) != 2 {
		t.Fatalf("history lost: %+v", reopened)
	}
	if reopened.View.History[0].Actor != "" || reopened.View.History[0].Reason != "" {
		t.Fatal("private text exposed by default")
	}
	req.ShowValues = true
	if got := app.OpenCorrelationReview(req); got.View.History[0].Reason != "separate observation" {
		t.Fatal("explicit private history unavailable")
	}
	if got := app.OpenSequence(sequenceRequest(root, identity, seqRulesEntry)); got.State != desktop.Completed {
		t.Fatal("evidence changed")
	}
}

func TestCorrelationReviewAddsExplicitPairWithoutResolvingOtherCandidates(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	// Declared scope makes the acknowledgement ambiguous across two captures.
	rules := strings.Replace(seqRules, `"operator": "acknowledges", "scope": "source"`, `"operator": "acknowledges", "scope": "declared", "sources": ["s0001", "s0002"]`, 1)
	if err := os.WriteFile(filepath.Join(root, seqRulesEntry), []byte(rules), 0600); err != nil {
		t.Fatal(err)
	}
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry}
	initial := app.OpenCorrelationReview(req)
	if initial.State != desktop.Completed || len(initial.View.Collisions) == 0 {
		t.Fatalf("missing ambiguity: %+v", initial)
	}
	req.Mapping = initial.View.Mapping
	req.Output = "manual"
	req.Decision = correlate.Decision{Action: "add", From: "s0001-e000002", To: "s0002-e000001", Actor: "analyst", Reason: "checked capture context locally"}
	got := app.DecideCorrelation(req)
	if got.State != desktop.Completed {
		t.Fatalf("add: %+v", got)
	}
	link := got.View.Links[len(got.View.Links)-1]
	if link.Linkage != "manual" || link.Status != "accepted" || len(got.View.Collisions) != len(initial.View.Collisions) {
		t.Fatalf("manual claim changed evidence: %+v", got.View)
	}
	req.Previous = "manual"
	req.Mapping = got.View.Mapping
	req.Output = "duplicate"
	if duplicate := app.DecideCorrelation(req); duplicate.State != desktop.Failed {
		t.Fatal("duplicate manual pair accepted")
	}
	req.Decision = correlate.Decision{Action: "reject", Link: link.ID, Actor: "analyst", Reason: "reconsidered context"}
	req.Output = "rejected"
	rejected := app.DecideCorrelation(req)
	if rejected.State != desktop.Completed || rejected.View.Links[len(rejected.View.Links)-1].Status != "rejected" {
		t.Fatalf("manual rejection: %+v", rejected)
	}
}

func TestCorrelationReviewRefusesInvalidDecisionsAndRecovers(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry}
	initial := app.OpenCorrelationReview(req)
	req.Mapping = initial.View.Mapping
	req.Output = "refused"
	good := correlate.Decision{Action: "add", From: "s0001-e000001", To: "s0002-e000001", Actor: "analyst", Reason: "local context"}
	for name, change := range map[string]func(*desktop.CorrelationReviewRequest){
		"no actor":           func(r *desktop.CorrelationReviewRequest) { r.Decision.Actor = "" },
		"no reason":          func(r *desktop.CorrelationReviewRequest) { r.Decision.Reason = " " },
		"control":            func(r *desktop.CorrelationReviewRequest) { r.Decision.Reason = "a\x00b" },
		"oversized reason":   func(r *desktop.CorrelationReviewRequest) { r.Decision.Reason = strings.Repeat("x", 1025) },
		"unknown action":     func(r *desktop.CorrelationReviewRequest) { r.Decision.Action = "merge" },
		"missing occurrence": func(r *desktop.CorrelationReviewRequest) { r.Decision.To = "s9999-e999999" },
		"unparsed":           func(r *desktop.CorrelationReviewRequest) { r.Decision.To = "s0002-e000002" },
		"same occurrence":    func(r *desktop.CorrelationReviewRequest) { r.Decision.To = r.Decision.From },
		"extra link":         func(r *desktop.CorrelationReviewRequest) { r.Decision.Link = "some-link" },
		"missing link": func(r *desktop.CorrelationReviewRequest) {
			r.Decision = correlate.Decision{Action: "accept", Link: "collision", Actor: "analyst", Reason: "reason"}
		},
		"accept extra pair": func(r *desktop.CorrelationReviewRequest) {
			r.Decision.Action = "accept"
			r.Decision.Link = initial.View.Links[0].ID
		},
		"stale identity":  func(r *desktop.CorrelationReviewRequest) { r.Identity = "stale" },
		"stale mapping":   func(r *desktop.CorrelationReviewRequest) { r.Mapping = "stale" },
		"absent mapping":  func(r *desktop.CorrelationReviewRequest) { r.Mapping = "" },
		"absent rules":    func(r *desktop.CorrelationReviewRequest) { r.Rules = "" },
		"unsafe output":   func(r *desktop.CorrelationReviewRequest) { r.Output = "incident/nested" },
		"occupied output": func(r *desktop.CorrelationReviewRequest) { r.Output = "incident" },
	} {
		t.Run(name, func(t *testing.T) {
			r := req
			r.Decision = good
			change(&r)
			got := app.DecideCorrelation(r)
			if got.State != desktop.Failed || got.View != nil {
				t.Fatalf("accepted: %+v", got)
			}
			if recovered := app.OpenCorrelationReview(req); recovered.State != desktop.Completed {
				t.Fatalf("did not recover: %+v", recovered)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "refused")); !os.IsNotExist(err) {
		t.Fatal("refusal created output")
	}
}

func TestCorrelationReviewRejectsDamagedOrStaleRetainedArtifacts(t *testing.T) {
	for _, name := range []string{"missing seal", "changed machine", "unknown nested", "unknown top", "omitted nested", "null decisions", "changed history", "extra member", "rules changed", "truncated", "symlink"} {
		t.Run(name, func(t *testing.T) {
			app, root, identity := sequenceWorkspace(t)
			req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry, Output: "review"}
			initial := app.OpenCorrelationReview(req)
			req.Mapping = initial.View.Mapping
			req.Decision = correlate.Decision{Action: "reject", Link: initial.View.Links[0].ID, Actor: "analyst", Reason: "local reason"}
			saved := app.DecideCorrelation(req)
			if saved.State != desktop.Completed {
				t.Fatalf("save: %+v", saved)
			}
			doc := filepath.Join(root, "review", "decisions.json")
			data, err := os.ReadFile(doc)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing seal":
				err = os.Remove(filepath.Join(root, "review", "identity.sha256"))
			case "changed machine":
				err = os.WriteFile(filepath.Join(root, "review", "machine.json"), []byte(`{}`), 0600)
			case "unknown nested":
				data = []byte(strings.Replace(string(data), `"action":`, `"typo":true,"action":`, 1))
			case "unknown top":
				data = append([]byte(`{"typo":true,`), data[1:]...)
			case "omitted nested":
				data = []byte(strings.Replace(string(data), `"from":"",`, "", 1))
			case "null decisions":
				data = []byte(`{"schema":"readmit-correlation-review/v1","machine":"x","parent":"","decisions":null}`)
			case "changed history":
				data = []byte(strings.Replace(string(data), "local reason", "changed reason", 1))
			case "extra member":
				err = os.WriteFile(filepath.Join(root, "review", "extra"), nil, 0600)
			case "rules changed":
				err = os.WriteFile(filepath.Join(root, seqRulesEntry), []byte(strings.Replace(seqRules, "same-message", "changed-rule", 1)), 0600)
			case "truncated":
				data = data[:len(data)/2]
			case "symlink":
				if err = os.Remove(doc); err == nil {
					err = os.Symlink(filepath.Join(root, seqRulesEntry), doc)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != "symlink" {
				if err = os.WriteFile(doc, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			req.Previous = "review"
			req.Mapping = ""
			got := app.OpenCorrelationReview(req)
			if got.State != desktop.Failed || got.View != nil {
				t.Fatalf("damaged review accepted: %+v", got)
			}
		})
	}
}

func TestCorrelationReviewRejectsReacceptingADuplicateManualPair(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry}
	view := app.OpenCorrelationReview(req).View
	firstID := ""
	for i, action := range []string{"add", "reject", "add", "accept"} {
		req.Mapping = view.Mapping
		req.Output = fmt.Sprintf("revision-%d", i)
		req.Decision = correlate.Decision{Action: action, Actor: "analyst", Reason: "local decision"}
		if action == "add" {
			req.Decision.From = "s0001-e000001"
			req.Decision.To = "s0002-e000001"
		} else {
			req.Decision.Link = firstID
		}
		got := app.DecideCorrelation(req)
		if i == 3 {
			if got.State != desktop.Failed {
				t.Fatal("two concurrent manual interpretations accepted")
			}
			break
		}
		if got.State != desktop.Completed {
			t.Fatalf("decision %d: %+v", i, got)
		}
		view = got.View
		req.Previous = req.Output
		if i == 0 {
			firstID = view.Links[len(view.Links)-1].ID
		}
	}
}

func TestCorrelationReviewWindowsKeepCountsAndStaleRulePinsRefuse(t *testing.T) {
	app, root, _ := sequenceWorkspace(t)
	var source strings.Builder
	for i := 0; i < 205; i++ {
		source.WriteString(framed(strings.Replace(seqBooking, "CTL-1", fmt.Sprintf("CTL-%d", i), 1)))
	}
	written := writeInputs(t, root, "large", []bundle.Input{
		{Path: "one", Data: []byte(source.String()), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}},
		{Path: "two", Data: []byte(source.String()), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}},
	})
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "large", Identity: written.Identity, Rules: seqRulesEntry}
	first := app.OpenCorrelationReview(req)
	if first.State != desktop.Completed || len(first.View.Links) != 200 || first.View.TotalLinks != 206 {
		t.Fatalf("first window: %+v", first)
	}
	req.Offset = 200
	req.Mapping = first.View.Mapping
	next := app.OpenCorrelationReview(req)
	if next.State != desktop.Completed || len(next.View.Links) != 6 || next.View.Mapping != first.View.Mapping {
		t.Fatalf("next window: %+v", next)
	}
	patient := next.View.Links[5]
	if patient.TotalOccurrences != 410 || len(patient.Occurrences) != 32 {
		t.Fatalf("membership window: %+v", patient)
	}
	req.Offset = int(^uint(0) >> 1)
	if got := app.OpenCorrelationReview(req); got.State != desktop.Completed || len(got.View.Links) != 0 {
		t.Fatalf("past window: %+v", got)
	}
	req.RulesSHA256 = "old-displayed-rules"
	if got := app.OpenCorrelationReview(req); got.State != desktop.Failed {
		t.Fatal("stale sequence rule pin accepted")
	}
}

func TestCorrelationReviewSharesOperationSlotAndCancellationDoesNotReplay(t *testing.T) {
	_, root, identity := sequenceWorkspace(t)
	reentrant := &chooser{folder: root}
	app := activatedApp(t, reentrant, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"))
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry, Output: "never-written"}
	reentrant.before = func() {
		for _, got := range []desktop.CorrelationReviewResult{app.OpenCorrelationReview(req), app.DecideCorrelation(req)} {
			if got.State != desktop.Busy || got.View != nil {
				t.Fatalf("shared slot bypassed: %+v", got)
			}
		}
	}
	if got := app.SelectWorkspace(); got.State != desktop.Completed {
		t.Fatalf("workspace: %+v", got)
	}
	app.Cancel("")
	if got := app.OpenCorrelationReview(req); got.State != desktop.Completed {
		t.Fatalf("cancel prevented read recovery: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, req.Output)); !os.IsNotExist(err) {
		t.Fatal("cancel or read replayed a write")
	}
}
