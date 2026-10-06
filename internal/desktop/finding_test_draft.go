package desktop

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/findingreview"
)

// findingTestOrigin resolves input scope from retained finding evidence. A
// diagnostic fact supplies a reason to test, never an invented passing value.
func (c *loadedCatalog) findingTestOrigin(origin TestOrigin, source *bundle.Bundle) (TestOrigin, error) {
	provenance := origin.Source
	analysis := c.analysisByReport(provenance.ReportSHA256)
	if analysis == nil {
		return origin, errors.New("the finding's original analysis is unavailable")
	}
	opened, problem := c.reviewedAnalysis(*analysis, provenance.ReportSHA256)
	if opened == nil {
		return origin, errors.New(problem.Problem)
	}
	if opened.source.Identity != source.Identity {
		return origin, errors.New("the finding belongs to different source evidence")
	}
	review := c.reviewFor(provenance.ReportSHA256)
	if review == nil || review.ID != provenance.Review {
		return origin, errors.New("the finding's saved confirmation is unavailable")
	}
	record := c.document.Items[c.document.Find(review.ID)]
	if len(record.Revisions) == 0 {
		return origin, errors.New("the finding has no saved confirmation")
	}
	decisions, err := c.savedDecisions(record.Revisions[len(record.Revisions)-1])
	if err != nil {
		return origin, err
	}
	_, reviewed, err := opened.review(decisions.Decisions)
	if err != nil {
		return origin, err
	}
	var status *findingreview.Status
	for i := range reviewed.Findings {
		if reviewed.Findings[i].Finding == provenance.Finding {
			status = &reviewed.Findings[i]
			break
		}
	}
	if status == nil || status.Verdict != findingreview.Confirmed {
		return origin, errors.New("confirm this finding before creating its test draft")
	}
	inputs := map[string]bool{}
	for _, finding := range opened.retained.Report.Findings {
		if finding.ID == provenance.Finding {
			for _, evidence := range finding.Evidence {
				inputs[evidence.Occurrence] = true
			}
		}
	}
	origin.Messages, origin.Proposals = []string{}, []TestProposal{}
	if status.Promotion != nil {
		for _, id := range status.Promotion.Messages {
			inputs[id] = true
		}
		for i, check := range status.Promotion.Expectations {
			origin.Proposals = append(origin.Proposals, TestProposal{ID: fmt.Sprintf("finding-%d", i+1), Source: ProposalFromFinding, Check: check})
		}
	}
	for _, event := range source.Events {
		if inputs[event.ID] && event.Kind == bundle.Message {
			origin.Messages = append(origin.Messages, event.ID)
		}
	}
	if len(origin.Messages) == 0 {
		return origin, errors.New("this finding has no linked sendable message; select its inputs in Messages to create a test")
	}
	return origin, nil
}
