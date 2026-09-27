package observeinterval_test

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type authority struct{ binding networkaction.Binding }

func (a authority) Check(context.Context, networkaction.Binding) (networkaction.Actor, error) {
	return networkaction.Actor{Kind: "runner", ID: "runner", Generation: "1", EvidenceIdentity: dataset.Digest([]byte("grant")), Expires: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
}

type captureOptions struct {
	directory  string
	sessions   int
	filters    []observeinterval.ScopeFilter
	projection *dataset.Projection
}

func liveCapture(t *testing.T, maxRecords int, options ...captureOptions) (*observeinterval.Session, *observeinterval.Capture, *clock, dataset.Projection, dataset.Binding) {
	t.Helper()
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	host, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.CaptureListen, Port: number, Destinations: []string{netip.PrefixFrom(netip.MustParseAddr(host), 32).String()}, Selection: "single-address"}}})
	settings := captureOptions{sessions: 10}
	if len(options) > 0 {
		settings = options[0]
		if settings.sessions == 0 {
			settings.sessions = 10
		}
	}
	source, _ := json.Marshal(observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchema, Address: address, ReceiverPolicy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "owned", SourceLabel: "independent", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}, TimeoutMS: 5000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: maxRecords, MaxConnections: 4, MaxSessions: settings.sessions, RunSelector: "ZRN-1", Include: settings.filters})
	binding := dataset.Binding{Run: "run-one", Phase: "after", Namespace: "appointments", Source: dataset.Digest(source)}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "events", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "PID-3.1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 1000}}
	if settings.projection != nil {
		projection = *settings.projection
	}
	def := observeinterval.Definition{Schema: observeinterval.Schema, Source: binding.Source, Namespace: binding.Namespace, Enabled: true, Mode: "stream", Freshness: "ingress", HorizonMS: 100, SampleMS: 10, MaxGapMS: 100, MaxSamples: 20, MaxRecords: maxRecords, MaxBytes: 65536}
	clock := &clock{}
	directory := t.TempDir()
	if settings.directory != "" {
		directory = settings.directory
	}
	session, err := observeinterval.Arm(context.Background(), def, binding, filepath.Join(directory, "interval"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	prepared, err := observeinterval.PrepareCapture(source, policy, networkaction.Binding{Plan: dataset.Digest([]byte("plan")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "receiver"})
	if err != nil {
		t.Fatal(err)
	}
	capture, err := observeinterval.ArmCapture(context.Background(), source, prepared, authority{prepared.Binding()}, binding, projection, filepath.Join(session.Path(), "capture"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(capture.Stop)
	baseline, err := capture.Poll(context.Background())
	if err != nil || session.Append(context.Background(), baseline) != nil || session.StimulusStarted() != nil {
		t.Fatal("collector not armed", err)
	}
	return session, capture, clock, projection, binding
}
func sendIndependent(t *testing.T, address, run string, count int) {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(connection)
	for i := 0; i < count; i++ {
		wire := "\x0bMSH|^~\\&|OWNED|LAB|||20260101||ADT^A01|CONTROL|P|2.5.1\rPID|1||SAME\rZRN|" + run + "\r\x1c\r"
		if _, err = connection.Write([]byte(wire)); err != nil {
			t.Fatal(err)
		}
		if _, err = reader.ReadString('\x1c'); err != nil {
			t.Fatal(err)
		}
		if _, err = reader.ReadByte(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLiveCaptureRetainsLateDuplicateAndEvaluatesMultiplicity(t *testing.T) {
	session, capture, clock, projection, binding := liveCapture(t, 10)
	// Immediate independent output is accepted before the application stimulus
	// reports its ACK/completion. Socket receipt is the test's causal handshake.
	sendIndependent(t, capture.Address(), binding.Run, 2)
	sample, _ := capture.Poll(context.Background())
	if sample.Records != 2 {
		t.Fatal(sample)
	}
	if err := session.Append(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if err := session.StimulusFinished(); err != nil {
		t.Fatal(err)
	}
	clock.elapsed = 99
	sendIndependent(t, capture.Address(), binding.Run, 1)
	sample, _ = capture.Poll(context.Background())
	if err := session.Append(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if session.ReadyToFinish() {
		t.Fatal("ended before horizon")
	}
	clock.elapsed = 100
	sample, _ = capture.Poll(context.Background())
	if err := session.Append(context.Background(), sample); err != nil {
		t.Fatal(err)
	}
	if !session.ReadyToFinish() {
		t.Fatal("horizon not observed")
	}
	final, err := capture.Finalize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Append(context.Background(), final); err != nil {
		t.Fatal(err)
	}
	result, err := session.Finish(context.Background())
	if err != nil || !result.Sufficient() {
		t.Fatal(result, err)
	}
	two := 2
	checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "after", Source: binding.Source, Namespace: binding.Namespace, Phase: "after", ProjectionIdentity: projection.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "multiplicity", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &two}}}
	raw, _ := json.Marshal(checks)
	set, err := assertion.DecodeDatasets(raw)
	if err != nil {
		t.Fatal(err)
	}
	report, err := set.Evaluate(context.Background(), binding.Run, map[string]*dataset.Snapshot{"after": final.Snapshot})
	if err != nil || report.Verdict != assertion.VerdictFail {
		t.Fatal(report, err)
	}
}

func TestDeclaredExclusionsAndSeparateACKChannelsRemainInEvidence(t *testing.T) {
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "acks", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "control", Type: "text", Selector: "MSA-2", Key: true, Required: true}, {Name: "code", Type: "text", Selector: "MSA-1", Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	type collecting struct {
		s       *observeinterval.Session
		c       *observeinterval.Capture
		clock   *clock
		binding dataset.Binding
		code    string
	}
	channels := []collecting{}
	// Both return channels are listening before either asynchronous stage arrives.
	for _, code := range []string{"CA", "AA"} {
		s, c, k, _, binding := liveCapture(t, 10, captureOptions{projection: &projection, filters: []observeinterval.ScopeFilter{{Selector: "MSH-9.1", Equals: "ACK"}}})
		channels = append(channels, collecting{s, c, k, binding, code})
	}
	for _, channel := range channels {
		sendIndependent(t, channel.c.Address(), channel.binding.Run, 1) // unrelated ADT is retained, then explicitly excluded
		conn, err := net.Dial("tcp", channel.c.Address())
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		wire := "\x0bMSH|^~\\&|RETURN|LAB|||20260101||ACK|CONTROL|P|2.5.1\rMSA|" + channel.code + "|STEP1\rZRN|" + channel.binding.Run + "\r\x1c\r"
		if _, err = conn.Write([]byte(wire)); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		if _, err = reader.ReadString('\x1c'); err != nil {
			t.Fatal(err)
		}
		reader.ReadByte()
		conn.Close()
		if channel.s.StimulusFinished() != nil {
			t.Fatal("not started")
		}
		channel.clock.elapsed = 100
		final, err := channel.c.Finalize(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(final.Excluded) != 1 || len(final.Snapshot.Document().Rows) != 1 || final.Snapshot.Document().Rows[0].Values[1].Text != channel.code {
			t.Fatal(final)
		}
		if err = channel.s.Append(context.Background(), final); err != nil {
			t.Fatal(err)
		}
		result, err := channel.s.Finish(context.Background())
		if err != nil || !result.Sufficient() {
			t.Fatal(result, err)
		}
		_, all, err := networkaction.OpenCapture(filepath.Join(channel.s.Path(), "capture"))
		if err != nil {
			t.Fatal(err)
		}
		inbound := 0
		for _, event := range all.Events {
			if event.Direction == "inbound" {
				inbound++
			}
		}
		if inbound != 2 {
			t.Fatal("excluded occurrence was removed", inbound)
		}
	}
}
