package suite_test

import (
	"context"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/suite"
)

func resealConnectedTree(t *testing.T, path, schema string) {
	t.Helper()
	files := map[string][]byte{}
	if err := filepath.WalkDir(path, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		relative, e := filepath.Rel(path, name)
		if e != nil {
			return e
		}
		raw, e := os.ReadFile(name)
		if e != nil {
			return e
		}
		files[filepath.ToSlash(relative)] = raw
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(artifactdir.Identity(schema, files)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConnectedSuiteRetainedPromotionRejectsAResealedSubstitutionWithTheSameInputs(t *testing.T) {
	h, path := connectedSuiteFixture(t)
	review, e := suite.ReviewConnectedPromotion(path, "qa", "independent-lab")
	if e != nil {
		t.Fatal(e)
	}
	promotionPath := filepath.Join(h.Root, "promotion.json")
	promotion, e := suite.ApproveConnectedPromotion(path, "qa", "independent-lab", review.Identity(), "Operator", "Exact private environment approval", promotionPath)
	if e != nil {
		t.Fatal(e)
	}
	request := suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(h.Root, "prepared-approved"), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: "independent-lab"}
	prepared, e := suite.PrepareConnected(request)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	output := filepath.Join(h.Root, "refused-execution")
	actual, e := suite.ExecuteConnected(ctx, prepared, "cancelled-occurrence", output, nil)
	if e != nil || actual.ExitCode() != 2 || len(actual.Jobs) != 1 || h.Lab.Creates.Load() != 0 {
		t.Fatalf("refused denominator/proof: %+v %v", actual, e)
	}
	if _, e = suite.OpenConnectedExecution(t.Context(), output); e != nil {
		t.Fatal("unaltered refusal proof cannot reopen", e)
	}
	// Even a structurally valid replacement approval over identical inputs
	// cannot replace the exact approval identity actually used by the runner.
	replacement := promotion
	replacement.Approver = "Different operator"
	replacement.Rationale = "Different approval record"
	raw, e := json.Marshal(replacement, json.Deterministic(true))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = suite.DecodeConnectedPromotion(raw); e != nil {
		t.Fatal(e)
	}
	retained := filepath.Join(output, "prepared")
	connectedlab.WriteJSON(t, filepath.Join(retained, "promotion.json"), replacement)
	resealConnectedTree(t, retained, suite.ConnectedPreparedSchema)
	resealConnectedTree(t, output, suite.ConnectedExecutionSchema)
	if _, e = suite.OpenConnectedExecution(t.Context(), output); e == nil {
		t.Fatal("resealed substitute approval changed provenance without refusal")
	}
}
