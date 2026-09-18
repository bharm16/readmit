package receiver_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/receiver"
)

func faultPolicy(address, action, stage string, delay int) string {
	base := strings.Replace(enhancedSameConnection, "readmit-receiver-policy/v2", "readmit-receiver-policy/v3", 1)
	return strings.TrimSuffix(base, "}") + fmt.Sprintf(`,"faults":{"environment_class":"nonproduction","approved_test_endpoints":[%q],"steps":[{"message":1,"stage":%q,"action":%q,"delay_ms":%d}]}}`, address, stage, action, delay)
}

func collectFault(t *testing.T, action, stage string, delay, count int) *collecting {
	t.Helper()
	return collectFaultPolicy(t, count, func(address string) string { return faultPolicy(address, action, stage, delay) })
}

func collectFaultPolicy(t *testing.T, count int, declared func(string) string) *collecting {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	config := receiver.CollectorConfig{Policy: policy(t, declared(listener.Addr().String())), OutputPath: filepath.Join(t.TempDir(), "collected"), MaxMessages: count, MaxFrameBytes: 1 << 20, IdleTimeout: time.Second, ApplicationTimeout: time.Second}
	c, err := receiver.NewCollector(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &collecting{address: listener.Addr().String(), output: config.OutputPath, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(h.done); _, h.err = c.Serve(ctx, listener) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func TestFaultRejectPreservesEnhancedStagesAndRecovers(t *testing.T) {
	h := collectFault(t, "reject", "application", 0, 2)
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("FAULT-1", "AL", "ER")))
	ack(t, reader, "CA", "FAULT-1")
	ack(t, reader, "AR", "FAULT-1")
	send(t, conn, mllp.Frame(enhanced("FAULT-2", "AL", "AL")))
	ack(t, reader, "CA", "FAULT-2")
	ack(t, reader, "AA", "FAULT-2")
	record := h.finished(t).Collection
	if record.Schema != "readmit-collection/v3" || record.ApplicationProcessing != collection.NoApplicationProcessing || record.Received[0].Application.Code != "AR" {
		t.Fatalf("fault evidence lost: %+v", record)
	}
}

func TestFaultWireOutcomesRetainEvidenceAndRecover(t *testing.T) {
	for _, action := range []string{"delay", "disconnect", "malformed-ack", "missing-response"} {
		t.Run(action, func(t *testing.T) {
			wait := 0
			if action == "delay" || action == "missing-response" {
				wait = 60
			}
			h := collectFault(t, action, "application", wait, 2)
			conn, reader := h.dial(t)
			request := mllp.Frame([]byte(plainADT))
			start := time.Now()
			send(t, conn, request)
			response, err := reader.ReadFrame()
			switch action {
			case "delay":
				if err != nil {
					t.Fatal(err)
				}
				code, control, _ := msa(t, response)
				if code != "AA" || control != "COLLECT-001" {
					t.Fatalf("delayed ACK changed: %q", response)
				}
			case "malformed-ack":
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(response, mllp.Frame([]byte("READMIT MALFORMED ACK\r"))) {
					t.Fatalf("unexpected malformed response: %q", response)
				}
				if _, err := hl7.Parse(response, hl7.Options{Format: hl7.MLLP}); err == nil {
					t.Fatal("malformed ACK is valid HL7")
				}
			default:
				if err != io.EOF || len(response) != 0 {
					t.Fatalf("expected disconnect without a response: %q %v", response, err)
				}
			}
			if wait > 0 && time.Since(start) < time.Duration(wait)*time.Millisecond {
				t.Fatal("configured wait was not observed")
			}
			conn.Close()
			next, nextReader := h.dial(t)
			send(t, next, mllp.Frame(enhanced("RECOVER", "AL", "AL")))
			ack(t, nextReader, "CA", "RECOVER")
			ack(t, nextReader, "AA", "RECOVER")
			b := h.finished(t)
			first := b.Collection.Received[0]
			if first.Fault == nil || first.Fault.Action != action || first.Fault.Status != "completed" || b.Collection.Received[1].Fault != nil {
				t.Fatalf("fault plan/outcome: %+v", b.Collection.Received)
			}
			wantCode := "none"
			if action == "delay" {
				wantCode = "AA"
			}
			if first.Application.Code != wantCode {
				t.Fatalf("fault became success: %+v", first)
			}
			raw, err := b.Raw("s0001-e000001")
			if err != nil || !bytes.Equal(raw, request) {
				t.Fatal("original request was not retained")
			}
			if action == "malformed-ack" {
				raw, err := b.Raw("s0001-e000002")
				if err != nil || !bytes.Equal(raw, response) {
					t.Fatal("malformed wire response was not retained")
				}
			}
		})
	}
}

func TestFaultCancellationRetainsUnacknowledgedEvidence(t *testing.T) {
	for _, action := range []string{"delay", "missing-response"} {
		t.Run(action, func(t *testing.T) {
			h := collectFault(t, action, "application", 30000, 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame(enhanced("CANCEL", "AL", "AL")))
			ack(t, reader, "CA", "CANCEL") // proves the inbound frame was committed
			h.cancel()
			b := h.finished(t)
			first := b.Collection.Received[0]
			if first.Accept.Code != "CA" || first.Application.Code != "none" || first.Fault.Status != "interrupted" {
				t.Fatalf("cancelled fault evidence: %+v", first)
			}
		})
	}
}

func TestFaultDoesNotInventUnrequestedStages(t *testing.T) {
	for _, action := range []string{"delay", "reject", "disconnect", "malformed-ack", "missing-response"} {
		t.Run(action, func(t *testing.T) {
			wait := 0
			if action == "delay" || action == "missing-response" {
				wait = 30000
			}
			h := collectFault(t, action, "application", wait, 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame(enhanced("NO-APP", "AL", "NE")))
			ack(t, reader, "CA", "NO-APP")
			first := h.finished(t).Collection.Received[0]
			if first.Application.Code != "none" || first.Fault.Status != "not-requested" {
				t.Fatalf("unrequested stage was changed: %+v", first)
			}
		})
	}
}

func TestFaultAcceptStageStopsApplicationAndRecordsDistinctRejection(t *testing.T) {
	for _, action := range []string{"reject", "disconnect", "malformed-ack"} {
		t.Run(action, func(t *testing.T) {
			h := collectFault(t, action, "accept", 0, 1)
			conn, reader := h.dial(t)
			send(t, conn, mllp.Frame(enhanced("ACCEPT-FAULT", "AL", "AL")))
			switch action {
			case "reject":
				ack(t, reader, "CR", "ACCEPT-FAULT")
				ack(t, reader, "AA", "ACCEPT-FAULT")
			case "disconnect":
				if raw, err := reader.ReadFrame(); err != io.EOF || len(raw) != 0 {
					t.Fatalf("unexpected response %q %v", raw, err)
				}
			case "malformed-ack":
				if _, err := reader.ReadFrame(); err != nil {
					t.Fatal(err)
				}
			}
			first := h.finished(t).Collection.Received[0]
			if action == "reject" {
				if first.Accept.Code != "CR" || first.Application.Code != "AA" {
					t.Fatalf("stages conflated: %+v", first)
				}
			} else if first.Accept.Code != "none" || first.Application.Code != "none" {
				t.Fatalf("application stage was fabricated: %+v", first)
			}
		})
	}
}

func TestFaultUnapprovedListenerRefusedBeforeReceive(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c, err := receiver.NewCollector(receiver.CollectorConfig{Policy: policy(t, faultPolicy("127.0.0.1:1", "reject", "application", 0)), OutputPath: filepath.Join(t.TempDir(), "case"), MaxFrameBytes: 1024, IdleTimeout: time.Second, ApplicationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Serve(context.Background(), listener)
	if err == nil || b != nil {
		t.Fatal("unapproved fault listener served")
	}
}

func TestFaultMalformedApplicationUsesApprovedSeparateEndpoint(t *testing.T) {
	sink := newApplicationSink(t)
	h := collectFaultPolicy(t, 1, func(address string) string {
		declared := faultPolicy(address, "malformed-ack", "application", 0)
		declared = strings.Replace(declared, `"application_delivery":"same-connection"`, `"application_delivery":"separate-endpoint"`, 1)
		declared = strings.Replace(declared, `"application_endpoint":""`, `"application_endpoint":"`+sink.address+`"`, 1)
		return strings.Replace(declared, fmt.Sprintf(`"approved_test_endpoints":[%q]`, address), fmt.Sprintf(`"approved_test_endpoints":[%q,%q]`, address, sink.address), 1)
	})
	conn, reader := h.dial(t)
	send(t, conn, mllp.Frame(enhanced("ASYNC-FAULT", "AL", "AL")))
	ack(t, reader, "CA", "ASYNC-FAULT")
	b := h.finished(t)
	frames := sink.await(t, 1)
	if len(frames) != 1 || !bytes.Equal(frames[0], mllp.Frame([]byte("READMIT MALFORMED ACK\r"))) {
		t.Fatalf("wrong asynchronous fault: %q", frames)
	}
	first := b.Collection.Received[0]
	if first.Accept.Code != "CA" || first.Application.Code != "none" || first.Fault.Status != "completed" {
		t.Fatalf("async fault became application success: %+v", first)
	}
	raw, err := b.Raw("s0001-e000003")
	if err != nil || !bytes.Equal(raw, frames[0]) {
		t.Fatal("async fault bytes not retained")
	}
}
