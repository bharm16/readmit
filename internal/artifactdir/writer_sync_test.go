package artifactdir_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testrunner"
)

// writerSyncs records, for each directory a writer syncs, the names it held at
// its latest sync. A receiver writes beside the sender, so it is locked.
type writerSyncs struct {
	mu   sync.Mutex
	held map[string][]string
}

// observeWriterSyncs records every directory sync started afterwards. A sync
// of a directory failing answers true for is refused instead, as a failed
// sync.
func observeWriterSyncs(t *testing.T, failing func(directory string) bool) *writerSyncs {
	t.Helper()
	record := &writerSyncs{held: map[string][]string{}}
	t.Cleanup(artifactdir.ObserveDirectorySyncsForTest(func(directory string) error {
		directory = filepath.Clean(directory)
		if failing != nil && failing(directory) {
			return errors.New("injected directory sync failure")
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		record.mu.Lock()
		defer record.mu.Unlock()
		record.held[directory] = names
		return nil
	}))
	return record
}

// requireSynced fails for every entry in folder or below it that the latest
// sync of the directory holding it did not hold. folder is new to the writer
// under test, so every entry in it is one that writer made and reported: the
// output's own entry, each name below it and anything it keeps beside it.
func (r *writerSyncs) requireSynced(t *testing.T, folder string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := 0
	err := filepath.WalkDir(folder, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == folder {
			return err
		}
		entries++
		if directory := filepath.Dir(name); !slices.Contains(r.held[directory], entry.Name()) {
			relative, _ := filepath.Rel(folder, name)
			t.Errorf("reported written before the entry naming %s was synced", filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries == 0 {
		t.Fatal("the writer left nothing in its folder")
	}
}

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

// A run bundle, a test result and a durable run each send over the network
// and record what happened; each is found through names in several
// directories, the folder that holds it among them. Before any of them is
// reported written, the latest sync of every one of those directories held
// every name the output left there: payloads/, the run, the result holding it,
// the send-policy decision beside the result, the job, and each output's entry
// in its folder.
func TestSentEvidenceIsReportedWrittenOnlyOnceEveryDirectoryEntryItDependsOnIsSynced(t *testing.T) {
	address, _ := ackPeer(t)
	specPath, casePath := ackSpec(t, address)

	t.Run("run bundle", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		if _, err := executeRun(t, casePath, address, filepath.Join(folder, "run")); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, folder)
	})
	t.Run("test result", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		artifact, err := testrunner.Run(t.Context(), specPath, filepath.Join(folder, "result"))
		if err != nil || artifact.Result.Status != testrunner.Pass {
			t.Fatalf("the test did not pass: %v", err)
		}
		if _, err := os.Stat(filepath.Join(folder, "result.decision.json")); err != nil {
			t.Fatalf("the test kept no send-policy decision beside its result: %v", err)
		}
		synced.requireSynced(t, folder)
	})
	t.Run("durable run", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		summary, err := durablerun.Start(t.Context(), specPath, filepath.Join(folder, "job"))
		if err != nil || summary.State != durablerun.Passed || summary.ResultIdentity == "" {
			t.Fatalf("the durable run did not pass with a result: %+v %v", summary, err)
		}
		synced.requireSynced(t, folder)
	})
}

