// Project operations shared by the command line and the desktop shell: the
// composition of verifying a bundle through the shared reader, changing the
// project or editable document through internal/project, and replacing that
// document atomically. Presentation and state mapping stay in the adapters;
// verification and admission rules live here once, so a case the desktop
// registers is the case the command line registers, refused for the same
// reasons.
package operation

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/project"
)

var (
	// ErrProjectWrite reports a document that could not be replaced: a folder
	// this account cannot write, or an interrupted write retained beside the
	// document. The adapter separates the first from the rest by probing.
	ErrProjectWrite = errors.New("project document could not be written")

	// ErrProjectInvalid reports a change the project document cannot hold: an
	// unknown status, an undeclared interface version, a name already
	// registered, a bound exceeded. The sentence wrapped with it is the
	// project package's own refusal.
	ErrProjectInvalid = errors.New("the project refused the change")
)

// InvalidError carries a change the project refused beside the project
// package's own sentence, so an adapter can report the refusal in the
// project's words without parsing an error string.
type InvalidError struct {
	Detail error
}

func (e *InvalidError) Error() string { return ErrProjectInvalid.Error() + ": " + e.Detail.Error() }
func (e *InvalidError) Unwrap() []error {
	return []error{ErrProjectInvalid, e.Detail}
}

// invalidChange wraps one refused change.
func invalidChange(detail error) error {
	return &InvalidError{Detail: detail}
}

// InvalidDetail reports the project's own sentence for a refused change, so a
// caller that already matched ErrProjectInvalid can say what to fix.
func InvalidDetail(err error) (string, bool) {
	var invalid *InvalidError
	if errors.As(err, &invalid) {
		return invalid.Detail.Error(), true
	}
	return "", false
}

// Facts are what a project records about one piece of evidence: the four
// values a verified bundle declared about itself. They travel as one value so
// that four same-typed strings cannot be silently transposed at a call site.
type Facts struct {
	Name       string
	Identity   string
	Schema     string
	Provenance string
}

// VerifiedCase reads those facts from the bundle itself, and the derivation its
// manifest declares when the evidence is the output of a transformation. The
// provenance mode and the derivation are the ones the verified manifest
// carries, so neither is ever inferred from a directory name or supplied by a
// person. The name must be one directory entry of the project.
func VerifiedCase(root, name string) (Facts, string, error) {
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return Facts{}, "", errors.New("a case must be named by one directory entry of the project")
	}
	opened, err := OpenCase(path)
	if err != nil {
		return Facts{}, "", err
	}
	return Facts{
		Name:       name,
		Identity:   opened.Identity,
		Schema:     opened.Manifest.Schema,
		Provenance: string(opened.Manifest.Provenance.Mode),
	}, opened.Manifest.Provenance.Derivation, nil
}

// Evidence states, shared by `readmit project show` and the desktop project
// overview. Verified requires every recorded evidence fact to still hold, so a
// hand-edited document cannot make replaced evidence read as verified.
const (
	EvidenceVerified   = "verified"
	EvidenceChanged    = "changed"
	EvidenceUnreadable = "unreadable"
	EvidenceMissing    = "missing"
)

// EvidenceState re-verifies one registered case or revision through the same
// reader that accepted it, and compares every evidence fact the project
// recorded with what the reader reported. What the project recorded is
// reported exactly as recorded whatever this finds: a caller reports, and
// never rewrites what a project recorded.
func EvidenceState(root string, recorded Facts) string {
	path, err := artifactpath.Child(root, recorded.Name)
	if err != nil {
		return EvidenceMissing
	}
	opened, err := OpenCase(path)
	if err != nil {
		return EvidenceUnreadable
	}
	if opened.Identity != recorded.Identity || opened.Manifest.Schema != recorded.Schema ||
		string(opened.Manifest.Provenance.Mode) != recorded.Provenance {
		return EvidenceChanged
	}
	return EvidenceVerified
}

// openProjectDocuments reads both documents of one project directory: the
// recorded one and the editable one beside it. Every project operation needs
// both, because registration keeps the two sides disjoint by name and identity.
func openProjectDocuments(path string) (string, project.Document, project.Revisions, error) {
	opened, err := project.Open(path)
	if err != nil {
		return "", project.Document{}, project.Revisions{}, fmt.Errorf("%w: %w", ErrProjectOpen, err)
	}
	revisions, err := project.ReadRevisions(opened.Root)
	if err != nil {
		return "", project.Document{}, project.Revisions{}, fmt.Errorf("%w: %w", ErrProjectRevisions, err)
	}
	return opened.Root, opened.Document, revisions, nil
}

// saveProject replaces the project document and reports a refused write as
// ErrProjectWrite, so an adapter can separate a folder this account cannot
// write from a change the document refused.
func saveProject(root string, document project.Document) error {
	if err := project.WriteDocument(root, document); err != nil {
		return fmt.Errorf("%w: %v", ErrProjectWrite, err)
	}
	return nil
}

// SettingsChange is one project-settings edit. A member left nil is left
// exactly as it was; DeclareVersions names further interface versions to
// declare, and a version already declared is left as it was. A declared
// version is never removed, because registered cases still name it.
type SettingsChange struct {
	Title                   *string
	DefaultOwner            *string
	DefaultInterfaceVersion *string
	DeclareVersions         []string
}

