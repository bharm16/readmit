package diagnose

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
)

// maxErrorPosition bounds each position a declared error location may name, so
// an acknowledgement cannot ask for an unbounded selector.
const maxErrorPosition = 999

// segmentIdentifier is the segment part of the shared selector grammar. An error
// location that does not name a segment this way is never turned into a field
// reference, so no acknowledgement text reaches a report through one.
var segmentIdentifier = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)

// ackStageHypotheses fixes the prose of the acknowledgement-stage rule. Each
// summary names the stage's own code vocabulary, the window it is bounded by,
// and the control-identifier assumption the match rests on.
var ackStageHypotheses = map[string]string{
	collection.AcceptStage:      "The header of this occurrence asks unconditionally for an accept acknowledgement, and no acknowledgement carrying an accept-stage MSA-1 (CA, CE or CR) and echoing this control identifier was found in the observed case window. An uncaptured acknowledgement may exist; acknowledgements are matched to occurrences by the echoed control identifier inside this case alone.",
	collection.ApplicationStage: "The header of this occurrence asks unconditionally for an application acknowledgement, and no acknowledgement carrying an application-stage MSA-1 (AA, AE or AR) and echoing this control identifier was found in the observed case window. An uncaptured acknowledgement may exist; acknowledgements are matched to occurrences by the echoed control identifier inside this case alone.",
}

// stageKey is one acknowledgement stage observed for one control identifier.
type stageKey struct{ controlID, stage string }

// requested names the header field one stage is asked for in.
var requested = []struct{ path, stage string }{
	{"MSH-15", collection.AcceptStage},
	{"MSH-16", collection.ApplicationStage},
}

// acknowledgements evaluates the two rules that read an acknowledgement against
// the occurrence it answers: a stage the header asked for and the capture does
// not contain, and the location an ERR gives inside the acknowledged occurrence.
//
// Both passes run after every occurrence has been read, so capture order never
// decides whether an acknowledgement is linked. The link itself is the control
// identifier the acknowledgement echoes, compared inside this case only.
func (e *evaluator) acknowledgements(requests *grouped[string, message], acks []message) {
	if !e.linksAcknowledgements() {
		return
	}
	observed := make(map[stageKey]bool)
	for _, m := range acks {
		controlID, stage, ok := e.acknowledgedStage(m)
		if !ok {
			continue
		}
		acknowledged := requests.values[controlID]
		switch {
		case len(acknowledged) > 1:
			e.unsupportedItem("ambiguous_acknowledged_occurrence", m.event.ID, "MSA-2", "More than one captured occurrence carries the acknowledged control identifier; the acknowledgement was not linked to any of them.")
			continue
		case len(acknowledged) == 0:
			e.unsupportedItem("acknowledged_occurrence_not_observed", m.event.ID, "MSA-2", "No interpreted occurrence in this case carries the acknowledged control identifier; the acknowledgement was not linked.")
			continue
		}
		observed[stageKey{controlID, stage}] = true
		e.errorLocations(m, acknowledged[0])
	}
	e.stagesNotObserved(requests, observed)
}

