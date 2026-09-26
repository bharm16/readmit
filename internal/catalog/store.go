package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// Store is the catalog of one project directory. It is one writer's view: the
// project is written by one process at a time, as every readmit artifact is.
type Store struct {
	root string
}

// Open resolves a project directory for its catalog. It reads and writes
// nothing.
func Open(root string) (*Store, error) {
	physical, err := artifactpath.Directory(root)
	if err != nil {
		return nil, errors.New("a project must be an existing directory that is not a symbolic link")
	}
	return &Store{root: physical}, nil
}

// open opens the project directory itself.
func (s *Store) open() (*os.Root, error) {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, errors.New("cannot open the project folder")
	}
	return root, nil
}

// Root is the resolved project directory.
func (s *Store) Root() string { return s.root }

// Read reads the catalog. present is false for a project whose catalog was
// never recorded, which is an empty catalog rather than a failure.
func (s *Store) Read() (Document, bool, error) {
	root, err := s.open()
	if err != nil {
		return Document{}, false, err
	}
	defer root.Close()
	data, err := store.ReadIn(root, path.Join(Folder, DocumentName))
	if errors.Is(err, fs.ErrNotExist) {
		return Document{}, false, nil
	}
	if err != nil {
		return Document{}, false, err
	}
	document, err := Decode(data)
	if err != nil {
		return Document{}, false, err
	}
	return document, true, nil
}

// Fresh is the catalog a project receives the first time the application
// records one: a new project identity and no items.
func Fresh(now time.Time) (Document, error) {
	id, err := NewID()
	if err != nil {
		return Document{}, err
	}
	return Document{Schema: Schema, Project: Project{ID: id, RecordedAt: Stamp(now)}, Items: []Item{}, Intents: []Intent{}}, nil
}

// Write replaces the catalog atomically with document.
func (s *Store) Write(document Document) error {
	data, err := Encode(document)
	if err != nil {
		return err
	}
	root, err := s.managed()
	if err != nil {
		return err
	}
	defer root.Close()
	return store.ReplaceIn(root, DocumentName, data)
}

