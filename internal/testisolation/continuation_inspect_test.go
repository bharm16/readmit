package testisolation_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/networkaction"
	isolation "github.com/bharm16/readmit/internal/testisolation"
)

func TestInspectContinuationHeldSnapshotPreservesUnsealedCleanupAttempts(t *testing.T) {
	for _, failure := range []string{"none", "delete", "release"} {
		t.Run(failure, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			initial, previous, err := h.start(t, "original")
			if err != nil {
				t.Fatal(err)
			}
			initial.Close()
			a := continuationAuthorities(t, h, previous)
			path := filepath.Join(h.root, "continuation")
			c, err := isolation.Continue(t.Context(), h.p, a, previous, path, isolation.Confirmation{})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if failure != "none" {
				h.target.mu.Lock()
				h.target.failDelete = failure == "delete"
				h.target.failRelease = failure == "release"
				h.target.mu.Unlock()
				if c.Cleanup(t.Context()) == nil {
					t.Fatal("fixture failed to interrupt cleanup")
				}
			}
			files := transitionFiles(t, path)
			if _, exists := files["result.json"]; exists {
				t.Fatal("fixture closed outer continuation")
			}
			before := encode(files)
			h.target.mu.Lock()
			requests := h.target.requests
			h.target.mu.Unlock()
			if err = os.Remove(h.registry); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(h.policy); err != nil {
				t.Fatal(err)
			}
			result, err := isolation.InspectContinuation(files)
			if err != nil {
				t.Fatal(err)
			}
			if result.Setup != "interrupted" || result.Complete || len(result.Manual) != 0 {
				t.Fatal("inspection restored readiness or consent", result)
			}
			if failure == "none" {
				if result.Cleanup != "not-started" || len(result.Entries) != 0 {
					t.Fatal(result)
				}
			} else {
				action, count := "delete", 1
				if failure == "release" {
					action, count = "lease-release", 3
				}
				if result.Cleanup != "uncertain" || len(result.Entries) != count || result.Entries[count-1].Action != action || result.Entries[count-1].State != "uncertain" {
					t.Fatal("outstanding effect disappeared", result)
				}
			}
			if !bytes.Equal(before, encode(files)) {
				t.Fatal("inspector mutated held evidence")
			}
			h.target.mu.Lock()
			if h.target.requests != requests {
				t.Fatal("inspection contacted adapter")
			}
			h.target.mu.Unlock()
			if _, err = isolation.VerifyContinuation(files); err == nil {
				t.Fatal("unsealed facts entered completed reader")
			}
			if failure == "delete" {
				prefix := inspectFixtureClone(files)
				delete(prefix, "cleanup/interrupted.json")
				for name := range prefix {
					if strings.HasPrefix(name, "cleanup/actions/n0002/") {
						delete(prefix, name)
					}
				}
				recovered, err := isolation.InspectContinuation(prefix)
				if err != nil || recovered.Cleanup != "uncertain" || len(recovered.Entries) != 1 || recovered.Entries[0].Network != "" {
					t.Fatal("durable pre-request intent was lost", recovered, err)
				}
				var forged isolation.Entry
				if err = json.Unmarshal(prefix["cleanup/entry-0001-intent.json"], &forged); err != nil {
					t.Fatal(err)
				}
				forged.Request.Resource.Version = "another-version"
				prefix["cleanup/entry-0001-intent.json"] = encode(forged)
				if _, err = isolation.InspectContinuation(prefix); err == nil {
					t.Fatal("unapproved partial mutation version accepted")
				}
			}
		})
	}
}

