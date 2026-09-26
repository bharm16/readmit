package hub

import (
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/hubprotocol"
)

type backupEntry struct {
	Digest     string `json:"sha256"`
	Size       int64  `json:"size"`
	RetainedAt string `json:"retained_at"`
}
type projectLink struct {
	Project string `json:"project"`
	Digest  string `json:"sha256"`
}
type backupManifest struct {
	Schema          string           `json:"schema"`
	MetadataVersion int              `json:"metadata_version"`
	Artifacts       []backupEntry    `json:"artifacts"`
	Projects        []projectLink    `json:"projects"`
	Reviews         []ReviewEvent    `json:"reviews,omitzero"`
	Lifecycle       []LifecycleEvent `json:"lifecycle,omitzero"`
	Team            *bool            `json:"team_enabled,omitzero"`
}

// Backup creates a new, complete directory. The manifest is its completion marker;
// it is published only after every referenced byte is verified and synchronized.
func (s *Store) Backup(ctx context.Context, destination string) error {
	// The artifact backup contract cannot silently omit scheduler claims: restoring
	// without them could repeat uncertain work. Use a stopped deployment snapshot.
	if _, err := s.root.Lstat("scheduler"); !os.IsNotExist(err) {
		return ErrSchedule
	}
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
	m := backupManifest{Schema: "readmit-hub-backup/v5", MetadataVersion: schemaVersion, Artifacts: []backupEntry{}}
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
	team, err := s.teamEnabled(ctx)
	if err != nil {
		return err
	}
	m.Team = &team
	m.Projects = []projectLink{}
	links, err := s.db.QueryContext(ctx, `SELECT project,digest FROM readmit_hub_project_artifacts ORDER BY project COLLATE "C",digest COLLATE "C"`)
	if err != nil {
		return err
	}
	for links.Next() {
		var link projectLink
		if err = links.Scan(&link.Project, &link.Digest); err != nil {
			links.Close()
			return err
		}
		m.Projects = append(m.Projects, link)
		if len(m.Projects) > 65536 {
			links.Close()
			return ErrLimit
		}
	}
	err = links.Err()
	links.Close()
	if err != nil {
		return err
	}
	m.Reviews, err = reviewLog.readAll(ctx, s.db)
	if err != nil {
		return err
	}
	m.Lifecycle, err = lifecycleLog.readAll(ctx, s.db)
	if err != nil {
		return err
	}
	data, err := encodeBackupManifest(m)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = writeNew(out, "manifest.json", data); err != nil {
		return err
	}
	return syncRoot(out)
}

// The v2 bound accommodates both bounded catalogues at their maximum field
// widths. Keep encoding's final bound paired with the reader's bound.
func encodeBackupManifest(m backupManifest) ([]byte, error) {
	data, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	limit := 32 << 20
	if m.Schema == "readmit-hub-backup/v3" {
		limit = 64 << 20
	}
	if m.Schema == "readmit-hub-backup/v4" || m.Schema == "readmit-hub-backup/v5" {
		limit = 128 << 20
	}
	if len(data) > limit {
		return nil, ErrLimit
	}
	return data, nil
}

// writeNew is the one durable file write of a backup or restore: exclusive
// create, a short-write check, and a sync, through the shared artifact
// discipline. A refused or partial write is never reported as complete.
func writeNew(root *os.Root, name string, data []byte) error {
	return artifactdir.WriteFile(root, name, data)
}
func syncRoot(root *os.Root) error {
	return artifactdir.SyncDirectory(root, ".")
}

// backupManifestFile is how a backup's manifest is read: never through a
// link, and never past 128 MiB.
var backupManifestFile = artifactdir.Document{
	MaxBytes: 128 << 20,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("backup incomplete"),
		Open:      artifactdir.FilesystemReport,
		Read:      ErrLimit,
		Size:      errors.New("backup incomplete"),
	},
}

