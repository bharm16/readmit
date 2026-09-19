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

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
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
	if err != nil || runnerprotocol.Exact(raw, "schema", "id", "spec") != nil || json.Unmarshal(raw, &job, json.RejectUnknownMembers(true)) != nil || job.Schema != "readmit-runner-job/v1" || !runnerprotocol.ID(job.ID) || !filepath.IsAbs(job.Spec) {
		return job, ErrRefused
	}
	return job, nil
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
	entries, err := entries(root, 10001)
	if err != nil {
		return s, ErrRefused
	}
	for _, e := range entries {
		if e.IsDir() && runnerprotocol.ID(e.Name()) {
			s.Jobs++
		}
	}
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
func persist(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrRefused
	}
	n, e := f.Write(data)
	if e == nil && n != len(data) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return ErrRefused
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return ErrRefused
	}
	defer root.Close()
	return durablerun.SyncDirectory(root, ".")
}
func storeLease(active string, lease runnerprotocol.Lease) error {
	raw, err := json.Marshal(lease)
	if err != nil {
		return ErrRefused
	}
	tmp := filepath.Join(active, "lease.next")
	if err = persist(tmp, raw); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(active, "lease.json")); err != nil {
		return ErrRefused
	}
	return syncDirectory(active)
}

// Run acquires one exclusive environment lease within this configured root. A
// crash leaves the claim intact. Neither restart nor lease expiry replays work.
func Run(ctx context.Context, c Config, job Job) (durablerun.Summary, error) {
	var zero durablerun.Summary
	if c.validate() != nil {
		return zero, ErrRefused
	}
	if job.Schema != "readmit-runner-job/v1" || !runnerprotocol.ID(job.ID) || !filepath.IsAbs(job.Spec) {
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
	lease, err := enroll(ctx, c, instance, job.ID)
	if err != nil {
		return zero, err
	}
	defer func() { admission(context.Background(), c, instance, job.ID, "DELETE") }()
	status, err := Health(c.Root)
	if err != nil || status.Jobs >= lease.MaxJobs {
		return zero, ErrRefused
	}
	plan, err := durablerun.Prepare(job.Spec)
	if err != nil {
		return zero, ErrRefused
	}
	bound := false
	for _, r := range plan.Resources() {
		if r.Kind == durablerun.EnvironmentResource {
			bound = r.Name == c.Environment
			if !bound {
				return zero, ErrRefused
			}
		}
	}
	if !bound {
		return zero, ErrRefused
	}
	dir := filepath.Join(c.Root, job.ID)
	if os.Mkdir(dir, 0700) != nil {
		return zero, ErrRefused
	}
	if syncDirectory(c.Root) != nil {
		return zero, ErrRefused
	}
	lease, err = enroll(ctx, c, instance, job.ID)
	if err != nil {
		return zero, err
	}
	status, err = Health(c.Root)
	if err != nil || status.Jobs > lease.MaxJobs {
		return zero, ErrRefused
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(lease.MaxSeconds)*time.Second)
	defer cancel()
	// This timer runs independently of renewal HTTP and filesystem persistence.
	// Once it fires cancellation is irreversible, even if a renewal later arrives.
	expiry := time.AfterFunc(time.Until(lease.Expires), cancel)
	defer expiry.Stop()
	raw, _ := json.Marshal(job)
	if persist(filepath.Join(dir, "claim.json"), raw) != nil || storeLease(active, lease) != nil || runCtx.Err() != nil {
		return zero, ErrRefused
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				// Cancellation at the last lease's expiry does not wait for a failed hub.
				renewCtx, stop := context.WithDeadline(runCtx, lease.Expires)
				next, e := enroll(renewCtx, c, instance, job.ID)
				stop()
				if e != nil || next.MaxSeconds < lease.MaxSeconds || next.MaxJobs < lease.MaxJobs || storeLease(active, next) != nil {
					cancel()
					return
				}
				if runCtx.Err() != nil {
					return
				}
				expiry.Reset(time.Until(next.Expires))
				lease = next
			}
		}
	}()
	summary, err := plan.Start(runCtx, filepath.Join(dir, "run"))
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
			if _, err = os.Lstat(filepath.Join(c.Root, job.ID)); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				return ErrRefused
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
