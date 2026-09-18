package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// Kind is what a declared location is. It is stated on the command line and
// checked against the filesystem; it is never read from a file name.
type Kind string

const (
	FileContainer    Kind = "file"
	FolderContainer  Kind = "folder"
	ArchiveContainer Kind = "archive"
)

// State is what an import did with one member: imported as records, or excluded
// with the reason recorded, so a container's entries are all accounted for.
type State string

const (
	Included State = "included"
	Excluded State = "excluded"
)

// The two reasons a member is excluded. Both are fixed sentences: an exclusion
// reason never repeats a name, a value, or any byte of the evidence.
const (
	ReasonSuffix    = "name does not end with a declared member suffix"
	ReasonDirectory = "container directory entry holds no bytes of its own"
)

// MaxContainers bounds the locations one import declares, MaxContainerEntries
// bounds the entries one folder or archive may hold, MaxArchiveBytes bounds one
// archive file, and MaxContainerBytes bounds the bytes one container may be
// read for. MaxContainers is the number of sources one case holds, because a
// container that contributes nothing is still a container that was read. A container's members are read even when
// the plan excludes them, so that an excluded entry is recorded with its real
// size and digest rather than with a size the container merely claims; that
// bound is therefore larger than the evidence bound an import may write.
// A container past any of the three is refused rather than imported in part.
const (
	MaxContainers       = bundle.MaxSources
	MaxContainerEntries = 4096
	MaxArchiveBytes     = bundle.MaxEvidenceBytes
	MaxContainerBytes   = 2 * bundle.MaxEvidenceBytes
)

// The four named refusals. Each says that a declaration and the bytes disagree,
// or that the bytes cannot be divided without inventing a boundary. None of
// them is recoverable by guessing: the operator declares the missing fact.
var (
	// ErrDeclaredFraming reports a member whose own framing bytes contradict
	// the framing the plan declares.
	ErrDeclaredFraming = errors.New("the member's framing bytes contradict the declared framing")
	// ErrAmbiguousBatch reports a member that the declared framing cannot
	// divide without choosing a message boundary that was never declared.
	ErrAmbiguousBatch = errors.New("the declared framing cannot divide this member without guessing a message boundary")
	// ErrDeclaredEncoding reports a member whose bytes cannot be the encoding
	// the plan declares. Nothing is transcoded, repaired, or reinterpreted.
	ErrDeclaredEncoding = errors.New("the member's bytes contradict the declared encoding")
	// ErrUnsafeEntry reports a container entry that is not one relative path of
	// regular file bytes: an absolute name, a parent traversal, a symbolic
	// link, a device, or a name a second entry already used.
	ErrUnsafeEntry = errors.New("a container entry must be one relative path of regular file bytes")
)

// Record is one extracted unit of evidence: the byte range of a member that
// became a single case bundle source, and the occurrences that source holds.
type Record struct {
	SourceID    string `json:"source_id"`
	Offset      int    `json:"offset"`
	Size        int    `json:"size"`
	Occurrences int    `json:"occurrences"`
}

// Member is one entry of a container. Size and SHA256 identify the member's
// complete original bytes, whether or not it was imported.
type Member struct {
	Name    string   `json:"name"`
	Size    int      `json:"size"`
	SHA256  string   `json:"sha256"`
	State   State    `json:"state"`
	Reason  string   `json:"reason,omitzero"`
	Records []Record `json:"records"`
}

// Container is one declared location and what it was found to hold. Path is the
// resolved physical location; a folder has no bytes of its own, so its Size is
// zero and its SHA256 is empty.
type Container struct {
	Kind    Kind     `json:"kind"`
	Path    string   `json:"path"`
	Size    int      `json:"size"`
	SHA256  string   `json:"sha256"`
	Members []Member `json:"members"`
}

// Totals are the counts a person reads to see that nothing went missing.
type Totals struct {
	Containers  int `json:"containers"`
	Members     int `json:"members"`
	Excluded    int `json:"excluded"`
	Sources     int `json:"sources"`
	Occurrences int `json:"occurrences"`
}

