package hub_test

import (
	"context"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

// An import checksum establishes byte integrity, not that a copied aggregate
// accurately describes coverage. The passive public reader must derive that
// statement from the retained policy and actual independently witnessed run.
func TestConnectedCIImportCannotClaimCoverageWithoutItsPolicy(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, config, request, authority := provisionConnectedSuite(t, f)
	var result suite.CIReport
	err := operationguard.New(testlicense.New(t)).Run(t.Context(), operationguard.Profile{Name: "runner", Execution: operationguard.ExecuteEachJob}, func(ctx context.Context) error {
		result = customerrunner.RunConnectedCI(ctx, config, request, authority, "")
		return nil
	})
	if err != nil || result.ExitCode != 0 || h.Lab.Creates.Load() != 2 {
		t.Fatalf("real CI fixture did not pass: %+v %v target writes=%d", result, err, h.Lab.Creates.Load())
	}
	if _, err := customerrunner.InspectConnectedCI(t.Context(), request.Output); err != nil {
		t.Fatal("unaltered real CI proof did not open", err)
	}
	manifestPath := filepath.Join(request.Output, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema    string         `json:"schema"`
		Execution string         `json:"execution"`
		Summary   suite.CIReport `json:"summary"`
	}
	if err := json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	result.Coverage, manifest.Summary.Coverage = "passed", "passed"
	for path, document := range map[string]any{manifestPath: manifest, filepath.Join(request.Output, "ci.json"): result} {
		bytes, err := json.Marshal(document, json.Deterministic(true))
		if err != nil || os.WriteFile(path, bytes, 0600) != nil {
			t.Fatal("could not prepare the imported aggregate mutation")
		}
	}
	// The purported imported packet is internally sealed and retains the
	// genuine passing execution; its unsupported coverage claim is the sole
	// contradiction. No job records, target outcomes or evaluator are mocked.
	files := map[string][]byte{}
	err = filepath.WalkDir(request.Output, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(request.Output, path)
		if err != nil {
			return err
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = bytes
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := artifactdir.Identity(customerrunner.ConnectedCISchema, files)
	if err := os.WriteFile(filepath.Join(request.Output, "identity.sha256"), []byte(identity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := customerrunner.InspectConnectedCI(t.Context(), request.Output); err == nil {
		t.Fatal("imported CI aggregate claimed successful coverage without a retained coverage policy")
	}
}
