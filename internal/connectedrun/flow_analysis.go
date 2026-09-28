package connectedrun

import (
	"context"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/engine"
)

const FlowAnalysisSchema = "readmit-connected-reanalysis/v1"

type FlowVerdict struct {
	Engine  string            `json:"engine"`
	Verdict assertion.Verdict `json:"verdict"`
}
type FlowAnalysis struct {
	Schema           string      `json:"schema"`
	OriginalIdentity string      `json:"original_identity"`
	Original         FlowVerdict `json:"original"`
	Reanalysis       FlowVerdict `json:"reanalysis"`
	Result           FlowResult  `json:"result"`
}

// ReanalyzeFlow labels current evaluation separately from the immutable original
// verdict. The fixed-version readers preserve original operator semantics; an
// unsupported or inconsistent historical artifact is refused, never rewritten.
func ReanalyzeFlow(ctx context.Context, path string) (FlowAnalysis, error) {
	files, err := artifactdir.Read(path, flowResultFamily.Layout)
	if err != nil {
		return FlowAnalysis{}, err
	}
	original, err := openFlowFiles(ctx, path, files)
	if err != nil {
		return FlowAnalysis{}, err
	}
	current := original
	summarizeFlow(&current)
	if original.State == "cancelled" && current.Verdict == assertion.VerdictPass {
		current.Verdict = assertion.VerdictUndecided
	}
	return FlowAnalysis{Schema: FlowAnalysisSchema, OriginalIdentity: artifactdir.Identity(original.Schema, files), Original: FlowVerdict{Engine: original.Engine, Verdict: original.Verdict}, Reanalysis: FlowVerdict{Engine: engine.Version(), Verdict: current.Verdict}, Result: original}, nil
}