// managed opens the project's .readmit folder, creating it if the project
// has none yet. A .readmit entry that is not a folder of the project itself —
// a symbolic link, a file — is refused, never written through.
func (s *Store) managed() (*os.Root, error) {
	project, err := s.open()
	if err != nil {
		return nil, err
	}
	defer project.Close()
	if err := project.Mkdir(Folder, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := project.Lstat(Folder)
	if err != nil || !info.IsDir() {
		return nil, errors.New("the project's " + Folder + " entry is not a folder of the project")
	}
	return project.OpenRoot(Folder)
}

// Update reads the catalog, recording a fresh one when there is none, applies
// change and writes the result when change reports that it changed anything.
// It answers the document as it now stands.
func (s *Store) Update(now time.Time, change func(*Document) (bool, error)) (Document, error) {
	unlock, err := s.lock()
	if err != nil {
		return Document{}, err
	}
	defer unlock()
	document, present, err := s.Read()
	if err != nil {
		return Document{}, err
	}
	if !present {
		if document, err = Fresh(now); err != nil {
			return Document{}, err
		}
	}
	changed, err := change(&document)
	if err != nil {
		return Document{}, err
	}
	if changed || !present {
		if err := s.Write(document); err != nil {
			return Document{}, err
		}
	}
	return document, nil
}

// Staged is one member of a draft: its role, the file name it is staged
// under, and its bytes.
type Staged struct {
	Role string
	File string
	Data []byte
}

// Draft is one whole-object save. ItemID empty creates a new object with a
// new identity; Base is the revision the editor started from, empty for an
// object with no published revision. Intent is the submission identity the
// window allocated once on a deliberate submit, and Digest the digest of the
// whole submission, so the same submission repeated is recognized and a
// different submission under the same identity is refused.
type Draft struct {
	Kind    string
	ItemID  string
	Name    string
	Base    string
	Intent  string
	Digest  string
	Members []Staged
}

// Verifier reads a staged revision through its kind's own readers: every
// member's file, by role, at once, so members that only mean something
// together are verified together.
type Verifier func(files map[string]string) error

// Saved is the outcome of a save that published, or that recognized a
// submission already published.
type Saved struct {
	Item     Item
	Revision int
	Replayed bool
}

// Conflict is a save whose base is not the item's current revision. Nothing
// was published and the draft is the caller's to keep.
type Conflict struct{ Current string }

func (c *Conflict) Error() string { return "the object changed since this edit began" }

var (
	// ErrIntentReused refuses a different submission under an identity that
	// already named another.
	ErrIntentReused = errors.New("this submission identity already named different content")
	// ErrNoItem refuses a save of an object the catalog does not hold.
	ErrNoItem = errors.New("the catalog holds no such object")
	// ErrIncomplete reports a save that staged work it could not publish. The
	// previous revision stays current; the work is recoverable under its
	// submission identity.
	ErrIncomplete = errors.New("the save did not complete; the previous revision is still current")
	// ErrTooManyPending refuses a new save while the project holds as many
	// interrupted saves as it keeps; one is retried or discarded first.
	ErrTooManyPending = errors.New("the project holds as many interrupted saves as it keeps; retry or discard one first")
)

// Options are the save's collaborators: the clock, and the fault a test
// injects at a named point of the publication.
type Options struct {
	Now   func() time.Time
	Fault func(point string) error
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) fault(point string) error {
	if o.Fault != nil {
		return o.Fault(point)
	}
	return nil
}

// The points a publication passes, in order. A fault at any of them stops the
// save there, as a crash would.
const (
	PointPending   = "pending"
	PointMember    = "member:" // followed by the member's role
	PointVerified  = "verified"
	PointPublished = "published"
)

// pending is the record written before the first member file: everything
// recovery needs to complete or refuse the publication.
type pending struct {
	Schema    string   `json:"schema"`
	Intent    string   `json:"intent"`
	Digest    string   `json:"digest"`
	Kind      string   `json:"kind"`
	Item      string   `json:"item"`
	Create    bool     `json:"create"`
	Name      string   `json:"name,omitzero"`
	Base      string   `json:"base,omitzero"`
	Members   []Member `json:"members"`
	CreatedAt string   `json:"created_at"`
}

// Save publishes one draft as one revision, or nothing.
//
// A submission already published is answered with its revision and nothing is
// written again. A base that is not the current revision is a Conflict. The
// pending record is written, every member is staged as a new file in the
// item's own managed folder and read back, verify reads each through its
// kind's reader, and only then is the catalog replaced to name the revision.
// Anything that stops the save before that replacement leaves the previous
// revision current.
func (s *Store) Save(draft Draft, verify Verifier, options Options) (Saved, error) {
	now := options.now()
	document, present, err := s.Read()
	if err != nil {
		return Saved{}, err
	}
	if !present {
		if document, err = Fresh(now); err != nil {
			return Saved{}, err
		}
	}
	if !token(draft.Intent) || !digest(draft.Digest) || !token(draft.Kind) {
		return Saved{}, errors.New("a save names its kind, its submission identity and the digest of what it submits")
	}
	if saved, done, err := replayed(document, draft); done || err != nil {
		return saved, err
	}
	record, err := s.readPending(draft.Intent)
	switch {
	case err == nil && (record.Digest != draft.Digest || !sameMembers(record, draft)):
		return Saved{}, ErrIntentReused
	case err == nil:
		// A retry of an interrupted submission resumes it: the same item, the
		// same staged files, the same base.
	case errors.Is(err, fs.ErrNotExist):
		if held, err := s.pendingRecords(); err != nil {
			return Saved{}, err
		} else if len(held) >= MaxPending {
			return Saved{}, ErrTooManyPending
		}
		record, err = s.plan(document, draft, now)
		if err != nil {
			return Saved{}, err
		}
	default:
		return Saved{}, err
	}
	if err := checkBase(document, record); err != nil {
		return Saved{}, err
	}
	if err := options.fault(PointPending); err != nil {
		return Saved{}, err
	}
	if err := s.writePending(record); err != nil && !errors.Is(err, fs.ErrExist) {
		return Saved{}, err
	}
	for i, staged := range draft.Members {
		if err := options.fault(PointMember + staged.Role); err != nil {
			return Saved{}, err
		}
		if err := s.stage(record.Members[i], staged.Data); err != nil {
			return Saved{}, errors.Join(ErrIncomplete, err)
		}
	}
	if err := s.verifyMembers(record, verify); err != nil {
		return Saved{}, errors.Join(ErrIncomplete, err)
	}
	if err := options.fault(PointVerified); err != nil {
		return Saved{}, err
	}
	saved, err := s.publish(record, now)
	var conflict *Conflict
	if errors.As(err, &conflict) {
		// Another writer published first: nothing of this save is published,
		// so what it staged is withdrawn and the draft is the caller's alone.
		if abandonErr := s.abandon(record); abandonErr != nil {
			return Saved{}, errors.Join(err, abandonErr)
		}
		return Saved{}, err
	}
	if err != nil {
		return Saved{}, err
	}
	if err := options.fault(PointPublished); err != nil {
		// Published: the revision is current whatever happens to the pending
		// record, which recovery removes.
		return saved, nil
	}
	s.removePending(record.Intent)
	return saved, nil
}

// sameMembers reports whether a pending record stages exactly the members a
// retried submission carries, role by role and byte for byte.
func sameMembers(record pending, draft Draft) bool {
	if len(record.Members) != len(draft.Members) || record.Kind != draft.Kind {
		return false
	}
	for i, staged := range draft.Members {
		sum := sha256.Sum256(staged.Data)
		if record.Members[i].Role != staged.Role || record.Members[i].SHA256 != hex.EncodeToString(sum[:]) {
			return false
		}
	}
	return true
}

// replayed answers a submission the catalog already records.
func replayed(document Document, draft Draft) (Saved, bool, error) {
	at := slices.IndexFunc(document.Intents, func(intent Intent) bool { return intent.ID == draft.Intent })
	if at < 0 {
		return Saved{}, false, nil
	}
	intent := document.Intents[at]
	if intent.Digest != draft.Digest {
		return Saved{}, true, ErrIntentReused
	}
	index := document.Find(intent.Item)
	if index < 0 {
		return Saved{}, true, ErrNoItem
	}
	return Saved{Item: document.Items[index], Revision: intent.Revision, Replayed: true}, true, nil
}

// plan decides the item, its revision's files and the pending record of a new
// submission.
func (s *Store) plan(document Document, draft Draft, now time.Time) (pending, error) {
	if len(draft.Members) == 0 || len(draft.Members) > MaxMembers {
		return pending{}, errors.New("a save consists of between one and " + strconv.Itoa(MaxMembers) + " files")
	}
	if draft.Name != "" && !ValidName(draft.Name) {
		return pending{}, errors.New("an object name is bounded printable text")
	}
	record := pending{Schema: PendingSchema, Intent: draft.Intent, Digest: draft.Digest, Kind: draft.Kind,
		Name: draft.Name, Base: draft.Base, CreatedAt: Stamp(now)}
	if draft.ItemID == "" {
		id, err := NewID()
		if err != nil {
			return pending{}, err
		}
		record.Item, record.Create = id, true
		if draft.Base != "" {
			return pending{}, &Conflict{}
		}
	} else {
		index := document.Find(draft.ItemID)
		if index < 0 {
			return pending{}, ErrNoItem
		}
		if document.Items[index].Kind != draft.Kind {
			return pending{}, errors.New("a save names the kind of the object it saves")
		}
		record.Item = draft.ItemID
	}
	prefix := MemberPrefix(draft.Kind, record.Item) + intentKey(draft.Intent)[:8] + "-"
	for _, staged := range draft.Members {
		if !token(staged.Role) || artifactpath.EntryName(staged.File) != nil || len(staged.Data) > MaxMemberBytes {
			return pending{}, errors.New("a saved file has a role, one file name and bounded content")
		}
		sum := sha256.Sum256(staged.Data)
		record.Members = append(record.Members, Member{Role: staged.Role, Path: prefix + staged.File, SHA256: hex.EncodeToString(sum[:])})
	}
	return record, nil
}

// checkBase refuses a submission whose base is not the item's current
// revision, or that would create an item the catalog already holds.
func checkBase(document Document, record pending) error {
	index := document.Find(record.Item)
	if record.Create {
		if index >= 0 {
			return &Conflict{Current: document.Items[index].RevisionLabel()}
		}
		return nil
	}
	if index < 0 {
		return ErrNoItem
	}
	if current := document.Items[index].RevisionLabel(); current != record.Base {
		return &Conflict{Current: current}
	}
	return nil
}

// stage writes one member as a new immutable file. A file already there from
// an interrupted attempt of the same submission is accepted only when it holds
// exactly the bytes this submission stages.
func (s *Store) stage(target Member, data []byte) error {
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	err = member.CreateIn(root, target.Path, data)
	if errors.Is(err, fs.ErrExist) {
		held, readErr := member.ReadIn(root, target.Path)
		if readErr == nil && bytes.Equal(held, data) {
			return nil
		}
		return errors.New("a staged file of this submission holds other bytes")
	}
	return err
}

// verifyMembers reads every staged member back, checks it is the bytes the
// pending record names, and hands it to the kind's reader.
func (s *Store) verifyMembers(record pending, verify Verifier) error {
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	files := map[string]string{}
	for _, staged := range record.Members {
		data, err := member.ReadIn(root, staged.Path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != staged.SHA256 {
			return errors.New("a staged file is not the bytes that were staged")
		}
		files[staged.Role] = s.Path(staged)
	}
	if verify != nil {
		return verify(files)
	}
	return nil
}

// Path is where one member lives on disk.
func (s *Store) Path(m Member) string { return filepath.Join(s.root, m.Path) }

// publish replaces the catalog to name the verified revision. It re-reads the
// catalog first, so a publication that raced another is decided against what
// is current now.
func (s *Store) publish(record pending, now time.Time) (Saved, error) {
	unlock, err := s.lock()
	if err != nil {
		return Saved{}, errors.Join(ErrIncomplete, err)
	}
	defer unlock()
	document, present, err := s.Read()
	if err != nil {
		return Saved{}, errors.Join(ErrIncomplete, err)
	}
	if !present {
		if document, err = Fresh(now); err != nil {
			return Saved{}, errors.Join(ErrIncomplete, err)
		}
	}
	if saved, done, err := replayed(document, Draft{Intent: record.Intent, Digest: record.Digest}); done || err != nil {
		return saved, err
	}
	if err := checkBase(document, record); err != nil {
		return Saved{}, err
	}
	stamp := Stamp(now)
	index := document.Find(record.Item)
	if index < 0 {
		document.Items = append(document.Items, Item{Kind: record.Kind, ID: record.Item, Name: record.Name, CreatedAt: stamp})
		index = len(document.Items) - 1
	}
	item := &document.Items[index]
	number := 1
	if current := item.Current(); current != nil {
		number = current.Number + 1
	}
	item.Revisions = append(slices.Clip(item.Revisions), Revision{Number: number, Members: record.Members, PublishedAt: stamp, Intent: record.Intent})
	item.UpdatedAt = stamp
	if record.Name != "" {
		item.Name = record.Name
	}
	document.Intents = append(document.Intents, Intent{ID: record.Intent, Digest: record.Digest, Item: record.Item, Revision: number})
	if excess := len(document.Intents) - MaxIntents; excess > 0 {
		document.Intents = slices.Delete(document.Intents, 0, excess)
	}
	if err := s.Write(document); err != nil {
		return Saved{}, errors.Join(ErrIncomplete, err)
	}
	return Saved{Item: document.Items[index], Revision: number}, nil
}

// Incomplete is one interrupted save recovery could not complete: its
// submission identity, which is the recoverable operation, and the object it
// was for. The object's previous revision is still current.
type Incomplete struct {
	Operation string
	Kind      string
	Item      string
	Name      string
	Reason    string
}

// Recover settles every pending record. A publication the catalog already
// names is finished by removing its record; one whose every member verifies
// against a base that is still current is published; any other is reported
// as incomplete and left, with the previous revision current, until its
// submission is retried or discarded. verifier supplies each kind's reader.
func (s *Store) Recover(verifier func(kind string) Verifier, options Options) ([]Incomplete, error) {
	return s.settle(verifier, options, true)
}

// Inspect reports the saves an interruption left unpublished, as Recover
// would find them, and writes nothing: a save whose every member verifies is
// reported as waiting for the project to be opened for writing.
func (s *Store) Inspect(verifier func(kind string) Verifier) ([]Incomplete, error) {
	return s.settle(verifier, Options{}, false)
}

func (s *Store) settle(verifier func(kind string) Verifier, options Options, write bool) ([]Incomplete, error) {
	records, err := s.pendingRecords()
	if err != nil {
		return nil, err
	}
	incomplete := []Incomplete{}
	for _, record := range records {
		document, _, err := s.Read()
		if err != nil {
			return nil, err
		}
		if _, done, _ := replayed(document, Draft{Intent: record.Intent, Digest: record.Digest}); done {
			if write {
				s.removePending(record.Intent)
			}
			continue
		}
		reason := ""
		switch err := checkBase(document, record); {
		case err != nil:
			reason = "the object changed since this save began"
		case s.verifyMembers(record, verifier(record.Kind)) != nil:
			reason = "not every file of this save was written and verified"
		}
		if reason == "" && !write {
			reason = "every file of this save verified; it is published when the project is next opened for writing"
		}
		if reason == "" {
			if _, err := s.publish(record, options.now()); err == nil {
				s.removePending(record.Intent)
				continue
			}
			reason = "the verified save could not be published"
		}
		incomplete = append(incomplete, Incomplete{Operation: record.Intent, Kind: record.Kind, Item: record.Item, Name: record.Name, Reason: reason})
	}
	return incomplete, nil
}

// Discard drops one incomplete save: its pending record and the files it
// staged, which no published revision names. The current revision is not
// touched.
func (s *Store) Discard(operation string) error {
	record, err := s.readPending(operation)
	if err != nil {
		return errors.New("no incomplete save is held under that operation")
	}
	document, _, err := s.Read()
	if err != nil {
		return err
	}
	if _, done, _ := replayed(document, Draft{Intent: record.Intent, Digest: record.Digest}); done {
		return s.removePending(operation)
	}
	return s.abandon(record)
}

// abandon removes the files an unpublished save staged, which no revision
// names, and then its pending record.
func (s *Store) abandon(record pending) error {
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	for _, staged := range record.Members {
		if err := root.Remove(staged.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.New("the files of the incomplete save could not be removed")
		}
	}
	return s.removePending(record.Intent)
}

func intentKey(intent string) string {
	sum := sha256.Sum256([]byte(intent))
	return hex.EncodeToString(sum[:])
}

func pendingPath(intent string) string {
	return path.Join(Folder, pendingFolder, intentKey(intent)[:32]+".json")
}

func (s *Store) writePending(record pending) error {
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the pending save")
	}
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Join(Folder, pendingFolder), 0o700); err != nil {
		return errors.New("cannot create the pending save folder")
	}
	return pendingRecord.CreateIn(root, pendingPath(record.Intent), append(data, '\n'))
}

