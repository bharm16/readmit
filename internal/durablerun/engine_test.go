package durablerun_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// passed executes one accepted run and returns its job directory.
func passed(t *testing.T) (string, string) {
	t.Helper()
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	summary, err := durablerun.Start(context.Background(), spec, out)
	if err != nil || summary.State != durablerun.Passed {
		t.Fatalf("%+v %v", summary, err)
	}
	return spec, out
}

// repin rewrites the retained pin so a later build's job can be read by this
// one. The pin is a sibling document outside the journal chain, so rewriting
// it leaves every other verification in place.
func repin(t *testing.T, out string, pin engine.Pin) {
	t.Helper()
	raw, err := engine.Encode(pin)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, "engine.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// Every job this release writes names the build that evaluated it, the spec
// contract that spec declared and the profile applied to its observations.
func TestARunRetainsThePinOfTheEngineThatEvaluatedIt(t *testing.T) {
	_, out := passed(t)
	pin, err := durablerun.Engine(out)
	if err != nil {
		t.Fatal(err)
	}
	if pin != engine.Current(testrunner.SpecSchema) {
		t.Fatalf("%+v", pin)
	}
	if pin.Engine != engine.Version() || pin.Profile != observation.Profile {
		t.Fatalf("a run recorded an engine it was not executed by: %+v", pin)
	}
	raw, err := os.ReadFile(filepath.Join(out, "engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := engine.Encode(pin)
	if err != nil || string(raw) != string(expected) {
		t.Fatalf("%s %v", raw, err)
	}
	// The pin is evidence: cleanup retains it and removes only a stale lease.
	cleanup, err := durablerun.Clean(out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cleanup.Retained, "engine.json") || len(cleanup.Removed) != 0 {
		t.Fatalf("%+v", cleanup)
	}
}

// A build identity this release does not recognize is recorded, never refused.
func TestAJobAnotherBuildEvaluatedIsStillRead(t *testing.T) {
	_, out := passed(t)
	repin(t, out, engine.Pin{Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: testrunner.SpecSchema, Profile: observation.Profile})
	summary, err := durablerun.Open(out)
	if err != nil || summary.State != durablerun.Passed {
		t.Fatalf("%+v %v", summary, err)
	}
	pin, err := durablerun.Engine(out)
	if err != nil || pin.Engine != "0.9.0-alpha.7" {
		t.Fatalf("%+v %v", pin, err)
	}
}

// A spec or profile version this release does not evaluate is refused by name
// on every read, and nothing in the job is changed or repaired.
func TestAJobEvaluatedUnderAnUnsupportedVersionIsRefusedByEveryRead(t *testing.T) {
	for name, pin := range map[string]engine.Pin{
		"spec":     {Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: "readmit-test/v2", Profile: observation.Profile},
		"profile":  {Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: testrunner.SpecSchema, Profile: "readmit-siu-v2"},
		"contract": {Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: "readmit-test/v2", Profile: "readmit-siu-v2"},
	} {
		t.Run(name, func(t *testing.T) {
			spec, out := passed(t)
			repin(t, out, pin)
			before := journalBytes(t, out)
			if _, err := durablerun.Open(out); !errors.Is(err, engine.ErrUnsupportedVersion) {
				t.Fatalf("status reported %v", err)
			}
			if _, err := durablerun.Recover(out); !errors.Is(err, engine.ErrUnsupportedVersion) {
				t.Fatalf("recovery reported %v", err)
			}
			if _, err := durablerun.Clean(out); !errors.Is(err, engine.ErrUnsupportedVersion) {
				t.Fatalf("cleanup reported %v", err)
			}
			if _, err := durablerun.Resume(context.Background(), out, spec, out+"-resumed"); !errors.Is(err, engine.ErrUnsupportedVersion) {
				t.Fatalf("resume reported %v", err)
			}
			if _, err := os.Lstat(out + "-resumed"); err == nil {
				t.Fatal("a refused resume created a new job")
			}
			if string(journalBytes(t, out)) != string(before) {
				t.Fatal("a refusal changed retained evidence")
			}
			// The versions are still reported, so an operator reads why the
			// run is unreadable rather than only that it is.
			retained, err := durablerun.Engine(out)
			if err != nil || retained != pin {
				t.Fatalf("%+v %v", retained, err)
			}
		})
	}
}

// A directory retaining no readable pin is not a job this release wrote.
func TestAJobWithNoReadableEnginePinIsRefused(t *testing.T) {
	for name, write := range map[string]func(string) error{
		"absent":    func(path string) error { return os.Remove(path) },
		"malformed": func(path string) error { return os.WriteFile(path, []byte("{"), 0600) },
		"directory": func(path string) error { return os.Remove(path) },
	} {
		t.Run(name, func(t *testing.T) {
			_, out := passed(t)
			path := filepath.Join(out, "engine.json")
			if err := write(path); err != nil {
				t.Fatal(err)
			}
			if name == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := durablerun.Open(out); err == nil {
				t.Fatal("read a job retaining no engine pin")
			}
			if _, err := durablerun.Engine(out); err == nil {
				t.Fatal("reported an engine pin a job does not retain")
			}
		})
	}
}

// A run that is stopped still names the engine that evaluated it. The pin is
// synced before the first journal record, so a cancellation, a deadline and an
// interruption all leave a job whose versions recovery can read.
func TestAStoppedRunStillRetainsItsEnginePin(t *testing.T) {
	address, _ := peer(t, "")
	for name, stop := range map[string]func() (context.Context, context.CancelFunc){
		"cancelled": func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		},
		"timed out": func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 300*time.Millisecond)
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec, out := setup(t, address)
			ctx, cancel := stop()
			defer cancel()
			summary, err := durablerun.Start(ctx, spec, out)
			if err != nil || summary.State == durablerun.Passed {
				t.Fatalf("%+v %v", summary, err)
			}
			pin, err := durablerun.Engine(out)
			if err != nil || pin != engine.Current(testrunner.SpecSchema) {
				t.Fatalf("%+v %v", pin, err)
			}
			recovery, err := durablerun.Recover(out)
			if err != nil {
				t.Fatalf("a stopped run could not be recovered: %v", err)
			}
			if recovery.Run.State == durablerun.Passed {
				t.Fatalf("%+v", recovery)
			}
		})
	}
}
