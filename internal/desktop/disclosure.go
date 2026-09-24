package desktop

import (
	"time"
)

// The disclosure states. The privacy region's per-operation table is declared
// statically by the shell; this file answers the live half of it — whether each
// deliberately configurable activity is connected, offline, configured or idle
// right now — from state the window already holds, and contacts nothing: the
// answer is read out of the hub's own connection objects, the retained
// commercial selection, the operation slot and the operator-declared programs
// running under it, never by probing a destination.
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
// It does not claim the operation slot. Every operation that can reach a
// network destination or change a target runs under a name (runNamed), and
// the activity that name belongs to is active while it holds the slot, so a
// read that waited for the slot could never say one is. The slot's holder is
// read once, under the slot's own lock, and the hub and portal states are each
// read under the lock that guards them; the read starts and changes nothing.
// An operation that holds the slot without a name is local work that no
// activity can be attributed to, so while one does the answer is busy rather
// than an idle state the status did not establish.
//
// An operator-declared program reports itself while it runs, and the count of
// those running is read together with the slot's holder, so the
// declared-program row is active exactly while one runs and names the
// operation running it. What the program itself reaches is outside this
// window: the row says a program is running, never where it connects.
func (a *App) DisclosureStatus() DisclosureStatusResult {
	holder, held, programs := a.slotState()
	if held && holder == "" {
		var result DisclosureStatusResult
		result.refuse(Busy, busyRefusal.reason)
		return result
	}
	states := make([]DisclosureState, 0, len(slotActivities)+3)
	for _, activity := range slotActivities {
		states = append(states, slotDisclosure(activity, holder))
	}
	states = append(states, a.hubDisclosure(holder), declaredProgramDisclosure(holder, programs), a.portalDisclosure())
	return DisclosureStatusResult{State: Completed, States: states}
}

// operating reports whether the operation slot is held by the named operation.
func (a *App) operating(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.operation == name
}

// slotState names the operation holding the slot now, reports whether the
// slot is held at all — an operation can hold it without a name — and counts
// the operator-declared programs running now.
func (a *App) slotState() (string, bool, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.operation, a.running, a.programs
}

// slotActivity is one disclosed activity whose network use happens only while
// one of its operations holds the slot, under the names its operations are
// started with.
type slotActivity struct {
	id   string
	ops  []namedOperation
	idle string
}

// namedOperation is one operation name and the sentence that says what that
// operation is doing while it holds the slot.
type namedOperation struct {
	name   string
	active string
}

// slotActivities are those activities, each with the sentence for each of its
// operations and for its idle state.
var slotActivities = []slotActivity{
	{
		id: "run",
		ops: []namedOperation{
			{runOperation, "A durable run is executing now; it sends only to the target its preflight named, and the run panel holds its progress."},
			{"practice", "The guided sample's practice run is executing now; it sends only to the built-in fixture receiver it started on loopback in this process."},
			{privacyOperation, "A disclosure review or derived export is in progress now; its proof sends synthetic fixture messages only to built-in receivers it starts on loopback in this process."},
			{syntheticPacketOperation, "A synthetic demonstration packet is being generated now; it sends the scenario's synthetic messages only to built-in receivers it starts on loopback in this process."},
		},
		idle: "No run is in progress. A run sends only while it executes; selecting, preflighting and reading history connect to nothing.",
	},
	{
		id: "runner",
		ops: []namedOperation{
			{"runner", "Runner execution is in progress now; it reaches the hub to fetch scheduled work and report run records."},
			{"runner-enrollment", "A runner enrollment is in progress now; it asks the hub the runner configuration names to admit this runner, and saves no credential."},
		},
		idle: "No recurring execution is in progress. An enrolled runner connects to the hub only while it fetches or reports work.",
	},
	{
		id: "capture",
		ops: []namedOperation{
			{"capture", captureInProgress},
			{"collect", captureInProgress},
			{"source-diagnosis", "A source access check is in progress now; it reaches the source its registration declares and collects nothing."},
		},
		idle: "No capture or collection is in progress. Nothing listens and no source is read until you start one.",
	},
	{
		id: "environment",
		ops: []namedOperation{
			{targetCheckOperation, "A connectivity check is in progress now; it opens one connection to the address the recorded environment names and sends no HL7 payload."},
			{targetResetOperation, "A fixture reset is in progress now; it opens the one connection its reviewed plan needs to the recorded environment."},
			{sendPolicyOperation, "A send-policy evaluation is in progress now; it resolves the host names it was asked about and opens no connection."},
			{"reduction", "A controlled reduction is in progress now; the fixture resets it performs reach the recorded environment its target names."},
		},
		idle: "No connectivity check, fixture reset, send-policy evaluation or reduction is in progress. Recording a named environment, a send policy or a reset plan reads and writes local files only.",
	},
	{
		id: "observe",
		ops: []namedOperation{
			{"observation", "An observation window is open now, reading the source and scope its validated pair declares."},
		},
		idle: "No observation window is open. Authoring a source or window reads local files only.",
	},
}

// captureInProgress is what a capture and a source collection say while one
// runs: to a person they are the same activity.
const captureInProgress = "A capture or collection is in progress now, on the sources and local port its registration and policy declare."

// hubOperations are the names the hub's requests and its sign-in run under,
// each with the sentence for the hub while it holds the slot.
var hubOperations = []namedOperation{
	{hubRequestOperation, "A hub action you started is in progress now, reaching the configured hub over mutual TLS."},
	{hubSignInStartOperation, "A sign-in is starting now: this window opens the loopback listener your browser returns to from your identity provider."},
	{hubSignInOperation, "A sign-in is in progress now: this window waits for your browser to return from your identity provider, then exchanges the code with it."},
}

