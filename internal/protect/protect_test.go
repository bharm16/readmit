package protect

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The key store a test points a control at is this test binary re-executed with
// storeSwitch set. It is a stand-in for an operating system credential store or
// a customer-managed key provider, and every byte it returns is generated
// inside the test and marked test-only. No key is committed in this repository.
const (
	storeSwitch      = "READMIT_TEST_PROTECT_KEY_STORE"
	testOnlyKey      = "test-only-not-a-real-key-4f8c1d2e6b0a9357"
	testOnlyOtherKey = "test-only-not-a-real-key-0000000000000000"
)

func TestMain(m *testing.M) {
	if mode, ok := os.LookupEnv(storeSwitch); ok {
		os.Exit(store(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// store emits the bytes of the file its locator argument names. A locator is an
// argument; key material never is, which is the rule this package enforces.
func store(mode string, args []string) int {
	switch mode {
	case "fail":
		return 3
	case "short":
		os.Stdout.WriteString("too-short")
		return 0
	}
	if len(args) != 1 {
		return 4
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return 4
	}
	os.Stdout.Write(data)
	return 0
}

func storeCommand(t *testing.T) string {
	t.Helper()
	// The provider is this race-instrumented binary re-executed. It has no
	// background work to drain; avoid a one-second race-runtime exit sleep
	// on every lookup. The parent race runtime keeps its default settings.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	path, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// locator writes test-only key material to a file and returns its path.
func locator(t *testing.T, material string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-only-key-material")
	if err := os.WriteFile(path, []byte(material+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func control(t *testing.T, name, material string) Control {
	t.Helper()
	return Control{
		Name:       name,
		Storage:    OSVolumeEncryption,
		State:      Active,
		Command:    storeCommand(t),
		Arguments:  []string{locator(t, material)},
		Generation: 1,
		RotatedAt:  time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
		MaxAge:     "720h",
		Retain:     "2160h",
	}
}

// The encoded form is pinned by an independently authored golden. It was
// written from the contract description, never produced by this package.
const goldenDocument = `{"schema":"readmit-protection/v1","controls":[{"name":"lab-evidence","storage":"os-volume-encryption","state":"active","command":"/usr/bin/true","arguments":["read","lab-key"],"generation":2,"rotated_at":"2026-09-18T09:00:00Z","max_age":"720h","retain":"2160h"}]}
`

func goldenControl() Control {
	return Control{
		Name: "lab-evidence", Storage: OSVolumeEncryption, State: Active,
		Command: "/usr/bin/true", Arguments: []string{"read", "lab-key"},
		Generation: 2, RotatedAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC),
		MaxAge: "720h", Retain: "2160h",
	}
}

func TestProtectionDocumentEncodesToItsIndependentlyAuthoredForm(t *testing.T) {
	encoded, err := Encode(Document{Schema: Schema, Controls: []Control{goldenControl()}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != goldenDocument {
		t.Fatalf("encoded form drifted from the contract:\n got %s\nwant %s", encoded, goldenDocument)
	}
	document, err := Decode([]byte(goldenDocument))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := goldenControl()
	if len(document.Controls) != 1 {
		t.Fatalf("decoded %d controls, want 1", len(document.Controls))
	}
	got := document.Controls[0]
	if !slices.Equal(got.Arguments, want.Arguments) {
		t.Fatalf("decoded locator arguments %v", got.Arguments)
	}
	if got.Name != want.Name || got.Storage != want.Storage || got.State != want.State ||
		got.Command != want.Command || got.Generation != want.Generation ||
		!got.RotatedAt.Equal(want.RotatedAt) || got.MaxAge != want.MaxAge || got.Retain != want.Retain {
		t.Fatalf("decoded control is not the authored one: %+v", got)
	}
}

func TestProtectionDocumentRefusesEverythingItDoesNotDefine(t *testing.T) {
	base := `{"schema":"readmit-protection/v1","controls":[{"name":"lab","storage":"os-volume-encryption","state":"active","command":"/usr/bin/true","arguments":[],"generation":1,"rotated_at":"2026-09-18T09:00:00Z"`
	for name, document := range map[string]string{
		"an unknown member":         base + `,"algorithm":"aes"}]}`,
		"an unknown version":        `{"schema":"readmit-protection/v2","controls":[]}`,
		"an unknown storage":        strings.Replace(base, "os-volume-encryption", "hoped-for", 1) + `}]}`,
		"an unknown state":          strings.Replace(base, `"state":"active"`, `"state":"provisional"`, 1) + `}]}`,
		"a relative command":        strings.Replace(base, "/usr/bin/true", "true", 1) + `}]}`,
		"a zero generation":         strings.Replace(base, `"generation":1`, `"generation":0`, 1) + `}]}`,
		"an unrecorded rotation":    strings.Replace(base, `"rotated_at":"2026-09-18T09:00:00Z"`, `"rotated_at":"0001-01-01T00:00:00Z"`, 1) + `}]}`,
		"a negative retention":      base + `,"retain":"-1h"}]}`,
		"an unparsable max age":     base + `,"max_age":"soon"}]}`,
		"a name outside the set":    strings.Replace(base, `"name":"lab"`, `"name":"lab/evidence"`, 1) + `}]}`,
		"duplicate control names":   `{"schema":"readmit-protection/v1","controls":[` + base[len(`{"schema":"readmit-protection/v1","controls":[`):] + `},{"name":"lab","storage":"customer-key","state":"active","command":"/usr/bin/true","arguments":[],"generation":1,"rotated_at":"2026-09-18T09:00:00Z"}]}`,
		"a truncated document":      `{"schema":"readmit-protection/v1","controls":[`,
		"a document that is a list": `[]`,
	} {
		if _, err := Decode([]byte(document)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := Decode([]byte(`{"schema":"readmit-protection/v2","controls":[]}`)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Error("a later version is not reported as the version it declares")
	}
}

func TestRotationAndRetentionNeverReportAnUndeclaredStateAsPassing(t *testing.T) {
	now := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	entry := goldenControl()
	if got := entry.Rotation(now.Add(time.Hour)); got != RotationCurrent {
		t.Errorf("rotation within its interval is %s", got)
	}
	if got := entry.Rotation(now.Add(800 * time.Hour)); got != RotationOverdue {
		t.Errorf("rotation past its interval is %s", got)
	}
	entry.MaxAge = ""
	if got := entry.Rotation(now); got != RotationNotDeclared {
		t.Errorf("an undeclared rotation interval is %s, want not-declared", got)
	}
	until, declared := goldenControl().RetainUntil(now)
	if !declared || !until.Equal(now.Add(2160*time.Hour)) {
		t.Errorf("declared retention resolved to %s (%v)", until, declared)
	}
	entry.Retain = ""
	if _, declared := entry.RetainUntil(now); declared {
		t.Error("an undeclared retention period reported a retention instant")
	}
	descriptor := Package{}
	if got := descriptor.Retention(now); got != RetentionNotDeclared {
		t.Errorf("a package declaring no retention is %s, want not-declared", got)
	}
	descriptor.RetainUntil = now.Add(time.Hour)
	if got := descriptor.Retention(now); got != WithinRetention {
		t.Errorf("a package inside its retention is %s", got)
	}
	if got := descriptor.Retention(now.Add(2 * time.Hour)); got != PastRetention {
		t.Errorf("a package past its retention is %s", got)
	}
}

func FuzzDecodeDocument(f *testing.F) {
	f.Add(goldenDocument)
	f.Add(`{"schema":"readmit-protection/v1","controls":[]}`)
	f.Add(`{"schema":"readmit-protection/v2"}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, raw string) {
		document, err := Decode([]byte(raw))
		if err != nil {
			return
		}
		encoded, err := Encode(document)
		if err != nil {
			t.Fatalf("an accepted document did not re-encode: %v", err)
		}
		again, err := Decode(encoded)
		if err != nil {
			t.Fatalf("a re-encoded document did not decode: %v", err)
		}
		if len(again.Controls) != len(document.Controls) {
			t.Fatal("re-encoding changed the controls a document declares")
		}
	})
}
