// Package upgrade answers the two questions an administrator has before
// installing a newer readmit on a workstation: is the build staged on this
// machine the build it claims to be, and does the build running the check read
// the evidence this machine already holds.
//
// It installs nothing. It downloads nothing, opens no network connection and
// contacts no update service — [ADR-0003] contracts are read where they lie,
// and the candidate is a directory an administrator already put there. The
// installation itself is the platform's own installer run with elevation,
// which is where the administrator approves an upgrade; nothing here can
// perform one, so nothing here can perform one silently.
//
// Above all it writes nothing to the evidence it reviews. A project whose
// documents this release cannot read is reported as unreadable and left
// exactly as it is, and a run whose retained `readmit-engine/v1` pin names a
// spec contract or profile this release does not read is reported as
// unsupported and left exactly as it is. There is no converter, no in-place
// migration and no rewrite: the answer to "this build does not read your
// evidence" is to keep the build that does, which is what the recovery archive
// taken beside this check exists for.
//
// [ADR-0003]: https://github.com/bharm16/readmit/blob/main/docs/adr/0003-specs-are-strict-json-with-typed-operators.md
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/lifecycle"
)

const (
	// PlanSchema is the document this package writes. It is a new contract
	// beside the existing ones: readmit-desktop-package/v1,
	// readmit-desktop-packaging/v1, readmit-backup/v1, readmit-engine/v1 and
	// every project document gain no member and change no byte because an
	// upgrade can be checked.
	PlanSchema = "readmit-upgrade-plan/v1"

	// CandidateSchema and CandidateDocumentName are the manifest
	// tools/package_desktop.py writes beside the packages it builds. This
	// package reads that contract and never writes it.
	CandidateSchema       = "readmit-desktop-package/v1"
	CandidateDocumentName = "manifest.json"

	// MaxCandidateBytes bounds the manifest, MaxPackages bounds how many
	// packages one staged candidate holds, and MaxPackageBytes bounds one
	// package file. Past a bound the candidate is refused rather than read as
	// far as it fit.
	MaxCandidateBytes = 64 << 10
	MaxPackages       = 16
	MaxPackageBytes   = 1 << 30

	maxIdentity  = 64
	digestLength = 64
)

// ErrUnsupportedVersion reports a staged manifest written under a contract
// version this release does not read. It is distinct from a directory holding
// no manifest at all, so a caller can say which one it found.
var ErrUnsupportedVersion = errors.New("unsupported desktop package manifest version")

// The five reasons an upgrade is refused are stated once, here, because the
// command line reports the same refusals and prose that drifted between the
// two would read as two different rules.
var (
	// ErrOtherMachine is a candidate built for a machine this is not.
	ErrOtherMachine = errors.New("the staged candidate was built for another operating system or architecture")
	// ErrSameBuild is a candidate naming the build already running the check.
	ErrSameBuild = errors.New("the staged candidate is the build already running this check; there is nothing to upgrade")
	// ErrUnsigned is a development preview. Every package this repository
	// builds records that it is one.
	ErrUnsigned = errors.New("the staged candidate records that it is not signed for distribution, so it is a development preview and not an upgrade this release installs")
	// ErrNotStaged is a package file that is not the package the manifest
	// recorded, which is what a download that stopped partway leaves.
	ErrNotStaged = errors.New("a staged package is not the package the candidate manifest recorded; stage the candidate again before installing anything")
	// ErrNotReadable is evidence on this machine that the build running the
	// check does not read. The answer is the build that does.
	ErrNotReadable = errors.New("this machine holds evidence the build running this check does not read; keep the build that does and take a recovery archive before changing anything")
)

// Staging is what re-reading one staged package file found. Only Intact is a
// package this machine may be upgraded from.
type Staging string

const (
	// Intact is a package file whose bytes are the bytes the manifest records.
	Intact Staging = "intact"
	// Altered is a package file the manifest records whose bytes are no longer
	// those. A partly written download reads as this.
	Altered Staging = "altered"
	// Absent is a package file the manifest records that the staged directory
	// does not hold.
	Absent Staging = "absent"
)

// Readability is what the build running the check can do with one artifact
// this machine already holds. Only Readable is a pass.
type Readability string

const (
	// Readable is evidence every document of which this build reads.
	Readable Readability = "readable"
	// Unsupported is evidence that names a contract version this build does
	// not read. It is reported only for a run, because a run retains an
	// explicit readmit-engine/v1 pin and a project does not.
	Unsupported Readability = "unsupported"
	// Unreadable is evidence this build refused for any other reason:
	// damaged, absent, or holding a document it could not open.
	Unreadable Readability = "unreadable"
)

