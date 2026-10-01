package profilepackage

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/artifactdir"
	"slices"
)

var importFamily = artifactdir.Family{
	Layout: artifactdir.Layout{Noun: "profile import", RequiredFiles: []string{"profile.json", "pack.json", "version.json", "origin.json", "package.json"}, AllowFile: func(name string) bool {
		return slices.Contains([]string{"profile.json", "pack.json", "version.json", "origin.json", "package.json"}, name)
	}, MaxFiles: 5, MaxFileBytes: MaxBytes, MaxBytes: 2 * MaxBytes},
	Seal: artifactdir.CompletionRecord("package.json", ".package.json.incomplete"),
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot create profile import directory; destination must be new and parent writable"),
		Open:    errors.New("cannot open profile import directory; incomplete import retained"),
		Create:  errors.New("cannot create profile document; incomplete import retained"),
		Write:   errors.New("cannot write profile document; incomplete import retained"),
		Sync:    errors.New("cannot sync profile import directory; the import was written in full but a power loss could still lose it"),
	},
}

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
	if err = ctx.Err(); err != nil {
		return err
	}
	writer, err := artifactdir.Create(destination, importFamily, artifactdir.Durable)
	if err != nil {
		return err
	}
	defer writer.Close()
	for _, name := range []string{"profile.json", "pack.json", "version.json", "origin.json"} {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err := writer.WriteFile(name, files[name]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = writer.Seal(canonical)
	return err
}
