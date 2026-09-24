package drift

import (
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/runresult"
)

// record is one side's retained document as the raw comparison sees it: what
// state this build could read it in, and the digest of the bytes themselves.
type record struct {
	state       string
	fingerprint string
}

// scope is the sentence every report carries about what it is not. It is
// written into the document rather than left to documentation, because the
// report is the thing that gets pasted into a ticket.
const scope = "Drift names which of four retained records differ: the input, the target configuration, the engine that evaluated it, and the profile it named. It is not a field comparison and does not replace one; which fields differ stays in readmit-diff/v1, whose raw comparison is preserved whatever this says. A cause nothing retained is undeclared and a cause this evidence cannot settle is undecided; neither is a statement that it did not change. No outcome here is a verdict, and no drift is a claim that a result moved because of it."

// Compare opens two artifact directories and reports which of the four causes
// drifted between them.
//
// Each side is read through the verified reader for what it is, so a manifest
// that no longer describes the evidence beside it is refused rather than
// compared. Nothing is written, no network connection is opened, and neither
// artifact is changed by producing the report.
func Compare(left, right string) (Report, error) {
	if left == "" || right == "" {
		return Report{}, errors.New("a drift comparison names two artifact directories")
	}
	l, err := openArtifact(left)
	if err != nil {
		return Report{}, err
	}
	r, err := openArtifact(right)
	if err != nil {
		return Report{}, err
	}
	return CompareOpened(l, r), nil
}

// CompareOpened reports which of the four causes drifted between two
// artifacts already opened by runresult, so a caller holding the evidence it
// verified compares exactly that evidence rather than opening it again.
func CompareOpened(left, right *runresult.Evidence) Report {
	l, r := fromEvidence(left), fromEvidence(right)
	report := Report{Schema: Schema, Scope: scope, Left: l.report, Right: r.report, Drift: []Drift{
		compareInput(l, r),
		compareTarget(l, r),
		compareEnvironment(l, r),
		compareRule(l, r),
	}}
	report.Attribution = attribute(report.Drift)
	return report
}

// nothingToCompare answers the part of every cause that is the same: whether
// there is anything to compare at all. Two sides that retained nothing leave
// the cause undeclared; one side that retained it and one that did not leaves
// it undecided, because a record compared against an absence establishes
// nothing about either. It reports false when both sides retained something
// and the cause is the caller's to settle.
func nothingToCompare(cause, left, right string) (Drift, bool) {
	switch {
	case left == Undeclared && right == Undeclared:
		return Drift{Cause: cause, Outcome: Undeclared, Comparison: NotCompared, Parts: []string{}}, true
	case left == Undeclared || right == Undeclared:
		return Drift{Cause: cause, Outcome: Undecided, Comparison: NotCompared, Parts: []string{}, Reason: DeclaredOnOneSide}, true
	}
	return Drift{}, false
}

// settle turns the parts of a record that differ into an outcome. Every caller
// reaching it has read both records as the contracts they declare, so the
// comparison is always semantic here.
func settle(cause string, parts []string) Drift {
	outcome := Unchanged
	if len(parts) > 0 {
		outcome = Changed
	}
	return Drift{Cause: cause, Outcome: outcome, Comparison: Semantic, Parts: parts}
}

// compareInput compares the evidence that went in: the case each side names as
// its source, the replay operators declared over it, and how many field
// changes those operators actually made.
//
// A declared operator's own parameter is part of it, so two runs that shift
// dates by different amounts are input drift even though both name the same
// operator. The parameter is compared and is not reported: the report names
// `transformations` as the part that differs and shows each side's operator
// names, which is what `explain` already prints of a run.
func compareInput(left, right *side) Drift {
	l, r := left.report.Input, right.report.Input
	if drift, settled := nothingToCompare(InputCause, l.State, r.State); settled {
		return drift
	}
	parts := []string{}
	if l.Identity != r.Identity {
		parts = append(parts, "source")
	}
	if !slices.Equal(left.transformations, right.transformations) {
		parts = append(parts, "transformations")
	}
	if l.RecordedChanges != r.RecordedChanges {
		parts = append(parts, "recorded_changes")
	}
	return settle(InputCause, parts)
}

