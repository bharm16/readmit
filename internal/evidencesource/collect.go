package evidencesource

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"hash"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
)

// EntryState is what a collection did with one declared entry. Every entry the
// source listed carries one, so a collection accounts for its whole listing
// rather than reporting only what it kept.
type EntryState string

const (
	// Collected means one attempt read the entry whole and its bytes were
	// staged unchanged.
	Collected EntryState = "collected"
	// Duplicate means an entry already collected in this run holds exactly
	// these bytes. The first is staged; this one is recorded and not staged
	// again, with the entry it duplicates named.
	Duplicate EntryState = "duplicate"
	// Excluded means the declared plan's members do not select this entry. It
	// is recorded with that reason rather than omitted.
	Excluded EntryState = "excluded"
	// Unreadable means no attempt read the entry whole. It is never an empty
	// entry: a read that did not finish read nothing.
	Unreadable EntryState = "unreadable"
)

// Entry is one declared entry as the collection left it. It carries the name
// the source knows it by, what it holds, and what happened — never a byte of
// the entry and never a decoded value.
type Entry struct {
	Name  string     `json:"name"`
	State EntryState `json:"state"`
	// DeclaredSize is what the source's own listing said the entry holds, and
	// Size is what was read. A collected entry has both, and they are equal,
	// because an attempt that produced a different length is not a completed
	// read. Every other state carries the declared length and a size of zero:
	// a read that did not finish read nothing, and saying so in two members is
	// what keeps "the source said 4 KiB" separate from "readmit has 4 KiB".
	DeclaredSize int64  `json:"declared_size"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	// Records and Occurrences are what the declared plan divided this entry
	// into, counted by the same streaming reader an import of the same bytes
	// would use.
	Records     int64 `json:"records"`
	Occurrences int64 `json:"occurrences"`
	// Attempts is how many reads it took. A collected entry was read whole by
	// one single attempt; earlier attempts contributed no byte to it.
	Attempts int `json:"attempts"`
	// DuplicateOf names the collected entry this one repeats, and is empty for
	// every other state.
	DuplicateOf string `json:"duplicate_of"`
	// Reason is the bounded diagnostic for an excluded or unreadable entry, and
	// is empty for the two that produced evidence.
	Reason string `json:"reason"`
}

// Totals reconcile the listing. Declared is every entry the source named;
// NotRead counts what it named and readmit deliberately did not open.
type Totals struct {
	Declared    int   `json:"declared"`
	Collected   int   `json:"collected"`
	Duplicates  int   `json:"duplicates"`
	Excluded    int   `json:"excluded"`
	Unreadable  int   `json:"unreadable"`
	NotRead     int   `json:"not_read"`
	Bytes       int64 `json:"bytes"`
	Records     int64 `json:"records"`
	Occurrences int64 `json:"occurrences"`
}

// Collection is the retained record of one collection, written beside the bytes
// it staged. It carries names, sizes, digests and counts; no byte of the
// evidence, no decoded value, no credential and no source address reaches it.
//
// [DecodeCollection] is its reader, which an import of the staged directory
// goes through: the contract it reads is exactly the one written here.
type Collection struct {
	Schema      string    `json:"schema"`
	Source      Identity  `json:"source"`
	CollectedAt time.Time `json:"collected_at"`
	// Plan is the readmit-import-plan/v1 every entry was streamed under,
	// recorded verbatim, so the declarations a collection ran under stay with
	// the bytes it staged.
	Plan importer.Plan `json:"plan"`
	// Status and RunState are observewindow's source-neutral vocabulary. Only
	// complete says the staged bytes are the whole of what this scope held;
	// every other status is an execution error, and the entries below describe
	// an attempt rather than a source.
	Status   observewindow.Status `json:"status"`
	RunState string               `json:"run_state"`
	Reason   string               `json:"reason"`
	Quota    Quota                `json:"quota"`
	Entries  []Entry              `json:"entries"`
	Totals   Totals               `json:"totals"`
	// Identity is a digest over the staged entries' names and contents only,
	// following ADR-0002: no timestamp, no absolute path and nothing about the
	// machine enters it, so the same evidence collected twice identifies the
	// same way.
	//
	// It carries no verdict about the case bundle bounds. A collection stages a
	// directory, and how many sources, occurrences and bytes an import of that
	// directory may write is decided by the import that reads it, against the
	// whole container it is given. Answering it here from a collection's own
	// totals would be a claim about evidence nothing checked.
	Identity string `json:"identity"`
}

// EncodeCollection renders one collection as the document a command retains.
func EncodeCollection(collection Collection) ([]byte, error) {
	data, err := json.Marshal(collection, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the source collection receipt")
	}
	return append(data, '\n'), nil
}

// ErrIncomplete reports a collection that did not complete. The receipt it is
// returned with records what the attempt reached; the error is what stops a
// caller from reading those counts as a description of the source.
var ErrIncomplete = errors.New("the collection did not complete")

// Collect stages one source's evidence in a new directory and returns the
// receipt of what it did.
//
// It refuses before it reads. The destination is reserved, the source's own
// listing is obtained, and the declared quota is applied to what that listing
// declares — so a source past a limit is refused rather than collected in part,
// and the refusal costs nothing and reads nothing.
//
// Each selected entry is then streamed through [importer.Scan] under the
// declared plan, copied to the staging directory as it is read, and digested as
// it is copied. One record and one parsing batch are resident whatever the
// entry's length; no entry is ever held whole.
//
// A collection that did not complete returns the receipt and [ErrIncomplete].
// Its status names how it failed, and the entries it did collect stay staged:
// bytes that were read are evidence, and the status is what says they are not
// the whole of the scope.
func Collect(ctx context.Context, source Source, output string, options Options) (Collection, error) {
	if err := source.Validate(); err != nil {
		return Collection{}, err
	}
	if err := options.Plan.Validate(); err != nil {
		return Collection{}, err
	}
	if source.Kind == API {
		return Collection{}, errors.New("this release collects from no application interface; `readmit source diagnose` records the unsupported source, and docs/source.md states the read-only contract one must satisfy")
	}
	if source.Reaches() {
		if decision := decide(ctx, source, options); !decision.Allowed {
			return Collection{}, errors.New("the source's destination was not approved: " + string(decision.Reason))
		}
	}
	// The declared credential is bound before anything is read, so a reference
	// that is missing, registered for another purpose or scoped to another
	// endpoint is reported as the permission problem it is rather than as a
	// transfer program that would not answer. Binding reads no value.
	if report := bindCredential(source, options.now()); report.State == CredentialRefused {
		return Collection{}, errors.New(report.Reason)
	}
	// The listing is obtained before the destination exists, so a source that
	// cannot be listed never leaves an empty collection directory behind.
	found, err := list(ctx, source)
	if err != nil {
		return Collection{}, err
	}
	// Every bound is applied to what the source declares before anything is
	// read. A collection past one is refused, never cut short: a prefix of a
	// source is not the source.
	if past := source.Quota.exceededBy(found.selection(options.Plan)); len(past) > 0 {
		return Collection{}, errors.New("the declared source exceeds its own quota (" + strings.Join(past, ", ") + "); raise the quota deliberately or collect the source in parts")
	}
	destination, err := reservedDirectory(output)
	if err != nil {
		return Collection{}, err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return Collection{}, errors.New("cannot open the new collection directory")
	}
	defer root.Close()
	run := &collector{source: source, options: options, root: root, plan: scanPlan(options.Plan)}
	return run.collect(ctx, found)
}

// scanPlan is the declared plan as one entry is streamed under it. The declared
// members select which entries of the source are in scope, which is a question
// about the listing rather than about a stream, and [importer.Scan] refuses a
// plan that still carries them.
func scanPlan(plan importer.Plan) importer.Plan {
	plan.Members = []string{}
	return plan
}

// collector holds one collection: the declarations it runs under, the directory
// it stages into, and the digests it has already seen.
type collector struct {
	source  Source
	options Options
	root    *os.Root
	plan    importer.Plan
	// seen maps the digest of a collected entry to the name it was collected
	// under, which is how a repeat of the same bytes is recognised. It holds
	// digests, never contents.
	seen map[string]string
}

func (c *collector) collect(ctx context.Context, found listing) (Collection, error) {
	record := Collection{
		Schema: CollectionSchema, Source: c.source.Identity(), CollectedAt: c.options.now(),
		Plan: c.options.Plan, Quota: c.source.Quota, Entries: []Entry{},
		Totals: Totals{Declared: len(found.entries) + found.notRead, NotRead: found.notRead},
		Status: observewindow.Complete,
	}
	c.seen = make(map[string]string, len(found.entries))
	// Entries are collected in the source's own declared order, so a repeat of
	// the same bytes is always recorded against the entry that was collected
	// first rather than against whichever one a map happened to visit.
	for index, declared := range found.entries {
		if ctx.Err() != nil {
			return c.stopped(ctx, record, found.entries[index:])
		}
		if !c.options.Plan.Selects(declared.name) {
			record.Entries = append(record.Entries, Entry{
				Name: declared.name, State: Excluded, DeclaredSize: declared.size,
				Reason: importer.ReasonSuffix,
			})
			record.Totals.Excluded++
			continue
		}
		entry := c.entry(ctx, declared)
		record.Entries = append(record.Entries, entry)
		switch entry.State {
		case Collected:
			record.Totals.Collected++
			record.Totals.Bytes += entry.Size
			record.Totals.Records += entry.Records
			record.Totals.Occurrences += entry.Occurrences
		case Duplicate:
			record.Totals.Duplicates++
		default:
			record.Totals.Unreadable++
		}
	}
	// A cancellation that arrived while the last entry was being read ends the
	// loop rather than reaching the guard above it, so it is asked again here.
	// A collection is reported as cancelled whatever else it interrupted: an
	// entry a cancellation stopped is not an entry the source could not read.
	if ctx.Err() != nil {
		return c.stopped(ctx, record, nil)
	}
	if record.Totals.Unreadable > 0 {
		return c.finish(record, observewindow.Failed,
			"the source holds entries this collection could not read; what it staged is not the whole of the declared scope")
	}
	return c.finish(record, observewindow.Complete, "")
}

// stopped closes a collection the caller ended. Every entry it never reached is
// recorded as one it never reached, so the receipt still accounts for the whole
// listing rather than trailing off where the collection stopped.
//
// A deadline and a cancellation are separate statuses because they are separate
// facts: running out of time is a timeout, never a negative result about the
// source, and neither is a collection that completed.
func (c *collector) stopped(ctx context.Context, record Collection, remaining []listedEntry) (Collection, error) {
	status, reason := observewindow.Cancelled, "the collection was cancelled; what it staged is not the whole of the declared scope"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status, reason = observewindow.TimedOut, "the collection ran out of time; what it staged is not the whole of the declared scope"
	}
	for _, declared := range remaining {
		record.Entries = append(record.Entries, Entry{
			Name: declared.name, State: Unreadable, DeclaredSize: declared.size,
			Reason: "the collection stopped before this entry was reached",
		})
		record.Totals.Unreadable++
	}
	return c.finish(record, status, reason)
}

func (c *collector) finish(record Collection, status observewindow.Status, reason string) (Collection, error) {
	record.Status, record.RunState, record.Reason = status, status.RunState(), reason
	record.Identity = collectionIdentity(record.Entries)
	if status == observewindow.Complete {
		return record, nil
	}
	return record, ErrIncomplete
}

// entry reads one declared entry, retrying only what is safe to retry.
//
// A retry never turns an uncertain read into a confident one. Every attempt
// starts the entry again from nothing: a partial staged copy is removed before
// the next attempt, so a collected entry was read whole by one single attempt
// and its digest is over exactly the bytes that attempt read.
//
// Only a delivery failure is retried — an entry that could not be opened, a
// stream that stopped, a program that did not complete, and a source that
// returned a different number of bytes than it declared, which is what a
// transfer that dropped after a clean exit looks like. A refusal the bytes
// themselves caused is never retried, because reading the same bytes again
// produces the same refusal and repeating it would only make a declaration
// error look intermittent. Neither is a quota refusal or a cancellation.
func (c *collector) entry(ctx context.Context, declared listedEntry) Entry {
	entry := Entry{Name: declared.name, DeclaredSize: declared.size, State: Unreadable}
	for attempt := 1; attempt <= c.source.Retry.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			entry.Attempts, entry.Reason = attempt-1, "the collection was cancelled before this entry was read"
			return entry
		}
		if attempt > 1 {
			if !sleep(ctx, c.source.Retry.Wait()) {
				entry.Attempts, entry.Reason = attempt-1, "the collection was cancelled between attempts to read this entry"
				return entry
			}
		}
		entry.Attempts = attempt
		read, transport, err := c.read(ctx, declared)
		if err == nil {
			return c.settle(entry, read)
		}
		entry.Reason = err.Error()
		if !transport {
			return entry
		}
	}
	return entry
}

// settle decides what a completed read of an entry means. Bytes identical to an
// entry already collected are a duplicate: the staged copy is removed and the
// entry it repeats is named, so one collection never stages the same evidence
// twice under two names.
//
// Sameness is decided by the bytes alone. A name is what a source calls an
// entry, not what the entry is, and two names over identical bytes are one
// piece of evidence; the receipt names both, so the source's own listing is
// reconstructed from it rather than from the staged directory. Nothing about
// the machine, and no timestamp or absolute path, enters the comparison.
func (c *collector) settle(entry Entry, read reading) Entry {
	entry.Size, entry.SHA256 = read.size, read.digest
	entry.Records, entry.Occurrences = read.records, read.occurrences
	if first, repeated := c.seen[read.digest]; repeated {
		if err := c.root.Remove(entry.Name); err != nil {
			entry.State, entry.Reason = Unreadable, "the repeated entry could not be removed from the collection"
			return entry
		}
		entry.State, entry.DuplicateOf = Duplicate, first
		return entry
	}
	c.seen[read.digest] = entry.Name
	entry.State = Collected
	return entry
}

// reading is what one completed attempt produced.
type reading struct {
	size        int64
	digest      string
	records     int64
	occurrences int64
}

// read performs one attempt. It reports whether the failure was a transport
// failure, which is the only kind another attempt could answer differently.
func (c *collector) read(ctx context.Context, declared listedEntry) (reading, bool, error) {
	// The staged copy is created exclusively inside the reserved collection
	// directory, so a listing that named the same entry twice cannot overwrite
	// what the first one staged.
	staged, err := c.root.OpenFile(declared.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return reading{}, false, errors.New("the entry could not be staged in the collection directory")
	}
	stream, err := open(ctx, c.source, declared)
	if err != nil {
		staged.Close()
		c.root.Remove(declared.name)
		return reading{}, true, err
	}
	digest := sha256.New()
	source := &countingReader{from: stream, limit: c.source.Quota.MaxEntryBytes}
	result, scanErr := importer.Scan(ctx, io.TeeReader(source, io.MultiWriter(staged, digest)), importer.ScanOptions{Plan: c.plan})
	closeErr := stream.Close()
	syncErr := staged.Sync()
	if err := staged.Close(); syncErr == nil {
		syncErr = err
	}
	failure, transport := c.failure(source, scanErr, closeErr, syncErr, declared)
	if failure != nil {
		// Nothing a failed attempt read is kept. The next attempt starts the
		// entry again from nothing, which is what makes a completed read the
		// work of one attempt rather than of several stitched together.
		c.root.Remove(declared.name)
		return reading{}, transport, failure
	}
	return reading{size: source.read, digest: hex.EncodeToString(digest.Sum(nil)), records: result.Records, occurrences: result.Occurrences}, false, nil
}

// failure names what went wrong with one attempt and whether another attempt
// could answer differently. The order is deliberate: a cancellation is reported
// as a cancellation whatever else it interrupted, a quota refusal before
// whatever a truncated read looked like to the parser, and a refusal the
// declaration caused is never a delivery failure.
func (c *collector) failure(source *countingReader, scanErr, closeErr, syncErr error, declared listedEntry) (error, bool) {
	switch {
	case errors.Is(scanErr, importer.ErrScanCancelled):
		return errors.New("the collection was cancelled while this entry was being read"), false
	case source.over:
		return errors.New("the entry is larger than the declared per-entry quota allows; it was refused rather than collected in part"), false
	case source.failed != nil:
		return errors.New("the source stopped returning this entry's bytes before it ended"), true
	case closeErr != nil:
		return errors.New("the source did not complete this entry"), true
	case syncErr != nil:
		return errors.New("the entry could not be written to the collection directory"), false
	case scanErr != nil:
		return scanErr, false
	case source.read != declared.size:
		return errors.New("the source returned a different number of bytes than it declared for this entry"), true
	}
	return nil, false
}

// countingReader counts what was read, stops an entry past the declared
// per-entry quota, and keeps the source's own failure separate from whatever
// the reader above it made of the bytes. That separation is the whole of how a
// transport failure is told apart from a declaration the bytes contradict.
type countingReader struct {
	from   io.Reader
	limit  int64
	read   int64
	over   bool
	failed error
}

func (c *countingReader) Read(p []byte) (int, error) {
	// One byte past the limit is read on purpose. An entry exactly at the
	// declared limit fits, and an entry past it is recognised by the byte that
	// does not fit rather than by the length the source claimed for it.
	if room := c.limit - c.read + 1; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := c.from.Read(p)
	c.read += int64(n)
	if c.read > c.limit {
		c.over = true
		return n, errors.New("entry exceeds the declared per-entry quota")
	}
	if err != nil && err != io.EOF {
		c.failed = err
	}
	return n, err
}

// sleep waits between attempts and reports whether the wait completed. A
// cancellation during a backoff stops the collection rather than being noticed
// only when the next attempt fails.
func sleep(ctx context.Context, wait time.Duration) bool {
	if wait <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// collectionIdentity is the digest a receipt names its staged evidence by: the
// ADR-0002 scheme in its streaming form, over each collected entry's name and
// digest in the source's declared order, where the entry's own digest stands
// for its bytes because a collection never holds one. No timestamp and no
// absolute path enters it. The collection that writes a receipt and the reader
// that verifies one both compute it here.
func collectionIdentity(entries []Entry) string {
	identity := sha256.New()
	identity.Write([]byte(CollectionSchema + "\n"))
	for _, entry := range entries {
		if entry.State == Collected {
			writeSized(identity, []byte(entry.Name))
			writeSized(identity, []byte(entry.SHA256))
		}
	}
	return hex.EncodeToString(identity.Sum(nil))
}

// writeSized hashes one length-prefixed value, which is the framing a case
// bundle identity uses so that no two different collections can hash the same.
func writeSized(h hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	h.Write(size[:])
	h.Write(value)
}
