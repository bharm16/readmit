package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// retainPrevious preserves exact previous document bytes before replacement.
// Copies are content-addressed and never overwritten, including damaged copies.
func retainPrevious(root, name string, data []byte) error {
	old, missing, err := readDocument(root, name)
	if err != nil || missing || bytes.Equal(old, data) {
		return err
	}
	sum := sha256.Sum256(old)
	recovery := name + ".recovery-" + hex.EncodeToString(sum[:])
	if info, err := os.Lstat(filepath.Join(root, recovery)); err == nil && !info.Mode().IsRegular() {
		return errors.New("recovery copy must be a regular file")
	}
	retained, absent, err := readDocument(root, recovery)
	if err != nil {
		return err
	}
	if !absent {
		if !bytes.Equal(retained, old) {
			return errors.New("recovery copy is damaged; current document was not changed")
		}
		return nil
	}
	destination, err := artifactpath.Destination(filepath.Join(root, recovery))
	if err != nil {
		return errors.New("cannot retain recovery copy")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot retain recovery copy")
	}
	_, writeErr := f.Write(old)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("recovery copy incomplete; current document was not changed")
	}
	return nil
}

// Recover restores one explicitly selected retained document. The current bytes
// become another recovery copy; no evidence or derived index is rewritten.
func Recover(root, name, digest string) error {
	if name != DocumentName && name != RevisionsDocumentName && name != QuotaDocumentName {
		return errors.New("recovery supports project, revisions, and quota documents only")
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest {
		return errors.New("recovery requires a lowercase SHA-256 digest")
	}
	physical, err := artifactpath.Directory(root)
	if err != nil {
		return err
	}
	root = physical
	if info, err := os.Lstat(filepath.Join(root, name+".recovery-"+digest)); err != nil || !info.Mode().IsRegular() {
		return errors.New("recovery copy must be a regular file")
	}
	data, missing, err := readDocument(root, name+".recovery-"+digest)
	if err != nil {
		return err
	}
	if missing {
		return errors.New("recovery copy is missing")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return errors.New("recovery copy is damaged")
	}
	switch name {
	case DocumentName:
		_, err = Decode(data)
	case RevisionsDocumentName:
		_, err = DecodeRevisions(data)
	case QuotaDocumentName:
		var q Quota
		if json.Unmarshal(data, &q, json.RejectUnknownMembers(true)) != nil {
			return errors.New("invalid recovered quota")
		}
		if err = q.validate(); err == nil {
			var u Usage
			u, err = projected(root, name, data)
			if err == nil {
				err = q.permits(u)
			}
		}
	}
	if err != nil {
		return err
	}
	return installWithQuota(root, name, data, name != QuotaDocumentName)
}
