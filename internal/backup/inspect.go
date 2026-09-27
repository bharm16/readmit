package backup

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// ErrNotReviewed reports a backup that is no longer the one a caller
// identified: its completion marker seals a different manifest now.
var ErrNotReviewed = errors.New("the backup is not the one that was reviewed; nothing was deleted")

// errUnrecorded refuses a removal of a backup that holds an entry its manifest
// does not record.
var errUnrecorded = errors.New("the backup holds a file its document does not record; nothing was deleted")

// Inspect reads what one backup declares without reading what it stores: the
// completion marker, the seal it holds over the manifest, and the manifest
// itself, answered with the identity the marker records. It is the cheap
// reading a list of backups is made from. A stored file changed after the
// backup was sealed is found only by Verify, which reads every byte, so Inspect
// never stands in for verifying a backup before it is restored.
func Inspect(backupPath string) (Document, string, error) {
	root, err := artifactpath.Directory(backupPath)
	if err != nil {
		return Document{}, "", errors.New("a backup must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return Document{}, "", errors.New("cannot open the backup directory")
	}
	defer opened.Close()
	return sealed(opened)
}

// Remove deletes one backup: exactly the files its manifest records, the
// directories that hold them, the manifest and the marker, and then the
// backup's own directory. identity is the marker's digest the caller
// identified the backup by, and a backup sealed over anything else is refused.
// So is a backup holding any entry its manifest does not record — a file a
// person put beside the stored ones, a link, a device — before anything is
// removed: a removal never deletes what the backup does not stand behind.
//
// The marker is removed first, so a removal that stops part way leaves a
// directory that reads as an incomplete backup rather than a usable one.
// Removing is unlinking; it is not secure erasure.
func Remove(backupPath, identity string) error {
	root, err := artifactpath.Directory(backupPath)
	if err != nil {
		return errors.New("a backup must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return errors.New("cannot open the backup directory")
	}
	defer opened.Close()
	document, found, err := sealed(opened)
	if err != nil {
		return err
	}
	if found != identity {
		return ErrNotReviewed
	}
	listed := map[string]bool{DocumentName: true, MarkerName: true}
	folders := map[string]bool{FilesDirectory: true}
	for _, file := range document.Files {
		name := FilesDirectory + "/" + file.Path
		listed[name] = true
		for parent := path.Dir(name); parent != FilesDirectory; parent = path.Dir(parent) {
			folders[parent] = true
		}
	}
	var files, directories []string
	err = fs.WalkDir(opened.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return errors.New("cannot read the backup; nothing was deleted")
		case name == ".":
			return nil
		case entry.IsDir() && folders[name]:
			directories = append(directories, name)
			return nil
		case entry.Type() == 0 && listed[name]:
			if name != MarkerName && name != DocumentName {
				files = append(files, name)
			}
			return nil
		}
		return errUnrecorded
	})
	if err != nil {
		return err
	}
	incomplete := errors.New("backup deletion incomplete; what remains no longer carries its completion marker and reads as an incomplete backup")
	if err := opened.Remove(MarkerName); err != nil {
		return errors.New("cannot delete the backup; nothing was deleted")
	}
	for _, name := range files {
		if err := opened.Remove(name); err != nil {
			return incomplete
		}
	}
	// A walk lists a directory before what it holds, so the reverse removes
	// every directory after its contents.
	slices.Reverse(directories)
	for _, name := range directories {
		if err := opened.Remove(name); err != nil {
			return incomplete
		}
	}
	if err := opened.Remove(DocumentName); err != nil {
		return incomplete
	}
	if err := os.Remove(root); err != nil {
		return incomplete
	}
	return nil
}
