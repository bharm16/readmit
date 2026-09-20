package replay_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
)

func caseAt(t *testing.T, raw ...[]byte) string {
	t.Helper()
	var inputs []bundle.Input
	for i, data := range raw {
		inputs = append(inputs, bundle.Input{Path: fmt.Sprintf("PRIVATE-SYNTHETIC-%d.hl7", i), Data: data})
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "source")
	if _, err := bundle.Write(path, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); err != nil {
		t.Fatal(err)
	}
	return path
}

func request(id string) []byte {
	return []byte("MSH|^~\\&|SYNTHETIC|LAB|READMIT|FIXTURE|20260101120000||SIU^S12|" + id + "|P|2.5.1\rSCH|APPT^LAB|FILL^LAB|||||||||^^^20260102120000\rPID|1||SYNTHETIC-PATIENT^^^LAB\r")
}

func ack(code, id string) []byte {
	return mllp.Frame([]byte("MSH|^~\\&|PEER|TEST|SYNTHETIC|LAB|20260101120000||ACK^S12|ACK1|P|2.5.1\rMSA|" + code + "|" + id + "\r"))
}

func target(address string) replay.Target {
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "150ms", MaxACKBytes: 4096}
}

func peer(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		handle(conn)
	}()
	t.Cleanup(func() {
		_ = l.Close()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("scripted peer did not stop")
		}
	})
	return l.Addr().String()
}

func execute(t *testing.T, source string, target replay.Target, options replay.Options) (*replay.Run, string) {
	t.Helper()
	plan, err := replay.Prepare(source, target, options)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	r, err := replay.Execute(ctx, plan, path)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := replay.Open(path)
	if err != nil {
		t.Fatalf("final run does not reopen: %v; events=%+v", err, r.Events)
	}
	if r.Identity != verified.Identity || !reflect.DeepEqual(r.Events, verified.Events) {
		t.Fatal("reader changed recorded evidence")
	}
	return verified, path
}

func raw(t *testing.T, r *replay.Run, payload bundle.Payload) []byte {
	t.Helper()
	b, err := r.Raw(payload)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSequentialReplayPreservesVerbatimBytesAndSourceIdentity(t *testing.T) {
	first, second := request("CONTROL-A"), request("CONTROL-B")
	framed := append(mllp.Frame(first), mllp.Frame(second)...)
	source := caseAt(t, framed)
	before, _ := bundle.Open(source)
	address := peer(t, func(c net.Conn) {
		reader, _ := mllp.NewReader(c, 1<<20)
		for i, id := range []string{"CONTROL-A", "CONTROL-B"} {
			got, err := reader.ReadFrame()
			want := [][]byte{mllp.Frame(first), mllp.Frame(second)}[i]
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("peer request %d was changed: %v", i, err)
				return
			}
			// No second request may be outstanding before this ACK. Buffered
			// bytes and a short socket read both independently detect pipelining.
			if extra := reader.Buffered(); len(extra) != 0 {
				t.Error("pipelined message before ACK")
				return
			}
			if i == 0 {
				_ = c.SetReadDeadline(time.Now().Add(25 * time.Millisecond))
				var extra [1]byte
				n, err := c.Read(extra[:])
				if n != 0 || err == nil {
					t.Error("second message sent before first ACK")
					return
				}
				_ = c.SetReadDeadline(time.Now().Add(time.Second))
			}
			_, _ = c.Write(ack("AA", id))
		}
	})
	r, _ := execute(t, source, target(address), replay.Options{})
	if !r.Successful() || !r.Manifest.ContainsSourceValues || r.Manifest.ExportPolicy != "customer-local-only" || len(r.Manifest.Changes) != 0 || len(r.Manifest.Transformations) != 0 {
		t.Fatal("incorrect untransformed run summary")
	}
	for i, e := range r.Events {
		want := [][]byte{mllp.Frame(first), mllp.Frame(second)}[i]
		if !bytes.Equal(raw(t, r, e.Source), want) || !bytes.Equal(raw(t, r, e.Sent), want) || !bytes.Equal(raw(t, r, e.Intended), want) || !bytes.Equal(raw(t, r, e.Received), ack("AA", []string{"CONTROL-A", "CONTROL-B"}[i])) || e.Delivery != "acknowledged" || e.ACK.Correlation != "matched" || e.ElapsedNS <= 0 {
			t.Fatal("missing exact transport evidence")
		}
		if r.Manifest.Mappings[i].SourceOccurrence != fmt.Sprintf("s0001-e%06d", i+1) || r.Manifest.Mappings[i].OutboundOccurrence != fmt.Sprintf("o%06d", i+1) {
			t.Fatal("source occurrence mapping lost")
		}
	}
	after, err := bundle.Open(source)
	if err != nil || after.Identity != before.Identity || r.Manifest.SourceBundleIdentity != before.Identity {
		t.Fatal("source bundle changed")
	}
}

