package desktop

import (
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/profilepack"
)

// ProfileLibraryRow reports one published combination of the support matrix.
type ProfileLibraryRow struct {
	HL7Version string `json:"hl7_version"`
	Family     string `json:"family"`
	profilepack.Outcomes
	Pack profilepack.Identity `json:"pack"`
}

// writeWorkspaceEntry writes data as one new workspace entry name.
func writeWorkspaceEntry(root, name string, data []byte) error {
	if err := artifactpath.EntryName(name); err != nil {
		return errors.New("destination must be one regular entry of the open workspace")
	}
	dest, err := artifactpath.Destination(filepath.Join(root, name))
	if err != nil {
		return err
	}
	err = newWorkspaceEntry.Create(dest, data)
	switch {
	case errors.Is(err, errEntryUncreated) && errors.Is(err, fs.ErrExist):
		return errors.New("cannot create destination; file already exists in workspace")
	case errors.Is(err, errEntryUncreated) && errors.Is(err, fs.ErrPermission):
		return fs.ErrPermission
	}
	return err
}

// errEntryUncreated is a new workspace entry that could not be created.
var errEntryUncreated = errors.New("cannot create destination file")

// newWorkspaceEntry is how a save that never overwrites creates its entry,
// through the shared document store.
var newWorkspaceEntry = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errEntryUncreated,
		Write:  errors.New("cannot write destination file"),
	},
}
