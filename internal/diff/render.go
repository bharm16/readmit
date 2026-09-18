package diff

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode diff report")
	}
	return append(data, '\n'), nil
}

type line struct {
	heading bool
	text    string
}

// Terminal and Markdown share all content, including empty sections and every
// ignore rule. Markdown escapes source displays so values cannot become markup.
func Terminal(report Report) []byte { return render(report, false) }
func Markdown(report Report) []byte { return render(report, true) }

func render(report Report, markdown bool) []byte {
	var out bytes.Buffer
	for _, line := range content(report) {
		if line.heading {
			fmt.Fprintln(&out)
			if markdown {
				fmt.Fprint(&out, "## ")
			}
			fmt.Fprintln(&out, line.text)
			fmt.Fprintln(&out)
		} else if markdown {
			fmt.Fprintf(&out, "- %s\n", markdownText(line.text))
		} else {
			fmt.Fprintln(&out, line.text)
		}
	}
	return bytes.TrimLeft(out.Bytes(), "\n")
}

func markdownText(text string) string {
	var out strings.Builder
	for _, c := range text {
		if strings.ContainsRune("\\`*_{}[]<>()#+-.!|&", c) {
			out.WriteByte('\\')
		}
		out.WriteRune(c)
	}
	return out.String()
}

func content(report Report) []line {
	lines := []line{{true, "readmit field diff"}}
	add := func(format string, args ...any) { lines = append(lines, line{text: fmt.Sprintf(format, args...)}) }
	heading := func(text string) { lines = append(lines, line{heading: true, text: text}) }
	add("Contract: %s", report.Schema)
	add("Boundary: %s. %s", report.Boundary, report.Scope)
	add("Values explicitly requested: %t. Source paths are never included.", report.ShowValues)
	for _, side := range []struct {
		name    string
		summary InputSummary
	}{{"Left", report.Left}, {"Right", report.Right}} {
		s := side.summary
		add("%s: %s; identity=%s; %s; occurrences=%d; excluded by boundary=%d", side.name, s.Kind, s.Identity, s.Payloads, s.Occurrences, s.Excluded)
		if s.SourceIdentity != "" {
			add("%s source identity: %s", side.name, s.SourceIdentity)
		}
		if s.TargetIdentity != "" {
			add("%s target configuration identity: %s", side.name, s.TargetIdentity)
		}
		if s.ResultStatus != "" {
			add("%s recorded result: %s; its observation boundary=%s (not evaluated by this diff)", side.name, s.ResultStatus, s.ResultBoundary)
		}
	}
	add("Alignment: %s", report.Alignment)
	if len(report.Keys) == 0 {
		add("Declared keys: none")
	} else {
		add("Declared keys: %s (used only for declared-keys alignment)", strings.Join(report.Keys, ", "))
	}
	if len(report.Fields) == 0 {
		add("Field scope: all encountered field repetitions; segment order is also compared")
	} else {
		add("Field scope: %s", strings.Join(report.Fields, ", "))
	}
	heading("Applied ignore rules (exact selector scope)")
	if len(report.Ignore) == 0 {
		add("None")
	}
	for _, rule := range report.Ignore {
		add("%s: compared=%d; suppressed differences=%d", rule.Selector, rule.Compared, rule.Suppressed)
	}
	heading("Summary")
	s := report.Summary
	add("Paired=%d; changed=%d; unchanged=%d; uncompared=%d; field changes=%d; inserted=%d; missing=%d; ambiguous groups=%d; unaligned=%d; unsupported=%d", s.Paired, s.Changed, s.Unchanged, s.Uncompared, s.FieldChanges, s.Inserted, s.Missing, s.Ambiguous, s.Unaligned, len(report.Unsupported))
	heading("Paired occurrences")
	if len(report.Pairs) == 0 {
		add("None")
	}
	for _, pair := range report.Pairs {
		add("%s -> %s: %s", reference(pair.Left), reference(pair.Right), pair.Status)
		for _, segment := range pair.Segments {
			add("Segment position %d: %s -> %s", segment.Position, segment.Left, segment.Right)
		}
		for _, field := range pair.Fields {
			label := field.Selector
			if field.Name != "" {
				label += " " + field.Name
			}
			add("%s: %s; left=%s; right=%s", label, field.Status, valueText(field.Left), valueText(field.Right))
		}
	}
	for _, group := range []struct {
		title string
		refs  []Reference
	}{{"Missing from right", report.Missing}, {"Inserted in right", report.Inserted}} {
		heading(group.title)
		if len(group.refs) == 0 {
			add("None")
		}
		for _, ref := range group.refs {
			add("%s", reference(ref))
		}
	}
	heading("Ambiguous alignment")
	if len(report.Ambiguous) == 0 {
		add("None")
	}
	for i, ambiguity := range report.Ambiguous {
		add("Group %d: %s; no pairing selected", i+1, ambiguity.Reason)
		for _, ref := range ambiguity.Left {
			add("Left: %s", reference(ref))
		}
		for _, ref := range ambiguity.Right {
			add("Right: %s", reference(ref))
		}
	}
	heading("Unaligned evidence")
	if len(report.Unaligned) == 0 {
		add("None")
	}
	for _, item := range report.Unaligned {
		add("%s %s: %s", item.Side, reference(item.Reference), item.Reason)
	}
	heading("Unsupported evidence and labels")
	if len(report.Unsupported) == 0 {
		add("None")
	}
	for _, item := range report.Unsupported {
		add("%s %s %s: %s", item.Side, item.Occurrence, item.Selector, item.Code)
	}
	return lines
}

func reference(ref Reference) string {
	s := fmt.Sprintf("%s (%s, %s)", ref.Occurrence, ref.Kind, ref.PayloadState)
	if ref.SourceOccurrence != "" {
		s += "; source=" + ref.SourceOccurrence
	}
	if ref.Outcome != "" {
		s += "; outcome=" + ref.Outcome + "; delivery=" + ref.Delivery
	}
	return s
}

func valueText(value Value) string {
	s := string(value.State)
	if value.Encoding != "" {
		s += " (" + value.Encoding + ")"
	}
	if value.Display != nil {
		s += " " + *value.Display
	}
	return s
}
