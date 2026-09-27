package testisolation

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

// readyContinuationSnapshot verifies setup effects from one captured file map.
// Inspect is deliberately weaker: an interrupted intent can be reported there,
// but can never restore readiness or establish permission to continue work.
func readyContinuationSnapshot(p *Prepared, path string) (Result, string, map[string][]byte, error) {
	if p == nil {
		return Result{}, "", nil, refused
	}
	files, err := artifactdir.Read(path, family.Layout)
	if err != nil {
		return Result{}, "", nil, refused
	}
	return verifyReadyContinuationSnapshot(p, files)
}

func verifyReadyContinuationSnapshot(p *Prepared, files map[string][]byte) (Result, string, map[string][]byte, error) {
	if p == nil {
		return Result{}, "", nil, refused
	}
	retained, err := readPlan(files)
	if err != nil || retained.identity != p.identity {
		return Result{}, "", nil, refused
	}
	var result Result
	if decodeResult(files["setup.json"], &result) != nil || result.Schema != ResultSchema || result.Plan != p.identity || result.Scope != p.document.Scope || result.Setup != "ready" || result.Cleanup != "not-started" || result.Complete || result.Reconciliation != "" || len(result.Resources) != len(p.document.Resources) || len(result.Manual) != len(p.document.Contract.Manual) {
		return Result{}, "", nil, refused
	}
	if _, ok := files["identity.sha256"]; ok {
		return Result{}, "", nil, refused
	}
	if _, ok := files["result.json"]; ok {
		return Result{}, "", nil, refused
	}
	if raw, ok := files["interrupted.json"]; ok {
		var checkpoint Result
		if decodeResult(raw, &checkpoint) != nil || !reflect.DeepEqual(checkpoint, result) {
			return Result{}, "", nil, refused
		}
	}
	journal := make(map[string][]byte, len(files))
	for name, raw := range files {
		journal[name] = raw
	}
	delete(journal, "interrupted.json")
	if verifyJournal(journal, result) != nil {
		return Result{}, "", nil, refused
	}
	for i, m := range p.document.Contract.Manual {
		if result.Manual[i] != (ManualClaim{ID: m.ID, Provenance: "operator-declared-current-execution"}) {
			return Result{}, "", nil, refused
		}
	}
	if !bytes.Equal(files["manual.json"], canonical(result.Manual)) {
		return Result{}, "", nil, refused
	}
	names := []string{}
	for name := range files {
		if strings.HasPrefix(name, "actions/") && strings.HasSuffix(name, "/action.json") {
			names = append(names, strings.TrimSuffix(name, "/action.json"))
		}
	}
	sort.Strings(names)
	snapshots := map[string]Snapshot{}
	replies := map[string]Reply{}
	requests := map[string]Request{}
	for _, name := range names {
		receipt, err := networkaction.VerifyHTTP(nestedFiles(files, name))
		if err != nil || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained {
			return Result{}, "", nil, refused
		}
		var spec networkaction.HTTPSpec
		if json.Unmarshal(files[name+"/action.json"], &spec, json.RejectUnknownMembers(true)) != nil {
			return Result{}, "", nil, refused
		}
		action := strings.TrimPrefix(spec.URL, p.registration.URL+"/fixture/v1/")
		phase := "setup"
		var request *Request
		if spec.Method == "GET" {
			action = strings.Split(action, "?")[0]
			phase = "read"
			if action != "state" {
				return Result{}, "", nil, refused
			}
		} else {
			if action != "lease-acquire" && action != "create" && action != "claim" {
				return Result{}, "", nil, refused
			}
			var r Request
			if decode(spec.Body, &r) != nil || p.validateRequest(r) != nil || r.Action != action {
				return Result{}, "", nil, refused
			}
			requests[name] = r
			request = &r
		}
		expected, err := p.httpPlan(phase, action, request)
		if err != nil || expected.Binding() != receipt.Binding {
			return Result{}, "", nil, refused
		}
		if phase == "read" {
			var snapshot Snapshot
			if decode(files[name+"/response.bin"], &snapshot) != nil || p.validateSnapshot(snapshot) != nil {
				return Result{}, "", nil, refused
			}
			snapshots[name] = snapshot
		} else {
			var reply Reply
			if decode(files[name+"/response.bin"], &reply) != nil || reply.Schema != ProtocolSchema || reply.Scope != p.document.Scope || reply.Outcome != "applied" {
				return Result{}, "", nil, refused
			}
			replies[name] = reply
		}
	}
	required := 0
	for _, r := range p.document.Contract.Resources {
		if r.Ownership != "select" {
			required++
		}
	}
	if len(result.Entries) != required+1 || len(replies) != required+1 || len(snapshots) < required+1 || len(names) < 2*(required+1) {
		return Result{}, "", nil, refused
	}
	lease := Lease{}
	var current Snapshot
	position := 0
	provisioned := map[string]Resource{}
	for i, e := range result.Entries {
		if e.Sequence != i+1 || e.Network != names[position] || e.Action != e.Request.Action || !reflect.DeepEqual(requests[e.Network], e.Request) {
			return Result{}, "", nil, refused
		}
		reply := replies[e.Network]
		next, ok := snapshots[names[position+1]]
		if !ok {
			return Result{}, "", nil, refused
		}
		if i == 0 {
			if e.Action != "lease-acquire" || e.State != "acknowledged" || e.Before != nil || e.After != nil || reply.Lease.Key != p.document.Scope.LeaseKey || reply.Lease.Owner != p.document.Scope.Owner || !token.MatchString(reply.Lease.Version) {
				return Result{}, "", nil, refused
			}
			lease = reply.Lease
			if next.Lease != lease || p.verifyInitial(next) != nil {
				return Result{}, "", nil, refused
			}
		} else {
			if e.Action != "create" && e.Action != "claim" || e.State != "verified" || e.Request.Lease != lease || reply.Lease != lease || next.Lease != lease || e.Before == nil || e.After == nil || !reflect.DeepEqual(*e.Before, current) || !reflect.DeepEqual(*e.After, next) {
				return Result{}, "", nil, refused
			}
			resource := e.Request.Resource
			actual := find(next.Resources, resource.Kind, resource.ID)
			if actual == nil || !matches(*actual, *resource) || actual.Owner != lease.Owner || !unchangedOthers(current, next, resource.Kind, resource.ID) {
				return Result{}, "", nil, refused
			}
			if _, exists := provisioned[resource.Alias]; exists {
				return Result{}, "", nil, refused
			}
			provisioned[resource.Alias] = *actual
		}
		current = next
		position += 2
	}
	for _, name := range names[position:] {
		actual, ok := snapshots[name]
		if !ok || currentPrerequisites(p, result, actual) != nil {
			return Result{}, "", nil, refused
		}
	}
	if result.Lease != lease {
		return Result{}, "", nil, refused
	}
	for i, want := range p.document.Resources {
		actual := result.Resources[i]
		if actual.Alias != want.Alias || !matches(actual, want) || !reflect.DeepEqual(find(current.Resources, want.Kind, want.ID), &actual) {
			return Result{}, "", nil, refused
		}
		if p.document.Contract.Resources[i].Ownership == "select" {
			if actual.Owner != "" || actual.Version != want.Version {
				return Result{}, "", nil, refused
			}
		} else if known, ok := provisioned[actual.Alias]; !ok || !reflect.DeepEqual(known, actual) {
			return Result{}, "", nil, refused
		}
	}
	return result, artifactdir.Identity("readmit-isolation-ready-snapshot/v1", files), files, nil
}

