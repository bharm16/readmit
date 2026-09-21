package hub

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// RunnerHandler adds short-lived admission for customer-local execution. Each
// renewal reloads both policies; loss of either authority fails closed.
func (s *Store) RunnerHandler(access *Access, policyPath string) http.Handler {
	return s.runnerHandler(access, policyPath, time.Now().Add(10*time.Second))
}
func (s *Store) runnerHandler(access *Access, policyPath string, readyAt time.Time) http.Handler {
	team := s.TeamHandler(access)
	var mu sync.Mutex
	type held struct {
		instance string
		subject  string
		job      string
		expires  time.Time
		deadline time.Time
		release  func() error
	}
	leases := map[string]held{}
	// A fresh handler waits out grants a stopped predecessor may have issued.

	runner := s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
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
					if old.release != nil {
						_ = old.release()
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
						if lease.release != nil {
							_ = lease.release()
						}
						delete(leases, key)
					}
				}
				if len(leases) >= 4096 && (old.instance != req.Instance || old.job != req.Job || old.subject != p.Subject) {
					mu.Unlock()
					http.Error(w, "capacity refused", 503)
					return
				}
				same := now.Before(old.expires) && old.instance == req.Instance && old.job == req.Job && old.subject == p.Subject
				if same && !now.Before(old.deadline) {
					mu.Unlock()
					http.Error(w, "job duration exhausted", 403)
					return
				}
				if !same {
					release, e := s.operationGuard().AdmitContext(r.Context(), "hub")
					if e != nil {
						mu.Unlock()
						http.Error(w, "operation admission refused", 403)
						return
					}
					old.release = release
					old.deadline = now.Add(time.Duration(g.MaxSeconds) * time.Second)
				}
				expires := now.UTC().Add(10 * time.Second)
				if expires.After(old.deadline) {
					expires = old.deadline
				}
				leases[key] = held{instance: req.Instance, subject: p.Subject, job: req.Job, expires: expires, deadline: old.deadline, release: old.release}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				data, _ := json.Marshal(runnerprotocol.Lease{Schema: "readmit-runner-lease/v1", Expires: expires, MaxSeconds: g.MaxSeconds, MaxJobs: g.MaxJobs})
				w.Write(data)
				return
			}
		}
		http.Error(w, "version or environment refused", 403)
	}), 5*time.Second)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) != 4 || parts[0] != "v1" || parts[1] != "projects" || parts[3] != "runner" {
			team.ServeHTTP(w, r)
			return
		}
		runner.ServeHTTP(w, r)
	})
}
func readRunnerPolicy(path string) (runnerprotocol.Policy, error) {
	data, err := readPrivatePolicy(path)
	if err != nil {
		return runnerprotocol.Policy{}, errAccess
	}
	return runnerprotocol.DecodePolicy(data)
}
