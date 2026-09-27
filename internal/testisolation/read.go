package testisolation

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

func readPlan(files map[string][]byte) (*Prepared, error) {
	var document planDocument
	if decode(files["plan.json"], &document) != nil || document.Schema != PlanSchema {
		return nil, refused
	}
	p, err := prepare(canonical(document.Contract), canonical(document.Registry), document.Policy, document.Options)
	if err != nil || !bytes.Equal(canonical(p.document), files["plan.json"]) {
		return nil, refused
	}
	return p, nil
}

// Inspect reads completed or interrupted effect journals without contacting an
// adapter. It never returns execution authority or reconstructs manual consent.
func Inspect(directory string) (Result, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Result{}, refused
	}
	p, err := readPlan(files)
	if err != nil {
		return Result{}, err
	}
	if _, ok := files["identity.sha256"]; ok {
		return Open(directory)
	}
	result := Result{Schema: ResultSchema, Plan: p.identity, Scope: p.document.Scope, Setup: "interrupted", Cleanup: "not-started", Entries: []Entry{}, Resources: []Resource{}, Manual: []ManualClaim{}}
	if raw, ok := files["setup.json"]; ok {
		var checkpoint Result
		if decodeResult(raw, &checkpoint) != nil || checkpoint.Schema != ResultSchema || checkpoint.Plan != p.identity || checkpoint.Scope != p.document.Scope || checkpoint.Setup != "ready" || checkpoint.Complete {
			return Result{}, refused
		}
		result = checkpoint
		result.Entries = []Entry{}
	}
	if raw, ok := files["interrupted.json"]; ok {
		var checkpoint Result
		if decodeResult(raw, &checkpoint) != nil || checkpoint.Schema != ResultSchema || checkpoint.Plan != p.identity || checkpoint.Scope != p.document.Scope || checkpoint.Complete {
			return Result{}, refused
		}
		result = checkpoint
		result.Entries = []Entry{}
	}
	for i := 1; i <= 128; i++ {
		name := fmt.Sprintf("entry-%04d-intent.json", i)
		raw, ok := files[name]
		if !ok {
			break
		}
		var entry Entry
		if decode(raw, &entry) != nil || entry.Sequence != i || entry.State != "uncertain" || p.validateRequest(entry.Request) != nil || entry.Action != entry.Request.Action {
			return Result{}, refused
		}
		for _, suffix := range []string{"reply", "observed"} {
			if raw, ok := files[fmt.Sprintf("entry-%04d-%s.json", i, suffix)]; ok {
				var next Entry
				if decode(raw, &next) != nil || next.Sequence != i || !reflect.DeepEqual(next.Request, entry.Request) {
					return Result{}, refused
				}
				entry = next
			}
		}
		result.Entries = append(result.Entries, entry)
		// An unsettled intent may have reached the target. Verified prior setup
		// remains separate from a later cleanup failure.
		if entry.State == "uncertain" || entry.State == "acknowledged" && (entry.Action == "create" || entry.Action == "claim" || entry.Action == "delete" || entry.Action == "lease-release") {
			if entry.Action == "delete" || entry.Action == "lease-release" {
				result.Cleanup = "uncertain"
			} else {
				result.Setup = "uncertain"
			}
		}
	}
	return result, nil
}

// Open independently checks the retained network actions and postconditions.
// A sealed result proves its recorded effects, never that the target stayed so.
func Open(directory string) (Result, error) {
	result, _, _, err := readSnapshot(directory)
	return result, err
}

func readSnapshot(directory string) (Result, string, map[string][]byte, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Result{}, "", nil, refused
	}
	result, err := openSnapshot(files)
	if err != nil {
		return Result{}, "", nil, err
	}
	return result, strings.TrimSpace(string(files["identity.sha256"])), files, nil
}

func nestedFiles(files map[string][]byte, prefix string) map[string][]byte {
	nested := map[string][]byte{}
	for name, raw := range files {
		if strings.HasPrefix(name, prefix+"/") {
			nested[strings.TrimPrefix(name, prefix+"/")] = raw
		}
	}
	return nested
}

