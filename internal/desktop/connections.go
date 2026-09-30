package desktop

import (
	"cmp"
	"context"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The connection inventory. Security lists the external access actually
// configured — the open project's saved environments and observation sources,
// the selected team and operator hubs and the selected customer portal — with
// the state each is in now, and every operation reaching outside the window
// while it runs, even one whose configuration was removed meanwhile. It is
// built from saved configuration and this process's own runtime state alone:
// listing resolves no credential, looks no name up, opens no connection,
// runs no program and does not claim the operation slot, so an operation that
// holds the slot is exactly what it can report as active.

// ConnectionState is what a connection is now, a closed vocabulary.
//
//	active        an operation of this window is reaching it now
//	connected     a session this window holds is connected now
//	disconnected  a session this window held was ended
//	checked       configured; an explicit check or collection reached it at CheckedAt
//	not-checked   configured; nothing this window did has reached it
//	unavailable   the configuration cannot be read; Reason says why
type ConnectionState string

const (
	ConnectionActive       ConnectionState = "active"
	ConnectionConnected    ConnectionState = "connected"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionChecked      ConnectionState = "checked"
	ConnectionNotChecked   ConnectionState = "not-checked"
	ConnectionUnavailable  ConnectionState = "unavailable"
)

// ConnectionKind is what a connection reaches.
type ConnectionKind string

const (
	ConnectionEnvironment ConnectionKind = "environment"
	ConnectionSource      ConnectionKind = "source"
	ConnectionTeam        ConnectionKind = "team"
	ConnectionRunner      ConnectionKind = "runner"
	ConnectionPortal      ConnectionKind = "portal"
	ConnectionRun         ConnectionKind = "run"
	ConnectionProgram     ConnectionKind = "program"
)

// ConnectionAction is an action a row authorizes. Edit opens the owner's own
// setup; Disconnect is offered only for a session this window holds
// connected now.
type ConnectionAction string

const (
	ConnectionEdit       ConnectionAction = "edit"
	ConnectionDisconnect ConnectionAction = "disconnect"
)

// ConnectionOwner names where a connection is edited: the kind of object or
// setting that owns it, and the object's identity when it is a project
// object. The window maps it to its own route.
type ConnectionOwner struct {
	Kind     ConnectionOwnerKind `json:"kind"`
	ObjectID string              `json:"object_id,omitzero"`
}

// ConnectionOwnerKind is the owner of a connection's setup: a project
// object, a Settings category, or the running activity a row names.
type ConnectionOwnerKind string

const (
	OwnerEnvironment ConnectionOwnerKind = "environment"
	OwnerObservation ConnectionOwnerKind = "observation"
	OwnerTeam        ConnectionOwnerKind = "team"
	OwnerLicense     ConnectionOwnerKind = "license"
	OwnerSource      ConnectionOwnerKind = "source"
	OwnerRunner      ConnectionOwnerKind = "runner"
	OwnerRun         ConnectionOwnerKind = "run"
	OwnerPortal      ConnectionOwnerKind = "portal"
	OwnerProgram     ConnectionOwnerKind = "program"
)

// ConnectionDetail is the technical configuration a row's selection shows.
type ConnectionDetail struct {
	Data           string `json:"data,omitzero"`
	Authorization  string `json:"authorization,omitzero"`
	Protocol       string `json:"protocol,omitzero"`
	Version        string `json:"version,omitzero"`
	Authentication string `json:"authentication,omitzero"`
	Validator      string `json:"validator,omitzero"`
	Transport      string `json:"transport,omitzero"`
	Classification string `json:"classification,omitzero"`
	SourceType     string `json:"source_type,omitzero"`
	Outcome        string `json:"outcome,omitzero"`
	Revision       string `json:"revision,omitzero"`
	ConfigPath     string `json:"config_path,omitzero"`
	SignedIn       bool   `json:"signed_in"`
	Operation      string `json:"operation,omitzero"`
}

// ConnectionRow is one configured connection or one operation reaching
// outside the window now. Ref is stable across reads: environment:ID,
// observation:ID, hub:client, hub:operator, runner:config, portal,
// operation:NAME or program. Disclosure is the id of the privacy status
// operation that states its data and authorization.
type ConnectionRow struct {
	Ref         string             `json:"ref"`
	Name        string             `json:"name"`
	Kind        ConnectionKind     `json:"kind"`
	Destination string             `json:"destination"`
	State       ConnectionState    `json:"state"`
	Reason      string             `json:"reason,omitzero"`
	CheckedAt   *string            `json:"checked_at"`
	LastSeen    *string            `json:"last_seen"`
	Owner       ConnectionOwner    `json:"owner"`
	Disclosure  string             `json:"disclosure"`
	Actions     []ConnectionAction `json:"actions"`
	Detail      ConnectionDetail   `json:"detail"`
}

// ConnectionsResult is the inventory, active rows first. ProjectReason says
// why the open project's saved connections could not be read; the rest of
// the inventory is still answered.
type ConnectionsResult struct {
	State         State           `json:"state"`
	Reason        string          `json:"reason,omitzero"`
	Context       RequestContext  `json:"context"`
	Rows          []ConnectionRow `json:"rows"`
	ProjectReason string          `json:"project_reason,omitzero"`
}

// reachingTarget is what an operation holding the slot is reaching, recorded
// by the operation itself, so the inventory still names it after its
// configuration was removed. An empty ref is the operation itself,
// operation:NAME, named by the object it carries.
type reachingTarget struct {
	ref, name, destination string
	kind                   ConnectionKind
}

// reach records what the operation holding the slot reaches, until the slot
// is released. Each operation that reaches outside the window records it as
// soon as it has resolved what it reaches, before it reaches it.
func (a *App) reach(target reachingTarget) {
	a.mu.Lock()
	if a.running {
		if target.ref == "" {
			target.ref = "operation:" + a.operation
		}
		a.reaching = &target
	}
	reached := a.reached
	a.mu.Unlock()
	if reached != nil {
		reached()
	}
}

// specNamed is what an active row calls a run of the workspace spec entry:
// the name the spec declares, or the operation's own activity when the spec
// cannot be read. A file's name is never the label.
func specNamed(workspace, entry, operation string) string {
	if spec, err := testrunner.ReadSpec(filepath.Join(workspace, filepath.FromSlash(entry))); err == nil && spec.Name != "" {
		return spec.Name
	}
	return activityTitle(operation)
}

// configuredRunner is the runner configuration this window last read, saved
// or enrolled with: parsed values already in hand, so listing reads no file.
type configuredRunner struct {
	path, name, hub string
	// enrolledAt is when an enrollment with it last succeeded.
	enrolledAt *time.Time
}

// runnerName is what the inventory calls a runner: the environment its
// configuration serves.
func runnerName(environment string) string {
	return "Runner " + environment
}

// configureRunner records the runner configuration at path as the one this
// window last configured, keeping its enrollment time when it is the same
// configuration.
func (a *App) configureRunner(path, environment, hub string, enrolled bool) {
	a.runnerMu.Lock()
	defer a.runnerMu.Unlock()
	next := &configuredRunner{path: path, name: runnerName(environment), hub: hostOf(hub)}
	if previous := a.runnerConfig; previous != nil && previous.path == path && previous.hub == next.hub {
		next.enrolledAt = previous.enrolledAt
	}
	if enrolled {
		at := a.now()
		next.enrolledAt = &at
	}
	a.runnerConfig = next
}

// reachRunner records that the runner operation holding the slot reaches the
// hub of the configuration at path: the runner row when it is the one this
// window configured, or else the operation named by that configuration.
func (a *App) reachRunner(path, environment, hub string) {
	a.runnerMu.Lock()
	configured := a.runnerConfig != nil && a.runnerConfig.path == path
	a.runnerMu.Unlock()
	target := reachingTarget{name: runnerName(environment), kind: ConnectionRunner, destination: hostOf(hub)}
	if configured {
		target.ref = runnerRef
	}
	a.reach(target)
}

// runnerRef is the runner row's ref.
const runnerRef = "runner:config"

// sessionMark is when this window last connected or ended a session.
type sessionMark struct {
	at    time.Time
	ended bool
}

// markSession records a session connected or ended now.
func (a *App) markSession(ref string, ended bool) {
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()
	if a.sessions == nil {
		a.sessions = map[string]sessionMark{}
	}
	a.sessions[ref] = sessionMark{at: a.now(), ended: ended}
}

func (a *App) sessionOf(ref string) (sessionMark, bool) {
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()
	mark, ok := a.sessions[ref]
	return mark, ok
}

// ListConnections reports every configured connection and every operation
// reaching outside the window now. See the file comment for what it never
// does.
func (a *App) ListConnections(request RequestContext) ConnectionsResult {
	result := ConnectionsResult{State: Completed, Context: request, Rows: []ConnectionRow{}}
	a.mu.Lock()
	holder, programs := a.operation, a.programs
	var reaching *reachingTarget
	if a.running && a.reaching != nil {
		held := *a.reaching
		reaching = &held
	}
	a.mu.Unlock()

	rows := []ConnectionRow{}
	if request.Project != "" {
		project, reason := a.projectConnections(request)
		rows = append(rows, project...)
		result.ProjectReason = reason
	}
	rows = append(rows, a.hubConnections(holder)...)
	if row, ok := a.runnerConnection(holder, reaching != nil); ok {
		rows = append(rows, row)
	}
	if row, ok := a.portalConnection(); ok {
		rows = append(rows, row)
	}

	// The operation holding the slot: its own row when it recorded what it
	// reaches, or the row of the activity its name belongs to.
	attributed := false
	if reaching != nil {
		for i := range rows {
			if rows[i].Ref == reaching.ref {
				rows[i].State, rows[i].Reason, rows[i].Detail.Operation = ConnectionActive, "", holder
				attributed = true
			}
		}
		if !attributed {
			disclosure := disclosureOf(reaching.kind)
			if activity, _, ok := activityOf(holder); ok {
				disclosure = activity
			}
			rows = append(rows, ConnectionRow{Ref: reaching.ref, Name: reaching.name, Kind: reaching.kind, Destination: reaching.destination,
				State: ConnectionActive, Owner: ownerOf(reaching.ref, reaching.kind), Disclosure: disclosure,
				Actions: []ConnectionAction{}, Detail: ConnectionDetail{Operation: holder}})
			attributed = true
		}
	}
	if !attributed {
		for i := range rows {
			if rows[i].State == ConnectionActive {
				attributed = true
			}
		}
	}
	if !attributed && holder != "" {
		if activity, kind, ok := activityOf(holder); ok {
			rows = append(rows, ConnectionRow{Ref: "operation:" + holder, Name: activityTitle(activity), Kind: kind,
				State: ConnectionActive, Owner: ConnectionOwner{Kind: ConnectionOwnerKind(kind)}, Disclosure: activity, Actions: []ConnectionAction{},
				Detail: ConnectionDetail{Operation: holder}})
		}
	}
	if programs > 0 {
		rows = append(rows, ConnectionRow{Ref: "program", Name: activityTitle(declaredProgramActivity), Kind: ConnectionProgram,
			Destination: "Program", State: ConnectionActive, Owner: ConnectionOwner{Kind: OwnerProgram},
			Disclosure: declaredProgramActivity, Actions: []ConnectionAction{}, Detail: ConnectionDetail{Operation: holder}})
	}

	slices.SortStableFunc(rows, func(x, y ConnectionRow) int {
		if active := cmp.Compare(boolRank(x.State != ConnectionActive), boolRank(y.State != ConnectionActive)); active != 0 {
			return active
		}
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.Ref, y.Ref))
	})
	result.Rows = rows
	if len(rows) == 0 && result.ProjectReason == "" {
		result.State, result.Reason = Empty, "no connection is configured"
	}
	return result
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// projectConnections are the open project's saved environments and
// observation sources, read from the saved files without recording anything.
func (a *App) projectConnections(request RequestContext) ([]ConnectionRow, string) {
	loaded, declined := a.loadCatalog(context.Background(), request, false)
	if loaded == nil {
		return nil, declined.reason
	}
	rows := []ConnectionRow{}
	for _, item := range loaded.document.Items {
		if loaded.removed(item) {
			continue
		}
		switch ItemKind(item.Kind) {
		case EnvironmentItem:
			rows = append(rows, environmentConnection(loaded.read(item)))
			if members, err := loaded.environmentOf(item); err == nil && members.isolation != nil {
				plan := members.isolation
				row := ConnectionRow{Ref: "fixture:" + item.ID, Name: plan.Name, Kind: ConnectionEnvironment, State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerEnvironment, ObjectID: item.ID}, Disclosure: "environment", Actions: []ConnectionAction{ConnectionEdit}, Detail: ConnectionDetail{Protocol: "fixture-adapter", Operation: "read, authorized setup, guarded cleanup"}, Reason: "Customer fixture adapter; side effects require their explicit reviews"}
				choices, err := isolationRegistryChoices(plan.RegistryFile)
				if err != nil {
					row.State = ConnectionUnavailable
					row.Reason = "The selected fixture registration is unavailable"
				} else {
					for _, choice := range choices {
						if choice.ID == plan.Adapter {
							row.Destination = choice.Address
							row.Detail.Classification = "nonproduction"
							row.Detail.Revision = choice.Revision
						}
					}
					if row.Destination == "" {
						row.State = ConnectionUnavailable
						row.Reason = "The selected fixture adapter is no longer registered"
					}
				}
				if state, err := readIsolationState(loaded.root, item.ID); err == nil && state.Outcome != nil {
					row.CheckedAt = &state.Outcome.CheckedAt
					if row.State != ConnectionUnavailable {
						row.State = ConnectionChecked
						row.Detail.Outcome = state.Outcome.Setup + " / " + state.Outcome.Cleanup
					}
				}
				rows = append(rows, row)
			}
		case ObservationItem:
			rows = append(rows, loaded.observationConnection(item))
		}
	}
	return rows, ""
}

