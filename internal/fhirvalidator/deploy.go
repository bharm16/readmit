package fhirvalidator

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// PackageSchema is the offline transfer form of one staged capability: the
// sealed capability directory, the worker image as the container engine saves
// it, and a manifest binding the two by digest. Its identity is the SHA-256 of
// the manifest's bytes, which an administrator publishes beside the package
// and every installation compares before anything is loaded.
const PackageSchema = "readmit-fhir-validator-package/v1"

// The members of a package directory.
const (
	packageManifest   = "package.json"
	packageCapability = "capability"
	packageArchive    = "image.tar"
)

// maxImageArchive bounds a saved worker image. The qualified image is about
// 600 MiB; anything past this is not that image.
const maxImageArchive = 4 << 30

// PackageArchiveRecord names the saved image archive by size and digest.
type PackageArchiveRecord struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// PackageManifest binds a capability identity to the exact image its worker
// runs and to the saved archive holding that image.
type PackageManifest struct {
	Schema     string               `json:"schema"`
	Capability string               `json:"capability"`
	Image      string               `json:"image"`
	Platform   string               `json:"platform"`
	Archive    PackageArchiveRecord `json:"archive"`
}

// VerifiedPackage is a package whose manifest matched the identity the
// administrator expected, whose capability reopened under its seal and whose
// archive holds the exact image the capability names.
type VerifiedPackage struct {
	Identity   string
	Manifest   PackageManifest
	Capability *Capability
	dir        string
}

func refusedPackage(requirement string) error { return unavailable(StatePackageInvalid, requirement) }

// ExportPackage writes a new package directory from a staged capability whose
// image this engine holds. It saves the image, copies the sealed capability
// and answers the package identity. Nothing is fetched.
func (e *Engine) ExportPackage(ctx context.Context, capability, output string) (*VerifiedPackage, error) {
	c, err := OpenCapability(capability)
	if err != nil {
		return nil, unavailable(StateCapabilityUnavailable, "select a staged validator capability")
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	if status := e.Check(ctx, c); status.State != StateReady {
		return nil, status
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		return nil, unavailable(StatePackageInvalid, "choose a new package folder inside an existing folder")
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(output)
		}
	}()
	assets := map[string][]byte{}
	for name, raw := range c.files {
		if name != "manifest.json" && name != "identity.sha256" {
			assets[name] = raw
		}
	}
	copied, err := Stage(ctx, filepath.Join(output, packageCapability), c.manifest, assets)
	if err != nil || copied.Identity() != c.Identity() {
		return nil, refusedPackage("stage the capability again")
	}
	archive := filepath.Join(output, packageArchive)
	if _, err := e.long(ctx, "save", "--output", archive, c.manifest.Image); err != nil {
		return nil, unavailable(StateWorkerUnavailable, "start the selected local container engine")
	}
	record, err := readImageArchive(archive, c.manifest.Image)
	if err != nil {
		return nil, err
	}
	m := PackageManifest{Schema: PackageSchema, Capability: c.Identity(), Image: c.manifest.Image, Platform: c.manifest.Platform, Archive: record}
	raw := encode(m)
	if err := os.WriteFile(filepath.Join(output, packageManifest), raw, 0o600); err != nil {
		return nil, refusedPackage("choose a writable package folder")
	}
	complete = true
	return &VerifiedPackage{Identity: digest(raw), Manifest: m, Capability: c, dir: output}, nil
}

// VerifyPackage checks a package offline against the identity its
// administrator published: the manifest's digest, the capability's seal and
// pins, and that the archive is the recorded bytes and holds the named image.
// It starts no program and opens no connection.
func VerifyPackage(dir, identity string) (*VerifiedPackage, error) {
	if !validDigest(identity) {
		return nil, unavailable(StateUntrustedPackage, "enter the full package identity your administrator published")
	}
	raw, err := readBounded(filepath.Join(dir, packageManifest), 64<<10)
	if err != nil {
		return nil, refusedPackage("copy the complete package folder again")
	}
	if digest(raw) != identity {
		return nil, unavailable(StateUntrustedPackage, "the package is not the one whose identity was published; obtain the published package again")
	}
	var m PackageManifest
	if json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)) != nil || m.Schema != PackageSchema || m.Archive.File != packageArchive || m.Archive.Bytes < 1 || m.Archive.Bytes > maxImageArchive || !validDigest(m.Archive.SHA256) || !validDigest(m.Capability) {
		return nil, refusedPackage("copy the complete package folder again")
	}
	c, err := OpenCapability(filepath.Join(dir, packageCapability))
	if err != nil || c.Identity() != m.Capability || c.manifest.Image != m.Image || c.manifest.Platform != m.Platform {
		return nil, refusedPackage("copy the complete package folder again")
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	record, err := readImageArchive(filepath.Join(dir, packageArchive), m.Image)
	if err != nil {
		return nil, err
	}
	if record != m.Archive {
		return nil, refusedPackage("copy the complete package folder again")
	}
	return &VerifiedPackage{Identity: identity, Manifest: m, Capability: c, dir: dir}, nil
}

