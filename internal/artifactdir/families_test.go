package artifactdir_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/protect"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/sharing"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ackPeer accepts every message it receives with an AA acknowledgement naming
// the one control ID ackSpec sends, and counts the connections it accepted.
func ackPeer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				reader, _ := mllp.NewReader(conn, 1<<20)
				for {
					if _, err := reader.ReadFrame(); err != nil {
						return
					}
					fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
				}
			}()
		}
	}()
	return listener.Addr().String(), &accepted
}

// ackSpec writes, in a folder of its own, a case holding one SIU message, a
// plain loopback target for address and an ACK-boundary spec sending that
// message, and answers the spec's path and the case's.
func ackSpec(t *testing.T, address string) (string, string) {
	t.Helper()
	inputs := caseFolder(t)
	raw, err := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	casePath := filepath.Join(inputs, "case")
	if _, err := bundle.Write(casePath, []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	accepted := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &accepted}}}}}
	for name, document := range map[string]any{"target.json": ackTarget(address), "spec.json": spec} {
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inputs, name), encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(inputs, "spec.json"), casePath
}

func ackTarget(address string) replay.Target {
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 4096}
}

// executeRun replays the one message of a case written by ackSpec to address.
func executeRun(t *testing.T, casePath, address, output string) (*replay.Run, error) {
	t.Helper()
	plan, err := replay.Prepare(casePath, ackTarget(address), replay.Options{Occurrences: []string{"s0001-e000001"}})
	if err != nil {
		t.Fatal(err)
	}
	return replay.Execute(t.Context(), plan, output)
}

var synthInputs = bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1"}

// createReproducer writes a case of eight occurrences in a folder of its own
// and a reproducer of its first one at output.
func createReproducer(t *testing.T, output string) (*reproducer.Manifest, error) {
	t.Helper()
	casePath := filepath.Join(caseFolder(t), "case")
	source, err := writeCase(t, casePath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reproducer.NewPlan(source.Identity)
	if err == nil {
		plan, err = reproducer.Append(plan, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: "s0001-e000001"})
	}
	if err != nil {
		t.Fatal(err)
	}
	return reproducer.Create(source, casePath, plan, output)
}