func TestInspectTransitionsValidatesSuccessorReadsWithoutRestoringPhaseApproval(t *testing.T) {
	for _, failure := range []string{"delete", "release"} {
		t.Run(failure, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			initial, _, err := h.start(t, "original")
			if err != nil {
				t.Fatal(err)
			}
			defer initial.Close()
			prepared, a := transitionPreparation(t, h)
			path := filepath.Join(h.root, "transitions")
			live, err := isolation.StartTransitions(t.Context(), initial, prepared, a, path)
			if err != nil {
				t.Fatal(err)
			}
			defer live.Close()
			h.target.mu.Lock()
			for _, r := range h.target.resources {
				if r["kind"] == "appointment" {
					r["attributes"] = map[string]any{"status": "moved"}
					r["version"] = "successor"
				}
			}
			h.target.mu.Unlock()
			if err = live.Accept(t.Context(), "reschedule", networkaction.Digest([]byte("passed-phase"))); err != nil {
				t.Fatal(err)
			}
			h.target.mu.Lock()
			h.target.failDelete = failure == "delete"
			h.target.failRelease = failure == "release"
			h.target.mu.Unlock()
			if live.Cleanup(t.Context()) == nil {
				t.Fatal("fixture failed to interrupt cleanup")
			}
			files := transitionFiles(t, path)
			if _, exists := files["result.json"]; exists {
				t.Fatal("fixture closed transitions")
			}
			expected := isolation.TransitionPolicy{Schema: isolation.TransitionPolicySchema, ParentPlan: h.options.ParentPlan, Phases: []isolation.Transition{{Phase: "reschedule", Alias: "booking", Attributes: map[string]string{"status": "moved"}}}}
			before := encode(files)
			h.target.mu.Lock()
			requests := h.target.requests
			h.target.mu.Unlock()
			result, err := isolation.InspectTransitions(files, expected)
			if err != nil || result.Cleanup != "uncertain" || result.Setup != "interrupted" || result.Complete || len(result.Manual) != 0 {
				t.Fatal(result, err)
			}
			if result.Resources[1].Version != "successor" || result.Resources[1].Attributes["status"] != "moved" || result.Entries[0].Request.Resource.Version != "successor" {
				t.Fatal("approved observed successor or mutation request lost", result)
			}
			if failure == "release" && (len(result.Entries) != 3 || result.Entries[2].Action != "lease-release" || result.Entries[2].State != "uncertain") {
				t.Fatal("release uncertainty hidden", result)
			}
			if !bytes.Equal(before, encode(files)) {
				t.Fatal("inspection rewrote evidence")
			}
			h.target.mu.Lock()
			if h.target.requests != requests {
				t.Fatal("inspection contacted adapter")
			}
			h.target.mu.Unlock()
			expected.Phases[0].Attributes["status"] = "unapproved"
			if _, err = isolation.InspectTransitions(files, expected); err == nil {
				t.Fatal("retained state substituted for pinned policy")
			}
			if _, err = isolation.VerifyTransitions(files); err == nil {
				t.Fatal("unsealed transition became complete")
			}
		})
	}
}
func TestInspectWrappersRecognizesCompletedCleanupWithoutOuterCompletion(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	initial, previous, err := h.start(t, "original")
	if err != nil {
		t.Fatal(err)
	}
	initial.Close()
	a := continuationAuthorities(t, h, previous)
	path := filepath.Join(h.root, "continued")
	live, err := isolation.Continue(t.Context(), h.p, a, previous, path, isolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if err = live.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	files := transitionFiles(t, path)
	delete(files, "identity.sha256")
	delete(files, "result.json")
	result, err := isolation.InspectContinuation(files)
	if err != nil || result.Cleanup != "complete" || result.Setup != "interrupted" || result.Complete || result.Lease != (isolation.Lease{}) {
		t.Fatal(result, err)
	}
	delete(files, "previous/plan.json")
	if _, err = isolation.InspectContinuation(files); err == nil {
		t.Fatal("partially copied prior setup was trusted")
	}
}
func inspectFixtureClone(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for name, raw := range files {
		out[name] = bytes.Clone(raw)
	}
	return out
}

func TestInspectContinuationDoesNotRestoreRecordedManualClaims(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.contract.Manual = []isolation.ManualStep{{ID: "notifications", Instructions: "Confirm notifications are disabled for this attempt"}}
	h.prepare(t)
	preflight := h.preflight(t, "preflight")
	previous := filepath.Join(h.root, "original")
	original, err := isolation.Start(t.Context(), h.p, h.a, preflight, previous, isolation.Confirmation{Plan: h.p.Identity(), Instance: h.options.Instance, Steps: []string{"notifications"}})
	if err != nil {
		t.Fatal(err)
	}
	original.Close()
	review, err := isolation.ContinuationReview(h.p, previous, "read")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.root, "continuation")
	live, err := isolation.Continue(t.Context(), h.p, continuationAuthorities(t, h, previous), previous, path, isolation.Confirmation{Plan: review.Identity, Instance: h.options.Instance, Steps: []string{"notifications"}})
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if len(live.Result().Manual) != 1 {
		t.Fatal("fixture did not have a current manual claim")
	}
	result, err := isolation.InspectContinuation(transitionFiles(t, path))
	if err != nil || len(result.Manual) != 0 || result.Setup != "interrupted" || result.Complete {
		t.Fatal("inspection reconstructed manual consent", result, err)
	}
}
