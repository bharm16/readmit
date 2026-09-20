package suite_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/secret"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
)

func TestPublicPromotionRunsApprovedSuiteInTwoEnvironments(t *testing.T) {
	dir, doc, _ := releasedFixture(t)
	for _, env := range []string{"east", "west"} {
		address := peer(t, func(c net.Conn) { ack(c, "AA") })
		raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
		var target map[string]any
		_ = json.Unmarshal(raw, &target)
		target["address"] = address
		write(t, filepath.Join(dir, env+".json"), target)
	}
	doc.Environments = append(doc.Environments, suite.Environment{ID: "west", Site: "hospital-b", Bindings: []suite.Binding{{Parameter: "interface", Target: "west.json"}}})
	write(t, filepath.Join(dir, "suite.json"), doc)
	for _, env := range []string{"east", "west"} {
		common := []string{filepath.Join(dir, "suite.json"), "--environment", env, "--releases", filepath.Join(dir, "releases.json"), "--revision", "fixture-v1"}
		var stdout, stderr bytes.Buffer
		args := append([]string{"suite", "review-promotion"}, common...)
		if e := licensedCLI(t, args, &stdout, &stderr); e != nil {
			t.Fatal(e, stderr.String())
		}
		var review suite.PromotionReview
		if e := json.Unmarshal(stdout.Bytes(), &review); e != nil {
			t.Fatal(e)
		}
		approval := filepath.Join(dir, env+"-approval.json")
		args = append(append([]string{"suite", "approve-promotion"}, common...), "--review", review.Identity(), "--approver", "reviewer", "--rationale", "synthetic fixture", "--output", approval)
		stdout.Reset()
		if e := licensedCLI(t, args, &stdout, &stderr); e != nil {
			t.Fatal(e, stderr.String())
		}
		raw, _ := os.ReadFile(approval)
		p, e := suite.DecodePromotion(raw)
		if e != nil {
			t.Fatal(e)
		}
		args = append(append([]string{"suite", "run"}, common...), "--promotion", approval, "--promotion-identity", p.Identity(), "--output", filepath.Join(dir, env+"-run"), "--send", "--json")
		stdout.Reset()
		if e := licensedCLI(t, args, &stdout, &stderr); e != nil {
			t.Fatal(e, stderr.String())
		}
		var report runqueue.Report
		if e := json.Unmarshal(stdout.Bytes(), &report); e != nil || report.ExitCode() != 0 || report.Executed != 1 {
			t.Fatalf("%+v %v", report, e)
		}
	}
}