// redactRequest writes the redaction fixture's case, spec, policy and
// inventory in a folder of their own, and asks for the review and its private
// state in folder.
func redactRequest(t *testing.T, folder string) redact.Request {
	t.Helper()
	inputs := caseFolder(t)
	var sources []bundle.Input
	for _, name := range []string{"booking", "reschedule"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".mllp")
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, bundle.Input{Path: name + ".mllp", Data: raw, Options: hl7.Options{Format: hl7.MLLP}})
	}
	importedAt := time.Date(2030, 3, 4, 5, 6, 7, 0, time.UTC)
	casePath := filepath.Join(inputs, "original.case")
	if _, err := bundle.Write(casePath, sources, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"spec", "policy", "inventory"} {
		raw, err := os.ReadFile("../../testdata/fixtures/redact-" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inputs, name+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return redact.Request{CasePath: casePath, SpecPath: filepath.Join(inputs, "spec.json"), PolicyPath: filepath.Join(inputs, "policy.json"), InventoryPath: filepath.Join(inputs, "inventory.json"), Output: filepath.Join(folder, "review"), LocalState: filepath.Join(folder, "private")}
}

// derivedSources is what the report and export writers start from: a report
// packet, a retained packet assembled from it and a redaction review ready
// for approval, each written once in a folder of its own.
type derivedSources struct {
	packet, retained string
	review           redact.Request
	approval         string
}

func writeDerivedSources(t *testing.T) derivedSources {
	t.Helper()
	isolateWorkspace(t)
	sources := derivedSources{packet: filepath.Join(caseFolder(t), "packet"), retained: filepath.Join(caseFolder(t), "retained")}
	if _, err := report.Create(t.Context(), report.Scenario, sources.packet); err != nil {
		t.Fatal(err)
	}
	if _, err := report.Assemble(t.Context(), sources.retainedInput(), sources.retained); err != nil {
		t.Fatal(err)
	}
	sources.review = redactRequest(t, caseFolder(t))
	review, err := redact.Create(t.Context(), sources.review)
	if err != nil || review.State != "ready-for-approval" {
		t.Fatalf("the review is not ready for approval: %v", err)
	}
	sources.approval = review.Identity
	return sources
}

func (s derivedSources) retainedInput() report.RetainedInput {
	return report.RetainedInput{Case: filepath.Join(s.packet, "reproducer"), Spec: filepath.Join(s.packet, "spec.json"), Current: filepath.Join(s.packet, "post-fix"), Baseline: filepath.Join(s.packet, "baseline")}
}

// outputWriter is one writer under test: what it writes at output, how the
// reader of that output opens it, and what else it keeps beside the output.
type outputWriter struct {
	name  string
	write func(t *testing.T, output string) error
	open  func(output string) error
	// kept names what the writer keeps beside its output besides "output"
	// itself, such as a test's send-policy decision.
	kept []string
}

// beside reports whether name is the output or something the writer keeps
// beside it.
func (w outputWriter) beside(name string) bool {
	return name == "output" || slices.Contains(w.kept, name)
}

// outputWriters lists every writer this change syncs, each writing a new
// output of its own. A redaction review keeps its private state beside the
// review, in the same folder.
func outputWriters(t *testing.T, address, specPath, casePath string, sources derivedSources) []outputWriter {
	correlated := filepath.Join(caseFolder(t), "case")
	opened, err := writeCase(t, correlated)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../testdata/fixtures/correlate-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := correlate.ParseRules(raw)
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := correlate.Run(correlated, rules)
	if err != nil {
		t.Fatal(err)
	}
	return []outputWriter{{
		name:  "case",
		write: func(t *testing.T, output string) error { _, err := writeCase(t, output); return err },
		open:  func(output string) error { _, err := bundle.Open(output); return err },
	}, {
		name: "correlation review",
		write: func(t *testing.T, output string) error {
			revision, _, err := correlate.Review(opened, correlation, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			return correlate.SaveReview(output, opened, correlation, revision)
		},
		open: func(output string) error { _, err := correlate.ReadReview(output, opened, correlation); return err },
	}, {
		name: "run bundle",
		write: func(t *testing.T, output string) error {
			_, err := executeRun(t, casePath, address, output)
			return err
		},
		open: func(output string) error { _, err := replay.Open(output); return err },
	}, {
		name: "test result",
		write: func(t *testing.T, output string) error {
			_, err := testrunner.Run(t.Context(), specPath, output)
			return err
		},
		open: func(output string) error { _, err := testrunner.Open(output); return err },
		kept: []string{"output.decision.json"},
	}, {
		name: "durable run",
		write: func(t *testing.T, output string) error {
			_, err := durablerun.Start(t.Context(), specPath, output)
			return err
		},
		open: func(output string) error { _, err := durablerun.Open(output); return err },
	}, {
		name:  "synth",
		write: func(t *testing.T, output string) error { _, err := synth.Write(output, synthInputs); return err },
		open: func(output string) error {
			for _, variant := range []string{"regression", "cancellation", "invalid"} {
				if _, err := bundle.Open(filepath.Join(output, variant)); err != nil {
					return err
				}
			}
			_, err := os.Stat(filepath.Join(output, "family.json"))
			return err
		},
	}, {
		name:  "reproducer",
		write: func(t *testing.T, output string) error { _, err := createReproducer(t, output); return err },
		open:  func(output string) error { _, err := reproducer.Open(output); return err },
	}, {
		name: "redaction review",
		write: func(t *testing.T, output string) error {
			request := redactRequest(t, filepath.Dir(output))
			request.Output = output
			_, err := redact.Create(t.Context(), request)
			return err
		},
		open: func(output string) error { _, err := redact.OpenReview(output); return err },
		kept: []string{"private"},
	}, {
		name: "redaction export",
		write: func(t *testing.T, output string) error {
			_, err := redact.Export(t.Context(), redact.ExportRequest{ReviewPath: sources.review.Output, LocalState: sources.review.LocalState, Approval: sources.approval, Output: output})
			return err
		},
		open: func(output string) error { _, err := redact.OpenExport(output); return err },
	}, {
		name: "report preparation",
		write: func(t *testing.T, output string) error {
			_, err := report.Prepare(sources.packet, output, "127.0.0.1:2575")
			return err
		},
		open: func(output string) error { _, err := report.ReadPreparation(output); return err },
	}, {
		name: "retained report",
		write: func(t *testing.T, output string) error {
			_, err := report.Assemble(t.Context(), sources.retainedInput(), output)
			return err
		},
		open: func(output string) error { _, err := report.OpenRetained(context.Background(), output); return err },
	}, {
		name: "report review",
		write: func(t *testing.T, output string) error {
			_, err := report.ExportReview(t.Context(), sources.retained, output)
			return err
		},
		open: func(output string) error { _, err := report.OpenReview(context.Background(), output); return err },
	}, {
		name: "support summary",
		write: func(t *testing.T, output string) error {
			policy := filepath.Join(caseFolder(t), "sharing.json")
			if err := os.WriteFile(policy, []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096}`), 0600); err != nil {
				t.Fatal(err)
			}
			candidate, err := sharing.Prepare(t.Context(), sharing.Request{Source: sources.review.Output, Kind: "derived-review", Private: sources.review.LocalState, Policy: policy})
			if err != nil {
				t.Fatal(err)
			}
			return candidate.Publish(t.Context(), candidate.Identity(), output)
		},
		open: func(output string) error { _, err := sharing.Open(output); return err },
	}, {
		name: "backup",
		write: func(t *testing.T, output string) error {
			_, err := backup.Create(t.Context(), newProject(t), output)
			return err
		},
		open: func(output string) error { _, err := backup.Verify(output); return err },
	}, {
		name: "transfer package",
		write: func(t *testing.T, output string) error {
			inputs := caseFolder(t)
			key := filepath.Join(inputs, "test-only-key-material")
			evidence := filepath.Join(inputs, "evidence.txt")
			for path, data := range map[string]string{key: "test-only-not-a-real-key-4f8c1d2e6b0a9357\n", evidence: "synthetic retained evidence bytes"} {
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sources, notRead, err := protect.Collect([]string{evidence})
			if err != nil || notRead != 0 {
				t.Fatalf("collect: %v, %d not read", err, notRead)
			}
			// cat reads the test-only key material the way an operator's store
			// program would, from the locator it is given.
			control := protect.Control{Name: "lab-evidence", Storage: protect.OSVolumeEncryption, State: protect.Active, Command: "/bin/cat", Arguments: []string{key}, Generation: 1, RotatedAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC), MaxAge: "720h", Retain: "2160h"}
			_, err = protect.Pack(t.Context(), control, sources, 0, output, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
			return err
		},
		open: func(output string) error { _, _, err := protect.ReadPackage(output); return err },
	}}
}

// newProject creates a project in a folder of its own holding one registered
// case, so a backup of it stores files two directories deep.
func newProject(t *testing.T) string {
	t.Helper()
	created, err := project.Create(filepath.Join(caseFolder(t), "project"), project.Document{Schema: project.Schema, Settings: project.Settings{Title: "Investigation"}, InterfaceVersions: []string{"v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeCase(t, filepath.Join(created.Root, "case")); err != nil {
		t.Fatal(err)
	}
	return created.Root
}

// Every family writes its layout: what it writes in a new folder is its
// output alone, and its own reader opens it. How a sealed write syncs is
// artifactdir's, tested once at its interface.
func TestEveryFamilyWritesItsLayout(t *testing.T) {
	address, _ := ackPeer(t)
	specPath, casePath := ackSpec(t, address)
	for _, writer := range outputWriters(t, address, specPath, casePath, writeDerivedSources(t)) {
		t.Run(writer.name, func(t *testing.T) {
			folder := caseFolder(t)
			output := filepath.Join(folder, "output")
			if err := writer.write(t, output); err != nil {
				t.Fatal(err)
			}
			if err := writer.open(output); err != nil {
				t.Fatalf("the output is not the layout its reader opens: %v", err)
			}
			entries, err := os.ReadDir(folder)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if !writer.beside(entry.Name()) {
					t.Errorf("the writer left %s beside its output", entry.Name())
				}
			}
		})
	}
}

// Each family's output is reserved first, and the folder holding it is synced
// last, so a folder the writer can create in but cannot open is refused before
// it writes anything, and a writer that sends refuses before it connects.
func TestEveryFamilyRefusesAFolderItCannotSyncBeforeWritingOrSendingAnything(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder its owner can create in but not open")
	}
	address, accepted := ackPeer(t)
	specPath, casePath := ackSpec(t, address)
	for _, writer := range outputWriters(t, address, specPath, casePath, writeDerivedSources(t)) {
		t.Run(writer.name, func(t *testing.T) {
			folder := caseFolder(t)
			if err := os.Chmod(folder, 0300); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(folder, 0700) })
			if err := writer.write(t, filepath.Join(folder, "output")); err == nil {
				t.Fatal("an output was reported written into a folder that cannot be synced")
			}
			if err := os.Chmod(folder, 0700); err != nil {
				t.Fatal(err)
			}
			if left, err := os.ReadDir(folder); err != nil || len(left) != 0 {
				t.Fatalf("a refused write left %d entries behind: %v", len(left), err)
			}
			if accepted.Load() != 0 {
				t.Fatal("a writer connected before refusing a folder it cannot sync")
			}
		})
	}
}

// A transfer package that cannot be completed is removed, but one whose last
// sync fails is written in full: it is kept, and the pack says it is not
// confirmed against a power loss rather than calling it incomplete.
func TestATransferPackageWhoseFolderCannotBeSyncedIsKeptAndSaysSo(t *testing.T) {
	address, _ := ackPeer(t)
	specPath, casePath := ackSpec(t, address)
	for _, writer := range outputWriters(t, address, specPath, casePath, derivedSources{}) {
		if writer.name != "transfer package" {
			continue
		}
		folder := caseFolder(t)
		observeSyncs(t, nil, func(directory string) bool { return directory == folder })
		output := filepath.Join(folder, "output")
		if err := writer.write(t, output); err == nil || !strings.Contains(err.Error(), "the package was written in full but a power loss could still lose it") {
			t.Fatalf("a package whose folder could not be synced answered %v", err)
		}
		if err := writer.open(output); err != nil {
			t.Fatalf("the package written in full was not kept: %v", err)
		}
		return
	}
	t.Fatal("no transfer package writer")
}

// A durable run syncs its job's entry in the folder holding it before its
// first send, so one whose folder cannot be synced sends nothing.
func TestADurableRunWhoseFolderCannotBeSyncedSendsNothing(t *testing.T) {
	address, accepted := ackPeer(t)
	specPath, _ := ackSpec(t, address)
	folder := caseFolder(t)
	observeSyncs(t, nil, func(directory string) bool { return directory == folder })
	if _, err := durablerun.Start(context.Background(), specPath, filepath.Join(folder, "job")); err == nil || !strings.Contains(err.Error(), "cannot sync durable evidence directory") {
		t.Fatalf("a durable run whose folder could not be synced was not refused: %v", err)
	}
	if accepted.Load() != 0 {
		t.Fatal("a durable run sent before its job's entry in its folder was synced")
	}
}

// A durable run's result syncs its own entries and its entry in the job last,
// before the job records how it finished. When that sync fails the result is
// written in full but not confirmed, so the job names no result, records that
// the run stopped with an execution error, and tells its caller why.
func TestADurableRunWhoseResultCannotBeSyncedNamesNoResultAndSaysWhy(t *testing.T) {
	address, _ := ackPeer(t)
	specPath, _ := ackSpec(t, address)
	job := filepath.Join(caseFolder(t), "job")
	observeSyncs(t, nil, func(directory string) bool {
		_, err := os.Stat(filepath.Join(job, "result", "identity.sha256"))
		return directory == job && err == nil
	})
	summary, err := durablerun.Start(context.Background(), specPath, job)
	if err == nil || !strings.Contains(err.Error(), "the result was written in full but a power loss could still lose it") {
		t.Fatalf("a durable run whose result could not be synced did not say so: %v", err)
	}
	if summary.ResultIdentity != "" || summary.State != durablerun.ExecutionError {
		t.Fatalf("a durable run named a result it could not confirm: %+v", summary)
	}
	if reopened, err := durablerun.Open(job); err != nil || reopened.ResultIdentity != "" || reopened.State != durablerun.ExecutionError {
		t.Fatalf("the job's journal does not record a run that stopped without a result: %+v %v", reopened, err)
	}
	if _, err := testrunner.Open(filepath.Join(job, "result")); err != nil {
		t.Fatalf("the result said to be written in full does not open: %v", err)
	}
}
