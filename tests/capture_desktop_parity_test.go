package tests

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

// fixtureExchange sends frozen SIU fixtures, one after another on one
// connection, to a listening fixture and returns what each acknowledgement
// said: its MSA segment and its receipt, with the random session named SESSION.
func fixtureExchange(t *testing.T, address string, fixtures ...string) []string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	frames, err := mllp.NewReader(conn, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var answered []string
	for _, fixture := range fixtures {
		payload, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(mllp.Frame(payload)); err != nil {
			t.Fatal(err)
		}
		ack, err := frames.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		var said []string
		for segment := range strings.SplitSeq(strings.TrimRight(string(ack), "\r"), "\r") {
			switch {
			case strings.HasPrefix(segment, "MSA|"):
				said = append(said, segment)
			case strings.HasPrefix(segment, "ZRT|"):
				fields := strings.Split(segment, "|")
				if len(fields) != 4 {
					t.Fatalf("receipt %q", segment)
				}
				fields[2] = "SESSION"
				said = append(said, strings.Join(fields, "|"))
			}
		}
		answered = append(answered, strings.Join(said, "\r"))
	}
	return answered
}

// windowListening waits until the window's running fixture reports the
// address it bound, as Sample data reads it.
func windowListening(t *testing.T, app *desktop.App, answered chan desktop.SampleFixtureResult) string {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case result := <-answered:
			t.Fatalf("the window's fixture answered before it listened: %+v", result)
		default:
		}
		if progress := app.CaptureProgress(); progress.State == desktop.Completed && progress.Progress != nil && progress.Progress.BoundAddress != "" {
			return progress.Progress.BoundAddress
		}
	}
	t.Fatal("the window's fixture never reported where it listens")
	return ""
}

// Sample data's SIU fixture runs the listen operation `readmit listen` runs,
// so what is left to prove is what crosses between the two on disk and on
// the wire: the window reports where it listens as the command prints it, on
// the declared host and the port it chose, once its initial ledger is
// installed; it acknowledges the frozen exchange as the fixture does; it
// exports the hand-authored ledger for its mode; and it seals a case the
// command line reads exactly as the window described it. `readmit listen`'s
// own process contract is TestListenExecutableExportsBothLedgersAndReopensRecordedCase's.
func TestTheWindowsFixtureListenIsTheCommandLinesListen(t *testing.T) {
	for _, mode := range []string{"fixed", "defective"} {
		t.Run(mode, func(t *testing.T) {
			workspace := newProject(t)
			app := desktopApp(t, t.TempDir())
			answered := make(chan desktop.SampleFixtureResult, 1)
			go func() {
				answered <- app.StartSampleFixture(desktop.SampleFixtureRequest{Context: desktop.RequestContext{Project: workspace},
					Mode: mode, Address: "127.0.0.1:0", MaxMessages: 2, IdleTimeout: "10s"})
			}()
			bound := windowListening(t, app, answered)
			if host, port, err := net.SplitHostPort(bound); err != nil || host != "127.0.0.1" || port == "0" {
				t.Fatalf("the window bound %q for a declared 127.0.0.1:0", bound)
			}
			acks := fixtureExchange(t, bound, "listen-s12.hl7", "listen-s13.hl7")
			result := <-answered
			if result.State != desktop.Completed || result.CaseEntry == "" || result.Ledger == nil {
				t.Fatalf("the window's listen: %+v", result)
			}
			for i, control := range []string{"LISTEN-BOOK", "LISTEN-MOVE"} {
				msa, _, _ := strings.Cut(acks[i], "\r")
				if fields := strings.Split(msa, "|"); len(fields) < 3 || fields[0] != "MSA" || fields[1] != "AA" || fields[2] != control {
					t.Fatalf("acknowledgement %d: %q", i, acks[i])
				}
			}

			// The exported ledger is the hand-authored expectation for the mode.
			expected := readStrictDocument[[]observation.Record](t, filepath.Join("..", "testdata", "fixtures", "listen-"+mode+".json"))
			ledger, err := observation.Read(filepath.Join(workspace, result.ObservationEntry))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ledger.Records, expected) || string(ledger.Mode) != mode || !ledger.Consistent {
				t.Fatalf("exported ledger %+v, want %+v", ledger, expected)
			}

			// The command line reads the window's case as the window reported it.
			sealed, err := bundle.Open(filepath.Join(workspace, result.CaseEntry))
			if err != nil || !reflect.DeepEqual(sealed.Observation.Records, expected) {
				t.Fatalf("the sealed ledger is not the exported one: %v", err)
			}
			timeline, stderr, err := run(t, "timeline", filepath.Join(workspace, result.CaseEntry))
			if err != nil || stderr != "" {
				t.Fatalf("timeline of the window's case: %v %s", err, stderr)
			}
			for _, line := range []string{
				"Bundle: " + sealed.Identity,
				"Observation: " + result.Ledger.Schema,
				"Observation profile: " + result.Ledger.Profile,
				"Receiver mode: " + result.Ledger.Mode,
				fmt.Sprintf("Processed occurrences: %d", result.Ledger.Processed),
				fmt.Sprintf("Ledger records: %d", result.Ledger.Records),
				fmt.Sprintf("Consistent: %t", result.Ledger.Consistent),
			} {
				if !strings.Contains(timeline, line+"\n") {
					t.Errorf("the window reported %q, which `readmit timeline` does not read", line)
				}
			}
		})
	}
}