func environmentConnection(item CatalogItem) ConnectionRow {
	row := ConnectionRow{Ref: "environment:" + item.Ref.ID, Name: item.Name, Kind: ConnectionEnvironment,
		State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerEnvironment, ObjectID: item.Ref.ID},
		Disclosure: "environment", Actions: []ConnectionAction{ConnectionEdit}}
	summary := item.Summary.Environment
	switch {
	case item.Availability != ItemAvailable || summary == nil:
		row.State, row.Reason = ConnectionUnavailable, item.Reason
	default:
		row.Destination = summary.Address
		row.Detail = ConnectionDetail{Transport: summary.Transport, Classification: summary.Classification,
			Protocol: summary.Protocol, Version: summary.Version, Authentication: summary.Authentication, Validator: summary.Validator,
			Outcome: summary.LastCheckOutcome, Revision: summary.LastCheckRevision}
		if summary.Protocol == "fhir-r4" {
			row.Detail.Data = "Test connection opens TLS only. Test authorization reads the registered key and requests a token. Check capabilities reads public metadata. Observe reads the reviewed typed resource search; no clinical payload is sent by these actions."
			row.Detail.Authorization = "The saved connection revision, explicit operation review, allowed destinations and registered SMART scope authorize only that action. Local offline validation is a separate installed capability."
		}
		if summary.LastCheckedAt != nil {
			row.State, row.CheckedAt = ConnectionChecked, summary.LastCheckedAt
		}
	}
	return row
}

