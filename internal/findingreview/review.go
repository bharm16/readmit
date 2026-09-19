// Package findingreview records what a person decided about the findings one
// diagnosis produced, and derives from a confirmed finding the assertions a
// regression test draft would hold.
//
// A finding is a hypothesis about evidence; a test is a commitment. Promotion
// is the moment one becomes the other, so it is never automatic: a finding is
// promoted only where a person confirmed it, and what a person decided lives in
// its own document. [diagnose]'s readmit-diagnosis/v1 report is read and never
// written, gains no member and changes no byte ([ADR-0003]); the judgment is
// readmit-finding-decisions/v1, and the two joined are
// readmit-finding-review/v1.
//
// Nothing here authors a test. The assertions a promotion reports are answered
// into [testauthor]'s own draft, through the same Answer a person answering the
// flow by hand gives, so every refusal that flow makes is made again here.
//
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
package findingreview

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The two contract names this package owns. The judgment a person recorded and
// the record that joins it to the machine's findings are separate documents,
// because machine evidence and human judgment are separate things: a report can
// be produced again from the same bytes, and a decision cannot.
const (
	DecisionsSchema = "readmit-finding-decisions/v1"
	Schema          = "readmit-finding-review/v1"
)

// The verdicts a person may record. There is no "accepted by default": a
// finding nobody decided is NotReviewed, which is not a decision and never
// promotes anything.
const (
	NotReviewed = "not_reviewed"
	Confirmed   = "confirmed"
	Dismissed   = "dismissed"
	Suppressed  = "suppressed"
)

// The scopes a suppression may be recorded at. Every one of them is inside the
// case the diagnosis was run over, because a review is made over exactly that
// evidence and cannot speak for a case nobody read.
const (
	ScopeFinding    = "finding"
	ScopeOccurrence = "occurrence"
	ScopeCase       = "case"
)

// How a finding came by its verdict: nobody decided it, a person decided that
// finding, or a suppression recorded on another finding covers it.
const (
	BasisUnreviewed = "unreviewed"
	BasisDecision   = "decision"
	BasisScope      = "scope"
)

// Bounds. A document past one of these is refused, never truncated.
const (
	MaxDecisionsBytes = 1 << 20
	MaxReportBytes    = 16 << 20
	MaxDecisions      = 4096
	MaxRationaleBytes = 1024
)

