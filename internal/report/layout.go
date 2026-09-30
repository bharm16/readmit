package report

import (
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The readable layout every document rendering draws: one ordered list of
// blocks made once from the structured report, so HTML, PDF and Markdown
// state the same verdicts, identities, values and limitations in the same
// order and wording. Every value in a docBlock is already escaped for display
// by Escape; each format adds only its own syntax escaping.

type docBlockKind int

const (
	titleBlock docBlockKind = iota
	subtitleBlock
	headingBlock
	subheadingBlock
	paragraphBlock
	listBlock
	tableBlock
	rawBlock
)

type column struct {
	header string
	// width is the share of the text width a column takes in print.
	width float64
}

type docBlock struct {
	kind    docBlockKind
	text    string
	items   []string
	columns []column
	rows    [][]string
	// id names an appendix entry, which a table cell refers to.
	id string
}

// longValue is the length past which a value is moved to a labelled
// appendix and its cell refers to it, so a table is never a clipped
// horizontal scroll and no byte is dropped.
const longValue = 160

type layout struct {
	blocks   []docBlock
	appendix []docBlock
}

// appendixRef moves a long value to the appendix and answers what its cell
// shows instead.
func (l *layout) appendixRef(label, value string) string {
	if len([]rune(value)) <= longValue {
		return value
	}
	id := "A" + strconv.Itoa(len(l.appendix)/2+1)
	l.appendix = append(l.appendix, docBlock{kind: subheadingBlock, text: id + " · " + label, id: id}, docBlock{kind: rawBlock, text: value})
	return "See appendix " + id
}

func (l *layout) add(b docBlock) { l.blocks = append(l.blocks, b) }

// documentLayout is the report as readable blocks: Result, Checks,
// Comparison when included, Messages, Notes when written, Details, and the
// appendix.
func documentLayout(doc *Document) []docBlock {
	l := &layout{}
	l.add(docBlock{kind: titleBlock, text: Escape(doc.Title)})
	if line := subtitle(doc); line != "" {
		l.add(docBlock{kind: subtitleBlock, text: line})
	}

	l.add(docBlock{kind: headingBlock, text: "Result"})
	l.add(docBlock{kind: paragraphBlock, text: OutcomeLabel(doc.Result.Outcome)})
	for _, fact := range lifecycleFacts(doc.Result) {
		l.add(docBlock{kind: paragraphBlock, text: fact})
	}

	names := messageNames(doc.Messages)
	l.add(docBlock{kind: headingBlock, text: "Checks"})
	if len(doc.Checks) == 0 {
		l.add(docBlock{kind: paragraphBlock, text: "No checks"})
	} else {
		rows := [][]string{}
		for _, check := range doc.Checks {
			label := CheckLabel(check.Operator, check.Selector, names[check.Message])
			observed := "Unavailable: " + check.Unavailable
			if check.Observed != nil {
				observed = l.appendixRef(label+" · Observed", l.value(label+" · Observed", *check.Observed))
			}
			rows = append(rows, []string{label, l.appendixRef(label+" · Expected", l.value(label+" · Expected", check.Expected)), observed, CheckResultLabel(check.Result)})
		}
		l.add(docBlock{kind: tableBlock, columns: []column{{"Check", 0.34}, {"Expected", 0.24}, {"Observed", 0.24}, {"Result", 0.18}}, rows: rows})
	}

	if doc.Comparison != nil {
		l.comparison(doc, names)
	}

	l.add(docBlock{kind: headingBlock, text: "Messages"})
	if len(doc.Messages) == 0 {
		l.add(docBlock{kind: paragraphBlock, text: "No messages"})
	} else {
		rows := [][]string{}
		for _, message := range doc.Messages {
			rows = append(rows, []string{names[message.Source], Escape(message.Source), DeliveryLabel(message.Delivery), cmpOr(Escape(message.ACKCode), "—")})
		}
		l.add(docBlock{kind: tableBlock, columns: []column{{"Message", 0.3}, {"Source", 0.3}, {"Delivery", 0.22}, {"Response", 0.18}}, rows: rows})
	}

	if doc.Notes != nil {
		l.add(docBlock{kind: headingBlock, text: "Notes"})
		for _, paragraph := range strings.Split(strings.TrimSpace(*doc.Notes), "\n") {
			if strings.TrimSpace(paragraph) != "" {
				l.add(docBlock{kind: paragraphBlock, text: Escape(strings.TrimRight(paragraph, "\r"))})
			}
		}
	}

	l.details(doc)
	if len(l.appendix) > 0 {
		l.add(docBlock{kind: headingBlock, text: "Appendix"})
		l.blocks = append(l.blocks, l.appendix...)
	}
	return l.blocks
}

func (l *layout) comparison(doc *Document, names map[string]string) {
	comparison := doc.Comparison
	l.add(docBlock{kind: headingBlock, text: "Comparison"})
	var before DocumentRun
	for _, run := range doc.Runs {
		if run.Role == ComparisonRole {
			before = run
		}
	}
	line := "Compared with " + cmpOr(Escape(before.Test), "the comparison run")
	if when := readableTime(before.CompletedAt); when != "" {
		line += " · " + when
	}
	l.add(docBlock{kind: paragraphBlock, text: line + " · " + OutcomeLabel(before.Result.Outcome)})
	l.add(docBlock{kind: tableBlock, columns: []column{{"", 0.4}, {"", 0.6}}, rows: [][]string{
		{"Case", sameLabel(comparison.SameCase, "Same", "Different")},
		{"Target configuration", sameLabel(comparison.SameTarget, "Same", "Changed")},
		{"Test definition", DefinitionLabel(comparison.Specification)},
	}})
	unchanged, changed := [][]string{}, [][]string{}
	for _, check := range comparison.Checks {
		label := CheckLabel(check.Operator, check.Selector, names[check.Message])
		beforeCell := l.side(label+" · Before", check.Before, check.BeforeObserved)
		afterCell := l.side(label+" · After", check.After, check.AfterObserved)
		if check.Definition == "unchanged" {
			unchanged = append(unchanged, []string{label, beforeCell, afterCell})
		} else {
			changed = append(changed, []string{label, DefinitionLabel(check.Definition), beforeCell, afterCell})
		}
	}
	if len(unchanged) > 0 {
		l.add(docBlock{kind: tableBlock, columns: []column{{"Check", 0.4}, {"Before", 0.3}, {"After", 0.3}}, rows: unchanged})
	}
	if len(changed) > 0 {
		l.add(docBlock{kind: subheadingBlock, text: "Changed definitions"})
		l.add(docBlock{kind: tableBlock, columns: []column{{"Check", 0.3}, {"Change", 0.16}, {"Before", 0.27}, {"After", 0.27}}, rows: changed})
	}
}

// side is one run's result and observed value for a compared check.
func (l *layout) side(label, result string, observed *testrunner.Value) string {
	switch result {
	case "excluded":
		return "Not in this run"
	case "unknown", "":
		return "Unknown"
	}
	text := CheckResultLabel(result)
	if observed != nil {
		text += " · " + l.appendixRef(label, l.value(label, *observed))
	}
	return text
}

func (l *layout) details(doc *Document) {
	l.add(docBlock{kind: headingBlock, text: "Details"})
	for _, run := range doc.Runs {
		name := "Run"
		if run.Role == ComparisonRole {
			name = "Comparison run"
		}
		l.add(docBlock{kind: subheadingBlock, text: name})
		rows := [][]string{
			{"Test", cmpOr(Escape(run.Test), "—")},
			{"Result", OutcomeLabel(run.Result.Outcome)},
			{"Started", cmpOr(readableTime(run.StartedAt), "—")},
			{"Completed", cmpOr(readableTime(run.CompletedAt), "—")},
			{"Observed at", BoundaryLabel(run.Boundary)},
			{"Lifecycle", cmpOr(Escape(run.Result.RunState), "—")},
			{"Result identity", run.ResultIdentity},
			{"Test version", run.SpecIdentity},
			{"Case", run.CaseIdentity},
			{"Case provenance", cmpOr(Escape(run.CaseProvenance), "—")},
			{"Target configuration", run.TargetIdentity},
		}
		l.add(docBlock{kind: tableBlock, columns: []column{{"", 0.3}, {"", 0.7}}, rows: rows})
	}
	l.add(docBlock{kind: subheadingBlock, text: "Limitations"})
	l.add(docBlock{kind: listBlock, items: doc.Limitations})
	l.add(docBlock{kind: subheadingBlock, text: "Evidence"})
	rows := [][]string{{"Packet", "", doc.PacketIdentity}}
	for _, item := range doc.Evidence {
		label := EvidenceLabel(item.Kind)
		if item.Role == ComparisonRole {
			label += " · comparison"
		}
		if item.Source != "" {
			label += " · " + Escape(item.Source)
		}
		rows = append(rows, []string{label, Escape(item.Path), item.SHA256})
	}
	l.add(docBlock{kind: tableBlock, columns: []column{{"Item", 0.28}, {"Reference", 0.3}, {"SHA-256", 0.42}}, rows: rows})
}

// value is a typed value as a report cell shows it. A record list is
// counted in its cell and listed whole in the appendix.
func (l *layout) value(label string, value testrunner.Value) string {
	switch {
	case value.Count != nil:
		return strconv.Itoa(*value.Count)
	case value.Records != nil:
		records := *value.Records
		count := strconv.Itoa(len(records)) + " records"
		if len(records) == 1 {
			count = "1 record"
		}
		if len(records) == 0 {
			return count
		}
		id := "A" + strconv.Itoa(len(l.appendix)/2+1)
		rows := [][]string{}
		for _, record := range records {
			rows = append(rows, recordRow(record))
		}
		l.appendix = append(l.appendix, docBlock{kind: subheadingBlock, text: id + " · " + label, id: id},
			docBlock{kind: tableBlock, columns: []column{{"Record", 0.2}, {"Patient", 0.2}, {"Placer", 0.2}, {"Filler", 0.2}, {"Start", 0.2}}, rows: rows})
		return count + " · appendix " + id
	case value.Field != nil:
		return FieldLabel(value.Field.State, value.Field.Text)
	}
	return "—"
}

func recordRow(record observation.Record) []string {
	return []string{Escape(record.RecordID), identifier(record.PatientID), identifier(record.PlacerID), identifier(record.FillerID), Escape(record.AppointmentStart)}
}

func identifier(id observation.Identifier) string {
	parts := []string{}
	for _, part := range []string{id.Value, id.Namespace, id.UniversalID, id.UniversalIDType} {
		if part != "" {
			parts = append(parts, Escape(part))
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

// FieldLabel is a field value as a report shows it: the text of a present
// value, and Empty, Null or Not present otherwise, never the same words.
func FieldLabel(state hl7.State, text *string) string {
	switch state {
	case hl7.Present:
		if text == nil {
			return "Present"
		}
		if *text == "" {
			return `""`
		}
		return Escape(*text)
	case hl7.Empty:
		return "Empty"
	case hl7.Null:
		return "Null"
	case hl7.Omitted:
		return "Not present"
	}
	return Escape(string(state))
}

func subtitle(doc *Document) string {
	parts := []string{}
	for _, run := range doc.Runs {
		if run.Role == CurrentRole {
			if run.Test != "" {
				parts = append(parts, Escape(run.Test))
			}
			if when := readableTime(cmpOr(run.CompletedAt, run.StartedAt)); when != "" {
				parts = append(parts, when)
			}
		}
	}
	return strings.Join(parts, " · ")
}

// lifecycleFacts are what stays prominent beside an outcome: an error, an
// uncertain delivery, an incomplete journal or an undecided state.
func lifecycleFacts(result DocumentResult) []string {
	facts := []string{}
	if result.ErrorClass != "" {
		facts = append(facts, "Error: "+Escape(result.ErrorClass))
	}
	if result.DeliveryUncertain {
		facts = append(facts, "Delivery uncertain")
	}
	if result.JournalIncomplete {
		facts = append(facts, "Journal incomplete")
	}
	if result.Outcome == OutcomeIncomplete && !result.DeliveryUncertain && !result.JournalIncomplete {
		facts = append(facts, "The run did not reach a decided state")
	}
	return facts
}

func readableTime(value string) string {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func messageNames(messages []DocumentMessage) map[string]string {
	names := map[string]string{}
	for _, message := range messages {
		names[message.Source] = MessageLabel(message.Kind, message.Code, message.Trigger, message.Source)
	}
	return names
}

// MessageLabel is a message type as a report names it.
func MessageLabel(kind, code, trigger, source string) string {
	switch {
	case kind == "ack":
		return "ACK"
	case kind == "unparsed":
		return "Unparsed"
	case code != "" && trigger != "":
		return code + " " + trigger
	case code != "":
		return code
	}
	return Escape(source)
}

// CheckLabel is a check as a report names it.
func CheckLabel(operator, selector, message string) string {
	switch operator {
	case ACKCheck:
		label := strings.TrimSpace("ACK " + Escape(selector))
		if message != "" {
			label += " · " + message
		}
		return label
	case "ledger_count":
		return "Record count"
	case "ledger_equals":
		return "Exact records"
	}
	return Escape(operator)
}

// OutcomeLabel names an outcome.
func OutcomeLabel(outcome Outcome) string {
	switch outcome {
	case OutcomePassed:
		return "Passed"
	case OutcomeFailed:
		return "Failed"
	case OutcomeError:
		return "Error"
	}
	return "Incomplete"
}

// CheckResultLabel names a check result.
func CheckResultLabel(result string) string {
	switch result {
	case testrunner.Passed:
		return "Passed"
	case testrunner.Failed:
		return "Failed"
	case testrunner.NotEvaluated:
		return "Not evaluated"
	}
	return Escape(result)
}

// DeliveryLabel names a delivery.
func DeliveryLabel(delivery string) string {
	switch delivery {
	case DeliveryAcknowledged:
		return "Acknowledged"
	case DeliveryUncertain:
		return "Uncertain"
	}
	return "Not attempted"
}

// DefinitionLabel names what happened to a check's definition.
func DefinitionLabel(definition string) string {
	switch definition {
	case "unchanged":
		return "Unchanged"
	case "changed":
		return "Changed"
	case "added":
		return "Added"
	case "removed":
		return "Removed"
	}
	return "Unknown"
}

// BoundaryLabel names where a run was observed.
func BoundaryLabel(boundary string) string {
	switch boundary {
	case testrunner.ACKBoundary:
		return "Acknowledgements"
	case testrunner.LedgerBoundary:
		return "Appointment records"
	case "":
		return "—"
	}
	return Escape(boundary)
}

// EvidenceLabel names a kind of evidence item.
func EvidenceLabel(kind string) string {
	switch kind {
	case "case":
		return "Case"
	case "specification":
		return "Test version"
	case "result":
		return "Result"
	case "initial-observation":
		return "Records before"
	case "final-observation":
		return "Records after"
	case "sent-message":
		return "Sent message"
	case "received-message":
		return "Response"
	}
	return Escape(kind)
}

func sameLabel(same bool, yes, no string) string {
	if same {
		return yes
	}
	return no
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
