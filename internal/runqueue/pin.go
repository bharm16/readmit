package runqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablerun"
)

// PinnedJob is one test of a prepared suite as a schedule runs it: its queue
// id, the spec it executes, the jobs that must pass first and the input
// identity its run is pinned to.
type PinnedJob struct {
	ID    string
	Spec  string
	After []string
	Input string
}

// ErrUnpinned reports a prepared suite whose queue or any test in it
// does not prepare on this host.
var ErrUnpinned = errors.New("the prepared suite cannot be read on this host")

// PinnedJobs reads a prepared suite's queue.json and prepares every test in
// it, answering one identity over the ordered jobs and their input
// identities, and the jobs. The same bytes answer the same identity on the
// machine that prepared the suite and on the runner host that runs it.
func PinnedJobs(ctx context.Context, queuePath string) (string, []PinnedJob, error) {
	raw, err := artifactdir.Document{MaxBytes: 1 << 20}.Read(queuePath)
	if err != nil {
		return "", nil, ErrUnpinned
	}
	plan, err := DecodePlan(raw)
	if err != nil {
		return "", nil, ErrUnpinned
	}
	hash := sha256.New()
	jobs := make([]PinnedJob, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		spec := filepath.Join(filepath.Dir(queuePath), job.Spec)
		prepared, err := durablerun.Prepare(spec)
		if err != nil {
			return "", nil, ErrUnpinned
		}
		identity, err := prepared.InputIdentity()
		if err != nil {
			return "", nil, ErrUnpinned
		}
		hash.Write([]byte(job.ID + "\x00" + identity + "\n"))
		jobs = append(jobs, PinnedJob{ID: job.ID, Spec: spec, After: job.After, Input: identity})
	}
	return hex.EncodeToString(hash.Sum(nil)), jobs, nil
}
