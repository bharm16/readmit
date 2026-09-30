package desktop

import (
	"encoding/json/v2"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testlicense"
)

func isolationRegistration(t *testing.T) (string, testisolation.Registry) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	role := func(endpoint string, purpose sendpolicy.Operation) testisolation.Credential {
		return testisolation.Credential{Endpoint: endpoint, Reference: networkaction.Credential{Endpoint: "127.0.0.1:443", Purpose: purpose, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: executable, Arguments: []string{"--never-invoke-" + endpoint}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "fixtures", Revision: "registered-1", Project: "project", Environment: "customer-lab", EnvironmentRevision: "environment-1", Classification: "nonproduction", Tenant: "customer-tenant", Namespace: "synthetic", URL: "https://127.0.0.1", ServerName: "localhost", Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "person", Kind: "patient", Attributes: []string{"name"}}, {ID: "booking", Kind: "appointment", Attributes: []string{"status"}}}, TimeoutMS: 1000}}}
	path := filepath.Join(t.TempDir(), "operator-selected.json")
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, registry
}

func isolationDraft(path string) EnvironmentIsolation {
	return EnvironmentIsolation{Name: "Prepare lab", RegistryFile: path, Adapter: "fixtures", Mode: "reserved-namespace", Resources: []IsolationResource{{Name: "Person", Kind: "patient", Template: "person", Ownership: "create", Attributes: map[string]string{"name": "Synthetic"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "business", Value: "duplicate-authored-value"}}}, {Name: "Booking", Kind: "appointment", Template: "booking", Ownership: "create", DependsOn: []string{"Person"}, Attributes: map[string]string{"status": "booked"}}}, Manual: []IsolationManual{{Name: "Confirm lab", Instructions: "Confirm the customer lab is reserved."}}}
}

func TestIsolationEditorUsesRegisteredTypedResourcesAndNamedDependencies(t *testing.T) {
	path, _ := isolationRegistration(t)
	draft := isolationDraft(path)
	staged, saved, problems := validateIsolationDraft("project", draft, replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}})
	if len(problems) != 0 || len(staged) != 2 {
		t.Fatalf("save preparation: %+v", problems)
	}
	if saved.Resources[0].ID == "" || saved.Resources[1].DependsOn[0] != saved.Resources[0].ID || saved.Manual[0].ID == "" {
		t.Fatalf("named resource bindings lost: %+v", saved)
	}
	if draft.Resources[0].ID != "" || draft.Resources[1].DependsOn[0] != "Person" || draft.Manual[0].ID != "" {
		t.Fatal("validation mutated the retained editor draft")
	}
	contract, registration, policy, err := isolationConfiguration("project", saved, replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(contract)
	registryRaw, _ := json.Marshal(registration)
	policyRaw, _ := json.Marshal(policy)
	if err := testisolation.ValidateConfiguration(raw, registryRaw, policyRaw, testisolation.Options{ParentPlan: networkaction.Digest([]byte("test plan")), Instance: "prepared", Seed: 0}); err != nil {
		t.Fatal(err)
	}
	if contract.Environment != "customer-lab" || contract.Revision != "environment-1" || contract.Resources[0].Identifiers[0].Value != "duplicate-authored-value" || len(contract.Manual) != 1 {
		t.Fatalf("operator scope or authored identity changed: %+v", contract)
	}
}

