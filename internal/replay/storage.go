package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
)

type runWriter struct {
	root       *os.Root
	events     *os.File
	durability artifactdir.Durability
}

func (w *runWriter) Close() {
	if w.events != nil {
		_ = w.events.Close()
	}
	_ = w.root.Close()
}

func begin(plan *Plan, path string) (*Run, *runWriter, error) {
	path, err := checkDestination(plan, path)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	r := &Run{Manifest: Manifest{Schema: Schema, State: "in_progress", ContainsSourceValues: true, ExportPolicy: "customer-local-only", SourceBundleIdentity: plan.sourceIdentity, Target: plan.Target(), StartedAt: now, MessageCount: len(plan.messages), Mappings: plan.Mappings(), Transformations: slices.Clone(plan.options.Transformations), Changes: cloneChanges(plan.changes)}, payloads: make(map[string][]byte)}
	if r.Manifest.Transformations == nil {
		r.Manifest.Transformations = []Transformation{}
	}
	eventReserve := 0
	for _, message := range plan.messages {
		id := message.mapping.OutboundOccurrence
		e := Event{SourceOccurrence: message.mapping.SourceOccurrence, OutboundOccurrence: id, ControlID: bytes.Clone(message.controlID), Outcome: NotAttempted, Delivery: "not_sent", ACK: ACK{Correlation: "none", ControlID: []byte{}}}
		e.Source = r.addPayload(id, "source", message.source)
		e.Intended = r.addPayload(id, "intended", message.wire)
		e.Sent = r.addPayload(id, "sent", nil)
		e.Received = r.addPayload(id, "received", nil)
		r.Events = append(r.Events, e)
		// Reserve the longest variable byte-valued ACK field plus fixed error,
		// timing and payload descriptor growth before a socket can be opened.
		encoded, _ := json.Marshal(e, json.Deterministic(true))
		eventReserve += len(encoded) + 2048
	}
	preview := r.Manifest
	preview.State, preview.CompletedAt = "complete", now
	manifest, err := json.Marshal(preview, json.Deterministic(true))
	if err != nil || len(manifest)+128 > maxFileBytes || eventReserve > maxFileBytes {
		return nil, nil, errors.New("replay metadata exceeds run storage limits")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, nil, errors.New("cannot create run; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, errors.New("cannot open new run directory")
	}
	w := &runWriter{root: root, durability: plan.options.Durability}
	ok := false
	defer func() {
		if !ok {
			w.Close()
		}
	}()
	if err := root.Mkdir("payloads", 0700); err != nil {
		return nil, nil, errors.New("cannot create run payload directory")
	}
	initial, _ := json.Marshal(r.Manifest, json.Deterministic(true))
	if err := w.writeFile("manifest.json", append(initial, '\n')); err != nil {
		return nil, nil, err
	}
	for _, e := range r.Events {
		for _, payload := range []bundle.Payload{e.Source, e.Intended} {
			if err := w.writeFile(payload.Path, r.payloads[payload.Path]); err != nil {
				return nil, nil, err
			}
		}
	}
	w.events, err = root.OpenFile("events.jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, errors.New("cannot initialize run event file")
	}
	ok = true
	return r, w, nil
}

func checkDestination(plan *Plan, path string) (string, error) {
	// Equal contents do not establish directory identity: a copied replacement
	// would invalidate containment checks made against the original directory.
	sourceInfo, err := os.Stat(plan.sourcePath)
	if err != nil || !os.SameFile(plan.sourceInfo, sourceInfo) {
		return "", errors.New("source bundle directory changed after replay preparation")
	}
	resolved, err := artifactpath.Destination(path, sourceInfo)
	if err != nil {
		return "", err
	}
	source, err := bundle.Open(plan.sourcePath)
	if err != nil || source.Identity != plan.sourceIdentity {
		return "", errors.New("source bundle changed after replay preparation")
	}
	return resolved, nil
}

func (r *Run) addPayload(id, kind string, raw []byte) bundle.Payload {
	path := "payloads/" + id + "-" + kind + ".bin"
	r.payloads[path] = bytes.Clone(raw)
	return bundle.Payload{Path: path, Size: len(raw), SHA256: digest(raw)}
}

func (w *runWriter) record(r *Run, index int, sent, received []byte) error {
	e := &r.Events[index]
	e.Sent = r.addPayload(e.OutboundOccurrence, "sent", sent)
	e.Received = r.addPayload(e.OutboundOccurrence, "received", received)
	for _, payload := range []bundle.Payload{e.Sent, e.Received} {
		if err := w.writeFile(payload.Path, r.payloads[payload.Path]); err != nil {
			return err
		}
	}
	data, err := json.Marshal(e, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode run event")
	}
	if _, err = w.events.Write(append(data, '\n')); err == nil {
		err = w.durability.Sync(w.events)
	}
	if err != nil {
		return errors.New("cannot write run event; incomplete evidence retained")
	}
	return nil
}

func (w *runWriter) finish(r *Run) error {
	if err := w.events.Close(); err != nil {
		return errors.New("cannot finalize run event file")
	}
	w.events = nil
	r.Manifest.State, r.Manifest.CompletedAt = "complete", time.Now().UTC()
	manifest, err := json.Marshal(r.Manifest, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode run manifest")
	}
	manifest = append(manifest, '\n')
	if err := w.writeFile("manifest.pending", manifest); err != nil {
		return err
	}
	if err := w.root.Rename("manifest.pending", "manifest.json"); err != nil {
		return errors.New("cannot finalize run manifest")
	}
	events, err := w.root.ReadFile("events.jsonl")
	if err != nil {
		return errors.New("cannot verify run event file")
	}
	files := map[string][]byte{"manifest.json": manifest, "events.jsonl": events}
	for path, raw := range r.payloads {
		files[path] = raw
	}
	r.Identity = identityFor(files)
	return w.writeFile("identity.sha256", []byte(r.Identity+"\n"))
}

func (w *runWriter) writeFile(path string, data []byte) error {
	err := w.durability.WriteFile(w.root, path, data)
	if errors.Is(err, artifactdir.ErrCreateFile) {
		return errors.New("cannot create run file; incomplete evidence retained")
	}
	if err != nil {
		return errors.New("cannot write run file; incomplete evidence retained")
	}
	return nil
}

// Open verifies completion, content identity, occurrence mappings, exact payload
// hashes, every transformation, sent prefixes, and outcomes before returning a
// run. A run directory is customer-local evidence even when changes is empty.
func Open(path string) (*Run, error) {
	files, err := readFiles(path)
	if err != nil {
		return nil, err
	}
	r := &Run{payloads: make(map[string][]byte)}
	if err := json.Unmarshal(files["manifest.json"], &r.Manifest, json.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid run manifest")
	}
	if r.Manifest.Schema != Schema {
		return nil, errors.New("unsupported run bundle schema version")
	}
	if r.Manifest.State != "complete" {
		return nil, errors.New("run bundle is incomplete")
	}
	marker := files["identity.sha256"]
	delete(files, "identity.sha256")
	r.Identity = identityFor(files)
	if string(marker) != r.Identity+"\n" {
		return nil, errors.New("run identity does not match contents")
	}
	data := files["events.jsonl"]
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, errors.New("run event file is incomplete")
	}
	for line := range bytes.SplitSeq(data[:len(data)-1], []byte{'\n'}) {
		var e Event
		if len(r.Events) >= MaxMessages || json.Unmarshal(line, &e, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("invalid run event")
		}
		r.Events = append(r.Events, e)
	}
	for name, data := range files {
		if strings.HasPrefix(name, "payloads/") {
			r.payloads[name] = data
		}
	}
	if len(files) != 2+4*len(r.Events) {
		return nil, errors.New("unexpected run file layout")
	}
	if err := validateRun(r); err != nil {
		return nil, err
	}
	return r, nil
}

var occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)

func validateRun(r *Run) error {
	invalid := errors.New("run metadata disagrees with recorded evidence")
	m := r.Manifest
	if !m.ContainsSourceValues || m.ExportPolicy != "customer-local-only" || !validDigest(m.SourceBundleIdentity) || m.StartedAt.IsZero() || m.CompletedAt.Before(m.StartedAt) || m.MessageCount != len(r.Events) || len(r.Events) < 1 || len(m.Mappings) != len(r.Events) || m.Transformations == nil || m.Changes == nil {
		return invalid
	}
	target := Target{Schema: TargetSchema, TestEndpoint: m.Target.TestEndpoint, Address: m.Target.Address, Transport: m.Target.Transport, ApprovedTransport: m.Target.ApprovedTransport, ConnectTimeout: m.Target.ConnectTimeout, MessageTimeout: m.Target.MessageTimeout, MaxACKBytes: m.Target.MaxACKBytes}
	if validateTarget(target) != nil || m.Target.CASHA256 != "" && (!validDigest(m.Target.CASHA256) || m.Target.Transport != "tls") || validateTransforms(m.Transformations) != nil {
		return invalid
	}
	rebases := make(map[string][]byte)
	allChanges := []Change{}
	halted := false
	previousSource := ""
	for i, e := range r.Events {
		mapping := m.Mappings[i]
		if e.OutboundOccurrence != fmt.Sprintf("o%06d", i+1) || mapping.OutboundOccurrence != e.OutboundOccurrence || mapping.SourceOccurrence != e.SourceOccurrence || !occurrencePattern.MatchString(e.SourceOccurrence) || e.SourceOccurrence <= previousSource {
			return invalid
		}
		previousSource = e.SourceOccurrence
		payloads := []bundle.Payload{e.Source, e.Intended, e.Sent, e.Received}
		kinds := []string{"source", "intended", "sent", "received"}
		var raw [4][]byte
		for n, payload := range payloads {
			if payload.Path != "payloads/"+e.OutboundOccurrence+"-"+kinds[n]+".bin" {
				return invalid
			}
			var err error
			raw[n], err = r.Raw(payload)
			if err != nil {
				return invalid
			}
		}
		if mapping.SourceSHA256 != digest(raw[0]) {
			return invalid
		}
		intended, changes, err := transform(raw[0], mapping, m.Transformations, rebases)
		if err != nil || !bytes.Equal(intended, raw[1]) || !bytes.HasPrefix(raw[1], raw[2]) || len(raw[3]) > m.Target.MaxACKBytes+4099 {
			return invalid
		}
		allChanges = append(allChanges, changes...)
		doc, err := parseRequest(raw[1])
		if err != nil {
			return invalid
		}
		control, err := selectedBytes(doc, "MSH-10", true)
		if err != nil || !bytes.Equal(control, e.ControlID) {
			return invalid
		}
		if halted {
			if e.Outcome != NotAttempted || e.Delivery != "not_sent" || len(raw[2])+len(raw[3]) != 0 || e.TransportError != nil || e.ElapsedNS != 0 || !sameJSON(e.ACK, ACK{Correlation: "none"}) {
				return invalid
			}
			continue
		}
		if e.ElapsedNS <= 0 || e.Outcome == NotAttempted {
			return invalid
		}
		reader, _ := mllp.NewReader(bytes.NewReader(raw[3]), m.Target.MaxACKBytes)
		frame, readErr := reader.ReadFrame()
		extra := reader.Buffered()
		ack := ACK{Correlation: "none"}
		if readErr == nil {
			ack = correlate(frame, control)
		}
		if !sameJSON(ack, e.ACK) {
			return invalid
		}
		if e.TransportError == nil {
			if !bytes.Equal(raw[1], raw[2]) || readErr != nil || len(extra) != 0 || ack.Correlation != "matched" || e.Delivery != "acknowledged" || e.Outcome != ackOutcome(ack.Code) {
				return invalid
			}
		} else {
			if !validTransportError(*e.TransportError) || e.Outcome != outcomeFor(e.TransportError.Class) {
				return invalid
			}
			delivery := "not_sent"
			if len(raw[2]) > 0 {
				delivery = "uncertain"
			}
			if e.Delivery != delivery {
				return invalid
			}
			if e.TransportError.Phase == "dial" || e.TransportError.Phase == "tls" {
				if i != 0 || len(raw[2])+len(raw[3]) != 0 {
					return invalid
				}
			}
			if e.TransportError.Phase == "write" && len(raw[3]) > 0 {
				return invalid
			}
			if e.TransportError.Phase == "read" || e.TransportError.Phase == "ack" {
				if !bytes.Equal(raw[1], raw[2]) {
					return invalid
				}
			}
			if e.TransportError.Phase == "read" && readErr == nil || e.TransportError.Phase == "ack" && readErr != nil {
				return invalid
			}
			if e.Outcome == ProtocolError && readErr == nil && len(extra) == 0 && ack.Correlation == "matched" {
				return invalid
			}
			halted = true
		}
	}
	if !sameJSON(allChanges, m.Changes) {
		return invalid
	}
	return nil
}

