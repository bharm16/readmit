package testisolation

import (
	"bytes"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/networkaction"
)

// InspectContinuation reports interrupted facts from one caller-held bounded
// snapshot. It never reopens a path, restores manual consent, or returns a live
// continuation. A completed outer artifact belongs to VerifyContinuation.
func InspectContinuation(files map[string][]byte) (Result, error) {
	if !inspectUnsealed(files, "checks", "cleanup") {
		return Result{}, refused
	}
	p, prior, err := inspectPrevious(files)
	if err != nil {
		return Result{}, err
	}
	groups, err := inspectGroups(files, "checks", "c", 128)
	if err != nil {
		return Result{}, err
	}
	for i, name := range groups {
		child := nestedFiles(files, name)
		if _, sealed := child["identity.sha256"]; sealed {
			result, err := openSnapshot(child)
			if err != nil || result.Plan != p.identity || result.Setup != "reconciled-not-ready" || result.Cleanup != "not-applicable" {
				return Result{}, refused
			}
			actual, err := verifyTransitionRead(p, nestedFiles(child, "actions/n0001"))
			if err != nil || currentPrerequisites(p, prior, actual) != nil {
				return Result{}, refused
			}
		} else {
			if i != len(groups)-1 {
				return Result{}, refused
			}
			retained, err := readPlan(child)
			if err != nil || retained.identity != p.identity {
				return Result{}, refused
			}
			if _, _, err = inspectStateRead(p, nestedFiles(child, "actions/n0001")); err != nil {
				return Result{}, err
			}
		}
	}
	result := interruptedFacts(prior, ContinuationSchema)
	cleanup := nestedFiles(files, "cleanup")
	if len(cleanup) == 0 {
		return result, nil
	}
	if len(groups) == 0 {
		return Result{}, refused
	}
	reconciliation := nestedFiles(cleanup, "recovery")
	recovered, err := openSnapshot(reconciliation)
	if err != nil || recovered.Plan != p.identity || recovered.Setup != "reconciled-not-ready" || recovered.Cleanup != "not-applicable" {
		return Result{}, refused
	}
	actual, err := verifyTransitionRead(p, nestedFiles(reconciliation, "actions/n0001"))
	if err != nil || currentPrerequisites(p, prior, actual) != nil {
		return Result{}, refused
	}
	if !equalFiles(reconciliation, nestedFiles(files, groups[len(groups)-1])) {
		return Result{}, refused
	}
	if _, sealed := cleanup["identity.sha256"]; sealed {
		done, err := openSnapshot(cleanup)
		if err != nil || done.Plan != p.identity || done.Setup != "recovered-no-setup" || done.Cleanup != "complete" || done.Reconciliation != strings.TrimSpace(string(reconciliation["identity.sha256"])) {
			return Result{}, refused
		}
		result.Cleanup = "complete"
		result.Lease = Lease{}
		result.Entries = clone(done.Entries)
		return result, nil
	}
	return inspectCleanup(p, prior, cleanup, result)
}

