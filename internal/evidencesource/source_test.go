package evidencesource_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// The documents below are authored here rather than produced by the engine, so
// the reader is exercised against text an operator could have written.
const declaredDirectorySource = `{
  "schema": "readmit-source/v1",
  "name": "scheduling-exports",
  "kind": "directory",
  "scope": "appointments",
  "root": "ROOT",
  "quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},
  "retry": {"attempts": 2, "backoff": "0s"}
}`

const declaredTransferSource = `{
  "schema": "readmit-source/v1",
  "name": "lab-sftp",
  "kind": "transfer",
  "scope": "appointments",
  "address": "127.0.0.1:2222",
  "classification": "nonproduction",
  "command": "COMMAND",
  "arguments": ["ARGUMENT"],
  "quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},
  "retry": {"attempts": 2, "backoff": "0s"}
}`

const declaredAPISource = `{
  "schema": "readmit-source/v1",
  "name": "scheduling-api",
  "kind": "api",
  "scope": "appointments",
  "address": "127.0.0.1:8443",
  "classification": "nonproduction",
  "quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},
  "retry": {"attempts": 1, "backoff": "0s"}
}`

// message is one whole HL7 v2 message with carriage-return segment endings. It
// is authored here so a test never depends on a fixture the engine generated.
const message = "MSH|^~\\&|SEND|FAC|RECV|FAC|20260103110000||SIU^S12|MSG00001|P|2.5.1\r" +
	"SCH|1||||||||||||||||||||||||BOOKED\r"

func plan(t *testing.T, members ...string) importer.Plan {
	t.Helper()
	if members == nil {
		members = []string{}
	}
	declared := importer.Plan{
		Schema: importer.PlanSchema, Framing: importer.RawFraming, Terminator: hl7.CR,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: members,
	}
	if err := declared.Validate(); err != nil {
		t.Fatal(err)
	}
	return declared
}

func options(t *testing.T, members ...string) evidencesource.Options {
	t.Helper()
	return evidencesource.Options{
		Plan: plan(t, members...),
		Now:  func() time.Time { return time.Date(2026, 1, 3, 11, 0, 0, 0, time.UTC) },
	}
}

// directorySource writes a source declaration over a new directory holding the
// named entries, and returns the decoded declaration and the directory.
func directorySource(t *testing.T, entries map[string]string) (evidencesource.Source, string) {
	t.Helper()
	root := t.TempDir()
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := evidencesource.Decode([]byte(strings.Replace(declaredDirectorySource, "ROOT", root, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return source, root
}

func destination(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "collected")
}

func TestCollectStagesTheSelectedEntriesOfADirectorySourceUnchanged(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message, "notes.md": "not evidence"})
	output := destination(t)
	collection, err := evidencesource.Collect(context.Background(), source, output, options(t, ".hl7"))
	if err != nil {
		t.Fatal(err)
	}
	if collection.Status != observewindow.Complete || collection.RunState != "" {
		t.Fatalf("collection status = %q run state = %q, want complete with no run state", collection.Status, collection.RunState)
	}
	if collection.Totals.Collected != 1 || collection.Totals.Excluded != 1 || collection.Totals.Declared != 2 {
		t.Fatalf("totals = %+v, want one collected and one excluded of two declared", collection.Totals)
	}
	if collection.Totals.Records != 1 || collection.Totals.Occurrences != 1 {
		t.Fatalf("totals = %+v, want the one record and occurrence the plan divides the entry into", collection.Totals)
	}
	staged, err := os.ReadFile(filepath.Join(output, "a.hl7"))
	if err != nil || string(staged) != message {
		t.Fatalf("staged bytes = %q (err %v), want the source's own bytes unchanged", staged, err)
	}
	if _, err := os.Lstat(filepath.Join(output, "notes.md")); !os.IsNotExist(err) {
		t.Fatal("an excluded entry must not be staged")
	}
	if len(collection.Identity) != 64 {
		t.Fatalf("identity = %q, want a digest over the staged names and contents", collection.Identity)
	}
}

