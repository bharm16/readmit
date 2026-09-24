package desktop

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
)

func analyzeSequence(root, name string, b *bundle.Bundle, links *correlate.Report) (*sequenceanalysis.Report, refusal) {
	err := artifactpath.EntryName(name)
	path := artifactpath.JoinReference(root, name)
	if err != nil {
		return nil, refusal{Failed, "sequence analysis must be one regular workspace file"}
	}
	data, err := sequenceFile.Read(path)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	declaration, err := sequenceanalysis.Parse(data)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	report, err := sequenceanalysis.Evaluate(b, declaration, links)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	return report, refusal{}
}

// sequenceFile is how a sequence analysis declaration of the workspace is
// read: never through a link, and never past its bound.
var sequenceFile = artifactdir.Document{
	MaxBytes: sequenceanalysis.MaxBytes,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("sequence analysis must be one regular workspace file of at most 1 MiB"),
		Read:      errors.New("sequence analysis could not be read"),
		Size:      errors.New("sequence analysis must be one regular workspace file of at most 1 MiB"),
	},
}
