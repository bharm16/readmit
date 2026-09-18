package capturejournal

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
)

// reserve routes the journal destination through the one path policy, so a
// journal cannot be written inside retained evidence, onto a symlink, or over
// an existing artifact.
func reserve(path string) (string, error) {
	if path == "" {
		return "", errors.New("a capture journal requires a new directory")
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return "", errors.New("capture journal destination must be new")
	}
	return destination, nil
}

func (c Capture) validate() error {
	if c.Schema != Schema {
		return errors.New("unsupported capture journal schema version")
	}
	if c.CreatedAt.IsZero() || !identityPattern.MatchString(c.SessionID) {
		return errors.New("a capture journal records its creation time and session identity")
	}
	if !labelPattern.MatchString(c.PolicyName) || !schemaPattern.MatchString(c.PolicySchema) || !digestPattern.MatchString(c.PolicySHA256) {
		return errors.New("a capture journal records the declared policy name, version and digest")
	}
	l := c.Limits
	if l.MaxConnections < 1 || l.MaxSessions < 0 || l.MaxMessages < 0 || l.MaxCaptureBytes < 0 || l.MaxFrameBytes < 1 {
		return errors.New("a capture journal records usable declared limits")
	}
	return nil
}

func (l *Limits) UnmarshalJSON(data []byte) error {
	var required struct {
		MaxConnections  *int `json:"max_connections"`
		MaxSessions     *int `json:"max_sessions"`
		MaxMessages     *int `json:"max_messages"`
		MaxCaptureBytes *int `json:"max_capture_bytes"`
		MaxFrameBytes   *int `json:"max_frame_bytes"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.MaxConnections == nil || required.MaxSessions == nil || required.MaxMessages == nil || required.MaxCaptureBytes == nil || required.MaxFrameBytes == nil {
		return errors.New("declared capture limits require every member")
	}
	type plain Limits
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid declared capture limits")
	}
	*l = Limits(value)
	return nil
}

func (t *Transport) UnmarshalJSON(data []byte) error {
	var required struct {
		TLS               *bool `json:"tls"`
		ClientCertificate *bool `json:"client_certificate"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.TLS == nil || required.ClientCertificate == nil {
		return errors.New("a recorded capture transport declares TLS and client certificate use explicitly")
	}
	type plain Transport
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid recorded capture transport")
	}
	*t = Transport(value)
	return nil
}

func (c *Capture) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema       *string    `json:"schema"`
		CreatedAt    *string    `json:"created_at"`
		SessionID    *string    `json:"session_id"`
		PolicyName   *string    `json:"policy_name"`
		PolicySchema *string    `json:"policy_schema"`
		PolicySHA256 *string    `json:"policy_sha256"`
		Limits       *Limits    `json:"limits"`
		Transport    *Transport `json:"transport"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.CreatedAt == nil || required.SessionID == nil || required.PolicyName == nil || required.PolicySchema == nil || required.PolicySHA256 == nil || required.Limits == nil || required.Transport == nil {
		return errors.New("a capture plan requires schema, time, session, policy identity, limits and transport")
	}
	type plain Capture
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid capture plan JSON")
	}
	*c = Capture(value)
	return nil
}

func (p *payload) UnmarshalJSON(data []byte) error {
	var required struct {
		Path   *string `json:"path"`
		Size   *int    `json:"size"`
		SHA256 *string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Path == nil || required.Size == nil || required.SHA256 == nil {
		return errors.New("a retained frame requires a path, a size and a digest")
	}
	type plain payload
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid retained frame record")
	}
	*p = payload(value)
	return nil
}

func (s *Summary) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema            *string `json:"schema"`
		State             *string `json:"state"`
		StopReason        *string `json:"stop_reason"`
		DeliveryUncertain *bool   `json:"delivery_uncertain"`
		Received          *int    `json:"received"`
		Acknowledged      *int    `json:"acknowledged"`
		Unsent            *int    `json:"unsent"`
		Uncertain         *int    `json:"uncertain"`
		Recovered         *bool   `json:"recovered"`
		JournalIncomplete *bool   `json:"journal_incomplete"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.State == nil || required.StopReason == nil || required.DeliveryUncertain == nil || required.Received == nil || required.Acknowledged == nil || required.Unsent == nil || required.Uncertain == nil || required.Recovered == nil || required.JournalIncomplete == nil {
		return errors.New("a capture summary requires every member")
	}
	type plain Summary
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid capture summary")
	}
	*s = Summary(value)
	return nil
}

