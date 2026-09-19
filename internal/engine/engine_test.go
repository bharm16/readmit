package engine_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The pin an unstamped build writes names that build, the spec contract it was
// handed and the profile it applies, and encodes to the same bytes every time.
func TestCurrentPinsTheBuildTheSpecContractAndTheProfile(t *testing.T) {
	pin := engine.Current(testrunner.SpecSchema)
	if pin.Schema != engine.Schema || pin.Engine != engine.Version() || pin.Spec != testrunner.SpecSchema || pin.Profile != observation.Profile {
		t.Fatalf("%+v", pin)
	}
	if pin.Engine != "dev" {
		t.Fatalf("an unstamped build reported %q rather than dev", pin.Engine)
	}
	if err := pin.Supported(); err != nil {
		t.Fatal(err)
	}
	first, err := engine.Encode(pin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Encode(pin)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("%s %s %v", first, second, err)
	}
	if !bytes.HasSuffix(first, []byte{'\n'}) {
		t.Fatal("an encoded pin is not one line")
	}
	decoded, err := engine.Decode(first)
	if err != nil || decoded != pin {
		t.Fatalf("%+v %v", decoded, err)
	}
}

// A pin carries the versions and nothing else: no path, no address, no
// evidence, and no member this release did not write.
func TestPinRefusesEveryDocumentThisReleaseDoesNotRead(t *testing.T) {
	valid := `{"schema":"readmit-engine/v1","engine":"dev","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`
	for name, document := range map[string]string{
		"no contract":      `{"engine":"dev","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"no build":         `{"schema":"readmit-engine/v1","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"no spec":          `{"schema":"readmit-engine/v1","engine":"dev","profile":"readmit-siu-v1"}`,
		"no profile":       `{"schema":"readmit-engine/v1","engine":"dev","spec":"readmit-test/v1"}`,
		"unknown member":   `{"schema":"readmit-engine/v1","engine":"dev","spec":"readmit-test/v1","profile":"readmit-siu-v1","output":"/case/job"}`,
		"duplicate member": `{"schema":"readmit-engine/v1","engine":"dev","engine":"other","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"empty build":      `{"schema":"readmit-engine/v1","engine":"","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"spaced build":     `{"schema":"readmit-engine/v1","engine":"readmit dev","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"escaped tab":      `{"schema":"readmit-engine/v1","engine":"dev\t0","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"build too long":   `{"schema":"readmit-engine/v1","engine":"` + strings.Repeat("v", 65) + `","spec":"readmit-test/v1","profile":"readmit-siu-v1"}`,
		"not an object":    `"readmit-engine/v1"`,
	} {
		if _, err := engine.Decode([]byte(document)); err == nil {
			t.Fatalf("accepted a pin with %s", name)
		} else if errors.Is(err, engine.ErrUnsupportedVersion) {
			t.Fatalf("reported %s as a version this release does not read", name)
		}
	}
	if _, err := engine.Decode([]byte(valid + strings.Repeat(" ", engine.MaxPinBytes))); err == nil {
		t.Fatal("accepted a pin over its size limit")
	}
	if _, err := engine.Decode([]byte(valid)); err != nil {
		t.Fatal(err)
	}
}

// A build this release does not recognize is recorded, not refused: builds
// change and the contracts decide readability. A spec or profile version this
// release does not evaluate is refused by name.
func TestADifferentBuildIsReadAndAnUnsupportedContractIsRefusedByName(t *testing.T) {
	later := engine.Pin{Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: testrunner.SpecSchema, Profile: observation.Profile}
	if err := later.Supported(); err != nil {
		t.Fatalf("refused a run another build evaluated: %v", err)
	}
	for name, pin := range map[string]engine.Pin{
		"spec":     {Schema: engine.Schema, Engine: "dev", Spec: "readmit-test/v2", Profile: observation.Profile},
		"profile":  {Schema: engine.Schema, Engine: "dev", Spec: testrunner.SpecSchema, Profile: "readmit-siu-v2"},
		"contract": {Schema: "readmit-engine/v2", Engine: "dev", Spec: testrunner.SpecSchema, Profile: observation.Profile},
	} {
		if err := pin.Supported(); !errors.Is(err, engine.ErrUnsupportedVersion) {
			t.Fatalf("an unsupported %s version reported %v", name, err)
		}
	}
	if _, err := engine.Decode([]byte(`{"schema":"readmit-engine/v2","engine":"dev","spec":"readmit-test/v1","profile":"readmit-siu-v1","ledger":true}`)); !errors.Is(err, engine.ErrUnsupportedVersion) {
		t.Fatalf("a later contract version with its own members reported %v", err)
	}
	if _, err := engine.Encode(engine.Pin{Schema: engine.Schema, Engine: "dev", Spec: "", Profile: observation.Profile}); err == nil {
		t.Fatal("encoded a pin naming no spec contract")
	}
}