// synth, reproducer and redaction each make a folder to hold a case beside
// what they record about it; a report assembles, exports and prepares copies
// of evidence. Before any of them is reported written, the latest sync of every
// directory holding one of its names held that name, down to the folder
// holding the output. A redaction review is reported with the private state its
// export reads, the original proof among it, so that is synced too.
func TestDerivedOutputsAreReportedWrittenOnlyOnceEveryDirectoryEntryTheyDependOnIsSynced(t *testing.T) {
	t.Run("synth", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		if _, err := synth.Write(filepath.Join(folder, "family"), synthInputs); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, folder)
	})
	t.Run("reproducer", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		if _, err := createReproducer(t, filepath.Join(folder, "reproducer")); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, folder)
	})
	t.Run("redact", func(t *testing.T) {
		synced := observeWriterSyncs(t, nil)
		folder := caseFolder(t)
		request := redactRequest(t, folder)
		review, err := redact.Create(t.Context(), request)
		if err != nil || review.State != "ready-for-approval" {
			t.Fatalf("the review is not ready for approval: %v", err)
		}
		if _, err := os.Stat(filepath.Join(request.LocalState, "original-proof")); err != nil {
			t.Fatalf("the review kept no original proof: %v", err)
		}
		synced.requireSynced(t, folder)

		exported := caseFolder(t)
		if _, err := redact.Export(t.Context(), redact.ExportRequest{ReviewPath: request.Output, LocalState: request.LocalState, Approval: review.Identity, Output: filepath.Join(exported, "packet")}); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, exported)
	})
	t.Run("report", func(t *testing.T) {
		isolateWorkspace(t)
		packet := filepath.Join(caseFolder(t), "packet")
		if _, err := report.Create(t.Context(), report.Scenario, packet); err != nil {
			t.Fatal(err)
		}
		synced := observeWriterSyncs(t, nil)
		prepared := caseFolder(t)
		if _, err := report.Prepare(packet, filepath.Join(prepared, "rerun"), "127.0.0.1:2575"); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, prepared)
		assembled := caseFolder(t)
		retained := filepath.Join(assembled, "retained")
		if _, err := report.Assemble(t.Context(), report.RetainedInput{Case: filepath.Join(packet, "reproducer"), Spec: filepath.Join(packet, "spec.json"), Current: filepath.Join(packet, "post-fix"), Baseline: filepath.Join(packet, "baseline")}, retained); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, assembled)
		exported := caseFolder(t)
		if _, err := report.ExportReview(t.Context(), retained, filepath.Join(exported, "review")); err != nil {
			t.Fatal(err)
		}
		synced.requireSynced(t, exported)
	})
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

// outputWriter is one writer under test: what it writes at output, and how
// the reader of that output opens it.
type outputWriter struct {
	name  string
	write func(t *testing.T, output string) error
	open  func(output string) error
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
		open: func(output string) error { _, err := os.Stat(filepath.Join(output, "preparation.sha256")); return err },
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
	}}
}

// The last sync each writer makes is of the folder holding its output, after
// its completion record: every file is written and synced by then. A writer
// whose last sync fails does not report success, and does not say it left an
// incomplete output either; it says the output was written in full but may
// not survive a power loss, and the output does open. A durable run syncs its
// folder before its first send instead, which the tests below cover.
func TestALateDirectorySyncFailureReportsAnOutputWrittenInFull(t *testing.T) {
	address, _ := ackPeer(t)
	specPath, casePath := ackSpec(t, address)
	for _, writer := range outputWriters(t, address, specPath, casePath, writeDerivedSources(t)) {
		if writer.name == "durable run" {
			continue
		}
		t.Run(writer.name, func(t *testing.T) {
			folder := caseFolder(t)
			observeWriterSyncs(t, func(directory string) bool { return directory == folder })
			output := filepath.Join(folder, "output")
			err := writer.write(t, output)
			if err == nil {
				t.Fatal("a write whose folder could not be synced reported success")
			}
			if !strings.Contains(err.Error(), "written in full but a power loss could still lose") || strings.Contains(err.Error(), "incomplete") {
				t.Fatalf("a write that failed only its last directory sync did not say what it left: %v", err)
			}
			if err := writer.open(output); err != nil {
				t.Fatalf("the output said to be written in full does not open: %v", err)
			}
		})
	}
}

// Each writer syncs the folder holding its output last, so a folder it can
// create in but cannot open is refused before it writes anything into it, and
// a writer that sends refuses before it connects.
func TestWritersRefuseAFolderTheyCannotSyncBeforeWritingOrSendingAnything(t *testing.T) {
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

// A durable run syncs its job's entry in the folder holding it before its
// first send, so one whose folder cannot be synced sends nothing.
func TestADurableRunWhoseFolderCannotBeSyncedSendsNothing(t *testing.T) {
	address, accepted := ackPeer(t)
	specPath, _ := ackSpec(t, address)
	folder := caseFolder(t)
	observeWriterSyncs(t, func(directory string) bool { return directory == folder })
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
	observeWriterSyncs(t, func(directory string) bool {
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
