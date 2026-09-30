package hub

import (
	"crypto/rand"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

const (
	connectedStateSchema = "readmit-runner-dispatches/v1"
	connectedStateFile   = "runner-connected.json"
	connectedStateNext   = ".runner-connected-next"
	maxConnectedState    = 16 << 20
	maxConnectedRecords  = 32768
	maxConnectedAttempts = 16384
)

var errConnectedState = errors.New("connected dispatch storage unavailable; uncertain ownership requires reconciliation")

var connectedDispatchFile = artifactdir.Document{
	MaxBytes: maxConnectedState, OwnerOnly: true,
	Staging: artifactdir.StagingName(connectedStateNext), RetainFailed: true,
	Errors:   artifactdir.DocumentErrors{Create: errConnectedState, Write: errConnectedState, Install: errConnectedState, Sync: errConnectedState},
	Refusals: artifactdir.DocumentRefusals{Irregular: errConnectedState, Read: errConnectedState},
}

type connectedDispatches struct {
	Schema     string              `json:"schema"`
	Generation uint64              `json:"generation"`
	Records    []connectedDispatch `json:"records"`
}

type connectedDispatch struct {
	Project    string                          `json:"project"`
	Subject    string                          `json:"subject"`
	Session    string                          `json:"session"`
	Request    runnerprotocol.ConnectedRequest `json:"request"`
	Generation uint64                          `json:"generation"`
	Admitted   time.Time                       `json:"admitted_at"`
	Expires    time.Time                       `json:"expires_at"`
	Deadline   time.Time                       `json:"deadline"`
	MaxSeconds int                             `json:"max_seconds"`
	MaxJobs    int                             `json:"max_jobs"`
	State      string                          `json:"state"`
	Attempts   []string                        `json:"attempts"`
}

func (r connectedDispatch) key() string {
	return r.Project + "/" + r.Request.Environment + "/" + r.Request.Job
}

func (s *Store) readConnectedDispatches() (connectedDispatches, error) {
	var state connectedDispatches
	if _, err := s.root.Lstat(connectedStateNext); !errors.Is(err, fs.ErrNotExist) {
		return state, errConnectedState
	}
	b, err := connectedDispatchFile.ReadIn(s.root, connectedStateFile)
	if errors.Is(err, fs.ErrNotExist) {
		return connectedDispatches{Schema: connectedStateSchema, Records: []connectedDispatch{}}, nil
	}
	if err != nil || runnerprotocol.Exact(b, "schema", "generation", "records") != nil || json.Unmarshal(b, &state, json.RejectUnknownMembers(true)) != nil || state.Schema != connectedStateSchema || state.Records == nil || len(state.Records) > maxConnectedRecords {
		return state, errConnectedState
	}
	var raw struct {
		Records []jsontext.Value `json:"records"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return state, errConnectedState
	}
	jobs, resources := map[string]bool{}, map[string]bool{}
	var previous uint64
	for i, record := range state.Records {
		if runnerprotocol.Exact(raw.Records[i], "project", "subject", "session", "request", "generation", "admitted_at", "expires_at", "deadline", "max_seconds", "max_jobs", "state", "attempts") != nil ||
			!validProject(record.Project) || record.Subject == "" || len(record.Subject) > 256 || record.Session == "" || len(record.Session) > 64 || record.Generation <= previous || record.Generation > state.Generation ||
			record.Admitted.IsZero() || !record.Deadline.After(record.Admitted) || record.Deadline.After(record.Admitted.Add(time.Duration(record.MaxSeconds)*time.Second)) || !record.Expires.After(record.Admitted) || record.Expires.After(record.Deadline) ||
			record.MaxSeconds < 1 || record.MaxSeconds > 3600 || record.MaxJobs < 1 || record.MaxJobs > 10000 || record.State != "admitted" && record.State != "uncertain" && record.State != "settled" ||
			record.Attempts == nil || len(record.Attempts) > maxConnectedAttempts || jobs[record.key()] {
			return state, errConnectedState
		}
		var nested struct {
			Request jsontext.Value `json:"request"`
		}
		if json.Unmarshal(raw.Records[i], &nested) != nil {
			return state, errConnectedState
		}
		if _, err := runnerprotocol.DecodeConnectedRequest(nested.Request); err != nil {
			return state, errConnectedState
		}
		steps := map[string]bool{}
		for _, step := range record.Attempts {
			if !runnerprotocol.ID(step) || steps[step] {
				return state, errConnectedState
			}
			steps[step] = true
		}
		if record.State != "settled" {
			for _, resource := range record.Request.Resources {
				if resources[resource] {
					return state, errConnectedState
				}
				resources[resource] = true
			}
		}
		jobs[record.key()] = true
		previous = record.Generation
	}
	if previous != state.Generation {
		return state, errConnectedState
	}
	return state, nil
}

func (s *Store) saveConnectedDispatches(state connectedDispatches) error {
	b, err := json.Marshal(state)
	if err != nil || len(b) > maxConnectedState || connectedDispatchFile.ReplaceIn(s.root, connectedStateFile, b) != nil {
		return errConnectedState
	}
	return nil
}

// Connected admission uses the existing runner route and holds its migration
// gate until the durable decision is retained. A v1 lease cannot slip between
// the compatibility check and publication of a connected resource claim.
func (s *Store) connectedRunnerHandler(access *Access, policyPath string, clock func() time.Time, legacyGate func(project, environment string, now time.Time) (bool, func())) http.Handler {
	session := rand.Text()
	readyAt := clock().Add(runnerHold)
	poisoned := false // guarded by s.mu, as is the durable sidecar
	return s.admitRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || len(parts) != 4 && len(parts) != 5 || parts[0] != "v2" || parts[1] != "projects" || !validProject(parts[2]) || parts[3] != "runner" {
			http.Error(w, "request refused", http.StatusBadRequest)
			return
		}
		action := "admit"
		if len(parts) == 5 {
			action = parts[4]
			if action != "renew" && action != "attempt" && action != "settle" {
				http.Error(w, "request refused", http.StatusBadRequest)
				return
			}
		}
		if access == nil {
			http.Error(w, "access refused", http.StatusForbidden)
			return
		}
		principal, view, err := s.authorizeProject(access, r, parts[2], "enrollment")
		if err != nil || principal.Kind != "runner" {
			http.Error(w, "access refused", http.StatusForbidden)
			return
		}
		if _, err = s.authorize(access, r, view, "execution"); err != nil {
			http.Error(w, "access refused", http.StatusForbidden)
			return
		}
		project := parts[2]
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, runnerprotocol.MaxConnectedBytes))
		if err != nil {
			http.Error(w, "request refused", http.StatusBadRequest)
			return
		}
		var request runnerprotocol.ConnectedRequest
		var command runnerprotocol.ConnectedCommand
		if action == "admit" {
			request, err = runnerprotocol.DecodeConnectedRequest(body)
		} else {
			command, err = runnerprotocol.DecodeConnectedCommand(body)
			request = command.Request
			if action == "renew" && (command.Step != "" || command.Outcome != "") || action == "attempt" && (command.Step == "" || command.Outcome != "") || action == "settle" && (command.Step != "" || command.Outcome == "") {
				err = runnerprotocol.ErrInvalid
			}
		}
		if err != nil {
			http.Error(w, "capability or request contract refused", http.StatusConflict)
			return
		}
		data, err := readPrivatePolicy(policyPath)
		if err != nil {
			http.Error(w, "connected policy unavailable", http.StatusServiceUnavailable)
			return
		}
		policy, err := runnerprotocol.DecodeConnectedPolicy(data)
		if err != nil {
			http.Error(w, "connected policy unavailable", http.StatusServiceUnavailable)
			return
		}
		capability, _ := request.Capabilities.Identity()
		at := slices.IndexFunc(policy.Runners, func(g runnerprotocol.ConnectedGrant) bool {
			return g.Project == project && g.Subject == principal.Subject && g.Environment == request.Environment && g.Engine == request.Engine && g.Capability == capability
		})
		if at == -1 {
			http.Error(w, "engine, capability or environment grant refused", http.StatusForbidden)
			return
		}
		grant := policy.Runners[at]
		if action != "settle" && clock().Before(readyAt) {
			http.Error(w, "runner authority transition pending; previous lease must finish", http.StatusConflict)
			return
		}
		if legacyGate != nil {
			active, finish := legacyGate(project, request.Environment, clock())
			defer finish()
			if active && action != "settle" {
				http.Error(w, "runner authority transition pending; previous lease must finish", http.StatusConflict)
				return
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if poisoned || s.Ready(r.Context()) != nil {
			http.Error(w, errConnectedState.Error(), http.StatusServiceUnavailable)
			return
		}
		release, err := s.operationGuard().AdmitContext(r.Context(), "hub")
		if err != nil {
			http.Error(w, "operation admission refused", http.StatusForbidden)
			return
		}
		defer release()
		state, err := s.readConnectedDispatches()
		if err != nil {
			poisoned = true
			http.Error(w, errConnectedState.Error(), http.StatusServiceUnavailable)
			return
		}
		now := clock().UTC()
		save := func() bool {
			if s.saveConnectedDispatches(state) != nil {
				poisoned = true
				http.Error(w, errConnectedState.Error(), http.StatusServiceUnavailable)
				return false
			}
			return true
		}
		changed := false
		for i := range state.Records {
			record := &state.Records[i]
			if record.State == "admitted" && (record.Session != session || now.Before(record.Admitted) || !now.Before(record.Expires)) {
				record.State = "uncertain"
				changed = true
			}
		}
		if changed && !save() {
			return
		}
		key := project + "/" + request.Environment + "/" + request.Job
		index := slices.IndexFunc(state.Records, func(record connectedDispatch) bool { return record.key() == key })
		if action == "admit" {
			if index != -1 {
				http.Error(w, "dispatch already retained; no retry is authorized", http.StatusConflict)
				return
			}
			count := 0
			for _, record := range state.Records {
				if record.State == "settled" {
					continue
				}
				if record.Project == project && record.Request.Environment == request.Environment {
					count++
				}
				for _, resource := range request.Resources {
					if slices.Contains(record.Request.Resources, resource) {
						http.Error(w, "target resource held; unresolved work requires reconciliation", http.StatusConflict)
						return
					}
				}
			}
			if count >= grant.MaxJobs || len(state.Records) >= maxConnectedRecords || state.Generation == math.MaxUint64 {
				http.Error(w, "dispatch capacity refused", http.StatusServiceUnavailable)
				return
			}
			state.Generation++
			deadline := now.Add(time.Duration(grant.MaxSeconds) * time.Second)
			state.Records = append(state.Records, connectedDispatch{Project: project, Subject: principal.Subject, Session: session, Request: request, Generation: state.Generation, Admitted: now, Expires: minExpiry(now, deadline), Deadline: deadline, MaxSeconds: grant.MaxSeconds, MaxJobs: grant.MaxJobs, State: "admitted", Attempts: []string{}})
			if !save() {
				return
			}
			writeConnectedLease(w, state.Records[len(state.Records)-1])
			return
		}
		if index == -1 {
			http.Error(w, "dispatch fence refused", http.StatusConflict)
			return
		}
		record := &state.Records[index]
		admittedIdentity, _ := record.Request.Identity()
		commandIdentity, _ := request.Identity()
		if record.Subject != principal.Subject || record.Generation != command.Generation || admittedIdentity != commandIdentity {
			http.Error(w, "dispatch fence refused", http.StatusConflict)
			return
		}
		if action == "settle" {
			if record.State == "settled" && command.Outcome != "settled" {
				http.Error(w, "terminal dispatch fence refused", http.StatusConflict)
				return
			}
			if command.Outcome == "settled" && record.State != "settled" && (record.State != "admitted" || record.Session != session || !now.Before(record.Expires) || !now.Before(record.Deadline)) {
				http.Error(w, "uncertain dispatch cannot be upgraded by a stale fence", http.StatusConflict)
				return
			}
			record.State = command.Outcome
			if save() {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		if record.State != "admitted" || record.Session != session || !now.Before(record.Expires) || !now.Before(record.Deadline) {
			http.Error(w, "dispatch uncertain or authority expired; no retry is authorized", http.StatusConflict)
			return
		}
		// Changed grants can tighten an existing duration, never extend it.
		deadline := record.Admitted.Add(time.Duration(grant.MaxSeconds) * time.Second)
		if deadline.Before(record.Deadline) {
			record.Deadline = deadline
			record.MaxSeconds = grant.MaxSeconds
			if record.Expires.After(deadline) {
				record.Expires = deadline
			}
		}
		record.MaxJobs = min(record.MaxJobs, grant.MaxJobs)
		if !now.Before(record.Deadline) {
			record.State = "uncertain"
			if save() {
				http.Error(w, "dispatch authority duration exhausted", http.StatusForbidden)
			}
			return
		}
		if action == "attempt" {
			if slices.Contains(record.Attempts, command.Step) {
				http.Error(w, "effect already attempted; no retry is authorized", http.StatusConflict)
				return
			}
			if len(record.Attempts) >= maxConnectedAttempts {
				http.Error(w, "attempt capacity refused", http.StatusServiceUnavailable)
				return
			}
			record.Attempts = append(record.Attempts, command.Step)
			record.Expires = minExpiry(now, record.Deadline)
			if save() {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		record.Expires = minExpiry(now, record.Deadline)
		if save() {
			writeConnectedLease(w, *record)
		}
	}), 5*time.Second)
}

// A v2 authority explicitly reserves its project/environment from legacy
// leases, which carry no resource keys and cannot share connected fences.
func (s *Store) connectedEnvironmentReserved(policyPath, project, environment string) (bool, error) {
	b, err := readPrivatePolicy(policyPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err == nil {
		p, err := runnerprotocol.DecodeConnectedPolicy(b)
		if err != nil {
			return false, err
		}
		for _, grant := range p.Runners {
			if grant.Project == project && grant.Environment == environment {
				return true, nil
			}
		}
	}
	// Withdrawing a grant cannot withdraw uncertainty or reopen a legacy bypass.
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readConnectedDispatches()
	if err != nil {
		return false, err
	}
	for _, record := range state.Records {
		if record.Project == project && record.Request.Environment == environment && record.State != "settled" {
			return true, nil
		}
	}
	return false, nil
}

func minExpiry(now, deadline time.Time) time.Time {
	expires := now.Add(10 * time.Second)
	if expires.After(deadline) {
		return deadline
	}
	return expires
}

func writeConnectedLease(w http.ResponseWriter, record connectedDispatch) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(runnerprotocol.ConnectedLease{Schema: runnerprotocol.ConnectedLeaseSchema, Expires: record.Expires, MaxSeconds: record.MaxSeconds, MaxJobs: record.MaxJobs, Generation: record.Generation})
	w.Write(b)
}
