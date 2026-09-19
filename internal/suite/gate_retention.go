package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/artifactpath"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const gateRetentionSchema = "readmit-ci-retention/v1"
const gateMaxFiles = 100000
const gateMaxBytes = 256 << 20

type gateFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (f *gateFile) UnmarshalJSON(raw []byte) error {
	type plain gateFile
	return required(raw, (*plain)(f), "path", "sha256", "size")
}

type gateRetention struct {
	Schema     string     `json:"schema"`
	Policy     string     `json:"policy_identity"`
	AssessedAt string     `json:"assessed_at"`
	Files      []gateFile `json:"files"`
}

func (r *gateRetention) UnmarshalJSON(raw []byte) error {
	type plain gateRetention
	return required(raw, (*plain)(r), "schema", "policy_identity", "assessed_at", "files")
}

// gateTree reads bounded regular files through an OS root. Symlinks, devices and
// directory changes refuse. With destination set it copies exclusively into a
// private new tree; hashes are calculated from precisely those copied bytes.
func gateTree(ctx context.Context, source, destination string, maxBytes int64) ([]gateFile, error) {
	root, e := os.OpenRoot(source)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	files := []gateFile{}
	var total int64
	entries := 0
	e = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		entries++
		if entries > gateMaxFiles {
			return errors.New("retention entry limit exceeded")
		}
		info, e := root.Lstat(name)
		if e != nil {
			return e
		}
		if info.IsDir() {
			if destination != "" && name != "." {
				return os.Mkdir(filepath.Join(destination, filepath.FromSlash(name)), 0700)
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxBytes-total {
			return errors.New("retention requires bounded regular files")
		}
		f, e := root.Open(name)
		if e != nil {
			return e
		}
		opened, e := f.Stat()
		if e != nil || !os.SameFile(info, opened) {
			f.Close()
			return errors.New("retention input changed")
		}
		raw, e := io.ReadAll(io.LimitReader(f, maxBytes-total+1))
		f.Close()
		if e != nil || int64(len(raw)) != info.Size() {
			return errors.New("retention input changed or exceeded limit")
		}
		total += int64(len(raw))
		if total > maxBytes {
			return errors.New("retention byte limit exceeded")
		}
		if destination != "" {
			if e = retain(filepath.Dir(filepath.Join(destination, filepath.FromSlash(name))), filepath.Base(name), raw); e != nil {
				return e
			}
		}
		files = append(files, gateFile{name, promotionHash(raw), int64(len(raw))})
		return nil
	})
	return files, e
}

// RetainGate never sends and never mutates its inputs. Failed/incomplete runs
// are retained too. An interrupted copy has no manifest and cannot verify.
func RetainGate(ctx context.Context, current, baseline, policy, pinned, output string, at time.Time) GateReport {
	unknown := gateUnknown()
	if ctx.Err() != nil || at.IsZero() || !validDigest(pinned) {
		return unknown
	}
	raw, e := read(policy, MaxBytes)
	if e != nil {
		return unknown
	}
	p, e := DecodeGatePolicy(raw)
	if e != nil || p.Identity() != pinned {
		return unknown
	}
	until, _ := time.Parse(time.RFC3339, p.RetainUntil)
	if !at.Before(until) {
		unknown.Retention = "expired"
		return unknown
	}
	current, e = artifactpath.Directory(current)
	if e != nil {
		return unknown
	}
	baseline, e = artifactpath.Directory(baseline)
	if e != nil || baseline == current {
		return unknown
	}
	out, e := artifactpath.Destination(output)
	if e != nil {
		return unknown
	}
	for _, source := range []string{current, baseline} {
		rel, e := filepath.Rel(source, out)
		if e != nil || rel == "." || filepath.IsLocal(rel) {
			return unknown
		}
	}
	if e = os.Mkdir(out, 0700); e != nil {
		return unknown
	}
	if e = retain(out, "policy.json", raw); e != nil {
		return unknown
	}
	coverage, _ := json.Marshal(p.Coverage, json.Deterministic(true))
	if e = retain(out, "coverage.json", coverage); e != nil {
		return unknown
	}
	for _, item := range []struct{ name, path string }{{"current", current}, {"baseline", baseline}} {
		target := filepath.Join(out, item.name)
		if e = os.Mkdir(target, 0700); e != nil {
			return unknown
		}
		if _, e = gateTree(ctx, item.path, target, p.MaxBytes); e != nil {
			return unknown
		}
	}
	files, e := gateTree(ctx, out, "", p.MaxBytes)
	if e != nil {
		return unknown
	}
	// Evaluate only the retained copy. The manifest pins it independently of any
	// final report and is required even for unknown/failed retained assessments.
	r := assessGate(ctx, out, p, at)
	check, e := gateTree(ctx, out, "", p.MaxBytes)
	if e != nil || !sameCoverageJSON(files, check) {
		return unknown
	}
	manifest := gateRetention{gateRetentionSchema, pinned, at.UTC().Format(time.RFC3339Nano), files}
	encoded, _ := json.Marshal(manifest, json.Deterministic(true))
	if e = retain(out, "retention.json", encoded); e != nil {
		return unknown
	}
	encoded, _ = json.Marshal(r, json.Deterministic(true))
	if e = retain(out, "gate.json", encoded); e != nil {
		return unknown
	}
	if _, e = gateTree(ctx, out, "", p.MaxBytes); e != nil {
		return unknown
	}
	return r
}

// VerifyGate revalidates every retained byte, then repeats the original
// assessment at its retained instant. now enforces retention expiry, never
// deletes evidence, and never converts an old unknown into a current pass.
func VerifyGate(ctx context.Context, directory, pinned string, now time.Time) GateReport {
	unknown := gateUnknown()
	if now.IsZero() || !validDigest(pinned) {
		return unknown
	}
	root, e := artifactpath.Directory(directory)
	if e != nil {
		return unknown
	}
	raw, e := read(filepath.Join(root, "retention.json"), 32<<20)
	if e != nil {
		return unknown
	}
	var manifest gateRetention
	if json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != gateRetentionSchema || manifest.Policy != pinned || len(manifest.Files) > gateMaxFiles {
		return unknown
	}
	at, e := time.Parse(time.RFC3339Nano, manifest.AssessedAt)
	if e != nil || at.IsZero() || now.Before(at) {
		return unknown
	}
	policyRaw, e := read(filepath.Join(root, "policy.json"), MaxBytes)
	if e != nil {
		return unknown
	}
	p, e := DecodeGatePolicy(policyRaw)
	if e != nil || p.Identity() != pinned {
		return unknown
	}
	files, e := gateTree(ctx, root, "", p.MaxBytes)
	if e != nil {
		return unknown
	}
	actual := []gateFile{}
	for _, f := range files {
		if f.Path != "retention.json" && f.Path != "gate.json" {
			actual = append(actual, f)
		}
	}
	if !sameCoverageJSON(actual, manifest.Files) {
		return unknown
	}
	until, _ := time.Parse(time.RFC3339, p.RetainUntil)
	if !now.Before(until) {
		unknown.Retention = "expired"
		return unknown
	}
	coverage, _ := json.Marshal(p.Coverage, json.Deterministic(true))
	raw, e = read(filepath.Join(root, "coverage.json"), MaxBytes)
	if e != nil || string(raw) != string(coverage) {
		return unknown
	}
	r := assessGate(ctx, root, p, at)
	after, e := gateTree(ctx, root, "", p.MaxBytes)
	if e != nil || !sameCoverageJSON(files, after) {
		return unknown
	}
	return r
}