func TestTransportOutcomesAndNoRetryAfterUncertainDelivery(t *testing.T) {
	for _, tc := range []struct {
		name        string
		response    []byte
		outcome     replay.Outcome
		uncertainty bool
		hold        bool
	}{
		{"AA", ack("AA", "DUP"), replay.Accepted, false, false},
		{"AE", ack("AE", "DUP"), replay.ApplicationError, false, false},
		{"AR", ack("AR", "DUP"), replay.Rejected, false, false},
		{"disconnect", []byte("\x0bMSH|PARTIAL"), replay.Disconnect, true, false},
		{"timeout", []byte("\x0bMSH|PARTIAL"), replay.Timeout, true, true},
		{"mismatch", ack("AA", "OTHER"), replay.ProtocolError, true, false},
		{"enhanced", ack("CA", "DUP"), replay.ProtocolError, true, false},
		{"unsolicited", append(ack("AA", "DUP"), ack("AA", "DUP")...), replay.ProtocolError, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := caseAt(t, request("DUP"), request("DUP"))
			var observed [][]byte
			done := make(chan struct{})
			address := peer(t, func(c net.Conn) {
				defer close(done)
				reader, _ := mllp.NewReader(c, 1<<20)
				frame, err := reader.ReadFrame()
				if err != nil {
					t.Error(err)
					return
				}
				observed = append(observed, frame)
				_, _ = c.Write(tc.response)
				if tc.hold {
					remainder, _ := io.ReadAll(c)
					if len(remainder) != 0 {
						t.Error("sender retried or advanced after timeout")
					}
					return
				}
				if !tc.uncertainty {
					frame, err = reader.ReadFrame()
					if err != nil {
						t.Error(err)
						return
					}
					observed = append(observed, frame)
					_, _ = c.Write(ack("AA", "DUP"))
				}
			})
			started := time.Now()
			r, _ := execute(t, source, target(address), replay.Options{})
			<-done
			if r.Events[0].Outcome != tc.outcome || !bytes.Equal(raw(t, r, r.Events[0].Received), tc.response) {
				t.Fatalf("outcome/evidence = %+v", r.Events[0])
			}
			if tc.uncertainty {
				if r.Events[0].Delivery != "uncertain" || r.Events[1].Outcome != replay.NotAttempted || len(observed) != 1 {
					t.Fatal("uncertain delivery retried or advanced")
				}
			} else if len(observed) != 2 || r.Events[0].Delivery != "acknowledged" {
				t.Fatal("acknowledged requests did not continue")
			}
			if tc.hold && (time.Since(started) < 100*time.Millisecond || time.Since(started) > 2*time.Second) {
				t.Fatal("timeout was not bounded")
			}
		})
	}
}

// persistingObserver stands in for a durable run's writer: Sent persists the
// bytes that were written before the acknowledgement is waited for, and
// persisting takes longer than the whole exchange is budgeted.
type persistingObserver struct {
	pause, recordedPause time.Duration
	ctx                  context.Context
	sent, recorded       bool
	recordedContextErr   error
}

func (o *persistingObserver) BeforeSend(string) error { return nil }
func (o *persistingObserver) Sent(string, []byte) error {
	o.sent = true
	time.Sleep(o.pause)
	return nil
}
func (o *persistingObserver) Recorded(replay.Event) error {
	o.recorded = true
	o.recordedContextErr = o.ctx.Err()
	time.Sleep(o.recordedPause)
	return nil
}