func TestIsolationEditorRefusesUnsupportedMappingWithoutDroppingDraft(t *testing.T) {
	path, _ := isolationRegistration(t)
	for _, change := range []struct {
		name   string
		mutate func(*EnvironmentIsolation)
	}{
		{"unregistered template", func(d *EnvironmentIsolation) { d.Resources[1].Template = "missing" }},
		{"unregistered attribute", func(d *EnvironmentIsolation) { d.Resources[1].Attributes["sql"] = "arbitrary" }},
		{"future dependency", func(d *EnvironmentIsolation) { d.Resources[0].DependsOn = []string{"Booking"} }},
		{"duplicate resource", func(d *EnvironmentIsolation) { d.Resources[1].Name = d.Resources[0].Name }},
		{"unsupported mode", func(d *EnvironmentIsolation) { d.Mode = "recorded-baseline" }},
		{"claim without exact version", func(d *EnvironmentIsolation) {
			d.Resources[0].Ownership = "claim"
			d.Resources[0].LogicalID = "existing"
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			draft := isolationDraft(path)
			change.mutate(&draft)
			before, _ := json.Marshal(draft, json.Deterministic(true))
			_, _, problems := validateIsolationDraft("project", draft, replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}})
			after, _ := json.Marshal(draft, json.Deterministic(true))
			if len(problems) == 0 || string(before) != string(after) {
				t.Fatalf("unsupported draft lost or accepted: %+v", problems)
			}
		})
	}
}

func TestIsolationEditorPreservesClassificationAndScopeAuthority(t *testing.T) {
	path, _ := isolationRegistration(t)
	for _, classification := range []replay.Classification{replay.Unclassified, replay.Production} {
		if _, _, problems := validateIsolationDraft("project", isolationDraft(path), classification, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}); len(problems) == 0 {
			t.Fatalf("classification acquired isolation authority: %s", classification)
		}
	}
	if _, _, problems := validateIsolationDraft("another-project", isolationDraft(path), replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}); len(problems) == 0 {
		t.Fatal("adapter retargeted to another project")
	}
}

func TestIsolationNamedSelectionsKeepSavedIdentityAfterRenaming(t *testing.T) {
	path, _ := isolationRegistration(t)
	draft := isolationDraft(path)
	draft.Resources[0].ID, draft.Resources[0].Name = "first", "First renamed"
	draft.Resources[1].ID, draft.Resources[1].Name, draft.Resources[1].DependsOn = "second", "first", []string{"first"}
	draft.Resources = append(draft.Resources, IsolationResource{Name: "Later booking", Kind: "appointment", Template: "booking", Ownership: "create", DependsOn: []string{"first"}, Attributes: map[string]string{"status": "booked"}})
	_, saved, problems := validateIsolationDraft("project", draft, replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}})
	if len(problems) != 0 || saved.Resources[2].DependsOn[0] != draft.Resources[0].ID {
		t.Fatalf("a renamed prerequisite retargeted a saved selection: %+v %+v", saved, problems)
	}
	draft.Resources[1].ID = ""
	if _, _, problems := validateIsolationDraft("project", draft, replay.Nonproduction, &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}); len(problems) == 0 {
		t.Fatal("an ambiguous new named selection was silently retargeted")
	}
}

func TestIsolationRegistryChoicesOmitCredentialLocators(t *testing.T) {
	path, _ := isolationRegistration(t)
	choices, err := isolationRegistryChoices(path)
	if err != nil || len(choices) != 1 || len(choices[0].Templates) != 2 {
		t.Fatalf("choices: %+v %v", choices, err)
	}
	raw, _ := json.Marshal(choices)
	var exposed map[string]any
	if json.Unmarshal(raw, &exposed) == nil {
		t.Fatal("registry choices are not a list")
	}
	for _, forbidden := range []string{"locator", "arguments", "command", "authorities", "reference"} {
		if containsJSONKey(raw, forbidden) {
			t.Fatalf("registry metadata exposed %s", forbidden)
		}
	}
}

func containsJSONKey(raw []byte, key string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var found func(any) bool
	found = func(v any) bool {
		switch v := v.(type) {
		case map[string]any:
			for k, item := range v {
				if k == key || found(item) {
					return true
				}
			}
		case []any:
			for _, item := range v {
				if found(item) {
					return true
				}
			}
		}
		return false
	}
	return found(value)
}

func TestIsolationManualConfirmationsAreFreshExactClaims(t *testing.T) {
	review := &IsolationActionReview{Manual: []IsolationManual{{ID: "confirm-one"}, {ID: "confirm-two"}}}
	if !isolationConfirmations(review, []string{"confirm-two", "confirm-one"}) {
		t.Fatal("the current manual claims were refused")
	}
	for _, claimed := range [][]string{nil, {"confirm-one"}, {"confirm-one", "confirm-two", "confirm-one"}, {"confirm-one", "other"}} {
		if isolationConfirmations(review, claimed) {
			t.Fatal("partial, duplicate or unrelated claims authorized setup")
		}
	}
}

