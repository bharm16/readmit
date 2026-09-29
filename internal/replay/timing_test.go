package replay_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// generatedCaseAt is a case the case generator wrote, as its provenance
// records: its generation declared how it is sent, which the case never says.
func generatedCaseAt(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generated")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	provenance := bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 1, BaseTime: base, GeneratorVersion: casegen.Version, ProfileVersion: "owned-casegen-siu+1"}}
	if _, err := bundle.Write(path, []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.CR}}}, provenance); err != nil {
		t.Fatal(err)
	}
	return path
}

// A raw replay never applies a generated case's scenario timing, so it sends
// one only as the byte-only replay its caller chose. Without that choice the
// send is refused before anything is decided, opened or written; with it the
// run is a readmit-byte-only-run/v1, which says the timing was not applied and
// which only OpenByteOnly reads. The choice is refused where it means nothing:
// for any other case, and for a connected send, which keeps the schedule.
func TestAGeneratedCaseIsSentRawOnlyAsAChosenByteOnlyReplay(t *testing.T) {
	source := generatedCaseAt(t, request("GENERATED"))
	preview, err := replay.Preview(t.Context(), source, target("127.0.0.1:9"), replay.Options{}, replay.SendOptions{})
	if err != nil || preview.ScenarioTiming() != replay.TimingRequired {
		t.Fatalf("the preview does not say the timing is required: %v", err)
	}

	decisions := 0
	record := replay.SendOptions{Record: func(sendpolicy.Decision) error { decisions++; return nil }}
	refused := peer(t, func(net.Conn) { t.Error("a refused byte-only replay connected") })
	plan, err := replay.PrepareSend(t.Context(), source, target(refused), replay.Options{}, record)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "refused")
	if _, err := replay.Send(t.Context(), plan, output, record); !errors.Is(err, replay.ErrScenarioTiming) || decisions != 0 {
		t.Fatalf("an unchosen byte-only replay: %v, %d decisions", err, decisions)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("a refused send wrote its run")
	}

	accepted := peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 4096)
		if _, err := reader.ReadFrame(); err == nil {
			_, _ = conn.Write(ack("AA", "GENERATED"))
		}
	})
	chosen := replay.Options{IgnoreScenarioTiming: true}
	plan, err = replay.PrepareSend(t.Context(), source, target(accepted), chosen, record)
	if err != nil || plan.ScenarioTiming() != replay.TimingNotApplied {
		t.Fatalf("the chosen byte-only replay: %v", err)
	}
	output = filepath.Join(t.TempDir(), "byte-only")
	run, err := replay.Send(t.Context(), plan, output, record)
	if err != nil || !run.Successful() || run.Manifest.Schema != replay.ByteOnlySchema || run.Manifest.ScenarioTiming != replay.TimingNotApplied {
		t.Fatalf("the byte-only run: %v %+v", err, run)
	}
	if reopened, err := replay.OpenByteOnly(output); err != nil || reopened.Manifest.ScenarioTiming != replay.TimingNotApplied || reopened.Identity != run.Identity {
		t.Fatalf("the byte-only run did not reopen as one: %v", err)
	}
	if _, err := replay.Open(output); err == nil {
		t.Fatal("a reader of ordinary runs took a byte-only replay")
	}

	if _, err := replay.Prepare(caseAt(t, request("IMPORTED")), target("127.0.0.1:9"), chosen); err == nil {
		t.Fatal("an imported case was prepared to ignore scenario timing it does not have")
	}
	if _, err := replay.PrepareScoped(source, target("127.0.0.1:9"), chosen); err == nil {
		t.Fatal("a connected send was prepared to ignore its schedule")
	}
}
