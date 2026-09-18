package replay

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

// Prepare validates all selected messages and named transformations without DNS,
// sockets, TLS handshakes, output writes, or mutation of source evidence. Empty
// selection means all message events; ACKs/unparsed evidence are never sent.
func Prepare(sourcePath string, target Target, options Options) (*Plan, error) {
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, errors.New("cannot resolve source bundle directory")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, errors.New("cannot resolve source bundle directory")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, errors.New("source bundle must be a directory")
	}
	source, err := bundle.Open(resolved)
	if err != nil {
		return nil, err
	}
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	if err := validateTransforms(options.Transformations); err != nil {
		return nil, err
	}
	// readmit-target/v3 can declare a client certificate. This release's MLLP
	// transport presents none, so a configuration that declares one is refused
	// here rather than sent without it: an unsupported member is not a passing
	// one, and silently ignoring it would make a successful diagnosis predict
	// a handshake this transport cannot complete.
	if target.ClientCertificate != "" {
		return nil, errors.New("this release's replay transport presents no client certificate, so a configuration declaring one cannot be replayed; readmit target check diagnoses it against the same endpoint")
	}
	ca, err := LoadCA(target)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool)
	for _, id := range options.Occurrences {
		if selected[id] {
			return nil, errors.New("duplicate occurrence selection")
		}
		selected[id] = true
	}
	p := &Plan{sourcePath: resolved, sourceInfo: info, sourceIdentity: source.Identity, target: target, ca: ca, options: Options{Transformations: slices.Clone(options.Transformations)}, changes: []Change{}}
	rebases := make(map[string][]byte)
	total := 0
	for _, event := range source.Events {
		if len(options.Occurrences) > 0 && !selected[event.ID] {
			continue
		}
		if event.Kind != bundle.Message {
			if selected[event.ID] {
				return nil, errors.New("selected occurrence is not a message")
			}
			continue
		}
		delete(selected, event.ID)
		if len(p.messages) >= MaxMessages {
			return nil, errors.New("replay exceeds 4000 message limit")
		}
		raw, err := source.Raw(event.ID)
		if err != nil {
			return nil, err
		}
		mapping := Mapping{SourceOccurrence: event.ID, OutboundOccurrence: fmt.Sprintf("o%06d", len(p.messages)+1), SourceSHA256: digest(raw)}
		wire, changes, err := transform(raw, mapping, options.Transformations, rebases)
		if err != nil {
			return nil, err
		}
		if len(wire) > maxFileBytes {
			return nil, errors.New("outbound framed message exceeds 16 MiB")
		}
		doc, err := parseRequest(wire)
		if err != nil {
			return nil, err
		}
		control, err := selectedBytes(doc, "MSH-10", true)
		if err != nil {
			return nil, err
		}
		// Reserve actual sent bytes and bounded ACK read-ahead before any send.
		// A plan cannot run out of evidence capacity midway through delivery.
		total += len(raw) + 2*len(wire) + target.MaxACKBytes + 4099
		if total > (64<<20)-1024 {
			return nil, errors.New("replay evidence reservation exceeds 64 MiB; select fewer messages or reduce max_ack_bytes")
		}
		p.messages = append(p.messages, plannedMessage{mapping: mapping, source: raw, wire: wire, controlID: control})
		p.changes = append(p.changes, changes...)
	}
	if len(selected) != 0 {
		return nil, errors.New("unknown occurrence selection")
	}
	if len(p.messages) == 0 {
		return nil, errors.New("replay requires at least one message")
	}
	return p, nil
}

func validateTransforms(transforms []Transformation) error {
	seen := make(map[string]bool)
	for _, t := range transforms {
		if seen[t.Name] {
			return errors.New("duplicate named transformation")
		}
		seen[t.Name] = true
		switch t.Name {
		case "rebase-control-ids":
			if t.Shift != "" {
				return errors.New("control ID transformation does not accept a shift")
			}
		case "shift-timestamps":
			d, err := time.ParseDuration(t.Shift)
			if err != nil || d == 0 || d%time.Second != 0 || d < -10*365*24*time.Hour || d > 10*365*24*time.Hour {
				return errors.New("timestamp shift requires nonzero whole seconds within ten years")
			}
		default:
			return errors.New("unknown named replay transformation")
		}
	}
	return nil
}

func parseRequest(raw []byte) (*hl7.Document, error) {
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil || len(doc.Messages) != 1 {
		return nil, errors.New("selected occurrence is not one supported HL7 message")
	}
	header := doc.Messages[0].Segments[0]
	if len(header.Field(10).Repetitions) != 1 {
		return nil, errors.New("replay requires one MSH-10 control ID repetition")
	}
	for _, n := range []int{15, 16} {
		state := header.Field(n).State
		if state != hl7.Omitted && state != hl7.Empty {
			return nil, errors.New("enhanced acknowledgement requests are unsupported; MSH-15 and MSH-16 must be empty or omitted")
		}
	}
	if _, err := selectedBytes(doc, "MSH-10", true); err != nil {
		return nil, err
	}
	return doc, nil
}