// acknowledgedStage reads the one MSA an acknowledgement is interpreted through.
// An acknowledgement carrying none, or more than one, references no single
// occurrence; the two stage vocabularies decide which stage its code belongs to
// and are never merged, so a commit acceptance is never read as an application
// outcome.
func (e *evaluator) acknowledgedStage(m message) (string, string, bool) {
	// An acknowledgement past the bounded decoder is already reported as
	// unsupported_ack_cardinality, and it is not read here either: a message
	// readmit declared undecodable never satisfies a stage somebody asked for.
	if m.segmentCount("MSA") > maxACKSegments || m.segmentCount("ERR") > maxACKSegments {
		return "", "", false
	}
	switch count := m.segmentCount("MSA"); {
	case count == 0:
		e.unsupportedItem("unsupported_acknowledgement_reference", m.event.ID, "MSA-2", "The acknowledgement contains no MSA segment; it was not linked to an occurrence.")
		return "", "", false
	case count > 1:
		e.unsupportedItem("unsupported_acknowledgement_reference", m.event.ID, "MSA-2", "The acknowledgement contains more than one MSA segment; it was not linked to an occurrence.")
		return "", "", false
	}
	code, codeOK := e.text(m, "MSA-1")
	stage := ""
	switch {
	case collection.StageCode(collection.AcceptStage, code):
		stage = collection.AcceptStage
	case collection.StageCode(collection.ApplicationStage, code):
		stage = collection.ApplicationStage
	}
	if !codeOK || stage == "" {
		e.unsupportedItem("unsupported_acknowledgement_stage", m.event.ID, "MSA-1", "MSA-1 is missing or belongs to neither the accept nor the application acknowledgement vocabulary; no stage was recorded.")
		return "", "", false
	}
	controlID, ok := e.text(m, "MSA-2")
	if !ok || controlID == "" {
		e.unsupportedItem("unsupported_acknowledgement_reference", m.event.ID, "MSA-2", "The acknowledgement echoes no control identifier; it was not linked to an occurrence.")
		return "", "", false
	}
	return controlID, stage, true
}

// stagesNotObserved reports a stage an occurrence's own header asks for
// unconditionally and the capture does not contain. A conditional request is
// answered only on a processing outcome the capture cannot establish, so it is
// recorded as unevaluated rather than treated as met or unmet.
func (e *evaluator) stagesNotObserved(requests *grouped[string, message], observed map[stageKey]bool) {
	if !e.rules[ACKStageNotObserved] {
		return
	}
	for _, controlID := range requests.keys {
		asking := requests.values[controlID]
		if len(asking) > 1 {
			// An occurrence that asks for no stage needs no acknowledgement to
			// be attributable to it, so a repeated control identifier is only
			// reported where it stops a declared request being evaluated.
			for _, ambiguous := range asking {
				if e.asksForAStage(ambiguous) {
					e.unsupportedItem("ambiguous_acknowledged_occurrence", ambiguous.event.ID, "MSH-10", "More than one captured occurrence carries this control identifier; the acknowledgement stages it asks for were not evaluated.")
				}
			}
			continue
		}
		m := asking[0]
		for _, asked := range requested {
			v, supported := e.value(m, asked.path)
			if !supported {
				continue
			}
			// An absent or empty condition is original acknowledgement mode,
			// which asks for no stage in its header. readmit does not infer a
			// request from that silence.
			if v.State == hl7.Omitted || v.State == hl7.Empty {
				continue
			}
			if v.State == hl7.Null {
				e.unsupportedItem("unsupported_acknowledgement_condition", m.event.ID, asked.path, "An explicit-null acknowledgement condition is not AL, NE, ER or SU; the stage it asks for was not evaluated.")
				continue
			}
			condition, ok := e.text(m, asked.path)
			switch {
			case !ok:
			case condition == collection.Never:
			case condition == collection.OnError || condition == collection.OnSuccess:
				e.unsupportedItem("conditional_acknowledgement_request", m.event.ID, asked.path, "The header asks for this acknowledgement stage only on a processing outcome; the capture does not establish that outcome, so the stage was not evaluated.")
			case condition == collection.Always:
				if observed[stageKey{controlID, asked.stage}] {
					continue
				}
				e.finding(ACKStageNotObserved, "hypothesis", ackStageHypotheses[asked.stage], e.report.Window.Description, m.evidence("MSH-10"), m.evidence(asked.path))
			default:
				e.unsupportedItem("unsupported_acknowledgement_condition", m.event.ID, asked.path, "The declared acknowledgement condition is not AL, NE, ER or SU; the stage it asks for was not evaluated.")
			}
		}
	}
}

// asksForAStage reports whether an occurrence's own header declares a condition
// that would have been evaluated. Original acknowledgement mode declares none,
// and a declared "never" asks for nothing, so neither needs an acknowledgement
// to be attributable to this occurrence.
func (e *evaluator) asksForAStage(m message) bool {
	for _, asked := range requested {
		v, supported := e.value(m, asked.path)
		if !supported || v.State == hl7.Omitted || v.State == hl7.Empty {
			continue
		}
		if condition, ok := e.text(m, asked.path); !ok || condition != collection.Never {
			return true
		}
	}
	return false
}

