package index

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// Write creates one new index file at a reserved destination and returns the
// path it actually wrote. artifactpath owns where an index may go: it refuses a
// destination inside retained case, run, result, review or report evidence, and
// one reached through a symbolic link, so an index can never be written into
// the evidence it describes. Creation is exclusive, so an existing index is
// never overwritten in place; replacing one is deleting it and building again.
//
// A failed write removes its own partial file rather than leaving something a
// later read would have to guess about.
func Write(destination string, document Document) (string, error) {
	data, err := Encode(document)
	if err != nil {
		return "", err
	}
	path, err := artifactpath.Destination(destination)
	if err != nil {
		return "", err
	}
	if err := indexFile.Create(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// indexFile is how an index is created and read, through the shared document
// store. A read follows a link at its name.
var indexFile = artifactdir.Document{
	MaxBytes: MaxIndexBytes,
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("an index must be a readable regular file"),
		Open:      errors.New("cannot open index"),
		Changed:   errors.New("an index must be a regular file"),
		Read:      errors.New("cannot read index"),
		Size:      errors.New("index exceeds its size limit"),
	},
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("cannot create index; destination must be new and parent writable"),
		Write:  errors.New("cannot write index"),
	},
}

// Open reads one bounded, regular index file. A file past the size limit is
// refused before it is decoded rather than read into memory first.
func Open(path string) (Document, error) {
	data, err := indexFile.Read(path)
	if err != nil {
		return Document{}, err
	}
	return Decode(data)
}