// UpdateProjectSettings changes the project-level settings through the shared
// project document, and returns the document exactly as it was stored. A
// change that names nothing stores the document exactly as it was, which is
// how an adapter forwards only the members its form explicitly filled.
func UpdateProjectSettings(path string, change SettingsChange) (project.Document, error) {
	root, document, _, err := openProjectDocuments(path)
	if err != nil {
		return project.Document{}, err
	}
	if change.Title != nil {
		document.Settings.Title = *change.Title
	}
	if change.DefaultOwner != nil {
		document.Settings.DefaultOwner = *change.DefaultOwner
	}
	for _, version := range change.DeclareVersions {
		if !document.Declares(version) {
			document.InterfaceVersions = append(append([]string{}, document.InterfaceVersions...), version)
		}
	}
	if change.DefaultInterfaceVersion != nil {
		document.Settings.DefaultInterfaceVersion = *change.DefaultInterfaceVersion
	}
	if err := project.Validate(document); err != nil {
		return project.Document{}, invalidChange(err)
	}
	if err := saveProject(root, document); err != nil {
		return project.Document{}, err
	}
	return document, nil
}

// CaseRegistration is one case to register, with the metadata a person
// maintains. Name is the directory entry of the project holding the bundle;
// the evidence facts are read from the bundle itself and are never taken from
// this registration. A member left at its zero value inherits the project
// default, exactly as the command line leaves a flag absent.
type CaseRegistration struct {
	Title            string
	Owner            string
	Status           project.Status
	InterfaceVersion string
	Tags             []string
	Incidents        []string
}

// RegisterCase verifies one case bundle of the project through the shared
// reader and registers what that reader accepted. Derived evidence is refused,
// because registering it as a case would lose the parent identity and the
// operation that produced it; it is registered as a revision instead. The
// stored entry is returned exactly as it was written.
func RegisterCase(path, name string, registration CaseRegistration) (project.Case, error) {
	root, document, revisions, err := openProjectDocuments(path)
	if err != nil {
		return project.Case{}, err
	}
	facts, _, err := VerifiedCase(root, name)
	if err != nil {
		return project.Case{}, err
	}
	entry := project.Case{
		Name:             facts.Name,
		Identity:         facts.Identity,
		Schema:           facts.Schema,
		Provenance:       facts.Provenance,
		Title:            registration.Title,
		InterfaceVersion: registration.InterfaceVersion,
		Owner:            registration.Owner,
		Status:           registration.Status,
		Tags:             registration.Tags,
		Incidents:        registration.Incidents,
	}
	updated, stored, err := project.AddCase(document, revisions, entry)
	if err != nil {
		return project.Case{}, invalidChange(err)
	}
	if err := saveProject(root, updated); err != nil {
		return project.Case{}, err
	}
	return stored, nil
}

// RegisterRevision registers one derived case bundle as a revision of a case
// or revision the project already holds. Both the revision and its parent are
// re-verified through the shared reader before any lineage is recorded, and
// the operation manifest is read from the evidence itself, so a transformation
// can never be relabelled and a parent whose evidence has been replaced is
// refused rather than silently re-identified under a new revision.
func RegisterRevision(path, name, parent string) (project.Revision, error) {
	root, document, revisions, err := openProjectDocuments(path)
	if err != nil {
		return project.Revision{}, err
	}
	facts, derivation, err := VerifiedCase(root, name)
	if err != nil {
		return project.Revision{}, err
	}
	ancestor, _, err := VerifiedCase(root, parent)
	if err != nil {
		return project.Revision{}, err
	}
	updated, stored, err := project.AddRevision(document, revisions, project.Revision{
		Name:       facts.Name,
		Identity:   facts.Identity,
		Schema:     facts.Schema,
		Provenance: facts.Provenance,
		Operation: project.Operation{
			Name:           derivation,
			Parent:         ancestor.Name,
			ParentIdentity: ancestor.Identity,
		},
	})
	if err != nil {
		return project.Revision{}, invalidChange(err)
	}
	if err := project.WriteRevisions(root, updated); err != nil {
		return project.Revision{}, fmt.Errorf("%w: %v", ErrProjectWrite, err)
	}
	return stored, nil
}

// CaseChange is the mutable metadata of one registered case. A member left nil
// is left exactly as it was, which is how an update can change a status
// without restating the tags. No member can reach the recorded evidence facts.
type CaseChange struct {
	Title            *string
	Owner            *string
	Status           *project.Status
	InterfaceVersion *string
	Tags             *[]string
	Incidents        *[]string
}

// Empty reports whether the change would change nothing.
func (c CaseChange) Empty() bool { return c == CaseChange{} }

// UpdateRegisteredCase replaces the mutable metadata of one registered case
// and returns the entry exactly as it was stored. The name, identity, contract
// version and provenance of the evidence stay as recorded.
func UpdateRegisteredCase(path, name string, change CaseChange) (project.Case, error) {
	if change.Empty() {
		return project.Case{}, fmt.Errorf("%w: the change names nothing", ErrProjectInvalid)
	}
	root, document, _, err := openProjectDocuments(path)
	if err != nil {
		return project.Case{}, err
	}
	updated, stored, err := project.UpdateCase(document, name, project.Change{
		Title:            change.Title,
		Owner:            change.Owner,
		Status:           change.Status,
		InterfaceVersion: change.InterfaceVersion,
		Tags:             change.Tags,
		Incidents:        change.Incidents,
	})
	if err != nil {
		return project.Case{}, invalidChange(err)
	}
	if err := saveProject(root, updated); err != nil {
		return project.Case{}, err
	}
	return stored, nil
}
