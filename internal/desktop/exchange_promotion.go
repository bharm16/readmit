package desktop

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedtest"
)

// newExchangeTestDraft composes a verified retained exchange into the existing
// named draft editor. It copies mechanical input order and context, never an
// output value, consent, runtime marker or assertion.
func (a *App) newExchangeTestDraft(ctx context.Context, c *loadedCatalog, result ItemDraftResult, origin *TestOrigin) ItemDraftResult {
	fail := func(reason string) ItemDraftResult { result.refuse(Failed, reason); return result }
	history := a.listExchanges(ctx, result.Context)
	if history.State != Completed {
		return fail(history.Reason)
	}
	var exchange *ExchangeView
	for i := range history.Exchanges {
		view := &history.Exchanges[i]
		if view.ID == origin.Exchange.ID {
			exchange = view
			break
		}
	}
	if exchange == nil || exchange.Identity != origin.Exchange.Identity {
		return fail("the retained exchange changed or is unavailable; select its exact evidence again")
	}
	if exchange.Inputs == nil || exchange.TargetRef == nil {
		return fail("this legacy exchange did not retain exact source and target references")
	}
	inputs := exchange.Inputs
	if len(inputs.Messages) > maxConnectedSteps {
		return fail("the retained exchange has more ordered inputs than one connected draft supports; nothing was dropped")
	}
	index := c.document.Find(inputs.Case.ID)
	if index < 0 || c.removed(c.document.Items[index]) || c.document.Items[index].Kind != string(inputs.Case.Kind) {
		return fail("the original exchange input is unavailable")
	}
	record := c.document.Items[index]
	_, name, source := c.caseOf(record.Entry)
	if source == nil || source.Identity != inputs.Identity || source.Identity != exchange.InputIdentity {
		return fail("the original exchange input changed; nothing was rebound")
	}
	if origin.Case.ID != inputs.Case.ID || origin.Case.Kind != inputs.Case.Kind || len(origin.Messages) > 0 && !slices.Equal(origin.Messages, inputs.Messages) {
		return fail("promotion selects the exact retained ordered inputs")
	}
	messages := caseMessages(source)
	for _, occurrence := range inputs.Messages {
		if !slices.ContainsFunc(messages, func(message TestMessage) bool { return message.ID == occurrence && message.Sendable }) {
			return fail("a retained selected input is unavailable; nothing was dropped")
		}
	}
	state := "suggested setup; expected behavior is not authored"
	available, err := c.exchangeConfigurationCurrent(exchange)
	if err != nil {
		return fail(err.Error())
	}
	if !available {
		state = "configuration unavailable; complete this draft deliberately"
	}
	provenance := &ExchangeTestProvenance{Origin: *origin.Exchange, Inputs: *inputs, Receiver: exchange.Review.Source, ReceiverIdentity: exchange.Review.SourceIdentity, Target: *exchange.TargetRef, Coverage: exchange.Coverage, Received: exchange.Received, HorizonMS: exchange.Review.Options.HorizonMS, ConfigurationState: state}
	provenance.Inputs.Messages = slices.Clone(inputs.Messages)
	draft := ConnectedTestDraft{Schema: ConnectedTestSchema, Boundary: EngineOutputBoundary, Generation: connectedtest.Generation{Seed: 1, BaseTime: "2026-01-01T00:00:00Z"}, Variables: []ConnectedVariable{}, Steps: []ConnectedStep{}, Phases: []ConnectedPhase{}}
	phase := ConnectedPhase{ID: "phase-1", Name: "Exchange inputs", Steps: []string{}, After: []connectedtest.PhaseDependency{}, Observations: []ConnectedPhaseObservation{}, Checks: []ConnectedCheck{}, Responses: []ConnectedResponseCheck{}, Validations: []ConnectedValidationCheck{}, Acks: []ConnectedAckCheck{}}
	for i, occurrence := range inputs.Messages {
		id := "step-" + strconv.Itoa(i+1)
		step := ConnectedStep{ID: id, After: []string{}, Source: ConnectedSource{Case: inputs.Case, Identity: inputs.Identity, Occurrence: occurrence}, V2: &ConnectedV2{}}
		if i > 0 {
			step.After = []string{draft.Steps[i-1].ID}
		}
		draft.Steps = append(draft.Steps, step)
		phase.Steps = append(phase.Steps, id)
	}
	draft.Phases = append(draft.Phases, phase)
	title := name
	if origin.Title != "" && catalog.ValidName(origin.Title) {
		title = origin.Title
	}
	links := &TestLinks{Schema: TestLinksSchemaV2, Source: &TestSource{Kind: SourceExchange, Exchange: provenance}}
	// The context is a proposal. An execution target and observation are selected
	// by the author in the existing editor rather than restored as authority.
	result.New, result.Ref, result.State = true, &ItemRef{Kind: TestItem}, Completed
	result.Draft = &ItemDraft{Name: title, ConnectedTest: &draft, TestLinks: links}
	result.Test = &TestContext{Case: &inputs.Case, CaseName: name, Messages: messages, Observations: c.testObservations(), Unsupported: []TestClause{}, Proposals: []TestProposal{}, Connected: c.connectedContext(&draft)}
	return result
}

func (c *loadedCatalog) exchangeConfigurationCurrent(exchange *ExchangeView) (bool, error) {
	available := true
	for _, ref := range []ItemRef{exchange.Review.Source, *exchange.TargetRef} {
		index := c.document.Find(ref.ID)
		if index < 0 || c.removed(c.document.Items[index]) {
			available = false
			continue
		}
		record := c.document.Items[index]
		if record.Kind != string(ref.Kind) || record.RevisionLabel() != ref.Revision {
			return false, errors.New("the exchange source or target changed; select a fresh exchange instead of rebinding")
		}
		_, availability, _ := c.backing(record)
		if availability == ItemMissing {
			available = false
		} else if availability != ItemAvailable {
			return false, errors.New("the exchange source or target cannot be verified unchanged; nothing was rebound")
		}
	}
	return available, nil
}
