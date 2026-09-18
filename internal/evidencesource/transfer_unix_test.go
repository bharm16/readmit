//go:build !windows

package evidencesource_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// transferSource writes a read-only transfer program and the declaration that
// names it. The program is written here rather than shipped, so a test never
// reaches a host outside itself and no fixture has to be committed for it.
//
// Every program below implements the same two-verb contract the documentation
// states: `list` prints one line per entry as a byte count, a tab and a name,
// and `get NAME` streams that entry's bytes.
func transferSource(t *testing.T, program string) evidencesource.Source {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "transfer.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+program), 0700); err != nil {
		t.Fatal(err)
	}
	source, err := evidencesource.Decode([]byte(strings.NewReplacer(
		"COMMAND", path, "ARGUMENT", filepath.Join(directory, "state")).Replace(declaredTransferSource)))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// transferOptions selects the loopback policy, because a transfer source is a
// destination and readmit decides one before it reaches it.
func transferOptions(t *testing.T) evidencesource.Options {
	t.Helper()
	selected := approvedLoopback(t)
	chosen := options(t, ".hl7")
	chosen.Policy = &selected
	return chosen
}

// serving prints a listing of one entry and streams it, which is the whole of
// what a conforming read-only transfer program does. The message is written by
// the program so nothing but the program's own output reaches the collection.
const serving = `
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  printf '%s' "$MESSAGE" ;;
esac
`

func withMessage(program string) string {
	return "MESSAGE='" + message + "'\n" + program
}

func TestTransferCollectionReadsThroughTheDeclaredProgram(t *testing.T) {
	source := transferSource(t, withMessage(serving))
	output := destination(t)
	collection, err := evidencesource.Collect(context.Background(), source, output, transferOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if collection.Status != observewindow.Complete || collection.Totals.Collected != 1 {
		t.Fatalf("collection = %+v status %q, want one entry collected", collection.Totals, collection.Status)
	}
	staged, err := os.ReadFile(filepath.Join(output, "a.hl7"))
	if err != nil || string(staged) != message {
		t.Fatalf("staged bytes = %q (err %v), want the program's own bytes unchanged", staged, err)
	}
	if collection.Entries[0].Attempts != 1 {
		t.Fatalf("attempts = %d, want one", collection.Entries[0].Attempts)
	}
}

// A transfer that stops part way through is a failed read, not a short entry.
// The next attempt starts the entry again from nothing, so a collected entry
// was read whole by one single attempt rather than stitched out of several.
func TestTransferCollectionRetriesATransportFailureAndStartsTheEntryAgain(t *testing.T) {
	source := transferSource(t, withMessage(`
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)
  if [ -f "$state" ]; then printf '%s' "$MESSAGE"; else : > "$state"; printf 'MSH|^~'; exit 1; fi ;;
esac
`))
	output := destination(t)
	collection, err := evidencesource.Collect(context.Background(), source, output, transferOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if collection.Totals.Collected != 1 || collection.Entries[0].Attempts != 2 {
		t.Fatalf("collection = %+v entry %+v, want the entry collected on the second attempt", collection.Totals, collection.Entries[0])
	}
	staged, err := os.ReadFile(filepath.Join(output, "a.hl7"))
	if err != nil || string(staged) != message {
		t.Fatalf("staged bytes = %q (err %v), want only the bytes the successful attempt read", staged, err)
	}
}

// A transfer that never completes is reported as a read that did not complete,
// with the attempts it took, and never as an entry that held nothing.
func TestTransferCollectionReportsAnEntryNoAttemptCompleted(t *testing.T) {
	source := transferSource(t, withMessage(`
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  printf 'MSH|^~'; exit 1 ;;
esac
`))
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), transferOptions(t))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if collection.Status != observewindow.Failed || collection.RunState != "execution_error" {
		t.Fatalf("status = %q run state = %q, want failed as an execution error", collection.Status, collection.RunState)
	}
	entry := collection.Entries[0]
	if entry.State != evidencesource.Unreadable || entry.Attempts != source.Retry.Attempts || entry.Size != 0 {
		t.Fatalf("entry = %+v, want every attempt used and nothing collected", entry)
	}
}

// A source that returns fewer bytes than it declared is a source readmit cannot
// vouch for. It is reported rather than staged as a shorter entry that looks
// complete.
func TestTransferCollectionRefusesAnEntryShorterThanTheSourceDeclared(t *testing.T) {
	source := transferSource(t, withMessage(`
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  printf '%s' "MSH|^~\&|SEND|" ;;
esac
`))
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), transferOptions(t))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if !strings.Contains(collection.Entries[0].Reason, "different number of bytes than it declared") {
		t.Fatalf("entry = %+v, want the declared-length disagreement reported", collection.Entries[0])
	}
}

