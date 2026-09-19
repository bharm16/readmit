package billing

import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

const (
	// AccountSchema is the vendor's account ledger contract: what an
	// organization bought, what was issued to it, and which events decided
	// that. It is never signed and never leaves the vendor; the customer
	// receives the signed entitlement it produced, not the ledger.
	AccountSchema = "readmit-billing-account/v1"

	// MaxLedgerEntries bounds each of the ledger's three append-only lists. It
	// is a document limit that keeps an account inside the shared size limit,
	// not a statement about how long an account may live.
	MaxLedgerEntries = 1024

	accountKind = "billing account ledger"
)

// The refusals processing an event against a ledger can reach. A refusal is a
// mistake by whatever produced the event, so it is an error; a duplicate and a
// stale event are ordinary delivery, so they are outcomes instead.
var (
	ErrDifferentAccount   = errors.New("billing event belongs to a different account")
	ErrBeforeAccount      = errors.New("billing event occurred before the account was opened")
	ErrNotYetOccurred     = errors.New("a billing event cannot be processed before it occurred")
	ErrLedgerFull         = errors.New("billing account ledger is full")
	ErrProrationUnsettled = errors.New("increased paid access needs explicit settled proration")
	ErrDowngradeExceeded  = errors.New("issue exceeds the quantities a scheduled downgrade left")
	ErrTermPrecedesIssue  = errors.New("term starts before the term of the latest issue")
	ErrProvisionalOverlap = errors.New("a provisional term overlaps a term already issued")
	ErrNoIssue            = errors.New("account ledger has issued nothing")
)

// Renewal is the closed set of renewal states. Stopped is reached by a
// cancellation, a refund or a chargeback and is left only by a later verified
// payment.
type Renewal string

const (
	RenewalOpen    Renewal = "open"
	RenewalStopped Renewal = "stopped"
)

var renewals = []Renewal{RenewalOpen, RenewalStopped}

// IssueKind separates the two grants an account can produce. Paid access
// follows a verified completed payment; a provisional grant follows an invoice
// to an approved net-terms customer and is never recorded as paid.
type IssueKind string

const (
	IssuePaid        IssueKind = "paid"
	IssueProvisional IssueKind = "provisional"
)

var issueKinds = []IssueKind{IssuePaid, IssueProvisional}

// Outcome is what one event did. Every processed event records exactly one.
type Outcome string

const (
	// OutcomeIssued and OutcomeProvisional each added an issue to the ledger.
	OutcomeIssued      Outcome = "issued"
	OutcomeProvisional Outcome = "provisional"

	// OutcomeNoGrant is an invoice without approved net terms: recorded,
	// authorizing nothing. Creating a subscription is the same non-event.
	OutcomeNoGrant Outcome = "no-grant"

	// OutcomeScheduled is a downgrade recorded for the next renewal. The term
	// already paid for is untouched.
	OutcomeScheduled Outcome = "scheduled"

	// OutcomeRenewalStopped is a cancellation, refund or chargeback. Access
	// through the paid-through term and its grace stands.
	OutcomeRenewalStopped Outcome = "renewal-stopped"

	// OutcomeDuplicate is an event identifier the ledger already holds. The
	// ledger is returned unchanged, which is what makes redelivery safe.
	OutcomeDuplicate Outcome = "duplicate"

	// OutcomeStale is a granting event that occurred before the last event
	// applied. It is recorded and applied to nothing, so a payment delivered
	// after a cancellation cannot restore the renewal right that withdrew.
	OutcomeStale Outcome = "stale"
)

var outcomes = []Outcome{
	OutcomeIssued, OutcomeProvisional, OutcomeNoGrant,
	OutcomeScheduled, OutcomeRenewalStopped, OutcomeDuplicate, OutcomeStale,
}

