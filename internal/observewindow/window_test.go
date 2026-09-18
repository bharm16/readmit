package observewindow_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// declaredWindow is the hand-authored document every test in this package
// starts from. It is written out as bytes rather than built from the Go types
// so the reader is exercised against text an operator could have typed.
const declaredWindow = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "30s", "quiet_period": "2s", "stable_samples": 3, "max_records": 100, "max_samples": 16}
}`

func mustDecodeWindow(t *testing.T, document string) observewindow.Window {
	t.Helper()
	window, err := observewindow.DecodeWindow([]byte(document))
	if err != nil {
		t.Fatalf("a valid observation window was refused: %v", err)
	}
	return window
}

func TestDeclaredWindowReadsExactlyAsWritten(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	if window.Source.Kind != "downstream-capture" || window.Source.Identity != "scheduling-archive" || window.Source.Scope != "appointments" {
		t.Fatalf("the source was not read as declared: %+v", window.Source)
	}
	if window.Watermark.Kind != observewindow.DeclaredPosition || window.Watermark.Position != "2026-01-03T11:00:00Z" {
		t.Fatalf("the watermark was not read as declared: %+v", window.Watermark)
	}
	if window.PreExisting.Declaration != observewindow.DeclaredEmpty {
		t.Fatalf("the pre-existing-state declaration was not read as declared: %+v", window.PreExisting)
	}
	if window.Completion.StableSamples != 3 || window.Completion.QuietPeriod != "2s" || window.Completion.Deadline != "30s" {
		t.Fatalf("the completion rule was not read as declared: %+v", window.Completion)
	}
	if len(window.Identity()) != 64 {
		t.Fatalf("a valid window has no identity: %q", window.Identity())
	}
}

// Identity names the canonical form, so two files that declare the same window
// correlate with the same completions and one that declares a different window
// never does.
func TestWindowIdentityFollowsWhatWasDeclaredNotHowItWasSpelled(t *testing.T) {
	spaced := mustDecodeWindow(t, strings.ReplaceAll(declaredWindow, ": ", ":"))
	if spaced.Identity() != mustDecodeWindow(t, declaredWindow).Identity() {
		t.Fatal("the same declared window produced two identities")
	}
	other := mustDecodeWindow(t, strings.Replace(declaredWindow, `"stable_samples": 3`, `"stable_samples": 4`, 1))
	if other.Identity() == spaced.Identity() {
		t.Fatal("a different completion rule produced the same identity")
	}
}

func TestWindowRefusesDeclarationsItCannotHonour(t *testing.T) {
	for name, document := range map[string]string{
		"a later contract":             strings.Replace(declaredWindow, "window/v1", "window/v2", 1),
		"an unknown member":            strings.Replace(declaredWindow, `"completion":`, `"poll_forever": true, "completion":`, 1),
		"an omitted watermark":         strings.Replace(declaredWindow, `"watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},`, "", 1),
		"an omitted position":          strings.Replace(declaredWindow, `, "position": "2026-01-03T11:00:00Z"`, "", 1),
		"an unknown watermark kind":    strings.Replace(declaredWindow, `"kind": "declared-position"`, `"kind": "latest"`, 1),
		"an empty declared position":   strings.Replace(declaredWindow, `"position": "2026-01-03T11:00:00Z"`, `"position": ""`, 1),
		"a position without a kind":    strings.Replace(declaredWindow, `"kind": "declared-position"`, `"kind": "none"`, 1),
		"an unknown pre-existing rule": strings.Replace(declaredWindow, `"declaration": "declared-empty"`, `"declaration": "ignore"`, 1),
		"a baseline without one":       strings.Replace(declaredWindow, `"baseline_identity": ""`, `"baseline_identity": "`+strings.Repeat("a", 64)+`"`, 1),
		"a baseline with no identity":  strings.Replace(declaredWindow, `"declaration": "declared-empty"`, `"declaration": "recorded-baseline"`, 1),
		"a quiet period past its end":  strings.Replace(declaredWindow, `"quiet_period": "2s"`, `"quiet_period": "40s"`, 1),
		"a single stable sample":       strings.Replace(declaredWindow, `"stable_samples": 3`, `"stable_samples": 1`, 1),
		"fewer samples than stable":    strings.Replace(declaredWindow, `"max_samples": 16`, `"max_samples": 2`, 1),
		"no records in scope":          strings.Replace(declaredWindow, `"max_records": 100`, `"max_records": 0`, 1),
		"a deadline of zero":           strings.Replace(declaredWindow, `"deadline": "30s"`, `"deadline": "0s"`, 1),
		"a deadline past five minutes": strings.Replace(declaredWindow, `"deadline": "30s"`, `"deadline": "6m"`, 1),
		"a deadline that is not one":   strings.Replace(declaredWindow, `"deadline": "30s"`, `"deadline": "soon"`, 1),
		"an empty source scope":        strings.Replace(declaredWindow, `"scope": "appointments"`, `"scope": ""`, 1),
		"a null completion rule":       strings.Replace(declaredWindow, `"completion": {"deadline": "30s", "quiet_period": "2s", "stable_samples": 3, "max_records": 100, "max_samples": 16}`, `"completion": null`, 1),
	} {
		if _, err := observewindow.DecodeWindow([]byte(document)); err == nil {
			t.Fatalf("a window declaring %s was accepted", name)
		}
	}
}

func TestOversizedWindowIsRefusedBeforeItIsRead(t *testing.T) {
	padding := `{"schema":"readmit-observation-window/v1","note":"` + strings.Repeat("x", observewindow.MaxWindowBytes) + `"}`
	if _, err := observewindow.DecodeWindow([]byte(padding)); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("an oversized window was not refused by its limit: %v", err)
	}
}

// A window built in Go rather than decoded is held to the same bounds, so the
// desktop facade and a collector cannot construct one the reader would refuse.
func TestConstructedWindowIsHeldToTheSameBounds(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	window.Completion.StableSamples = 1
	if _, err := observewindow.EncodeWindow(window); err == nil {
		t.Fatal("a window with a single stable sample was encoded")
	}
	if window.Identity() != "" {
		t.Fatal("an invalid window reported an identity")
	}
}

// The canonical encoding is what Identity digests, so it has to read back.
func TestCanonicalWindowReadsBack(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	data, err := observewindow.EncodeWindow(window)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := observewindow.DecodeWindow(data)
	if err != nil {
		t.Fatalf("the canonical form of a window was refused: %v", err)
	}
	if reread.Identity() != window.Identity() {
		t.Fatal("re-encoding a window changed its identity")
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"schema", "source", "watermark", "pre_existing_state", "completion"} {
		if _, ok := members[member]; !ok {
			t.Fatalf("the canonical window omits %q", member)
		}
	}
	if len(members) != 5 {
		t.Fatalf("the canonical window carries unexpected members: %v", members)
	}
}