// The same bytes under a second name are the same evidence. The first entry the
// source listed is staged and the repeat is recorded against it, so one
// collection never stages the same evidence twice.
func TestCollectRecordsRepeatedBytesAsADuplicateOfTheEntryCollectedFirst(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": message})
	output := destination(t)
	collection, err := evidencesource.Collect(context.Background(), source, output, options(t, ".hl7"))
	if err != nil {
		t.Fatal(err)
	}
	if collection.Totals.Collected != 1 || collection.Totals.Duplicates != 1 {
		t.Fatalf("totals = %+v, want one collected and one duplicate", collection.Totals)
	}
	for _, entry := range collection.Entries {
		if entry.State == evidencesource.Duplicate && entry.DuplicateOf != "a.hl7" {
			t.Fatalf("duplicate names %q, want the entry collected first", entry.DuplicateOf)
		}
	}
	if _, err := os.Lstat(filepath.Join(output, "b.hl7")); !os.IsNotExist(err) {
		t.Fatal("a duplicate must not stay staged beside the entry it repeats")
	}
	// The identity is over the staged names and contents only, so a second
	// collection of the same source identifies the same way.
	again, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if err != nil || again.Identity != collection.Identity {
		t.Fatalf("identity = %q, want the same identity as %q (err %v)", again.Identity, collection.Identity, err)
	}
}

// A quota is applied to what the source declares, before anything is read, and
// a source past one is refused rather than collected in part.
func TestCollectRefusesASourcePastItsDeclaredQuotaWithoutStagingAnything(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": message + "\r", "c.hl7": message + "\r\r"})
	source.Quota.MaxEntries = 2
	output := destination(t)
	_, err := evidencesource.Collect(context.Background(), source, output, options(t, ".hl7"))
	if err == nil || !strings.Contains(err.Error(), "exceeds its own quota") {
		t.Fatalf("collect error = %v, want the quota refusal", err)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("a refused collection must leave no collection directory behind")
	}
}

// An entry larger than the per-entry quota is refused rather than collected as
// a prefix of itself, and the refusal is the whole collection's execution error.
func TestCollectRefusesAnEntryLargerThanItsPerEntryQuota(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message})
	source.Quota.MaxEntryBytes = 8
	_, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if err == nil || !strings.Contains(err.Error(), "exceeds its own quota") {
		t.Fatalf("collect error = %v, want the declared quota refusal", err)
	}
	// An entry exactly at the declared limit fits: the limit is what an entry
	// may hold, not one byte less than it.
	source.Quota.MaxEntryBytes = int64(len(message))
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if err != nil || collection.Totals.Collected != 1 {
		t.Fatalf("collection = %+v (err %v), want the entry at the limit collected", collection.Totals, err)
	}
}

// An entry that could not be read is an execution error. It is never reported
// as an entry that held nothing, and the collection that hit one never reports
// what it staged as the whole of the declared scope.
func TestCollectReportsAnUnreadableEntryAsAnExecutionErrorRatherThanAnEmptyOne(t *testing.T) {
	source, root := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": message + "\r"})
	if err := os.Chmod(filepath.Join(root, "b.hl7"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "b.hl7"), 0600) })
	if os.Geteuid() == 0 {
		t.Skip("a privileged user reads a mode 0000 file, so there is no unreadable entry to report")
	}
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if collection.Status != observewindow.Failed || collection.RunState != "execution_error" {
		t.Fatalf("status = %q run state = %q, want failed as an execution error", collection.Status, collection.RunState)
	}
	if collection.Totals.Collected != 1 || collection.Totals.Unreadable != 1 {
		t.Fatalf("totals = %+v, want the readable entry staged and the unreadable one reported", collection.Totals)
	}
}

