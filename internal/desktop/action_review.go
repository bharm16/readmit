package desktop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
)

// A person reviews content and a destination; the backend binds and checks
// the exact content without asking them to type an identity. One review
// lifecycle serves every reviewed action, and each action keeps its own typed
// policy: what it binds, what consent it takes, and how it executes.
//
// A review is prepared from the objects themselves: the backend reads them,
// resolves where the output goes, and binds everything the displayed action
// depends on — the project, the reviewer, the exact artifact identities, the
// target configuration, the operation policy and its grants, the selected
// messages and the proposed output — into one binding held under an opaque
// token in this process alone. The final explicit Send, Export or Approve
// executes that binding once: every binding is read again before anything
// happens, a changed one is answered as a stale review with a fresh one to
// look at, and nothing refreshed is ever executed without a new click.

// reviewLifetime is how long a prepared review can be executed.
const reviewLifetime = 15 * time.Minute

// Consent is what the final explicit action of a review is consent to.
type Consent string

const (
	// SendConsent: the displayed messages leave for the displayed target.
	SendConsent Consent = "send"
	// ExportConsent: the displayed export is written.
	ExportConsent Consent = "export"
	// ApproveConsent: a durable approval of the displayed version is
	// recorded; nothing is sent or exported.
	ApproveConsent Consent = "approve"
	// CollectConsent: the displayed source is read once; nothing in it is
	// changed.
	CollectConsent Consent = "collect"
	// ResetConsent: the displayed reset actions run against the displayed
	// environment.
	ResetConsent Consent = "reset"
	// ScanConsent: the displayed files are scanned for the project's
	// registered credentials; nothing is written.
	ScanConsent Consent = "scan"
)

// ReviewRequirement is one explicit decision an action needs beyond its
// final click.
type ReviewRequirement string

const (
	// RationaleRequirement: an approval records why it was given.
	RationaleRequirement ReviewRequirement = "rationale"
	// ConfirmationsRequirement: every manual step of a reset is confirmed,
	// by its action identity, in the final click.
	ConfirmationsRequirement ReviewRequirement = "confirmations"
)

// ReplayActionOptions are what a send sends: the selected messages (none is
// every message, in source order), the named transformations, and the send
// policy the destination is decided under, if the project uses one.
type ReplayActionOptions struct {
	Connected       *ConnectedActionOptions `json:"connected,omitzero"`
	Messages        []string                `json:"messages"`
	Transformations []replay.Transformation `json:"transformations"`
	Policy          string                  `json:"policy,omitzero"`
}

// PromotionActionOptions name the suite environment and the revision
// assumption a promotion approval is recorded for. The release pins are the
// project's own: the one set of released tests that accepts the suite.
type PromotionActionOptions struct {
	Environment string `json:"environment"`
	Revision    string `json:"revision"`
}

// ScanActionOptions narrow a credential scan to one registered reference;
// empty scans for every one.
type ScanActionOptions struct {
	Reference string `json:"reference,omitzero"`
}

// PrepareActionRequest asks for the review of one action: the objects it is
// scoped to, the destination it goes to, and its typed options. A send is
// scoped to one case and goes to one environment; a promotion approval is
// scoped to one suite; an export is scoped to one export review, a report of
// the project, whose private local state the project resolves. A
// collection is scoped to one observation and, when its source is reached
// under an environment's send policy, goes to that environment; a reset is
// scoped to one environment; a credential scan to the project.
type PrepareActionRequest struct {
	Context     RequestContext          `json:"context"`
	Action      ActionID                `json:"action"`
	Items       []ItemRef               `json:"items"`
	Destination *ItemRef                `json:"destination,omitzero"`
	Replay      *ReplayActionOptions    `json:"replay,omitzero"`
	Promotion   *PromotionActionOptions `json:"promotion,omitzero"`
	Scan        *ScanActionOptions      `json:"scan,omitzero"`
	Storage     *StorageActionOptions   `json:"storage,omitzero"`
	// DeriveReview names the inputs an export review of the one scoped case
	// is derived with.
	DeriveReview *DeriveReviewOptions `json:"derive_review,omitzero"`
}

// ReviewDestination is where an action's effect lands: a named target and
// the address it reaches, or the output the application named in the
// project.
type ReviewDestination struct {
	Name           string `json:"name,omitzero"`
	Classification string `json:"classification,omitzero"`
	Address        string `json:"address,omitzero"`
	Output         string `json:"output,omitzero"`
}

