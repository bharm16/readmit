package operation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// ErrSampleFixtureUnavailable and ErrSampleFixtureChanged refuse a sample
// capture before anything is written: a fixture that is not there, and one
// that is not the pinned bytes. Their words are the ones `readmit sample
// capture` has always printed.
var (
	ErrSampleFixtureUnavailable = errors.New("frozen sample fixture unavailable")
	ErrSampleFixtureChanged     = errors.New("sample requires the unchanged frozen synthetic fixtures")
)

// sampleFixtures are the two frozen synthetic receiver fixtures the sample
// capture imports, each pinned to its SHA-256, in the order they become the
// case's sources: the booking, then its reschedule.
var sampleFixtures = []struct{ name, hash string }{
	{"listen-s12.hl7", "cfb563097687c8b1a5a273a68243648f9d25f42ee8a10516ab6df850fca3e521"},
	{"listen-s13.hl7", "291757d6252dc5544e29f8958bdf98abac643ae61804852f617f5708f0f408bb"},
}

// CaptureSample imports the two frozen synthetic receiver fixtures found in
// the fixtures folder as one imported case at output, recorded as imported at
// the instant given. It is the ungated frozen walkthrough `readmit sample
// capture` runs and the window's guided sample offers, not an admission
// override: the fixture bytes are pinned here, and a folder holding any other
// bytes under those names is refused before anything is written. The bytes
// checked are the exact bytes handed to the writer, so a file changed after
// the check cannot win a re-read race.
func CaptureSample(fixtures, output string, at time.Time) (*bundle.Bundle, error) {
	inputs := make([]bundle.Input, 0, len(sampleFixtures))
	for _, fixture := range sampleFixtures {
		path, err := artifactpath.Resolve(filepath.Join(fixtures, fixture.name))
		if err != nil {
			return nil, ErrSampleFixtureUnavailable
		}
		data, err := ReadInputFile(path, 4096)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != fixture.hash {
			return nil, ErrSampleFixtureChanged
		}
		inputs = append(inputs, bundle.Input{Path: path, Data: data, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.CR}})
	}
	return bundle.Write(output, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &at})
}