// A refusal the bytes themselves caused is never retried: reading the same
// bytes again produces the same refusal, and repeating it would make a
// declaration error look intermittent.
func TestTransferCollectionDoesNotRetryARefusalTheBytesCaused(t *testing.T) {
	// The program serves an MLLP-framed entry under a plan that declares raw
	// framing, which the streaming reader refuses before it divides anything.
	source := transferSource(t, `
FRAMED="$(printf '\013MSH|^~\\&|A\015\034\015')"
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#FRAMED}" ;;
get)  printf '%s' "$FRAMED"; printf 'attempt\n' >> "$state" ;;
esac
`)
	collection, err := evidencesource.Collect(context.Background(), source, destination(t), transferOptions(t))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if collection.Entries[0].Attempts != 1 {
		t.Fatalf("attempts = %d, want the declaration refusal attempted exactly once", collection.Entries[0].Attempts)
	}
	if !strings.Contains(collection.Entries[0].Reason, "framing") {
		t.Fatalf("entry = %+v, want the declared-framing refusal", collection.Entries[0])
	}
}

// A listing readmit cannot read is a source it could not list. It is an error
// rather than a source with no entries, and nothing is staged for it.
func TestTransferCollectionRefusesAListingItCannotRead(t *testing.T) {
	for name, program := range map[string]string{
		"no byte count":     "state=\"$1\"; shift\ncase \"$1\" in list) printf 'a.hl7\\n' ;; esac\n",
		"a traversing name": "state=\"$1\"; shift\ncase \"$1\" in list) printf '4\\t../escape.hl7\\n' ;; esac\n",
		"an absolute name":  "state=\"$1\"; shift\ncase \"$1\" in list) printf '4\\t/etc/hosts\\n' ;; esac\n",
		"a failing program": "exit 3\n",
	} {
		source := transferSource(t, program)
		output := destination(t)
		if _, err := evidencesource.Collect(context.Background(), source, output, transferOptions(t)); err == nil {
			t.Fatalf("%s: collected from a listing readmit must refuse", name)
		}
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatalf("%s: a refused listing must leave no collection directory behind", name)
		}
	}
}

// A declared credential is resolved and written to the transfer program's
// standard input, never to an argument, and the collection that used it carries
// no trace of the value.
func TestTransferCollectionPresentsACredentialOnStandardInputOnly(t *testing.T) {
	directory := t.TempDir()
	reader := filepath.Join(directory, "reader")
	if err := os.WriteFile(reader, []byte("#!/bin/sh\nprintf 'test-only-collection-credential\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(directory, "secrets.json")
	writeSourceCredential(t, store, reader, "lab-sftp", "127.0.0.1:2222")
	// The program refuses to serve unless the expected credential arrives on
	// standard input, so a collection that succeeds proves it was presented
	// there. The arguments it was given are recorded so the test can show that
	// no credential reached one.
	source := transferSource(t, withMessage(`
state="$1"; shift
printf '%s\n' "$@" > "$state.arguments"
read -r presented
[ "$presented" = "test-only-collection-credential" ] || exit 4
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  printf '%s' "$MESSAGE" ;;
esac
`))
	source.SecretsFile, source.Credential = store, "lab-sftp"
	output := destination(t)
	collection, err := evidencesource.Collect(context.Background(), source, output, transferOptions(t))
	if err != nil || collection.Totals.Collected != 1 {
		t.Fatalf("collection = %+v (err %v), want the credential accepted on standard input", collection.Totals, err)
	}
	recorded, err := os.ReadFile(filepath.Join(filepath.Dir(source.Command), "state.arguments"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorded), "test-only-collection-credential") {
		t.Fatal("a credential must never reach a program argument")
	}
	encoded, err := evidencesource.EncodeCollection(collection)
	if err != nil || strings.Contains(string(encoded), "test-only-collection-credential") {
		t.Fatalf("a retained receipt must carry no credential value (err %v)", err)
	}
}

// A collection stopped while its last entry was being read is stopped, not
// failed: an entry a deadline interrupted is not an entry the source could not
// read, and running out of time is never a negative result about the source.
//
// The last entry is the case that matters. A stop that arrives earlier is seen
// by the guard at the top of the collection loop; one that arrives during the
// last entry ends the loop instead, and was reported as a failed collection
// before this was fixed.
func TestTransferCollectionStoppedOnItsLastEntryReportsTheStopNotAFailure(t *testing.T) {
	source := transferSource(t, withMessage(`
state="$1"; shift
case "$1" in
list) printf '%d\ta.hl7\n' "${#MESSAGE}" ;;
get)  sleep 30 ;;
esac
`))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	collection, err := evidencesource.Collect(ctx, source, destination(t), transferOptions(t))
	if !errors.Is(err, evidencesource.ErrIncomplete) {
		t.Fatalf("collect error = %v, want %v", err, evidencesource.ErrIncomplete)
	}
	if collection.Status != observewindow.TimedOut || collection.RunState != "timed_out" {
		t.Fatalf("status = %q run state = %q, want the timeout in the durable-run vocabulary", collection.Status, collection.RunState)
	}
	if collection.Totals.Collected != 0 || collection.Totals.Unreadable != 1 {
		t.Fatalf("totals = %+v, want the interrupted entry reported and nothing collected", collection.Totals)
	}
	// The listing is still accounted for: an entry the collection never reached
	// is recorded as one it never reached rather than left out.
	// Declared counts the entries readmit deliberately did not open too, so
	// what a receipt lists is every entry the source named that a collection
	// could have read.
	if listed := collection.Totals.Declared - collection.Totals.NotRead; len(collection.Entries) != listed {
		t.Fatalf("entries = %d of %d listed, want every listed entry accounted for", len(collection.Entries), listed)
	}
}