// ActionReview is what one prepared action will do, as its owner displays
// it: the named objects and their versions, the destination, the effect in
// the action's own terms, and whether it can proceed. Token is opaque and
// internal: a window passes it back unchanged and never shows it. A review
// that cannot proceed carries no token and says why.
type ActionReview struct {
	Token        string                  `json:"token,omitzero"`
	Action       ActionID                `json:"action"`
	Consent      Consent                 `json:"consent"`
	Items        []CatalogItem           `json:"items"`
	Destination  ReviewDestination       `json:"destination"`
	ExpiresAt    string                  `json:"expires_at,omitzero"`
	Requirements []ReviewRequirement     `json:"requirements"`
	Ready        bool                    `json:"ready"`
	Refusal      string                  `json:"refusal,omitzero"`
	Replay       *ReplayPreview          `json:"replay,omitzero"`
	Export       *ExportReviewView       `json:"export,omitzero"`
	Promotion    *suite.PromotionReview  `json:"promotion,omitzero"`
	Collect      *CollectReview          `json:"collect,omitzero"`
	Reset        *EnvironmentResetReview `json:"reset,omitzero"`
	Scan         *ScanReview             `json:"scan,omitzero"`
	Storage      *StorageReview          `json:"storage,omitzero"`
	Derive       *DeriveReviewView       `json:"derive,omitzero"`
	Transport    *TransportReview        `json:"transport,omitzero"`
}

// ExportReviewView is the export review a derived packet is exported from:
// its state and how many of its findings are unresolved.
type ExportReviewView struct {
	Review     string `json:"review"`
	State      string `json:"state"`
	Findings   int    `json:"findings"`
	Unresolved int    `json:"unresolved"`
}

// ActionReviewResult carries one review and the context it answers.
type ActionReviewResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Review  *ActionReview  `json:"review,omitzero"`
}

