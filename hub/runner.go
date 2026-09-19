package hub

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// RunnerHandler adds short-lived admission for customer-local execution. Each
// renewal reloads both policies; loss of either authority fails closed.
func (s *Store) RunnerHandler(access *Access, policyPath string) http.Handler {
	team := s.TeamHandler(access)
	slots := make(chan struct{}, 4)
	var mu sync.Mutex
	type held struct {
		instance string
		subject  string
		job      string
		expires  time.Time
	}
	leases := map[string]held{}
	// A fresh handler waits out grants a stopped predecessor may have issued.
	readyAt := time.Now().Add(10 * time.Second)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "v1" || parts[1] != "projects" || parts[3] != "runner" {
			team.ServeHTTP(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "busy", 503)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if (r.Method != "POST" && r.Method != "DELETE") || r.URL.RawQuery != "" || !validProject(parts[2]) {
			http.Error(w, "request refused", 400)
			return
		}
		if access == nil {
			http.Error(w, "access refused", 403)
			return
		}
		p, err := s.authorize(access, r, parts[2], "enrollment")
		if err != nil || p.Kind != "runner" {
			http.Error(w, "access refused", 403)
			return
		}
		if _, err = s.authorize(access, r, parts[2], "execution"); err != nil {
			http.Error(w, "access refused", 403)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			http.Error(w, "request refused", 400)
			return
		}
		req, err := runnerprotocol.DecodeRequest(body)
		if err != nil {
			http.Error(w, "version or environment refused", 409)
			return
		}
		policy, err := readRunnerPolicy(policyPath)
		if err != nil {
			http.Error(w, "policy unavailable", 503)
			return
		}
		for _, g := range policy.Runners {
			if g.Project == parts[2] && g.Subject == p.Subject && g.Environment == req.Environment && g.Engine == req.Engine && g.Spec == req.Spec && g.Profile == req.Profile {
				mu.Lock()
				now := time.Now()
				key := g.Project + "/" + g.Environment
				old := leases[key]
				if r.Method == "DELETE" {
					if old.instance != req.Instance || old.job != req.Job || old.subject != p.Subject {
						mu.Unlock()
						http.Error(w, "lease refused", 409)
						return
					}
					delete(leases, key)
					mu.Unlock()
					w.WriteHeader(204)
					return
				}
				if now.Before(readyAt) || (now.Before(old.expires) && (old.instance != req.Instance || old.job != req.Job || old.subject != p.Subject)) {
					mu.Unlock()
					http.Error(w, "environment leased or recovering", 409)
					return
				}
				for key, lease := range leases {
					if !now.Before(lease.expires) {
						delete(leases, key)
					}
				}
				if len(leases) >= 4096 && (old.instance != req.Instance || old.job != req.Job || old.subject != p.Subject) {
					mu.Unlock()
					http.Error(w, "capacity refused", 503)
					return
				}
				expires := now.UTC().Add(10 * time.Second)
				leases[key] = held{req.Instance, p.Subject, req.Job, expires}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				data, _ := json.Marshal(runnerprotocol.Lease{Schema: "readmit-runner-lease/v1", Expires: expires, MaxSeconds: g.MaxSeconds, MaxJobs: g.MaxJobs})
				w.Write(data)
				return
			}
		}
		http.Error(w, "version or environment refused", 403)
	})
}
func readRunnerPolicy(path string) (runnerprotocol.Policy, error) {
	var zero runnerprotocol.Policy
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return zero, errAccess
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, errAccess
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return zero, errAccess
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return zero, errAccess
	}
	return runnerprotocol.DecodePolicy(data)
}