// Extraction is the complete result of reading the declared containers: what
// would be written, and the inputs that would write it. Nothing is created,
// modified, or removed while producing one.
type Extraction struct {
	Containers  []Container
	Inputs      []bundle.Input
	Totals      Totals
	recordBytes int
}

// Extract reads every declared container under one plan and returns the case
// bundle sources it would produce. Files are read in the order they were
// declared, then folders, then archives, so the source order of an import is a
// property of the command that ran it rather than of a directory listing.
// Nothing is written: the same result answers a preview and drives an import.
// A cancelled context stops before the next member and writes nothing.
func Extract(ctx context.Context, plan Plan, files, folders, archives []string) (*Extraction, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	declared := len(files) + len(folders) + len(archives)
	if declared == 0 {
		return nil, errors.New("an import declares at least one file, folder, or archive")
	}
	// The bound is checked before any container is opened: refusing after
	// walking them would protect nothing it had not already read.
	if declared > MaxContainers {
		return nil, errors.New("an import declares at most " + strconv.Itoa(MaxContainers) + " files, folders, and archives")
	}
	e := &Extraction{}
	for _, location := range files {
		if err := e.add(ctx, plan, FileContainer, location); err != nil {
			return nil, err
		}
	}
	for _, location := range folders {
		if err := e.add(ctx, plan, FolderContainer, location); err != nil {
			return nil, err
		}
	}
	for _, location := range archives {
		if err := e.add(ctx, plan, ArchiveContainer, location); err != nil {
			return nil, err
		}
	}
	e.Totals.Containers = len(e.Containers)
	return e, nil
}

// entry is one member's complete bytes and the physical file they were read
// from. A folder member names its own file; an archive member names the archive,
// because that is the file that exists and that the import actually read.
type entry struct {
	name      string
	path      string
	data      []byte
	directory bool
}

func (e *Extraction) add(ctx context.Context, plan Plan, kind Kind, declared string) error {
	if err := ctx.Err(); err != nil {
		return errors.New("import cancelled before reading a declared container")
	}
	container := Container{Kind: kind, Members: []Member{}}
	var entries []entry
	var err error
	switch kind {
	case FolderContainer:
		container.Path, entries, err = readFolderContainer(declared)
	default:
		// A file and an archive are both one file on disk, read whole so that
		// the bytes the receipt identifies are the bytes that were used.
		limit := bundle.MaxSourceBytes
		if kind == ArchiveContainer {
			limit = MaxArchiveBytes
		}
		var file entry
		container.Path, file, err = readFileContainer(declared, limit)
		if err == nil {
			container.Size, container.SHA256 = len(file.data), digest(file.data)
			entries = []entry{file}
			if kind == ArchiveContainer {
				entries, err = archiveEntries(file)
			}
		}
	}
	if err != nil {
		return err
	}
	index := len(e.Containers) + 1
	for position, member := range entries {
		if err := ctx.Err(); err != nil {
			return errors.New("import cancelled before reading a container member")
		}
		recorded := Member{Name: member.name, Size: len(member.data), SHA256: digest(member.data), State: Included, Records: []Record{}}
		switch {
		case member.directory:
			recorded.State, recorded.Reason = Excluded, ReasonDirectory
		case kind != FileContainer && !selected(plan, member.name):
			recorded.State, recorded.Reason = Excluded, ReasonSuffix
		}
		if recorded.State == Excluded {
			e.Totals.Excluded++
		} else if recorded.Records, err = e.importMember(plan, member); err != nil {
			return fmt.Errorf("container %d member %d: %w", index, position+1, err)
		}
		container.Members = append(container.Members, recorded)
		e.Totals.Members++
	}
	e.Containers = append(e.Containers, container)
	return nil
}