func openSnapshot(files map[string][]byte) (Result, error) {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ResultSchema, files) {
		return Result{}, refused
	}
	p, err := readPlan(files)
	if err != nil {
		return Result{}, err
	}
	var result Result
	if decodeResult(files["result.json"], &result) != nil || result.Schema != ResultSchema || result.Plan != p.identity || result.Scope != p.document.Scope || !result.Complete || len(result.Entries) > 128 {
		return Result{}, refused
	}
	if verifyJournal(files, result) != nil {
		return Result{}, refused
	}
	snapshots := []Snapshot{}
	snapshotNames := []string{}
	replies := map[string]Reply{}
	requests := map[string]Request{}
	capabilities := []Capabilities{}
	for name, raw := range files {
		if !strings.HasPrefix(name, "actions/") || !strings.HasSuffix(name, "/action.json") {
			continue
		}
		base := strings.TrimSuffix(name, "/action.json")
		receipt, err := networkaction.VerifyHTTP(nestedFiles(files, base))
		if err != nil || receipt.State != "responded" || receipt.HTTPStatus != 200 || !receipt.ResponseRetained {
			return Result{}, refused
		}
		var spec networkaction.HTTPSpec
		if json.Unmarshal(raw, &spec, json.RejectUnknownMembers(true)) != nil {
			return Result{}, refused
		}
		action := strings.TrimPrefix(spec.URL, p.registration.URL+"/fixture/v1/")
		phase := "read"
		var request *Request
		if spec.Method == "GET" {
			action = strings.Split(action, "?")[0]
		} else {
			if action == "delete" || action == "lease-release" {
				phase = "cleanup"
			} else {
				phase = "setup"
			}
			var req Request
			if decode(spec.Body, &req) != nil || p.validateRequest(req) != nil {
				return Result{}, refused
			}
			if req.Action != action {
				return Result{}, refused
			}
			request = &req
			requests[base] = req
		}
		expected, err := p.httpPlan(phase, action, request)
		if err != nil || expected.Binding() != receipt.Binding {
			return Result{}, refused
		}
		body := files[base+"/response.bin"]
		switch action {
		case "capabilities":
			var c Capabilities
			if decode(body, &c) != nil || p.validateCapabilities(c) != nil {
				return Result{}, refused
			}
			capabilities = append(capabilities, c)
		case "state":
			var snapshot Snapshot
			if decode(body, &snapshot) != nil || p.validateSnapshot(snapshot) != nil {
				return Result{}, refused
			}
			snapshots = append(snapshots, snapshot)
			snapshotNames = append(snapshotNames, base)
		default:
			var reply Reply
			if decode(body, &reply) != nil || reply.Schema != ProtocolSchema || reply.Scope != p.document.Scope || reply.Outcome != "applied" {
				return Result{}, refused
			}
			replies[base] = reply
		}
	}
	if result.Setup == "preflight-verified" {
		var c Capabilities
		if len(capabilities) != 1 || len(snapshots) != 0 || len(replies) != 0 || len(result.Resources) != 0 || len(result.Manual) != 0 || result.Lease != (Lease{}) || len(result.Entries) != 0 || decode(files["preflight.json"], &c) != nil || !reflect.DeepEqual(c, capabilities[0]) || result.Cleanup != "not-applicable" {
			return Result{}, refused
		}
		return result, nil
	}
	if result.Setup == "reconciled-not-ready" {
		if result.Reconciliation != "" || len(capabilities) != 0 || len(replies) != 0 || len(result.Manual) != 0 || len(result.Entries) != 0 || len(snapshots) != 1 || !reflect.DeepEqual(result.Resources, ownedResources(p, snapshots[0])) || result.Lease != snapshots[0].Lease || !validReconciliation(p, snapshots[0], result) {
			return Result{}, refused
		}
		for _, actual := range result.Resources {
			expected := find(p.document.Resources, actual.Kind, actual.ID)
			if expected == nil || !matches(actual, *expected) {
				return Result{}, refused
			}
		}
		return result, nil
	}
	if result.Cleanup != "complete" || result.Lease != (Lease{}) || !slices.Contains([]string{"ready", "failed", "recovered-no-setup"}, result.Setup) {
		return Result{}, refused
	}
	observed := func(snapshot *Snapshot, at string, before bool) bool {
		if snapshot == nil {
			return false
		}
		for i, actual := range snapshots {
			if (before && snapshotNames[i] < at || !before && snapshotNames[i] > at) && reflect.DeepEqual(actual, *snapshot) {
				return true
			}
		}
		return false
	}
	lease := Lease{}
	owned := map[string]Resource{}
	if result.Setup == "recovered-no-setup" {
		recovered, err := openSnapshot(nestedFiles(files, "recovery"))
		if err != nil || recovered.Setup != "reconciled-not-ready" || recovered.Plan != p.identity || result.Reconciliation != strings.TrimSpace(string(files["recovery/identity.sha256"])) || !reflect.DeepEqual(result.Resources, recovered.Resources) || len(result.Manual) != 0 {
			return Result{}, refused
		}
		lease = recovered.Lease
		for _, r := range recovered.Resources {
			owned[r.Kind+"/"+r.ID] = r
		}
	} else if result.Reconciliation != "" {
		return Result{}, refused
	}
	creates := 0
	deletes := 0
	provisioned := []Resource{}
	deleted := []Resource{}
	if len(result.Entries) < 1 || result.Setup != "recovered-no-setup" && len(result.Entries) < 2 {
		return Result{}, refused
	}
	for i, entry := range result.Entries {
		if entry.Sequence != i+1 || entry.Action != entry.Request.Action || p.validateRequest(entry.Request) != nil {
			return Result{}, refused
		}
		var intent Entry
		if decodeResult(files[fmt.Sprintf("entry-%04d-intent.json", i+1)], &intent) != nil || intent.State != "uncertain" || !reflect.DeepEqual(intent.Request, entry.Request) {
			return Result{}, refused
		}
		reply, ok := replies[entry.Network]
		if !ok || !reflect.DeepEqual(requests[entry.Network], entry.Request) {
			return Result{}, refused
		}
		delete(replies, entry.Network)
		switch entry.Action {
		case "lease-acquire":
			if i != 0 || reply.Lease.Owner != p.document.Scope.Owner || reply.Lease.Key != p.document.Scope.LeaseKey || !token.MatchString(reply.Lease.Version) {
				return Result{}, refused
			}
			lease = reply.Lease
		case "create", "claim":
			if entry.Request.Lease != lease || reply.Lease != lease || entry.State != "verified" || !observed(entry.Before, entry.Network, true) || !observed(entry.After, entry.Network, false) || entry.Before.Lease != lease || entry.After.Lease != lease {
				return Result{}, refused
			}
			r := entry.Request.Resource
			actual := find(entry.After.Resources, r.Kind, r.ID)
			if actual == nil || !matches(*actual, *r) || actual.Owner != lease.Owner || !unchangedOthers(*entry.Before, *entry.After, r.Kind, r.ID) {
				return Result{}, refused
			}
			if creates == 0 && p.verifyInitial(*entry.Before) != nil {
				return Result{}, refused
			}
			owned[r.Kind+"/"+r.ID] = *actual
			provisioned = append(provisioned, *actual)
			creates++
		case "delete":
			if entry.Request.Lease != lease || reply.Lease != lease || entry.State != "verified" || !observed(entry.Before, entry.Network, true) || !observed(entry.After, entry.Network, false) {
				return Result{}, refused
			}
			r := entry.Request.Resource
			actual := find(entry.Before.Resources, r.Kind, r.ID)
			if known, ok := owned[r.Kind+"/"+r.ID]; !ok || !reflect.DeepEqual(known, *r) {
				return Result{}, refused
			}
			if actual == nil || !reflect.DeepEqual(*actual, *r) || r.Owner != lease.Owner || find(entry.After.Resources, r.Kind, r.ID) != nil || !unchangedOthers(*entry.Before, *entry.After, r.Kind, r.ID) {
				return Result{}, refused
			}
			delete(owned, r.Kind+"/"+r.ID)
			deleted = append(deleted, *r)
			deletes++
		case "lease-release":
			if i != len(result.Entries)-1 || entry.Request.Lease != lease || reply.Lease != (Lease{}) || entry.State != "verified" || !observed(entry.Before, entry.Network, true) || !observed(entry.After, entry.Network, false) || entry.Before.Lease != lease || entry.After.Lease != (Lease{}) || !unchangedOthers(*entry.Before, *entry.After, "", "") || !p.noOwnedResources(*entry.After) {
				return Result{}, refused
			}
			lease = Lease{}
		default:
			return Result{}, refused
		}
	}
	if lease != (Lease{}) || len(owned) != 0 || len(replies) != 0 || creates != deletes && result.Setup != "recovered-no-setup" {
		return Result{}, refused
	}
	expectedDeletes := clone(provisioned)
	if result.Setup == "recovered-no-setup" {
		expectedDeletes = clone(result.Resources)
	}
	slices.Reverse(expectedDeletes)
	if !bytes.Equal(canonical(deleted), canonical(expectedDeletes)) {
		return Result{}, refused
	}
	if result.Setup == "ready" {
		if len(result.Manual) != len(p.document.Contract.Manual) {
			return Result{}, refused
		}
		for i, m := range p.document.Contract.Manual {
			if result.Manual[i] != (ManualClaim{ID: m.ID, Provenance: "operator-declared-current-execution"}) {
				return Result{}, refused
			}
		}
		found := false
		for _, snapshot := range snapshots {
			if p.verifyInitial(snapshot) == nil {
				found = true
				break
			}
		}
		if !found {
			return Result{}, refused
		}
		required := 0
		for _, r := range p.document.Contract.Resources {
			if r.Ownership != "select" {
				required++
			}
		}
		var setup Result
		if decodeResult(files["setup.json"], &setup) != nil || setup.Schema != ResultSchema || setup.Plan != p.identity || setup.Scope != p.document.Scope || setup.Setup != "ready" || setup.Cleanup != "not-started" || setup.Reconciliation != "" || setup.Complete || !reflect.DeepEqual(setup.Resources, result.Resources) || !reflect.DeepEqual(setup.Manual, result.Manual) {
			return Result{}, refused
		}
		if len(result.Resources) != len(p.document.Resources) {
			return Result{}, refused
		}
		for i, expected := range p.document.Resources {
			actual := result.Resources[i]
			if !matches(actual, expected) {
				return Result{}, refused
			}
			if p.document.Contract.Resources[i].Ownership == "select" {
				if actual.Owner != "" || actual.Version != expected.Version {
					return Result{}, refused
				}
			} else {
				found := find(provisioned, actual.Kind, actual.ID)
				if found == nil || !reflect.DeepEqual(*found, actual) {
					return Result{}, refused
				}
			}
		}
		if len(setup.Entries) != required+1 || !reflect.DeepEqual(setup.Entries, result.Entries[:required+1]) || setup.Lease != repliesLease(result.Entries, files) {
			return Result{}, refused
		}
		if creates != required {
			return Result{}, refused
		}
	}
	return result, nil
}
func decodeResult(raw []byte, v any) error {
	if len(raw) > 16<<20 || json.Unmarshal(raw, v, json.RejectUnknownMembers(true)) != nil {
		return refused
	}
	return nil
}
func (p *Prepared) validateRequest(r Request) error {
	if r.Schema != ProtocolSchema || r.Scope != p.document.Scope {
		return refused
	}
	if r.Action == "lease-acquire" {
		if r.Resource != nil || r.Lease != (Lease{}) {
			return refused
		}
		return nil
	}
	if r.Lease.Key != p.document.Scope.LeaseKey || r.Lease.Owner != p.document.Scope.Owner || !token.MatchString(r.Lease.Version) {
		return refused
	}
	if r.Action == "lease-release" {
		if r.Resource != nil {
			return refused
		}
		return nil
	}
	if r.Resource == nil {
		return refused
	}
	for i, expected := range p.document.Resources {
		if r.Resource.Alias != expected.Alias {
			continue
		}
		if !matches(*r.Resource, expected) {
			return refused
		}
		ownership := p.document.Contract.Resources[i].Ownership
		if r.Action == "delete" && ownership != "select" && r.Resource.Owner == p.document.Scope.Owner && token.MatchString(r.Resource.Version) {
			return nil
		}
		if r.Action == ownership && ownership != "select" && reflect.DeepEqual(*r.Resource, expected) {
			return nil
		}
	}
	return refused
}
func ownedResources(p *Prepared, s Snapshot) []Resource {
	resources := []Resource{}
	for _, expected := range p.document.Resources {
		if actual := find(s.Resources, expected.Kind, expected.ID); actual != nil && actual.Owner == p.document.Scope.Owner {
			resources = append(resources, *actual)
		}
	}
	return resources
}