// A cancellation is a cancellation, never a completed collection of whatever
// had been staged when it arrived, and a collection that stopped early still
// accounts for every entry the source named.
func TestCollectCancelledPartWayThroughReportsCancellationNotCompletion(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": message + "\r", "c.hl7": message + "\r\r"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collection, err := evidencesource.Collect(ctx, source, destination(t), options(t, ".hl7"))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if collection.Status != observewindow.Cancelled || collection.RunState != "cancelled" {
		t.Fatalf("status = %q run state = %q, want cancellation in the durable-run vocabulary", collection.Status, collection.RunState)
	}
	if collection.Totals.Collected != 0 || collection.Totals.Unreadable != 3 {
		t.Fatalf("totals = %+v, want nothing collected and every entry it never reached reported", collection.Totals)
	}
	// Declared counts the entries readmit deliberately did not open too, so
	// what a receipt lists is every entry the source named that a collection
	// could have read.
	if listed := collection.Totals.Declared - collection.Totals.NotRead; len(collection.Entries) != listed {
		t.Fatalf("entries = %d of %d listed, want every listed entry accounted for", len(collection.Entries), listed)
	}
	for _, entry := range collection.Entries {
		if !strings.Contains(entry.Reason, "before this entry was reached") {
			t.Fatalf("entry = %+v, want the reason it was never reached", entry)
		}
	}
}

// Unsupported is not an empty source. This release collects from no application
// interface, and says so rather than collecting nothing and reporting success.
func TestCollectRefusesAnAPISourceAndDiagnoseRecordsItAsUnsupported(t *testing.T) {
	source, err := evidencesource.Decode([]byte(declaredAPISource))
	if err != nil {
		t.Fatal(err)
	}
	selected := approvedLoopback(t)
	chosen := options(t, ".hl7")
	chosen.Policy = &selected
	if _, err := evidencesource.Collect(context.Background(), source, destination(t), chosen); err == nil ||
		!strings.Contains(err.Error(), "no application interface") {
		t.Fatalf("collect error = %v, want the unsupported refusal", err)
	}
	access, err := evidencesource.Diagnose(context.Background(), source, chosen)
	if err != nil {
		t.Fatal(err)
	}
	if access.Status != observewindow.Unsupported || access.RunState != "execution_error" {
		t.Fatalf("status = %q run state = %q, want unsupported as an execution error", access.Status, access.RunState)
	}
	if access.Listed {
		t.Fatal("an unsupported source was not listed and must not report that it was")
	}
}

// A source that reaches off this machine goes through the one destination rule,
// and a destination nobody approved is denied rather than reached.
func TestCollectRefusesASourceNoApprovedDestinationCovers(t *testing.T) {
	source, err := evidencesource.Decode([]byte(strings.NewReplacer(
		"COMMAND", "/bin/sh", "ARGUMENT", "-c", "127.0.0.1:2222", "10.9.9.9:2222").Replace(declaredTransferSource)))
	if err != nil {
		t.Fatal(err)
	}
	selected := approvedLoopback(t)
	chosen := options(t, ".hl7")
	chosen.Policy = &selected
	chosen.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("a test resolves no name outside itself")
	}
	_, err = evidencesource.Collect(context.Background(), source, destination(t), chosen)
	if err == nil || !strings.Contains(err.Error(), string(sendpolicy.UnapprovedDestination)) {
		t.Fatalf("collect error = %v, want the unapproved-destination refusal", err)
	}
	access, diagnoseErr := evidencesource.Diagnose(context.Background(), source, chosen)
	if diagnoseErr != nil {
		t.Fatal(diagnoseErr)
	}
	if access.Destination == nil || access.Destination.Allowed || access.Status != observewindow.Failed {
		t.Fatalf("access = %+v, want a recorded denial", access)
	}
}