// importMember divides one member by its declared framing and appends each
// record as its own case bundle source, in order. Concatenating a member's
// records reproduces the member byte for byte, so the split retains everything
// the member held, including bytes no message boundary claimed.
func (e *Extraction) importMember(plan Plan, member entry) ([]Record, error) {
	if err := declaredEncoding(plan, member.data); err != nil {
		return nil, err
	}
	starts, err := recordStarts(plan, member.data)
	if err != nil {
		return nil, err
	}
	format := plan.Framing.storedFormat()
	records := make([]Record, 0, len(starts))
	for i, start := range starts {
		end := len(member.data)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		data := bytes.Clone(member.data[start:end])
		if len(data) > bundle.MaxSourceBytes {
			return nil, errors.New("a record exceeds the 16 MiB source limit")
		}
		e.recordBytes += len(data)
		if e.recordBytes > bundle.MaxEvidenceBytes {
			return nil, errors.New("an import exceeds the 64 MiB evidence limit")
		}
		count := occurrences(data, format)
		if count > bundle.MaxEvents {
			return nil, errors.New("a record holds more occurrences than one import writes")
		}
		observations := make(map[int]bundle.Observation, count)
		for sequence := 1; sequence <= count; sequence++ {
			observations[sequence] = bundle.Observation{Direction: plan.Direction}
		}
		e.Inputs = append(e.Inputs, bundle.Input{Path: member.path, Data: data, Options: hl7.Options{Format: format, Terminator: plan.Terminator}, Observations: observations})
		if len(e.Inputs) > bundle.MaxSources {
			return nil, errors.New("an import writes at most 128 case bundle sources")
		}
		e.Totals.Sources++
		e.Totals.Occurrences += count
		if e.Totals.Occurrences > bundle.MaxEvents {
			return nil, errors.New("an import writes at most 10000 occurrences")
		}
		records = append(records, Record{SourceID: fmt.Sprintf("s%04d", len(e.Inputs)), Offset: start, Size: len(data), Occurrences: count})
	}
	return records, nil
}

// occurrences counts the occurrences a source holds under the case bundle's own
// framing policy, so a preview reports exactly what an import would store. It
// stops one past the case bundle's own limit: a record that reaches the cap is
// refused, so no caller allocates per occurrence for evidence it cannot write.
func occurrences(data []byte, format hl7.Format) int {
	count := 0
	for start := 0; start < len(data) || count == 0; {
		end, _ := bundle.NextOccurrence(data, start, format)
		count++
		start = end
		if count > bundle.MaxEvents {
			break
		}
	}
	return count
}

// selected reports whether a folder or archive entry matches the plan's
// declared member suffixes. An empty list makes every entry a member.
func selected(plan Plan, name string) bool {
	if len(plan.Members) == 0 {
		return true
	}
	lowered := strings.ToLower(name)
	for _, suffix := range plan.Members {
		if strings.HasSuffix(lowered, suffix) {
			return true
		}
	}
	return false
}

// declaredEncoding refuses a member whose bytes cannot be what the plan states.
// ISO-8859-1 admits every byte sequence and an unknown encoding makes no claim,
// so neither is checkable; both are recorded exactly as declared.
func declaredEncoding(plan Plan, data []byte) error {
	switch plan.Encoding {
	case UTF8:
		if !utf8.Valid(data) {
			return ErrDeclaredEncoding
		}
	case USASCII:
		for _, b := range data {
			if b > 0x7f {
				return ErrDeclaredEncoding
			}
		}
	}
	return nil
}

