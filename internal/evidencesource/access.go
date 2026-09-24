package evidencesource

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// ReadSource opens one declared evidence source. Like every other input reader
// here it accepts a regular file only: a declaration that came from a pipe or a
// device is not a document an operator selected.
func ReadSource(path string) (Source, error) {
	data, err := declarationFile.Read(path)
	if err != nil {
		return Source{}, err
	}
	return Decode(data)
}

// declarationFile is how an evidence source declaration is read, through a
// link at its name.
var declarationFile = artifactdir.Document{
	MaxBytes: MaxSourceBytes,
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("an evidence source declaration must be a readable regular file"),
		Open:      errors.New("cannot open the evidence source declaration"),
		Changed:   errors.New("an evidence source declaration must be a regular file"),
		Read:      errors.New("cannot read the evidence source declaration"),
		Size:      errors.New("evidence source declaration exceeds its size limit"),
	},
}

// CredentialState is what binding a declared credential reference established,
// and never what it is. A value is not resolved to produce one: binding settles
// what a credential may be used for.
type CredentialState string

const (
	// CredentialNone is a source that declares no credential.
	CredentialNone CredentialState = "none"
	// CredentialBound is a reference registered for this source's own purpose
	// and address.
	CredentialBound CredentialState = "bound"
	// CredentialRefused is a reference that is missing, registered for another
	// purpose, or scoped to another endpoint. It is reported as refused rather
	// than as absent: a credential nobody could bind is not no credential.
	CredentialRefused CredentialState = "refused"
)

// CredentialReport names the reference a source declares and what binding it
// established. It carries no value, no locator argument and no store output.
type CredentialReport struct {
	State CredentialState `json:"state"`
	// Reference is the registered name, which is an operator-chosen label.
	Reference string `json:"reference"`
	// Rotation is what the recorded rotation says now: current, overdue or
	// not-declared. An undeclared interval is never reported as current.
	Rotation string `json:"rotation"`
	// Reason is why a refused reference was refused, and is empty otherwise.
	Reason string `json:"reason"`
}

// Access is what a permission diagnosis actually established. Every count in it
// was produced by attempting the access it reports: an entry is readable
// because this diagnosis opened it and read from it, never because its recorded
// mode bits looked permissive. Nothing here is assumed, and nothing absent is
// reported as allowed.
//
// It is a written document with no reader in this release, so it carries a
// version string and is encoded deterministically, exactly as a retained policy
// decision is. A reader for it is a later contract change, not an addition to
// this version.
type Access struct {
	Schema    string    `json:"schema"`
	Source    Identity  `json:"source"`
	CheckedAt time.Time `json:"checked_at"`
	// Status and RunState are observewindow's source-neutral vocabulary rather
	// than a second one: complete is the only status under which the counts
	// below describe the source, and every other status is an execution error.
	Status   observewindow.Status `json:"status"`
	RunState string               `json:"run_state"`
	Reason   string               `json:"reason"`
	// Listed reports whether the source's own listing was obtained at all. A
	// diagnosis that could not list reports nothing about entries.
	Listed     bool             `json:"listed"`
	Credential CredentialReport `json:"credential"`
	// Destination is the decision reached for a source that reaches off this
	// machine, and is null for a directory source, which reaches none.
	Destination *sendpolicy.Decision `json:"destination"`
	// Declared, Selected, Readable and Unreadable account for every entry the
	// listing held. NotRead counts what the listing named and this diagnosis
	// deliberately did not open: anything that is not regular file bytes.
	Declared   int `json:"declared"`
	Selected   int `json:"selected"`
	Readable   int `json:"readable"`
	Unreadable int `json:"unreadable"`
	NotRead    int `json:"not_read"`
	// DeclaredBytes is what the selected entries say they hold. It is what the
	// quota is applied to, before anything is read.
	DeclaredBytes int64 `json:"declared_bytes"`
	Quota         Quota `json:"quota"`
	// QuotaExceeded names every declared limit this listing is already past, in
	// the order the quota declares them, and is empty when a collection of the
	// same listing would fit.
	QuotaExceeded []string `json:"quota_exceeded"`
}

