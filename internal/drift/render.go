package drift

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

// JSON writes the report deterministically, so the same evidence produces the
// same bytes on every machine.
func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode drift report")
	}
	return append(data, '\n'), nil
}

type line struct {
	heading bool
	text    string
}

// Terminal and Markdown carry the same content, including every cause that was
// not compared. A cause is never left out of a rendering for having nothing to
// say: that it had nothing to say is the report.
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

// markdownText escapes the same set the diff renderer escapes, for the same
// reason and independently of it: a profile identity and an engine build come
// out of a retained pin, and a pin is a document somebody can write. Nothing
// either renderer prints may become markup.
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
	lines := []line{{true, "readmit drift"}}
	add := func(format string, args ...any) { lines = append(lines, line{text: fmt.Sprintf(format, args...)}) }
	heading := func(text string) { lines = append(lines, line{heading: true, text: text}) }
	add("Contract: %s", report.Schema)
	add("%s", report.Scope)
	for _, side := range []struct {
		name string
		side Side
	}{{"Left", report.Left}, {"Right", report.Right}} {
		heading(side.name + " side")
		s := side.side
		add("Artifact: %s; identity=%s", s.Kind, absentText(s.Identity))
		add("Input: %s%s; declared transformations=%s; recorded changes=%d", s.Input.State, fingerprint(s.Input.Identity), absent(s.Input.Transformations), s.Input.RecordedChanges)
		add("Target: %s%s; receiving application revision=%s (never recorded; an acknowledgement does not name a build)", s.Target.State, fingerprint(s.Target.Fingerprint), s.Target.Revision)
		add("Environment: %s%s; engine build=%s; spec contract=%s", s.Environment.State, fingerprint(s.Environment.Fingerprint), absentText(s.Environment.Engine), absentText(s.Environment.Spec))
		add("Rule: %s%s; profile=%s; resolves here=%s", s.Rule.State, fingerprint(s.Rule.Fingerprint), absentText(s.Rule.Profile), absentText(s.Rule.Resolution))
	}
	heading("Drift by cause")
	for _, drift := range report.Drift {
		text := fmt.Sprintf("%s: %s; compared=%s", drift.Cause, drift.Outcome, drift.Comparison)
		if len(drift.Parts) > 0 {
			text += "; differing parts=" + strings.Join(drift.Parts, ", ")
		}
		if drift.Reason != "" {
			text += "; reason=" + drift.Reason
		}
		add("%s", text)
	}
	heading("Attribution")
	add("Outcome: %s", report.Attribution.Outcome)
	add("Changed causes: %s", absent(report.Attribution.Changed))
	add("Causes this evidence does not settle: %s", absent(report.Attribution.Unresolved))
	return lines
}

func fingerprint(value string) string {
	if value == "" {
		return ""
	}
	return "; fingerprint=" + value
}

func absent(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func absentText(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