// InspectTransitions validates actual retained reads against the exact policy
// supplied by the enclosing immutable flow. It reports observed successor
// facts, never inferred phase approvals: the enclosing flow owns phase proofs.
func InspectTransitions(files map[string][]byte, expected TransitionPolicy) (Result, error) {
	if !inspectUnsealed(files, "reads", "effects") {
		return Result{}, refused
	}
	p, prior, err := inspectPrevious(files)
	if err != nil {
		return Result{}, err
	}
	prepared, err := PrepareTransitions(p, expected)
	if err != nil {
		return Result{}, err
	}
	if raw, ok := files["result.json"]; ok {
		var document transitionDocument
		if decodeResult(raw, &document) != nil || document.Schema != TransitionSchema || !bytes.Equal(canonical(document.Policy), canonical(expected)) {
			return Result{}, refused
		}
	}
	groups, err := inspectGroups(files, "reads", "r", 256)
	if err != nil {
		return Result{}, err
	}
	current := clone(prior)
	// A same-state observation can be either a check or an authored no-op
	// successor. Keep all possible policy positions without inventing acceptance.
	positions := map[int]bool{0: true}
	for i, name := range groups {
		actual, complete, err := inspectStateRead(p, nestedFiles(files, name))
		if err != nil {
			return Result{}, err
		}
		if !complete {
			if i != len(groups)-1 {
				return Result{}, refused
			}
			break
		}
		nextPositions := map[int]bool{}
		nextCurrent := current
		for position := range positions {
			if currentPrerequisites(p, current, actual) == nil {
				nextPositions[position] = true
			}
			if position < len(expected.Phases) {
				changed, next, err := successor(prepared, current, actual, position, expected.Phases[position].Phase)
				if err == nil {
					nextCurrent = changed
					nextPositions[next] = true
				}
			}
		}
		if len(nextPositions) == 0 {
			return Result{}, refused
		}
		current = nextCurrent
		positions = nextPositions
	}
	result := interruptedFacts(current, TransitionSchema)
	effects := nestedFiles(files, "effects")
	if len(effects) == 0 {
		return result, nil
	}
	if len(groups) == 0 {
		return Result{}, refused
	}
	if _, sealed := effects["identity.sha256"]; sealed {
		var done Result
		if decodeResult(effects["result.json"], &done) != nil || verifyTransitionEffects(p, current, effects, done) != nil {
			return Result{}, refused
		}
		result.Cleanup = "complete"
		result.Lease = Lease{}
		result.Entries = clone(done.Entries)
		return result, nil
	}
	return inspectCleanup(p, current, effects, result)
}
func interruptedFacts(prior Result, schema string) Result {
	result := clone(prior)
	result.Schema = schema
	result.Setup = "interrupted"
	result.Cleanup = "not-started"
	result.Complete = false
	result.Manual = []ManualClaim{}
	result.Entries = []Entry{}
	result.Reconciliation = ""
	return result
}
func inspectPrevious(files map[string][]byte) (*Prepared, Result, error) {
	previous := nestedFiles(files, "previous")
	p, err := readPlan(previous)
	if err != nil {
		return nil, Result{}, err
	}
	result, _, _, err := verifyReadyContinuationSnapshot(p, previous)
	return p, result, err
}
func inspectUnsealed(files map[string][]byte, readPrefix, effectPrefix string) bool {
	if _, ok := files["identity.sha256"]; ok {
		return false
	}
	if len(files) > 16000 {
		return false
	}
	total := 0
	for name, raw := range files {
		if name != "result.json" && !strings.HasPrefix(name, "previous/") && !strings.HasPrefix(name, readPrefix+"/") && !strings.HasPrefix(name, effectPrefix+"/") {
			return false
		}
		if !fs.ValidPath(name) || len(raw) > 16<<20 {
			return false
		}
		total += len(raw)
		if total > 512<<20 {
			return false
		}
	}
	return true
}
func inspectGroups(files map[string][]byte, prefix, letter string, limit int) ([]string, error) {
	seen := map[string]bool{}
	for name := range files {
		if strings.HasPrefix(name, prefix+"/") {
			parts := strings.Split(name, "/")
			if len(parts) < 3 {
				return nil, refused
			}
			seen[parts[1]] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > limit {
		return nil, refused
	}
	for i, name := range names {
		if name != fmt.Sprintf("%s%04d", letter, i+1) {
			return nil, refused
		}
		names[i] = prefix + "/" + name
	}
	return names, nil
}
func equalFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for name, raw := range a {
		other, exists := b[name]
		if !exists || !bytes.Equal(raw, other) {
			return false
		}
	}
	return true
}
func inspectStateRead(p *Prepared, files map[string][]byte) (Snapshot, bool, error) {
	if len(files) == 0 {
		return Snapshot{}, false, nil
	}
	expected, err := p.httpPlan("read", "state", nil)
	if err != nil {
		return Snapshot{}, false, err
	}
	if raw, ok := files["action.json"]; ok {
		var spec networkaction.HTTPSpec
		if decode(raw, &spec) != nil || !reflect.DeepEqual(spec, expected.Declaration()) {
			return Snapshot{}, false, refused
		}
	} else {
		return Snapshot{}, false, refused
	}
	if _, sealed := files["identity.sha256"]; !sealed {
		return Snapshot{}, false, nil
	}
	receipt, err := networkaction.VerifyHTTP(files)
	if err != nil || receipt.Binding != expected.Binding() {
		return Snapshot{}, false, refused
	}
	if receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained {
		return Snapshot{}, false, nil
	}
	actual, err := verifyTransitionRead(p, files)
	return actual, err == nil, err
}

// inspectCleanup validates the durable prefix and preserves the first unknown
// delete/release as uncertain. Even all observed entries without a completion
// seal remain an interrupted cleanup, never restored success or authority.
func inspectCleanup(p *Prepared, current Result, files map[string][]byte, result Result) (Result, error) {
	retained, err := readPlan(files)
	if err != nil || retained.identity != p.identity {
		return Result{}, refused
	}
	owned := []Resource{}
	for i := len(current.Resources) - 1; i >= 0; i-- {
		if current.Resources[i].Owner == p.document.Scope.Owner {
			owned = append(owned, current.Resources[i])
		}
	}
	intents := map[int]Entry{}
	for name, raw := range files {
		if strings.HasPrefix(name, "entry-") && strings.HasSuffix(name, "-intent.json") {
			number, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "entry-"), "-intent.json"))
			var intent Entry
			if err != nil || number < 1 || number > len(owned)+1 || name != fmt.Sprintf("entry-%04d-intent.json", number) || decode(raw, &intent) != nil || intent.Sequence != number || intent.State != "uncertain" || intent.Network != "" || intent.After != nil {
				return Result{}, refused
			}
			intents[number] = intent
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "entry-") {
			found := false
			for number := range intents {
				for _, suffix := range []string{"intent", "reply", "observed"} {
					if name == fmt.Sprintf("entry-%04d-%s.json", number, suffix) {
						found = true
					}
				}
			}
			if !found {
				return Result{}, refused
			}
		}
	}
	groups, err := inspectGroups(files, "actions", "n", 2*(len(owned)+1)+1)
	if err != nil || len(groups) > 2*len(intents)+1 {
		return Result{}, refused
	}
	result.Cleanup = "failed"
	if len(intents) == 0 {
		for _, name := range groups {
			if _, _, err := inspectStateRead(p, nestedFiles(files, name)); err != nil {
				return Result{}, err
			}
		}
		return result, nil
	}
	result.Cleanup = "uncertain"
	var previous Snapshot
	for i := 1; i <= len(intents); i++ {
		intent, ok := intents[i]
		if !ok {
			return Result{}, refused
		}
		expected := Request{Schema: ProtocolSchema, Scope: p.document.Scope, Lease: current.Lease, Action: "lease-release"}
		alias := ""
		if i <= len(owned) {
			resource := owned[i-1]
			expected.Action = "delete"
			expected.Resource = &resource
			alias = resource.Alias
		}
		if intent.Action != expected.Action || intent.Alias != alias || !reflect.DeepEqual(intent.Request, expected) {
			return Result{}, refused
		}
		before, complete, err := inspectStateRead(p, nestedFiles(files, fmt.Sprintf("actions/n%04d", 2*i-1)))
		if err != nil || !complete || intent.Before == nil || !reflect.DeepEqual(*intent.Before, before) || i == 1 && currentPrerequisites(p, current, before) != nil || i > 1 && !reflect.DeepEqual(previous, before) {
			return Result{}, refused
		}
		path := fmt.Sprintf("actions/n%04d", 2*i)
		network := nestedFiles(files, path)
		plan, err := p.httpPlan("cleanup", expected.Action, &expected)
		if err != nil {
			return Result{}, err
		}
		if raw, exists := network["action.json"]; exists {
			var spec networkaction.HTTPSpec
			if decode(raw, &spec) != nil || !reflect.DeepEqual(spec, plan.Declaration()) {
				return Result{}, refused
			}
			intent.Network = path
		}
		entry := intent
		if raw, exists := files[fmt.Sprintf("entry-%04d-reply.json", i)]; exists {
			var replyEntry Entry
			if decode(raw, &replyEntry) != nil || replyEntry.Sequence != i || replyEntry.State != "acknowledged" || replyEntry.Network != path || replyEntry.Alias != alias || replyEntry.Action != expected.Action || !reflect.DeepEqual(replyEntry.Request, expected) || !reflect.DeepEqual(replyEntry.Before, intent.Before) || replyEntry.After != nil {
				return Result{}, refused
			}
			receipt, err := networkaction.VerifyHTTP(network)
			var reply Reply
			if err != nil || receipt.Binding != plan.Binding() || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained || decode(network["response.bin"], &reply) != nil || reply.Schema != ProtocolSchema || reply.Scope != p.document.Scope || reply.Outcome != "applied" || expected.Action == "delete" && reply.Lease != current.Lease || expected.Action == "lease-release" && reply.Lease != (Lease{}) {
				return Result{}, refused
			}
			entry = replyEntry
			if raw, exists := files[fmt.Sprintf("entry-%04d-observed.json", i)]; exists {
				var observed Entry
				after, complete, err := inspectStateRead(p, nestedFiles(files, fmt.Sprintf("actions/n%04d", 2*i+1)))
				if err != nil || !complete || decode(raw, &observed) != nil || observed.Sequence != i || observed.State != "verified" || observed.Network != path || observed.Alias != alias || observed.Action != expected.Action || !reflect.DeepEqual(observed.Request, expected) || !reflect.DeepEqual(observed.Before, intent.Before) || observed.After == nil || !reflect.DeepEqual(*observed.After, after) {
					return Result{}, refused
				}
				if expected.Resource != nil {
					r := expected.Resource
					if before.Lease != current.Lease || after.Lease != current.Lease || !reflect.DeepEqual(find(before.Resources, r.Kind, r.ID), r) || find(after.Resources, r.Kind, r.ID) != nil || !unchangedOthers(before, after, r.Kind, r.ID) {
						return Result{}, refused
					}
				} else if before.Lease != current.Lease || after.Lease != (Lease{}) || !p.noOwnedResources(after) || !unchangedOthers(before, after, "", "") {
					return Result{}, refused
				}
				entry = observed
				previous = after
			}
		} else if _, exists := files[fmt.Sprintf("entry-%04d-observed.json", i)]; exists {
			return Result{}, refused
		}
		if i < len(intents) && entry.State != "verified" {
			return Result{}, refused
		}
		if entry.Action == "lease-release" && entry.State == "verified" {
			result.Lease = Lease{}
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}