func (c *loadedCatalog) observationConnection(item catalog.Item) ConnectionRow {
	read := c.read(item)
	row := ConnectionRow{Ref: "observation:" + item.ID, Name: read.Name, Kind: ConnectionSource,
		State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerObservation, ObjectID: item.ID},
		Disclosure: "observe", Actions: []ConnectionAction{ConnectionEdit}}
	summary := read.Summary.Observation
	if read.Availability != ItemAvailable || summary == nil {
		row.State, row.Reason = ConnectionUnavailable, read.Reason
		return row
	}
	row.Detail = ConnectionDetail{SourceType: summary.SourceType, Outcome: summary.LatestStatus}
	if paths, availability, _ := c.backing(item); availability == ItemAvailable {
		if _, held := paths["setup"]; held {
			if draft, err := openConnectedObservation(paths); err == nil && draft.Connected.FHIR != nil {
				row.Detail.Protocol = "fhir-r4"
				row.Detail.Version = "4.0.1"
				if i := c.document.Find(draft.Connected.Environment); i >= 0 {
					environment := c.read(c.document.Items[i])
					if summary := environment.Summary.Environment; summary != nil {
						row.Destination = summary.Address
						row.Detail.Authentication = summary.Authentication
						row.Detail.Validator = summary.Validator
					}
				}
				if summary.LatestCollection != nil {
					row.State = ConnectionChecked
					row.CheckedAt = summary.LatestCollection
					row.Detail.Outcome = summary.LatestStatus
				}
				return row
			}
		}
		if source, _, err := operation.ValidateObservationSource(paths[primaryRole(ObservationItem)]); err == nil {
			row.Destination = sourceDestination(source)
		}
	}
	if summary.LatestCollection != nil {
		row.State, row.CheckedAt = ConnectionChecked, summary.LatestCollection
	}
	return row
}