func TestIsolationHistoryPublicationRefusesConcurrentClaimsAndLinkedStorage(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, catalog.Folder), 0700); err != nil {
		t.Fatal(err)
	}
	item, err := catalog.NewID()
	if err != nil {
		t.Fatal(err)
	}
	first := isolationState{Schema: isolationStateSchema, Item: item, Revision: "1", Parent: networkaction.Digest([]byte("parent")), Plan: networkaction.Digest([]byte("prepared")), Generation: 1, Instance: "execution"}
	if err := retainIsolationState(root, first, "absent"); err != nil {
		t.Fatal(err)
	}
	next := first
	next.Setup = "managed-outcome"
	if err := retainIsolationState(root, next, isolationHistoryIdentity(first)); err != nil {
		t.Fatal(err)
	}
	if err := retainIsolationState(root, first, isolationHistoryIdentity(first)); err == nil {
		t.Fatal("a stale capability result replaced an attempted setup")
	}
	saved, err := readIsolationState(root, item)
	if err != nil || saved.Setup != next.Setup {
		t.Fatal("the setup intent was lost")
	}
	linkedRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(root, catalog.Folder), filepath.Join(linkedRoot, catalog.Folder)); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := retainIsolationState(linkedRoot, first, "absent"); err == nil {
		t.Fatal("history publication followed a managed-folder link")
	}
}

// Build the customer-store stand-in before any reviewed action starts. A
// reexecuted desktop test binary also initializes every imported engine package
// and the race runtime inside the operation's one-second budget. This provider
// only records the lookup and emits a synthetic role value; production
// deadlines and the instrumented facade/transport remain unchanged.
func isolationCredentialProvider(t *testing.T) string {
	t.Helper()
	const source = `package main
import ("fmt"; "os")
func main() {
 if len(os.Args) != 3 { os.Exit(2) }
 file, err := os.OpenFile(os.Args[1], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
 if err != nil { os.Exit(1) }
 if _, err := file.WriteString("resolved\n"); err != nil { os.Exit(1) }
 if err := file.Close(); err != nil { os.Exit(1) }
 fmt.Print(os.Args[2] + "-token")
}
`
	folder := t.TempDir()
	program := filepath.Join(folder, "provider.go")
	if err := os.WriteFile(program, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(folder, "provider")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	command := exec.Command("go", "build", "-p", "2", "-o", executable, program)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build the synthetic credential store: %v\n%s", err, output)
	}
	return executable
}

// An independent HTTPS target implements its own lease and record store from
// the fixture protocol. It imports no engine allocator, transition or reader.
type isolationEditorTarget struct {
	mu        sync.Mutex
	requests  int
	mutations []string
	version   int
	lease     map[string]any
	resources map[string]map[string]any
}

func (s *isolationEditorTarget) counts() (requests, mutations, resources int, leaseHeld bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, len(s.mutations), len(s.resources), s.lease != nil
}

