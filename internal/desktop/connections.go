package desktop

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
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
// observation:ID, hub:client, hub:operator, portal, operation:NAME or
// program. Disclosure is the id of the privacy status operation that states
// its data and authorization.
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
// configuration was removed.
type reachingTarget struct {
	ref, name, destination string
	kind                   ConnectionKind
}

// reach records what the operation holding the slot reaches, until the slot
// is released.
func (a *App) reach(target reachingTarget) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		a.reaching = &target
	}
}

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
			rows = append(rows, ConnectionRow{Ref: reaching.ref, Name: reaching.name, Kind: reaching.kind, Destination: reaching.destination,
				State: ConnectionActive, Owner: ownerOf(reaching.ref, reaching.kind), Disclosure: disclosureOf(reaching.kind),
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
			Outcome: summary.LastCheckOutcome, Revision: summary.LastCheckRevision}
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

// ownerOf is where a reached object is edited.
func ownerOf(ref string, kind ConnectionKind) ConnectionOwner {
	switch kind {
	case ConnectionEnvironment:
		return ConnectionOwner{Kind: OwnerEnvironment, ObjectID: ref[len("environment:"):]}
	case ConnectionSource:
		return ConnectionOwner{Kind: OwnerObservation, ObjectID: ref[len("observation:"):]}
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
