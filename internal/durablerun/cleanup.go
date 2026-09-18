package durablerun

import (
	"errors"
	"os"
	"slices"
)

// CleanupSchema reports what one cleanup removed and what it retained.
const CleanupSchema = "readmit-run-cleanup/v1"

type Cleanup struct {
	Schema   string   `json:"schema"`
	Run      Summary  `json:"run"`
	Removed  []string `json:"removed"`
	Retained []string `json:"retained"`
}

// evidenceEntries are the entries a job directory may hold. Every one of them
// except the lease is evidence and is never removed: the plan and intended
// bytes say what was meant, the sent prefixes and journal say what happened,
// and the result holds the replay's own record and the destination decision.
var evidenceEntries = []string{"plan.json", "intended", "journal.jsonl", "sent", "result", "result.decision.json"}

// Clean removes what a terminal run no longer needs, which is only a lease its
// writer could not release. It verifies the job first and refuses to change
// anything when the run recorded no completion, because the writer may still
// hold that lease, and when the directory holds an entry this package did not
// write, because nothing there is known to be safe. Evidence is never removed.
func Clean(path string) (Cleanup, error) {
	recovery, _, err := readJob(path)
	if err != nil {
		return Cleanup{}, err
	}
	root, _, err := openJob(path)
	if err != nil {
		return Cleanup{}, err
	}
	defer root.Close()
	names, err := os.ReadDir(root.Name())
	if err != nil {
		return Cleanup{}, errors.New("cannot list durable run; nothing was removed")
	}
	cleanup := Cleanup{Schema: CleanupSchema, Run: recovery.Run, Removed: []string{}, Retained: []string{}}
	stale := false
	for _, name := range names {
		switch {
		case slices.Contains(evidenceEntries, name.Name()):
			cleanup.Retained = append(cleanup.Retained, name.Name())
		case name.Name() == "lease.json":
			stale = true
		default:
			return Cleanup{}, errors.New("durable run holds an entry this release did not write; nothing was removed")
		}
	}
	if !recovery.Terminal {
		return Cleanup{}, errors.New("completion was not recorded; the writer may still hold its lease and nothing was removed")
	}
	if stale {
		if err := root.Remove("lease.json"); err != nil {
			return Cleanup{}, errors.New("cannot remove the stale durable lease; evidence is unchanged")
		}
		cleanup.Removed = append(cleanup.Removed, "lease.json")
	}
	slices.Sort(cleanup.Retained)
	return cleanup, nil
}
