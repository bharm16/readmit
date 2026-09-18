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

	"github.com/bharm16/readmit/internal/artifactpath"
)

const QuotaSchema = "readmit-project-quota/v1"
const QuotaDocumentName = "quota.json"

var ErrQuota = errors.New("project quota exceeded; no document was changed")

// Quota bounds retained regular-file bytes and file count, including recovery
// copies and derived files. External writes are detected on the next check.
type Quota struct {
	Schema   string `json:"schema"`
	MaxBytes int64  `json:"max_bytes"`
	MaxFiles int    `json:"max_files"`
}
type Usage struct {
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
}

func (q Quota) validate() error {
	if q.Schema != QuotaSchema {
		return ErrUnsupportedVersion
	}
	if q.MaxBytes <= 0 || q.MaxFiles <= 0 || q.MaxFiles > 65536 {
		return errors.New("quota requires positive bytes and 1 to 65536 files")
	}
	return nil
}
func ReadQuota(root string) (Quota, bool, error) {
	data, missing, err := readDocument(root, QuotaDocumentName)
	if err != nil || missing {
		return Quota{}, false, err
	}
	var q Quota
	if err := json.Unmarshal(data, &q, json.RejectUnknownMembers(true)); err != nil {
		return q, true, errors.New("invalid project quota document")
	}
	return q, true, q.validate()
}
func usage(root string) (Usage, error) {
	var u Usage
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cannot inspect quota usage")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("quota refuses nonregular project entries")
		}
		u.Files++
		if u.Files > 65536 {
			return errors.New("project inventory exceeds file limit")
		}
		u.Bytes += info.Size()
		return nil
	})
	return u, err
}
func (q Quota) permits(u Usage) error {
	if u.Files > q.MaxFiles || u.Bytes > q.MaxBytes {
		return ErrQuota
	}
	return nil
}

// CheckQuota inspects all files without deleting expired or excessive data.
func CheckQuota(path string) (Usage, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return Usage{}, err
	}
	q, present, err := ReadQuota(root)
	if err != nil {
		return Usage{}, err
	}
	u, err := usage(root)
	if err == nil && present {
		err = q.permits(u)
	}
	return u, err
}
func projected(root, name string, data []byte) (Usage, error) {
	u, err := usage(root)
	if err != nil {
		return u, err
	}
	old, missing, err := readDocument(root, name)
	if err != nil {
		return u, err
	}
	if missing {
		u.Files++
		u.Bytes += int64(len(data))
		return u, nil
	}
	u.Bytes += int64(len(data) - len(old))
	if !bytes.Equal(old, data) {
		sum := sha256.Sum256(old)
		recovery := name + ".recovery-" + hex.EncodeToString(sum[:])
		if _, err := os.Lstat(filepath.Join(root, recovery)); errors.Is(err, fs.ErrNotExist) {
			u.Files++
			u.Bytes += int64(len(old))
		} else if err != nil {
			return u, errors.New("cannot inspect recovery quota")
		}
	}
	return u, nil
}
func enforceQuota(root, name string, data []byte) error {
	q, present, err := ReadQuota(root)
	if err != nil || !present {
		return err
	}
	u, err := projected(root, name, data)
	if err != nil {
		return err
	}
	return q.permits(u)
}

// SetQuota stores a versioned declaration only if the resulting project fits.
func SetQuota(root string, q Quota) error {
	opened, err := Open(root)
	if err != nil {
		return err
	}
	root = opened.Root
	if err := q.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(q, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode quota")
	}
	data = append(data, '\n')
	u, err := projected(root, QuotaDocumentName, data)
	if err != nil {
		return err
	}
	if err := q.permits(u); err != nil {
		return err
	}
	// A quota can be raised even when external writes exceeded the old limit.
	return installWithQuota(root, QuotaDocumentName, data, false)
}