// The message timeout bounds one message and its acknowledgement on the
// network. A sender's own durability between the two is not network time: a
// receiver that answers at once is acknowledged however long the sender's
// fsyncs took, and a receiver that never answers is still an uncertain
// delivery, bounded by the same budget.
func TestLocalDurabilityIsNotChargedToTheMessageTimeout(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   bool
		outcome  replay.Outcome
		delivery string
	}{
		{"an immediate acknowledgement outlives persistence", true, replay.Accepted, "acknowledged"},
		{"a silent receiver is still an uncertain delivery", false, replay.Timeout, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := caseAt(t, request("PERSISTED"))
			address := peer(t, func(c net.Conn) {
				reader, _ := mllp.NewReader(c, 1<<20)
				if _, err := reader.ReadFrame(); err != nil {
					t.Error(err)
					return
				}
				if !tc.answer {
					// Hold the connection open so the budget, not a
					// disconnection, is what ends the wait.
					if remainder, _ := io.ReadAll(c); len(remainder) != 0 {
						t.Error("sender retried after an uncertain delivery")
					}
					return
				}
				_, _ = c.Write(ack("AA", "PERSISTED"))
			})
			endpoint := target(address)
			budget, err := time.ParseDuration(endpoint.MessageTimeout)
			if err != nil {
				t.Fatal(err)
			}
			// Twice the message timeout: the window is exhausted by
			// persistence alone before the acknowledgement is read.
			pause := 2 * budget
			plan, err := replay.Prepare(source, endpoint, replay.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			// Slow post-exchange persistence is deliberately longer than the
			// old whole-execution wall cap permitted. It cannot change which
			// timer ended the network exchange.
			observer := &persistingObserver{ctx: ctx, pause: pause, recordedPause: 4 * budget}
			path := filepath.Join(t.TempDir(), "run")
			r, err := replay.ExecuteObserved(ctx, plan, path, nil, nil, observer)
			if err != nil {
				t.Fatal(err)
			}
			// The same invariant the shared execute helper asserts: what the
			// run reports is what the finalized evidence reopens as.
			verified, err := replay.Open(path)
			if err != nil {
				t.Fatalf("final run does not reopen: %v; events=%+v", err, r.Events)
			}
			if r.Identity != verified.Identity || !reflect.DeepEqual(r.Events, verified.Events) {
				t.Fatal("reader changed recorded evidence")
			}
			e := r.Events[0]
			if e.Outcome != tc.outcome || e.Delivery != tc.delivery {
				t.Fatalf("persistence was charged to the network budget: %+v", e)
			}
			// Assert the timer that ended the exchange, not elapsed time for
			// filesystem setup, syncing/finalization, or scheduler pauses. An
			// outer deadline or a peer disconnect cannot stand in for the
			// message read timeout. Callback markers also prevent a skipped
			// durability hook from making the positive branch vacuously pass.
			if !observer.sent || !observer.recorded || observer.recordedContextErr != nil {
				t.Fatalf("exchange did not finish before the run deadline through both durability hooks: %+v", observer)
			}
			if !tc.answer && (e.TransportError == nil || e.TransportError.Phase != "read" || e.TransportError.Class != "timeout") {
				t.Fatalf("silent receiver did not end on the message read timeout: %+v", e.TransportError)
			}
		})
	}
}

func TestConnectionRefusedIsNotSentAndRunStillFinalizes(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	r, _ := execute(t, caseAt(t, request("PRIVATE")), target(address), replay.Options{})
	e := r.Events[0]
	if e.Outcome != replay.ConnectionRefused || e.Delivery != "not_sent" || e.Sent.Size != 0 || e.Received.Size != 0 || e.TransportError.Class != "connection_refused" {
		t.Fatalf("wrong refused outcome: %+v", e)
	}
}

// net.Pipe makes the accepted prefix independent of kernel send-buffer sizes.
// Map its closed-pipe error to the closed-connection error returned by net.Conn.
type disconnectedPipe struct{ net.Conn }

func (c disconnectedPipe) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	if err != nil {
		return n, net.ErrClosed
	}
	return n, nil
}