// hubConnections are the selected team hub and operator-only hub, read from
// the connection objects this window holds. Nothing is asked of either hub.
func (a *App) hubConnections(holder string) []ConnectionRow {
	status := a.hub.Status()
	a.hubOperatorMu.Lock()
	operatorConfig, operatorPath, operatorClient := a.hubOperatorConfig, a.hubOperatorConfigPath, a.hubOperatorClient
	a.hubOperatorMu.Unlock()
	_, operating := holding(hubOperations, holder)
	rows := []ConnectionRow{}
	if status.Config != nil || status.Refusal != "" {
		row := ConnectionRow{Ref: "hub:client", Name: "Team hub", Kind: ConnectionTeam, State: ConnectionNotChecked,
			Owner: ConnectionOwner{Kind: OwnerTeam}, Disclosure: "hub", Actions: []ConnectionAction{ConnectionEdit},
			Detail: ConnectionDetail{ConfigPath: status.ConfigPath, SignedIn: status.SignedIn()}}
		switch {
		case status.Config == nil:
			row.State, row.Reason = ConnectionUnavailable, status.Refusal
		case operating:
			row.Destination, row.State, row.Detail.Operation = hostOf(status.Config.Hub), ConnectionActive, holder
		case status.Client != nil:
			row.Destination, row.State = hostOf(status.Config.Hub), ConnectionConnected
			row.Actions = append(row.Actions, ConnectionDisconnect)
		default:
			row.Destination = hostOf(status.Config.Hub)
			if mark, ok := a.sessionOf(row.Ref); ok && mark.ended {
				row.State = ConnectionDisconnected
			}
		}
		if row.State != ConnectionUnavailable {
			if mark, ok := a.sessionOf(row.Ref); ok {
				row.LastSeen = catalogStamp(mark.at)
			}
		}
		rows = append(rows, row)
		operating = operating && status.Config == nil
	}
	if operatorConfig != nil {
		row := ConnectionRow{Ref: "hub:operator", Name: "Operator hub", Kind: ConnectionTeam, Destination: hostOf(operatorConfig.Hub),
			State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerTeam}, Disclosure: "hub", Actions: []ConnectionAction{ConnectionEdit},
			Detail: ConnectionDetail{ConfigPath: operatorPath}}
		switch {
		case operating:
			row.State, row.Detail.Operation = ConnectionActive, holder
		case operatorClient != nil:
			row.State = ConnectionConnected
			row.Actions = append(row.Actions, ConnectionDisconnect)
		default:
			if mark, ok := a.sessionOf(row.Ref); ok && mark.ended {
				row.State = ConnectionDisconnected
			}
		}
		if mark, ok := a.sessionOf(row.Ref); ok {
			row.LastSeen = catalogStamp(mark.at)
		}
		rows = append(rows, row)
	}
	return rows
}

