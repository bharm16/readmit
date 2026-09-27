package testauthor

import (
	"errors"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/testrunner"
)

// StageCase names the case a draft is bound to. It is not a stage the flow
// asks: a draft is opened over its case, and a problem with it is reported
// there.
const StageCase = "case"

// Problem is one reason a draft cannot be generated, at the stage it is about.
// Index is the position of the expectation a problem is about, and -1 for a
// problem of any other stage or of the expectations as a whole.
type Problem struct {
	Stage   string
	Index   int
	Message string
}

// Problems reports every reason one draft cannot be generated yet, each at
// its stage, in the order the flow asks them. Where Answer and Generate stop
// at the first, this names them all, so an editor that holds a whole draft
// shows every problem at the field it belongs to at once.
//
// The rules are the ones check, Resolve and Generate hold a draft to. The
// case's own messages are checked when source is given, and the target
// against the workspace when root is; neither is opened here. A draft with
// no problems generates.
func Problems(root string, source *bundle.Bundle, draft Draft) []Problem {
	problems := []Problem{}
	add := func(stage string, index int, message string) {
		problems = append(problems, Problem{Stage: stage, Index: index, Message: message})
	}
	missing := func(stage string) {
		for _, rule := range stageRules {
			if rule.name == stage {
				add(stage, -1, rule.unanswered)
			}
		}
	}
	if draft.Schema != Schema {
		add(StageCase, -1, "test draft declares a contract version this release does not read")
		return problems
	}
	switch {
	case artifactpath.EntryName(draft.Case.Entry) != nil || !printable(draft.Case.Entry, MaxEntryBytes) || !identityPattern.MatchString(draft.Case.Identity):
		add(StageCase, -1, "a draft names one entry of the open workspace and the verified identity of the case in it")
	case source != nil && source.Identity != draft.Case.Identity:
		add(StageCase, -1, "this draft was authored against different evidence; reopen the case before answering it")
	}

	switch {
	case draft.Name == "":
		missing(StageName)
	case !printable(draft.Name, MaxNameBytes):
		add(StageName, -1, "a test name is 1 to 256 bytes of printable text")
	}

	switch err := checkMessages(draft.Messages); {
	case len(draft.Messages) == 0:
		missing(StageMessages)
	case err != nil:
		add(StageMessages, -1, err.Error())
	case source != nil:
		if err := sendable(source, draft.Messages); err != nil {
			add(StageMessages, -1, err.Error())
		}
	}

	switch {
	case draft.Target == "":
		missing(StageTarget)
	case artifactpath.EntryName(draft.Target) != nil || !printable(draft.Target, MaxEntryBytes):
		add(StageTarget, -1, "a target is named by one entry of the open workspace")
	case root != "":
		targets, err := Targets(root)
		if err == nil {
			err = chosen(targets, draft.Target)
		}
		if err != nil {
			add(StageTarget, -1, err.Error())
		}
	}

	boundary := draft.Boundary == testrunner.LedgerBoundary || draft.Boundary == testrunner.ACKBoundary
	switch {
	case draft.Boundary == "":
		missing(StageBoundary)
	case !boundary:
		add(StageBoundary, -1, "this release decides a test at the appointment-ledger or the ack-contract boundary")
	}

	switch {
	case draft.Observation != "" && draft.Boundary != testrunner.LedgerBoundary:
		add(StageObservation, -1, "only the appointment-ledger boundary reads an observation document; the ack-contract boundary observes correlated acknowledgements")
	case draft.Observation != "" && (artifactpath.EntryName(draft.Observation) != nil || !printable(draft.Observation, MaxEntryBytes)):
		add(StageObservation, -1, "an observation source is named by one entry of the open workspace")
	case draft.Observation == "" && draft.Boundary == testrunner.LedgerBoundary:
		missing(StageObservation)
	}

	switch {
	case draft.Reset == "":
		missing(StageReset)
	case !printable(draft.Reset, MaxResetBytes):
		add(StageReset, -1, "reset instructions are 1 to 8192 bytes of printable text")
	}

	switch {
	case len(draft.Expectations) == 0:
		missing(StageExpectations)
	case len(draft.Expectations) > MaxExpectations:
		add(StageExpectations, -1, "a test holds at most 256 expectations")
	case !boundary:
		add(StageExpectations, -1, "the outcome boundary decides which expectations a test can make; answer it before them")
	default:
		sent := make(map[string]bool, len(draft.Messages))
		for _, id := range draft.Messages {
			sent[id] = true
		}
		ids := make(map[string]bool, len(draft.Expectations))
		found := len(problems)
		for i, expectation := range draft.Expectations {
			if !expectationPattern.MatchString(expectation.ID) || ids[expectation.ID] {
				add(StageExpectations, i, "an expectation is named once, by lowercase letters, digits and hyphens beginning with a letter")
				continue
			}
			ids[expectation.ID] = true
			if err := checkExpectation(draft, expectation, sent); err != nil {
				add(StageExpectations, i, err.Error())
			}
		}
		if len(problems) == found && !stageRules[len(stageRules)-1].answered(draft) {
			add(StageExpectations, -1, unanswered(draft, StageExpectations).Error())
		}
	}
	return problems
}

// sendable holds selected occurrences to the case: each is one of its
// messages, never an acknowledgement or an occurrence nothing decoded.
func sendable(source *bundle.Bundle, messages []string) error {
	selected := make(map[string]bool, len(messages))
	for _, id := range messages {
		selected[id] = true
	}
	for _, event := range source.Events {
		if !selected[event.ID] {
			continue
		}
		if event.Kind != bundle.Message {
			return errors.New("a test sends messages; an acknowledgement and an occurrence nothing decoded cannot be sent")
		}
		delete(selected, event.ID)
	}
	if len(selected) != 0 {
		return errors.New("a selected occurrence is not in this case")
	}
	return nil
}

// ACKPositions are the acknowledgement positions an expectation addresses,
// in the order an editor offers them: every MSA and ERR field the shared
// selector reads.
func ACKPositions() []string {
	positions := make([]string, 0, 18)
	for i := 1; i <= 6; i++ {
		positions = append(positions, "MSA-"+strconv.Itoa(i))
	}
	for i := 1; i <= 12; i++ {
		positions = append(positions, "ERR-"+strconv.Itoa(i))
	}
	return positions
}