// Issue is one entitlement this account decided to issue, at the
// organization-scoped sequence a customer store follows. It records what was
// decided, not the signed document: signing is [Account.Claims] plus
// [entitlement.SignV2], and the ledger keeps every issue it ever made.
type Issue struct {
	Sequence   int        `json:"sequence"`
	Kind       IssueKind  `json:"kind"`
	Plan       string     `json:"plan"`
	Event      string     `json:"event"`
	Applied    time.Time  `json:"applied"`
	Term       Term       `json:"term"`
	Quantities Quantities `json:"quantities"`
}

// Downgrade is a reduction recorded for the next renewal.
type Downgrade struct {
	Event      string     `json:"event"`
	Scheduled  time.Time  `json:"scheduled"`
	Quantities Quantities `json:"quantities"`
}

// Processed is one event this ledger has seen, kept whatever it decided. It is
// the deduplication record and the audit trail at once: a refund that changed
// only the renewal state is as visible here as a payment that issued a term.
type Processed struct {
	Event    string    `json:"event"`
	Occurred time.Time `json:"occurred"`
	Applied  time.Time `json:"applied"`
	Outcome  Outcome   `json:"outcome"`
}

// Account is one organization's billing ledger.
//
// AppliedThrough is the occurrence time of the most recent event applied in
// order, whatever it decided. It is what out-of-order delivery is reconciled
// against: an event that decides what is granted next and occurred before it is
// stale, while an event that only stops renewal is applied whenever it arrives
// — and when it arrives late it leaves AppliedThrough where it was, so a late
// cancellation never makes a grant that already happened look stale.
//
// Nothing here describes the customer's work. The organization identifier, the
// plan labels and the event identifiers are names the vendor and the provider
// chose; a case title, an endpoint, a patient identifier and an evidence hash
// are not members of this contract.
type Account struct {
	Schema         string      `json:"schema"`
	Organization   string      `json:"organization"`
	Opened         time.Time   `json:"opened"`
	AppliedThrough time.Time   `json:"applied_through"`
	Renewal        Renewal     `json:"renewal"`
	Issues         []Issue     `json:"issues"`
	Downgrades     []Downgrade `json:"downgrades"`
	Processed      []Processed `json:"processed"`
}

// Open starts a ledger for an organization. It grants nothing: an account
// exists before anything is bought, and every grant arrives as an event.
func Open(organization string, at time.Time) (Account, error) {
	account := Account{
		Schema:         AccountSchema,
		Organization:   organization,
		Opened:         at,
		AppliedThrough: at,
		Renewal:        RenewalOpen,
	}
	if err := validateAccount(account); err != nil {
		return Account{}, err
	}
	return account, nil
}

// LatestIssue reports the most recent issue and whether there is one.
func (a Account) LatestIssue() (Issue, bool) {
	if len(a.Issues) == 0 {
		return Issue{}, false
	}
	return a.Issues[len(a.Issues)-1], true
}

// PaidThrough reports the end of the latest paid term and whether the account
// has one. A provisional grant never sets it, so "paid through" means paid.
func (a Account) PaidThrough() (time.Time, bool) {
	for i := len(a.Issues) - 1; i >= 0; i-- {
		if a.Issues[i].Kind == IssuePaid {
			return a.Issues[i].Term.Ends, true
		}
	}
	return time.Time{}, false
}

// scheduledDowngrade reports the reduction that applies to the next issue: the
// last one recorded after the latest issue was applied. A downgrade recorded
// before that issue was already taken into account by it.
func (a Account) scheduledDowngrade() (Downgrade, bool) {
	if len(a.Downgrades) == 0 {
		return Downgrade{}, false
	}
	last := a.Downgrades[len(a.Downgrades)-1]
	if latest, issued := a.LatestIssue(); issued && !last.Scheduled.After(latest.Applied) {
		return Downgrade{}, false
	}
	return last, true
}

