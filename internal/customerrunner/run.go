package customerrunner

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// Job is operator-selected data, never a shell command. Paths are private local
// input; output is always a fresh child of the configured private runner root.
type Job struct {
	Schema string `json:"schema"`
	ID     string `json:"id"`
	Spec   string `json:"spec"`
}

func ReadJob(path string) (Job, error) {
	var job Job
	raw, err := privateRead(path, 16384)
	if err != nil || json.Unmarshal(raw, &job, json.RejectUnknownMembers(true)) != nil {
		return job, ErrRefused
	}
	return job, ValidateJob(job)
}

// ValidateJob is the document's exact rule set: the contract name, a job id
// the admission protocol accepts, and one absolute spec path. ReadJob applies
// it to file bytes (whose member set was already checked exactly); the
// application applies it to generated documents before anything is written.
func ValidateJob(job Job) error {
	if job.Schema != "readmit-runner-job/v1" || !runnerprotocol.ID(job.ID) || !filepath.IsAbs(job.Spec) {
		return ErrRefused
	}
	return nil
}

// Status is a read-only local snapshot. A lease is not evidence the process is
// alive; expired or unreadable leases need operator recovery and never reassign.
type Status struct {
	Schema string `json:"schema"`
	State  string `json:"state"`
	Jobs   int    `json:"jobs"`
}

func Health(root string) (Status, error) {
	s := Status{Schema: "readmit-runner-status/v1", State: "idle"}
	jobs, err := Jobs(root)
	if err != nil {
		return s, ErrRefused
	}
	s.Jobs = len(jobs)
	if _, err := os.Lstat(filepath.Join(root, ".active")); err == nil {
		s.State = "recovery_required"
		raw, err := privateRead(filepath.Join(root, ".active", "lease.json"), 4096)
		if err == nil {
			lease, err := runnerprotocol.DecodeLease(raw)
			if err == nil && time.Now().Before(lease.Expires) {
				s.State = "lease_current"
			}
		}
	} else if !os.IsNotExist(err) {
		return s, ErrRefused
	}
	return s, nil
}

// persist creates one new runner record, such as a job's claim, through the
// shared document store.
func persist(path string, data []byte) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return ErrRefused
	}
	defer root.Close()
	return runnerFile.CreateIn(root, filepath.Base(path), data)
}

func syncDirectory(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return ErrRefused
	}
	defer root.Close()
	return artifactdir.SyncDirectory(root, ".")
}

// storeLease replaces the active lease through the shared document store,
// staged as lease.next.
func storeLease(active string, lease runnerprotocol.Lease) error {
	raw, err := json.Marshal(lease)
	if err != nil {
		return ErrRefused
	}
	root, err := os.OpenRoot(active)
	if err != nil {
		return ErrRefused
	}
	defer root.Close()
	return leaseFile.ReplaceIn(root, "lease.json", raw)
}

// runnerFile is how a runner record is created, and leaseFile how the active
// lease is replaced. A record whose write failed is retained and refuses the
// next write, and every failure is ErrRefused.
var (
	runnerFile = artifactdir.Document{
		RetainFailed: true,
		Errors:       artifactdir.DocumentErrors{Create: ErrRefused, Write: ErrRefused, Sync: ErrRefused},
	}
	leaseFile = artifactdir.Document{
		Staging:      artifactdir.StagingName("lease.next"),
		RetainFailed: true,
		Errors:       artifactdir.DocumentErrors{Create: ErrRefused, Write: ErrRefused, Sync: ErrRefused},
	}
)

// Run acquires one exclusive environment lease within this configured root. A
// crash leaves the claim intact. Neither restart nor lease expiry replays work.
func Run(ctx context.Context, c Config, job Job) (durablerun.Summary, error) {
	return run(ctx, c, job, nil, remote{c}, hostClock{})
}

// RunPinned refuses changed prepared inputs before any execution. The same
// prepared plan whose identity was checked performs the run.
func RunPinned(ctx context.Context, c Config, job Job, inputIdentity string) (durablerun.Summary, error) {
	return run(ctx, c, job, &inputIdentity, remote{c}, hostClock{})
}

// run is one job, admitted under whatever admission the caller's operation
// carries in ctx (operationguard.RunJob): its own bounded, settled execution
// for a runner that serves job after job, or a recheck under an execution
// its operation already holds. A context that carries none refuses it. A
// pinned job's inputs are compared with its pin first, before anything is
// admitted. The hub admits it and the clock times its lease.
func run(ctx context.Context, c Config, job Job, pin *string, h Hub, clock Clock) (summary durablerun.Summary, err error) {
	var p Preflight
	if pin != nil {
		if ValidateJob(job) != nil {
			return summary, ErrRefused
		}
		if p, err = prepare(job, pin); err != nil {
			return summary, err
		}
	}
	err = operationguard.RunJob(ctx, func(ctx context.Context) (jobErr error) {
		summary, jobErr = runAdmitted(ctx, c, job, p, h, clock)
		return jobErr
	})
	return summary, err
}