func TestMidMessageDisconnectRetainsOnlySyscallAcceptedPrefix(t *testing.T) {
	message := request("PARTIAL")
	plan, err := replay.Prepare(caseAt(t, message, request("NEXT")), target("127.0.0.1:2575"), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	var received [32]byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		if _, err := io.ReadFull(server, received[:]); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "run")
	if _, err := replay.ExecuteWithConnectionForTest(ctx, plan, path, disconnectedPipe{client}); err != nil {
		t.Fatal(err)
	}
	<-done
	r, err := replay.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := r.Events[0]
	if e.Outcome != replay.Disconnect || e.Delivery != "uncertain" || e.Sent.Size != 32 || !bytes.Equal(raw(t, r, e.Sent), received[:]) || !bytes.Equal(raw(t, r, e.Intended), mllp.Frame(message)) {
		t.Fatalf("missing partial sent evidence: %+v", e)
	}
	if r.Events[1].Outcome != replay.NotAttempted || r.Events[1].Sent.Size != 0 {
		t.Fatal("replay continued after uncertain delivery")
	}
}

func TestTransformsPreserveDuplicatesAndRecordEveryChangedField(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/fixtures/replay-duplicates.mllp")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("../../testdata/fixtures/replay-transformed.mllp")
	if err != nil {
		t.Fatal(err)
	}
	source := caseAt(t, fixture)
	var received []byte
	done := make(chan struct{})
	address := peer(t, func(c net.Conn) {
		defer close(done)
		reader, _ := mllp.NewReader(c, 1<<20)
		for range 2 {
			frame, err := reader.ReadFrame()
			if err != nil {
				t.Error(err)
				return
			}
			received = append(received, frame...)
			_, _ = c.Write(ack("AA", "READMIT000001"))
		}
	})
	r, _ := execute(t, source, target(address), replay.Options{Transformations: []replay.Transformation{{Name: "rebase-control-ids"}, {Name: "shift-timestamps", Shift: "24h"}}})
	<-done
	if !bytes.Equal(received, expected) || len(r.Manifest.Changes) != 6 {
		t.Fatalf("transformed wire or complete change inventory differs: changes=%+v", r.Manifest.Changes)
	}
	want := []struct{ selector, old, new string }{
		{"MSH[1]-10[1]", "DUPLICATE", "READMIT000001"}, {"MSH[1]-7[1]", "20260101120000", "20260102120000"}, {"SCH[1]-11[1].4", "20260102120000", "20260103120000"},
		{"MSH[1]-10[1]", "DUPLICATE", "READMIT000001"}, {"MSH[1]-7[1]", "20260101130000", "20260102130000"}, {"SCH[1]-11[1].4", "20260102130000", "20260103130000"},
	}
	for i, change := range r.Manifest.Changes {
		w := want[i]
		if change.Selector != w.selector || string(change.Old) != w.old || string(change.New) != w.new || change.OldState != hl7.Present || change.NewState != hl7.Present {
			t.Errorf("change %d differs: %+v", i, change)
		}
	}
	if string(r.Events[0].ControlID) != string(r.Events[1].ControlID) {
		t.Fatal("intentional duplicate control IDs erased")
	}
}

func TestPreparationSelectionAndDefaultValues(t *testing.T) {
	source := caseAt(t, request("A"), request("B"))
	config := target("must-not-resolve.invalid:2575")
	config.ApprovedTransport = true
	plan, err := replay.Prepare(source, config, replay.Options{Occurrences: []string{"s0002-e000001", "s0001-e000001"}})
	if err != nil || plan.Count() != 2 {
		t.Fatalf("preparation tried network or rejected valid selection: %v", err)
	}
	if plan.Mappings()[0].SourceOccurrence != "s0001-e000001" {
		t.Fatal("selection reordered source evidence")
	}
	out, _ := plan.Outbound("o000001")
	out[0] = 0
	out, _ = plan.Outbound("o000001")
	if out[0] != 0x0b {
		t.Fatal("plan payload mutated through read API")
	}
	for _, options := range []replay.Options{{Occurrences: []string{"s0001-e000001", "s0001-e000001"}}, {Occurrences: []string{"s9999-e000001"}}, {Transformations: []replay.Transformation{{Name: "invented"}}}} {
		if _, err := replay.Prepare(source, config, options); err == nil {
			t.Fatal("invalid selection/transformation accepted")
		}
	}
	for _, suffix := range []string{"|||AL|NE", "|||\"\"|"} {
		message := bytes.Replace(request("ENHANCED"), []byte("|2.5.1\r"), []byte("|2.5.1"+suffix+"\r"), 1)
		if _, err := replay.Prepare(caseAt(t, message), config, replay.Options{}); err == nil {
			t.Fatal("enhanced request accepted")
		}
	}
}

