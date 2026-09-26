package scenario

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"time"
)

// OrderSchema adds order identities and result repetitions without widening
// readmit-scenario/v1. The original reader and profiles remain unchanged.
const OrderSchema = "readmit-order-scenario/v1"

const (
	// ORMLifecycle designs order creation, update and cancellation requests.
	ORMLifecycle ProfileName = "readmit-orm-lifecycle-v1"
	// ORULifecycle designs preliminary, final and corrected result reports.
	ORULifecycle ProfileName = "readmit-oru-lifecycle-v1"
	// OrderSubject is a patient-linked order with separate placer/filler names.
	OrderSubject Kind = "order"
)

var orderProfiles = map[ProfileName]profile{
	ORMLifecycle: {
		states: map[Kind]map[State]bool{PatientSubject: {PatientActive: true}, OrderSubject: {"none": true, "ordered": true, "cancelled": true}},
		events: map[Event]transition{
			"ORM-NW": {kind: OrderSubject, description: "new order request", from: []State{"none"}, to: "ordered"},
			"ORM-XO": {kind: OrderSubject, description: "change order request", from: []State{"ordered"}, to: "ordered"},
			"ORM-CA": {kind: OrderSubject, description: "cancel order request", from: []State{"ordered"}, to: "cancelled"},
		},
	},
	ORULifecycle: {
		states: map[Kind]map[State]bool{PatientSubject: {PatientActive: true}, OrderSubject: {"ordered": true, "preliminary": true, "final": true, "corrected": true}},
		events: map[Event]transition{
			"ORU-P": {kind: OrderSubject, description: "preliminary result report", from: []State{"ordered", "preliminary"}, to: "preliminary"},
			"ORU-F": {kind: OrderSubject, description: "final result report", from: []State{"ordered", "preliminary"}, to: "final"},
			"ORU-C": {kind: OrderSubject, description: "corrected result report", from: []State{"final", "corrected"}, to: "corrected"},
		},
	},
}

// OrderIdentifier is an identifier scoped by its assigning namespace.
type OrderIdentifier struct {
	Namespace  string `json:"namespace"`
	Identifier string `json:"identifier"`
}

// UnmarshalJSON checks presence before the strict decoder, including nulls.
func (i *OrderIdentifier) UnmarshalJSON(data []byte) error {
	var required struct {
		Namespace  *string `json:"namespace"`
		Identifier *string `json:"identifier"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Namespace == nil || required.Identifier == nil {
		return errors.New("an order identifier requires namespace and identifier")
	}
	type wire OrderIdentifier
	var decoded wire
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid order identifier members")
	}
	*i = OrderIdentifier(decoded)
	return nil
}

// Order binds the two independently scoped identifiers to one scenario subject.
// These bindings are immutable throughout the sequence, including corrections.
type Order struct {
	Subject string          `json:"subject"`
	Placer  OrderIdentifier `json:"placer"`
	Filler  OrderIdentifier `json:"filler"`
}

// UnmarshalJSON refuses absent or null bindings and unknown members.
func (o *Order) UnmarshalJSON(data []byte) error {
	var required struct {
		Subject *string          `json:"subject"`
		Placer  *OrderIdentifier `json:"placer"`
		Filler  *OrderIdentifier `json:"filler"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Subject == nil || required.Placer == nil || required.Filler == nil {
		return errors.New("an order requires subject, placer and filler")
	}
	type wire Order
	var decoded wire
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid order binding members")
	}
	*o = Order(decoded)
	return nil
}

// Observation is one repeated OBX-like value. Code plus sub-id identifies the
// observation within this result; position preserves the authored repetition.
// Only explicit textual fixture values are supported, never clinical typing.
type Observation struct {
	Code   string `json:"code"`
	SubID  string `json:"sub_id"`
	Value  string `json:"value"`
	Status string `json:"status"`
}

// UnmarshalJSON preserves an explicit empty value and refuses absent/null values.
func (o *Observation) UnmarshalJSON(data []byte) error {
	var required struct {
		Code   *string `json:"code"`
		SubID  *string `json:"sub_id"`
		Value  *string `json:"value"`
		Status *string `json:"status"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Code == nil || required.SubID == nil || required.Value == nil || required.Status == nil {
		return errors.New("an observation requires code, sub_id, value and status")
	}
	type wire Observation
	var decoded wire
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation members")
	}
	*o = Observation(decoded)
	return nil
}

// Result is the ordered set of observations one result-report step carries.
type Result struct {
	Step         string        `json:"step"`
	Observations []Observation `json:"observations"`
}

// UnmarshalJSON refuses absent/null result members and unknown members.
func (r *Result) UnmarshalJSON(data []byte) error {
	var required struct {
		Step         *string        `json:"step"`
		Observations *[]Observation `json:"observations"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Step == nil || required.Observations == nil {
		return errors.New("a result requires step and observations")
	}
	type wire Result
	var decoded wire
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid result members")
	}
	*r = Result(decoded)
	return nil
}

