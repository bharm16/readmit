package receiver_test

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

// A readmit-receiver-policy/v2 sink that answers both acknowledgement stages on
// the connection that delivered the message.
const enhancedSameConnection = `{"schema":"readmit-receiver-policy/v2","name":"downstream-sink","source_label":"downstream-test-endpoint",` +
	`"acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},` +
	`"accepted_message_types":{"operator":"any-message-type","values":[]},` +
	`"enhanced_acknowledgement":{"operator":"enhanced-mode-fixed-codes","accept_code":"CA","application_code":"AA",` +
	`"application_delivery":"same-connection","application_endpoint":"","approved_transport":false}}`

// The same sink with enhanced mode explicitly declined.
const enhancedDeclined = `{"schema":"readmit-receiver-policy/v2","name":"downstream-sink","source_label":"downstream-test-endpoint",` +
	`"acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},` +
	`"accepted_message_types":{"operator":"any-message-type","values":[]},` +
	`"enhanced_acknowledgement":{"operator":"unsupported","accept_code":"","application_code":"",` +
	`"application_delivery":"","application_endpoint":"","approved_transport":false}}`

func separateEndpointPolicy(address string) string {
	return strings.Replace(strings.Replace(enhancedSameConnection,
		`"application_delivery":"same-connection"`, `"application_delivery":"separate-endpoint"`, 1),
		`"application_endpoint":""`, `"application_endpoint":"`+address+`"`, 1)
}

// enhanced builds a message declaring the given MSH-15 and MSH-16 conditions.
// An empty condition leaves that field empty, which is original mode.
func enhanced(control, accept, application string) []byte {
	return []byte(fmt.Sprintf(
		"MSH|^~\\&|SENDER|FACILITY|READMIT|COLLECT|20260101120000||ADT^A01|%s|P|2.5.1|||%s|%s\rPID|1||SYNTH-001\r",
		control, accept, application))
}

// applicationSink is a loopback endpoint that receives asynchronous application
// acknowledgements. It is a separate socket from the one that delivered the
// message, which is the whole point of the configuration it exercises.
type applicationSink struct {
	address  string
	listener net.Listener
	mutex    sync.Mutex
	frames   [][]byte
	arrived  chan struct{}
}

func newApplicationSink(t *testing.T) *applicationSink {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sink := &applicationSink{address: listener.Addr().String(), listener: listener, arrived: make(chan struct{}, 8)}
	go sink.serve()
	t.Cleanup(func() { listener.Close() })
	return sink
}

func (s *applicationSink) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader, _ := mllp.NewReader(conn, 1<<20)
		for {
			raw, err := reader.ReadFrame()
			if err != nil {
				break
			}
			s.mutex.Lock()
			s.frames = append(s.frames, raw)
			s.mutex.Unlock()
			select {
			case s.arrived <- struct{}{}:
			default:
			}
		}
		conn.Close()
	}
}

func (s *applicationSink) await(t *testing.T, count int) [][]byte {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		s.mutex.Lock()
		delivered := append([][]byte(nil), s.frames...)
		s.mutex.Unlock()
		if len(delivered) >= count {
			return delivered[:count]
		}
		select {
		case <-s.arrived:
		case <-deadline:
			t.Fatalf("separate application endpoint received %d of %d acknowledgements", len(delivered), count)
		}
	}
}

func msa(t *testing.T, raw []byte) (code, acknowledged, control string) {
	t.Helper()
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		t.Fatalf("acknowledgement is not readable HL7: %v", err)
	}
	message := doc.Messages[0]
	if len(message.Segments) < 2 || message.Segments[1].ID != "MSA" {
		t.Fatalf("acknowledgement carries no MSA segment: %q", raw)
	}
	header, segment := message.Segments[0], message.Segments[1]
	return string(doc.Bytes(segment.Field(1).Span)), string(doc.Bytes(segment.Field(2).Span)), string(doc.Bytes(header.Field(10).Span))
}