func TestExplicitTLSVerifiesCAAndHostname(t *testing.T) {
	cert, ca := certificate(t)
	for _, tc := range []struct {
		name     string
		trusted  bool
		hostname string
		accepted bool
	}{{"untrusted", false, "", false}, {"trusted", true, "", true}, {"wrong-host", true, "localhost", false}} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, err := l.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				reader, _ := mllp.NewReader(c, 1<<20)
				if _, err := reader.ReadFrame(); err == nil {
					_, _ = c.Write(ack("AA", "TLS"))
				}
			}()
			config := target(l.Addr().String())
			config.Transport = "tls"
			if tc.hostname != "" {
				config.Schema, config.Name, config.Classification = replay.TargetSchemaV3, "tls-test", replay.Nonproduction
				config.ServerName = tc.hostname
			}
			if tc.trusted {
				path := filepath.Join(t.TempDir(), "ca.pem")
				if err := os.WriteFile(path, ca, 0600); err != nil {
					t.Fatal(err)
				}
				config.CAFile = path
			}
			r, _ := execute(t, caseAt(t, request("TLS")), config, replay.Options{})
			<-done
			if r.Successful() != tc.accepted {
				t.Fatalf("TLS trust decision wrong: %+v", r.Events)
			}
			if !tc.accepted && (r.Events[0].Outcome != replay.TLSError || r.Events[0].TransportError.Class != "tls_verification" || r.Events[0].Sent.Size != 0) {
				t.Fatal("unverified TLS did not fail closed")
			}
			if tc.trusted {
				sum := sha256.Sum256(ca)
				if r.Manifest.Target.CASHA256 != hex.EncodeToString(sum[:]) {
					t.Fatal("actual CA digest not recorded")
				}
			}
		})
	}
}

func certificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "readmit synthetic test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := tls.X509KeyPair(ca, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	return cert, ca
}

func TestTargetStrictnessAndSourceDestinationAliases(t *testing.T) {
	source := caseAt(t, request("A"))
	for _, change := range []func(*replay.Target){func(t *replay.Target) { t.TestEndpoint = false }, func(t *replay.Target) { t.Address = "192.0.2.1:2575" }, func(t *replay.Target) { t.Address = "localhost:2575" }, func(t *replay.Target) { t.Address = "127.0.0.1\x1b:2575" }, func(t *replay.Target) { t.MessageTimeout = "0s" }, func(t *replay.Target) { t.ConnectTimeout = "10m" }, func(t *replay.Target) { t.Transport = "insecure" }} {
		config := target("127.0.0.1:2575")
		change(&config)
		if _, err := replay.Prepare(source, config, replay.Options{}); err == nil {
			t.Fatal("unsafe target accepted")
		}
	}
	data, _ := json.Marshal(target("127.0.0.1:2575"))
	data = append(data[:len(data)-1], []byte(",\"insecure_skip_verify\":true}")...)
	path := filepath.Join(t.TempDir(), "target.json")
	_ = os.WriteFile(path, data, 0600)
	if _, err := replay.ReadTarget(path); err == nil {
		t.Fatal("unknown target field accepted")
	}
	plan, err := replay.Prepare(source, target("127.0.0.1:2575"), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{filepath.Join(source, "run"), filepath.Join(source, "payloads", "run")} {
		if _, err := replay.Execute(context.Background(), plan, out); err == nil {
			t.Fatal("run modified source bundle")
		}
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(t.TempDir(), "source-alias")
		if err := os.Symlink(source, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := replay.Execute(context.Background(), plan, filepath.Join(alias, "run")); err == nil {
			t.Fatal("symlink bypassed source protection")
		}
	}
	if _, err := bundle.Open(source); err != nil {
		t.Fatal("source no longer validates")
	}
}

func TestCancelledRunFinalizesWithoutSending(t *testing.T) {
	plan, err := replay.Prepare(caseAt(t, request("CANCEL")), target("127.0.0.1:2575"), replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "run")
	r, err := replay.Execute(ctx, plan, path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Events[0].Outcome != replay.Cancelled || r.Events[0].Delivery != "not_sent" {
		t.Fatal("cancelled run attempted delivery")
	}
	if _, err := replay.Open(path); err != nil {
		t.Fatal(err)
	}
}