// InstallPackage installs a verified package into this engine and stages its
// capability at output, the folder a connection then selects. The engine must
// be the qualified platform; the image is loaded from the package only, and
// the capability is written only once the engine holds that exact image.
func (e *Engine) InstallPackage(ctx context.Context, dir, identity, output string) (*Capability, error) {
	p, err := VerifyPackage(dir, identity)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
		return nil, unavailable(StateCapabilityExists, "choose a new folder for the installed capability")
	}
	if _, err := os.Stat(filepath.Dir(output)); err != nil {
		return nil, unavailable(StateCapabilityExists, "choose a new folder inside an existing folder")
	}
	if status := e.platform(ctx); status.State != StateReady {
		return nil, status
	}
	if _, err := e.long(ctx, "load", "--quiet", "--input", filepath.Join(dir, packageArchive)); err != nil {
		return nil, unavailable(StateWorkerUnavailable, "start the selected local container engine")
	}
	if status := e.Check(ctx, p.Capability); status.State != StateReady {
		return nil, status
	}
	assets := map[string][]byte{}
	for name, raw := range p.Capability.files {
		if name != "manifest.json" && name != "identity.sha256" {
			assets[name] = raw
		}
	}
	installed, err := Stage(ctx, output, p.Capability.manifest, assets)
	if err != nil || installed.Identity() != p.Capability.Identity() {
		return nil, unavailable(StateCapabilityExists, "choose a new folder for the installed capability")
	}
	return installed, nil
}

// RemoveInstalled removes an installed capability: its worker image from this
// engine, unless a capability that stays installed names the same image, and
// then its folder. Retained validation results keep their own copy of the
// capability and stay readable offline.
func (e *Engine) RemoveInstalled(ctx context.Context, capability string, keep ...string) error {
	c, err := OpenCapability(capability)
	if err != nil {
		return unavailable(StateCapabilityUnavailable, "select an installed validator capability")
	}
	shared := false
	for _, other := range keep {
		kept, err := OpenCapability(other)
		if err != nil {
			return unavailable(StateCapabilityUnavailable, "select only installed validator capabilities to keep")
		}
		if kept.manifest.Image == c.manifest.Image {
			shared = true
		}
	}
	if !shared {
		if status := e.platform(ctx); status.State != StateReady {
			return status
		}
		if _, err := e.control(ctx, "image", "inspect", c.manifest.Image, "--format", "{{json .Id}}"); err == nil {
			if _, err := e.long(ctx, "image", "rm", c.manifest.Image); err != nil {
				return unavailable(StateWorkerUnavailable, "stop every validation that uses this capability, then remove it again")
			}
		}
	}
	if err := os.RemoveAll(capability); err != nil {
		return unavailable(StateCapabilityUnavailable, "this account cannot remove the capability folder")
	}
	return nil
}

// platform reports whether this engine is reachable and the qualified worker
// platform, before anything is loaded into it.
func (e *Engine) platform(ctx context.Context) Status {
	if e == nil || e.config == "" {
		return Status{StateWorkerMissing, "install the optional local validation capability"}
	}
	raw, err := e.control(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		return Status{StateWorkerUnavailable, "start the selected local container engine"}
	}
	var info struct {
		ServerVersion string `json:"ServerVersion"`
		OSType        string `json:"OSType"`
		Architecture  string `json:"Architecture"`
	}
	// A command line that cannot reach its engine can still print its own
	// half of the answer with no server in it: that engine is unavailable,
	// not an unsupported platform.
	if json.Unmarshal(raw, &info) == nil && info.ServerVersion == "" {
		return Status{StateWorkerUnavailable, "start the selected local container engine"}
	}
	if json.Unmarshal(raw, &info) != nil || info.OSType != "linux" || (info.Architecture != "aarch64" && info.Architecture != "arm64") || !engineVersion.MatchString(info.ServerVersion) {
		return Status{StateUnsupportedRuntime, "use the qualified Linux arm64 worker platform"}
	}
	return Status{State: StateReady}
}