// Verify checks a completed historical isolation artifact using only the
// caller's captured bytes. It does not reconstruct execution authority.
func Verify(files map[string][]byte) (Result, error) { return openSnapshot(files) }

// VerifyReady checks an incomplete but fully established setup checkpoint.
// Its identity binds the exact byte map, and its result is facts, not authority.
func VerifyReady(p *Prepared, files map[string][]byte) (Result, string, error) {
	result, identity, _, err := verifyReadyContinuationSnapshot(p, files)
	return result, identity, err
}

// VerifyReadyRetained verifies ready facts without live registry paths or providers.
func VerifyReadyRetained(files map[string][]byte) (Result, string, error) {
	p, err := readPlan(files)
	if err != nil {
		return Result{}, "", err
	}
	return VerifyReady(p, files)
}

// verifyIncompleteCleanup preserves declared uncertainty without allowing an
// enclosing wrapper to hide an already retained mutation intent as not started.
func verifyIncompleteCleanup(p *Prepared, files map[string][]byte, expected Result, schema, setup string, resources []Resource) error {
	if len(files) == 0 {
		return refused
	}
	retained, err := readPlan(files)
	if err != nil || retained.identity != p.identity {
		return refused
	}
	var partial Result
	if decodeResult(files["interrupted.json"], &partial) != nil || partial.Schema != schema || partial.Plan != p.identity || partial.Scope != p.document.Scope || partial.Setup != setup || partial.Complete || partial.Cleanup != expected.Cleanup || !reflect.DeepEqual(partial.Entries, expected.Entries) || !reflect.DeepEqual(partial.Resources, resources) {
		return refused
	}
	if _, exists := files["identity.sha256"]; exists {
		return refused
	}
	for i, entry := range partial.Entries {
		if entry.Sequence != i+1 || entry.Action != entry.Request.Action || entry.Request.Scope != p.document.Scope || entry.Request.Schema != ProtocolSchema || entry.Request.Lease.Key != p.document.Scope.LeaseKey || entry.Request.Lease.Owner != p.document.Scope.Owner || !token.MatchString(entry.Request.Lease.Version) {
			return refused
		}
		if entry.Action == "delete" {
			r := entry.Request.Resource
			if r == nil || r.Owner != p.document.Scope.Owner || !reflect.DeepEqual(find(resources, r.Kind, r.ID), r) {
				return refused
			}
		} else if entry.Action != "lease-release" || entry.Request.Resource != nil {
			return refused
		}
		var intent Entry
		if decode(files[fmt.Sprintf("entry-%04d-intent.json", i+1)], &intent) != nil || intent.State != "uncertain" || intent.Sequence != entry.Sequence || !reflect.DeepEqual(intent.Request, entry.Request) || intent.Network != "" || intent.After != nil {
			return refused
		}
	}
	return nil
}
