package testisolation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	isolation "github.com/bharm16/readmit/internal/testisolation"
)

func transitionPreparation(t *testing.T, h *harness) (*isolation.TransitionPrepared, isolation.Authorities) {
	t.Helper()
	p, err := isolation.PrepareTransitions(h.p, isolation.TransitionPolicy{Schema: isolation.TransitionPolicySchema, ParentPlan: h.options.ParentPlan, Phases: []isolation.Transition{{Phase: "reschedule", Alias: "booking", Attributes: map[string]string{"status": "moved"}}}})
	if err != nil {
		t.Fatal(err)
	}
	grant := func(phase string) networkaction.Authority {
		review, err := p.Review(phase)
		if err != nil {
			t.Fatal(err)
		}
		return approval{binding: review.Binding, actor: networkaction.Actor{Kind: "runner", ID: "runner", Generation: "transitions", EvidenceIdentity: networkaction.Digest([]byte("policy-" + phase)), Expires: time.Now().Add(time.Hour)}, revoked: h.revoked}
	}
	return p, isolation.Authorities{Read: grant("read"), Cleanup: grant("cleanup")}
}
func TestIsolationTransitionsAdoptOnlyAuthoredSuccessorsAndGuardCleanup(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	live, _, err := h.start(t, "initial")
	if err != nil {
		t.Fatal(err)
	}
	p, a := transitionPreparation(t, h)
	if s, err := isolation.StartTransitions(t.Context(), live, p, h.a, filepath.Join(h.root, "old-grants")); err == nil || s != nil {
		t.Fatal("old grants authorized mutable successor policy")
	}
	output := filepath.Join(h.root, "transitions")
	s, err := isolation.StartTransitions(t.Context(), live, p, a, output)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if live.Ready() || !s.Ready() {
		t.Fatal("ownership was not transferred to explicit policy")
	}
	h.target.mu.Lock()
	for _, resource := range h.target.resources {
		if resource["kind"] == "appointment" {
			resource["attributes"] = map[string]any{"status": "moved"}
			resource["version"] = "successor"
		}
	}
	h.target.mu.Unlock()
	proof := networkaction.Digest([]byte("complete-passed-reschedule-phase"))
	if err = s.Accept(t.Context(), "reschedule", proof); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := isolation.OpenTransitions(output)
	if err != nil || result.Cleanup != "complete" {
		t.Fatal(result, err)
	}
	if _, err = isolation.Open(filepath.Join(output, "effects")); err == nil {
		t.Fatal("new transition effects entered frozen v1 reader")
	}
	files := transitionFiles(t, output)
	proofs, err := isolation.TransitionProofs(files)
	if err != nil || proofs["reschedule"] != proof {
		t.Fatal(proofs, err)
	}
	if strings.Join(h.target.effects, ",") != "create:patient,create:appointment,delete:appointment,delete:patient" || len(h.target.resources) != 0 {
		t.Fatal("setup repeated or guarded cleanup missed successor", h.target.effects)
	}
}
func TestIsolationTransitionsRefuseUnapprovedMutationAndOutOfOrderProof(t *testing.T) {
	for _, variant := range []string{"foreign-owner", "undeclared-attribute", "unchanged-version", "wrong-phase", "changed-reference", "changed-business-id"} {
		t.Run(variant, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			live, _, err := h.start(t, "initial")
			if err != nil {
				t.Fatal(err)
			}
			defer live.Close()
			p, a := transitionPreparation(t, h)
			s, err := isolation.StartTransitions(t.Context(), live, p, a, filepath.Join(h.root, "transitions"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			h.target.mu.Lock()
			for _, resource := range h.target.resources {
				if resource["kind"] == "appointment" {
					resource["attributes"] = map[string]any{"status": "moved"}
					if variant != "unchanged-version" {
						resource["version"] = "successor"
					}
					switch variant {
					case "foreign-owner":
						resource["owner"] = networkaction.Digest([]byte("other"))
					case "undeclared-attribute":
						resource["attributes"] = map[string]any{"status": "moved", "unreviewed": "yes"}
					case "changed-reference":
						resource["references"] = []any{}
					case "changed-business-id":
						resource["identifiers"] = []any{}
					}
				}
			}
			h.target.mu.Unlock()
			phase := "reschedule"
			if variant == "wrong-phase" {
				phase = "unknown"
			}
			if s.Accept(t.Context(), phase, networkaction.Digest([]byte("passed"))) == nil {
				t.Fatal("unapproved successor accepted")
			}
			if s.Cleanup(t.Context()) == nil {
				t.Fatal("unapproved changed resource deleted")
			}
			if len(h.target.effects) != 2 {
				t.Fatal("refusal caused writes")
			}
		})
	}
}
func transitionFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	if err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		name, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}
