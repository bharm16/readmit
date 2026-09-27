package testisolation_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
	isolation "github.com/bharm16/readmit/internal/testisolation"
)

func continuationAuthorities(t *testing.T, h *harness, previous string) isolation.Authorities {
	t.Helper()
	grant := func(phase string) networkaction.Authority {
		review, err := isolation.ContinuationReview(h.p, previous, phase)
		if err != nil {
			t.Fatal(err)
		}
		return approval{binding: review.Binding, actor: networkaction.Actor{Kind: "runner", ID: "runner", Generation: "continuation", EvidenceIdentity: networkaction.Digest([]byte("continuation-" + phase)), Expires: time.Now().Add(time.Hour)}, revoked: h.revoked}
	}
	return isolation.Authorities{Read: grant("read"), Cleanup: grant("cleanup")}
}
func TestIsolationContinuationChecksCurrentOwnershipWithoutReplayingSetup(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, previous, err := h.start(t, "old-ready")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	requests := h.target.requests
	read, err := isolation.ContinuationReview(h.p, previous, "read")
	if err != nil {
		t.Fatal(err)
	}
	if read.Binding == h.p.Review("read").Binding || h.target.requests != requests {
		t.Fatal("continuation review contacted target or reused old authority")
	}
	if _, err = isolation.Continue(t.Context(), h.p, h.a, previous, filepath.Join(h.root, "old-authority"), isolation.Confirmation{}); err == nil {
		t.Fatal("old execution authority revived")
	}
	a := continuationAuthorities(t, h, previous)
	c, err := isolation.Continue(t.Context(), h.p, a, previous, filepath.Join(h.root, "continued"), isolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.Ready() || len(h.target.effects) != 2 {
		t.Fatal("continuation repeated setup or failed readiness")
	}
	if err = c.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = c.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(h.target.effects, ",") != "create:patient,create:appointment,delete:appointment,delete:patient" {
		t.Fatal(h.target.effects)
	}
	if _, err = isolation.OpenContinuation(filepath.Join(h.root, "continued")); err != nil {
		t.Fatal("continuation readback", err)
	}
	if c.Ready() || c.Check(t.Context()) == nil || c.Cleanup(t.Context()) == nil {
		t.Fatal("closed continuation resumed")
	}
}
func TestIsolationContinuationRefusesChangedLeaseAndPrerequisites(t *testing.T) {
	for _, variant := range []string{"lease-version", "lease-owner", "resource-version", "resource-content", "revoked"} {
		t.Run(variant, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			s, previous, err := h.start(t, "old-ready")
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			a := continuationAuthorities(t, h, previous)
			h.target.mu.Lock()
			switch variant {
			case "lease-version":
				for _, v := range h.target.leases {
					v["version"] = "replacement"
				}
			case "lease-owner":
				for _, v := range h.target.leases {
					v["owner"] = networkaction.Digest([]byte("another"))
				}
			case "resource-version":
				for _, v := range h.target.resources {
					v["version"] = "replacement"
					break
				}
			case "resource-content":
				for _, v := range h.target.resources {
					v["attributes"] = map[string]string{"changed": "true"}
					break
				}
			case "revoked":
				*h.revoked = true
			}
			h.target.mu.Unlock()
			c, err := isolation.Continue(t.Context(), h.p, a, previous, filepath.Join(h.root, "continued"), isolation.Confirmation{})
			if c != nil {
				defer c.Close()
			}
			if err == nil || c != nil && c.Ready() {
				t.Fatal("changed prerequisite authorized continuation")
			}
			if len(h.target.effects) != 2 {
				t.Fatal("refused continuation wrote setup or cleanup")
			}
		})
	}
}
func TestIsolationLiveSessionRechecksBeforeNextPhase(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, _, err := h.start(t, "normal")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CheckReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	for _, v := range h.target.resources {
		v["version"] = "changed"
		break
	}
	h.target.mu.Unlock()
	if s.CheckReady(t.Context()) == nil || s.Ready() {
		t.Fatal("stale setup remained ready")
	}
}

func TestIsolationContinuationRequiresNewExactManualConfirmation(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.contract.Manual = []isolation.ManualStep{{ID: "notifications", Instructions: "Confirm disabled notifications for this attempt"}}
	h.prepare(t)
	preflight := h.preflight(t, "preflight")
	old := isolation.Confirmation{Plan: h.p.Identity(), Instance: h.options.Instance, Steps: []string{"notifications"}}
	previous := filepath.Join(h.root, "old-ready")
	s, err := isolation.Start(t.Context(), h.p, h.a, preflight, previous, old)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	a := continuationAuthorities(t, h, previous)
	for i, confirmation := range []isolation.Confirmation{{}, old} {
		if c, err := isolation.Continue(t.Context(), h.p, a, previous, filepath.Join(t.TempDir(), "refused"), confirmation); err == nil || c != nil {
			t.Fatalf("confirmation %d reconstructed old consent", i)
		}
	}
	review, err := isolation.ContinuationReview(h.p, previous, "read")
	if err != nil {
		t.Fatal(err)
	}
	fresh := isolation.Confirmation{Plan: review.Identity, Instance: h.options.Instance, Steps: []string{"notifications"}}
	output := filepath.Join(h.root, "continued")
	c, err := isolation.Continue(t.Context(), h.p, a, previous, output, fresh)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	got, err := isolation.OpenContinuation(output)
	if err != nil || len(got.Manual) != 1 || got.Manual[0].Provenance != "operator-declared-current-continuation" {
		t.Fatal(got, err)
	}
	if _, err = isolation.Open(output); err == nil {
		t.Fatal("continuation entered frozen v1 reader")
	}
}