var (
	findingPattern  = regexp.MustCompile(`^f[0-9]{6}$`)
	identityPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Statement is what a review is and is not. It is carried in the record for the
// same reason a diagnosis carries its scope: a document that will be read later
// says what it claims without anyone having to remember.
const Statement = "A review records what a person decided about findings that were already produced. It adds no evidence, changes no finding, and is not proof that the capture was complete or correct. A promoted assertion states what a future run should produce; it does not establish that the captured evidence was right."

// Decision is one person's judgment about one finding. The rationale is
// required for every verdict, including a confirmation: recording a judgment
// without saying why is how a review becomes a rubber stamp.
type Decision struct {
	Finding   string `json:"finding"`
	Verdict   string `json:"verdict"`
	Scope     string `json:"scope,omitzero"`
	Rationale string `json:"rationale"`
}

// Decisions is what one analyst recorded about one diagnosis report.
//
// It names the report it was read against by identity, so the same file cannot
// later be applied to a different diagnosis whose finding identifiers mean
// other things.
type Decisions struct {
	Schema    string     `json:"schema"`
	Report    string     `json:"report_sha256"`
	Decisions []Decision `json:"decisions"`
}

// Boundary is the outcome boundary every promotion is drafted at. It is not a
// choice: a diagnosis reads a captured case and observes no appointment ledger,
// so the only expectation it can support is one about an acknowledgement, and
// readmit-test/v1 decides those at the ack-contract boundary. Offering the
// other one would produce a draft the authoring flow could never generate.
const Boundary = testrunner.ACKBoundary

// ParseDecisions reads a judgment document. Unknown members, unknown versions,
// a verdict this release does not record and a scope wider than the case are
// errors; there is no migration and no repair.
func ParseDecisions(data []byte) (Decisions, error) {
	if len(data) > MaxDecisionsBytes {
		return Decisions{}, errors.New("finding decisions exceed their size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Decisions{}, errors.New("invalid finding decisions")
	}
	if declared.Schema != DecisionsSchema {
		return Decisions{}, errors.New("finding decisions declare a contract version this release does not read")
	}
	var present struct {
		Report    *string `json:"report_sha256"`
		Decisions *[]struct {
			Finding   *string `json:"finding"`
			Verdict   *string `json:"verdict"`
			Rationale *string `json:"rationale"`
		} `json:"decisions"`
	}
	if json.Unmarshal(data, &present) != nil {
		return Decisions{}, errors.New("invalid finding decisions")
	}
	if present.Report == nil || present.Decisions == nil {
		return Decisions{}, errors.New("finding decisions name the diagnosis report they were read against and the decisions themselves")
	}
	for _, decision := range *present.Decisions {
		if decision.Finding == nil || decision.Verdict == nil || decision.Rationale == nil {
			return Decisions{}, errors.New("a decision names the finding it is about, the verdict, and the reason for it")
		}
	}
	var decisions Decisions
	if json.Unmarshal(data, &decisions, json.RejectUnknownMembers(true)) != nil {
		return Decisions{}, errors.New("invalid finding decisions")
	}
	if err := decisions.validate(); err != nil {
		return Decisions{}, err
	}
	return decisions, nil
}

func (d Decisions) validate() error {
	if !identityPattern.MatchString(d.Report) {
		return errors.New("finding decisions name the diagnosis report by its SHA-256")
	}
	if len(d.Decisions) > MaxDecisions {
		return errors.New("a review records at most 4096 decisions")
	}
	seen := make(map[string]bool, len(d.Decisions))
	for _, decision := range d.Decisions {
		if !findingPattern.MatchString(decision.Finding) || seen[decision.Finding] {
			return errors.New("a decision names one finding of the report, decided once")
		}
		seen[decision.Finding] = true
		switch decision.Verdict {
		case Confirmed, Dismissed:
			if decision.Scope != "" {
				return errors.New("a scope is what a suppression is bounded by; a confirmation and a dismissal are about one finding")
			}
		case Suppressed:
			if decision.Scope != ScopeFinding && decision.Scope != ScopeOccurrence && decision.Scope != ScopeCase {
				return errors.New("a suppression is scoped to this finding, to its occurrences, or to this case; a review speaks only for the evidence it was made over")
			}
		default:
			return errors.New("a decision confirms, dismisses or suppresses a finding; leaving it undecided is recording no decision at all")
		}
		if !printable(decision.Rationale, MaxRationaleBytes) {
			return errors.New("a decision states its reason in 1 to 1024 bytes of printable text")
		}
	}
	return nil
}

// Provenance is which diagnosis the judgment was made over. It carries
// identities and contract names, never a path and never a value, so a review
// can be checked against the report and the case it names without either of
// them being copied into it.
type Provenance struct {
	Schema       string `json:"schema"`
	Report       string `json:"report_sha256"`
	CaseIdentity string `json:"case_identity"`
	ConfigSHA256 string `json:"config_sha256"`
	Profile      string `json:"profile"`
	Ruleset      string `json:"ruleset"`
}

// Status is one finding as the review leaves it: what the machine found, what a
// person decided, how it came by that verdict, what evidence would settle it,
// and what it promotes to.
type Status struct {
	Finding        string     `json:"finding"`
	RuleID         string     `json:"rule_id"`
	Classification string     `json:"classification"`
	Verdict        string     `json:"verdict"`
	Basis          string     `json:"basis"`
	Scope          string     `json:"scope,omitzero"`
	SuppressedBy   string     `json:"suppressed_by,omitzero"`
	Rationale      string     `json:"rationale,omitzero"`
	NextEvidence   string     `json:"next_evidence"`
	Promotion      *Promotion `json:"promotion,omitzero"`
}

// Record is the review: the machine's findings and one person's judgment of
// them, joined but still distinguishable, bound to the exact documents both
// were read from.
type Record struct {
	Schema    string     `json:"schema"`
	Engine    string     `json:"engine"`
	Diagnosis Provenance `json:"diagnosis"`
	Decisions string     `json:"decisions_sha256"`
	Boundary  string     `json:"boundary"`
	Findings  []Status   `json:"findings"`
	Statement string     `json:"statement"`
}

// printable accepts bounded text a person typed. Control characters are refused
// rather than escaped, so nothing a decision carries can rewrite a line it is
// displayed on.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

// ParseReport reads the diagnosis a review is made over. It is the same strict
// reading every other reader of a versioned document does; readmit-diagnosis/v1
// gains no member here and changes no byte.
func ParseReport(data []byte) (diagnose.Report, error) {
	if len(data) > MaxReportBytes {
		return diagnose.Report{}, errors.New("diagnosis report exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return diagnose.Report{}, errors.New("invalid diagnosis report")
	}
	if declared.Schema != diagnose.Schema {
		return diagnose.Report{}, errors.New("diagnosis report declares a contract version this release does not read")
	}
	var report diagnose.Report
	if json.Unmarshal(data, &report, json.RejectUnknownMembers(true)) != nil {
		return diagnose.Report{}, errors.New("invalid diagnosis report")
	}
	for _, finding := range report.Findings {
		if !findingPattern.MatchString(finding.ID) {
			return diagnose.Report{}, errors.New("a diagnosis report names each finding once, as this release writes them")
		}
	}
	if !identityPattern.MatchString(report.CaseIdentity) {
		return diagnose.Report{}, errors.New("a diagnosis report names the verified identity of the case it was run over")
	}
	return report, nil
}