// declaredProgramActivity is the privacy status row of the programs an
// operator declared: the locator of a credential reference, the key command
// of a hub configuration, the key and token commands of a runner
// configuration, the key program of a protection control, the locator naming
// a client certificate's or a TLS capture listener's private key, a source's
// transfer program or credential locator, and an observation source's
// credential locator. Readmit runs one only by the absolute path the operator
// declared, and only while an operation needs it.
const declaredProgramActivity = "declared-program"

// declaredProgramReach ends every sentence the declared-program row says while
// one runs: what the program reaches is its own configuration's business, and
// Readmit neither sees it nor gains any access of its own through it.
const declaredProgramReach = " It may contact whatever it is configured to reach; Readmit cannot see or vouch for that program's destinations and adds no network access of its own."

// declaredProgramOperations are the names of the operations that can run an
// operator-declared program, each with the sentence the declared-program row
// says while that operation's program runs: which program it is and why the
// operation runs it.
var declaredProgramOperations = []namedOperation{
	{secretTestOperation, "An operator-declared program is running now: the locator of the credential reference being tested." + declaredProgramReach},
	{secretRotationOperation, "An operator-declared program is running now: the locator of the credential reference being rotated, confirming it still resolves." + declaredProgramReach},
	{secretScanOperation, "An operator-declared program is running now: the locator of each credential reference a residual scan checks for." + declaredProgramReach},
	{protectOperation, "An operator-declared program is running now: the key program of the protection control a rotation, packing or opening uses." + declaredProgramReach},
	{hubRequestOperation, "An operator-declared program is running now: the key command of the selected hub configuration, reading the client key a hub action connects with." + declaredProgramReach},
	{hubSignInOperation, "An operator-declared program is running now: the key command of the selected hub configuration, reading the client key the signed-in connection uses." + declaredProgramReach},
	{"runner-enrollment", "An operator-declared program is running now: the key or token command of the runner configuration an enrollment presents." + declaredProgramReach},
	{"runner", "An operator-declared program is running now: the key or token command of the runner configuration a runner execution presents." + declaredProgramReach},
	{targetCheckOperation, "An operator-declared program is running now: the locator of the private key the connectivity check's client certificate presents." + declaredProgramReach},
	{targetResetOperation, "An operator-declared program is running now: the locator of the private key the client certificate of a fixture reset's check presents." + declaredProgramReach},
	{"reduction", "An operator-declared program is running now: the locator of the private key the client certificate of a reduction's fixture reset presents." + declaredProgramReach},
	{"source-diagnosis", "An operator-declared program is running now: the transfer program or credential locator the source registration declares, for a source access check." + declaredProgramReach},
	{"collect", "An operator-declared program is running now: the transfer program or credential locator the source registration declares, for a source collection." + declaredProgramReach},
	{"capture", "An operator-declared program is running now: the locator of the private key a TLS capture listener presents." + declaredProgramReach},
	{"observation", "An operator-declared program is running now: the credential locator the observation source declares." + declaredProgramReach},
}

// declaredProgramDisclosure answers for the programs an operator declared:
// active while at least one runs, saying which operation's program it is, and
// idle otherwise. A program is counted only while it runs (runNamed), so an
// operation that could run one never makes the row active on its own. A
// program running under a name without a sentence of its own is still
// reported as running.
func declaredProgramDisclosure(holder string, programs int) DisclosureState {
	if programs == 0 {
		return DisclosureState{ID: declaredProgramActivity, State: disclosureIdle,
			Detail: "No operator-declared program is running. One runs only while an action you started needs it, such as testing, rotating or scanning a credential reference, a hub, runner or protection action, or a check, reset, reduction, collection, capture or observation whose configuration declares one."}
	}
	active, ok := holding(declaredProgramOperations, holder)
	if !ok {
		active = "An operator-declared program is running now." + declaredProgramReach
	}
	return DisclosureState{ID: declaredProgramActivity, State: disclosureActive, Detail: active}
}

// holding answers the sentence of the named operation that holds the slot, if
// it is one of ops.
func holding(ops []namedOperation, holder string) (string, bool) {
	for _, op := range ops {
		if op.name == holder {
			return op.active, true
		}
	}
	return "", false
}

// slotDisclosure answers one slot-held activity: active when the slot's holder
// is one of the operations it happens under, idle otherwise.
func slotDisclosure(activity slotActivity, holder string) DisclosureState {
	if active, ok := holding(activity.ops, holder); ok {
		return DisclosureState{ID: activity.id, State: disclosureActive, Detail: active}
	}
	return DisclosureState{ID: activity.id, State: disclosureIdle, Detail: activity.idle}
}

// hubDisclosure answers for the customer artifact hub: active while one of its
// operations holds the slot, and otherwise from the hub's own connection
// objects — no configuration, a configuration without a connection, or a live
// connection with or without a current sign-in session. With no configuration
// a hub operation refuses before it reaches anything, so it is not active.
func (a *App) hubDisclosure(holder string) DisclosureState {
	a.hubMu.Lock()
	cfg := a.hubConfig
	client := a.hubClient
	session := a.hubSession
	a.hubMu.Unlock()
	active, operating := holding(hubOperations, holder)
	switch {
	case cfg == nil:
		return DisclosureState{ID: "hub", State: disclosureNotConfigured,
			Detail: "No hub configuration is selected. Every hub operation is unavailable and nothing is connected."}
	case operating:
		return DisclosureState{ID: "hub", State: disclosureActive, Detail: active}
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
