package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/runcompare"
)

// compareConnectedRunItems uses the same verified retained lifecycle snapshots
// the CLI comparison consumes. The caller supplies roles; no target, current
// build or authored definition is reconstructed to compare an old result.
func (a *App) compareConnectedRunItems(ctx context.Context, request RunComparisonItemsRequest, loaded *loadedCatalog) (RunComparisonItemsResult, bool) {
	result := RunComparisonItemsResult{Context: request.Context}
	paths := []string{}
	sides := []RunComparisonSide{}
	flow := false
	for i, ref := range request.Runs {
		window, index, declined := a.findAcross(ctx, request.Context, loaded, ref.ID, false)
		if window == nil {
			result.refuse(declined.state, declined.reason)
			return result, true
		}
		if index < 0 || ref.Kind != RunItem {
			result.refuse(Failed, "select distinct retained runs of this project")
			return result, true
		}
		record := window.document.Items[index]
		path := filepath.Join(window.root, record.Entry)
		connected := connectedIndividualArtifact(path)
		if i == 0 {
			flow = connected
			if !flow {
				return result, false
			}
		}
		if !connected {
			result.refuse(Failed, "connected Before/After compares two retained connected lifecycles; these execution contracts differ")
			return result, true
		}
		if len(request.Runs) != 2 || i == 1 && ref.ID == request.Runs[0].ID {
			result.refuse(Failed, "connected Before/After needs exactly two distinct retained results")
			return result, true
		}
		item := window.read(record)
		summary := item.Summary.Run
		if summary == nil || item.Availability != ItemAvailable || summary.Active {
			result.refuse(Failed, "the connected result is unavailable or still being written")
			return result, true
		}
		paths = append(paths, path)
		sides = append(sides, RunComparisonSide{Run: item.Ref, Name: item.Name, Version: summary.Version, EnvironmentName: summary.EnvironmentName, StartedAt: summary.StartedAt, Result: summary.Result})
	}
	if !flow {
		return result, false
	}
	left, err := connectedrun.OpenFlowEvidence(ctx, paths[0])
	if err != nil {
		result.refuse(Failed, "the Before result cannot be verified as a complete retained snapshot")
		return result, true
	}
	right, err := connectedrun.OpenFlowEvidence(ctx, paths[1])
	if err != nil {
		result.refuse(Failed, "the After result cannot be verified as a complete retained snapshot")
		return result, true
	}
	compared, err := runcompare.CompareFlowEvidence(ctx, left, right)
	if err != nil {
		result.refuse(Failed, "the retained connected evidence cannot be compared under these contracts")
		return result, true
	}
	result.State = Completed
	result.Comparison = &RunComparisonView{Connected: &compared, Earlier: sides[0], Later: sides[1], Repeats: []RunComparisonSide{}, Checks: []RunCheckComparison{}, Configuration: []RunConfigurationPart{}}
	return result, true
}

// Associations name the original archived publication when known. A removed
// object remains Missing; unrelated current content cannot invent a test link.
func (c *loadedCatalog) runAssociations(summary *RunSummary) {
	summary.TestAssociation = "unlinked"
	if summary.Kind == SendRunKind {
		summary.TestAssociation = "not-applicable"
	} else if summary.Test != nil {
		summary.TestAssociation = "linked"
		i := c.document.Find(summary.Test.ID)
		if i < 0 || c.removed(c.document.Items[i]) {
			summary.TestAssociation = "missing"
		} else if _, availability, _ := c.revisionBacking(c.document.Items[i], summary.Test.Revision); availability != ItemAvailable {
			summary.TestAssociation = "missing"
		}
	}
	if len(summary.SourceCases) == 0 && summary.SourceCase != nil {
		summary.SourceCases = []ItemRef{*summary.SourceCase}
	}
	summary.SourceAssociation = "unlinked"
	if len(summary.SourceCases) > 0 {
		summary.SourceAssociation = "linked"
		for _, ref := range summary.SourceCases {
			i := c.document.Find(ref.ID)
			if i < 0 || c.removed(c.document.Items[i]) {
				summary.SourceAssociation = "missing"
				continue
			}
			if summary.SourceName != "" {
				summary.SourceName += " · "
			}
			summary.SourceName += c.document.Items[i].Name
		}
	}
}
func (c *loadedCatalog) connectedRunAssociations(summary *RunSummary) {
	if len(summary.SourceCases) == 0 && summary.Test != nil {
		// v1 origins can still recover exact pins from a readable archived test.
		index := c.document.Find(summary.Test.ID)
		if index >= 0 {
			if saved, err := c.connectedTestOf(c.document.Items[index], summary.Test.Revision); err == nil {
				seen := map[string]bool{}
				for _, step := range saved.draft.Steps {
					ref := step.Source.Case
					if !seen[ref.ID] {
						seen[ref.ID] = true
						summary.SourceCases = append(summary.SourceCases, ref)
					}
				}
			}
		}
	}
	if len(summary.SourceCases) == 1 {
		summary.SourceCase = &summary.SourceCases[0]
	}
	c.runAssociations(summary)
}
