package correlate

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
)

const maxMachineBytes = 32 << 20

func readReviewFile(path string, limit int) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > int64(limit) {
		return nil, errors.New("correlation review requires bounded regular files")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read correlation review")
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.New("correlation review changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read correlation review within its limit")
	}
	return data, nil
}

// ReadReview revalidates both retained documents against the verified evidence
// and freshly reproduced machine report. Unknown, extra, missing, symlinked or
// incomplete members are refused. No historical document is repaired.
func ReadReview(path string, opened *bundle.Bundle, report Report) (ReviewRevision, error) {
	fail := func() (ReviewRevision, error) {
		return ReviewRevision{}, errors.New("correlation review is incomplete, changed or incompatible; reopen the original evidence and rules")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fail()
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 3 {
		return fail()
	}
	machine, err := readReviewFile(filepath.Join(path, "machine.json"), maxMachineBytes)
	if err != nil || !bytes.Equal(machine, reviewBytes(report)) {
		return fail()
	}
	data, err := readReviewFile(filepath.Join(path, "decisions.json"), MaxReviewBytes)
	if err != nil {
		return fail()
	}
	r, err := DecodeReview(data)
	if err != nil {
		return fail()
	}
	seal, err := readReviewFile(filepath.Join(path, "identity.sha256"), 65)
	if err != nil || string(seal) != r.Identity()+"\n" {
		return fail()
	}
	r, _, err = Review(opened, report, &r, false)
	if err != nil {
		return fail()
	}
	return r, nil
}

// SaveReview writes a new owner-readable directory, marking completion last.
// Failed/interrupted writes stay incomplete and are refused. Retrying requires
// a new output name; neither source evidence nor a prior revision is replaced.
func SaveReview(path string, opened *bundle.Bundle, report Report, r ReviewRevision) error {
	if _, _, err := Review(opened, report, &r, false); err != nil {
		return err
	}
	machine := reviewBytes(report)
	if len(machine) > maxMachineBytes {
		return errors.New("machine report exceeds correlation review size limit")
	}
	files := map[string][]byte{"machine.json": machine, "decisions.json": reviewBytes(r)}
	_, err := artifactdir.Write(path, artifactdir.WriteOptions{Completion: []byte(r.Identity() + "\n")}, files)
	if errors.Is(err, artifactdir.ErrCreateDirectory) {
		return errors.New("correlation review destination must be new and writable")
	}
	if err != nil {
		return errors.New("cannot complete correlation review; incomplete directory retained")
	}
	return nil
}
