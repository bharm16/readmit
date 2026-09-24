package profilepack

import (
	"bytes"
	"errors"

	"github.com/bharm16/readmit/internal/hl7"
)

// Outcome is the one answer a pack gives about one level of one combination.
// Only OutcomeSupported passes; every other outcome is a reason the consumer
// must state rather than a verdict it may report.
type Outcome string

const (
	// OutcomeSupported: the pack declared this level supported for this
	// combination and carries whatever content that level needs.
	OutcomeSupported Outcome = "supported"
	// OutcomeUntested: the pack declared no verification either way.
	OutcomeUntested Outcome = "untested"
	// OutcomeUnsupported: the pack declared this level unsupported.
	OutcomeUnsupported Outcome = "unsupported"
	// OutcomeUnknown: the pack declares nothing about this combination, or
	// the version, family or level is outside the closed sets. Nothing is
	// inferred from a neighbouring version, family or level.
	OutcomeUnknown Outcome = "unknown"
)

// Passing reports whether an outcome may contribute to a passing verdict. It
// is the only place that question is answered, so a consumer cannot treat
// unknown, untested or unsupported as anything but not passing.
func (o Outcome) Passing() bool { return o == OutcomeSupported }

// Combination is one HL7 version and message family, as a message declares
// them in MSH-12 and MSH-9.
type Combination struct {
	Version string
	Family  string
}

// FieldLabel is a pack's answer about one field position. Name is nonempty
// only when Outcome is OutcomeSupported and the pack carries a name for the
// position; a supported combination may still leave a position unlabelled.
type FieldLabel struct {
	Outcome Outcome
	Name    string
}

// Satisfies reports whether this pack is exactly the one a consumer pinned.
// Both members are compared byte for byte; a different version of the same
// pack is a different pack. The caller holds both identities, so the error
// repeats neither.
func (p Pack) Satisfies(pin Identity) error {
	if !p.decoded || p.Identity != pin {
		return errors.New("the selected profile pack is not the pinned pack id and version")
	}
	return nil
}

// Bundleable reports whether this pack may be bundled with a release: its
// publisher recorded an approved rights review of its exact content. It checks
// what was written down; it cannot establish that the review happened or what
// it covered. A pending review is readable and not bundleable, and a pack that
// did not come through Decode is neither.
func (p Pack) Bundleable() error {
	if !p.decoded || p.Provenance.RightsReview.Status != ReviewApproved {
		return errors.New("the pack " + p.Identity.ID + " records no approved rights review, so it is readable and not bundleable")
	}
	return nil
}

// Outcomes is a pack's answer about one combination at all four levels, each
// level answered separately: the one type every consumer that reports all four
// carries, so no consumer declares the levels again.
type Outcomes struct {
	Parse      Outcome `json:"parse"`
	Labels     Outcome `json:"labels"`
	Structural Outcome `json:"structural"`
	Workflow   Outcome `json:"workflow"`
}

// Covered reports whether the pack that answered declares the combination at
// all. A pack declares every level of a combination it covers and none of one
// it does not, so the parse level answers for all four.
func (o Outcomes) Covered() bool {
	return o.Parse == OutcomeSupported || o.Parse == OutcomeUntested || o.Parse == OutcomeUnsupported
}

// Outcomes answers all four levels of one combination at once, each exactly as
// Support answers it alone. Nothing is borrowed between levels: a combination
// the pack does not declare, and a pack that did not come through Decode, are
// unknown at every level.
func (p Pack) Outcomes(version, family string) Outcomes {
	return Outcomes{
		Parse:      p.Support(version, family, LevelParse),
		Labels:     p.Support(version, family, LevelLabels),
		Structural: p.Support(version, family, LevelStructural),
		Workflow:   p.Support(version, family, LevelWorkflow),
	}
}

// outcomes maps what a pack declares onto the answer a consumer receives.
var outcomes = map[Support]Outcome{
	Supported:   OutcomeSupported,
	Untested:    OutcomeUntested,
	Unsupported: OutcomeUnsupported,
}

// Support answers for one level of one combination. A combination the pack
// does not declare, a version, family or level outside the closed sets, and
// a pack that did not come through Decode are each unknown.
func (p Pack) Support(version, family string, level Level) Outcome {
	if !p.decoded {
		return OutcomeUnknown
	}
	for _, coverage := range p.Coverage {
		if coverage.HL7Version != version || coverage.Family != family {
			continue
		}
		var declared Support
		switch level {
		case LevelParse:
			declared = coverage.Parse
		case LevelLabels:
			declared = coverage.Labels
		case LevelStructural:
			declared = coverage.Structural
		case LevelWorkflow:
			declared = coverage.Workflow
		default:
			return OutcomeUnknown
		}
		if outcome, ok := outcomes[declared]; ok {
			return outcome
		}
		return OutcomeUnknown
	}
	return OutcomeUnknown
}

// Label answers for one field position under one combination. The name is
// withheld unless the combination's labels level is supported, so content the
// pack carries for a version cannot reach a family it was not verified for.
func (p Pack) Label(version, family, segment string, position int) FieldLabel {
	label := FieldLabel{Outcome: p.Support(version, family, LevelLabels)}
	if label.Outcome != OutcomeSupported {
		return label
	}
	for _, labels := range p.Labels {
		if labels.HL7Version == version {
			label.Name = labels.Segments[segment][position]
			return label
		}
	}
	return label
}

// Declared reads the combination one parsed message declares: the first
// component of MSH-12 and of MSH-9, as the bytes are written. Nothing is
// normalized and nothing is inferred from the segments present, so a message
// that declares neither, one that does not begin with MSH, or one outside the
// document declares nothing and every question about it is unknown.
func Declared(doc *hl7.Document, messageIndex int) Combination {
	if doc == nil || messageIndex < 0 || messageIndex >= len(doc.Messages) {
		return Combination{}
	}
	message := doc.Messages[messageIndex]
	if len(message.Segments) == 0 || message.Segments[0].ID != "MSH" {
		return Combination{}
	}
	return Combination{
		Version: firstComponent(doc, message, 12),
		Family:  firstComponent(doc, message, 9),
	}
}

func firstComponent(doc *hl7.Document, message hl7.Message, field int) string {
	value := message.Segments[0].Field(field)
	if value.State != hl7.Present || len(value.Repetitions) == 0 {
		return ""
	}
	raw := doc.Bytes(value.Repetitions[0].Span)
	if component := message.Delimiters.Component; component != 0 {
		raw, _, _ = bytes.Cut(raw, []byte{component})
	}
	return string(raw)
}
