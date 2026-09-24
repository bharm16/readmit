package replay

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/sendpolicy"
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
	// The one refusal nothing can grant, applied before a plan exists so that
	// no path holding a plan can send to a production-classified environment
	// and no command has to remember to ask. A class is a claim and not proof
	// that an address is safe, so it can only ever refuse: a nonproduction
	// label authorizes nothing by itself, and what a send may actually reach is
	// decided against the addresses a configuration resolves to, in
	// internal/sendpolicy, which owns this comparison too.
	if sendpolicy.RefusesEverySend(string(target.Environment().Classification)) {
		return nil, errors.New("this configuration records the production classification; readmit does not replay to a production-classified environment")
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
	ca, err := destination.ReadAuthorities(target.CAFile)
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
	p := &Plan{sourcePath: resolved, sourceInfo: info, sourceIdentity: source.Identity, target: target, ca: ca, options: Options{Transformations: slices.Clone(options.Transformations), Durability: options.Durability}, changes: []Change{}}
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
			if _, err := hl7.ParseShift(t.Shift); err != nil {
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
	// Replay compares these fields as bytes, so it asks only that their
	// escapes resolve: neither valid UTF-8 nor MSH-18 is required of them.
	v, err := doc.Read(0, s, hl7.IgnoreMSH18)
	if err != nil {
		return nil, err
	}
	if v.State != hl7.Present {
		if !required && (v.State == hl7.Empty || v.State == hl7.Omitted) {
			return nil, nil
		}
		return nil, errors.New("replay requires present supported control and scope fields")
	}
	if v.Reason == hl7.UnsupportedEscape || len(v.Decoded) == 0 || len(v.Decoded) > 1024 {
		return nil, errors.New("replay control and scope fields must have supported escapes and at most 1024 bytes")
	}
	return v.Decoded, nil
}

func transform(raw []byte, mapping Mapping, transformations []Transformation, rebases map[string][]byte) ([]byte, []Change, error) {
	doc, err := parseRequest(raw)
	if err != nil {
		return nil, nil, err
	}
	var edits []hl7.Edit
	changes := []Change{}
	add := func(name string, edit hl7.Edit) error {
		field, err := doc.Select(0, edit.Selector)
		if err != nil {
			return err
		}
		old := doc.Bytes(field.Span)
		if bytes.Equal(old, edit.Value) {
			return nil
		}
		edits = append(edits, edit)
		changes = append(changes, Change{Transformation: name, SourceOccurrence: mapping.SourceOccurrence, OutboundOccurrence: mapping.OutboundOccurrence, Selector: edit.Selector.String(), OldState: field.State, NewState: hl7.Present, Old: old, New: bytes.Clone(edit.Value)})
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
				value = hl7.Surrogate(len(rebases) + 1)
				rebases[string(key)] = value
			}
			control, _ := hl7.ParseSelector("MSH-10")
			if err := add(transformation.Name, hl7.Edit{Selector: control, Value: value}); err != nil {
				return nil, nil, err
			}
		case "shift-timestamps":
			shift, _ := hl7.ParseShift(transformation.Shift)
			shifted, err := doc.ShiftTimestamps(0, shift)
			switch {
			case errors.Is(err, hl7.ErrShiftTimestamp):
				return nil, nil, errors.New("timestamp transformation supports only whole-second MSH-7 and SCH-11.4/5 with optional numeric offset")
			case errors.Is(err, hl7.ErrShiftYear):
				return nil, nil, errors.New("timestamp shift exceeds supported year range")
			case err != nil:
				return nil, nil, err
			}
			for _, edit := range shifted {
				if err := add(transformation.Name, edit); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	// The shared occurrence rewrite preserves every byte outside the selected
	// fields, including segment terminators, character encoding, and existing
	// framing, under whatever delimiters the message declares.
	result := bytes.Clone(raw)
	if len(edits) > 0 {
		rewritten, err := doc.Rewrite(0, edits, hl7.DeclaredDelimiters)
		switch {
		case errors.Is(err, hl7.ErrRewriteTooLarge):
			return nil, nil, errors.New("outbound framed message exceeds 16 MiB")
		case errors.Is(err, hl7.ErrUnreadableRewrite):
			return nil, nil, errors.New("selected occurrence is not one supported HL7 message")
		case err != nil:
			return nil, nil, err
		}
		result = rewritten.Bytes
	}
	if doc.Format == hl7.Raw {
		result = mllp.Frame(result)
	}
	return result, changes, nil
}