// Enhanced mode is two stages: a commit acknowledgement that the bytes were
// accepted for processing, and a later application acknowledgement. Both
// correlate to the sender's control ID; each carries its own.
func TestCollectorAnswersBothStagesOnOneConnection(t *testing.T) {
	h := collect(t, enhancedSameConnection, 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ENH-001", "AL", "AL")))
	first, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	acceptCode, acceptFor, acceptID := msa(t, first)
	appCode, appFor, appID := msa(t, second)
	if acceptCode != "CA" || appCode != "AA" {
		t.Fatalf("stages did not answer with their own codes: %s then %s", acceptCode, appCode)
	}
	if acceptFor != "ENH-001" || appFor != "ENH-001" {
		t.Fatalf("stages lost the sender's correlation: %s and %s", acceptFor, appFor)
	}
	if acceptID == appID || acceptID == "" || appID == "" {
		t.Fatalf("stages shared or omitted their own control IDs: %q and %q", acceptID, appID)
	}
	record := h.finished(t).Collection
	if record.Schema != collection.Schema || len(record.Received) != 1 {
		t.Fatalf("collected record is not a v2 record of one frame: %+v", record)
	}
	entry := record.Received[0]
	if entry.Mode != collection.EnhancedMode || entry.ControlID != "ENH-001" {
		t.Fatalf("enhanced mode was not recorded: %+v", entry)
	}
	if entry.Accept.Code != collection.CommitAcceptCode || entry.Accept.ControlID != acceptID || entry.Accept.Destination != collection.SameConnection {
		t.Fatalf("accept stage was not recorded as itself: %+v", entry.Accept)
	}
	if entry.Application.Code != collection.AcceptCode || entry.Application.ControlID != appID || entry.Application.Destination != collection.SameConnection {
		t.Fatalf("application stage was not recorded as itself: %+v", entry.Application)
	}
	// A commit acknowledgement is not evidence of application processing, and
	// neither is a simulated application acknowledgement.
	if record.ApplicationProcessing != collection.NoApplicationProcessing {
		t.Fatal("an enhanced exchange claimed application processing")
	}
}

// Async application acknowledgements may arrive through a separately configured
// endpoint, so the receiver must not assume one socket.
func TestCollectorDeliversTheApplicationStageToASeparateEndpoint(t *testing.T) {
	sink := newApplicationSink(t)
	h := collect(t, separateEndpointPolicy(sink.address), 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ENH-002", "AL", "AL")))
	acceptCode, acceptFor, _ := msa(t, ack(t, reader, "CA", "ENH-002"))
	if acceptCode != "CA" || acceptFor != "ENH-002" {
		t.Fatalf("the receiving connection did not get the commit acknowledgement: %s %s", acceptCode, acceptFor)
	}
	delivered := sink.await(t, 1)
	appCode, appFor, appID := msa(t, delivered[0])
	if appCode != "AA" || appFor != "ENH-002" {
		t.Fatalf("the separate endpoint did not get a correlated application acknowledgement: %s %s", appCode, appFor)
	}
	// Nothing further arrives on the receiving connection.
	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if extra, err := reader.ReadFrame(); err == nil {
		t.Fatalf("the application acknowledgement was also sent on the receiving connection: %q", extra)
	}
	collected := h.finished(t)
	entry := collected.Collection.Received[0]
	if entry.Application.Destination != collection.SeparateEndpoint || entry.Application.ControlID != appID {
		t.Fatalf("the separate delivery was not recorded: %+v", entry.Application)
	}
	// Every byte the receiver wrote stays in the evidence, wherever it went.
	retained := false
	for _, event := range collected.Events {
		if raw, err := collected.Raw(event.ID); err == nil && bytes.Equal(raw, delivered[0]) {
			retained = true
		}
	}
	if !retained {
		t.Fatal("the acknowledgement sent to the separate endpoint was not retained")
	}
}

// An application acknowledgement that could not be delivered is an execution
// error, never a pass and never an application rejection.
func TestCollectorRecordsAnUndeliverableApplicationStageAsUnanswered(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := closed.Addr().String()
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	h := collect(t, separateEndpointPolicy(address), 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ENH-003", "AL", "AL")))
	ack(t, reader, "CA", "ENH-003")
	entry := h.finished(t).Collection.Received[0]
	if entry.Accept.Code != collection.CommitAcceptCode {
		t.Fatalf("the commit stage was lost with the application stage: %+v", entry.Accept)
	}
	if entry.Application.Code != collection.NotAcknowledged || entry.Application.Destination != collection.NoDestination {
		t.Fatalf("an undelivered application acknowledgement was claimed: %+v", entry.Application)
	}
	if entry.Application.Reason == "" {
		t.Fatal("an undelivered application acknowledgement has no recorded reason")
	}
	for _, negative := range []string{collection.ApplicationErrorCode, collection.RejectCode} {
		if entry.Application.Code == negative {
			t.Fatalf("a delivery failure was recorded as the application result %s", negative)
		}
	}
}

// The declared MSH-15/MSH-16 conditions decide which stages are answered.
func TestCollectorHonoursDeclaredAcknowledgementConditions(t *testing.T) {
	for name, c := range map[string]struct {
		accept, application string
		codes               []string
	}{
		"both always":             {"AL", "AL", []string{"CA", "AA"}},
		"accept only":             {"AL", "NE", []string{"CA"}},
		"application only":        {"NE", "AL", []string{"AA"}},
		"neither":                 {"NE", "NE", nil},
		"on success":              {"SU", "SU", []string{"CA", "AA"}},
		"on error":                {"ER", "ER", nil},
		"accept on error":         {"ER", "AL", []string{"AA"}},
		"application unrequested": {"AL", "", []string{"CA"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := collect(t, enhancedSameConnection, 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame(enhanced("COND-1", c.accept, c.application)))
			for _, want := range c.codes {
				code, acknowledged, _ := msa(t, mustFrame(t, reader))
				if code != want || acknowledged != "COND-1" {
					t.Fatalf("expected %s for COND-1, got %s for %s", want, code, acknowledged)
				}
			}
			if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if extra, err := reader.ReadFrame(); err == nil {
				t.Fatalf("an unrequested stage was answered anyway: %q", extra)
			}
			h.cancel()
			entry := h.finished(t).Collection.Received[0]
			sent := []string{}
			for _, stage := range []collection.Stage{entry.Accept, entry.Application} {
				if stage.Code != collection.NotAcknowledged {
					sent = append(sent, stage.Code)
				}
			}
			if strings.Join(sent, ",") != strings.Join(c.codes, ",") {
				t.Fatalf("record disagrees with the wire: %v answered, %v recorded", c.codes, sent)
			}
			for _, stage := range []collection.Stage{entry.Accept, entry.Application} {
				if stage.Code == collection.NotAcknowledged && stage.Reason == "" {
					t.Fatalf("an unanswered stage records no reason: %+v", entry)
				}
			}
		})
	}
}

func mustFrame(t *testing.T, reader *mllp.Reader) []byte {
	t.Helper()
	raw, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A mode combination this receiver does not support is a named error, never an
// invented protocol outcome and never a silent pass.
func TestCollectorNamesUnsupportedAcknowledgementModes(t *testing.T) {
	for name, c := range map[string]struct {
		declared            string
		accept, application string
		expect              string
	}{
		"policy version without enhanced support": {anyTypePolicy, "AL", "", "enhanced"},
		"policy that declines enhanced mode":      {enhancedDeclined, "AL", "AL", "enhanced"},
		"undeclared accept condition":             {enhancedSameConnection, "XX", "AL", "condition"},
		"undeclared application condition":        {enhancedSameConnection, "AL", "ZZ", "condition"},
		"explicitly null accept condition":        {enhancedSameConnection, `""`, "AL", "condition"},
	} {
		t.Run(name, func(t *testing.T) {
			h := collect(t, c.declared, 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame(enhanced("MODE-1", c.accept, c.application)))
			code, acknowledged, _ := msa(t, mustFrame(t, reader))
			if code != collection.RejectCode || acknowledged != "MODE-1" {
				t.Fatalf("an unsupported mode was not refused against its own control ID: %s %s", code, acknowledged)
			}
			entry := h.finished(t).Collection.Received[0]
			if entry.Mode != collection.EnhancedMode {
				t.Fatalf("the requested mode was not recorded: %+v", entry)
			}
			if entry.Accept.Code != collection.NotAcknowledged {
				t.Fatalf("an unsupported mode produced a commit acknowledgement: %+v", entry.Accept)
			}
			if entry.Application.Code != collection.RejectCode || !strings.Contains(entry.Application.Reason, c.expect) {
				t.Fatalf("the refusal does not name what it refused: %+v", entry.Application)
			}
		})
	}
}

// A v2 policy changes nothing for a sender that did not ask for enhanced mode.
func TestCollectorKeepsOriginalModeUnderAnEnhancedPolicy(t *testing.T) {
	h := collect(t, enhancedSameConnection, 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame([]byte(plainADT)))
	code, acknowledged, own := msa(t, mustFrame(t, reader))
	if code != collection.AcceptCode || acknowledged != "COLLECT-001" {
		t.Fatalf("original mode did not get the declared original-mode code: %s %s", code, acknowledged)
	}
	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if extra, err := reader.ReadFrame(); err == nil {
		t.Fatalf("original mode got a second stage: %q", extra)
	}
	entry := h.finished(t).Collection.Received[0]
	if entry.Mode != collection.OriginalMode || entry.Accept.Code != collection.NotAcknowledged {
		t.Fatalf("original mode was recorded with an accept stage: %+v", entry)
	}
	if entry.Application.Code != collection.AcceptCode || entry.Application.ControlID != own || entry.Application.Destination != collection.SameConnection {
		t.Fatalf("original mode was not recorded as one application stage: %+v", entry.Application)
	}
}

// A sender that asked for no acknowledgement gets none, and its connection
// stays open for the next message.
func TestCollectorContinuesWhenNoStageWasRequested(t *testing.T) {
	h := collect(t, enhancedSameConnection, 2)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("SILENT-1", "NE", "NE")))
	send(t, conn, mllp.Frame(enhanced("SPEAK-1", "AL", "AL")))
	code, acknowledged, _ := msa(t, mustFrame(t, reader))
	if code != "CA" || acknowledged != "SPEAK-1" {
		t.Fatalf("the silent frame closed the connection: %s %s", code, acknowledged)
	}
	record := h.finished(t).Collection
	if len(record.Received) != 2 || record.Received[0].ControlID != "SILENT-1" {
		t.Fatalf("the unanswered frame was not retained as received: %+v", record.Received)
	}
	silent := record.Received[0]
	if silent.Accept.Code != collection.NotAcknowledged || silent.Application.Code != collection.NotAcknowledged {
		t.Fatalf("a declined stage was answered anyway: %+v", silent)
	}
}

// A separate application endpoint that does not take the frame inside
// --application-ack-timeout leaves the stage unanswered. A timeout is an
// explicit uncertainty, never a negative application result.
func TestCollectorRecordsAnApplicationDeliveryTimeoutAsUnanswered(t *testing.T) {
	sink := newApplicationSink(t)
	// A deadline this short cannot be met by any real connect, so the timeout
	// path is taken deterministically rather than raced for.
	h := collectTimed(t, separateEndpointPolicy(sink.address), 1, time.Nanosecond)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ENH-004", "AL", "AL")))
	ack(t, reader, "CA", "ENH-004")
	entry := h.finished(t).Collection.Received[0]
	if entry.Accept.Code != collection.CommitAcceptCode {
		t.Fatalf("the commit stage was lost with the application stage: %+v", entry.Accept)
	}
	if entry.Application.Code != collection.NotAcknowledged || entry.Application.Destination != collection.NoDestination {
		t.Fatalf("a timed-out application acknowledgement was claimed: %+v", entry.Application)
	}
	if entry.Application.Reason == "" {
		t.Fatal("a timed-out application acknowledgement has no recorded reason")
	}
	sink.mutex.Lock()
	delivered := len(sink.frames)
	sink.mutex.Unlock()
	if delivered != 0 {
		t.Fatalf("the endpoint received %d frames despite the expired timeout", delivered)
	}
}

// The case's own correlation graph is structural: it reports which
// acknowledgements name which message, and carries no outcome at all. Both
// stages of one enhanced exchange correlate to the message they answer, and the
// codes that tell them apart live only in the collection record.
func TestCollectorCorrelatesBothStagesWithoutClaimingAnOutcome(t *testing.T) {
	h := collect(t, enhancedSameConnection, 1)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ENH-005", "AL", "AL")))
	mustFrame(t, reader)
	mustFrame(t, reader)
	collected := h.finished(t)
	matched := 0
	for _, link := range collected.Correlations {
		if link.Kind == bundle.Matched {
			matched++
			if len(link.MessageIDs) != 1 || link.MessageIDs[0] != "s0001-e000001" {
				t.Fatalf("a stage correlated to the wrong message: %+v", link)
			}
		}
		if link.Kind == bundle.Unacknowledged {
			t.Fatalf("an answered message was recorded as unacknowledged: %+v", link)
		}
	}
	if matched != 2 {
		t.Fatalf("an enhanced exchange produced %d matched correlations, want 2", matched)
	}
	entry := collected.Collection.Received[0]
	if entry.Accept.Code != collection.CommitAcceptCode || entry.Application.Code != collection.AcceptCode {
		t.Fatalf("the stages that the correlation graph cannot distinguish are not distinct in the record: %+v", entry)
	}
}
