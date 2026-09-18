package tests

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

func declaredFaultPolicy(address string) string {
	return fmt.Sprintf(`{"schema":"readmit-receiver-policy/v3","name":"controlled-fault","source_label":"synthetic-test","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]},"enhanced_acknowledgement":{"operator":"unsupported","accept_code":"","application_code":"","application_delivery":"","application_endpoint":"","approved_transport":false},"faults":{"environment_class":"nonproduction","approved_test_endpoints":[%q],"steps":[{"message":1,"stage":"application","action":"reject","delay_ms":0}]}}`, address)
}

func TestCollectFaultExecutableRejectsThenRecovers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	output := filepath.Join(dir, "case")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "collect", "--address", address, "--policy", policyFile(t, dir, declaredFaultPolicy(address)), "--output", output, "--max-messages", "2")
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(ready, "Listening: ") {
		t.Fatalf("not ready: %q %v", ready, err)
	}
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	frames, _ := mllp.NewReader(conn, 4096)
	for _, want := range []string{"AR", "AA"} {
		if _, err := conn.Write(mllp.Frame([]byte(collectADT))); err != nil {
			t.Fatal(err)
		}
		response, err := frames.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		doc, err := hl7.Parse(response, hl7.Options{Format: hl7.MLLP})
		if err != nil {
			t.Fatal(err)
		}
		if got := string(doc.Bytes(doc.Messages[0].Segments[1].Field(1).Span)); got != want {
			t.Fatalf("ACK %s want %s", got, want)
		}
	}
	remaining, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("collect fault: %v %s", err, diagnostic.String())
	}
	if !strings.Contains(string(remaining), "Collection: readmit-collection/v3") || strings.Contains(string(remaining), "COLLECT-001") || diagnostic.Len() != 0 {
		t.Fatalf("fault output: %s %s", remaining, diagnostic.String())
	}
	b, err := bundle.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	if b.Collection.Received[0].Fault.Status != "completed" || b.Collection.Received[1].Fault != nil {
		t.Fatal("fault execution evidence lost")
	}
}

func TestCollectFaultExecutableRefusesUnapprovedEndpointsAndProduction(t *testing.T) {
	for _, kind := range []string{"different-endpoint", "production", "wildcard", "ephemeral"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			address := "127.0.0.1:2576"
			declared := declaredFaultPolicy("127.0.0.1:2575")
			switch kind {
			case "production":
				address = "127.0.0.1:2575"
				declared = strings.Replace(declared, `"nonproduction"`, `"production"`, 1)
			case "wildcard":
				address = "0.0.0.0:2575"
			case "ephemeral":
				address = "127.0.0.1:0"
			}
			stdout, stderr, err := run(t, "collect", "--address", address, "--approved-bind", "--policy", policyFile(t, dir, declared), "--output", filepath.Join(dir, "case"), "--max-messages", "1")
			if err == nil || stdout != "" || strings.Contains(stderr, dir) || !strings.Contains(stderr, "fault") {
				t.Fatalf("unsafe simulation accepted or leaked: %v %q %q", err, stdout, stderr)
			}
		})
	}
}