func validTransportError(e TransportError) bool {
	switch e.Phase {
	case "dial", "tls", "write", "read", "ack":
	default:
		return false
	}
	switch e.Class {
	case "timeout", "cancelled", "disconnect", "network":
		return e.Phase != "ack"
	case "connection_refused":
		return e.Phase == "dial"
	case "tls_verification", "tls_handshake":
		return e.Phase == "tls"
	case "invalid_ack":
		return e.Phase == "read" || e.Phase == "ack"
	}
	return false
}

func sameJSON(a, b any) bool {
	left, err := json.Marshal(a, json.Deterministic(true))
	if err != nil {
		return false
	}
	right, err := json.Marshal(b, json.Deterministic(true))
	return err == nil && bytes.Equal(left, right)
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func readFiles(path string) (map[string][]byte, error) {
	return artifactdir.Read(path, artifactdir.Layout{
		Noun:               "run",
		AllowedDirectories: []string{"payloads"},
		RequiredFiles:      []string{"manifest.json", "events.jsonl", "identity.sha256"},
		AllowFile: func(name string) bool {
			return name == "manifest.json" || name == "events.jsonl" || name == "identity.sha256" || strings.HasPrefix(name, "payloads/")
		},
		MaxFiles:     4*MaxMessages + 3,
		MaxFileBytes: maxFileBytes,
		MaxBytes:     maxRunBytes,
	})
}

// Identity uses ADR-0002's domain prefix and sorted, length-delimited relative
// paths and contents. No source path, filesystem time, or absolute path enters it.
func identityFor(files map[string][]byte) string {
	return artifactdir.Identity(Schema, files)
}