func (e *entry) UnmarshalJSON(data []byte) error {
	var required struct {
		Sequence *int    `json:"sequence"`
		Previous *string `json:"previous"`
		At       *string `json:"at"`
		Kind     *string `json:"kind"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Sequence == nil || required.Previous == nil || required.At == nil || required.Kind == nil {
		return errors.New("a capture journal record requires a sequence, a chain, a time and a kind")
	}
	type plain entry
	var value plain
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid capture journal record")
	}
	*e = entry(value)
	return nil
}

// Open recovers one capture journal read-only. It opens no network connection,
// changes no file and sends nothing. An unfinished journal means finalization
// was not recorded; it does not establish that the writing process is gone, and
// it never becomes a finalized capture.
func Open(path string) (Summary, error) {
	bad := errors.New("capture journal is invalid or retained evidence changed")
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Summary{}, bad
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Summary{}, bad
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Summary{}, bad
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return Summary{}, bad
	}
	defer root.Close()
	raw, err := readBounded(root, "capture.json", maxPlanBytes)
	if err != nil {
		return Summary{}, err
	}
	var capture Capture
	if json.Unmarshal(raw, &capture) != nil || capture.validate() != nil {
		return Summary{}, bad
	}
	journal, err := readBounded(root, "journal.jsonl", maxJournalBytes)
	if err != nil {
		return Summary{}, err
	}
	return replay(root, digest(raw), journal)
}

// replay verifies the chain and reports what the records establish. Every
// refusal is the same: a journal that does not read back is not repaired, not
// partially trusted, and not reported as a capture that finished.
func replay(root *os.Root, previous string, journal []byte) (Summary, error) {
	bad := errors.New("capture journal is invalid or retained evidence changed")
	summary := Summary{Schema: Schema, State: durablerun.Interrupted, StopReason: durablerun.Interrupted, Recovered: true}
	received := make(map[string]bool)
	pending := make(map[string]bool)
	settled := make(map[string]bool)
	retained := 0
	sequence := 0
	finished := false
	lines := bytes.Split(journal, []byte{'\n'})
	truncated := len(lines[len(lines)-1]) > 0
	for _, line := range lines[:len(lines)-1] {
		sequence++
		var e entry
		if finished || json.Unmarshal(line, &e) != nil || e.Sequence != sequence || e.Previous != previous || e.At.IsZero() {
			return Summary{}, bad
		}
		previous = digest(line)
		switch e.Kind {
		case "ready", "running":
			if sequence != 1 && e.Kind == "ready" || sequence != 2 && e.Kind == "running" {
				return Summary{}, bad
			}
		case "received":
			if sequence < 3 || e.Frame == nil || received[e.Occurrence] || !occurrencePattern.MatchString(e.Occurrence) || !connectionPattern.MatchString(e.Session) || len(e.ControlID) > maxControlBytes {
				return Summary{}, bad
			}
			if e.Frame.Path != "received/"+e.Occurrence+".bin" || e.Frame.Size < 0 || e.Frame.Size > maxFrameBytes {
				return Summary{}, bad
			}
			retained += e.Frame.Size
			if retained > maxRetainedBytes {
				return Summary{}, bad
			}
			if err := verify(root, *e.Frame); err != nil {
				return Summary{}, err
			}
			received[e.Occurrence] = true
			summary.Received++
		case "intent":
			key, ok := stageKey(e)
			if !ok || !received[e.Occurrence] || pending[key] || settled[key] || e.Sent != nil || e.Frame != nil {
				return Summary{}, bad
			}
			if !declaredDestination(e.Destination) {
				return Summary{}, bad
			}
			pending[key] = true
		case "sent", "unsent":
			key, ok := stageKey(e)
			if !ok || !pending[key] || e.Sent == nil || *e.Sent < 0 || e.Destination != "" || e.Frame != nil {
				return Summary{}, bad
			}
			delete(pending, key)
			settled[key] = true
			if e.Kind == "sent" {
				summary.Acknowledged++
			} else {
				summary.Unsent++
			}
		case "finished":
			if sequence < 3 || e.Final == nil {
				return Summary{}, bad
			}
			final := *e.Final
			summary.Uncertain = len(pending)
			summary.DeliveryUncertain = summary.Uncertain > 0
			if final.Schema != Schema || final.Recovered || final.JournalIncomplete || !terminal(final.StopReason) {
				return Summary{}, bad
			}
			if final.Received != summary.Received || final.Acknowledged != summary.Acknowledged || final.Unsent != summary.Unsent || final.Uncertain != summary.Uncertain || final.DeliveryUncertain != summary.DeliveryUncertain {
				return Summary{}, bad
			}
			expected := final.StopReason
			if summary.DeliveryUncertain {
				expected = durablerun.DeliveryUncertain
			}
			if final.State != expected {
				return Summary{}, bad
			}
			summary = final
			finished = true
		default:
			return Summary{}, bad
		}
	}
	if !finished {
		summary.Uncertain = len(pending)
		summary.DeliveryUncertain = summary.Uncertain > 0
		if summary.DeliveryUncertain {
			summary.State = durablerun.DeliveryUncertain
		}
		summary.Recovered = true
	}
	if truncated {
		// A torn trailing record proves the writer stopped mid-append. What it
		// was recording is unknown, so the capture is uncertain whatever the
		// records before it said.
		summary.JournalIncomplete = true
		summary.DeliveryUncertain = true
		summary.State = durablerun.DeliveryUncertain
		summary.StopReason = durablerun.Interrupted
		summary.Recovered = true
	}
	return summary, nil
}

// stageKey names one acknowledgement stage of one frame. Intent and completion
// are matched on it, so a completion can never resolve another stage's intent.
func stageKey(e entry) (string, bool) {
	if !connectionPattern.MatchString(e.Session) || !occurrencePattern.MatchString(e.Occurrence) {
		return "", false
	}
	if e.Stage != AcceptStage && e.Stage != ApplicationStage {
		return "", false
	}
	if e.ControlID != "" || e.Final != nil {
		return "", false
	}
	return e.Session + "/" + e.Occurrence + "/" + e.Stage, true
}

func terminal(state durablerun.State) bool {
	switch state {
	case Finalized, durablerun.Cancelled, durablerun.TimedOut, durablerun.ExecutionError:
		return true
	}
	return false
}

func verify(root *os.Root, frame payload) error {
	data, err := readBounded(root, frame.Path, frame.Size)
	if err != nil {
		return err
	}
	if len(data) != frame.Size || digest(data) != frame.SHA256 {
		return errors.New("a retained capture frame changed")
	}
	return nil
}

func readBounded(root *os.Root, name string, limit int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("capture evidence must be a bounded regular file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, errors.New("cannot open capture evidence")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid capture evidence")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read bounded capture evidence")
	}
	return data, nil
}
