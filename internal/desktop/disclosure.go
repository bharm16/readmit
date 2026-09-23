package desktop

import (
	"time"
)

// The disclosure states. The privacy region's per-operation table is declared
// statically by the shell; this file answers the live half of it — whether each
// deliberately configurable activity is connected, offline, configured or idle
// right now — from state the window already holds, and contacts nothing: the
// answer is read out of the hub's own connection objects, the retained
// commercial selection and the operation slot, never by probing a destination.
//
// The states are a closed vocabulary the interface renders word by word:
//
//	idle            not happening now, so nothing is connected
//	active          happening now, reaching its disclosed destination
//	not-configured  never set up; nothing to connect
//	offline         set up, and not connected now
//	connected       connected now
//	configured      selected; the activity itself belongs to the browser, not this window
const (
	disclosureIdle          = "idle"
	disclosureActive        = "active"
	disclosureNotConfigured = "not-configured"
	disclosureOffline       = "offline"
	disclosureConnected     = "connected"
	disclosureConfigured    = "configured"
)

// DisclosureState is one disclosed activity's current state and the sentence
// that says what it means.
type DisclosureState struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// DisclosureStatusResult carries one state and the per-operation answers.
type DisclosureStatusResult struct {
	State  State             `json:"state"`
	Reason string            `json:"reason,omitzero"`
	States []DisclosureState `json:"states,omitzero"`
}

func (r *DisclosureStatusResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// DisclosureStatus reports, for every operation the privacy status discloses,
// whether it is active, connected or offline right now. It reads this window's
// own state and nothing else, so a person can check what is and is not
// connected without anything being probed, contacted or reconnected to find
// out.
//
// It does not claim the operation slot. A run, a capture, a collection, an
// observation, a reduction or runner execution is active while its named
// operation holds the slot, so a read that waited for the slot could never say
// one is. The slot's holder is read once, under the slot's own lock, and the
// hub and portal states are each read under the lock that guards them; the
// read starts and changes nothing. An operation that holds the slot without a
// name — a connectivity check or a fixture reset among them — cannot be
// attributed to any one activity, so while one does the answer is busy rather
// than an idle state it cannot vouch for.
func (a *App) DisclosureStatus() DisclosureStatusResult {
	holder, held := a.slotHolder()
	if held && holder == "" {
		var result DisclosureStatusResult
		result.refuse(Busy, busyRefusal.reason)
		return result
	}
	states := make([]DisclosureState, 0, len(slotActivities)+2)
	for _, activity := range slotActivities {
		states = append(states, slotDisclosure(activity, holder))
	}
	states = append(states, a.hubDisclosure(), a.portalDisclosure())
	return DisclosureStatusResult{State: Completed, States: states}
}

// operating reports whether the operation slot is held by the named operation.
func (a *App) operating(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.operation == name
}

// slotHolder names the operation holding the slot now, and reports whether the
// slot is held at all: an operation can hold it without a name.
func (a *App) slotHolder() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.operation, a.running
}

// slotActivity is one disclosed activity whose network use happens only while
// its operation holds the slot, under the names its operation is started with.
type slotActivity struct {
	id     string
	ops    []string
	active string
	idle   string
}

// slotActivities are those activities, each with the sentence for its active
// and its idle state.
var slotActivities = []slotActivity{
	{
		id:     "run",
		ops:    []string{runOperation},
		active: "A durable run is executing now; it sends only to the target its preflight named, and the run panel holds its progress.",
		idle:   "No run is in progress. A run sends only while it executes; selecting, preflighting and reading history connect to nothing.",
	},
	{
		id:     "runner",
		ops:    []string{"runner"},
		active: "Runner execution is in progress now; it reaches the hub to fetch scheduled work and report run records.",
		idle:   "No recurring execution is in progress. An enrolled runner connects to the hub only while it fetches or reports work.",
	},
	{
		id:     "capture",
		ops:    []string{"capture", "collect"},
		active: "A capture or collection is in progress now, on the sources and local port its registration and policy declare.",
		idle:   "No capture or collection is in progress. Nothing listens and no source is read until you start one.",
	},
	{
		id:     "environment",
		ops:    []string{"reduction"},
		active: "A controlled reduction is in progress now; the fixture resets it performs reach the recorded environment its target names.",
		idle:   "No connectivity check, fixture reset or reduction is in progress. Recording a named environment, a send policy or a reset plan reads and writes local files only.",
	},
	{
		id:     "observe",
		ops:    []string{"observation"},
		active: "An observation window is open now, reading the source and scope its validated pair declares.",
		idle:   "No observation window is open. Authoring a source or window reads local files only.",
	},
}

// slotDisclosure answers one slot-held activity: active when the slot's holder
// is one of the operations it happens under, idle otherwise.
func slotDisclosure(activity slotActivity, holder string) DisclosureState {
	for _, op := range activity.ops {
		if holder == op {
			return DisclosureState{ID: activity.id, State: disclosureActive, Detail: activity.active}
		}
	}
	return DisclosureState{ID: activity.id, State: disclosureIdle, Detail: activity.idle}
}

// hubDisclosure answers for the customer artifact hub, from the hub's own
// connection objects: no configuration, a configuration without a connection,
// or a live connection with or without a current sign-in session.
func (a *App) hubDisclosure() DisclosureState {
	a.hubMu.Lock()
	cfg := a.hubConfig
	client := a.hubClient
	session := a.hubSession
	a.hubMu.Unlock()
	switch {
	case cfg == nil:
		return DisclosureState{ID: "hub", State: disclosureNotConfigured,
			Detail: "No hub configuration is selected. Every hub operation is unavailable and nothing is connected."}
	case client == nil:
		return DisclosureState{ID: "hub", State: disclosureOffline,
			Detail: "A hub configuration is selected; not connected. Connecting is a deliberate action."}
	case session != nil && !session.IsExpired(time.Now()):
		return DisclosureState{ID: "hub", State: disclosureConnected,
			Detail: "Connected to the configured hub, with a current sign-in session."}
	default:
		return DisclosureState{ID: "hub", State: disclosureConnected,
			Detail: "Connected to the configured hub; not signed in."}
	}
}

// portalDisclosure answers for the commercial portal. The destination is a
// browser address this window never requests, so its truthful state is whether
// the operator's destinations file has been selected at all.
func (a *App) portalDisclosure() DisclosureState {
	a.commercialMu.Lock()
	path := a.commercialConfigPath
	a.commercialMu.Unlock()
	if path == "" {
		return DisclosureState{ID: "portal", State: disclosureNotConfigured,
			Detail: "No commercial destinations file is selected, so no portal address is shown anywhere."}
	}
	return DisclosureState{ID: "portal", State: disclosureConfigured,
		Detail: "A portal destination is configured. This window makes no request to it; opening it is a separate deliberate act in your browser."}
}