// Apply records one authenticated event against a ledger and reports what it
// did. The ledger is returned by value and never modified in place, so a
// refused event leaves the caller holding exactly what it had.
//
// at is the instant the issuer processed the event, which on a vendor machine
// is its clock. Nothing here consults a clock of its own.
func (a Account) Apply(event Event, at time.Time) (Account, Outcome, error) {
	if err := validateAccount(a); err != nil {
		return a, "", err
	}
	if err := validateEvent(event); err != nil {
		return a, "", err
	}
	if err := entitlement.ValidateInstant(at); err != nil {
		return a, "", errors.New("processing time: " + err.Error())
	}
	if event.Account != a.Organization {
		return a, "", ErrDifferentAccount
	}
	if event.Occurred.Before(a.Opened) {
		return a, "", ErrBeforeAccount
	}
	if at.Before(event.Occurred) {
		return a, "", ErrNotYetOccurred
	}
	if slices.ContainsFunc(a.Processed, func(p Processed) bool { return p.Event == event.ID }) {
		return a, OutcomeDuplicate, nil
	}
	if len(a.Processed) >= MaxLedgerEntries || len(a.Issues) >= MaxLedgerEntries || len(a.Downgrades) >= MaxLedgerEntries {
		return a, "", ErrLedgerFull
	}

	behaviour := behaviours[event.Type]
	late := event.Occurred.Before(a.AppliedThrough)
	next := a.clone()
	outcome := Outcome("")
	switch {
	case behaviour.decides && (late || a.supersedesAWithdrawal(event)):
		outcome = OutcomeStale
	case behaviour.stops:
		next.Renewal = RenewalStopped
		outcome = OutcomeRenewalStopped
	case event.Type == EventDowngradeScheduled:
		next.Downgrades = append(next.Downgrades, Downgrade{
			Event:      event.ID,
			Scheduled:  event.Occurred,
			Quantities: event.Quantities,
		})
		outcome = OutcomeScheduled
	default:
		issued, granted, err := a.issue(event, at)
		if err != nil {
			return a, "", err
		}
		switch {
		case !granted:
			outcome = OutcomeNoGrant
		case issued.Kind == IssuePaid:
			// A verified payment is the only thing that resumes renewal after a
			// cancellation, a refund or a chargeback stopped it.
			next.Issues = append(next.Issues, issued)
			next.Renewal = RenewalOpen
			outcome = OutcomeIssued
		default:
			next.Issues = append(next.Issues, issued)
			outcome = OutcomeProvisional
		}
	}

	next.Processed = append(next.Processed, Processed{
		Event:    event.ID,
		Occurred: event.Occurred,
		Applied:  at,
		Outcome:  outcome,
	})
	if !late {
		next.AppliedThrough = event.Occurred
	}
	if err := validateAccount(next); err != nil {
		return a, "", err
	}
	return next, outcome, nil
}

// issue decides what a completed payment or an issued invoice grants. The
// reported flag is false for an invoice alone, which is the honest answer: an
// invoice is recorded and grants nothing.
func (a Account) issue(event Event, at time.Time) (Issue, bool, error) {
	latest, issued := a.LatestIssue()
	if issued && event.Term.Starts.Before(latest.Term.Starts) {
		return Issue{}, false, ErrTermPrecedesIssue
	}
	kind := IssuePaid
	if event.Type == EventInvoiceIssued {
		if event.NetTerms != NetTermsApproved {
			return Issue{}, false, nil
		}
		kind = IssueProvisional
		if issued && event.Term.Starts.Before(latest.Term.Ends) {
			return Issue{}, false, ErrProvisionalOverlap
		}
	}
	// A mid-term increase is a paid upgrade, and the decision requires the
	// proration to have been settled before the larger grant exists.
	if kind == IssuePaid && issued && event.Term.Starts.Before(latest.Term.Ends) &&
		event.Quantities.exceeds(latest.Quantities) && event.Proration != ProrationSettled {
		return Issue{}, false, ErrProrationUnsettled
	}
	if downgrade, scheduled := a.scheduledDowngrade(); scheduled && event.Quantities.exceeds(downgrade.Quantities) {
		return Issue{}, false, ErrDowngradeExceeded
	}
	return Issue{
		Sequence:   len(a.Issues) + 1,
		Kind:       kind,
		Plan:       event.Plan,
		Event:      event.ID,
		Applied:    at,
		Term:       event.Term,
		Quantities: event.Quantities,
	}, true, nil
}

