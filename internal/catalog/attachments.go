package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/strictdoc"
)

// A case's attachments are files a person added to it: a ticket export, a
// screenshot, an interface specification. Each one is copied into the
// project's own mutable area under a name the application generates, and the
// association — which object it belongs to, the name it was added under,
// its type and when — is recorded in readmit-attachments/v1 beside the
// catalog. Nothing here opens, interprets or executes an attachment, and
// removing an association leaves the stored copy where it is.

const (
	// AttachmentsSchema is the attachments document contract.
	AttachmentsSchema = "readmit-attachments/v1"
	// AttachmentsDocumentName is the document in the catalog's folder, and
	// AttachmentsFolder the folder beside it that holds the stored copies.
	AttachmentsDocumentName = "attachments.json"
	AttachmentsFolder       = "attachments"

	// MaxAttachments bounds one project's associations, and
	// MaxAttachmentBytes one stored copy.
	MaxAttachments     = 1024
	MaxAttachmentBytes = 16 << 20

	maxAttachmentsBytes = 1 << 20
	maxTypeBytes        = 16
)

// ErrNoAttachment reports an association the project does not hold.
var ErrNoAttachment = errors.New("the project holds no such attachment")

// ErrTooManyAttachments refuses associations past MaxAttachments.
var ErrTooManyAttachments = errors.New("the project holds as many attachments as this release keeps")

