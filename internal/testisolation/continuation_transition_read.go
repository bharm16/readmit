package testisolation

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

func OpenTransitions(path string) (Result, error) {
	files, err := artifactdir.Read(path, transitionFamily.Layout)
	if err != nil {
		return Result{}, err
	}
	return VerifyTransitions(files)
}
func VerifyTransitions(files map[string][]byte) (Result, error) {
	doc, err := verifyTransitions(files)
	return doc.Result, err
}

// TransitionProofs returns only policy-validated phase claims. The enclosing
// flow must match each identity to that exact phase's complete passed result.
func TransitionProofs(files map[string][]byte) (map[string]string, error) {
	doc, err := verifyTransitions(files)
	if err != nil {
		return nil, err
	}
	proofs := map[string]string{}
	for _, r := range doc.Reads {
		if r.Phase != "" {
			proofs[r.Phase] = r.Proof
		}
	}
	return proofs, nil
}
func verifyTransitions(files map[string][]byte) (transitionDocument, error) {
	var doc transitionDocument
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(TransitionSchema, files) || decodeResult(files["result.json"], &doc) != nil || doc.Schema != TransitionSchema || doc.Result.Schema != TransitionSchema || !doc.Result.Complete || len(doc.Reads) > 256 {
		return doc, refused
	}
	p, err := readPlan(nestedFiles(files, "previous"))
	if err != nil {
		return doc, err
	}
	prior, identity, _, err := verifyReadyContinuationSnapshot(p, nestedFiles(files, "previous"))
	if err != nil || identity != doc.Previous {
		return doc, refused
	}
	prepared, err := PrepareTransitions(p, doc.Policy)
	if err != nil {
		return doc, err
	}
	current := clone(prior)
	position := 0
	for i, r := range doc.Reads {
		if r.Path != fmt.Sprintf("reads/r%04d", i+1) {
			return doc, refused
		}
		actual, err := verifyTransitionRead(p, nestedFiles(files, r.Path))
		if err != nil {
			return doc, err
		}
		if r.Phase == "" {
			if r.Proof != "" || currentPrerequisites(p, current, actual) != nil {
				return doc, refused
			}
		} else {
			if !networkaction.ValidDigest(r.Proof) || position >= len(prepared.policy.Phases) || prepared.policy.Phases[position].Phase != r.Phase {
				return doc, refused
			}
			current, position, err = successor(prepared, current, actual, position, r.Phase)
			if err != nil {
				return doc, err
			}
		}
	}
	result := doc.Result
	if result.Plan != prior.Plan || result.Scope != prior.Scope || result.Reconciliation != "" || !reflect.DeepEqual(result.Resources, current.Resources) || !reflect.DeepEqual(result.Manual, prior.Manual) || !slices.Contains([]string{"transition-ready", "transition-invalidated"}, result.Setup) {
		return doc, refused
	}
	if result.Setup == "transition-ready" && len(doc.Reads) == 0 {
		return doc, refused
	}
	if result.Cleanup == "complete" {
		if len(doc.Reads) == 0 || result.Lease != (Lease{}) || verifyTransitionEffects(p, current, nestedFiles(files, "effects"), result) != nil {
			return doc, refused
		}
	} else {
		if !slices.Contains([]string{"not-started", "failed", "uncertain"}, result.Cleanup) || result.Lease != prior.Lease {
			return doc, refused
		}
		effectFiles := nestedFiles(files, "effects")
		if result.Cleanup == "not-started" {
			if len(result.Entries) != 0 || len(effectFiles) != 0 {
				return doc, refused
			}
		} else if !(result.Cleanup == "failed" && len(effectFiles) == 0 && len(result.Entries) == 0) && verifyIncompleteCleanup(p, effectFiles, result, TransitionEffectsSchema, "transition-ready", current.Resources) != nil {
			return doc, refused
		}
	}
	return doc, nil
}
func verifyTransitionRead(p *Prepared, files map[string][]byte) (Snapshot, error) {
	receipt, err := networkaction.VerifyHTTP(files)
	expected, e := p.httpPlan("read", "state", nil)
	var actual Snapshot
	if err != nil || e != nil || receipt.Binding != expected.Binding() || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained || decode(files["response.bin"], &actual) != nil || p.validateSnapshot(actual) != nil {
		return actual, refused
	}
	return actual, nil
}
func verifyTransitionEffects(p *Prepared, current Result, files map[string][]byte, outer Result) error {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(TransitionEffectsSchema, files) {
		return refused
	}
	retained, err := readPlan(files)
	if err != nil || retained.identity != p.identity {
		return refused
	}
	var result Result
	if decodeResult(files["result.json"], &result) != nil || result.Schema != TransitionEffectsSchema || result.Setup != "transition-ready" || result.Cleanup != "complete" || !result.Complete || result.Plan != p.identity || result.Scope != current.Scope || result.Lease != (Lease{}) || !reflect.DeepEqual(result.Resources, current.Resources) || !reflect.DeepEqual(result.Manual, current.Manual) || !reflect.DeepEqual(result.Entries, outer.Entries) || result.Reconciliation != "" || verifyJournal(files, result) != nil {
		return refused
	}
	owned := []Resource{}
	for i := len(current.Resources) - 1; i >= 0; i-- {
		if current.Resources[i].Owner == p.document.Scope.Owner {
			owned = append(owned, current.Resources[i])
		}
	}
	if len(result.Entries) != len(owned)+1 {
		return refused
	}
	names := []string{}
	for name := range files {
		if strings.HasPrefix(name, "actions/") && strings.HasSuffix(name, "/action.json") {
			names = append(names, strings.TrimSuffix(name, "/action.json"))
		}
	}
	sort.Strings(names)
	if len(names) != 2*len(result.Entries)+1 {
		return refused
	}
	before, err := verifyTransitionRead(p, nestedFiles(files, names[0]))
	if err != nil || currentPrerequisites(p, current, before) != nil {
		return refused
	}
	for i, entry := range result.Entries {
		path := names[2*i+1]
		expected := Request{Schema: ProtocolSchema, Scope: p.document.Scope, Lease: current.Lease, Action: "lease-release"}
		if i < len(owned) {
			expected.Action = "delete"
			resource := owned[i]
			expected.Resource = &resource
		}
		if entry.Sequence != i+1 || entry.Network != path || entry.State != "verified" || entry.Action != expected.Action || !reflect.DeepEqual(entry.Request, expected) || entry.Before == nil || !reflect.DeepEqual(*entry.Before, before) {
			return refused
		}
		networkFiles := nestedFiles(files, path)
		receipt, err := networkaction.VerifyHTTP(networkFiles)
		prepared, e := p.httpPlan("cleanup", expected.Action, &expected)
		var spec networkaction.HTTPSpec
		var reply Reply
		if err != nil || e != nil || receipt.Binding != prepared.Binding() || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained || json.Unmarshal(networkFiles["action.json"], &spec, json.RejectUnknownMembers(true)) != nil || !bytes.Equal(spec.Body, canonical(expected)) || decode(networkFiles["response.bin"], &reply) != nil || reply.Schema != ProtocolSchema || reply.Scope != p.document.Scope || reply.Outcome != "applied" {
			return refused
		}
		after, err := verifyTransitionRead(p, nestedFiles(files, names[2*i+2]))
		if err != nil || entry.After == nil || !reflect.DeepEqual(*entry.After, after) {
			return refused
		}
		if expected.Resource != nil {
			r := expected.Resource
			if reply.Lease != current.Lease || before.Lease != current.Lease || after.Lease != current.Lease || !reflect.DeepEqual(find(before.Resources, r.Kind, r.ID), r) || find(after.Resources, r.Kind, r.ID) != nil || !unchangedOthers(before, after, r.Kind, r.ID) {
				return refused
			}
		} else if reply.Lease != (Lease{}) || before.Lease != current.Lease || after.Lease != (Lease{}) || !p.noOwnedResources(after) || !unchangedOthers(before, after, "", "") {
			return refused
		}
		before = after
	}
	return nil
}

// VerifyTransitionsForPolicy also binds retained successor permissions to the
// policy independently derived from the enclosing immutable execution plan.
func VerifyTransitionsForPolicy(files map[string][]byte, expected TransitionPolicy) (Result, error) {
	doc, err := verifyTransitions(files)
	if err != nil || !bytes.Equal(canonical(doc.Policy), canonical(expected)) {
		return Result{}, refused
	}
	return doc.Result, nil
}
