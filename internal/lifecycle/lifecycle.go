// Package lifecycle previews compatibility and retires whole projects through
// verified recovery archives. It never migrates canonical evidence in place.
package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
)

const PreviewSchema = "readmit-project-migration-preview/v1"

type Compatibility struct {
	Document  string `json:"document"`
	Supported string `json:"supported"`
	Action    string `json:"action"`
}
type Plan struct {
	Schema     string          `json:"schema"`
	Compatible bool            `json:"compatible"`
	Documents  []Compatibility `json:"documents"`
}

// Preview reports the readers this release has. There is no converter for an
// unknown contract. Current indexes are disposable and rebuilt on restore.
func Preview(ctx context.Context, path string) (Plan, error) {
	p := Plan{Schema: PreviewSchema, Compatible: true}
	root, err := artifactpath.Directory(path)
	if err != nil {
		return p, err
	}
	add := func(name, schema, action string, err error) {
		if err != nil {
			action = "refused"
			p.Compatible = false
		}
		p.Documents = append(p.Documents, Compatibility{name, schema, action})
	}
	_, err = project.Open(root)
	add(project.DocumentName, project.Schema, "unchanged", err)
	_, err = project.ReadRevisions(root)
	add(project.RevisionsDocumentName, project.RevisionsSchema, "unchanged", err)
	_, present, err := project.ReadQuota(root)
	if present || err != nil {
		add(project.QuotaDocumentName, project.QuotaSchema, "unchanged", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return p, errors.New("cannot inspect project schemas")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return p, errors.New("cannot inspect project entry")
		}
		if !info.Mode().IsRegular() {
			return p, errors.New("project preview refuses nonregular entries")
		}
		if info.Size() > backup.MaxFileBytes {
			return p, errors.New("project preview refuses files beyond the archive file bound")
		}
		opened, err := os.Open(filepath.Join(root, entry.Name()))
		if err != nil {
			return p, errors.New("cannot open project entry")
		}
		data, err := io.ReadAll(io.LimitReader(opened, backup.MaxFileBytes+1))
		closeErr := opened.Close()
		if err != nil || closeErr != nil || int64(len(data)) > backup.MaxFileBytes {
			return p, errors.New("cannot read project entry within archive bound")
		}
		if !index.DeclaresSchema(data) {
			continue
		}
		_, err = index.Decode(data)
		add(entry.Name(), index.Schema, "rebuild-on-restore", err)
	}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	return p, nil
}

type fingerprint struct {
	Size   int64
	Digest string
}

func inventory(ctx context.Context, path string) (map[string]fingerprint, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot inspect retirement source")
	}
	defer root.Close()
	files := map[string]fingerprint{}
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cannot enumerate retirement source")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("retirement refuses nonregular entries")
		}
		if len(files) >= backup.MaxFiles || info.Size() > backup.MaxFileBytes || total > (backup.MaxBytes+int64(backup.MaxIndexes)*index.MaxIndexBytes)-info.Size() {
			return errors.New("retirement source exceeds backup limits")
		}
		total += info.Size()
		f, err := root.Open(name)
		if err != nil {
			return errors.New("cannot read retirement source")
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, backup.MaxFileBytes+1))
		closeErr := f.Close()
		if err != nil || closeErr != nil || n != info.Size() {
			return errors.New("retirement source changed or could not be read")
		}
		files[name] = fingerprint{n, hex.EncodeToString(h.Sum(nil))}
		return nil
	})
	return files, err
}