// Reconcile only observes. It neither releases a lease nor retries provisioning
// or cleanup, and its retained evidence never restores manual confirmation.
func Reconcile(ctx context.Context, p *Prepared, a Authorities, interrupted, output string) (Result, error) {
	previous, err := Inspect(interrupted)
	if err != nil || previous.Plan != p.identity {
		return Result{}, refused
	}
	if _, err := p.Check(ctx, a, "read"); err != nil {
		return Result{}, err
	}
	s, err := newSession(p, a, output)
	if err != nil {
		return Result{}, err
	}
	defer s.Close()
	var snapshot Snapshot
	if _, err = s.call(ctx, "read", "state", nil, &snapshot); err != nil || p.validateSnapshot(snapshot) != nil || snapshot.Lease != (Lease{}) && (snapshot.Lease.Key != p.document.Scope.LeaseKey || snapshot.Lease.Owner != p.document.Scope.Owner) {
		return Result{}, refused
	}
	s.result.Lease = snapshot.Lease
	s.result.Resources = ownedResources(p, snapshot)
	for _, actual := range s.result.Resources {
		expected := find(p.document.Resources, actual.Kind, actual.ID)
		if expected == nil || !matches(actual, *expected) {
			return Result{}, refused
		}
	}
	for _, actual := range snapshot.Resources {
		if actual.Owner == p.document.Scope.Owner && find(s.result.Resources, actual.Kind, actual.ID) == nil {
			return Result{}, refused
		}
	}
	s.result.Setup = "reconciled-not-ready"
	s.result.Cleanup = "not-applicable"
	if snapshot.Lease == (Lease{}) {
		s.result.Cleanup = "observed-absent"
	}
	if !validReconciliation(p, snapshot, s.result) {
		return Result{}, refused
	}
	if err := s.seal(); err != nil {
		return Result{}, err
	}
	return clone(s.result), nil
}

