// Package catalog holds the mutable, application-owned catalog of one project:
// which named objects the project holds, the stable identity the application
// gives each one, the name a person gave it, when it was created, changed and
// last opened, and — for an object the application saves itself — every
// published revision and the immutable files that make it up.
//
// The catalog lives beside evidence, never inside it, in the project's
// supported mutable area: one versioned strict-JSON document,
// readmit-catalog/v1, under the project's .readmit folder. The files of every
// saved revision are new entries of the project itself, named by the
// application, so every reader that opens a project entry — and every
// reference such a file makes to its neighbours — reads them exactly as it
// reads a file a person placed there. It never restates what evidence contains and
// never hashes evidence a second way: an object discovered in the project is
// recorded only by the project entry that holds it, and its own reader stays
// the authority for what it is. A saved revision records the SHA-256 of each
// file it staged, which is how the catalog knows its own outputs are still the
// bytes it published.
//
// A save publishes one logical revision or nothing. Every member is staged as
// a new immutable file, every staged file is read back and verified, and only
// then is the catalog document replaced atomically to name the new revision. A
// pending record written before the first file is what recovery reads after an
// interruption: it either completes a publication whose every member verifies,
// or leaves the previous revision current and reports the work as incomplete.
// See docs/desktop.md and docs/project.md.
package catalog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const (
	// Schema is the catalog document contract.
	Schema = "readmit-catalog/v1"
	// PendingSchema is the contract of one pending publication record.
	PendingSchema = "readmit-catalog-pending/v1"

	// Folder is the project's supported mutable area the catalog lives in,
	// and DocumentName the catalog document inside it.
	Folder       = ".readmit"
	DocumentName = "catalog.json"

	pendingFolder = "pending"

	// MaxItems, MaxRevisions and MaxIntents bound one document. Past the
	// item or revision bound a write is refused rather than truncated; the
	// intent window keeps the most recent submissions, which is how long a
	// repeated submission is recognized as the one already published.
	MaxItems     = 4096
	MaxRevisions = 256
	MaxIntents   = 512
	MaxMembers   = 8
	// MaxPending bounds the interrupted saves a project holds at once.
	MaxPending = 64

	maxDocumentBytes = 8 << 20
	// MaxMemberBytes bounds one staged file.
	MaxMemberBytes = 4 << 20
	idLength       = 24
	maxNameBytes   = 200
	maxTokenBytes  = 128
)

// ErrUnsupportedVersion reports a catalog written under a contract this
// release does not read. It is reported and left exactly as written.
var ErrUnsupportedVersion = errors.New("unsupported catalog document version")

// Document is the whole catalog of one project.
type Document struct {
	Schema  string   `json:"schema"`
	Project Project  `json:"project"`
	Items   []Item   `json:"items"`
	Intents []Intent `json:"intents"`
}

// Project is the stable identity the application gave the project. It is
// separate from the project's display name, which the project document holds,
// and from the folder the project lives in, which can be moved.
type Project struct {
	ID         string `json:"id"`
	RecordedAt string `json:"recorded_at"`
}

// Item is one named object of the project. Kind is the object kind, ID the
// stable application-owned key. Name is the name a person gave it here, empty
// when the object's own declared name or recorded title is its name. Entry is
// the one project entry that backs a discovered object; Revisions are the
// published revisions of an object the application saved, oldest first. Dates
// are the application's own record of its own acts, RFC 3339 in UTC, and are
// empty when the application did not see the act: a discovered object's
// creation is never guessed from a file's modification time.
type Item struct {
	Kind         string     `json:"kind"`
	ID           string     `json:"id"`
	Name         string     `json:"name,omitzero"`
	Entry        string     `json:"entry,omitzero"`
	Revisions    []Revision `json:"revisions"`
	CreatedAt    string     `json:"created_at,omitzero"`
	UpdatedAt    string     `json:"updated_at,omitzero"`
	LastOpenedAt string     `json:"last_opened_at,omitzero"`
}

// Current is the item's current published revision, or nil when it has none.
func (i Item) Current() *Revision {
	if len(i.Revisions) == 0 {
		return nil
	}
	return &i.Revisions[len(i.Revisions)-1]
}

// RevisionLabel is the revision a caller compares a base against: the current
// revision number, or empty when the item has no published revision.
func (i Item) RevisionLabel() string {
	if current := i.Current(); current != nil {
		return strconv.Itoa(current.Number)
	}
	return ""
}