func selectedBytes(doc *hl7.Document, path string, required bool) ([]byte, error) {
	s, err := hl7.ParseSelector(path)
	if err != nil {
		return nil, err
	}
	v, err := doc.Select(0, s)
	if err != nil {
		return nil, err
	}
	if v.State != hl7.Present {
		if !required && (v.State == hl7.Empty || v.State == hl7.Omitted) {
			return nil, nil
		}
		return nil, errors.New("replay requires present supported control and scope fields")
	}
	value, err := hl7.Decode(doc.Bytes(v.Span), doc.Messages[0].Delimiters)
	if err != nil || len(value) == 0 || len(value) > 1024 {
		return nil, errors.New("replay control and scope fields must have supported escapes and at most 1024 bytes")
	}
	return value, nil
}

type edit struct {
	span  hl7.Span
	value []byte
}

func transform(raw []byte, mapping Mapping, transformations []Transformation, rebases map[string][]byte) ([]byte, []Change, error) {
	doc, err := parseRequest(raw)
	if err != nil {
		return nil, nil, err
	}
	var edits []edit
	changes := []Change{}
	add := func(name, path string, value []byte) error {
		selector, err := hl7.ParseSelector(path)
		if err != nil {
			return err
		}
		field, err := doc.Select(0, selector)
		if err != nil {
			return err
		}
		old := doc.Bytes(field.Span)
		if bytes.Equal(old, value) {
			return nil
		}
		edits = append(edits, edit{field.Span, bytes.Clone(value)})
		changes = append(changes, Change{Transformation: name, SourceOccurrence: mapping.SourceOccurrence, OutboundOccurrence: mapping.OutboundOccurrence, Selector: selector.String(), OldState: field.State, NewState: hl7.Present, Old: old, New: bytes.Clone(value)})
		return nil
	}
	for _, transformation := range transformations {
		switch transformation.Name {
		case "rebase-control-ids":
			for _, number := range []int{3, 4} {
				field := doc.Messages[0].Segments[0].Field(number)
				if len(field.Repetitions) > 1 {
					return nil, nil, errors.New("control ID transformation requires nonrepeating sender scope")
				}
				selector, _ := hl7.ParseSelector(fmt.Sprintf("MSH-%d.4", number))
				value, _ := doc.Select(0, selector)
				if value.State != hl7.Omitted && field.State == hl7.Present {
					return nil, nil, errors.New("control ID transformation supports three-component HD sender scope")
				}
			}
			// HD sender application/facility components form the explicit scope.
			// Length-delimited JSON byte arrays avoid tuple collisions and preserve
			// equivalent standard escape spellings as the same decoded identifier.
			var scope [][]byte
			for _, path := range []string{"MSH-3.1", "MSH-3.2", "MSH-3.3", "MSH-4.1", "MSH-4.2", "MSH-4.3", "MSH-10"} {
				value, err := selectedBytes(doc, path, path == "MSH-10")
				if err != nil {
					return nil, nil, err
				}
				scope = append(scope, value)
			}
			key, _ := json.Marshal(scope, json.Deterministic(true))
			value, exists := rebases[string(key)]
			if !exists {
				value = fmt.Appendf(nil, "READMIT%06d", len(rebases)+1)
				rebases[string(key)] = value
			}
			if err := add(transformation.Name, "MSH-10", value); err != nil {
				return nil, nil, err
			}
		case "shift-timestamps":
			shift, _ := time.ParseDuration(transformation.Shift)
			paths := []string{"MSH-7"}
			count := 0
			for _, segment := range doc.Messages[0].Segments {
				if segment.ID != "SCH" {
					continue
				}
				count++
				for repetition := range segment.Field(11).Repetitions {
					paths = append(paths, fmt.Sprintf("SCH[%d]-11[%d].4", count, repetition+1))
					paths = append(paths, fmt.Sprintf("SCH[%d]-11[%d].5", count, repetition+1))
				}
			}
			for _, path := range paths {
				selector, _ := hl7.ParseSelector(path)
				field, _ := doc.Select(0, selector)
				if field.State != hl7.Present {
					continue
				}
				value := string(doc.Bytes(field.Span))
				layout := "20060102150405"
				if len(value) == 19 {
					layout += "-0700"
				}
				parsed, err := time.Parse(layout, value)
				if err != nil || parsed.Format(layout) != value {
					return nil, nil, errors.New("timestamp transformation supports only whole-second MSH-7 and SCH-11.4/5 with optional numeric offset")
				}
				shifted := parsed.Add(shift)
				if shifted.Year() < 1 || shifted.Year() > 9999 {
					return nil, nil, errors.New("timestamp shift exceeds supported year range")
				}
				if err := add(transformation.Name, path, []byte(shifted.Format(layout))); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	// Reverse-span replacement preserves every byte outside the selected fields,
	// including segment terminators, character encoding, and existing framing.
	slices.SortFunc(edits, func(a, b edit) int { return b.span.Start - a.span.Start })
	result := bytes.Clone(raw)
	for _, edit := range edits {
		result = append(append(append([]byte{}, result[:edit.span.Start]...), edit.value...), result[edit.span.End:]...)
	}
	if doc.Format == hl7.Raw {
		result = mllp.Frame(result)
	}
	return result, changes, nil
}