// runnerConnection is the runner configuration this window last read, saved
// or enrolled with, from what it recorded then. It is active while a runner
// operation holds the slot and recorded nothing else it reaches.
func (a *App) runnerConnection(holder string, recorded bool) (ConnectionRow, bool) {
	a.runnerMu.Lock()
	configured := a.runnerConfig
	a.runnerMu.Unlock()
	if configured == nil {
		return ConnectionRow{}, false
	}
	row := ConnectionRow{Ref: runnerRef, Name: configured.name, Kind: ConnectionRunner, Destination: configured.hub,
		State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerRunner}, Disclosure: "runner",
		Actions: []ConnectionAction{ConnectionEdit}, Detail: ConnectionDetail{ConfigPath: configured.path}}
	if configured.enrolledAt != nil {
		row.State, row.CheckedAt = ConnectionChecked, catalogStamp(*configured.enrolledAt)
	}
	if activity, _, ok := activityOf(holder); ok && activity == "runner" && !recorded {
		row.State, row.Detail.Operation = ConnectionActive, holder
	}
	return row, true
}

// portalConnection is the customer portal, listed only while a destinations
// file is selected. The portal is opened in the person's browser; this
// window never requests it.
func (a *App) portalConnection() (ConnectionRow, bool) {
	a.commercialMu.Lock()
	path := a.commercialConfigPath
	a.commercialMu.Unlock()
	if path == "" {
		return ConnectionRow{}, false
	}
	return ConnectionRow{Ref: "portal", Name: "Customer portal", Kind: ConnectionPortal, Destination: "Browser",
		State: ConnectionNotChecked, Owner: ConnectionOwner{Kind: OwnerLicense}, Disclosure: "portal",
		Actions: []ConnectionAction{ConnectionEdit}, Detail: ConnectionDetail{ConfigPath: path}}, true
}