// pendingRecord is how a pending record is created: written in full under a
// staging name and linked into place, so another writer listing the pending
// saves never reads one half written. A staged name never ends in ".json",
// which is how a listing tells it from a record.
var pendingRecord = artifactdir.Document{
	MaxBytes:     MaxMemberBytes,
	CreateByLink: true,
	Staging:      artifactdir.StagingTemp(".staging-*"),
	Errors:       member.Errors,
	Refusals:     member.Refusals,
}

func (s *Store) readPending(intent string) (pending, error) {
	root, err := s.open()
	if err != nil {
		return pending{}, err
	}
	defer root.Close()
	data, err := member.ReadIn(root, pendingPath(intent))
	if err != nil {
		if _, statErr := root.Lstat(pendingPath(intent)); errors.Is(statErr, fs.ErrNotExist) {
			return pending{}, fs.ErrNotExist
		}
		return pending{}, err
	}
	return decodePending(data)
}

func decodePending(data []byte) (pending, error) {
	var record pending
	if err := json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil || record.Schema != PendingSchema {
		return pending{}, errors.New("a pending save record cannot be read")
	}
	if !token(record.Intent) || !digest(record.Digest) || !token(record.Kind) || !ValidID(record.Item) {
		return pending{}, errors.New("a pending save record cannot be read")
	}
	if err := validateMembers(Item{Kind: record.Kind, ID: record.Item}, record.Members); err != nil {
		return pending{}, errors.New("a pending save record cannot be read")
	}
	return record, nil
}