func (s *isolationEditorTarget) serve(w http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	action := strings.TrimPrefix(request.URL.Path, "/fixture/v1/")
	var payload map[string]any
	if request.Method == "GET" {
		_ = json.Unmarshal([]byte(request.URL.Query().Get("scope")), &payload)
	} else {
		if err := json.UnmarshalRead(request.Body, &payload); err != nil {
			http.Error(w, "malformed", 400)
			return
		}
	}
	scope := payload
	if request.Method != "GET" {
		scope, _ = payload["scope"].(map[string]any)
	}
	credential := "read-token"
	if request.Method != "GET" {
		credential = "setup-token"
		if action == "delete" || action == "lease-release" {
			credential = "cleanup-token"
		}
	}
	if request.Header.Get("Authorization") != "Bearer "+credential {
		http.Error(w, "credential refused", 403)
		return
	}
	response := map[string]any{"schema": "readmit-fixture-adapter/v1", "scope": scope}
	write := func() { w.Header().Set("Content-Type", "application/json"); _ = json.MarshalWrite(w, response) }
	lease := s.lease
	if lease == nil {
		lease = map[string]any{"key": "", "owner": "", "version": ""}
	}
	if action == "capabilities" {
		response["protocol"], response["lease_mode"], response["version_guards"] = "typed-fixture-v1", "exclusive-no-expiry", true
		response["templates"] = []map[string]any{{"id": "person", "kind": "patient", "attributes": []string{"name"}}, {"id": "booking", "kind": "appointment", "attributes": []string{"status"}}}
		write()
		return
	}
	if action == "state" {
		resources := []map[string]any{}
		for _, resource := range s.resources {
			resources = append(resources, resource)
		}
		response["resources"], response["lease"] = resources, lease
		write()
		return
	}
	if action == "lease-acquire" {
		if s.lease != nil {
			http.Error(w, "lease held", 409)
			return
		}
		s.version++
		s.lease = map[string]any{"key": scope["lease_key"], "owner": scope["owner"], "version": strconv.Itoa(s.version)}
		lease = s.lease
	} else {
		requested, _ := payload["lease"].(map[string]any)
		if s.lease == nil || requested["key"] != s.lease["key"] || requested["owner"] != s.lease["owner"] || requested["version"] != s.lease["version"] {
			http.Error(w, "wrong lease", 409)
			return
		}
		if action == "lease-release" {
			if len(s.resources) != 0 {
				http.Error(w, "resources remain", 409)
				return
			}
			s.lease = nil
			lease = map[string]any{"key": "", "owner": "", "version": ""}
		} else {
			resource, _ := payload["resource"].(map[string]any)
			if resource == nil {
				http.Error(w, "resource missing", 400)
				return
			}
			key := resource["kind"].(string) + "/" + resource["id"].(string)
			switch action {
			case "create":
				if s.resources[key] != nil {
					http.Error(w, "resource exists", 409)
					return
				}
				s.version++
				resource["owner"], resource["version"] = scope["owner"], strconv.Itoa(s.version)
				s.resources[key] = resource
			case "delete":
				old := s.resources[key]
				if old == nil || old["owner"] != scope["owner"] || old["version"] != resource["version"] {
					http.Error(w, "resource changed", 409)
					return
				}
				delete(s.resources, key)
			default:
				http.Error(w, "unsupported", 400)
				return
			}
			response["resource"] = resource
		}
	}
	s.mutations = append(s.mutations, action)
	response["outcome"], response["lease"] = "applied", lease
	write()
}

