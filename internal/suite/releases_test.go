package suite_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func releasedFixture(t *testing.T) (string, suite.Document, expectation.Release) {
	t.Helper()
	dir, doc := fixture(t, "127.0.0.1:1")
	raw, e := os.ReadFile(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	pins := []profileversion.Version{}
	c, e := expectation.Review("booking", raw, pins, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	r, e := expectation.Approve("booking", raw, pins, nil, c.Identity, "reviewer", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	if e = expectation.Save(filepath.Join(dir, "release.json"), r); e != nil {
		t.Fatal(e)
	}
	refs := suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{{Test: "booking", Release: "release.json", Identity: r.Identity()}}}
	write(t, filepath.Join(dir, "releases.json"), refs)
	return dir, doc, r
}
func TestApprovedSuitePinsHistoryAndReportsImpactWithoutMovingPins(t *testing.T) {
	dir, doc, first := releasedFixture(t)
	out := filepath.Join(dir, "prepared")
	if _, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out, References: filepath.Join(dir, "releases.json")}); e != nil {
		t.Fatal(e)
	}
	retained, e := expectation.Read(filepath.Join(out, "release-booking.json"))
	if e != nil || retained.Identity() != first.Identity() {
		t.Fatal("released approval not retained", e)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "booking.json"))
	changed := bytes.Replace(raw, []byte(`"AA"`), []byte(`"AE"`), 1)
	next, e := expectation.Review("booking", changed, first.Profiles, &first, false)
	if e != nil {
		t.Fatal(e)
	}
	second, e := expectation.Approve("booking", changed, first.Profiles, &first, next.Identity, "reviewer", "changed")
	if e != nil {
		t.Fatal(e)
	}
	report, e := suite.AssessReleases(filepath.Join(dir, "suite.json"), filepath.Join(dir, "releases.json"), first, second, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(report.Tests) != 1 || report.Tests[0].State != "affected" || report.Tests[0].Rows != 1 || report.Tests[0].Pinned != first.Identity() {
		t.Fatalf("%+v", report)
	}
	// A data table may repeat an approved value, but cannot silently approve another.
	value := "AE"
	doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"ack": {Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}
	write(t, filepath.Join(dir, "suite.json"), doc)
	refused := filepath.Join(dir, "refused")
	if _, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: refused, References: filepath.Join(dir, "releases.json")}); e == nil {
		t.Fatal("table weakened approval")
	}
	if _, e := os.Stat(refused); !os.IsNotExist(e) {
		t.Fatal("failed preparation remained")
	}
	value = "AA"
	write(t, filepath.Join(dir, "suite.json"), doc)
	if _, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: refused, References: filepath.Join(dir, "releases.json")}); e != nil {
		t.Fatal("identical value refused", e)
	}
}
func TestReleasedSuiteRejectsChangedTemplatePinAndIncompleteCoverage(t *testing.T) {
	for _, kind := range []string{"template", "release", "missing", "unknown", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			dir, _, r := releasedFixture(t)
			switch kind {
			case "template":
				raw, _ := os.ReadFile(filepath.Join(dir, "booking.json"))
				os.WriteFile(filepath.Join(dir, "booking.json"), bytes.Replace(raw, []byte(`"AA"`), []byte(`"AE"`), 1), 0600)
			case "release":
				r.Baseline.Rationale = "changed record"
				raw, _ := r.Encode()
				os.WriteFile(filepath.Join(dir, "release.json"), raw, 0600)
			case "missing":
				write(t, filepath.Join(dir, "releases.json"), suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{}})
			case "unknown":
				raw, _ := os.ReadFile(filepath.Join(dir, "releases.json"))
				os.WriteFile(filepath.Join(dir, "releases.json"), bytes.Replace(raw, []byte(`"identity":`), []byte(`"extra":true,"identity":`), 1), 0600)
			case "truncated":
				os.WriteFile(filepath.Join(dir, "release.json"), []byte(`{"schema":`), 0600)
			}
			if _, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: filepath.Join(dir, "out"), References: filepath.Join(dir, "releases.json")}); e == nil {
				t.Fatal("unapproved suite accepted")
			}
		})
	}
}

func TestApprovedSuiteCancellationRetainsApprovalAndUncertainEvidence(t *testing.T) {
	received := make(chan struct{})
	address := peer(t, func(conn net.Conn) {
		reader, _ := mllp.NewReader(conn, 1<<20)
		if _, err := reader.ReadFrame(); err == nil {
			close(received)
			_, _ = io.Copy(io.Discard, conn)
		}
	})
	dir, _, released := releasedFixture(t)
	raw, err := os.ReadFile(filepath.Join(dir, "east.json"))
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	target["address"] = address
	write(t, filepath.Join(dir, "east.json"), target)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-received:
			cancel()
		case <-ctx.Done():
		}
	}()
	out := filepath.Join(dir, "cancelled")
	report, err := suite.Run(ctx, suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out, References: filepath.Join(dir, "releases.json")})
	if err != nil || len(report.Jobs) != 1 || report.Jobs[0].Run == nil || report.Jobs[0].Run.State != durablerun.DeliveryUncertain || report.Jobs[0].Run.StopReason != durablerun.Cancelled {
		t.Fatalf("%+v %v", report, err)
	}
	retained, err := expectation.Read(filepath.Join(out, "release-booking.json"))
	if err != nil || retained.Identity() != released.Identity() {
		t.Fatal("approval lost on cancellation", err)
	}
	recovered, err := durablerun.Recover(filepath.Join(out, "runs", "booking-one"))
	if err != nil || recovered.Uncertain != 1 || recovered.SafeToRepeat {
		t.Fatalf("%+v %v", recovered, err)
	}
	if _, err := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out, References: filepath.Join(dir, "releases.json")}); err == nil {
		t.Fatal("cancelled suite resumed")
	}
}