// PendingFiles are the project entries unpublished saves staged, which are
// not objects of their own.
func (s *Store) PendingFiles() []string {
	records, err := s.pendingRecords()
	if err != nil {
		return nil
	}
	var files []string
	for _, record := range records {
		for _, member := range record.Members {
			files = append(files, member.Path)
		}
	}
	return files
}

// Pending reports whether an unpublished save is held under intent.
func (s *Store) Pending(intent string) bool {
	_, err := s.readPending(intent)
	return err == nil
}

func (s *Store) removePending(intent string) error {
	root, err := s.open()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(pendingPath(intent)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.New("the pending save record could not be removed")
	}
	return nil
}

// pendingRecords reads every pending record, in name order. A record that
// cannot be read is refused rather than skipped, so no interrupted work is
// silently forgotten.
func (s *Store) pendingRecords() ([]pending, error) {
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	folder, err := root.Open(path.Join(Folder, pendingFolder))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("the pending saves cannot be read")
	}
	// A save is refused once MaxPending are held, so more are there only when
	// something else put them there; the first MaxPending read are reported,
	// and a listing is never refused for them.
	names, err := folder.Readdirnames(MaxPending)
	folder.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("the pending saves cannot be read")
	}
	slices.Sort(names)
	var records []pending
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			// A record another writer is staging now.
			continue
		}
		record := path.Join(Folder, pendingFolder, name)
		data, err := member.ReadIn(root, record)
		if err != nil {
			if _, statErr := root.Lstat(record); errors.Is(statErr, fs.ErrNotExist) {
				// Another writer published or withdrew it since the listing.
				continue
			}
			return nil, errors.New("a pending save record cannot be read")
		}
		decoded, err := decodePending(data)
		if err != nil {
			return nil, err
		}
		records = append(records, decoded)
	}
	return records, nil
}

