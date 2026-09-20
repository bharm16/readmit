package desktop

import (
	"io"
	"os"

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
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > sequenceanalysis.MaxBytes {
		return nil, refusal{Failed, "sequence analysis must be one regular workspace file of at most 1 MiB"}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, refusal{Failed, "sequence analysis could not be read"}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, sequenceanalysis.MaxBytes+1))
	if err != nil {
		return nil, refusal{Failed, "sequence analysis could not be read"}
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