// selectionOf is the token that binds an archive or a delete to exactly the
// bytes one preview inventoried: one hash over every file's name and content
// digest, so an added, removed, replaced or rewritten file changes it.
func selectionOf(files map[string]fingerprint) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		io.WriteString(h, name+"\n"+files[name].Digest+"\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Retirement is the preview one archive or delete consumes: the compatibility
// plan, the source as the backup limits measure it, and the selection token
// that binds a later Archive to exactly these bytes. It is answered to a
// caller and persisted nowhere.
type Retirement struct {
	Selection  string
	Compatible bool
	Files      int
	Bytes      int64
	Documents  []Compatibility
}

// PreviewRetirement inventories what archive or delete would affect, under the
// backup limits Archive is held to, and derives the selection token that binds
// a later Archive to the previewed bytes. It changes nothing.
func PreviewRetirement(ctx context.Context, path string) (Retirement, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return Retirement{}, err
	}
	plan, err := Preview(ctx, root)
	if err != nil {
		return Retirement{}, err
	}
	files, err := inventory(ctx, root)
	if err != nil {
		return Retirement{}, err
	}
	var total int64
	for _, held := range files {
		total += held.Size
	}
	return Retirement{
		Selection:  selectionOf(files),
		Compatible: plan.Compatible,
		Files:      len(files),
		Bytes:      total,
		Documents:  plan.Documents,
	}, nil
}

// Archive writes a new verified recovery backup. Delete additionally retires
// the source, but only when the selection still matches the bytes a
// PreviewRetirement inventoried: a project changed after its preview is
// refused before anything is written, and the source is retained. Callers
// must stop other writers for this whole operation (the project has one
// writer). A cancellation before retirement retains the source and any
// partial backup.
func Archive(ctx context.Context, path, destination, selection string, deleteSource bool) (backup.Report, error) {
	var report backup.Report
	if err := ctx.Err(); err != nil {
		return report, err
	}
	root, err := artifactpath.Directory(path)
	if err != nil {
		return report, err
	}
	if selection == "" {
		return report, errors.New("archive and delete require a current retirement preview selection")
	}
	before, err := inventory(ctx, root)
	if err != nil {
		return report, err
	}
	if selectionOf(before) != selection {
		return report, errors.New("the project changed since the retirement preview; nothing was deleted")
	}
	plan, err := Preview(ctx, root)
	if err != nil {
		return report, err
	}
	if !plan.Compatible {
		return report, errors.New("project migration preview is incompatible; no migration is supported")
	}
	report, err = backup.Create(ctx, root, destination)
	if err != nil {
		return report, err
	}
	if !report.Complete() {
		return report, errors.New("archive is incomplete; source retained")
	}
	held, err := backup.Verify(report.Root)
	if err != nil {
		return report, err
	}
	if !held.Complete() {
		return report, errors.New("archive is incomplete; source retained")
	}
	after, err := inventory(ctx, root)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(before, after) {
		return report, errors.New("project changed during archive; source retained")
	}
	// A complete manifest must account for every canonical source byte; indexes
	// deliberately have a recorded rebuild recipe instead of retained values.
	indexed := map[string]bool{}
	for _, entry := range held.Indexes {
		indexed[entry.Name] = true
	}
	for _, file := range held.Files {
		if before[file.Path] != (fingerprint{file.Size, file.SHA256}) {
			return report, errors.New("archive does not match source; source retained")
		}
	}
	if len(held.Files)+len(indexed) != len(before) {
		return report, errors.New("archive does not account for source; source retained")
	}
	if !deleteSource {
		return report, nil
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	retiring, err := artifactpath.Destination(root + ".retiring")
	if err != nil {
		return report, errors.New("retirement destination already exists or is unsafe; source retained")
	}
	if _, err := os.Lstat(retiring); !errors.Is(err, fs.ErrNotExist) {
		return report, errors.New("retirement destination already exists or cannot be inspected; source retained")
	}
	if err := os.Rename(root, retiring); err != nil {
		return report, errors.New("cannot retire project; source retained")
	}
	// Once renamed, finish the explicit deletion even if cancellation arrives.
	// A failed removal leaves the remainder at the documented .retiring path.
	if err := os.RemoveAll(retiring); err != nil {
		return report, errors.New("project deletion incomplete; remainder retained at PROJECT.retiring and recovery archive retained")
	}
	return report, nil
}