// OrderScenario is an editable order or result template. It carries no script,
// evidence, message bytes, network target or inferred default.
type OrderScenario struct {
	Schema   string      `json:"schema"`
	Scenario Identity    `json:"scenario"`
	Profile  ProfileName `json:"profile"`
	BaseTime time.Time   `json:"base_time"`
	Subjects []Subject   `json:"subjects"`
	Steps    []Step      `json:"steps"`
	Orders   []Order     `json:"orders"`
	Results  []Result    `json:"results"`
}

func (d OrderScenario) sequence() Scenario {
	return Scenario{Scenario: d.Scenario, Profile: d.Profile, BaseTime: d.BaseTime, Subjects: d.Subjects, Steps: d.Steps}
}

// DecodeOrders accepts only the separate order contract, checking every link,
// bound and repetition before any lifecycle is evaluated.
func DecodeOrders(data []byte) (OrderScenario, error) {
	if len(data) > MaxBytes {
		return OrderScenario{}, errors.New("an order scenario exceeds its size limit")
	}
	var required struct {
		Orders  *[]Order  `json:"orders"`
		Results *[]Result `json:"results"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Orders == nil || required.Results == nil {
		return OrderScenario{}, errors.New("an order scenario requires orders and results arrays")
	}
	var d OrderScenario
	if err := json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
		return OrderScenario{}, errors.New("invalid order scenario JSON")
	}
	if _, err := validateOrders(d); err != nil {
		return OrderScenario{}, err
	}
	return d, nil
}

func validateOrders(d OrderScenario) (profile, error) {
	if d.Schema != OrderSchema {
		return profile{}, errors.New("an order scenario must declare " + OrderSchema)
	}
	p, ok := orderProfiles[d.Profile]
	if !ok {
		return profile{}, errors.New("an order scenario requires readmit-orm-lifecycle-v1 or readmit-oru-lifecycle-v1")
	}
	if err := name(d.Scenario.ID, "a scenario id"); err != nil {
		return profile{}, err
	}
	if err := version(d.Scenario.Version); err != nil {
		return profile{}, err
	}
	if err := baseTime(d.BaseTime); err != nil {
		return profile{}, err
	}
	base := d.sequence()
	subjects, err := declaredSubjects(base, p)
	if err != nil {
		return profile{}, err
	}
	if err := declaredSteps(base, p, subjects); err != nil {
		return profile{}, err
	}
	if err := orderBindings(d.Orders, subjects); err != nil {
		return profile{}, err
	}
	if err := resultBindings(d); err != nil {
		return profile{}, err
	}
	return p, nil
}

func orderBindings(orders []Order, subjects map[string]Subject) error {
	if len(orders) == 0 || len(orders) > maxSubjects {
		return errors.New("an order scenario requires between 1 and 32 order bindings")
	}
	seen := map[string]bool{}
	placer, filler := map[OrderIdentifier]bool{}, map[OrderIdentifier]bool{}
	for _, order := range orders {
		if subjects[order.Subject].Kind != OrderSubject || seen[order.Subject] {
			return errors.New("each order binds one distinct declared order subject")
		}
		seen[order.Subject] = true
		for _, id := range []OrderIdentifier{order.Placer, order.Filler} {
			if err := value(id.Namespace, "an order namespace"); err != nil {
				return err
			}
			if err := value(id.Identifier, "an order identifier"); err != nil {
				return err
			}
		}
		if placer[order.Placer] || filler[order.Filler] {
			return errors.New("two order subjects share a placer or filler identity")
		}
		placer[order.Placer], filler[order.Filler] = true, true
	}
	for id, subject := range subjects {
		if subject.Kind == OrderSubject && !seen[id] {
			return errors.New("every order subject requires a placer and filler binding")
		}
	}
	return nil
}

func resultBindings(d OrderScenario) error {
	steps := make(map[string]Step, len(d.Steps))
	for _, step := range d.Steps {
		steps[step.ID] = step
	}
	seen := map[string]bool{}
	if len(d.Results) > maxSteps {
		return errors.New("an order scenario carries at most 64 results")
	}
	for _, result := range d.Results {
		step, exists := steps[result.Step]
		if d.Profile != ORULifecycle || !exists || seen[result.Step] {
			return errors.New("each result binds one distinct ORU step")
		}
		seen[result.Step] = true
		if len(result.Observations) == 0 || len(result.Observations) > 32 {
			return errors.New("a result requires between 1 and 32 observations")
		}
		observed := map[[2]string]bool{}
		for _, observation := range result.Observations {
			if err := value(observation.Code, "an observation code"); err != nil {
				return err
			}
			if err := value(observation.SubID, "an observation sub-id"); err != nil {
				return err
			}
			key := [2]string{observation.Code, observation.SubID}
			if observed[key] {
				return errors.New("a result repeats an observation code and sub-id")
			}
			observed[key] = true
			if observation.Status != strings.TrimPrefix(string(step.Event), "ORU-") {
				return errors.New("observation status must match its result event")
			}
			if len(observation.Value) > 256 {
				return errors.New("an observation value exceeds 256 bytes")
			}
			for _, c := range observation.Value {
				if c < 0x20 || c > 0x7e || strings.ContainsRune("|^~\\&", c) {
					return errors.New("an observation value requires printable ASCII without HL7 delimiters")
				}
			}
		}
	}
	if d.Profile == ORULifecycle && len(seen) != len(d.Steps) {
		return errors.New("every ORU step requires its observations, including refused steps")
	}
	return nil
}

// PreviewOrders validates even programmatically constructed templates, then
// walks the same typed transition engine as the original scenario reader.
// Identifiers and observation values never enter the displayed occurrences.
func PreviewOrders(d OrderScenario) (Timeline, error) {
	p, err := validateOrders(d)
	if err != nil {
		return Timeline{}, err
	}
	return preview(d.sequence(), p)
}

// Workflow is one designed workflow of either contract, read once from the
// contract name its document declares. Every workflow is the sequence of
// steps over linked subjects both contracts share; a workflow of the order
// contract additionally binds placer and filler identities to its order
// subjects and repeats textual observations on its ORU steps. Those members
// belong to the order contract alone: a sequence document carrying them is
// refused by the sequence reader, and a document of neither contract is
// refused as neither, never read as whichever contract would take it.
type Workflow struct {
	Scenario
	Orders  []Order  `json:"orders,omitzero"`
	Results []Result `json:"results,omitzero"`
}

// DecodeDocument reads one designed workflow of either contract, dispatching
// on the contract name the document declares. Each reader retains its own
// strict vocabulary, so the contract a Workflow follows is the one its bytes
// declared; a caller reads the members of the other through the same value
// without ever decoding a document permissively.
func DecodeDocument(data []byte) (Workflow, error) {
	if len(data) > MaxBytes {
		return Workflow{}, errors.New("a scenario exceeds its size limit")
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Workflow{}, errors.New("invalid scenario JSON")
	}
	switch header.Schema {
	case OrderSchema:
		orders, err := DecodeOrders(data)
		if err != nil {
			return Workflow{}, err
		}
		return Workflow{
			Scenario: Scenario{Schema: orders.Schema, Scenario: orders.Scenario, Profile: orders.Profile,
				BaseTime: orders.BaseTime, Subjects: orders.Subjects, Steps: orders.Steps},
			Orders: orders.Orders, Results: orders.Results,
		}, nil
	case Schema:
		designed, err := Decode(data)
		if err != nil {
			return Workflow{}, err
		}
		return Workflow{Scenario: designed}, nil
	default:
		return Workflow{}, errors.New("a scenario must declare " + Schema + " or " + OrderSchema)
	}
}

// Identity is what this workflow is referred to by, whichever contract
// declared it.
func (w Workflow) Identity() Identity { return w.Scenario.Scenario }

// Preview walks the sequence through its profile's typed transition
// operators, exactly as the contract the document declared previews it.
func (w Workflow) Preview() (Timeline, error) {
	if w.Schema == OrderSchema {
		return PreviewOrders(OrderScenario{
			Schema: w.Schema, Scenario: w.Scenario.Scenario, Profile: w.Profile, BaseTime: w.BaseTime,
			Subjects: w.Subjects, Steps: w.Steps, Orders: w.Orders, Results: w.Results,
		})
	}
	return Preview(w.Scenario)
}

// PreviewDocument dispatches by contract name, never by a permissive union of
// fields. Each reader retains its own strict vocabulary.
func PreviewDocument(data []byte) (Timeline, error) {
	workflow, err := DecodeDocument(data)
	if err != nil {
		return Timeline{}, err
	}
	return workflow.Preview()
}
