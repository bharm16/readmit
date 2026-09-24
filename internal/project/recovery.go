package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// recoverable names the documents a recovery copy is retained for, in the
// order a listing reports them.
var recoverable = []string{DocumentName, RevisionsDocumentName, QuotaDocumentName}

// errDamagedCopy is a recovery copy whose bytes no longer hash to the digest
// its name records.
var errDamagedCopy = errors.New("recovery copy is damaged")

// RecoveryState is what reading one recovery copy found. Only
// RecoveryReadable is a copy Recover restores.
type RecoveryState string

const (
	// RecoveryReadable is a copy whose bytes are the bytes its name records
	// and which the reader of the document it was retained for accepts.
	RecoveryReadable RecoveryState = "readable"
	// RecoveryDamaged is a copy whose bytes no longer hash to the digest its
	// name records.
	RecoveryDamaged RecoveryState = "damaged"
	// RecoveryUnreadable is a copy that is not a regular file, that exceeds
	// the document bound, or that the document's reader refuses: a document
	// that was already damaged when it was retained, or one of a version this
	// release does not read.
	RecoveryUnreadable RecoveryState = "unreadable"
)

// RecoveryCopy is one retained earlier version of a project document, as its
// file name records it: the document it was retained for and the SHA-256 of
// its bytes. Copies record neither authors nor times, so none is reported.
type RecoveryCopy struct {
	Document string
	Digest   string
	Size     int64
	State    RecoveryState
	// Current is a copy holding exactly the bytes of the document as it
	// stands, so recovering it changes nothing.
	Current bool
}

// RecoveryCopies lists every recovery copy of the project's documents, by
// document and then by digest, with what reading each one found. It writes
// nothing. The document store names and lists the copies; a file whose name is
// not one it gives a copy is not a copy Recover can select, and is not listed.
func RecoveryCopies(path string) ([]RecoveryCopy, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return nil, err
	}
	listed, err := artifactdir.ListPrevious(root)
	if err != nil {
		return nil, errors.New("cannot list the project directory")
	}
	copies := []RecoveryCopy{}
	for _, name := range recoverable {
		current, missing, err := readDocument(root, name)
		if err != nil || missing {
			current = nil
		}
		for _, kept := range listed {
			if kept.Document != name {
				continue
			}
			digest := kept.Digest
			retained := RecoveryCopy{Document: name, Digest: digest, State: RecoveryUnreadable}
			data, err := readRecoveryCopy(root, name, digest)
			switch {
			case errors.Is(err, errDamagedCopy):
				retained.State = RecoveryDamaged
			case err != nil:
			default:
				if _, err := readRecovered(name, data); err == nil {
					retained.State = RecoveryReadable
				}
			}
			retained.Size = int64(len(data))
			retained.Current = data != nil && current != nil && bytes.Equal(data, current)
			copies = append(copies, retained)
		}
	}
	return copies, nil
}

// Recover restores one explicitly selected retained document. The current bytes
// become another recovery copy; no evidence or derived index is rewritten.
func Recover(root, name, digest string) error {
	if !slices.Contains(recoverable, name) {
		return errors.New("recovery supports project, revisions, and quota documents only")
	}
	if !lowercaseDigest(name, digest) {
		return errors.New("recovery requires a lowercase SHA-256 digest")
	}
	physical, err := artifactpath.Directory(root)
	if err != nil {
		return err
	}
	root = physical
	data, err := readRecoveryCopy(root, name, digest)
	if err != nil {
		return err
	}
	quota, err := readRecovered(name, data)
	if err != nil {
		return err
	}
	if name == QuotaDocumentName {
		u, err := projected(root, name, data)
		if err == nil {
			err = quota.permits(u)
		}
		if err != nil {
			return err
		}
	}
	return installWithQuota(root, name, data, name != QuotaDocumentName)
}

// lowercaseDigest reports whether digest names a recovery copy of the document
// name, as the document store names one: a lowercase SHA-256.
func lowercaseDigest(name, digest string) bool {
	document, _, ok := artifactdir.ParsePreviousName(artifactdir.PreviousName(name, digest))
	return ok && document == name
}

// readRecoveryCopy reads one recovery copy and checks its bytes against the
// digest its name records. A damaged copy's bytes are returned with
// errDamagedCopy, so a listing can report their length.
func readRecoveryCopy(root, name, digest string) ([]byte, error) {
	copyName := artifactdir.PreviousName(name, digest)
	if info, err := os.Lstat(filepath.Join(root, copyName)); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("recovery copy must be a regular file")
	}
	data, missing, err := readDocument(root, copyName)
	if err != nil {
		return nil, err
	}
	if missing {
		return nil, errors.New("recovery copy is missing")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return data, errDamagedCopy
	}
	return data, nil
}

// readRecovered reads one copy's bytes with the reader of the document it was
// retained for. A quota copy's declaration is returned, because recovering it
// must also fit the project it declares limits for.
func readRecovered(name string, data []byte) (Quota, error) {
	var q Quota
	switch name {
	case DocumentName:
		_, err := Decode(data)
		return q, err
	case RevisionsDocumentName:
		_, err := DecodeRevisions(data)
		return q, err
	case QuotaDocumentName:
		if json.Unmarshal(data, &q, json.RejectUnknownMembers(true)) != nil {
			return q, errors.New("invalid recovered quota")
		}
		return q, q.validate()
	}
	return q, errors.New("recovery supports project, revisions, and quota documents only")
}
