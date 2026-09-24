package evidencesource

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/strictdoc"
)

// The refusals an import of a staged collection earns when the folder it reads
// is not the evidence the receipt names, and when the receipt does not name
// its own evidence. None repeats a name or a digest: which entry is at fault is
// what the receipt and the folder, both files the person holds, show.
var (
	// ErrIdentityMismatch reports a receipt whose identity is not the digest
	// of the collected entries it records.
	ErrIdentityMismatch = errors.New("the collection receipt's identity is not the digest of the entries it records")
	// ErrUnrecordedMember reports a member of the staged folder that the plan
	// selects and the collection did not collect.
	ErrUnrecordedMember = errors.New("the staged folder holds a member the collection did not collect")
	// ErrChangedEntry reports a collected entry whose staged bytes are not the
	// bytes the receipt records.
	ErrChangedEntry = errors.New("a staged entry's bytes are not the bytes the collection receipt records")
	// ErrMissingEntry reports a collected entry the staged folder no longer
	// holds.
	ErrMissingEntry = errors.New("the staged folder no longer holds every entry the collection collected")
	// ErrNotImportable reports a collection whose receipt does not record that
	// it completed.
	ErrNotImportable = errors.New("the collection did not complete; what it staged is not the whole of the declared scope, so it is not imported")
)

const invalidCollection = "invalid source collection receipt"

// collectionDocument is the one strict reading of a receipt. Every member the
// writer writes is required, because it always writes every one of them.
var collectionDocument = strictdoc.Document{
	MaxBytes:    MaxCollectionBytes,
	Schema:      CollectionSchema,
	Required:    []string{"source", "collected_at", "plan", "status", "run_state", "reason", "quota", "entries", "totals", "identity"},
	Invalid:     invalidCollection,
	TooLarge:    "the source collection receipt exceeds its size limit",
	MustDeclare: "a source collection receipt declares its contract version",
	Requires:    "a source collection receipt declares its source, collection time, plan, status, run state, reason, quota, entries, totals and identity",
	Unsupported: ErrUnsupportedVersion,
}

// collectionStatuses are the statuses a collection records: it completed, or
// it failed, was cancelled or ran out of time.
var collectionStatuses = []observewindow.Status{observewindow.Complete, observewindow.Failed, observewindow.Cancelled, observewindow.TimedOut}

var entryStates = []EntryState{Collected, Duplicate, Excluded, Unreadable}

// DecodeCollection reads one readmit-source-collection/v1 receipt exactly as a
// collection writes it. Every member is required at every level, a member the
// contract does not declare is refused, and a later version is unsupported
// rather than invalid; there is no migration and no repair. A receipt whose
// identity is not the digest of the collected entries it records is refused,
// so a receipt that reads names the evidence it says it does.
func DecodeCollection(data []byte) (Collection, error) {
	type plainCollection Collection
	var value plainCollection
	if err := collectionDocument.Decode(data, &value); err != nil {
		return Collection{}, err
	}
	collection := Collection(value)
	if err := collection.Plan.Validate(); err != nil {
		return Collection{}, err
	}
	if !slices.Contains(collectionStatuses, collection.Status) || collection.RunState != collection.Status.RunState() {
		return Collection{}, errors.New("a source collection receipt records a status this release does not write")
	}
	for _, entry := range collection.Entries {
		if !slices.Contains(entryStates, entry.State) {
			return Collection{}, errors.New("a source collection receipt records an entry state this release does not write")
		}
	}
	if collection.Identity != collectionIdentity(collection.Entries) {
		return Collection{}, ErrIdentityMismatch
	}
	return collection, nil
}

// Importable reports whether what this collection staged may become evidence.
// Only a completed collection staged the whole of its declared scope; what a
// failed, cancelled or timed-out one staged is evidence of an attempt, and an
// import of it would read as the source.
func (c Collection) Importable() error {
	if c.Status != observewindow.Complete {
		return ErrNotImportable
	}
	return nil
}

// VerifyStaged reports whether a folder an import read under this
// collection's plan holds exactly the evidence the collection staged: every
// collected entry, under the name it was collected under, with the size and
// digest the receipt records, and no other member the plan selects. It judges
// what the import read rather than reading the folder again, so the bytes it
// verifies are the bytes the import writes.
//
// A member the plan does not select is not evidence of this collection's. The
// import records it as excluded, as it would in any folder, and it never
// reaches the case.
func (c Collection) VerifyStaged(folder importer.Container) error {
	collected := make(map[string]Entry, len(c.Entries))
	for _, entry := range c.Entries {
		if entry.State == Collected {
			collected[entry.Name] = entry
		}
	}
	for _, member := range folder.Members {
		if member.State != importer.Included {
			continue
		}
		entry, recorded := collected[member.Name]
		if !recorded {
			return ErrUnrecordedMember
		}
		if int64(member.Size) != entry.Size || member.SHA256 != entry.SHA256 {
			return ErrChangedEntry
		}
		delete(collected, member.Name)
	}
	if len(collected) > 0 {
		return ErrMissingEntry
	}
	return nil
}

// UnmarshalJSON on each nested type applies its own presence check and its
// own unknown-member refusal rather than inheriting the enclosing receipt's.
func (i *Identity) UnmarshalJSON(data []byte) error {
	type plainIdentity Identity
	return strictMembers(data, (*plainIdentity)(i), "name", "kind", "scope")
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	type plainEntry Entry
	return strictMembers(data, (*plainEntry)(e), "name", "state", "declared_size", "size", "sha256",
		"records", "occurrences", "attempts", "duplicate_of", "reason")
}

func (t *Totals) UnmarshalJSON(data []byte) error {
	type plainTotals Totals
	return strictMembers(data, (*plainTotals)(t), "declared", "collected", "duplicates", "excluded", "unreadable",
		"not_read", "bytes", "records", "occurrences")
}

// strictMembers requires every named member, refusing an explicit null as a
// missing one, and then decodes refusing any member the type does not declare.
func strictMembers(data []byte, into any, names ...string) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New(invalidCollection)
	}
	for _, name := range names {
		value, present := members[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New(invalidCollection)
		}
	}
	if err := json.Unmarshal(data, into, json.RejectUnknownMembers(true)); err != nil {
		return errors.New(invalidCollection)
	}
	return nil
}
