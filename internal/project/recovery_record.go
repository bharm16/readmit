package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// A recovery copy's name records the document it was kept for and the digest
// of its bytes, and nothing else; a file's time is never read as when it was
// kept. So the project records it: each time a replacement keeps a new copy,
// the record gains when that was and why — a save, or the recovery of an
// earlier copy — in a versioned document of its own beside the copies. A copy
// kept before the record existed, or whose record could not be written, is
// listed without either.

const (
	// RecoveryRecordSchema is the contract of the record of recovery copies.
	// A new member means a new version string and a reader for both.
	RecoveryRecordSchema = "readmit-recovery-copies/v1"
	// RecoveryRecordName is the record's name in the project directory.
	RecoveryRecordName = "recovery-copies.json"
	// MaxRecoveryRecords bounds the record; the oldest entry is forgotten
	// first. Forgetting an entry deletes no copy.
	MaxRecoveryRecords = 1024

	maxRecordBytes = 1 << 20
)

// RecoveryReason is why a replacement kept the bytes it replaced.
type RecoveryReason string

const (
	// RecoverySaved: a save replaced the document.
	RecoverySaved RecoveryReason = "saved"
	// RecoveryRecovered: recovering an earlier copy replaced the document.
	RecoveryRecovered RecoveryReason = "recovered"
)

type recoveryEntry struct {
	Document string         `json:"document"`
	Digest   string         `json:"digest"`
	KeptAt   string         `json:"kept_at"`
	Reason   RecoveryReason `json:"reason"`
}

type recoveryRecord struct {
	Schema string          `json:"schema"`
	Copies []recoveryEntry `json:"copies"`
}

// recordFile is how the record is written and read: it keeps no previous
// copy of itself, and a replacement a crash interrupted refuses no later one.
var recordFile = artifactdir.Document{
	MaxBytes: maxRecordBytes,
	Staging:  artifactdir.StagingTemp(RecoveryRecordName + ".*.incomplete"),
}

// readRecoveryRecord reads the record: an empty one when there is none, and
// false when one is there that this release cannot read. A record is what
// the copies' listing adds to their names, never what a replacement or a
// recovery depends on.
func readRecoveryRecord(root string) (recoveryRecord, bool) {
	empty := recoveryRecord{Schema: RecoveryRecordSchema, Copies: []recoveryEntry{}}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return empty, false
	}
	defer opened.Close()
	data, err := recordFile.ReadIn(opened, RecoveryRecordName)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, true
	}
	if err != nil {
		return empty, false
	}
	var record recoveryRecord
	if json.Unmarshal(data, &record, json.RejectUnknownMembers(true)) != nil || record.Schema != RecoveryRecordSchema ||
		len(record.Copies) > MaxRecoveryRecords {
		return empty, false
	}
	for _, entry := range record.Copies {
		if _, err := time.Parse(time.RFC3339, entry.KeptAt); err != nil || !lowercaseDigest(entry.Document, entry.Digest) ||
			entry.Reason != RecoverySaved && entry.Reason != RecoveryRecovered {
			return empty, false
		}
	}
	if record.Copies == nil {
		record.Copies = []recoveryEntry{}
	}
	return record, true
}

// recordedCopy is the record once replacing name with data has kept a new
// recovery copy, for reason, at: nil when the replacement keeps none — there
// is no document yet, it holds these bytes already, or its copy was kept
// earlier and recorded then — and when a record this release cannot read is
// there, which is left as it is.
func recordedCopy(root, name string, data []byte, reason RecoveryReason, at time.Time) ([]byte, error) {
	current, missing, err := readDocument(root, name)
	if err != nil || missing || bytes.Equal(current, data) {
		return nil, nil
	}
	sum := sha256.Sum256(current)
	digest := hex.EncodeToString(sum[:])
	if _, err := os.Lstat(filepath.Join(root, artifactdir.PreviousName(name, digest))); !errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	record, readable := readRecoveryRecord(root)
	if !readable {
		return nil, nil
	}
	record.Copies = slices.DeleteFunc(record.Copies, func(entry recoveryEntry) bool {
		return entry.Document == name && entry.Digest == digest
	})
	record.Copies = append(record.Copies, recoveryEntry{Document: name, Digest: digest, KeptAt: at.UTC().Format(time.RFC3339), Reason: reason})
	if len(record.Copies) > MaxRecoveryRecords {
		record.Copies = record.Copies[len(record.Copies)-MaxRecoveryRecords:]
	}
	encoded, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the record of recovery copies")
	}
	return append(encoded, '\n'), nil
}

// recordGrowth is what recording the copy replacing name with data keeps
// adds to the project: the record's file when it is new, and the bytes it
// grows by.
func recordGrowth(root, name string, data []byte, reason RecoveryReason) (Usage, error) {
	record, err := recordedCopy(root, name, data, reason, time.Now())
	if err != nil || record == nil {
		return Usage{}, err
	}
	info, err := os.Lstat(filepath.Join(root, RecoveryRecordName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Usage{Files: 1, Bytes: int64(len(record))}, nil
	case err != nil:
		return Usage{}, errors.New("cannot inspect recovery quota")
	}
	return Usage{Bytes: int64(len(record)) - info.Size()}, nil
}

// keptAt is when and why each recorded copy was kept, by document and digest.
func keptAt(root string) map[[2]string]recoveryEntry {
	kept := map[[2]string]recoveryEntry{}
	record, _ := readRecoveryRecord(root)
	for _, entry := range record.Copies {
		kept[[2]string{entry.Document, entry.Digest}] = entry
	}
	return kept
}