// A source that reaches off this machine with no policy selected is denied.
// Selecting a policy is the operator's explicit approval, and nothing supplies
// one implicitly.
func TestCollectRefusesAReachingSourceWhenNoPolicyWasSelected(t *testing.T) {
	source, err := evidencesource.Decode([]byte(strings.NewReplacer(
		"COMMAND", "/bin/sh", "ARGUMENT", "-c", "127.0.0.1:2222", "10.9.9.9:2222").Replace(declaredTransferSource)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if err == nil || !strings.Contains(err.Error(), string(sendpolicy.PolicyRequired)) {
		t.Fatalf("collect error = %v, want the policy-required refusal", err)
	}
}

// Diagnose establishes what access was available by attempting it. An entry it
// could not open is counted, never skipped, and a source that holds one does
// not report that access is available.
func TestDiagnoseReportsTheEntriesThisAccountCouldNotRead(t *testing.T) {
	source, root := directorySource(t, map[string]string{"a.hl7": message, "b.hl7": message})
	if err := os.Chmod(filepath.Join(root, "b.hl7"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "b.hl7"), 0600) })
	if os.Geteuid() == 0 {
		t.Skip("a privileged user reads a mode 0000 file, so there is no unreadable entry to report")
	}
	access, err := evidencesource.Diagnose(context.Background(), source, options(t, ".hl7"))
	if err != nil {
		t.Fatal(err)
	}
	if !access.Listed || access.Selected != 2 || access.Readable != 1 || access.Unreadable != 1 {
		t.Fatalf("access = %+v, want one readable and one unreadable entry", access)
	}
	if access.Status != observewindow.Failed || access.RunState != "execution_error" {
		t.Fatalf("status = %q run state = %q, want failed as an execution error", access.Status, access.RunState)
	}
	if access.Destination != nil {
		t.Fatal("a directory source reaches no destination and must record none")
	}
	if access.Credential.State != evidencesource.CredentialNone {
		t.Fatalf("credential = %+v, want none declared", access.Credential)
	}
}

// A source root that cannot be listed at all is a source readmit could not
// list, not a source with no entries.
func TestDiagnoseReportsASourceItCouldNotListRatherThanAnEmptyOne(t *testing.T) {
	source, root := directorySource(t, nil)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	access, err := evidencesource.Diagnose(context.Background(), source, options(t, ".hl7"))
	if err != nil {
		t.Fatal(err)
	}
	if access.Listed || access.Status != observewindow.Failed || access.Declared != 0 {
		t.Fatalf("access = %+v, want an unlisted source reported as failed", access)
	}
}