// runAdmitted holds the environment for one admitted job: the root's
// exclusive claim, the hub's lease claimed, renewed every second and released,
// and the job stopped irreversibly when a renewal fails, narrows the grant or
// does not arrive before the lease expires. Once it holds them it prepares the
// job, unless its pin already did, and applies the runner's rules (Inspect).
func runAdmitted(ctx context.Context, c Config, job Job, p Preflight, h Hub, clock Clock) (durablerun.Summary, error) {
	var zero durablerun.Summary
	if c.validate() != nil {
		return zero, ErrRefused
	}
	if ValidateJob(job) != nil {
		return zero, ErrRefused
	}
	active, err := artifactpath.Destination(filepath.Join(c.Root, ".active"))
	if err != nil {
		return zero, ErrRefused
	}
	c.Root = filepath.Dir(active)
	if os.Mkdir(active, 0700) != nil {
		return zero, ErrRefused
	}
	defer func() {
		os.Remove(filepath.Join(active, "lease.json"))
		os.Remove(filepath.Join(active, "lease.next"))
		os.Remove(active)
	}()
	instance := strings.ToLower(rand.Text())
	lease, err := claim(ctx, h, clock, instance, job.ID)
	if err != nil {
		return zero, err
	}
	// The release is sent even when the run was cancelled, so it is never
	// cancelled with it, but it keeps the context's values: the key and token
	// commands it runs report themselves to an observer the caller installed
	// (secret.ObserveDeclaredPrograms), as the enrollment's did.
	defer func() { h.Release(context.WithoutCancel(ctx), instance, job.ID) }()
	jobs, err := Jobs(c.Root)
	if err != nil || len(jobs) >= lease.MaxJobs {
		return zero, ErrRefused
	}
	if p.prepared == nil {
		if p, err = prepare(job, nil); err != nil {
			return zero, err
		}
	}
	if err = p.bind(c, job.ID); err != nil {
		return zero, err
	}
	dir := filepath.Join(c.Root, job.ID)
	if os.Mkdir(dir, 0700) != nil {
		return zero, ErrRefused
	}
	if syncDirectory(c.Root) != nil {
		return zero, ErrRefused
	}
	lease, err = renew(ctx, h, clock, instance, job.ID)
	if err != nil {
		return zero, err
	}
	jobs, err = Jobs(c.Root)
	if err != nil || len(jobs) > lease.MaxJobs {
		return zero, ErrRefused
	}
	runCtx, cancel := context.WithDeadline(ctx, clock.Now().Add(time.Duration(lease.MaxSeconds)*time.Second))
	defer cancel()
	// This timer runs independently of renewal HTTP and filesystem persistence.
	// Once it fires cancellation is irreversible, even if a renewal later arrives.
	expiry := clock.AfterFunc(lease.Expires.Sub(clock.Now()), cancel)
	defer expiry.Stop()
	raw, _ := json.Marshal(job)
	if persist(filepath.Join(dir, "claim.json"), raw) != nil || storeLease(active, lease) != nil || runCtx.Err() != nil {
		return zero, ErrRefused
	}
	done := make(chan struct{})
	ticks, stopTicks := clock.Tick(time.Second)
	go func() {
		defer close(done)
		defer stopTicks()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticks:
				// Cancellation at the last lease's expiry does not wait for a failed hub.
				renewCtx, stop := context.WithCancel(runCtx)
				deadline := clock.AfterFunc(lease.Expires.Sub(clock.Now()), stop)
				next, e := renew(renewCtx, h, clock, instance, job.ID)
				deadline.Stop()
				stop()
				if e != nil || next.MaxSeconds < lease.MaxSeconds || next.MaxJobs < lease.MaxJobs || storeLease(active, next) != nil {
					cancel()
					return
				}
				if runCtx.Err() != nil {
					return
				}
				expiry.Reset(next.Expires.Sub(clock.Now()))
				lease = next
			}
		}
	}()
	summary, err := p.prepared.Start(runCtx, RunPath(c.Root, job.ID))
	cancel()
	<-done
	return summary, err
}

// Serve polls only explicitly selected customer-local inbox documents. Already
// claimed IDs are skipped forever, including interrupted and refused jobs.
func Serve(ctx context.Context, c Config, inbox string) error {
	if !filepath.IsAbs(inbox) {
		return ErrRefused
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		entries, err := entries(inbox, 1000)
		if err != nil || len(entries) > 1000 {
			return ErrRefused
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				return ErrRefused
			}
			job, err := ReadJob(filepath.Join(inbox, entry.Name()))
			if err != nil {
				return err
			}
			if retained, err := Retained(c.Root, job.ID); err != nil {
				return err
			} else if retained {
				continue
			}
			if _, err = Run(ctx, c, job); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}

// Jobs lists the job ids the runner root reserves, in name order: every
// admitted job's, whatever became of it.
func Jobs(root string) ([]string, error) {
	list, err := entries(root, 10001)
	if err != nil {
		return nil, ErrRefused
	}
	ids := []string{}
	for _, e := range list {
		if e.IsDir() && runnerprotocol.ID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// RunPath is where the runner root retains job id's durable run, the folder
// `readmit run status --recovery` reads.
func RunPath(root, id string) string { return filepath.Join(root, id, "run") }

// Recover reads the durable run the runner root retains for job id, read-only,
// exactly as `readmit run status --recovery` does. It needs neither the hub
// nor a credential, and never sends, resumes or resets anything.
func Recover(root, id string) (durablerun.Recovery, error) {
	if !runnerprotocol.ID(id) {
		return durablerun.Recovery{}, ErrRefused
	}
	return durablerun.Recover(RunPath(root, id))
}

// Retained reports whether the runner root already holds id: Run reserves an
// admitted job's id there permanently, whatever became of the job, so a
// retained id never runs again. A root that cannot be read is refused rather
// than reported free.
func Retained(root, id string) (bool, error) {
	_, err := os.Lstat(filepath.Join(root, id))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, ErrRefused
}

// entries bounds allocation before enumeration, including an attacker-filled inbox.
func entries(path string, limit int) ([]os.DirEntry, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateMode(info.Mode()) {
		return nil, ErrRefused
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrRefused
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrRefused
	}
	list, err := f.ReadDir(limit + 1)
	if err != nil && err != io.EOF || len(list) > limit {
		return nil, ErrRefused
	}
	// os.ReadDir sorts; preserve deterministic admission order after bounding.
	slices.SortFunc(list, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return list, nil
}