// supersedesAWithdrawal reports that an event carries information no later than
// the withdrawal that stopped renewal, and so must not act on it. Comparing
// against the applied-through time alone is not enough: an instant is recorded
// to the second, so a payment occurring in the same second as the cancellation
// that withdrew the renewal is not "before" it, and applying it would restore
// exactly the right the cancellation took away.
func (a Account) supersedesAWithdrawal(event Event) bool {
	if a.Renewal != RenewalStopped {
		return false
	}
	for i := len(a.Processed) - 1; i >= 0; i-- {
		if a.Processed[i].Outcome == OutcomeRenewalStopped {
			return !event.Occurred.After(a.Processed[i].Occurred)
		}
	}
	return false
}

func (a Account) clone() Account {
	next := a
	next.Issues = slices.Clone(a.Issues)
	next.Downgrades = slices.Clone(a.Downgrades)
	next.Processed = slices.Clone(a.Processed)
	return next
}

// Names are the parts of an entitlement billing does not know. Billing counts
// what was bought; who holds a seat, which devices they work from, which
// authorities admit runners and which capabilities a plan carries are the
// administration side's, and the identifier of the document is the issuer's.
type Names struct {
	ID           string
	Authors      []entitlement.Assignment
	Authorities  []entitlement.Authority
	Capabilities []string
}

// Claims builds the readmit-entitlement/v2 claims for the latest issue: the
// purchased term and quantities from the ledger, the assignment from the
// caller. It validates neither, because [entitlement.SignV2] validates both and
// refuses an assignment larger than the purchased scope, a capacity larger than
// the purchased instances, and a term that starts before it was issued.
func (a Account) Claims(names Names, issued time.Time) (entitlement.ClaimsV2, error) {
	latest, ok := a.LatestIssue()
	if !ok {
		return entitlement.ClaimsV2{}, ErrNoIssue
	}
	return entitlement.ClaimsV2{
		ID:           names.ID,
		Organization: a.Organization,
		Plan:         latest.Plan,
		Sequence:     latest.Sequence,
		Issued:       issued,
		NotBefore:    latest.Term.Starts,
		Expires:      latest.Term.Ends,
		GraceDays:    latest.Term.GraceDays,
		Authors: entitlement.Authors{
			Seats:          latest.Quantities.AuthorSeats,
			DevicesPerSeat: latest.Quantities.DevicesPerSeat,
			Assignments:    names.Authors,
		},
		Runners: entitlement.Runners{
			Instances:   latest.Quantities.RunnerInstances,
			Authorities: names.Authorities,
		},
		Capabilities: names.Capabilities,
	}, nil
}

// DecodeAccount reads an account ledger. Unknown members and unknown versions
// are errors; there is no migration and no repair.
func DecodeAccount(data []byte) (Account, error) {
	account, err := decode[Account](data, AccountSchema, accountKind)
	if err != nil {
		return Account{}, err
	}
	if err := validateAccount(account); err != nil {
		return Account{}, err
	}
	return account, nil
}

// EncodeAccount writes a validated ledger deterministically.
func EncodeAccount(account Account) ([]byte, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	return encode(account, accountKind)
}