// EncodeAccess renders one diagnosis as the document a command retains. The
// caller performs the write, so every artifact readmit produces is reserved by
// one path owner rather than by each package that has something to record.
func EncodeAccess(access Access) ([]byte, error) {
	data, err := json.Marshal(access, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the source access diagnosis")
	}
	return append(data, '\n'), nil
}

// Options are what a caller supplies beside the declaration: the approved
// destination policy the operator explicitly selected, how a name is resolved
// at the moment the question is asked, and the plan whose declared members
// select which entries of the source are in scope.
type Options struct {
	// Policy is the explicitly selected approved-destination document, or nil
	// when the operator selected none. It governs a source that reaches off
	// this machine exactly as it governs a send; a directory source reaches no
	// destination and is asked nothing.
	Policy *sendpolicy.Policy
	// Resolve turns a configured name into the addresses it resolves to now. It
	// is a parameter so a decision is always about a resolution readmit
	// performed, and so tests never depend on a name outside the test.
	Resolve sendpolicy.Resolver
	// Plan is the same readmit-import-plan/v1 an import declares. Its members
	// select which entries are in scope, and the rest of it is what each
	// collected entry is streamed under.
	Plan importer.Plan
	// Now is the clock a diagnosis stamps itself with, so a test states the
	// instant rather than reading one.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now().UTC()
	}
	return o.Now().UTC()
}

func (o Options) resolver() sendpolicy.Resolver {
	if o.Resolve == nil {
		return sendpolicy.SystemResolver
	}
	return o.Resolve
}

// Diagnose reports what access to this source was actually available, without
// collecting anything.
//
// It never assumes. A listing it could not obtain is reported as a source it
// could not list, not as a source with no entries; an entry it could not open
// is counted as unreadable, not skipped; a destination no policy approved is
// reported as the refusal it is; and a kind this release does not support
// reports unsupported, which is an execution error rather than a pass.
func Diagnose(ctx context.Context, source Source, options Options) (Access, error) {
	if err := source.Validate(); err != nil {
		return Access{}, err
	}
	if err := options.Plan.Validate(); err != nil {
		return Access{}, err
	}
	checked := options.now()
	access := Access{
		Schema: AccessSchema, Source: source.Identity(), CheckedAt: checked,
		Quota: source.Quota, QuotaExceeded: []string{}, Credential: bindCredential(source, checked),
	}
	if source.Reaches() {
		decision := decide(ctx, source, options)
		access.Destination = &decision
		if !decision.Allowed {
			return failedAccess(access, observewindow.Failed, "the source's destination was not approved: "+string(decision.Reason)), nil
		}
	}
	if access.Credential.State == CredentialRefused {
		return failedAccess(access, observewindow.Failed, access.Credential.Reason), nil
	}
	if source.Kind == API {
		return failedAccess(access, observewindow.Unsupported,
			"this release collects from no application interface; a read-only API collection contract is documented and none is approved"), nil
	}
	listing, err := list(ctx, source)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return failedAccess(access, observewindow.Cancelled, "the diagnosis was cancelled before the source was listed"), nil
		}
		return failedAccess(access, observewindow.Failed, err.Error()), nil
	}
	access.Listed = true
	access.Declared, access.NotRead = len(listing.entries)+listing.notRead, listing.notRead
	var largest int64
	access.Selected, access.DeclaredBytes, largest = listing.selection(options.Plan)
	access.QuotaExceeded = source.Quota.exceededBy(access.Selected, access.DeclaredBytes, largest)
	for _, entry := range listing.entries {
		if !options.Plan.Selects(entry.name) {
			continue
		}
		if ctx.Err() != nil {
			return failedAccess(access, observewindow.Cancelled, "the diagnosis was cancelled while the source was being checked"), nil
		}
		// Whether an entry can be read is established by reading from it. A
		// mode bit is a claim about permission; a byte that came back is the
		// permission itself.
		if probe(ctx, source, entry) == nil {
			access.Readable++
		} else {
			access.Unreadable++
		}
	}
	switch {
	case len(access.QuotaExceeded) > 0:
		return failedAccess(access, observewindow.Truncated, "the source declares more than its quota allows; a collection would be refused rather than cut short"), nil
	case access.Unreadable > 0:
		return failedAccess(access, observewindow.Failed, "the source holds entries this account could not read"), nil
	}
	access.Status, access.RunState = observewindow.Complete, observewindow.Complete.RunState()
	return access, nil
}