func validReconciliation(p *Prepared, snapshot Snapshot, result Result) bool {
	for _, actual := range snapshot.Resources {
		if actual.Owner == p.document.Scope.Owner && find(result.Resources, actual.Kind, actual.ID) == nil {
			return false
		}
	}
	if snapshot.Lease == (Lease{}) {
		if result.Cleanup != "observed-absent" || len(result.Resources) != 0 {
			return false
		}
		for i, expected := range p.document.Resources {
			if p.document.Contract.Resources[i].Ownership != "select" && find(snapshot.Resources, expected.Kind, expected.ID) != nil {
				return false
			}
		}
		return true
	}
	return snapshot.Lease.Owner == p.document.Scope.Owner && snapshot.Lease.Key == p.document.Scope.LeaseKey && token.MatchString(snapshot.Lease.Version) && result.Cleanup == "not-applicable"
}

func (p *Prepared) noOwnedResources(snapshot Snapshot) bool {
	for _, actual := range snapshot.Resources {
		if actual.Owner == p.document.Scope.Owner {
			return false
		}
	}
	for i, expected := range p.document.Resources {
		if p.document.Contract.Resources[i].Ownership != "select" && find(snapshot.Resources, expected.Kind, expected.ID) != nil {
			return false
		}
	}
	return true
}