func hostOf(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return address
}

func catalogStamp(at time.Time) *string {
	stamp := catalog.Stamp(at)
	return &stamp
}

// ownerOf is where a reached object is edited: the saved object its ref
// names, whatever the operation reaching it is, or else the kind's owner.
func ownerOf(ref string, kind ConnectionKind) ConnectionOwner {
	if id, ok := strings.CutPrefix(ref, "environment:"); ok {
		return ConnectionOwner{Kind: OwnerEnvironment, ObjectID: id}
	}
	if id, ok := strings.CutPrefix(ref, "observation:"); ok {
		return ConnectionOwner{Kind: OwnerObservation, ObjectID: id}
	}
	return ConnectionOwner{Kind: ConnectionOwnerKind(kind)}
}

// disclosureOf is the privacy status operation a kind's data and
// authorization are stated under.
func disclosureOf(kind ConnectionKind) string {
	switch kind {
	case ConnectionEnvironment:
		return "environment"
	case ConnectionSource:
		return "observe"
	case ConnectionTeam:
		return "hub"
	}
	return string(kind)
}

// activityOf is the disclosed activity a named operation belongs to, and the
// connection kind that activity is.
func activityOf(holder string) (string, ConnectionKind, bool) {
	kinds := map[string]ConnectionKind{"run": ConnectionRun, "runner": ConnectionRunner, "capture": ConnectionSource,
		"observe": ConnectionSource, "environment": ConnectionEnvironment}
	for _, activity := range slotActivities {
		if _, ok := holding(activity.ops, holder); ok {
			return activity.id, kinds[activity.id], true
		}
	}
	if _, ok := holding(hubOperations, holder); ok {
		return "hub", ConnectionTeam, true
	}
	return "", "", false
}

// activityTitle is the privacy status's own name for an activity.
func activityTitle(id string) string {
	for _, disclosed := range privacyStatus.Operations {
		if disclosed.ID == id {
			return disclosed.Activity
		}
	}
	return id
}

// savedObjectOf is the project's saved object of the kind given whose member
// in role is the project entry given, when the folder at root is a project
// that records one. It reads the catalog and nothing it names.
func savedObjectOf(root string, kind ItemKind, role, entry string) (catalog.Item, bool) {
	store, err := catalog.Open(root)
	if err != nil {
		return catalog.Item{}, false
	}
	document, present, err := store.Read()
	if err != nil || !present {
		return catalog.Item{}, false
	}
	entry = filepath.ToSlash(filepath.Clean(entry))
	for _, item := range document.Items {
		current := item.Current()
		if item.Kind != string(kind) || item.RemovedAt != "" || current == nil {
			continue
		}
		for _, member := range current.Members {
			if member.Role == role && filepath.ToSlash(filepath.Clean(member.Path)) == entry {
				return item, true
			}
		}
	}
	return catalog.Item{}, false
}

// reachTarget records that the operation holding the slot reaches target,
// read from the project entry given under root: the saved environment whose
// target that entry is, or else the operation itself, named by what it
// carries or by the target's own name.
func (a *App) reachTarget(root, entry, name string, kind ConnectionKind, target replay.Target) {
	if item, ok := savedObjectOf(root, EnvironmentItem, primaryRole(EnvironmentItem), entry); ok {
		a.reach(reachingTarget{ref: "environment:" + item.ID, name: cmp.Or(item.Name, target.Name), kind: kind, destination: target.Address})
		return
	}
	a.reach(reachingTarget{name: cmp.Or(name, target.Name, target.Address), kind: kind, destination: target.Address})
}

// reachObservation records that the collection holding the slot reads the
// observation source at the project entry given under root: the saved
// observation whose source that entry is, or else the collection itself,
// named by the source it declares.
func (a *App) reachObservation(root, entry, path string) {
	target := reachingTarget{kind: ConnectionSource}
	if source, _, err := operation.ValidateObservationSource(path); err == nil {
		target.name, target.destination = source.Observes.Identity, sourceDestination(source)
	}
	if item, ok := savedObjectOf(root, ObservationItem, "source", entry); ok {
		target.ref, target.name = "observation:"+item.ID, cmp.Or(item.Name, target.name)
	}
	a.reach(target)
}