// Revision is one published revision: every member file it consists of, when
// it was published, and the submission that published it.
type Revision struct {
	Number      int      `json:"number"`
	Members     []Member `json:"members"`
	PublishedAt string   `json:"published_at"`
	Intent      string   `json:"intent"`
}

// Member is one immutable file of a revision, named relative to the project
// root with forward slashes, with the SHA-256 of the bytes that were published.
type Member struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Intent is one recent submission: the identity the window allocated on a
// deliberate submit, the digest of what was submitted, and the revision it
// published.
type Intent struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	Item     string `json:"item"`
	Revision int    `json:"revision"`
}

var catalogDocument = strictdoc.Document{
	MaxBytes:    maxDocumentBytes,
	Schema:      Schema,
	Invalid:     "invalid catalog document",
	TooLarge:    "catalog document exceeds its size limit",
	MustDeclare: "a catalog document declares its contract version",
	Unsupported: ErrUnsupportedVersion,
}

// Decode reads a catalog document. Unknown members and unknown versions are
// errors; nothing is migrated or repaired.
func Decode(data []byte) (Document, error) {
	var decoded Document
	if err := catalogDocument.Decode(data, &decoded); err != nil {
		return Document{}, err
	}
	if err := Validate(decoded); err != nil {
		return Document{}, err
	}
	return decoded, nil
}

// Encode writes a validated document deterministically.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	if document.Items == nil {
		document.Items = []Item{}
	}
	if document.Intents == nil {
		document.Intents = []Intent{}
	}
	for i := range document.Items {
		if document.Items[i].Revisions == nil {
			document.Items[i].Revisions = []Revision{}
		}
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode catalog document")
	}
	data = append(data, '\n')
	if len(data) > maxDocumentBytes {
		return nil, errors.New("catalog document exceeds its size limit")
	}
	return data, nil
}

// Validate reports the first reason a document cannot be stored.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if !ValidID(document.Project.ID) {
		return errors.New("catalog project identity must be an application identity")
	}
	if !stamp(document.Project.RecordedAt) {
		return errors.New("catalog recording time must be an RFC 3339 time")
	}
	if len(document.Items) > MaxItems {
		return errors.New("a catalog holds at most " + strconv.Itoa(MaxItems) + " items")
	}
	ids := make(map[string]bool, len(document.Items))
	for _, item := range document.Items {
		if err := validateItem(item); err != nil {
			return err
		}
		if ids[item.ID] {
			return errors.New("two catalog items share an identity")
		}
		ids[item.ID] = true
	}
	if len(document.Intents) > MaxIntents {
		return errors.New("a catalog keeps at most " + strconv.Itoa(MaxIntents) + " submissions")
	}
	seen := make(map[string]bool, len(document.Intents))
	for _, intent := range document.Intents {
		if !token(intent.ID) || !digest(intent.Digest) || !ids[intent.Item] || intent.Revision < 1 || seen[intent.ID] {
			return errors.New("a recorded submission names its identity, digest, item and revision once")
		}
		seen[intent.ID] = true
	}
	return nil
}

func validateItem(item Item) error {
	if !token(item.Kind) || !ValidID(item.ID) {
		return errors.New("a catalog item names its kind and application identity")
	}
	if item.Name != "" && !ValidName(item.Name) {
		return errors.New("a catalog item name is bounded printable text")
	}
	if item.Entry != "" && artifactpath.EntryName(item.Entry) != nil {
		return errors.New("a catalog item is backed by one entry of the project")
	}
	if item.Entry == "" && len(item.Revisions) == 0 {
		return errors.New("a catalog item is backed by a project entry or a published revision")
	}
	for _, when := range []string{item.CreatedAt, item.UpdatedAt, item.LastOpenedAt} {
		if when != "" && !stamp(when) {
			return errors.New("catalog item times are RFC 3339 times")
		}
	}
	if len(item.Revisions) > MaxRevisions {
		return errors.New("a catalog item keeps at most " + strconv.Itoa(MaxRevisions) + " revisions")
	}
	for i, revision := range item.Revisions {
		if revision.Number < 1 || i > 0 && revision.Number <= item.Revisions[i-1].Number {
			return errors.New("catalog revisions are numbered upward from one")
		}
		if !stamp(revision.PublishedAt) || !token(revision.Intent) {
			return errors.New("a catalog revision records when and by which submission it was published")
		}
		if err := validateMembers(item, revision.Members); err != nil {
			return err
		}
	}
	return nil
}

