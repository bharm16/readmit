package observesource

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// EvidenceSchema is the contract the record beside each retained read carries.
// It is written, never migrated: a new member is a new version string.
const EvidenceSchema = "readmit-observation-evidence/v1"

// maxNoteBytes bounds the reason retained beside one read. A note names a
// condition, never a value, a field, a header or a path.
const maxNoteBytes = 200

// stateDomain separates a digest of an observed state from an identity of the
// material that state was read from. The two answer different questions and
// must never collide.
const stateDomain = "readmit-observation-state/v1"

// Evidence is what one attempt to read the source recorded about itself, kept
// beside the original material it read. It names conditions and counts only: no
// message value, no field, no response header, no credential and no path, so
// retaining it can never turn a snapshot into a second copy of the evidence's
// sensitive contents or of readmit's own configuration.
type Evidence struct {
	Schema string `json:"schema"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Note names the condition this attempt hit, in fixed wording.
	Note string `json:"note"`
	// Attempts is how many reads were performed, so a read that answered first
	// time records one. Retries is how many of them were retries this
	// collector judged safe to make, which is what makes a bounded retry a
	// recorded fact rather than an invisible one.
	Attempts int `json:"attempts"`
	Retries  int `json:"retries"`
	// HTTPStatus is the response status a read of an API received, and zero
	// for an export on disk.
	HTTPStatus int `json:"http_status"`
	// StatedAge is how old the state this attempt read was, as the source
	// itself stated it. It is empty when the source stated no age, which is a
	// reading with no single meaning rather than a fresh one.
	StatedAge string `json:"stated_age"`
	Bytes     int    `json:"bytes"`
	Records   int    `json:"records"`
}

// encodeEvidence writes one record deterministically, so two identical reads
// retain identical bytes and the identity over them is comparable.
func encodeEvidence(record Evidence) ([]byte, error) {
	if len(record.Note) > maxNoteBytes {
		record.Note = record.Note[:maxNoteBytes]
	}
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot record what this read observed")
	}
	return append(data, '\n'), nil
}

// snapshot is the new directory one collection retains its original material
// in. Every read gets its own numbered directory holding the bytes exactly as
// they were read and the record of the attempt that read them. Nothing is
// rewritten: original evidence is immutable, and a second collection writes a
// second snapshot beside the first rather than replacing it.
type snapshot struct {
	directory string
	root      *os.Root
}

// openSnapshot reserves and creates the snapshot directory. The destination
// must not exist and is refused inside retained case, run, result, review and
// report evidence, through the one owner of output reservation.
func openSnapshot(path string) (*snapshot, error) {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return nil, errors.New("cannot create the observation snapshot; destination must be new and its parent writable")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return nil, errors.New("cannot open the new observation snapshot directory")
	}
	return &snapshot{directory: destination, root: root}, nil
}

func (s *snapshot) close() {
	if s != nil && s.root != nil {
		s.root.Close()
	}
}

// retain writes one read into its own directory — the original material the
// source answered with, and beside it the record of the attempt that read it —
// and returns the identity of the material alone.
//
// The record is not part of that identity. It states how old the state was and
// how many attempts the read took, which are facts about this run rather than
// about the material, and following ADR-0002 an identity is a hash over
// relative names and contents with no timestamp in it. Two reads of an
// unchanged source therefore name the same material.
func (s *snapshot) retain(index int, material map[string][]byte, record []byte) (string, error) {
	name := "read-" + zeroPadded(index)
	if err := s.root.Mkdir(name, 0700); err != nil {
		return "", errors.New("cannot create the retained read directory")
	}
	if err := s.write(name+"/read.json", record); err != nil {
		return "", err
	}
	for _, file := range sortedNames(material) {
		if err := s.write(name+"/"+file, material[file]); err != nil {
			return "", err
		}
	}
	return evidenceIdentity(material), nil
}

// retainDecision keeps the destination decision an HTTP observation was made
// under beside the material it governed, before that decision is acted on.
func (s *snapshot) retainDecision(decision sendpolicy.Decision) error {
	return sendpolicy.WriteDecision(filepath.Join(s.directory, "decision.json"), decision)
}

func (s *snapshot) write(name string, data []byte) error {
	err := artifactdir.WriteFile(s.root, name, data)
	if errors.Is(err, artifactdir.ErrCreateFile) {
		return errors.New("cannot create a retained observation file")
	}
	if err != nil {
		return errors.New("cannot write a retained observation file")
	}
	return nil
}

// evidenceIdentity hashes a domain prefix and length-delimited relative names
// and contents in bytewise name order, exactly as every other readmit artifact
// identity is taken. It identifies the material an observation read; it does
// not authenticate it. Material nobody kept has no identity, so a read that
// answered with nothing names none.
func evidenceIdentity(material map[string][]byte) string {
	if len(material) == 0 {
		return ""
	}
	return artifactdir.Identity(EvidenceSchema, material)
}

// digestOf is the one hash this package takes: a domain prefix, then every part
// length-delimited in the order it was given. A state digest and an evidence
// identity differ in what they are given, never in how it is hashed.
func digestOf(domain string, parts [][]byte) string {
	sum := sha256.New()
	sum.Write([]byte(domain + "\n"))
	var size [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		sum.Write(size[:])
		sum.Write(part)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// zeroPadded keeps retained reads in the order they were taken when a person
// lists the snapshot, without the order ever being load-bearing.
func zeroPadded(index int) string {
	text := strconv.Itoa(index)
	for len(text) < 4 {
		text = "0" + text
	}
	return text
}