func validateAccount(account Account) error {
	if account.Schema != AccountSchema {
		return ErrUnsupportedVersion
	}
	if err := entitlement.ValidateIdentifier(account.Organization); err != nil {
		return errors.New("organization: " + err.Error())
	}
	for _, m := range []struct {
		member string
		value  time.Time
	}{{"opening time", account.Opened}, {"applied-through time", account.AppliedThrough}} {
		if err := entitlement.ValidateInstant(m.value); err != nil {
			return errors.New(m.member + ": " + err.Error())
		}
	}
	if account.AppliedThrough.Before(account.Opened) {
		return errors.New("an account applies nothing from before it was opened")
	}
	if !slices.Contains(renewals, account.Renewal) {
		return errors.New("renewal: not one of open, stopped")
	}
	if err := validateIssues(account.Issues); err != nil {
		return err
	}
	if err := validateDowngrades(account.Downgrades); err != nil {
		return err
	}
	return validateProcessed(account.Processed)
}

func validateIssues(issues []Issue) error {
	if len(issues) > MaxLedgerEntries {
		return errors.New("an account ledger holds at most " + strconv.Itoa(MaxLedgerEntries) + " issues")
	}
	for i, issue := range issues {
		if issue.Sequence != i+1 {
			return errors.New("issue sequences run from 1 without a gap")
		}
		if !slices.Contains(issueKinds, issue.Kind) {
			return errors.New("issue kind: not one of paid, provisional")
		}
		if err := entitlement.ValidateIdentifier(issue.Plan); err != nil {
			return errors.New("plan: " + err.Error())
		}
		if err := entitlement.ValidateIdentifier(issue.Event); err != nil {
			return errors.New("issuing event identifier: " + err.Error())
		}
		if err := entitlement.ValidateInstant(issue.Applied); err != nil {
			return errors.New("issue time: " + err.Error())
		}
		if err := validateTerm(issue.Term); err != nil {
			return err
		}
		if err := validateQuantities(issue.Quantities); err != nil {
			return err
		}
	}
	return nil
}

func validateDowngrades(downgrades []Downgrade) error {
	if len(downgrades) > MaxLedgerEntries {
		return errors.New("an account ledger holds at most " + strconv.Itoa(MaxLedgerEntries) + " scheduled downgrades")
	}
	for i, downgrade := range downgrades {
		if err := entitlement.ValidateIdentifier(downgrade.Event); err != nil {
			return errors.New("scheduling event identifier: " + err.Error())
		}
		if err := entitlement.ValidateInstant(downgrade.Scheduled); err != nil {
			return errors.New("downgrade time: " + err.Error())
		}
		if err := validateQuantities(downgrade.Quantities); err != nil {
			return err
		}
		if i > 0 && downgrade.Scheduled.Before(downgrades[i-1].Scheduled) {
			return errors.New("scheduled downgrades are recorded in the order they arrived")
		}
	}
	return nil
}

func validateProcessed(processed []Processed) error {
	if len(processed) > MaxLedgerEntries {
		return errors.New("an account ledger holds at most " + strconv.Itoa(MaxLedgerEntries) + " processed events")
	}
	for i, record := range processed {
		if err := entitlement.ValidateIdentifier(record.Event); err != nil {
			return errors.New("processed event identifier: " + err.Error())
		}
		for _, m := range []struct {
			member string
			value  time.Time
		}{{"event time", record.Occurred}, {"processing time", record.Applied}} {
			if err := entitlement.ValidateInstant(m.value); err != nil {
				return errors.New(m.member + ": " + err.Error())
			}
		}
		if record.Applied.Before(record.Occurred) {
			return ErrNotYetOccurred
		}
		if !slices.Contains(outcomes, record.Outcome) {
			return errors.New("outcome: not one of issued, provisional, no-grant, scheduled, renewal-stopped, duplicate, stale")
		}
		if i > 0 && record.Applied.Before(processed[i-1].Applied) {
			return errors.New("processed events are recorded in the order they were processed")
		}
		if slices.ContainsFunc(processed[:i], func(p Processed) bool { return p.Event == record.Event }) {
			return errors.New("an event is processed once")
		}
	}
	return nil
}