// Attachment is one association: the object it belongs to, the name and type
// it was added under, the stored copy that holds its bytes with their
// SHA-256 and size, and when the application added it.
type Attachment struct {
	ID      string `json:"id"`
	Item    string `json:"item"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	AddedAt string `json:"added_at"`
}

// NewAttachment is one file to attach: its name, its type and its bytes.
type NewAttachment struct {
	Name string
	Type string
	Data []byte
}

type attachmentsDoc struct {
	Schema      string       `json:"schema"`
	Attachments []Attachment `json:"attachments"`
}

var attachmentsReading = strictdoc.Document{
	MaxBytes:    maxAttachmentsBytes,
	Schema:      AttachmentsSchema,
	Invalid:     "invalid attachments document",
	TooLarge:    "attachments document exceeds its size limit",
	MustDeclare: "an attachments document declares its contract version",
	Unsupported: ErrUnsupportedVersion,
}

// attachmentsFile is how the attachments document is replaced, exactly as
// the catalog document is.
var attachmentsFile = artifactdir.Document{
	MaxBytes: maxAttachmentsBytes,
	Staging:  artifactdir.StagingTemp(AttachmentsDocumentName + ".*.incomplete"),
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot write the attachments here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot write the attachments"),
		Install:     errors.New("cannot replace the attachments"),
		Sync:        errors.New("cannot confirm the attachments were retained durably"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("the project holds no attachments"),
		Irregular: errors.New("the attachments must be a regular file"),
		Open:      errors.New("the attachments cannot be opened"),
		Read:      errors.New("the attachments cannot be read"),
	},
}

// storedCopy is how one stored copy is written: created exclusively, never
// replaced.
var storedCopy = artifactdir.Document{
	MaxBytes: MaxAttachmentBytes,
	Errors: artifactdir.DocumentErrors{
		Destination: errors.New("cannot store an attachment here"),
		Create:      artifactdir.FilesystemReport,
		Write:       errors.New("cannot store an attachment"),
		Install:     errors.New("cannot install an attachment"),
		Sync:        errors.New("cannot confirm an attachment was stored durably"),
	},
	Refusals: artifactdir.DocumentRefusals{
		Inspect:   errors.New("a stored attachment is missing"),
		Irregular: errors.New("a stored attachment must be a regular file"),
		Open:      errors.New("a stored attachment cannot be opened"),
		Read:      errors.New("a stored attachment cannot be read"),
	},
}

// Attachments reads every association the project holds, oldest first. A
// project that never attached anything holds none.
func (s *Store) Attachments() ([]Attachment, error) {
	root, err := s.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := attachmentsFile.ReadIn(root, path.Join(Folder, AttachmentsDocumentName))
	if errors.Is(err, fs.ErrNotExist) {
		return []Attachment{}, nil
	}
	if err != nil {
		return nil, err
	}
	var document attachmentsDoc
	if err := attachmentsReading.Decode(data, &document); err != nil {
		return nil, err
	}
	if err := validateAttachments(document.Attachments); err != nil {
		return nil, err
	}
	return document.Attachments, nil
}

// Detached is what a caller deciding whether the project has room leaves
// out: the stored copies no association names any more — removing an
// attachment removes its association and never its copy — and the writer
// lock held while it decides.
type Detached struct {
	Files int
	Bytes int64
}

// Attach stores a copy of each file under a new generated name and records
// every association at once. admit, when set, decides under the writer lock
// — before anything is stored — whether the project has room, given what the
// detached copies already hold. Nothing is recorded unless every copy was
// stored; a copy stored before a later one failed is removed again.
func (s *Store) Attach(item string, files []NewAttachment, now time.Time, admit func(Detached) error) ([]Attachment, error) {
	if !ValidID(item) {
		return nil, ErrNoItem
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	held, err := s.Attachments()
	if err != nil {
		return nil, err
	}
	if len(held)+len(files) > MaxAttachments {
		return nil, ErrTooManyAttachments
	}
	managed, err := s.managed()
	if err != nil {
		return nil, err
	}
	defer managed.Close()
	if err := managed.Mkdir(AttachmentsFolder, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, errors.New("cannot store an attachment in the project")
	}
	if info, err := managed.Lstat(AttachmentsFolder); err != nil || !info.IsDir() {
		return nil, errors.New("the project's attachment storage is not a folder of the project")
	}
	if admit != nil {
		detached, err := detachedCopies(managed, held)
		if err != nil {
			return nil, err
		}
		if err := admit(detached); err != nil {
			return nil, err
		}
	}
	added := []Attachment{}
	abandon := func() {
		for _, attachment := range added {
			managed.Remove(path.Join(AttachmentsFolder, attachment.File))
		}
	}
	for _, file := range files {
		if !ValidName(file.Name) || !attachmentType(file.Type) || len(file.Data) > MaxAttachmentBytes {
			abandon()
			return nil, errors.New("an attachment is named, typed and bounded")
		}
		id, err := NewID()
		if err != nil {
			abandon()
			return nil, err
		}
		stored := id + "." + file.Type
		if err := storedCopy.CreateIn(managed, path.Join(AttachmentsFolder, stored), file.Data); err != nil {
			abandon()
			return nil, errors.New("an attachment could not be stored in the project")
		}
		sum := sha256.Sum256(file.Data)
		added = append(added, Attachment{ID: id, Item: item, Name: file.Name, Type: file.Type, File: stored,
			SHA256: hex.EncodeToString(sum[:]), Size: int64(len(file.Data)), AddedAt: Stamp(now)})
	}
	if err := s.writeAttachments(append(slices.Clip(held), added...)); err != nil {
		abandon()
		return nil, err
	}
	return added, nil
}

// detachedCopies sums the stored copies no held association names.
func detachedCopies(managed *os.Root, held []Attachment) (Detached, error) {
	folder, err := managed.Open(AttachmentsFolder)
	if err != nil {
		return Detached{}, errors.New("the project's attachment storage cannot be read")
	}
	defer folder.Close()
	entries, err := folder.ReadDir(-1)
	if err != nil {
		return Detached{}, errors.New("the project's attachment storage cannot be read")
	}
	named := map[string]bool{}
	for _, attachment := range held {
		named[attachment.File] = true
	}
	var detached Detached
	if info, err := managed.Lstat(lockName); err == nil && info.Mode().IsRegular() {
		detached.Files++
		detached.Bytes += info.Size()
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if named[entry.Name()] || err != nil || !info.Mode().IsRegular() {
			continue
		}
		detached.Files++
		detached.Bytes += info.Size()
	}
	return detached, nil
}

// Detach removes one association of item. Its stored copy stays where it is.
func (s *Store) Detach(item, id string) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	held, err := s.Attachments()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(held, func(attachment Attachment) bool { return attachment.ID == id && attachment.Item == item })
	if index < 0 {
		return ErrNoAttachment
	}
	return s.writeAttachments(slices.Delete(held, index, index+1))
}

// AttachmentPath is where one association's stored copy lives on disk.
func (s *Store) AttachmentPath(attachment Attachment) string {
	return filepath.Join(s.root, Folder, AttachmentsFolder, attachment.File)
}

func (s *Store) writeAttachments(attachments []Attachment) error {
	if err := validateAttachments(attachments); err != nil {
		return err
	}
	data, err := json.Marshal(attachmentsDoc{Schema: AttachmentsSchema, Attachments: attachments}, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the attachments")
	}
	data = append(data, '\n')
	if len(data) > maxAttachmentsBytes {
		return errors.New("the attachments document exceeds its size limit")
	}
	managed, err := s.managed()
	if err != nil {
		return err
	}
	defer managed.Close()
	return attachmentsFile.ReplaceIn(managed, AttachmentsDocumentName, data)
}

func validateAttachments(attachments []Attachment) error {
	if len(attachments) > MaxAttachments {
		return errors.New("a project keeps at most " + strconv.Itoa(MaxAttachments) + " attachments")
	}
	ids := map[string]bool{}
	for _, attachment := range attachments {
		if !ValidID(attachment.ID) || !ValidID(attachment.Item) || !ValidName(attachment.Name) || !attachmentType(attachment.Type) ||
			attachment.File != attachment.ID+"."+attachment.Type || !digest(attachment.SHA256) || attachment.Size < 0 ||
			attachment.Size > MaxAttachmentBytes || !stamp(attachment.AddedAt) || ids[attachment.ID] {
			return errors.New("an attachment names its identity, object, name, type, stored copy and time once")
		}
		ids[attachment.ID] = true
	}
	return nil
}

// attachmentType is a short lowercase word of letters and digits.
func attachmentType(value string) bool {
	if value == "" || len(value) > maxTypeBytes {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// AttachmentType is the type an attachment is recorded under for a file
// extension: its letters and digits in lowercase, or "file".
func AttachmentType(extension string) string {
	lowered := []rune{}
	for _, r := range extension {
		switch {
		case r >= 'A' && r <= 'Z':
			lowered = append(lowered, r+'a'-'A')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			lowered = append(lowered, r)
		case r == '.' && len(lowered) == 0:
		default:
			return "file"
		}
	}
	if value := string(lowered); attachmentType(value) {
		return value
	}
	return "file"
}