// recordStarts returns the offset of every record the declared framing divides
// a member into. It never chooses a boundary the plan did not declare.
func recordStarts(plan Plan, data []byte) ([]int, error) {
	framed := len(data) > 0 && data[0] == 0x0b
	if plan.Framing == MLLPFraming {
		if !framed {
			return nil, ErrDeclaredFraming
		}
		return []int{0}, nil
	}
	if framed {
		return nil, ErrDeclaredFraming
	}
	headers := headerStarts(data, bundle.MaxSources+1)
	if plan.Framing == RawFraming {
		// A raw member is one message. More than one header would have to be
		// split at a boundary this plan never declared.
		if len(headers) > 1 {
			return nil, ErrAmbiguousBatch
		}
		return []int{0}, nil
	}
	if plan.BatchBoundary == HL7Batch && !segmentAt(data, 0, []string{"FHS", "BHS"}) {
		return nil, ErrDeclaredFraming
	}
	separator := terminator(plan.Terminator)
	boundaries := []string{"MSH"}
	if plan.BatchBoundary == HL7Batch {
		boundaries = append(boundaries, batchEnvelope...)
	}
	starts := segmentStarts(data, separator, boundaries, bundle.MaxSources+1)
	if len(starts) > bundle.MaxSources {
		return nil, errors.New("a member divides into more records than one import writes")
	}
	// The declared terminator must reach every message header the member
	// plainly holds. When it reaches fewer, the remaining headers could only be
	// separated by inventing a terminator the plan did not declare. A member
	// holding no message header at all is not ambiguous: there is no boundary
	// to guess, so it stays one record and the case quarantines it.
	if len(headers) != countSegments(data, starts, header) {
		return nil, ErrAmbiguousBatch
	}
	if len(starts) == 0 || starts[0] != 0 {
		starts = append([]int{0}, starts...)
	}
	return starts, nil
}

// batchEnvelope are the HL7 batch header and trailer segment identifiers. They
// carry no message, so each is retained as its own quarantined record rather
// than merged into the message beside it.
var batchEnvelope = []string{"FHS", "BHS", "BTS", "FTS"}

// header is the message header identifier, kept as its own list so the two
// scans below share one definition of what starts a message.
var header = []string{"MSH"}

// headerStarts finds every message header the member plainly holds, using any
// line ending rather than the declared one. It answers "does this file hold
// more than one message", which must not depend on the declaration under test.
// It stops at limit, because every caller only compares bounded counts.
func headerStarts(data []byte, limit int) []int {
	var starts []int
	for at := 0; at < len(data) && len(starts) < limit; {
		next := bytes.Index(data[at:], []byte("MSH"))
		if next < 0 {
			break
		}
		at += next
		if (at == 0 || data[at-1] == '\r' || data[at-1] == '\n') && segmentAt(data, at, header) {
			starts = append(starts, at)
		}
		at++
	}
	return starts
}

// segmentStarts finds the declared boundaries: the start of the member, and
// every position immediately after a complete declared terminator.
func segmentStarts(data []byte, separator []byte, ids []string, limit int) []int {
	var starts []int
	for at := 0; at < len(data) && len(starts) < limit; {
		if segmentAt(data, at, ids) {
			starts = append(starts, at)
		}
		next := bytes.Index(data[at:], separator)
		if next < 0 {
			break
		}
		at += next + len(separator)
	}
	return starts
}

func countSegments(data []byte, starts []int, ids []string) int {
	count := 0
	for _, at := range starts {
		if segmentAt(data, at, ids) {
			count++
		}
	}
	return count
}

// segmentAt reports whether one of the named three-character segment
// identifiers begins at this offset and is followed by a byte that is not a
// line ending. Which byte separates fields is the message's own declaration,
// so nothing here requires a particular separator.
func segmentAt(data []byte, at int, ids []string) bool {
	if at+3 >= len(data) || data[at+3] == '\r' || data[at+3] == '\n' {
		return false
	}
	return slices.Contains(ids, string(data[at:at+3]))
}

func terminator(declared hl7.Terminator) []byte {
	switch declared {
	case hl7.LF:
		return []byte{'\n'}
	case hl7.CRLF:
		return []byte{'\r', '\n'}
	default:
		return []byte{'\r'}
	}
}

func readFileContainer(declared string, limit int) (string, entry, error) {
	resolved, err := artifactpath.Resolve(declared)
	if err != nil {
		return "", entry{}, errors.New("cannot resolve a declared import location")
	}
	data, err := readRegularFile(resolved, limit)
	if err != nil {
		return "", entry{}, err
	}
	return resolved, entry{name: filepath.Base(resolved), path: resolved, data: data}, nil
}

