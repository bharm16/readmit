package testisolation

import (
	"reflect"
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/networkaction"
)

// VerifyHistoricalReady verifies the original setup checkpoint even when a
// later read-only prerequisite check failed. It says nothing about current
// readiness. Continue and VerifyReady deliberately retain their stricter guard.
func VerifyHistoricalReady(files map[string][]byte) (Result, error) {
	p, err := readPlan(files)
	if err != nil {
		return Result{}, err
	}
	var setup Result
	if decodeResult(files["setup.json"], &setup) != nil {
		return Result{}, refused
	}
	names := []string{}
	for name := range files {
		if strings.HasPrefix(name, "actions/") && strings.HasSuffix(name, "/action.json") {
			names = append(names, strings.TrimSuffix(name, "/action.json"))
		}
	}
	sort.Strings(names)
	required := 1
	for _, resource := range p.document.Contract.Resources {
		if resource.Ownership != "select" {
			required++
		}
	}
	boundary := 2 * required
	if len(names) < boundary || len(names) > 256 {
		return Result{}, refused
	}
	checkpoint := setup
	if raw, ok := files["interrupted.json"]; ok {
		if decodeResult(raw, &checkpoint) != nil {
			return Result{}, refused
		}
		normalized := clone(checkpoint)
		normalized.Cleanup = setup.Cleanup
		if (checkpoint.Cleanup != "not-started" && checkpoint.Cleanup != "failed") || !reflect.DeepEqual(normalized, setup) {
			return Result{}, refused
		}
	}
	initial := make(map[string][]byte, len(files))
	for name, raw := range files {
		initial[name] = raw
	}
	if _, ok := initial["interrupted.json"]; ok {
		initial["interrupted.json"] = canonical(setup)
	}
	expected, err := p.httpPlan("read", "state", nil)
	if err != nil {
		return Result{}, err
	}
	for _, name := range names[boundary:] {
		action := nestedFiles(files, name)
		if len(action) > 8 {
			return Result{}, refused
		}
		for member, raw := range action {
			switch member {
			case "action.json", "policy.json", "intent.json", "decision.json", "operation.json", "response.bin", "result.json", "identity.sha256":
			default:
				return Result{}, refused
			}
			if len(raw) > 24<<20 {
				return Result{}, refused
			}
		}
		prepared, e := networkaction.PrepareHTTP(action["action.json"], action["policy.json"])
		if e != nil || prepared.Binding() != expected.Binding() {
			return Result{}, refused
		}
		// A later failed read may have no completion marker. If it has one, its
		// retained outcome must independently verify. Neither case becomes current
		// readiness or permission to mutate a resource.
		if _, sealed := action["identity.sha256"]; sealed {
			if _, e = networkaction.VerifyHTTP(action); e != nil {
				return Result{}, refused
			}
		}
		for member := range initial {
			if strings.HasPrefix(member, name+"/") {
				delete(initial, member)
			}
		}
	}
	result, _, err := VerifyReadyRetained(initial)
	if err != nil {
		return Result{}, err
	}
	result.Cleanup = checkpoint.Cleanup
	return result, nil
}