func readBackup(root *os.Root) (backupManifest, error) {
	var m backupManifest
	data, err := backupManifestFile.ReadIn(root, "manifest.json")
	if err != nil {
		return m, err
	}
	var envelope map[string]jsontext.Value
	if err = json.Unmarshal(data, &envelope); err != nil {
		return m, errors.New("invalid backup manifest")
	}
	var schema string
	if err = json.Unmarshal(envelope["schema"], &schema); err != nil {
		return m, err
	}
	if (schema == "readmit-hub-backup/v1" && len(data) > 16<<20) || (schema == "readmit-hub-backup/v2" && len(data) > 32<<20) || (schema == "readmit-hub-backup/v3" && len(data) > 64<<20) {
		return m, ErrLimit
	}
	isV5 := schema == "readmit-hub-backup/v5"
	if isV5 {
		if requireExactMembers(data, "schema", "metadata_version", "artifacts", "projects", "team_enabled", "reviews", "lifecycle") != nil {
			return m, ErrIntegrity
		}
		var version int
		if json.Unmarshal(envelope["metadata_version"], &version) != nil || version != 6 {
			return m, ErrIntegrity
		}
		envelope["schema"] = jsontext.Value(`"readmit-hub-backup/v4"`)
		envelope["metadata_version"] = jsontext.Value(`5`)
		data, err = json.Marshal(envelope)
		if err != nil {
			return m, err
		}
		schema = "readmit-hub-backup/v4"
	}
	var lifecycle []LifecycleEvent
	isV4 := schema == "readmit-hub-backup/v4"
	if isV4 {
		if requireExactMembers(data, "schema", "metadata_version", "artifacts", "projects", "team_enabled", "reviews", "lifecycle") != nil {
			return m, ErrIntegrity
		}
		var version int
		if json.Unmarshal(envelope["metadata_version"], &version) != nil || version != 5 {
			return m, ErrIntegrity
		}
		var raw []jsontext.Value
		if json.Unmarshal(envelope["lifecycle"], &raw) != nil || len(raw) > hubprotocol.MaxLifecycle {
			return m, ErrIntegrity
		}
		for _, entry := range raw {
			if requireExactMembers(entry, "schema", "project", "sequence", "issuer", "actor", "at", "review_head", "command") != nil {
				return m, ErrIntegrity
			}
			var event LifecycleEvent
			if json.Unmarshal(entry, &event, json.RejectUnknownMembers(true)) != nil {
				return m, ErrIntegrity
			}
			var fields map[string]jsontext.Value
			if json.Unmarshal(entry, &fields) != nil {
				return m, ErrIntegrity
			}
			if _, e := hubprotocol.DecodeLifecycleCommand(fields["command"]); e != nil {
				return m, ErrIntegrity
			}
			lifecycle = append(lifecycle, event)
		}
		delete(envelope, "lifecycle")
		envelope["schema"] = jsontext.Value(`"readmit-hub-backup/v3"`)
		envelope["metadata_version"] = jsontext.Value(`4`)
		data, err = json.Marshal(envelope)
		if err != nil {
			return m, err
		}
		schema = "readmit-hub-backup/v3"
	}
	var reviews []ReviewEvent
	isV3 := schema == "readmit-hub-backup/v3"
	if isV3 {
		if requireExactMembers(data, "schema", "metadata_version", "artifacts", "projects", "team_enabled", "reviews") != nil {
			return m, ErrIntegrity
		}
		var version int
		if json.Unmarshal(envelope["metadata_version"], &version) != nil || version != 4 {
			return m, ErrIntegrity
		}
		var raw []jsontext.Value
		if json.Unmarshal(envelope["reviews"], &raw) != nil || len(raw) > hubprotocol.MaxReviews {
			return m, ErrLimit
		}
		for _, entry := range raw {
			if requireExactMembers(entry, "schema", "project", "sequence", "issuer", "actor", "at", "command") != nil {
				return m, ErrIntegrity
			}
			var event ReviewEvent
			if json.Unmarshal(entry, &event, json.RejectUnknownMembers(true)) != nil {
				return m, ErrIntegrity
			}
			var fields map[string]jsontext.Value
			if json.Unmarshal(entry, &fields) != nil {
				return m, ErrIntegrity
			}
			if _, e := hubprotocol.DecodeReviewCommand(fields["command"]); e != nil {
				return m, e
			}
			reviews = append(reviews, event)
		}
		delete(envelope, "reviews")
		envelope["schema"] = jsontext.Value(`"readmit-hub-backup/v2"`)
		envelope["metadata_version"] = jsontext.Value(`3`)
		data, err = json.Marshal(envelope)
		if err != nil {
			return m, err
		}
		schema = "readmit-hub-backup/v2"
	}
	var projects []projectLink
	var team *bool
	if schema == "readmit-hub-backup/v2" {
		if requireExactMembers(data, "schema", "metadata_version", "artifacts", "projects", "team_enabled") != nil {
			return m, errAccess
		}
		if err = json.Unmarshal(envelope["team_enabled"], &team); err != nil {
			return m, err
		}
		var raw []jsontext.Value
		if err = json.Unmarshal(envelope["projects"], &raw); err != nil {
			return m, err
		}
		if len(raw) > 65536 {
			return m, ErrLimit
		}
		for _, entry := range raw {
			if requireExactMembers(entry, "project", "sha256") != nil {
				return m, errAccess
			}
			var link projectLink
			if err = json.Unmarshal(entry, &link, json.RejectUnknownMembers(true)); err != nil {
				return m, err
			}
			projects = append(projects, link)
		}
		delete(envelope, "projects")
		delete(envelope, "team_enabled")
		data, err = json.Marshal(envelope)
		if err != nil {
			return m, err
		}
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
	if !((m.Schema == "readmit-hub-backup/v1" && m.MetadataVersion == 2) || (m.Schema == "readmit-hub-backup/v2" && m.MetadataVersion == 3)) || len(m.Artifacts) > 65536 {
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
	known := map[string]bool{}
	for _, entry := range m.Artifacts {
		known[entry.Digest] = true
	}
	previousLink := projectLink{}
	for _, link := range projects {
		if !validProject(link.Project) || !known[link.Digest] || link.Project < previousLink.Project || (link.Project == previousLink.Project && link.Digest <= previousLink.Digest) {
			return m, errors.New("invalid project link")
		}
		previousLink = link
	}
	if len(projects) > 0 && (team == nil || !*team) {
		return m, errors.New("project links require team mode")
	}
	m.Projects = projects
	m.Team = team
	m.Reviews = reviews
	if isV3 {
		m.Schema = "readmit-hub-backup/v3"
		m.MetadataVersion = 4
	}
	if len(reviews) > 0 && (team == nil || !*team) {
		return m, ErrIntegrity
	}
	// Every backed-up event is replayed, in order, through the project log's
	// own rules over storage held in memory: a backup a live write would have
	// refused is refused here, before a restore writes anything.
	replay := projectLog{storage: newMemoryStorage(projects, func(d string) ([]byte, error) {
		var entry backupEntry
		for _, x := range m.Artifacts {
			if x.Digest == d {
				entry = x
				break
			}
		}
		reader := &Store{root: root}
		if e := reader.verify(d, entry.Size); e != nil {
			return nil, e
		}
		f, e := root.Open(d)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, MaxArtifactBytes+1))
		if e != nil || int64(len(data)) != entry.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != d {
			return nil, ErrIntegrity
		}
		return data, nil
	})}
	ctx := context.Background()
	lastProject := ""
	for _, event := range reviews {
		if !hubprotocol.ValidEventVersion(event, isV5) || !validProject(event.Project) || event.Project < lastProject {
			return m, ErrIntegrity
		}
		if e := replay.replayReview(ctx, event); e != nil {
			return m, e
		}
		lastProject = event.Project
	}
	if len(lifecycle) > 0 && (team == nil || !*team) {
		return m, ErrIntegrity
	}
	lastProject = ""
	for _, event := range lifecycle {
		if !validProject(event.Project) || event.Project < lastProject {
			return m, ErrIntegrity
		}
		if e := replay.replayLifecycle(ctx, event); e != nil {
			return m, e
		}
		lastProject = event.Project
	}
	m.Lifecycle = lifecycle
	if isV4 {
		m.Schema = "readmit-hub-backup/v4"
		m.MetadataVersion = 5
	}
	if isV5 {
		m.Schema = "readmit-hub-backup/v5"
		m.MetadataVersion = 6
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
	if err = tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM readmit_hub_artifacts)+(SELECT count(*) FROM readmit_hub_reviews)+(SELECT count(*) FROM readmit_hub_lifecycle)").Scan(&count); err != nil || count != 0 {
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
	for _, link := range m.Projects {
		if _, err = tx.ExecContext(ctx, "INSERT INTO readmit_hub_project_artifacts(project,digest) VALUES($1,$2)", link.Project, link.Digest); err != nil {
			return errors.New("project metadata restore failed")
		}
	}
	for _, event := range m.Reviews {
		if err = reviewLog.appendInTx(ctx, tx, event.Project, event); err != nil {
			return errors.New("review restore failed")
		}
	}
	for _, event := range m.Lifecycle {
		if err = lifecycleLog.appendInTx(ctx, tx, event.Project, event); err != nil {
			return err
		}
	}
	team := false
	if m.Team != nil {
		team = *m.Team
	}
	if err = setTeamEnabled(ctx, tx, team); err != nil {
		return err
	}
	if err = syncRoot(s.root); err != nil {
		return err
	}
	return tx.Commit()
}

// VerifyBackup checks a stopped-service recovery directory without restoring or
// mutating it. Hashes prove consistency, not authenticity of the backup source.
func VerifyBackup(ctx context.Context, source string) error {
	info, e := os.Lstat(source)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrIntegrity
	}
	root, e := os.OpenRoot(source)
	if e != nil {
		return e
	}
	defer root.Close()
	m, e := readBackup(root)
	if e != nil {
		return e
	}
	for _, entry := range m.Artifacts {
		if e = ctx.Err(); e != nil {
			return e
		}
		reader := &Store{root: root}
		if e = reader.verify(entry.Digest, entry.Size); e != nil {
			return e
		}
	}
	return ctx.Err()
}