// failedAccess records a diagnosis that established that access is not
// available. The counts it reached are kept exactly as they were reached: they
// describe an attempt, and the status says they describe nothing more.
func failedAccess(access Access, status observewindow.Status, reason string) Access {
	access.Status, access.RunState, access.Reason = status, status.RunState(), reason
	return access
}

// bindCredential binds the reference a source declares to this source's own
// purpose and address, reading no value: binding settles what a credential may
// be used for, never what it is. A reference registered for an MLLP endpoint,
// or for another address, is refused here rather than presented to a transfer
// program.
//
// The rotation state is reported against the instant the caller is stamping its
// own record with, so what a record says about a rotation and when it says it
// are the same moment rather than two.
func bindCredential(source Source, at time.Time) CredentialReport {
	if !source.Declared() {
		return CredentialReport{State: CredentialNone, Rotation: string(secret.RotationNotDeclared)}
	}
	report := CredentialReport{State: CredentialRefused, Reference: source.Credential, Rotation: string(secret.RotationNotDeclared)}
	document, err := secret.ReadStore(source.SecretsFile)
	if err != nil {
		report.Reason = "the declared secret reference document could not be read"
		return report
	}
	reference, err := secret.Bind(document, source.Credential, secret.SourceEndpoint, source.Address)
	if err != nil {
		report.Reason = "the declared credential reference is not registered for this source's purpose and address"
		return report
	}
	report.State, report.Rotation = CredentialBound, string(reference.Rotation(at))
	return report
}

// decide asks internal/sendpolicy the one question it owns. Collecting reads a
// source rather than sending to one, and it still opens a connection to an
// address, so the destination is decided by the same rule and a source nobody
// approved is refused for the same reason a send to it would be.
func decide(ctx context.Context, source Source, options Options) sendpolicy.Decision {
	return sendpolicy.Decide(ctx, options.Policy, sendpolicy.Request{
		Address:        source.Address,
		Classification: source.Classification,
		Explicit:       true,
	}, options.resolver())
}

// exceededBy names the declared limits this listing is already past, in the
// order the quota declares them. A collection refuses a source past one rather
// than collecting the part of it that fits.
func (q Quota) exceededBy(entries int, total, largest int64) []string {
	var past []string
	if entries > q.MaxEntries {
		past = append(past, "entries ("+strconv.Itoa(q.MaxEntries)+")")
	}
	if largest > q.MaxEntryBytes {
		past = append(past, "entry bytes ("+strconv.FormatInt(q.MaxEntryBytes, 10)+")")
	}
	if total > q.MaxTotalBytes {
		past = append(past, "total bytes ("+strconv.FormatInt(q.MaxTotalBytes, 10)+")")
	}
	return past
}

// probe establishes that an entry can actually be read now, by reading one byte
// of it and closing it again. Nothing it read is retained.
func probe(ctx context.Context, source Source, entry listedEntry) error {
	stream, err := open(ctx, source, entry)
	if err != nil {
		return err
	}
	defer stream.Close()
	var one [1]byte
	if _, err := stream.Read(one[:]); err != nil && err != io.EOF {
		return errors.New("the source entry could not be read")
	}
	return nil
}

// reservedDirectory reserves a new directory through the one path owner and
// creates it owner-only. The path it creates is the path Destination returned.
func reservedDirectory(path string) (string, error) {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return "", errors.New("cannot create the collection directory; destination must be new and its parent writable")
	}
	return destination, nil
}