// approvedLoopback is a policy approving loopback only, authored here so the
// test never depends on a document outside it.
func approvedLoopback(t *testing.T) sendpolicy.Policy {
	t.Helper()
	policy, err := sendpolicy.DecodePolicy([]byte(`{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.0/8"]}`))
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestDecodeRefusesEveryDeclarationThisReleaseCannotRead(t *testing.T) {
	root := t.TempDir()
	valid := strings.Replace(declaredDirectorySource, "ROOT", root, 1)
	for name, document := range map[string]string{
		"unknown member":       strings.Replace(valid, `"kind": "directory",`, `"kind": "directory", "follow_links": true,`, 1),
		"unknown version":      strings.Replace(valid, "readmit-source/v1", "readmit-source/v2", 1),
		"absent quota":         strings.Replace(valid, `"quota": {"max_entries": 8, "max_entry_bytes": 4096, "max_total_bytes": 16384},`, "", 1),
		"absent quota member":  strings.Replace(valid, `"max_entries": 8, `, "", 1),
		"absent retry":         strings.Replace(valid, ",\n  \"retry\": {\"attempts\": 2, \"backoff\": \"0s\"}", "", 1),
		"unknown kind":         strings.Replace(valid, `"kind": "directory"`, `"kind": "database"`, 1),
		"root on a transfer":   strings.Replace(valid, `"kind": "directory"`, `"kind": "transfer"`, 1),
		"quota past its bound": strings.Replace(valid, `"max_entry_bytes": 4096`, `"max_entry_bytes": 33554432`, 1),
		"attempts past bound":  strings.Replace(valid, `"attempts": 2`, `"attempts": 9`, 1),
		"backoff past bound":   strings.Replace(valid, `"backoff": "0s"`, `"backoff": "1h"`, 1),
	} {
		if _, err := evidencesource.Decode([]byte(document)); err == nil {
			t.Fatalf("%s: decoded a declaration this release must refuse", name)
		}
	}
	if _, err := evidencesource.Decode([]byte(strings.Replace(valid, "readmit-source/v1", "readmit-source/v2", 1))); !errors.Is(err, evidencesource.ErrUnsupportedVersion) {
		t.Fatal("a later version must be reported as unsupported rather than as invalid")
	}
}

// A directory source declares the tree it reads and nothing about a remote one;
// a transfer source declares the opposite. A member that belongs to another kind
// is refused rather than ignored.
func TestDecodeRefusesAMemberThatBelongsToAnotherKind(t *testing.T) {
	root := t.TempDir()
	mixed := strings.Replace(strings.Replace(declaredDirectorySource, "ROOT", root, 1),
		`"root": "`+root+`",`, `"root": "`+root+`", "address": "127.0.0.1:22", "classification": "nonproduction",`, 1)
	if _, err := evidencesource.Decode([]byte(mixed)); err == nil {
		t.Fatal("a directory source must declare no address or classification")
	}
}

// A credential is bound to one purpose and one address. A reference registered
// for an MLLP endpoint is refused rather than handed to a transfer program, and
// a refused reference is reported as refused rather than as no credential.
func TestDiagnoseRefusesACredentialRegisteredForAnotherPurposeOrAddress(t *testing.T) {
	directory := t.TempDir()
	store := filepath.Join(directory, "secrets.json")
	// The locator is a program this test writes; nothing here is a credential
	// value, and no value is resolved by binding.
	reader := filepath.Join(directory, "reader")
	if err := os.WriteFile(reader, []byte("#!/bin/sh\necho test-only-not-a-real-credential\n"), 0700); err != nil {
		t.Fatal(err)
	}
	document := secret.Document{Schema: secret.Schema, References: []secret.Reference{{
		Name: "lab-mllp", Store: secret.OSKeychain, Purpose: secret.MLLPEndpoint,
		Address: "127.0.0.1:2222", Command: reader, Generation: 1, RotatedAt: time.Now().UTC(),
	}}}
	if err := secret.WriteStore(store, document); err != nil {
		t.Fatal(err)
	}
	source, err := evidencesource.Decode([]byte(strings.NewReplacer("COMMAND", reader, "ARGUMENT", "list").Replace(declaredTransferSource)))
	if err != nil {
		t.Fatal(err)
	}
	source.SecretsFile, source.Credential = store, "lab-mllp"
	selected := approvedLoopback(t)
	chosen := options(t, ".hl7")
	chosen.Policy = &selected
	access, err := evidencesource.Diagnose(context.Background(), source, chosen)
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.State != evidencesource.CredentialRefused || access.Status != observewindow.Failed {
		t.Fatalf("access = %+v, want the reference refused for this source's purpose", access)
	}
	// The same reference registered for this source's own purpose binds, and
	// binding reads no value: the rotation state is all that is reported.
	document.References[0].Purpose = secret.SourceEndpoint
	document.References[0].Name = "lab-sftp"
	if err := os.Remove(store); err != nil {
		t.Fatal(err)
	}
	if err := secret.WriteStore(store, document); err != nil {
		t.Fatal(err)
	}
	source.Credential = "lab-sftp"
	access, err = evidencesource.Diagnose(context.Background(), source, chosen)
	if err != nil {
		t.Fatal(err)
	}
	if access.Credential.State != evidencesource.CredentialBound || access.Credential.Rotation != string(secret.RotationNotDeclared) {
		t.Fatalf("credential = %+v, want a bound reference with an undeclared rotation", access.Credential)
	}
}

// Neither retained document carries a credential, an address or a byte of the
// evidence. Both are encoded deterministically, so the same collection produces
// the same bytes on every machine.
func TestRetainedDocumentsCarryNoAddressCredentialOrEvidence(t *testing.T) {
	source, _ := directorySource(t, map[string]string{"a.hl7": message})
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), options(t, ".hl7"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := evidencesource.EncodeCollection(collection)
	if err != nil {
		t.Fatal(err)
	}
	again, err := evidencesource.EncodeCollection(collection)
	if err != nil || string(again) != string(encoded) {
		t.Fatal("a collection receipt must encode the same bytes every time")
	}
	if strings.Contains(string(encoded), source.Root) || strings.Contains(string(encoded), "MSH|") {
		t.Fatal("a collection receipt carries no source path and no byte of the evidence")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(encoded, &declared) != nil || declared.Schema != evidencesource.CollectionSchema {
		t.Fatalf("schema = %q, want %q", declared.Schema, evidencesource.CollectionSchema)
	}
}