func readFolderContainer(declared string) (string, []entry, error) {
	root, err := artifactpath.Directory(declared)
	if err != nil {
		return "", nil, errors.New("a declared import folder must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return "", nil, errors.New("cannot open a declared import folder")
	}
	defer opened.Close()
	var entries []entry
	read := 0
	err = fs.WalkDir(opened.FS(), ".", func(name string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read a declared import folder")
		}
		if item.IsDir() {
			return nil
		}
		if item.Type() != 0 {
			return ErrUnsafeEntry
		}
		if len(entries) >= MaxContainerEntries {
			return errors.New("a container holds more entries than one import reads")
		}
		file, err := opened.Open(name)
		if err != nil {
			return errors.New("cannot read a folder member")
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			file.Close()
			return ErrUnsafeEntry
		}
		data, readErr := io.ReadAll(io.LimitReader(file, bundle.MaxSourceBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return errors.New("cannot read a folder member")
		}
		if len(data) > bundle.MaxSourceBytes {
			return errors.New("a folder member exceeds the 16 MiB source limit")
		}
		read += len(data)
		if read > MaxContainerBytes {
			return errors.New("a container holds more bytes than one import reads")
		}
		entries = append(entries, entry{name: name, path: filepath.Join(root, filepath.FromSlash(name)), data: data})
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return root, entries, nil
}

// archiveEntries reads a ZIP container. Entry names are refused before anything
// is read from them: an absolute name, a parent traversal, a name that is not
// one relative path, and a name a previous entry already used are all refused,
// as is any entry that is not directory or regular file bytes. Nothing is
// written to the filesystem, so a refusal protects the recorded evidence rather
// than a destination. Entries are read in name order, so an import of the same
// archive produces the same sources however the archive was written.
func archiveEntries(archive entry) ([]entry, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive.data), int64(len(archive.data)))
	if err != nil {
		return nil, errors.New("cannot read a declared import archive")
	}
	if len(reader.File) > MaxContainerEntries {
		return nil, errors.New("a container holds more entries than one import reads")
	}
	items := slices.Clone(reader.File)
	slices.SortFunc(items, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
	entries := make([]entry, 0, len(items))
	used := make(map[string]bool, len(items))
	total := 0
	for _, item := range items {
		name, err := archiveName(item.Name)
		if err != nil {
			return nil, err
		}
		if used[name] {
			return nil, ErrUnsafeEntry
		}
		used[name] = true
		if item.FileInfo().IsDir() {
			entries = append(entries, entry{name: name, path: archive.path, directory: true})
			continue
		}
		if item.Mode()&fs.ModeType != 0 {
			return nil, ErrUnsafeEntry
		}
		data, err := readArchiveMember(item, min(bundle.MaxSourceBytes, MaxContainerBytes-total))
		if err != nil {
			return nil, err
		}
		total += len(data)
		entries = append(entries, entry{name: name, path: archive.path, data: data})
	}
	return entries, nil
}

// archiveName refuses any entry name that is not one relative path of local
// elements. The element rule belongs to artifactpath, so an archive and a
// project directory hold a recorded name to the same standard.
func archiveName(name string) (string, error) {
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || !utf8.ValidString(trimmed) || len(trimmed) > 512 {
		return "", ErrUnsafeEntry
	}
	// ZIP names separate elements with '/'. A backslash is an element
	// character on one platform and a separator on another, so a name that
	// carries one is refused rather than read differently on each.
	if strings.ContainsRune(trimmed, '\\') || path.IsAbs(trimmed) || filepath.IsAbs(trimmed) {
		return "", ErrUnsafeEntry
	}
	for _, element := range strings.Split(trimmed, "/") {
		if err := artifactpath.EntryName(element); err != nil {
			return "", ErrUnsafeEntry
		}
	}
	return trimmed, nil
}

func readArchiveMember(item *zip.File, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("a container holds more bytes than one import reads")
	}
	file, err := item.Open()
	if err != nil {
		return nil, errors.New("cannot read an archive member")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("cannot read an archive member")
	}
	if len(data) > limit {
		return nil, errors.New("an archive member exceeds the import size limits")
	}
	return data, nil
}

func readRegularFile(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open a declared import location")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("a declared import file must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, errors.New("cannot read a declared import location")
	}
	if len(data) > limit {
		return nil, errors.New("a declared import location exceeds its size limit")
	}
	return data, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