// Kind separates the two things a review reads. A project is a directory of
// registered evidence and editable documents; a run is one durable job.
type Kind string

const (
	ProjectKind Kind = "project"
	RunKind     Kind = "run"
)

// Outcome is the whole plan's verdict. Ready means this release is willing to
// call the staged candidate an upgrade of this machine; everything else is
// Refused, and the members above it say which reason applied.
type Outcome string

const (
	Ready   Outcome = "ready"
	Refused Outcome = "refused"
)

// StagedPackage is one package file the candidate manifest records, named as
// the manifest names it, with what re-reading its bytes found.
type StagedPackage struct {
	Name   string  `json:"name"`
	Format string  `json:"format"`
	State  Staging `json:"state"`
}

// Retained is one artifact this machine already holds and what the build
// running the check can do with it. The name is the artifact's own directory
// entry, never the path an operator typed it at.
type Retained struct {
	Name  string      `json:"name"`
	Kind  Kind        `json:"kind"`
	State Readability `json:"state"`
}

// Plan is the complete answer to one explicit update check: the build running
// the check, the build staged beside it, whether that candidate is signed for
// distribution, what each staged package file turned out to be, and what this
// build makes of the evidence already on the machine.
type Plan struct {
	Schema                string          `json:"schema"`
	Installed             string          `json:"installed"`
	Candidate             string          `json:"candidate"`
	OS                    string          `json:"os"`
	Arch                  string          `json:"arch"`
	SignedForDistribution bool            `json:"signed_for_distribution"`
	Staged                []StagedPackage `json:"staged"`
	Retained              []Retained      `json:"retained"`
	State                 Outcome         `json:"state"`
}

// Refusal names the first reason this release will not treat the staged
// candidate as an upgrade of this machine, and nil when it will. Every reason
// is visible in the plan's own members, so a reader that only has the document
// reaches the same verdict the command did.
func (p Plan) Refusal() error {
	switch {
	case p.OS != runtime.GOOS || p.Arch != runtime.GOARCH:
		return ErrOtherMachine
	case p.Candidate == p.Installed:
		return ErrSameBuild
	case !p.SignedForDistribution:
		return ErrUnsigned
	case !p.StagedIntact():
		return ErrNotStaged
	case !p.RetainedReadable():
		return ErrNotReadable
	}
	return nil
}

// StagedIntact reports whether every package file the candidate records is the
// package it recorded. It is the part of the verdict that has to hold before a
// rollback point is worth taking, and it is deliberately separate from whether
// the candidate may be installed: a recovery archive must never depend on a
// signing decision this repository does not hold.
func (p Plan) StagedIntact() bool {
	for _, entry := range p.Staged {
		if entry.State != Intact {
			return false
		}
	}
	return true
}

// RetainedReadable reports whether the build running the check reads every
// artifact the review named.
func (p Plan) RetainedReadable() bool {
	for _, entry := range p.Retained {
		if entry.State != Readable {
			return false
		}
	}
	return true
}

// Check reads a staged candidate and reviews the artifacts an operator named.
// It opens no network connection and writes nothing at all, including nothing
// into the evidence it reviews.
func Check(ctx context.Context, candidate string, projects, runs []string) (Plan, error) {
	document, staged, err := readCandidate(ctx, candidate)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Schema:                PlanSchema,
		Installed:             engine.Version(),
		Candidate:             document.Version,
		OS:                    document.OS,
		Arch:                  document.Arch,
		SignedForDistribution: document.SignedForDistribution,
		Staged:                staged,
		Retained:              []Retained{},
	}
	for _, path := range projects {
		entry, err := reviewProject(ctx, path)
		if err != nil {
			return Plan{}, err
		}
		plan.Retained = append(plan.Retained, entry)
	}
	for _, path := range runs {
		entry, err := reviewRun(ctx, path)
		if err != nil {
			return Plan{}, err
		}
		plan.Retained = append(plan.Retained, entry)
	}
	plan.State = Ready
	if plan.Refusal() != nil {
		plan.State = Refused
	}
	return plan, nil
}

// Encode writes a plan deterministically, so the same machine and the same
// staged candidate produce the same bytes twice.
func Encode(plan Plan) ([]byte, error) {
	data, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode upgrade plan")
	}
	return append(data, '\n'), nil
}

// candidate is the `readmit-desktop-package/v1` manifest as this release reads
// it. It is a reader for the contract tools/package_desktop.py writes, added
// beside that writer; the contract itself gains no member.
type candidate struct {
	Schema                string             `json:"schema"`
	Version               string             `json:"version"`
	OS                    string             `json:"os"`
	Arch                  string             `json:"arch"`
	SignedForDistribution bool               `json:"signed_for_distribution"`
	Packages              []candidatePackage `json:"packages"`
}