func TestIsolationContinuationReviewRefusesUnsettledOrChangedPriorEffects(t *testing.T) {
	for _, variant := range []string{"uncertain-setup", "cleanup-started", "changed-owned-version", "changed-network-proof"} {
		t.Run(variant, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			if variant == "uncertain-setup" {
				h.target.failCreate = true
			}
			s, previous, err := h.start(t, "prior")
			if variant == "uncertain-setup" {
				if err == nil {
					t.Fatal("fixture did not leave uncertain setup")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if variant == "cleanup-started" {
				h.target.failDelete = true
				if err = s.Cleanup(t.Context()); err == nil {
					t.Fatal("fixture did not interrupt cleanup")
				}
			}
			s.Close()
			if variant == "changed-owned-version" {
				path := filepath.Join(previous, "setup.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var result isolation.Result
				if err = json.Unmarshal(raw, &result); err != nil {
					t.Fatal(err)
				}
				result.Resources[0].Version = "forged"
				if err = os.WriteFile(path, encode(result), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(previous, "interrupted.json"), encode(result), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "changed-network-proof" {
				if err = os.WriteFile(filepath.Join(previous, "actions", "n0001", "response.bin"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			requests := h.target.requests
			if _, err = isolation.ContinuationReview(h.p, previous, "read"); err == nil {
				t.Fatal("untrusted old effects restored readiness")
			}
			if h.target.requests != requests {
				t.Fatal("pure refusal contacted target")
			}
		})
	}
}

func TestIsolationContinuationPreservesAndRechecksSelectedUnownedPrerequisites(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			h := fixture(t, "isolated-tenant")
			h.contract.Mode = "recorded-baseline"
			h.contract.Resources = h.contract.Resources[:1]
			h.contract.Resources[0].Ownership = "select"
			h.contract.Resources[0].LogicalID = "baseline-patient"
			h.contract.Resources[0].Version = "old"
			resource := isolation.Resource{Alias: "patient", Kind: "patient", ID: "baseline-patient", Version: "old", Template: "patient", Attributes: map[string]string{"name": "fictional"}, Identifiers: h.contract.Resources[0].Identifiers, References: []isolation.Reference{}}
			h.contract.Baseline = isolation.SnapshotIdentity(isolation.Snapshot{Scope: h.p.Scope(), Resources: []isolation.Resource{resource}})
			h.prepare(t)
			var row map[string]any
			if err := json.Unmarshal(encode(resource), &row); err != nil {
				t.Fatal(err)
			}
			h.target.resources["synthetic/patient/baseline-patient"] = row
			s, previous, err := h.start(t, "prior")
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			a := continuationAuthorities(t, h, previous)
			if changed {
				h.target.mu.Lock()
				row["version"] = "changed"
				h.target.mu.Unlock()
			}
			output := filepath.Join(h.root, "continued")
			c, err := isolation.Continue(t.Context(), h.p, a, previous, output, isolation.Confirmation{})
			if c != nil {
				defer c.Close()
			}
			if changed {
				if err == nil {
					t.Fatal("changed selected prerequisite accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Cleanup(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(h.target.resources) != 1 || len(h.target.effects) != 0 {
				t.Fatal("selected resource was deleted or setup was repeated")
			}
			if _, err = isolation.OpenContinuation(output); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIsolationContinuationDoesNotRetryFailedCleanup(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, previous, err := h.start(t, "prior")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err = isolation.ContinuationReview(h.p, previous, "setup"); err == nil {
		t.Fatal("continuation offered setup authority")
	}
	a := continuationAuthorities(t, h, previous)
	output := filepath.Join(h.root, "continued")
	c, err := isolation.Continue(t.Context(), h.p, a, previous, output, isolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	h.target.failDelete = true
	h.target.mu.Unlock()
	if err = c.Cleanup(t.Context()); err == nil {
		t.Fatal("failed cleanup reported success")
	}
	h.target.mu.Lock()
	requests := h.target.requests
	h.target.failDelete = false
	h.target.mu.Unlock()
	if c.Cleanup(t.Context()) == nil {
		t.Fatal("cleanup automatically retried")
	}
	h.target.mu.Lock()
	if h.target.requests != requests {
		t.Fatal("retry contacted adapter")
	}
	h.target.mu.Unlock()
	c.Close()
	result, err := isolation.OpenContinuation(output)
	if err != nil || result.Cleanup != "uncertain" {
		t.Fatal("uncertain effects not retained", result, err)
	}
	files := transitionFiles(t, output)
	var proof map[string]any
	if err = json.Unmarshal(files["result.json"], &proof); err != nil {
		t.Fatal(err)
	}
	summary := proof["result"].(map[string]any)
	summary["cleanup"] = "not-started"
	summary["entries"] = []any{}
	files["result.json"] = encode(proof)
	files["identity.sha256"] = []byte(artifactdir.Identity(isolation.ContinuationSchema, files) + "\n")
	if _, err = isolation.VerifyContinuation(files); err == nil {
		t.Fatal("resealed summary hid actual cleanup intent")
	}
}
