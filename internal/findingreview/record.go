package findingreview

import (
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/testauthor"
)

// Reviewed is the diagnosis a review is made over: the report, the exact bytes
// it was read from, the verified case it was run over, and the entry of the
// workspace that case is named by — which is how a promoted test names the
// evidence it sends.
type Reviewed struct {
	Report   diagnose.Report
	Identity string
	Case     *bundle.Bundle
	Entry    string
}

// Review joins one diagnosis and one person's judgment of it.
//
// The two documents stay separate and are bound by identity rather than by
// name: the decisions name the report they were read against, and the report
// names the case it was run over, so a judgment cannot be applied to findings
// it was not made about. Every decision must name a finding the report holds;
// a decision about a finding nobody produced is refused rather than ignored.
//
// A finding nobody decided is not_reviewed, and only a finding a person
// confirmed is promoted. There is no accept-all and no default acceptance.
func Review(reviewed Reviewed, decisions Decisions, decisionsIdentity string) (Record, error) {
	if !identityPattern.MatchString(reviewed.Identity) || !identityPattern.MatchString(decisionsIdentity) {
		return Record{}, errors.New("a review names the exact diagnosis report and decisions it was built from")
	}
	if decisions.Report != reviewed.Identity {
		return Record{}, errors.New("these decisions were recorded against a different diagnosis report; finding identifiers name other findings there")
	}
	if reviewed.Case == nil || reviewed.Case.Identity != reviewed.Report.CaseIdentity {
		return Record{}, errors.New("this diagnosis was run over different evidence; open the case the report names")
	}
	// The draft a promotion answers is opened once, here, so a name that could
	// never hold a test is an argument this review refuses rather than a
	// per-finding outcome reported once for every confirmation in it.
	base, err := testauthor.NewDraft(reviewed.Entry, reviewed.Case.Identity)
	if err != nil {
		return Record{}, errors.New("a review names the case by one entry of the workspace holding it, because a promoted test names the evidence it sends by that entry")
	}
	if base, err = base.Answer(testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: Boundary}); err != nil {
		return Record{}, err
	}
	decided := make(map[string]Decision, len(decisions.Decisions))
	findings := make(map[string]diagnose.Finding, len(reviewed.Report.Findings))
	for _, finding := range reviewed.Report.Findings {
		findings[finding.ID] = finding
	}
	for _, decision := range decisions.Decisions {
		if _, ok := findings[decision.Finding]; !ok {
			return Record{}, errors.New("a decision names a finding this diagnosis report does not hold")
		}
		decided[decision.Finding] = decision
	}
	record := Record{
		Schema: Schema,
		Engine: engine.Version(),
		Diagnosis: Provenance{
			Schema:       diagnose.Schema,
			Report:       reviewed.Identity,
			CaseIdentity: reviewed.Report.CaseIdentity,
			ConfigSHA256: reviewed.Report.ConfigSHA256,
			Profile:      reviewed.Report.Profile,
			Ruleset:      reviewed.Report.Ruleset,
		},
		Decisions: decisionsIdentity,
		Boundary:  Boundary,
		Findings:  make([]Status, 0, len(reviewed.Report.Findings)),
		Statement: Statement,
	}
	for _, finding := range reviewed.Report.Findings {
		status := Status{Finding: finding.ID, RuleID: finding.RuleID, Classification: finding.Classification, Verdict: NotReviewed, Basis: BasisUnreviewed, NextEvidence: nextEvidence(finding)}
		if decision, ok := decided[finding.ID]; ok {
			status.Verdict, status.Basis, status.Scope, status.Rationale = decision.Verdict, BasisDecision, decision.Scope, decision.Rationale
		} else if covering, found := suppression(decisions.Decisions, findings, finding); found {
			status.Verdict, status.Basis = Suppressed, BasisScope
			status.Scope, status.SuppressedBy, status.Rationale = covering.Scope, covering.Finding, covering.Rationale
		}
		if status.Verdict == Confirmed && status.Basis == BasisDecision {
			promotion := promote(reviewed.Case, finding, base)
			status.Promotion = &promotion
		}
		record.Findings = append(record.Findings, status)
	}
	return record, nil
}

// suppression reports the first suppression, in the order the decisions
// document records them, whose scope covers a finding nobody decided directly.
//
// A decision recorded about the finding itself always wins over one that
// reaches it through a scope, so a person who looked at this finding is never
// overruled by a judgment made about another one. Every scope is inside the
// case, because the review was made over exactly that evidence.
func suppression(decisions []Decision, findings map[string]diagnose.Finding, finding diagnose.Finding) (Decision, bool) {
	for _, decision := range decisions {
		if decision.Verdict != Suppressed || decision.Scope == ScopeFinding {
			continue
		}
		source, ok := findings[decision.Finding]
		if !ok || source.RuleID != finding.RuleID {
			continue
		}
		if decision.Scope == ScopeCase || sharesOccurrence(source, finding) {
			return decision, true
		}
	}
	return Decision{}, false
}

// sharesOccurrence reports whether two findings reference a common occurrence,
// which is what an occurrence-scoped suppression is bounded to.
func sharesOccurrence(a, b diagnose.Finding) bool {
	for _, reference := range a.Evidence {
		if slices.ContainsFunc(b.Evidence, func(other diagnose.Evidence) bool { return other.Occurrence == reference.Occurrence }) {
			return true
		}
	}
	return false
}