// lockName is the file one writer holds while it reads, changes and replaces
// the catalog, so two processes never both pass the same revision check.
const lockName = "catalog.lock"

// staleLock is how old a lock may be before it is taken to belong to a writer
// that stopped: a writer holds it only for one read and one replacement.
const staleLock = 30 * time.Second

// lock takes the catalog's writer lock, waiting briefly for another writer,
// and answers the function that releases it. The lock file holds a token
// only its taker knows, so releasing it never removes a lock another writer
// took over; a stale lock is taken over by renaming it aside, which only one
// waiter can do.
func (s *Store) lock() (func(), error) {
	root, err := s.managed()
	if err != nil {
		return nil, err
	}
	token, err := NewID()
	if err != nil {
		root.Close()
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := root.OpenFile(lockName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, writeErr := file.WriteString(token)
			file.Close()
			if writeErr != nil {
				root.Remove(lockName)
				root.Close()
				return nil, errors.New("the catalog cannot be locked for writing")
			}
			return func() {
				if held, err := root.ReadFile(lockName); err == nil && string(held) == token {
					root.Remove(lockName)
				}
				root.Close()
			}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			root.Close()
			return nil, errors.New("the catalog cannot be locked for writing")
		}
		if info, statErr := root.Lstat(lockName); statErr == nil && time.Since(info.ModTime()) > staleLock {
			aside := lockName + "." + token + ".stale"
			if root.Rename(lockName, aside) == nil {
				root.Remove(aside)
			}
			continue
		}
		if time.Now().After(deadline) {
			root.Close()
			return nil, errors.New("another writer holds the catalog")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
