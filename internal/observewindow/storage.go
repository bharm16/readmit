package observewindow

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// ReadWindow opens one declared observation window. Like every other input
// reader here, it accepts a regular file only: a window that came from a pipe
// or a device is not a document an operator selected.
func ReadWindow(path string) (Window, error) {
	data, err := windowDocument.Read(path)
	if err != nil {
		return Window{}, err
	}
	return DecodeWindow(data)
}

// ReadCompletion opens one retained completion record. What it returns is
// evidence of what a collector observed; deciding whether those observations
// support the verdict is Window.Verify's job, and a caller holding the declared
// window is expected to ask.
func ReadCompletion(path string) (Completion, error) {
	data, err := completionDocument.Read(path)
	if err != nil {
		return Completion{}, err
	}
	return DecodeCompletion(data)
}

// WriteWindow records one declared observation window. It validates before
// writing, so a window readmit could not read back is never produced, and it
// replaces the document through the shared document store, so a reader never
// observes a partial document and a failed write leaves the previous one
// exactly as it was.
func WriteWindow(path string, w Window) error {
	data, err := EncodeWindow(w)
	if err != nil {
		return err
	}
	return windowDocument.Replace(path, append(data, '\n'))
}

// WriteCompletion retains one completion at a new destination. A completion is
// evidence, so the destination must not already exist: a window's verdict is
// never rewritten in place, and a second run writes a second record beside the
// first rather than replacing it.
func WriteCompletion(path string, c Completion) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	data, err := EncodeCompletion(c)
	if err != nil {
		return err
	}
	return completionDocument.Create(destination, data)
}

// windowDocument and completionDocument are how the two documents are written
// and read. A document a person names is read through a link at its name, as
// it always has been, and its diagnostics name what it was reading and no
// path. The store inspects the name before opening it, so a pipe never blocks
// the read, and checks the opened file again, because the path can change
// underneath it.
var (
	windowDocument = artifactdir.Document{
		MaxBytes: MaxWindowBytes,
		Links:    artifactdir.FollowLinks,
		Errors: artifactdir.DocumentErrors{
			Destination: errors.New("cannot write an observation window here"),
			Create:      errors.New("cannot create the new observation window; an interrupted write is retained"),
			Write:       errors.New("cannot write the new observation window"),
			Install:     errors.New("cannot replace the observation window"),
		},
		Refusals: refusals("observation window"),
	}
	completionDocument = artifactdir.Document{
		MaxBytes: MaxCompletionBytes,
		Links:    artifactdir.FollowLinks,
		Errors: artifactdir.DocumentErrors{
			Create: errors.New("cannot create observation completion; destination must be new and writable"),
			Write:  errors.New("cannot write observation completion"),
		},
		Refusals: refusals("observation completion"),
	}
)

// refusals are the sentences a read of one kind of document reports.
func refusals(kind string) artifactdir.DocumentRefusals {
	return artifactdir.DocumentRefusals{
		Irregular: errors.New("an " + kind + " must be a readable regular file"),
		Open:      errors.New("cannot open " + kind + " file"),
		Changed:   errors.New("an " + kind + " must be a regular file"),
		Read:      errors.New("cannot read " + kind + " file"),
		Size:      errors.New(kind + " exceeds size limit"),
	}
}
