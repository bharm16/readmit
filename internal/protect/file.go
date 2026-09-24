package protect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"
)

// File is the protection document at one path, and the only way a document is
// changed. Every operation reads the document afresh and writes a change back
// atomically before it returns, so the command line and the desktop window each
// make one call here per action, and what a new control starts as, when a
// rotation may be recorded and which control a package is written or opened
// under are decided once, here, for both of them.
type File struct {
	// Path is where the document is read and written.
	Path string
	// AbsentIsEmpty reads a path where no document exists yet as the empty
	// document the first registration writes, as the desktop window reads the
	// entry its protection screen names. Without it every operation but
	// Register refuses such a path, as the command line refuses a --protection
	// file that does not exist. Register starts the first document either way.
	AbsentIsEmpty bool
}

// Step is the part of a file-level operation a refusal came from. A refusal's
// sentence is its part's own, and the command line prints it unchanged; the
// desktop window words some parts in sentences of its own, and tells them apart
// here rather than by matching a sentence.
type Step int

const (
	// DocumentStep reads the document, finds the control an operation names
	// and writes a change back. It is every refusal no other step claims.
	DocumentStep Step = iota
	// KeyStep asks the control's declared store to answer before a rotation
	// is recorded. A key read to write or open a package is PackageStep's.
	KeyStep
	// SourcesStep reads the files and directories a package is packed from.
	SourcesStep
	// DescriptorStep reads a package's descriptor to learn the control that
	// wrote it, when no control was named.
	DescriptorStep
	// PackageStep writes or opens the package under the control's key.
	PackageStep
)

type stepRefusal struct {
	step Step
	err  error
}

func (r stepRefusal) Error() string { return r.err.Error() }
func (r stepRefusal) Unwrap() error { return r.err }

// StepOf reports the step of a file-level operation that refused with err.
func StepOf(err error) Step {
	var refused stepRefusal
	if errors.As(err, &refused) {
		return refused.step
	}
	return DocumentStep
}

// errUnanswered refuses a rotation whose declared store did not answer. Nothing
// was recorded, and the sentence says so.
var errUnanswered = errors.New("the key did not resolve from its declared store; the recorded rotation is unchanged")

// Read reads the document and every control it registers.
func (f File) Read() (Document, error) { return f.read(f.AbsentIsEmpty) }

func (f File) read(absentIsEmpty bool) (Document, error) {
	if absentIsEmpty {
		if _, err := os.Lstat(f.Path); errors.Is(err, fs.ErrNotExist) {
			return Document{Schema: Schema}, nil
		}
	}
	return readDocument(f.Path)
}

// Register adds a new control and writes the document, starting the first one
// where none exists yet. A new control is active at generation 1, recorded as
// rotated at the given instant to the second in UTC; the entry's own state,
// generation and rotation time are not read. Registering reads no key: a
// control is a reference, and registering one proves nothing about the store
// behind it. A path that holds anything but a readable document of this
// contract is reported, never replaced.
func (f File) Register(entry Control, at time.Time) (Document, Control, error) {
	document, err := f.read(true)
	if err != nil {
		return Document{}, Control{}, err
	}
	return f.save(register(document, entry, at))
}

// Rotate records that the operator replaced the key behind a control in its own
// store. The declared store must answer for the control before anything is
// recorded, so a generation is never recorded against a key readmit cannot read;
// until it answers the document is left exactly as it was. The material it
// printed is discarded, and readmit never read the previous key, so what this
// records is the operator's assertion, not a verification.
func (f File) Rotate(ctx context.Context, name string, at time.Time) (Document, Control, error) {
	document, err := f.Read()
	if err != nil {
		return Document{}, Control{}, err
	}
	entry, err := Find(document, name)
	if err != nil {
		return Document{}, Control{}, err
	}
	if _, err := ReadKey(ctx, entry); err != nil {
		return Document{}, Control{}, stepRefusal{KeyStep, errUnanswered}
	}
	return f.save(rotate(document, name, at))
}

// Retire stops a control writing new packages and writes the document. It still
// opens the packages it wrote: retirement is not revocation.
func (f File) Retire(name string) (Document, Control, error) {
	document, err := f.Read()
	if err != nil {
		return Document{}, Control{}, err
	}
	return f.save(retire(document, name))
}

// save writes a changed document back and returns it with the control it
// changed, or writes nothing when the change was refused.
func (f File) save(updated Document, stored Control, err error) (Document, Control, error) {
	if err != nil {
		return Document{}, Control{}, err
	}
	if err := writeDocument(f.Path, updated); err != nil {
		return Document{}, Control{}, err
	}
	return updated, stored, nil
}

// Pack writes a new encrypted transfer package from the named files and
// directories under a control the document registers as active, and returns its
// descriptor and the count of entries that were not regular files and so were
// not read. Nothing is read until the control admits a new package.
func (f File) Pack(ctx context.Context, name string, roots []string, output string, at time.Time) (Package, int, error) {
	document, err := f.Read()
	if err != nil {
		return Package{}, 0, err
	}
	entry, err := Writable(document, name)
	if err != nil {
		return Package{}, 0, err
	}
	sources, notRead, err := Collect(roots)
	if err != nil {
		return Package{}, 0, stepRefusal{SourcesStep, err}
	}
	descriptor, err := Pack(ctx, entry, sources, notRead, output, at)
	if err != nil {
		return Package{}, 0, stepRefusal{PackageStep, err}
	}
	return descriptor, notRead, nil
}

// Open decrypts a package into a new directory under a control the document
// registers: the one named, or the control the package's own descriptor names
// when none is, so an operator need not repeat it. Naming a control other than
// the one that wrote the package is refused by the open itself, and a retired
// control still opens what it wrote.
func (f File) Open(ctx context.Context, name, source, output string) (Package, Index, error) {
	document, err := f.Read()
	if err != nil {
		return Package{}, Index{}, err
	}
	if name == "" {
		descriptor, _, err := ReadPackage(source)
		if err != nil {
			return Package{}, Index{}, stepRefusal{DescriptorStep, err}
		}
		name = descriptor.Control
	}
	entry, err := Find(document, name)
	if err != nil {
		return Package{}, Index{}, err
	}
	descriptor, index, err := Open(ctx, entry, source, output)
	if err != nil {
		return Package{}, Index{}, stepRefusal{PackageStep, err}
	}
	return descriptor, index, nil
}