func approveFixture(t *testing.T, dir string) suite.Promotion {
	t.Helper()
	path := filepath.Join(dir, "suite.json")
	refs := filepath.Join(dir, "releases.json")
	review, e := suite.ReviewPromotion(path, "east", refs, "fixture-v1")
	if e != nil {
		t.Fatal(e)
	}
	p, e := suite.ApprovePromotion(path, "east", refs, "fixture-v1", review.Identity(), "reviewer", "fixture", filepath.Join(dir, "promotion.json"))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestPromotionRefusesChangedInputsBeforeAnySend(t *testing.T) {
	for _, kind := range []string{"target", "parameters", "isolation", "sequence", "approval", "release", "evidence", "unmapped", "secret", "revision", "identity", "production"} {
		t.Run(kind, func(t *testing.T) {
			var sends atomic.Int32
			address := peer(t, func(c net.Conn) { sends.Add(1); ack(c, "AA") })
			dir, doc, _ := releasedFixture(t)
			targetRaw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
			var target map[string]any
			_ = json.Unmarshal(targetRaw, &target)
			target["address"] = address
			write(t, filepath.Join(dir, "east.json"), target)
			p := approveFixture(t, dir)
			revision := "fixture-v1"
			pin := p.Identity()
			switch kind {
			case "target":
				target["message_timeout"] = "3s"
				write(t, filepath.Join(dir, "east.json"), target)
			case "production":
				target["test_endpoint"] = false
				write(t, filepath.Join(dir, "east.json"), target)
			case "parameters":
				doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "two", Case: "case-one"})
				write(t, filepath.Join(dir, "suite.json"), doc)
			case "isolation":
				doc.Tests[0].Isolation = runqueue.IsolatedState
				write(t, filepath.Join(dir, "suite.json"), doc)
			case "sequence":
				doc.Tests[0].Sequence = []string{"s0001-e000002"}
				write(t, filepath.Join(dir, "suite.json"), doc)
			case "approval":
				p.Rationale = "different decision"
				write(t, filepath.Join(dir, "promotion.json"), p)
			case "release":
				raw, _ := os.ReadFile(filepath.Join(dir, "release.json"))
				_ = os.WriteFile(filepath.Join(dir, "release.json"), bytes.Replace(raw, []byte(`"reviewer"`), []byte(`"other"`), 1), 0600)
			case "evidence":
				_ = os.RemoveAll(filepath.Join(dir, "case-one"))
			case "unmapped":
				doc.Environments[0].Bindings = nil
				write(t, filepath.Join(dir, "suite.json"), doc)
			case "secret":
				target["schema"] = "readmit-target/v2"
				target["credential"] = map[string]string{"secrets_file": "missing.json", "reference": "unavailable"}
				write(t, filepath.Join(dir, "east.json"), target)
			case "revision":
				revision = "fixture-v2"
			case "identity":
				pin = strings.Repeat("0", 64)
			}
			out := filepath.Join(dir, "out")
			if _, e := suite.RunPromoted(t.Context(), filepath.Join(dir, "suite.json"), "east", out, filepath.Join(dir, "releases.json"), filepath.Join(dir, "promotion.json"), pin, revision); e == nil {
				t.Fatal("changed approval accepted")
			}
			if sends.Load() != 0 {
				t.Fatal("refusal sent data")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("refused promotion left output")
			}
		})
	}
}
func TestPromotionStrictReaderAndStaleReview(t *testing.T) {
	dir, _, _ := releasedFixture(t)
	p := approveFixture(t, dir)
	raw, _ := json.Marshal(p)
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"approver":`), []byte(`"extra":true,"approver":`), 1),
		bytes.Replace(raw, []byte(`"review":{`), []byte(`"review":{"extra":true,`), 1),
		bytes.Replace(raw, []byte(`"jobs":[{`), []byte(`"jobs":[{"extra":true,`), 1),
		bytes.Replace(raw, []byte(`"schema":"readmit-suite-promotion/v1"`), []byte(`"schema":"readmit-suite-promotion/v2"`), 1),
		bytes.Replace(raw, []byte(`"approver":"reviewer"`), []byte(`"approver":null`), 1),
		bytes.Replace(raw, []byte(`"approver":"reviewer"`), []byte(`"approver":"reviewer","approver":"other"`), 1),
		bytes.Replace(raw, []byte(`"review":`), []byte(`"missing":`), 1),
		raw[:len(raw)-1],
	} {
		if _, e := suite.DecodePromotion(bad); e == nil {
			t.Fatal("malformed approval accepted")
		}
	}
	out := filepath.Join(dir, "stale.json")
	if _, e := suite.ApprovePromotion(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "releases.json"), "different", p.Reviewed, "reviewer", "fixture", out); e == nil {
		t.Fatal("stale review approved")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("stale review wrote approval")
	}
	if _, e := suite.ApprovePromotion(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "releases.json"), "fixture-v1", p.Reviewed, "reviewer", "fixture", filepath.Join(dir, "promotion.json")); e == nil {
		t.Fatal("approval overwritten")
	}
}
func TestPromotedSuiteCancellationRetainsApprovalAndUncertainRecovery(t *testing.T) {
	received := make(chan struct{})
	address := peer(t, func(c net.Conn) {
		reader, _ := mllp.NewReader(c, 1<<20)
		if _, e := reader.ReadFrame(); e == nil {
			close(received)
			_, _ = io.Copy(io.Discard, c)
		}
	})
	dir, _, _ := releasedFixture(t)
	raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	_ = json.Unmarshal(raw, &target)
	target["address"] = address
	write(t, filepath.Join(dir, "east.json"), target)
	p := approveFixture(t, dir)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-received:
			cancel()
		case <-ctx.Done():
		}
	}()
	out := filepath.Join(dir, "cancelled")
	report, e := suite.RunPromoted(ctx, filepath.Join(dir, "suite.json"), "east", out, filepath.Join(dir, "releases.json"), filepath.Join(dir, "promotion.json"), p.Identity(), "fixture-v1")
	if e != nil || report.ExitCode() != 2 || report.Jobs[0].Run.State != durablerun.DeliveryUncertain {
		t.Fatalf("%+v %v", report, e)
	}
	recovery, e := durablerun.Recover(filepath.Join(out, "runs", "booking-one"))
	if e != nil || recovery.Uncertain != 1 || recovery.SafeToRepeat {
		t.Fatalf("%+v %v", recovery, e)
	}
	retained, _ := os.ReadFile(filepath.Join(out, "promotion.json"))
	got, e := suite.DecodePromotion(retained)
	if e != nil || got.Identity() != p.Identity() {
		t.Fatal("lost approval", e)
	}
	if _, e = suite.RunPromoted(t.Context(), filepath.Join(dir, "suite.json"), "east", out, filepath.Join(dir, "releases.json"), filepath.Join(dir, "promotion.json"), p.Identity(), "fixture-v1"); e == nil {
		t.Fatal("uncertain execution resumed")
	}
}
func TestApprovedQueueRefusesAllJobsWhenLastPinDiffers(t *testing.T) {
	var sends atomic.Int32
	dir, doc, _ := releasedFixture(t)
	raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	_ = json.Unmarshal(raw, &target)
	target["address"] = peer(t, func(c net.Conn) { sends.Add(1); ack(c, "AA") })
	write(t, filepath.Join(dir, "east.json"), target)
	doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "two", Case: "case-one"})
	write(t, filepath.Join(dir, "suite.json"), doc)
	p := approveFixture(t, dir)
	prepared, e := suite.PrepareApproved(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "out"), filepath.Join(dir, "releases.json"))
	if e != nil {
		t.Fatal(e)
	}
	pins := map[string]string{}
	for _, j := range p.Review.Jobs {
		pins[j.Job] = j.SHA256
	}
	pins["booking-two"] = strings.Repeat("0", 64)
	queue, _ := json.Marshal(prepared.Queue)
	_, e = runqueue.Run(t.Context(), runqueue.Request{PlanBytes: queue, PlanDirectory: prepared.Directory, Runs: filepath.Join(prepared.Directory, "runs"), ApprovedInputs: pins})
	if e == nil || sends.Load() != 0 {
		t.Fatal("mixed-valid queue sent", e)
	}
	entries, e := os.ReadDir(filepath.Join(prepared.Directory, "runs"))
	if e != nil || len(entries) != 0 {
		t.Fatal("refused queue started a job", e)
	}
}

func TestPromotionCommitsCredentialRegistrationWithoutResolvingAValue(t *testing.T) {
	dir, _, _ := releasedFixture(t)
	store := secret.Document{Schema: secret.Schema, References: []secret.Reference{{Name: "fixture", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint, Address: "127.0.0.1:1", Command: filepath.Join(dir, "provider-that-does-not-exist"), Arguments: []string{}, Generation: 1, RotatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	write(t, filepath.Join(dir, "secrets.json"), store)
	raw, _ := os.ReadFile(filepath.Join(dir, "east.json"))
	var target map[string]any
	_ = json.Unmarshal(raw, &target)
	target["schema"] = "readmit-target/v2"
	target["credential"] = map[string]string{"secrets_file": "secrets.json", "reference": "fixture"}
	write(t, filepath.Join(dir, "east.json"), target)
	// Success with a nonexistent resolution program establishes that this path
	// validates and commits registration, never resolving or retaining a value.
	p := approveFixture(t, dir)
	store.References[0].Generation = 2
	write(t, filepath.Join(dir, "secrets.json"), store)
	if _, e := suite.RunPromoted(t.Context(), filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "out"), filepath.Join(dir, "releases.json"), filepath.Join(dir, "promotion.json"), p.Identity(), "fixture-v1"); e == nil {
		t.Fatal("changed credential registration kept approval")
	}
}
