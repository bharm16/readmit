package connectedtest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
)

var typedResultFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "typed connected result", AllowedDirectories: []string{"datasets"}, Nested: []string{"plan", "datasets"}, RequiredFiles: []string{"result.json", "identity.sha256"}, AllowFile: func(n string) bool { return n == "result.json" || n == "identity.sha256" }, MaxFiles: 8192, MaxFileBytes: 64 << 20, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(DatasetResultSchema)}

// RetainDatasetResult is the orchestration handoff for retaining checked typed
// evidence with its compiled plan. All evaluation and copying are local.
func RetainDatasetResult(ctx context.Context, p *Plan, execution Execution, evidence map[string]*dataset.Snapshot, output string) (DatasetResult, error) {
	result, err := EvaluateDatasets(ctx, p, execution, evidence)
	if err != nil {
		return DatasetResult{}, err
	}
	size := 0
	for name := range result.Report.Evidence {
		size += evidence[name].Document().Material.Size
	}
	if size > p.document.Test.Limits.MaxBytes {
		return DatasetResult{}, invalid
	}
	writer, err := artifactdir.Create(output, typedResultFamily, artifactdir.Durable)
	if err != nil {
		return DatasetResult{}, err
	}
	defer writer.Close()
	if err = p.Write(ctx, filepath.Join(writer.Path(), "plan")); err != nil {
		return DatasetResult{}, err
	}
	if err = writer.Mkdir("datasets"); err != nil {
		return DatasetResult{}, err
	}
	for name := range result.Report.Evidence {
		if err = evidence[name].Write(ctx, filepath.Join(writer.Path(), "datasets", name)); err != nil {
			return DatasetResult{}, err
		}
	}
	raw, err := json.Marshal(result, json.Deterministic(true))
	if err != nil {
		return DatasetResult{}, err
	}
	if err = writer.WriteFile("result.json", raw); err != nil {
		return DatasetResult{}, err
	}
	if _, err = writer.Seal(nil); err != nil {
		return DatasetResult{}, err
	}
	return OpenDatasetResult(ctx, writer.Path())
}
func OpenDatasetResult(ctx context.Context, directory string) (DatasetResult, error) {
	files, err := artifactdir.Read(directory, typedResultFamily.Layout)
	if err != nil {
		return DatasetResult{}, err
	}
	return verifyDatasetResult(ctx, files)
}

// VerifyDatasetResult reevaluates the captured plan and captured datasets.
func VerifyDatasetResult(ctx context.Context, captured map[string][]byte) (DatasetResult, error) {
	files, err := artifactdir.Snapshot(captured, typedResultFamily.Layout)
	if err != nil {
		return DatasetResult{}, err
	}
	return verifyDatasetResult(ctx, files)
}
func verifyDatasetResult(ctx context.Context, files map[string][]byte) (DatasetResult, error) {
	if !sealed(DatasetResultSchema, files) {
		return DatasetResult{}, invalid
	}
	var retained DatasetResult
	if json.Unmarshal(files["result.json"], &retained, json.RejectUnknownMembers(true)) != nil || retained.Schema != DatasetResultSchema {
		return DatasetResult{}, invalid
	}
	plan, err := VerifyPlan(artifactdir.Subtree(files, "plan"))
	if err != nil {
		return DatasetResult{}, err
	}
	set, err := assertion.DecodeDatasets(plan.files["dependencies/"+plan.document.Test.Checks.SHA256])
	if err != nil {
		return DatasetResult{}, err
	}
	snapshots := map[string]*dataset.Snapshot{}
	for _, binding := range set.Document().Bindings {
		snapshot, err := dataset.Verify(ctx, artifactdir.Subtree(files, "datasets/"+binding.Name))
		if err != nil {
			return DatasetResult{}, err
		}
		snapshots[binding.Name] = snapshot
	}
	for name := range files {
		if strings.HasPrefix(name, "datasets/") {
			parts := strings.Split(name, "/")
			if len(parts) != 3 || snapshots[parts[1]] == nil {
				return DatasetResult{}, invalid
			}
		}
	}
	result, err := EvaluateDatasets(ctx, plan, retained.Execution, snapshots)
	if err != nil {
		return DatasetResult{}, err
	}
	raw, _ := json.Marshal(result, json.Deterministic(true))
	if !bytes.Equal(raw, files["result.json"]) {
		return DatasetResult{}, invalid
	}
	return result, nil
}
