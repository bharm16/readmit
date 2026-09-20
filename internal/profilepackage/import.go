package profilepackage

import (
	"context"
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// Import validates before writing a new private directory. package.json is
// written last as the completion record. Failure/cancellation leaves an
// explicitly incomplete directory; retry uses a new destination, never a merge
// into an existing profile library or project.
func Import(ctx context.Context, destination string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := Decode(data)
	if err != nil {
		return err
	}
	files, err := p.Documents()
	if err != nil {
		return err
	}
	canonical, err := p.encode()
	if err != nil {
		return err
	}
	files["package.json"] = canonical
	destination, err = artifactpath.Destination(destination)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return errors.New("cannot create profile import directory; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return errors.New("cannot open profile import directory; incomplete import retained")
	}
	defer root.Close()
	for _, name := range []string{"profile.json", "pack.json", "version.json", "origin.json", "package.json"} {
		if err = ctx.Err(); err != nil {
			return err
		}
		file, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errors.New("cannot create profile document; incomplete import retained")
		}
		_, e = file.Write(files[name])
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return errors.New("cannot write profile document; incomplete import retained")
		}
	}
	return nil
}