func (r *ActionReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReviewDecisions are the explicit decisions an action's requirements ask
// for, made with the final click.
//
// DeclaredInventory is the declaration a derivation requires: the digest of
// the inventory its review showed, which the person declares complete.
type ReviewDecisions struct {
	Rationale         string   `json:"rationale,omitzero"`
	Confirmed         []string `json:"confirmed,omitzero"`
	DeclaredInventory string   `json:"declared_inventory,omitzero"`
}

// ExecuteActionRequest is the final explicit action of one review. IntentID
// is allocated once when the person clicks and reused for every retry.
type ExecuteActionRequest struct {
	Context   RequestContext  `json:"context"`
	Token     string          `json:"token"`
	IntentID  string          `json:"intent_id"`
	Decisions ReviewDecisions `json:"decisions"`
}

// ReviewedOutcome is what the final action did.
type ReviewedOutcome string

const (
	// ActionCompleted: the action happened, and the result says what it did.
	ActionCompleted ReviewedOutcome = "completed"
	// ActionUncertain: the action happened and some of its effect is not
	// known, such as a delivery no acknowledgement settled. It is never read
	// as success and never repeated.
	ActionUncertain ReviewedOutcome = "uncertain"
	// ActionStale: something the review bound changed. Nothing happened;
	// Refreshed is the review of the action as it stands now, which needs a
	// new click of its own.
	ActionStale ReviewedOutcome = "stale"
	// ActionExpired: the review is past its lifetime. Nothing happened.
	ActionExpired ReviewedOutcome = "expired"
	// ActionRefused: nothing happened, for the reason given.
	ActionRefused ReviewedOutcome = "refused"
	// ActionCancelled: the person stopped it; the result says what was
	// done before it stopped.
	ActionCancelled ReviewedOutcome = "cancelled"
)

// PromotionApproval is one durable approval: the identity of the record and
// the project entry it was written to. It stays evidence of exactly the
// version it approved.
type PromotionApproval struct {
	Identity string `json:"identity"`
	Reviewed string `json:"reviewed"`
	Output   string `json:"output"`
}

// ReviewedActionResult answers one final action. Operation names the
// operation it ran as — the click's intent, which CancelOperation stops while
// it runs. Replayed is true when the
// same click arrived again and was answered with the original result.
type ReviewedActionResult struct {
	State     State                 `json:"state"`
	Reason    string                `json:"reason,omitzero"`
	Context   RequestContext        `json:"context"`
	Outcome   ReviewedOutcome       `json:"outcome"`
	Operation string                `json:"operation,omitzero"`
	Replayed  bool                  `json:"replayed"`
	Refreshed *ActionReview         `json:"refreshed,omitzero"`
	Replay    *ReplayRun            `json:"replay,omitzero"`
	Export    *PrivacyExportOutcome `json:"export,omitzero"`
	Approval  *PromotionApproval    `json:"approval,omitzero"`
	Collected *CollectionRow        `json:"collected,omitzero"`
	Reset     *EnvironmentReset     `json:"reset,omitzero"`
	Scan      *ScanOutcome          `json:"scan,omitzero"`
	Storage   *StorageOutcome       `json:"storage,omitzero"`
	Derived   *PrivacyReviewOutcome `json:"derived,omitzero"`
	// Approved is the environment revision a transport approval published.
	Approved *ItemRef `json:"approved,omitzero"`
}

func (r *ReviewedActionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
	if r.Outcome == "" {
		r.Outcome = ActionRefused
	}
}

// boundAction is everything a review resolved and bound: the resolved
// request each action executes, the binding over all of it, the display, and
// the preparation it was made from, so executing it binds exactly that again.
type boundAction struct {
	connected       *connectedBinding
	executionReview *executionReview
	action          ActionID
	origin          PrepareActionRequest
	binding         string
	review          ActionReview
	replay          ReplayRequest
	export          PrivacyExportRequest
	suite           SuitePromotionApproveRequest
	collect         *collectBinding
	reset           *resetBinding
	scan            *scanBinding
	storage         storagePlan
	derive          PrivacyReviewRequest
	// transport is what a transport approval publishes.
	transport *transportBinding
}

// slot is the operation slot one step of an action holds: a declared,
// named profile, or local work that writes or does not, and that Stop can
// interrupt or not.
type slot struct {
	profile       string
	writes        bool
	interruptible bool
}

// actionPolicy is one action's own policy: its consent and requirements, the
// slot its review and its final action each hold, how it binds, and how it
// performs a binding. bind runs inside the slot already held; held says the
// final action's own admission was already taken there.
type actionPolicy struct {
	consent      Consent
	requirements []ReviewRequirement
	review       slot
	perform      slot
	bind         func(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal)
	execute      func(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult
}

var actionPolicies = map[ActionID]actionPolicy{
	ReplaySendAction: {consent: SendConsent, review: slot{profile: "PreviewReplay"}, perform: slot{profile: "SendReplay"},
		bind: bindReplaySend, execute: executeReplaySend},
	ExportPacketAction: {consent: ExportConsent, review: slot{}, perform: slot{profile: "ExportDerivedPacket"},
		bind: bindExport, execute: executeExport},
	ApprovePromotionAction: {consent: ApproveConsent, requirements: []ReviewRequirement{RationaleRequirement},
		review: slot{}, perform: slot{writes: true}, bind: bindPromotion, execute: executePromotion},
	CollectObservationAction: {consent: CollectConsent, review: slot{}, perform: slot{profile: "CollectObservation"},
		bind: bindCollect, execute: executeCollect},
	ResetEnvironmentAction: {consent: ResetConsent, review: slot{}, perform: slot{profile: "ResetTarget"},
		bind: bindReset, execute: executeReset},
	ScanSecretsAction: {consent: ScanConsent, review: slot{}, perform: slot{profile: "ScanSecrets"},
		bind: bindScan, execute: executeScan},
	DeriveReviewAction: {consent: DeriveConsent, requirements: []ReviewRequirement{InventoryDeclarationRequirement},
		review: slot{}, perform: slot{profile: "DeriveExportReview"}, bind: bindDeriveReview, execute: executeDeriveReview},
	ApproveTransportAction: {consent: ApproveConsent, review: slot{}, perform: slot{writes: true},
		bind: bindApproveTransport, execute: executeApproveTransport},
}

// hold runs work holding one slot.
func (a *App) hold(held slot, work func(context.Context) ReviewedActionResult) ReviewedActionResult {
	if held.profile != "" {
		return runNamed[ReviewedActionResult, *ReviewedActionResult](a, profiles[held.profile], work)
	}
	return run(a, held.interruptible, held.writes, work)
}

// reviewStore holds the reviews and final actions of this process.
type reviewStore struct {
	mu      sync.Mutex
	reviews map[string]*heldReview
	intents map[string]*heldIntent
	// running is the operation executing now, and slot the slot name it
	// holds, which CancelOperation cancels; interruptible says an unnamed
	// slot it holds can be stopped.
	running, slot string
	interruptible bool
}

type heldReview struct {
	bound     *boundAction
	expires   time.Time
	consumed  string
	withdrawn bool
}

type heldIntent struct {
	payload string
	at      time.Time
	done    chan struct{}
	result  ReviewedActionResult
}

func (s *reviewStore) init() {
	if s.reviews == nil {
		s.reviews, s.intents = map[string]*heldReview{}, map[string]*heldIntent{}
	}
}

// forget drops reviews and answered clicks long past a review's lifetime:
// a review just past it is still answered as expired, and a click is
// answered with its original result for as long as its review could have
// been used.
func (s *reviewStore) forget(now time.Time) {
	for token, review := range s.reviews {
		if review.expires.Add(reviewLifetime).Before(now) {
			delete(s.reviews, token)
		}
	}
	for id, intent := range s.intents {
		if intent.at.Add(2 * reviewLifetime).Before(now) {
			select {
			case <-intent.done:
				delete(s.intents, id)
			default:
			}
		}
	}
}

// keep holds a bound review under a new opaque token.
func (s *reviewStore) keep(bound *boundAction, now time.Time) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token, expires := hex.EncodeToString(raw), now.Add(reviewLifetime)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.forget(now)
	s.reviews[token] = &heldReview{bound: bound, expires: expires}
	return token, expires, nil
}

// mentions reports whether text carries a token this process holds, so no
// token is ever retained in a draft.
func (s *reviewStore) mentions(text []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token := range s.reviews {
		if strings.Contains(string(text), token) {
			return true
		}
	}
	return false
}

// PrepareAction prepares the review of one action: it reads the objects,
// resolves the destination and the output, decides whether the action could
// proceed, and binds all of it under a token that expires in fifteen minutes
// and dies with the process. Preparing sends, exports and records nothing.
func (a *App) PrepareAction(request PrepareActionRequest) ActionReviewResult {
	result := ActionReviewResult{Context: request.Context}
	policy, ok := actionPolicies[request.Action]
	if !ok {
		result.refuse(Failed, "this action is not one that is reviewed")
		return result
	}
	var review *ActionReview
	prepared := a.hold(policy.review, func(ctx context.Context) ReviewedActionResult {
		bound, declined := policy.bind(a, ctx, request, false)
		if bound == nil {
			return ReviewedActionResult{State: declined.state, Reason: declined.reason}
		}
		issued, err := a.issue(bound, policy)
		if err != nil {
			return ReviewedActionResult{State: Failed, Reason: "the review could not be held"}
		}
		review = issued
		return ReviewedActionResult{State: Completed}
	})
	if review == nil {
		result.refuse(prepared.State, prepared.Reason)
		return result
	}
	result.State, result.Review = Completed, review
	return result
}

// issue holds a ready bound review under a token and answers its display.
func (a *App) issue(bound *boundAction, policy actionPolicy) (*ActionReview, error) {
	review := bound.review
	review.Action, review.Consent = bound.action, policy.consent
	review.Requirements = append(slices.Clone(policy.requirements), bound.review.Requirements...)
	if review.Requirements == nil {
		review.Requirements = []ReviewRequirement{}
	}
	if review.Items == nil {
		review.Items = []CatalogItem{}
	}
	if review.Ready {
		token, expires, err := a.reviews.keep(bound, a.now())
		if err != nil {
			return nil, err
		}
		review.Token, review.ExpiresAt = token, catalog.Stamp(expires)
	}
	return &review, nil
}

// ExecuteReviewedAction is the final explicit action of one review. The
// review is admitted once, atomically with the click's intent: the same
// click arriving again is answered with the original result and nothing is
// done twice, and a different request under the same intent is refused. An
// expired, withdrawn, used or unknown review — every review after a restart
// — does nothing. Every binding is read again, holding the slot the action
// itself holds, before any effect; a change is a stale review, answered with
// the refreshed one.
func (a *App) ExecuteReviewedAction(request ExecuteActionRequest) ReviewedActionResult {
	result := ReviewedActionResult{Context: request.Context}
	if !catalog.ValidToken(request.IntentID) {
		result.refuse(Failed, "a final action carries the identity of the click that made it")
		return result
	}
	decisions, _ := json.Marshal(request.Decisions, json.Deterministic(true))
	sum := sha256.Sum256([]byte(request.Token + "\x00" + string(decisions)))
	payload := hex.EncodeToString(sum[:])

	store := &a.reviews
	store.mu.Lock()
	store.init()
	if intent, held := store.intents[request.IntentID]; held {
		store.mu.Unlock()
		if intent.payload != payload {
			result.refuse(Failed, "this click already asked for a different action; nothing was done")
			return result
		}
		<-intent.done
		repeated := intent.result
		repeated.Replayed = true
		return repeated
	}
	review, held := store.reviews[request.Token]
	switch {
	case !held || review.withdrawn:
		store.mu.Unlock()
		result.refuse(Failed, "no review is held for this action; review it again")
		return result
	case review.consumed != "":
		store.mu.Unlock()
		result.refuse(Failed, "this review was already used; review the action again")
		return result
	case !a.now().Before(review.expires):
		delete(store.reviews, request.Token)
		store.mu.Unlock()
		result.Outcome = ActionExpired
		result.refuse(Failed, "the review expired; review the action again")
		return result
	}
	review.consumed = request.IntentID
	review.bound.executionReview = &executionReview{token: request.Token, intent: request.IntentID, expires: review.expires}
	intent := &heldIntent{payload: payload, at: a.now(), done: make(chan struct{})}
	store.intents[request.IntentID] = intent
	store.mu.Unlock()

	policy := actionPolicies[review.bound.action]
	outcome := ReviewedActionResult{}
	retryable := false
	if missing := unmet(append(slices.Clone(policy.requirements), review.bound.review.Requirements...), review.bound, request.Decisions); missing != "" {
		outcome.refuse(Failed, missing)
		retryable = true
	} else {
		outcome = a.perform(request.IntentID, review.bound, policy, request.Decisions)
		retryable = outcome.State == Busy
	}
	// The click's own identity is the operation it ran as.
	outcome.Context, outcome.Operation = request.Context, request.IntentID

	store.mu.Lock()
	if retryable {
		// Nothing was attempted: the review stays usable and the click can
		// be made again.
		review.consumed = ""
		delete(store.intents, request.IntentID)
	}
	intent.result = outcome
	close(intent.done)
	store.mu.Unlock()
	return outcome
}

func unmet(requirements []ReviewRequirement, bound *boundAction, decisions ReviewDecisions) string {
	if slices.Contains(requirements, RationaleRequirement) && strings.TrimSpace(decisions.Rationale) == "" {
		return "an approval records why it is given; nothing was recorded"
	}
	if slices.Contains(requirements, ConfirmationsRequirement) && bound.review.Reset != nil {
		manual := []string{}
		for _, action := range bound.review.Reset.Actions {
			if action.Type == fixturereset.OperatorConfirms {
				manual = append(manual, action.ID)
			}
		}
		confirmed := slices.Clone(decisions.Confirmed)
		slices.Sort(confirmed)
		slices.Sort(manual)
		if !slices.Equal(slices.Compact(confirmed), manual) {
			return "every manual step is confirmed, and nothing else, before a reset; nothing was reset"
		}
	}
	if slices.Contains(requirements, InventoryDeclarationRequirement) {
		return declaredInventory(bound, decisions)
	}
	return ""
}

// perform holds the final action's slot, binds the review again inside it,
// and executes the binding only when it is unchanged. A final action its own
// admission declines is looked at again under the review's slot, so a change
// of license or grant is answered as the stale review it is.
func (a *App) perform(operation string, bound *boundAction, policy actionPolicy, decisions ReviewDecisions) ReviewedActionResult {
	performed := false
	result := a.hold(policy.perform, func(ctx context.Context) ReviewedActionResult {
		fresh, declined := policy.bind(a, ctx, bound.origin, true)
		if stale, changed := a.staleReview(bound, fresh, declined, policy); changed {
			return stale
		}
		a.markRunning(operation, policy.perform)
		defer a.markRunning("", slot{})
		performed = true
		fresh.executionReview = bound.executionReview
		return policy.execute(a, ctx, fresh, decisions)
	})
	if !performed && result.State == PermissionDenied {
		rebound := a.hold(policy.review, func(ctx context.Context) ReviewedActionResult {
			fresh, declined := policy.bind(a, ctx, bound.origin, false)
			if stale, changed := a.staleReview(bound, fresh, declined, policy); changed {
				return stale
			}
			return result
		})
		if rebound.Outcome == ActionStale {
			return rebound
		}
	}
	return result
}

// staleReview answers a stale review when what the review bound is not what
// binding it again found.
func (a *App) staleReview(bound, fresh *boundAction, declined refusal, policy actionPolicy) (ReviewedActionResult, bool) {
	if fresh == nil {
		result := ReviewedActionResult{Outcome: ActionStale}
		result.refuse(declined.state, "the reviewed action can no longer be prepared: "+declined.reason+". Nothing was done")
		return result, true
	}
	if fresh.binding != bound.binding || !fresh.review.Ready {
		refreshed, _ := a.issue(fresh, policy)
		result := ReviewedActionResult{Outcome: ActionStale, Refreshed: refreshed}
		result.refuse(Failed, "what this review showed has changed; look at it again before acting. Nothing was done")
		return result, true
	}
	return ReviewedActionResult{}, false
}

// markRunning records the operation a final action runs as and the slot it
// holds, for CancelOperation.
func (a *App) markRunning(operation string, held slot) {
	a.reviews.mu.Lock()
	defer a.reviews.mu.Unlock()
	a.reviews.running, a.reviews.slot, a.reviews.interruptible = operation, "", held.interruptible
	if held.profile != "" {
		a.reviews.slot = profiles[held.profile].Name
	}
}

// WithdrawReview drops a prepared review, so it can no longer be executed.
func (a *App) WithdrawReview(token string) ActionReviewResult {
	a.reviews.mu.Lock()
	defer a.reviews.mu.Unlock()
	review, held := a.reviews.reviews[token]
	if !held {
		return ActionReviewResult{State: Failed, Reason: "no review is held under that token"}
	}
	review.withdrawn = true
	return ActionReviewResult{State: Completed}
}

// CancelOperation stops the reviewed action running as operation, and
// nothing else. An operation the facade is not running does nothing.
func (a *App) CancelOperation(operation string) {
	a.reviews.mu.Lock()
	running, name, interruptible := a.reviews.running, a.reviews.slot, a.reviews.interruptible
	a.reviews.mu.Unlock()
	if operation == "" || operation != running || name == "" && !interruptible {
		return
	}
	a.Cancel(name)
}

// reviewer is the local account and, when one is signed in, the customer-hub
// subject a review is made by. A local action binds the local account; a
// sign-in or sign-out changes the reviewer.
func (a *App) reviewer() string {
	if a.actor != nil {
		return a.actor()
	}
	local := "local:unknown"
	if current, err := user.Current(); err == nil {
		local = "local:" + current.Uid + ":" + current.Username
	}
	if status := a.hub.Status(); status.SignedIn() {
		local += "|hub:" + status.Session.Issuer + ":" + status.Session.Subject
	}
	return local
}

// reviewerName is how an approval names the person who gave it: the local
// reviewer name kept in the preferences, or else the account name. It is a
// label only; the review binding (reviewer) never reads it.
func (a *App) reviewerName() string {
	if saved := a.savedReviewer(); saved != "" {
		return saved
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "Local reviewer"
}

// policyBinding binds the operation policy the window selected — its path
// and exact bytes — and the admission it grants now, so a changed license,
// role or grant changes the binding. held says the final action's own
// execution admission is already taken, which is that admission granted.
func (a *App) policyBinding(ctx context.Context, execute, held bool) string {
	_, path := a.selectedOperation()
	digest := sha256.New()
	digest.Write([]byte(path + "\x00"))
	if data, err := readOperationFile(path); err == nil {
		digest.Write(data)
	}
	switch {
	case execute && held:
	case execute:
		if admission := a.admissionPreview(ctx); !admission.Admitted {
			digest.Write([]byte("\x00execute:" + admission.Reason))
		}
	case !a.authorPreview(ctx):
		digest.Write([]byte("\x00author:refused"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// binding is the digest of one action's bound parts.
func binding(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// scoped loads the project's catalog and reads each object named, refusing
// one the project does not hold or that is not available now.
func (a *App) scoped(ctx context.Context, request RequestContext, refs []ItemRef) (*loadedCatalog, []CatalogItem, []catalog.Item, refusal) {
	loaded, declined := a.loadCatalog(ctx, request, false)
	if loaded == nil {
		return nil, nil, nil, declined
	}
	var items []CatalogItem
	var records []catalog.Item
	for _, ref := range refs {
		// An object of a later discovery window is read in its own.
		window, index, declined := a.findAcross(ctx, request, loaded, ref.ID, false)
		if window == nil {
			return nil, nil, nil, declined
		}
		if index < 0 || window.document.Items[index].Kind != string(ref.Kind) || window.removed(window.document.Items[index]) {
			return nil, nil, nil, refusal{Failed, "the project holds no such object"}
		}
		item := window.read(window.document.Items[index])
		if item.Availability != ItemAvailable {
			return nil, nil, nil, refusal{Failed, "an object this action needs is " + string(item.Availability)}
		}
		if ref.Revision != "" && ref.Revision != item.Ref.Revision {
			return nil, nil, nil, refusal{Failed, "the object changed since it was shown; look at it again"}
		}
		items, records = append(items, item), append(records, window.document.Items[index])
	}
	return loaded, items, records, refusal{}
}

// backingEntry is the one project entry an object is read from.
func backingEntry(item catalog.Item) string {
	if current := item.Current(); current != nil {
		return current.Members[0].Path
	}
	return item.Entry
}

func bindReplaySend(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if request.Replay != nil && request.Replay.Connected != nil {
		return bindConnectedSend(a, ctx, request, held)
	}
	if len(request.Items) != 1 || request.Items[0].Kind != CaseItem || request.Destination == nil || request.Destination.Kind != EnvironmentItem {
		return nil, refusal{Failed, "a send is reviewed for one case and one environment"}
	}
	options := ReplayActionOptions{}
	if request.Replay != nil {
		options = *request.Replay
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, []ItemRef{request.Items[0], *request.Destination})
	if loaded == nil {
		return nil, declined
	}
	entry := records[0].Entry
	identity := loaded.identities[entry]
	if identity == "" {
		if facts, _, err := operation.VerifiedCase(loaded.root, entry); err == nil {
			identity = facts.Identity
		}
	}
	// An environment saved with a send policy is decided under it unless the
	// send names another.
	policy := options.Policy
	if current := records[1].Current(); policy == "" && current != nil {
		for _, member := range current.Members {
			if member.Role == "policy" {
				policy = member.Path
			}
		}
	}
	replayRequest := ReplayRequest{Workspace: loaded.root, Case: entry, Identity: identity, Target: backingEntry(records[1]),
		Policy: policy, Messages: slices.Clone(options.Messages), Transformations: slices.Clone(options.Transformations)}
	previewed := a.previewReplay(ctx, replayRequest, held)
	if previewed.Preview == nil {
		return nil, refusal{previewed.State, previewed.Reason}
	}
	preview := previewed.Preview
	replayRequest.Output = preview.Destination.Name
	return &boundAction{action: ReplaySendAction, origin: request, replay: replayRequest,
		binding: binding(string(ReplaySendAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
			preview.Identity, preview.SourceIdentity, preview.Destination.Name, preview.Target.Name, preview.Target.Classification, preview.Target.Address),
		review: ActionReview{Items: items, Ready: preview.Sendable, Refusal: preview.Refusal, Replay: preview,
			Destination: ReviewDestination{Name: preview.Target.Name, Classification: preview.Target.Classification, Address: preview.Target.Address, Output: preview.Destination.Name}}}, refusal{}
}

// executeReplaySend sends the bound plan once. A delivery no acknowledgement
// settled makes the outcome uncertain, whether or not the send was stopped:
// uncertainty is never read as completion or as a clean stop.
func executeReplaySend(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	if bound.connected != nil {
		a.reach(reachingTarget{ref: "environment:" + bound.origin.Destination.ID, name: bound.review.Destination.Name,
			kind: ConnectionRun, destination: bound.review.Destination.Address})
		return executeConnectedSend(a, ctx, bound)
	}
	sent := a.sendReplay(ctx, ReplaySendRequest{Replay: bound.replay, Expected: bound.review.Replay.Identity, Approved: true})
	result := ReviewedActionResult{State: sent.State, Reason: sent.Reason, Replay: sent.Run, Outcome: ActionCompleted}
	switch {
	case sent.Run != nil && sent.Run.Uncertain > 0:
		result.Outcome = ActionUncertain
	case sent.State == Cancelled:
		result.Outcome = ActionCancelled
	case sent.Run == nil:
		result.Outcome = ActionRefused
	case sent.State != Completed:
		result.Outcome = ActionUncertain
	}
	return result
}

func bindExport(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != ReportItem {
		return nil, refusal{Failed, "an export is reviewed for one export review of the project"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	entry := records[0].Entry
	if items[0].Summary.Report == nil || items[0].Summary.Report.Form != "export-review" || entry == "" {
		return nil, refusal{Failed, "an export is reviewed for one export review of the project"}
	}
	opened, private, err := loaded.exportReview(filepath.Join(loaded.root, entry))
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	destination, refused := destinationFor(loaded.root, "", "export")
	if refused.state != "" {
		return nil, refused
	}
	view := &ExportReviewView{Review: entry, State: opened.State, Findings: len(opened.Findings)}
	for _, finding := range opened.Findings {
		if !finding.Resolved {
			view.Unresolved++
		}
	}
	ready, refusal := opened.State == readyForApproval && destination.Fresh, ""
	if !ready {
		refusal = "the export review is not ready for approval; an incomplete review exports nothing"
	}
	return &boundAction{action: ExportPacketAction, origin: request,
		export: PrivacyExportRequest{Workspace: loaded.root, Review: entry, LocalState: private.entry, Approval: opened.Identity, Output: destination.Name},
		binding: binding(string(ExportPacketAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
			opened.Identity, entry, private.entry, opened.LocalStateCommitment, destination.Name),
		review: ActionReview{Items: items, Ready: ready, Refusal: refusal, Export: view, Destination: ReviewDestination{Output: destination.Name}}}, noRefusal
}

func executeExport(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	exported := exportDerivedPacket(ctx, bound.export)
	result := ReviewedActionResult{State: exported.State, Reason: exported.Reason, Export: exported.Outcome, Outcome: ActionCompleted}
	switch {
	case exported.State == Cancelled:
		result.Outcome = ActionCancelled
	case exported.State != Completed:
		result.Outcome = ActionRefused
	}
	return result
}

// promotionOutput is where a promotion approval is written: a new entry of
// the project the application names.
var promotionOutput = outputRule{prefix: "promotion-approval",
	invalid:   "a promotion approval is written to one new entry of the project",
	exhausted: "the project holds more promotion approvals than this release numbers",
	taken:     "that entry already exists; an approval is written as a new entry",
}

func bindPromotion(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != SuiteItem || request.Promotion == nil {
		return nil, refusal{Failed, "a promotion approval is reviewed for one suite"}
	}
	options := *request.Promotion
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	entry := backingEntry(records[0])
	releases, review, declined := suiteReleases(loaded.root, entry, options)
	if releases == "" {
		return &boundAction{action: ApprovePromotionAction, origin: request,
			binding: binding(string(ApprovePromotionAction), loaded.root, loaded.document.Project.ID, a.reviewer(), declined.reason),
			review:  ActionReview{Items: items, Refusal: declined.reason}}, noRefusal
	}
	destination, refused := promotionOutput.destination(loaded.root, "")
	if refused.state != "" {
		return nil, refused
	}
	return &boundAction{action: ApprovePromotionAction, origin: request,
		suite: SuitePromotionApproveRequest{Workspace: loaded.root, Entry: entry, Environment: options.Environment, Releases: releases,
			Revision: options.Revision, Reviewed: review.Identity(), Output: destination.Name},
		binding: binding(string(ApprovePromotionAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
			review.Identity(), releases, destination.Name),
		review: ActionReview{Items: items, Ready: destination.Fresh, Refusal: destination.Reason, Promotion: &review,
			Destination: ReviewDestination{Output: destination.Name}}}, noRefusal
}

// suiteReleases resolves the released tests a promotion of the suite at entry
// is reviewed with: the one set of release pins of the project the suite's own
// preparation accepts for the environment. None, or more than one, is a
// review that is not ready, and says which.
func suiteReleases(root, entry string, options PromotionActionOptions) (string, suite.PromotionReview, refusal) {
	names, err := projectEntries(root, maxReleaseSets, func(name string) bool {
		path := filepath.Join(root, name)
		return regular(path) && declares(path, suite.ReleasesSchema)
	})
	if err != nil {
		return "", suite.PromotionReview{}, refusal{Failed, "the project's released tests cannot be read: " + err.Error()}
	}
	type accepted struct {
		entry  string
		review suite.PromotionReview
		tests  []string
	}
	var found []accepted
	for _, name := range names {
		path := filepath.Join(root, name)
		review, err := suite.ReviewPromotion(filepath.Join(root, entry), options.Environment, path, options.Revision)
		if err != nil {
			continue
		}
		found = append(found, accepted{entry: name, review: review, tests: releasedTests(root, path)})
	}
	switch len(found) {
	case 0:
		return "", suite.PromotionReview{}, refusal{Failed, "this suite has no released tests to approve for this environment"}
	case 1:
		return found[0].entry, found[0].review, refusal{}
	}
	sets := []string{}
	for _, set := range found {
		sets = append(sets, strings.Join(set.tests, ", "))
	}
	return "", suite.PromotionReview{}, refusal{Failed, "more than one set of released tests accepts this suite (" + strings.Join(sets, "; ") + "); approve one version at a time"}
}

// maxReleaseSets bounds the sets of release pins one approval review reads,
// each through a preparation of the suite.
const maxReleaseSets = 16

// releasedTests names each test a set of release pins releases, by the test
// and the version of its release: never by a file or a digest.
func releasedTests(root, path string) []string {
	tests := []string{}
	data, err := boundedFile(path, suite.MaxBytes)
	if err != nil {
		return tests
	}
	references, err := suite.DecodeReleases(data)
	if err != nil {
		return tests
	}
	for _, reference := range references.Tests {
		named := reference.Test
		if release, err := expectation.Read(artifactpath.JoinReference(root, reference.Release)); err == nil {
			named += " version " + strconv.Itoa(release.Baseline.Revision)
		}
		tests = append(tests, named)
	}
	return tests
}

func executePromotion(a *App, _ context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	approval := bound.suite
	approval.Approver, approval.Rationale = a.reviewerName(), strings.TrimSpace(decisions.Rationale)
	recorded := approveSuitePromotion(approval)
	result := ReviewedActionResult{State: recorded.State, Reason: recorded.Reason, Outcome: ActionCompleted}
	if recorded.State != Completed {
		result.Outcome = ActionRefused
		return result
	}
	result.Approval = &PromotionApproval{Identity: recorded.Identity, Reviewed: approval.Reviewed, Output: recorded.Output}
	return result
}

// noRefusal is a binding that succeeded.
var noRefusal = refusal{}