func TestIsolationEditorRealFacadeSavesPassivelyAndRunsReviewedLifecycle(t *testing.T) {
	root := t.TempDir()
	app := New(folderAnswer(root), ShellDocuments{Folder: t.TempDir()})
	if admitted := app.SelectOperationPolicy(testlicense.New(t)); admitted.State != Completed {
		t.Fatal(admitted)
	}
	location := app.ChooseProjectLocation()
	created := app.CreateNamedProject(NewProjectRequest{Name: "Isolation project", Location: location.Location})
	if created.State != Completed {
		t.Fatal(created)
	}
	target := &isolationEditorTarget{resources: map[string]map[string]any{}, mutations: []string{}}
	server := httptest.NewTLSServer(http.HandlerFunc(target.serve))
	defer server.Close()
	path, registry := isolationRegistration(t)
	adapter := &registry.Adapters[0]
	adapter.Project, adapter.URL, adapter.ServerName = created.Context.ProjectID, server.URL, "example.com"
	adapter.Authorities = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	address := server.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	providerLog := filepath.Join(t.TempDir(), "provider-events")
	executable := isolationCredentialProvider(t)
	for _, role := range []*testisolation.Credential{&adapter.Read, &adapter.Setup, &adapter.Cleanup} {
		role.Reference.Endpoint = address
		role.Reference.Locator = networkaction.Provider{Command: executable, Arguments: []string{providerLog, role.Endpoint}}
	}
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	connection := replay.Target{Schema: replay.TargetSchemaV3, Name: "lab", Classification: replay.Nonproduction, TestEndpoint: true, Address: "127.0.0.1:" + port, Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536}
	draft := isolationDraft(path)
	saved := app.SaveItem(SaveItemRequest{Context: created.Context, Kind: EnvironmentItem, Draft: ItemDraft{Name: "Receiving lab", Environment: &connection, SendPolicy: &sendpolicy.Policy{ApprovedDestinations: []string{"127.0.0.1/32"}}, Isolation: &draft}, IntentID: "save-isolation"})
	if saved.Outcome != SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	if reopened := app.OpenItemDraft(ItemRequest{Context: created.Context, Ref: *saved.Saved}); reopened.Draft == nil || reopened.Draft.Isolation == nil || len(reopened.Draft.Isolation.Resources) != 2 {
		t.Fatalf("reopen: %+v", reopened)
	}
	if choices := app.GetIsolationEditor(IsolationEditorRequest{Context: created.Context, RegistryFile: path}); choices.State != Completed || len(choices.Adapters) != 1 {
		t.Fatalf("registered picker: %+v", choices)
	}
	if requests, _, _, _ := target.counts(); requests != 0 {
		t.Fatal("save/reopen/picker contacted the fixture target")
	}
	if _, err := os.Stat(providerLog); !os.IsNotExist(err) {
		t.Fatal("save/reopen/picker resolved credentials")
	}
	beforePreflight := app.PrepareAction(PrepareActionRequest{Context: created.Context, Action: SetupIsolationAction, Items: []ItemRef{*saved.Saved}})
	requests, _, _, _ := target.counts()
	if beforePreflight.Review == nil || beforePreflight.Review.Ready || requests != 0 {
		t.Fatal("setup skipped its explicit capability prerequisite")
	}
	perform := func(action ActionID, intent string, confirmations []string) ReviewedActionResult {
		prepared := app.PrepareAction(PrepareActionRequest{Context: created.Context, Action: action, Items: []ItemRef{*saved.Saved}})
		if prepared.State != Completed || prepared.Review == nil || !prepared.Review.Ready || prepared.Review.Token == "" {
			t.Fatalf("review %s: %+v", action, prepared)
		}
		if prepared.Review.Isolation == nil || prepared.Review.Isolation.RegisteredEnvironment != adapter.Environment || prepared.Review.Destination.Address != server.URL {
			t.Fatalf("scope hidden or retargeted: %+v", prepared.Review)
		}
		result := app.ExecuteReviewedAction(ExecuteActionRequest{Context: created.Context, Token: prepared.Review.Token, IntentID: intent, Decisions: ReviewDecisions{Confirmed: confirmations}})
		if result.State != Completed || result.Isolation == nil {
			requests, mutations, resources, leased := target.counts()
			providerEvents, _ := os.ReadFile(providerLog)
			t.Fatalf("action %s: state=%s reason=%s outcome=%+v requests=%d mutations=%d resources=%d lease=%v provider_calls=%d", action, result.State, result.Reason, result.Isolation, requests, mutations, resources, leased, strings.Count(string(providerEvents), "resolved\n"))
		}
		return result
	}
	perform(PreflightIsolationAction, "preflight", nil)
	if _, mutations, _, _ := target.counts(); mutations != 0 {
		t.Fatal("fixture capability check mutated state")
	}
	opened := app.OpenItemDraft(ItemRequest{Context: created.Context, Ref: *saved.Saved})
	manual := opened.Draft.Isolation.Manual[0].ID
	review := app.PrepareAction(PrepareActionRequest{Context: created.Context, Action: SetupIsolationAction, Items: []ItemRef{*saved.Saved}})
	if review.Review == nil || !review.Review.Ready {
		t.Fatal(review)
	}
	beforeClaims, _, _, _ := target.counts()
	for i, claims := range [][]string{nil, {manual, manual}} {
		refused := app.ExecuteReviewedAction(ExecuteActionRequest{Context: created.Context, Token: review.Review.Token, IntentID: "incomplete-claim-" + strconv.Itoa(i), Decisions: ReviewDecisions{Confirmed: claims}})
		requests, _, _, _ := target.counts()
		if refused.Outcome != ActionRefused || requests != beforeClaims {
			t.Fatal("a missing or duplicate manual claim reached the target")
		}
	}
	adapter.Read.Reference.Generation = "rotated"
	changedRegistry, _ := json.Marshal(registry)
	if err := os.WriteFile(path, changedRegistry, 0600); err != nil {
		t.Fatal(err)
	}
	stale := app.ExecuteReviewedAction(ExecuteActionRequest{Context: created.Context, Token: review.Review.Token, IntentID: "stale-registration", Decisions: ReviewDecisions{Confirmed: []string{manual}}})
	requests, _, _, _ = target.counts()
	if stale.Outcome != ActionStale || requests != beforeClaims {
		t.Fatalf("a changed credential registration reached the target: %+v", stale)
	}
	if preserved := app.OpenItemDraft(ItemRequest{Context: created.Context, Ref: *saved.Saved}); preserved.Draft == nil || preserved.Draft.Isolation == nil || len(preserved.Draft.Isolation.Resources) != 2 {
		t.Fatal("a changed registration dropped the saved draft")
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	setup := perform(SetupIsolationAction, "setup", []string{manual})
	_, mutations, resources, leaseHeld := target.counts()
	if setup.Isolation.Setup != "ready" || setup.Isolation.Cleanup != "not-started" || resources != 2 || !leaseHeld || len(setup.Isolation.Manual) != 1 {
		t.Fatalf("setup was not retained separately: %+v", setup.Isolation)
	}
	setupMutations := mutations
	reconciled := perform(ReconcileIsolationAction, "reconcile", nil)
	_, mutations, _, _ = target.counts()
	if reconciled.Isolation.Setup != "reconciled-not-ready" || mutations != setupMutations {
		t.Fatal("reconciliation repeated resource mutations or setup claims")
	}
	cleanupReview := app.PrepareAction(PrepareActionRequest{Context: created.Context, Action: CleanupIsolationAction, Items: []ItemRef{*saved.Saved}})
	if cleanupReview.Review == nil || !cleanupReview.Review.Ready {
		t.Fatal(cleanupReview)
	}
	target.mu.Lock()
	for _, resource := range target.resources {
		if resource["kind"] == "appointment" {
			resource["version"] = "externally-updated"
		}
	}
	target.mu.Unlock()
	refusedCleanup := app.ExecuteReviewedAction(ExecuteActionRequest{Context: created.Context, Token: cleanupReview.Review.Token, IntentID: "changed-resource-cleanup"})
	_, mutations, resources, _ = target.counts()
	if refusedCleanup.State != Failed || refusedCleanup.Isolation == nil || refusedCleanup.Isolation.Cleanup == "complete" || mutations != setupMutations || resources != 2 {
		t.Fatalf("cleanup deleted an externally changed version: %+v", refusedCleanup)
	}
	perform(ReconcileIsolationAction, "reconcile-current-versions", nil)
	currentCleanup := app.PrepareAction(PrepareActionRequest{Context: created.Context, Action: CleanupIsolationAction, Items: []ItemRef{*saved.Saved}})
	if currentCleanup.Review == nil || !currentCleanup.Review.Ready {
		t.Fatal(currentCleanup)
	}
	if currentCleanup.Review.Isolation.Effects[0].Version != "externally-updated" {
		t.Fatal("fresh cleanup review hid the independently observed current version")
	}
	cleaned := perform(CleanupIsolationAction, "cleanup", nil)
	_, _, resources, leaseHeld = target.counts()
	if cleaned.Isolation.Cleanup != "complete" || !cleaned.Isolation.Complete || resources != 0 || leaseHeld || len(cleaned.Isolation.Manual) != 0 {
		t.Fatalf("cleanup did not preserve its own meaning: %+v", cleaned.Isolation)
	}
}