func verifyJournal(files map[string][]byte, result Result) error {
	expected := map[string]bool{}
	if _, ok := files["interrupted.json"]; ok {
		return refused
	}
	for i, entry := range result.Entries {
		prefix := fmt.Sprintf("entry-%04d-", i+1)
		intent := clone(entry)
		intent.State = "uncertain"
		intent.Network = ""
		intent.After = nil
		reply := clone(entry)
		reply.State = "acknowledged"
		reply.After = nil
		for suffix, want := range map[string]Entry{"intent.json": intent, "reply.json": reply} {
			name := prefix + suffix
			expected[name] = true
			var actual Entry
			if decodeResult(files[name], &actual) != nil || !reflect.DeepEqual(actual, want) {
				return refused
			}
		}
		if entry.State == "verified" {
			name := prefix + "observed.json"
			expected[name] = true
			var observed Entry
			if decodeResult(files[name], &observed) != nil || !reflect.DeepEqual(observed, entry) {
				return refused
			}
		} else if entry.State != "acknowledged" {
			return refused
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "entry-") && !expected[name] {
			return refused
		}
	}
	return nil
}
func repliesLease(entries []Entry, files map[string][]byte) Lease {
	if len(entries) == 0 {
		return Lease{}
	}
	var reply Reply
	_ = decode(files[entries[0].Network+"/response.bin"], &reply)
	return reply.Lease
}
