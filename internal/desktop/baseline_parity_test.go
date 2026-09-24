package desktop_test

// Inspecting a retained baseline or a released test version in the window is
// `readmit baseline show` and `readmit expectation show`: the same reader and
// the same inspection over the same retained file, refused for the same
// reasons, with the command's machine output keeping exactly its members and
// neither path changing the historical file it reads. The checked-in v1
// documents under testdata stand for revisions retained before this window
// could inspect them; their identities are pinned.

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
)

const inspectedSpec = `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`

// The identities the checked-in historical revisions were approved and
// released under.
const (
	historicalBaselineIdentity = "2abaea062e55415f5c8aced321c54e468cb09878d7927a370c4430669609640e"
	historicalReleaseIdentity  = "1afc4967fd655dcd9d4a3dd85afcf4074a49a989b9dffd2234d4545ee09a15ab"
)

// commandLine runs one readmit command in process and returns what it
// printed and the refusal it reported.
func commandLine(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cli.Execute("dev", args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// inspection names the command that shows a retained baseline or release.
func inspection(release bool) string {
	if release {
		return "expectation"
	}
	return "baseline"
}

// printedBaseline is `readmit baseline show`'s whole machine output.
type printedBaseline struct {
	Schema    string              `json:"schema"`
	Approver  string              `json:"approver"`
	Rationale string              `json:"rationale"`
	Baseline  baseline.Comparison `json:"baseline"`
}

// printedRelease is `readmit expectation show`'s whole machine output.
type printedRelease struct {
	Schema       string              `json:"schema"`
	ID           string              `json:"id"`
	Identity     string              `json:"identity"`
	Parent       string              `json:"parent"`
	Approver     string              `json:"approver"`
	Rationale    string              `json:"rationale"`
	ProfileCount int                 `json:"profile_count"`
	Baseline     baseline.Comparison `json:"baseline"`
}

// shownAlike inspects one retained entry in the window and with the command
// line, holds the window's comparison to the one the command printed, and
// returns both. The command's output is decoded strictly, so a member it
// gained or lost fails here.
func shownAlike[P any](t *testing.T, app *desktop.App, root, entry string, release, reveal bool, comparison func(P) baseline.Comparison) (desktop.BaselineResult, P) {
	t.Helper()
	window := app.OpenBaseline(desktop.BaselineRequest{Workspace: root, Release: release, Previous: entry, ShowValues: reveal})
	if window.State != desktop.Completed || window.Comparison == nil {
		t.Fatalf("%s: %+v", entry, window)
	}
	args := []string{inspection(release), "show", filepath.Join(root, entry)}
	if reveal {
		args = append(args, "--show-values")
	}
	stdout, stderr, err := commandLine(t, args...)
	if err != nil {
		t.Fatalf("%s: %v: %s", entry, err, stderr)
	}
	var printed P
	if err := json.Unmarshal([]byte(stdout), &printed, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s: the command's inspection changed its members: %v", entry, err)
	}
	if shown := comparison(printed); shown.ValuesShown != reveal || string(marshal(t, *window.Comparison)) != string(marshal(t, shown)) {
		t.Fatalf("%s: the window's inspection %+v differs from the command's %+v", entry, *window.Comparison, shown)
	}
	return window, printed
}

// refusedAlike holds the window's refusal of one entry to the command's
// refusal of the same file: both refuse, and the command reports the reason
// the window shows.
func refusedAlike(t *testing.T, app *desktop.App, root, entry string, release bool) {
	t.Helper()
	window := app.OpenBaseline(desktop.BaselineRequest{Workspace: root, Release: release, Previous: entry})
	_, stderr, err := commandLine(t, inspection(release), "show", filepath.Join(root, entry))
	if window.State != desktop.Failed || window.Comparison != nil || window.Reason == "" {
		t.Fatalf("%s: the window did not refuse: %+v", entry, window)
	}
	if err == nil || stderr != "readmit: "+window.Reason+"\n" {
		t.Fatalf("%s: the command line refused differently: %v %q, the window: %q", entry, err, stderr, window.Reason)
	}
}

func TestOpenBaselineShowsWhatBaselineShowPrints(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	writeDocument(t, root, "test.json", inspectedSpec)
	request := desktop.BaselineRequest{Workspace: root, Spec: "test.json", Approver: "reviewer", Rationale: "synthetic acknowledgement verified", Output: "baseline.json"}
	review := app.ReviewBaseline(request)
	if review.State != desktop.Completed {
		t.Fatal(review)
	}
	request.Review = review.Comparison.Identity
	if approved := app.ApproveBaseline(request); approved.State != desktop.Completed {
		t.Fatal(approved)
	}
	// Historical inspection needs no candidate specification.
	if err := os.Remove(filepath.Join(root, "test.json")); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "historical.json", read(t, filepath.Join("testdata", "historical-baseline-v1.json")))
	retained := map[string]string{}
	for _, entry := range []string{"baseline.json", "historical.json"} {
		retained[entry] = read(t, filepath.Join(root, entry))
		for _, reveal := range []bool{false, true} {
			window, printed := shownAlike(t, app, root, entry, false, reveal, func(p printedBaseline) baseline.Comparison { return p.Baseline })
			if printed.Schema != "readmit-baseline-inspection/v1" || window.PreviousApprover != printed.Approver || window.PreviousRationale != printed.Rationale || window.ReleaseID != "" {
				t.Fatalf("%s: the window shows %+v, the command line prints %+v", entry, window, printed)
			}
		}
	}
	if window := app.OpenBaseline(desktop.BaselineRequest{Workspace: root, Previous: "historical.json"}); window.Comparison.Identity != historicalBaselineIdentity || window.PreviousApprover != "Local reviewer" {
		t.Fatalf("the historical revision reads as %+v", window)
	}
	// A revision never approved, a revision of a version this release cannot
	// read and a revision whose approved specification changed are refused
	// by both, with the same reason.
	writeDocument(t, root, "next.json", replaceOnce(t, retained["baseline.json"], `"readmit-baseline/v1"`, `"readmit-baseline/v2"`))
	writeDocument(t, root, "changed.json", replaceOnce(t, retained["baseline.json"], `"text":"AA"`, `"text":"AE"`))
	for _, entry := range []string{"missing.json", "next.json", "changed.json"} {
		refusedAlike(t, app, root, entry, false)
	}
	for entry, held := range retained {
		if read(t, filepath.Join(root, entry)) != held {
			t.Fatalf("inspection changed the retained revision %s", entry)
		}
	}
}

func TestOpenReleaseShowsWhatExpectationShowPrints(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	writeDocument(t, root, "test.json", inspectedSpec)
	writeDocument(t, root, "profile.json", fixture(t, "local-profile.json"))
	request := desktop.BaselineRequest{Workspace: root, Spec: "test.json", Release: true, ReleaseID: "booking", Profiles: []string{"profile.json"}, Approver: "reviewer", Rationale: "reviewed profile pin", Output: "release.json"}
	review := app.ReviewBaseline(request)
	if review.State != desktop.Completed {
		t.Fatal(review)
	}
	request.Review = review.Comparison.Identity
	if released := app.ApproveBaseline(request); released.State != desktop.Completed {
		t.Fatal(released)
	}
	// Historical inspection needs neither the candidate nor its profiles.
	for _, name := range []string{"test.json", "profile.json"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeDocument(t, root, "historical.json", read(t, filepath.Join("testdata", "historical-release-v1.json")))
	retained := map[string]string{}
	for _, entry := range []string{"release.json", "historical.json"} {
		retained[entry] = read(t, filepath.Join(root, entry))
		for _, reveal := range []bool{false, true} {
			window, printed := shownAlike(t, app, root, entry, true, reveal, func(p printedRelease) baseline.Comparison { return p.Baseline })
			// The identity the window shows is the full release identity a
			// suite's release references pin, not a review commitment.
			if printed.Schema != "readmit-expectation-inspection/v1" || printed.ID != "booking" || window.ReleaseID != printed.ID ||
				printed.Identity != identityOf(t, root, entry) || window.Comparison.Identity != printed.Identity ||
				window.Comparison.Parent != printed.Parent || window.PreviousApprover != printed.Approver || window.PreviousRationale != printed.Rationale {
				t.Fatalf("%s: the window shows %+v, the command line prints %+v", entry, window, printed)
			}
			pinned := 0
			for _, change := range window.Comparison.Changes {
				if change.Kind == "pinned" {
					pinned++
				}
			}
			if printed.ProfileCount != 1 || pinned != printed.ProfileCount {
				t.Fatalf("%s: the window shows %d profile pins, the command line counts %d", entry, pinned, printed.ProfileCount)
			}
		}
	}
	if identity := identityOf(t, root, "historical.json"); identity != historicalReleaseIdentity {
		t.Fatalf("the historical release reads under identity %s", identity)
	}
	// A release never made, a release of a version this release cannot read
	// and a release whose committed test identity changed are refused alike.
	writeDocument(t, root, "next.json", replaceOnce(t, retained["release.json"], `"readmit-test-release/v1"`, `"readmit-test-release/v2"`))
	writeDocument(t, root, "changed.json", replaceOnce(t, retained["release.json"], `"id":"booking"`, `"id":"rebooking"`))
	for _, entry := range []string{"missing.json", "next.json", "changed.json"} {
		refusedAlike(t, app, root, entry, true)
	}
	for entry, held := range retained {
		if read(t, filepath.Join(root, entry)) != held {
			t.Fatalf("inspection changed the retained release %s", entry)
		}
	}
}
