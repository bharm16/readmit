package hub

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

type backupEntry struct {
	Digest     string `json:"sha256"`
	Size       int64  `json:"size"`
	RetainedAt string `json:"retained_at"`
}
type backupManifest struct {
	Schema          string        `json:"schema"`
	MetadataVersion int           `json:"metadata_version"`
	Artifacts       []backupEntry `json:"artifacts"`
}

// Backup creates a new, complete directory. The manifest is its completion marker;
// it is published only after every referenced byte is verified and synchronized.
func (s *Store) Backup(ctx context.Context, destination string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Ready(ctx); err != nil {
		return err
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return errors.New("absolute clean backup path required")
	}
	rel, err := filepath.Rel(s.config.Root, destination)
	if err != nil || rel == "." || filepath.IsLocal(rel) {
		return errors.New("backup must be outside storage")
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return errors.New("new backup directory required")
	}
	out, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer out.Close()
	m := backupManifest{Schema: "readmit-hub-backup/v1", MetadataVersion: schemaVersion, Artifacts: []backupEntry{}}
	rows, err := s.db.QueryContext(ctx, "SELECT digest,size,retained_at FROM readmit_hub_artifacts ORDER BY digest")
	if err != nil {
		return errors.New("metadata unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var e backupEntry
		var at time.Time
		if err = rows.Scan(&e.Digest, &e.Size, &at); err != nil {
			return err
		}
		e.RetainedAt = at.UTC().Format(time.RFC3339Nano)
		if len(m.Artifacts) >= 65536 {
			return ErrLimit
		}
		data, err := s.Get(ctx, e.Digest)
		if err != nil {
			return err
		}
		if err = writeNew(out, e.Digest, data); err != nil {
			return err
		}
		m.Artifacts = append(m.Artifacts, e)
	}
	if err = rows.Err(); err != nil {
		return errors.New("metadata backup interrupted")
	}
	data, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return ErrLimit
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = writeNew(out, "manifest.json", data); err != nil {
		return err
	}
	return syncRoot(out)
}

func writeNew(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func syncRoot(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func readBackup(root *os.Root) (backupManifest, error) {
	var m backupManifest
	info, err := root.Lstat("manifest.json")
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return m, errors.New("backup incomplete")
	}
	f, err := root.Open("manifest.json")
	if err != nil {
		return m, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return m, ErrLimit
	}
	var presence struct {
		Schema    *string           `json:"schema"`
		Version   *int              `json:"metadata_version"`
		Artifacts *[]jsontext.Value `json:"artifacts"`
	}
	if err = json.Unmarshal(data, &presence, json.RejectUnknownMembers(true)); err != nil || presence.Schema == nil || presence.Version == nil || presence.Artifacts == nil {
		return m, errors.New("invalid backup manifest")
	}
	for _, raw := range *presence.Artifacts {
		var e struct {
			Digest *string `json:"sha256"`
			Size   *int64  `json:"size"`
			At     *string `json:"retained_at"`
		}
		if err = json.Unmarshal(raw, &e, json.RejectUnknownMembers(true)); err != nil || e.Digest == nil || e.Size == nil || e.At == nil {
			return m, errors.New("incomplete backup entry")
		}
	}
	if err = json.Unmarshal(data, &m, json.RejectUnknownMembers(true)); err != nil {
		return m, errors.New("invalid backup manifest")
	}
	if m.Schema != "readmit-hub-backup/v1" || m.MetadataVersion != schemaVersion || len(m.Artifacts) > 65536 {
		return m, errors.New("unsupported backup")
	}
	previous := ""
	for _, e := range m.Artifacts {
		if !validDigest(e.Digest) || e.Digest <= previous || e.Size < 0 || e.Size > MaxArtifactBytes {
			return m, errors.New("invalid backup entry")
		}
		previous = e.Digest
		if _, err = time.Parse(time.RFC3339Nano, e.RetainedAt); err != nil {
			return m, errors.New("invalid backup timestamp")
		}
	}
	return m, nil
}

// Restore requires an empty catalogue. Database insertion is one transaction;
// interruption leaves at most verified unreferenced files, safe to retry.
func (s *Store) Restore(ctx context.Context, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Ready(ctx); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup directory unavailable")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	m, err := readBackup(root)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range m.Artifacts {
		total += e.Size
		if total > s.config.MaxBytes {
			return ErrLimit
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM readmit_hub_artifacts").Scan(&count); err != nil || count != 0 {
		return errors.New("restore requires an empty catalogue")
	}
	for _, e := range m.Artifacts {
		if err = ctx.Err(); err != nil {
			return err
		}
		// Reuse the same bounded byte and identity validation as online reads.
		reader := &Store{root: root}
		if err = reader.verify(e.Digest, e.Size); err != nil {
			return err
		}
		f, err := root.Open(e.Digest)
		if err != nil {
			return err
		}
		name, n, err := s.stage(ctx, e.Digest, f)
		f.Close()
		if err != nil {
			return err
		}
		if n != e.Size {
			s.root.Remove(name)
			return ErrIntegrity
		}
		err = s.publish(name, e.Digest, n)
		s.root.Remove(name)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO readmit_hub_artifacts(digest,size,retained_at) VALUES($1,$2,$3)", e.Digest, e.Size, e.RetainedAt); err != nil {
			return errors.New("metadata restore failed")
		}
	}
	if err = syncRoot(s.root); err != nil {
		return err
	}
	return tx.Commit()
}