// long runs one engine operation that moves a whole image, which can take
// minutes where a control query takes seconds.
func (e *Engine) long(ctx context.Context, args ...string) ([]byte, error) {
	return e.invoke(ctx, 10*time.Minute, args...)
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

func readBounded(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("not a bounded regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("not a bounded regular file")
	}
	return raw, nil
}

// readImageArchive reads a saved image archive once: its size and digest, and
// that its manifest names exactly one image whose configuration is the image
// ID, a blob of the archive whose own digest is that ID. Layers are verified
// by the engine against that configuration when it loads them.
func readImageArchive(name, image string) (PackageArchiveRecord, error) {
	missing := unavailable(StatePackageInvalid, "the worker image archive is missing or incomplete; copy the complete package folder again")
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxImageArchive {
		return PackageArchiveRecord{}, missing
	}
	f, err := os.Open(name)
	if err != nil {
		return PackageArchiveRecord{}, missing
	}
	defer f.Close()
	whole := sha256.New()
	reader := tar.NewReader(io.TeeReader(f, whole))
	blobs := map[string]bool{}
	var manifest []byte
	for entries := 0; ; entries++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || entries > 100000 {
			return PackageArchiveRecord{}, missing
		}
		member := path.Clean(header.Name)
		switch {
		case member == "manifest.json" && header.Typeflag == tar.TypeReg && header.Size <= 1<<20:
			if manifest, err = io.ReadAll(reader); err != nil {
				return PackageArchiveRecord{}, missing
			}
		case header.Typeflag == tar.TypeReg && header.Size <= 1<<20:
			h := sha256.New()
			if _, err := io.Copy(h, reader); err != nil {
				return PackageArchiveRecord{}, missing
			}
			blobs[member] = hex.EncodeToString(h.Sum(nil)) == configHex(member)
		}
	}
	if _, err := io.Copy(io.Discard, f); err != nil {
		return PackageArchiveRecord{}, missing
	}
	var entries []struct {
		Config string `json:"Config"`
	}
	wrong := unavailable(StatePackageInvalid, "the archive does not hold the image this capability names; export the package again")
	if json.Unmarshal(manifest, &entries) != nil || len(entries) != 1 {
		return PackageArchiveRecord{}, wrong
	}
	config := path.Clean(entries[0].Config)
	if "sha256:"+configHex(config) != image || !blobs[config] {
		return PackageArchiveRecord{}, wrong
	}
	return PackageArchiveRecord{File: packageArchive, Bytes: info.Size(), SHA256: hex.EncodeToString(whole.Sum(nil))}, nil
}

// configHex is the digest an archive member's own name claims: the blob name
// of the layout a current engine saves (blobs/sha256/HEX) or the configuration
// file of the older one (HEX.json).
func configHex(member string) string {
	base := path.Base(member)
	if dir := path.Dir(member); dir == "blobs/sha256" && validDigest(base) {
		return base
	}
	if hexName, ok := strings.CutSuffix(base, ".json"); ok && path.Dir(member) == "." && validDigest(hexName) {
		return hexName
	}
	return ""
}

// The states an installed validator's check and deployment answer, beside
// the worker states a validation records.
const (
	StateReady                 = "ready"
	StateNotConfigured         = "not-configured"
	StateCapabilityUnavailable = "capability-unavailable"
	StateCapabilityExists      = "capability-exists"
	StateUntrustedPackage      = "untrusted-package"
	StatePackageInvalid        = "package-invalid"
	StatePackageUnavailable    = "package-unavailable"
	StateUnsupportedRuntime    = "unsupported-runtime"
	StateWorkerMissing         = "worker-missing"
	StateWorkerUnavailable     = "worker-unavailable"
)

// CheckInstalled checks an installed validator folder as a validation would
// meet it: the capability's seal and pins offline, then whether the local
// engine at socket is the qualified platform and holds the exact worker
// image. It answers the capability when it could be read, and the state.
func CheckInstalled(ctx context.Context, directory, socket string) (*Capability, Status) {
	c, err := OpenCapability(directory)
	if err != nil {
		return nil, Status{StateCapabilityUnavailable, "install the validator package, then choose its folder"}
	}
	if err := c.Check(); err != nil {
		return c, statusOf(err)
	}
	engine, err := LocalEngine(socket)
	if err != nil {
		return c, statusOf(err)
	}
	defer engine.Close()
	return c, engine.Check(ctx, c)
}

// statusOf is the actionable state an error carries; any other error means
// the installed validator cannot be read.
func statusOf(err error) Status {
	var status Status
	if !errors.As(err, &status) {
		status = Status{StateCapabilityUnavailable, "install the validator package, then choose its folder"}
	}
	return status
}