type candidatePackage struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	SHA256 string `json:"sha256"`
}

// UnmarshalJSON checks the declared members are present before the strict
// decode, so a manifest from a later release is reported as the version it
// declares rather than as invalid because of the members that version added.
func (c *candidate) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema                *string         `json:"schema"`
		Version               *jsontext.Value `json:"version"`
		OS                    *jsontext.Value `json:"os"`
		Arch                  *jsontext.Value `json:"arch"`
		SignedForDistribution *jsontext.Value `json:"signed_for_distribution"`
		Packages              *jsontext.Value `json:"packages"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("a desktop package manifest declares its contract version")
	}
	if *required.Schema != CandidateSchema {
		return ErrUnsupportedVersion
	}
	if required.Version == nil || required.OS == nil || required.Arch == nil ||
		required.SignedForDistribution == nil || required.Packages == nil {
		return errors.New("a desktop package manifest declares its build, its target, whether it is signed for distribution, and the packages it names")
	}
	type plainCandidate candidate
	var value plainCandidate
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid desktop package manifest")
	}
	*c = candidate(value)
	return nil
}

// validate reports the first reason a staged manifest cannot be read as a
// candidate. Diagnostics name the member at fault and carry the position of
// the package at fault rather than repeating a name somebody chose.
func (c candidate) validate() error {
	if err := packageVersion(c.Version); err != nil {
		return errors.New("candidate build: " + err.Error())
	}
	if !slices.Contains([]string{"linux", "darwin", "windows"}, c.OS) {
		return errors.New("candidate target: not one of linux, darwin, windows")
	}
	if !slices.Contains([]string{"amd64", "arm64"}, c.Arch) {
		return errors.New("candidate target: not one of amd64, arm64")
	}
	if len(c.Packages) == 0 {
		return errors.New("a desktop package manifest names at least one package")
	}
	if len(c.Packages) > MaxPackages {
		return errors.New("a staged candidate holds at most " + strconv.Itoa(MaxPackages) + " packages")
	}
	seen := make(map[string]bool, len(c.Packages))
	for i, entry := range c.Packages {
		at := " at position " + strconv.Itoa(i+1)
		if artifactpath.EntryName(entry.Name) != nil {
			return errors.New("staged package name" + at + ": must be one file of the staged directory")
		}
		if !slices.Contains([]string{"deb", "dmg", "pkg", "msi"}, entry.Format) {
			return errors.New("staged package format" + at + ": not one of deb, dmg, pkg, msi")
		}
		if len(entry.SHA256) != digestLength || !hexadecimal(entry.SHA256) {
			return errors.New("staged package digest" + at + ": must be the digest of the package file")
		}
		if seen[entry.Name] {
			return errors.New("a staged package is recorded twice under the same name")
		}
		seen[entry.Name] = true
	}
	return nil
}

// readCandidate reads the staged manifest and re-reads every package file it
// records.
func readCandidate(ctx context.Context, path string) (candidate, []StagedPackage, error) {
	resolved, err := artifactpath.Directory(path)
	if err != nil {
		return candidate{}, nil, err
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return candidate{}, nil, errors.New("cannot open the staged candidate directory")
	}
	defer root.Close()
	raw, err := readFile(root, CandidateDocumentName, MaxCandidateBytes)
	if err != nil {
		return candidate{}, nil, errors.New("the staged candidate holds no readable " + CandidateDocumentName)
	}
	var document candidate
	if err := json.Unmarshal(raw, &document); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return candidate{}, nil, ErrUnsupportedVersion
		}
		return candidate{}, nil, errors.New("invalid desktop package manifest")
	}
	if err := document.validate(); err != nil {
		return candidate{}, nil, err
	}
	if err := onlyRecordedFiles(resolved, document); err != nil {
		return candidate{}, nil, err
	}
	staged := make([]StagedPackage, 0, len(document.Packages))
	for _, entry := range document.Packages {
		if err := ctx.Err(); err != nil {
			return candidate{}, nil, err
		}
		state, err := stagingOf(root, entry)
		if err != nil {
			return candidate{}, nil, err
		}
		staged = append(staged, StagedPackage{Name: entry.Name, Format: entry.Format, State: state})
	}
	return document, staged, nil
}

// onlyRecordedFiles checks the staged directory holds nothing but the manifest
// and the packages it names. A package the manifest records may still be
// absent, which is reported rather than refused; an unrecorded file beside
// them is not, because that is how the wrong installer gets run.
func onlyRecordedFiles(resolved string, document candidate) error {
	recorded := map[string]bool{CandidateDocumentName: true}
	for _, entry := range document.Packages {
		recorded[entry.Name] = true
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return errors.New("cannot inspect the staged candidate directory")
	}
	for _, entry := range entries {
		if !recorded[entry.Name()] {
			return errors.New("the staged candidate holds a file its manifest does not record")
		}
	}
	return nil
}

// stagingOf re-computes the digest of one staged package file. A file the
// staged directory does not hold is Absent; anything that is there and is not
// a regular file of the recorded bytes is refused or Altered, never read as
// the package it claims to be.
func stagingOf(root *os.Root, entry candidatePackage) (Staging, error) {
	info, err := root.Stat(entry.Name)
	if errors.Is(err, fs.ErrNotExist) {
		return Absent, nil
	}
	if err != nil {
		return "", errors.New("cannot inspect a staged package")
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("a staged package is not a regular file")
	}
	if info.Size() > MaxPackageBytes {
		return "", errors.New("a staged package exceeds the " + strconv.Itoa(MaxPackageBytes>>20) + " MiB package bound")
	}
	file, err := root.Open(entry.Name)
	if err != nil {
		return "", errors.New("cannot read a staged package")
	}
	sum := sha256.New()
	copied, err := io.Copy(sum, io.LimitReader(file, MaxPackageBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || copied != info.Size() {
		return "", errors.New("a staged package changed or could not be read")
	}
	if hex.EncodeToString(sum.Sum(nil)) != entry.SHA256 {
		return Altered, nil
	}
	return Intact, nil
}

// reviewProject reports whether this build reads a project's own documents. It
// is the existing migration preview, which states that there is no converter
// for an unknown contract and writes nothing.
func reviewProject(ctx context.Context, path string) (Retained, error) {
	resolved, name, err := reviewedName(path)
	if err != nil {
		return Retained{}, err
	}
	entry := Retained{Name: name, Kind: ProjectKind, State: Readable}
	plan, err := lifecycle.Preview(ctx, resolved)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Retained{}, ctxErr
	}
	if err != nil || !plan.Compatible {
		entry.State = Unreadable
	}
	return entry, nil
}

// reviewRun reports what this build makes of the readmit-engine/v1 pin a
// durable run retained. A pin naming a spec contract or profile this release
// does not read is Unsupported and distinct from a run that retains no
// readable pin at all, which is the distinction internal/engine exists to
// keep: an upgrade that read one as the other would tell an operator their
// evidence was damaged when it was only newer.
func reviewRun(ctx context.Context, path string) (Retained, error) {
	if err := ctx.Err(); err != nil {
		return Retained{}, err
	}
	_, name, err := reviewedName(path)
	if err != nil {
		return Retained{}, err
	}
	entry := Retained{Name: name, Kind: RunKind, State: Readable}
	pin, err := durablerun.Engine(path)
	if err == nil {
		err = pin.Supported()
	}
	switch {
	case errors.Is(err, engine.ErrUnsupportedVersion):
		entry.State = Unsupported
	case err != nil:
		entry.State = Unreadable
	}
	return entry, nil
}

// reviewedName resolves one reviewed artifact and names it by its own
// directory entry. A report states what an operator opened, never the path
// they opened it at: a path is somebody's filesystem and a plan repeats none.
func reviewedName(path string) (string, string, error) {
	resolved, err := artifactpath.Directory(path)
	if err != nil {
		return "", "", err
	}
	name := filepath.Base(resolved)
	if artifactpath.EntryName(name) != nil {
		return "", "", errors.New("a reviewed artifact is one named directory entry")
	}
	return resolved, name, nil
}

func readFile(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("file exceeds its size limit or could not be read")
	}
	return data, nil
}

// packageVersion accepts the release identity every package format carries
// verbatim. It restates the rule tools/package_desktop.py writes a manifest
// under, because a reader that accepted more would read a document that writer
// never produced.
func packageVersion(value string) error {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return errors.New("must begin with a digit")
	}
	if len(value) > maxIdentity {
		return errors.New("must be at most " + strconv.Itoa(maxIdentity) + " bytes")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c == '.' || c == '+' || c == '~' || c == '-':
		default:
			return errors.New("must hold only the characters a package version carries verbatim")
		}
	}
	return nil
}

// hexadecimal accepts the lowercase form every readmit digest is written in,
// so one digest cannot be recorded under two different spellings.
func hexadecimal(value string) bool {
	for i := range len(value) {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