func validateMembers(item Item, members []Member) error {
	if len(members) == 0 || len(members) > MaxMembers {
		return errors.New("a catalog revision consists of between one and " + strconv.Itoa(MaxMembers) + " files")
	}
	roles := map[string]bool{}
	for _, member := range members {
		if !token(member.Role) || roles[member.Role] || !digest(member.SHA256) {
			return errors.New("a catalog revision names each member role once, with its digest")
		}
		roles[member.Role] = true
		if artifactpath.EntryName(member.Path) != nil || !strings.HasPrefix(member.Path, MemberPrefix(item.Kind, item.ID)) {
			return errors.New("a catalog revision's files are entries of the project the application named for the item")
		}
	}
	return nil
}

// MemberPrefix is how every file the application saves for one item begins:
// the item's kind and the start of its identity. The rest of the name is the
// submission and the member, so two saves never name the same file.
func MemberPrefix(kind, id string) string { return kind + "-" + id[:8] + "-" }

// ValidID reports an application identity: 24 lowercase hexadecimal digits.
func ValidID(value string) bool { return lowerHex(value, idLength) }

// ValidName reports a display name: bounded, valid UTF-8, not only
// whitespace, and free of control characters.
func ValidName(value string) bool {
	if value == "" || len(value) > maxNameBytes || !utf8.ValidString(value) {
		return false
	}
	blank := true
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
		if r != ' ' && r != '\t' {
			blank = false
		}
	}
	return !blank
}

// ValidToken reports a submission identity or another internal word: a
// bounded word of letters, digits, '.', '_', '-' and ':'.
func ValidToken(value string) bool { return token(value) }

// token is a bounded word of letters, digits, '.', '_', '-' and ':'.
func token(value string) bool {
	if value == "" || len(value) > maxTokenBytes {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-' || r == ':':
		default:
			return false
		}
	}
	return true
}

func digest(value string) bool { return lowerHex(value, 64) }

// lowerHex reports exactly length lowercase hexadecimal digits.
func lowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func stamp(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

// Stamp is the one way the catalog writes a time.
func Stamp(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }

// NewID mints a new application identity.
func NewID() (string, error) {
	raw := make([]byte, idLength/2)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("cannot mint an application identity")
	}
	return hex.EncodeToString(raw), nil
}

// DiscoveredID is the identity of an object discovered at one project entry
// before the catalog recorded it: a function of the kind and the entry, so a
// project whose catalog cannot be written still lists the same identities on
// every read. Once recorded, an item keeps its identity wherever its entry
// later moves.
func DiscoveredID(kind, entry string) string {
	sum := sha256.Sum256([]byte("readmit-catalog-discovered\x00" + kind + "\x00" + entry))
	return hex.EncodeToString(sum[:])[:idLength]
}

// Find returns the index of the item with id, or -1.
func (d Document) Find(id string) int {
	return slices.IndexFunc(d.Items, func(item Item) bool { return item.ID == id })
}

// ByEntry returns the index of the item of kind backed by entry, or -1.
func (d Document) ByEntry(kind, entry string) int {
	return slices.IndexFunc(d.Items, func(item Item) bool { return item.Kind == kind && item.Entry == entry })
}

// store is how the catalog document is written: replaced whole through a
// staged file with a fresh name, so an interrupted replacement never blocks
// the next one, and the reader sees either the previous catalog or the new
// one.
var store = artifactdir.Document{
	MaxBytes: maxDocumentBytes,
	Staging:  artifactdir.StagingTemp(DocumentName + ".*.incomplete"),
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the catalog here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write the catalog"),
		Install:     errors.New("cannot replace the catalog"),
		Sync:        errors.New("cannot confirm the catalog was retained durably"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("the project holds no catalog"),
		Irregular: errors.New("the catalog must be a regular file"),
		Open:      errors.New("the catalog cannot be opened"),
		Read:      errors.New("the catalog cannot be read"),
	},
}

// member is how one staged revision file and one pending record are written
// and read: created exclusively, never replaced.
var member = artifactdir.Document{
	MaxBytes: MaxMemberBytes,
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write a saved file here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write a saved file"),
		Install:     errors.New("cannot install a saved file"),
		Sync:        errors.New("cannot confirm a saved file was retained durably"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("a saved file is missing"),
		Irregular: errors.New("a saved file must be a regular file"),
		Open:      errors.New("a saved file cannot be opened"),
		Read:      errors.New("a saved file cannot be read"),
	},
}