// compareTarget compares the retained target configuration part by part.
//
// Every part is named and no part is shown. `message_timeout` is one of them
// and is target drift rather than environment drift on purpose: it bounds one
// message and its acknowledgement on the network, so changing it changes what
// the target was given, not what evaluated the answer.
func compareTarget(left, right *side) Drift {
	l, r := left.report.Target, right.report.Target
	if drift, settled := nothingToCompare(TargetCause, l.State, r.State); settled {
		return drift
	}
	a, b := *left.target, *right.target
	parts := []string{}
	for _, part := range []struct {
		name string
		same bool
	}{
		{"address", a.Address == b.Address},
		{"transport", a.Transport == b.Transport},
		{"test_endpoint", a.TestEndpoint == b.TestEndpoint},
		{"approved_transport", a.ApprovedTransport == b.ApprovedTransport},
		{"ca_sha256", a.CASHA256 == b.CASHA256},
		{"connect_timeout", a.ConnectTimeout == b.ConnectTimeout},
		{"message_timeout", a.MessageTimeout == b.MessageTimeout},
		{"max_ack_bytes", a.MaxACKBytes == b.MaxACKBytes},
	} {
		if !part.same {
			parts = append(parts, part.name)
		}
	}
	return settle(TargetCause, parts)
}

// compareEnvironment compares what evaluated each side: the engine build and
// the spec contract that build read.
//
// The profile the same pin names is deliberately not here. Which rules were
// named and which build applied them are two different questions, and a report
// that answered them together could not tell a rebuilt evaluator apart from a
// changed rule.
func compareEnvironment(left, right *side) Drift {
	l, r := left.report.Environment, right.report.Environment
	if drift, settled := nothingToCompare(EnvironmentCause, l.State, r.State); settled {
		return drift
	}
	if drift, settled := rawFallback(EnvironmentCause, record{l.State, l.Fingerprint}, record{r.State, r.Fingerprint}); settled {
		return drift
	}
	parts := []string{}
	if l.Engine != r.Engine {
		parts = append(parts, "engine")
	}
	if l.Spec != r.Spec {
		parts = append(parts, "spec")
	}
	return settle(EnvironmentCause, parts)
}

// compareRule compares the profile each side's pin named, and refuses to call
// two identities equal rules unless this build holds what they name.
//
// Two differing identities are drift whether or not either resolves: the pin
// says the evaluation was held to a differently named contract, and that is a
// recorded fact rather than an inference. Two equal identities are only
// unchanged when both resolve here. This release bundles no profile library,
// so anything but the profile this build implements leaves the question open —
// which is the answer, not a gap in one.
func compareRule(left, right *side) Drift {
	l, r := left.report.Rule, right.report.Rule
	if drift, settled := nothingToCompare(RuleCause, l.State, r.State); settled {
		return drift
	}
	if drift, settled := rawFallback(RuleCause, record{l.State, l.Fingerprint}, record{r.State, r.Fingerprint}); settled {
		return drift
	}
	if l.Profile != r.Profile {
		return settle(RuleCause, []string{"profile"})
	}
	if l.Resolution != BundledProfile || r.Resolution != BundledProfile {
		return Drift{Cause: RuleCause, Outcome: Undecided, Comparison: Semantic, Parts: []string{}, Reason: ProfileUnresolved}
	}
	return settle(RuleCause, []string{})
}

// rawFallback is what is left when a retained record cannot be read here.
//
// The document's digest still answers one question honestly: two byte-identical
// pins declare the same build, the same spec contract and the same profile,
// whatever else they declare, so the cause is unchanged and the report says the
// comparison was raw. Two differing documents settle nothing, because one pin
// carries both the environment and the rule and the difference could be in
// either. That is undecided, and it is not folded into a change of one of them.
func rawFallback(cause string, left, right record) (Drift, bool) {
	if left.state != Unreadable && right.state != Unreadable {
		return Drift{}, false
	}
	if left.fingerprint == right.fingerprint {
		return Drift{Cause: cause, Outcome: Unchanged, Comparison: RawDocument, Parts: []string{}}, true
	}
	return Drift{Cause: cause, Outcome: Undecided, Comparison: RawDocument, Parts: []string{}, Reason: RecordUnreadable}, true
}

// attribute states what the four outcomes together support, and stops there.
//
// One changed cause beside three settled ones is the only case that names a
// single cause, and even that names a cause that drifted rather than a reason
// a verdict moved. Anything left undecided or undeclared makes the whole
// attribution undecided: a comparison that cannot say whether the target
// changed cannot say the input is why anything else did.
func attribute(drifts []Drift) Attribution {
	attribution := Attribution{Changed: []string{}, Unresolved: []string{}}
	for _, drift := range drifts {
		switch drift.Outcome {
		case Changed:
			attribution.Changed = append(attribution.Changed, drift.Cause)
		case Undecided, Undeclared:
			attribution.Unresolved = append(attribution.Unresolved, drift.Cause)
		}
	}
	switch {
	case len(attribution.Unresolved) > 0:
		attribution.Outcome = Undecided
	case len(attribution.Changed) == 0:
		attribution.Outcome = NoDeclaredChange
	case len(attribution.Changed) == 1:
		attribution.Outcome = SingleCause
	default:
		attribution.Outcome = SeveralCauses
	}
	return attribution
}