// errorLocation is one ERL an acknowledgement declares, as positions of the
// shared selector grammar. Segment occurrence and field repetition default to
// one; a component and a subcomponent are optional, and zero means the
// declaration named none. A field position is not optional.
type errorLocation struct {
	segment                                              string
	sequence, field, repetition, component, subcomponent int
}

// selector spells the declared location in the shared grammar.
func (l errorLocation) selector() string {
	path := fmt.Sprintf("%s[%d]-%d[%d]", l.segment, l.sequence, l.field, l.repetition)
	if l.component != 0 {
		path += "." + strconv.Itoa(l.component)
		if l.subcomponent != 0 {
			path += "." + strconv.Itoa(l.subcomponent)
		}
	}
	return path
}

// errorLocations reports where an acknowledgement says its error is, inside the
// occurrence it acknowledges. It reports the state of that field and never
// claims the field is at fault or copies either message's values.
func (e *evaluator) errorLocations(m, acknowledged message) {
	if !e.rules[ACKErrorLocation] {
		return
	}
	// The ERR count is inside the bounded decoder: an acknowledgement past it
	// was never linked to this occurrence.
	count := m.segmentCount("ERR")
	for i := 1; i <= count; i++ {
		path := fmt.Sprintf("ERR[%d]-2", i)
		v, supported := e.value(m, path)
		if !supported {
			continue
		}
		// An ERR that declares no location is complete evidence of an error
		// without one; only a declared location that cannot be read is
		// unsupported.
		if v.State == hl7.Omitted || v.State == hl7.Empty {
			continue
		}
		location, ok := e.declaredLocation(m, path)
		if !ok {
			e.unsupportedItem("unsupported_error_location", m.event.ID, path, "The declared error location is not a segment identifier with positive positions this profile can address; it was not resolved.")
			continue
		}
		located := location.selector()
		state := acknowledged.value(located).State
		e.finding(ACKErrorLocation, "observed_fact", fmt.Sprintf("The captured acknowledgement locates an error at %s of the occurrence whose control identifier it echoes; that field is %s there. This reports where the acknowledging system says the error is, not that the field is wrong, and the two occurrences are matched by the echoed control identifier inside this case alone.", located, state), e.findingWindow, m.evidence("MSA-2"), m.evidence(path), acknowledged.evidence(located))
	}
}

// declaredLocation reads one ERL structurally. Every position is decoded as a
// bounded positive integer and the result must parse as the shared selector
// grammar, so no acknowledgement text reaches the report through a reference.
func (e *evaluator) declaredLocation(m message, base string) (errorLocation, bool) {
	location := errorLocation{sequence: 1, repetition: 1}
	segment, ok := e.text(m, base+".1")
	if !ok || !segmentIdentifier.MatchString(segment) {
		return errorLocation{}, false
	}
	location.segment = segment
	for _, part := range []struct {
		component string
		into      *int
	}{
		{".2", &location.sequence},
		{".3", &location.field},
		{".4", &location.repetition},
		{".5", &location.component},
		{".6", &location.subcomponent},
	} {
		v, supported := e.value(m, base+part.component)
		if !supported {
			return errorLocation{}, false
		}
		if v.State == hl7.Omitted || v.State == hl7.Empty {
			continue
		}
		text, ok := e.text(m, base+part.component)
		if !ok {
			return errorLocation{}, false
		}
		position, err := strconv.Atoi(text)
		if err != nil || position < 1 || position > maxErrorPosition {
			return errorLocation{}, false
		}
		*part.into = position
	}
	// A location without a field position addresses no field, and a
	// subcomponent without its component addresses a different one.
	if location.field == 0 || location.component == 0 && location.subcomponent != 0 {
		return errorLocation{}, false
	}
	if _, err := hl7.ParseSelector(location.selector()); err != nil {
		return errorLocation{}, false
	}
	return location, true
}

func (e *evaluator) linksAcknowledgements() bool {
	return e.rules[ACKStageNotObserved] || e.rules[ACKErrorLocation]
}
