package billing_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/billing"
	"github.com/bharm16/readmit/internal/entitlement"
)

// soundLedger is a well-formed ledger with one paid issue, rebuilt for each
// case so an alteration never reaches the next one.
func soundLedger(t *testing.T) billing.Account {
	t.Helper()
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	return account
}

func opened(t *testing.T) billing.Account {
	t.Helper()
	account, err := billing.Open("example-hospital", moment(2026, time.September, 1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return account
}

func apply(t *testing.T, account billing.Account, event billing.Event, at time.Time) (billing.Account, billing.Outcome) {
	t.Helper()
	next, outcome, err := account.Apply(event, at)
	if err != nil {
		t.Fatalf("apply %s: %v", event.ID, err)
	}
	return next, outcome
}

func refuse(t *testing.T, account billing.Account, event billing.Event, at time.Time, want error) {
	t.Helper()
	next, outcome, err := account.Apply(event, at)
	if !errors.Is(err, want) {
		t.Fatalf("got %v (%s), want %v", err, outcome, want)
	}
	if encoded(t, next) != encoded(t, account) {
		t.Fatal("a refused event changed the ledger")
	}
}

func encoded(t *testing.T, account billing.Account) string {
	t.Helper()
	data, err := billing.EncodeAccount(account)
	if err != nil {
		t.Fatalf("encode ledger: %v", err)
	}
	return string(data)
}

func invoice(id string, occurred time.Time, terms billing.NetTerms, term billing.Term, quantities billing.Quantities) billing.Event {
	return billing.Event{
		ID: id, Type: billing.EventInvoiceIssued, Occurred: occurred, Account: "example-hospital",
		Plan: "example-plan", Term: term, Quantities: quantities,
		Proration: billing.ProrationNone, NetTerms: terms,
	}
}

func payment(id string, occurred time.Time, term billing.Term, quantities billing.Quantities, proration billing.Proration) billing.Event {
	return billing.Event{
		ID: id, Type: billing.EventPaymentCompleted, Occurred: occurred, Account: "example-hospital",
		Plan: "example-plan", Term: term, Quantities: quantities,
		Proration: proration, NetTerms: billing.NetTermsNone,
	}
}

func withdrawal(id string, kind billing.EventType, occurred time.Time) billing.Event {
	return billing.Event{
		ID: id, Type: kind, Occurred: occurred, Account: "example-hospital",
		Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone,
	}
}

func downgrade(id string, occurred time.Time, quantities billing.Quantities) billing.Event {
	return billing.Event{
		ID: id, Type: billing.EventDowngradeScheduled, Occurred: occurred, Account: "example-hospital",
		Quantities: quantities, Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone,
	}
}

var (
	firstTerm  = billing.Term{Starts: moment(2026, time.September, 18), Ends: moment(2027, time.September, 18), GraceDays: 14}
	secondTerm = billing.Term{Starts: moment(2027, time.September, 18), Ends: moment(2028, time.September, 18), GraceDays: 14}
	bought     = billing.Quantities{AuthorSeats: 3, DevicesPerSeat: 2, RunnerInstances: 1}
	reduced    = billing.Quantities{AuthorSeats: 2, DevicesPerSeat: 2, RunnerInstances: 1}
)

// The purchased lifecycle end to end: an invoice authorizes nothing, the
// verified payment issues the first term, a renewal payment issues the next
// organization-scoped sequence, and a cancellation stops future renewal while
// the paid-through term it already bought stands untouched.
func TestPurchaseRenewalAndCancellationThroughOneLedger(t *testing.T) {
	account := opened(t)

	account, outcome := apply(t, account, invoice("evt-0001", moment(2026, time.September, 10), billing.NetTermsNone, firstTerm, bought), moment(2026, time.September, 10))
	if outcome != billing.OutcomeNoGrant || len(account.Issues) != 0 {
		t.Fatalf("an invoice alone granted %s with %d issues", outcome, len(account.Issues))
	}

	account, outcome = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	if outcome != billing.OutcomeIssued {
		t.Fatalf("verified payment produced %s", outcome)
	}
	first, _ := account.LatestIssue()
	if first.Sequence != 1 || first.Kind != billing.IssuePaid || first.Quantities != bought {
		t.Fatalf("first issue is %+v", first)
	}

	account, outcome = apply(t, account, payment("evt-0003", moment(2027, time.September, 17), secondTerm, bought, billing.ProrationNone), moment(2027, time.September, 17))
	renewed, _ := account.LatestIssue()
	if outcome != billing.OutcomeIssued || renewed.Sequence != 2 {
		t.Fatalf("renewal produced %s at sequence %d", outcome, renewed.Sequence)
	}

	account, outcome = apply(t, account, withdrawal("evt-0004", billing.EventCancellationRecorded, moment(2027, time.October, 1)), moment(2027, time.October, 1))
	paidThrough, paid := account.PaidThrough()
	if outcome != billing.OutcomeRenewalStopped || account.Renewal != billing.RenewalStopped {
		t.Fatalf("cancellation produced %s leaving renewal %s", outcome, account.Renewal)
	}
	if !paid || !paidThrough.Equal(secondTerm.Ends) || len(account.Issues) != 2 {
		t.Fatalf("cancellation disturbed the paid-through term: %v (%d issues)", paidThrough, len(account.Issues))
	}
}

// Redelivery is ordinary: the same event identifier is reported as a duplicate
// and leaves the ledger byte for byte what it was, so processing an event twice
// cannot issue twice.
func TestRedeliveredEventLeavesTheLedgerUnchanged(t *testing.T) {
	account := opened(t)
	event := payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone)
	account, _ = apply(t, account, event, moment(2026, time.September, 18))
	before := encoded(t, account)

	again, outcome := apply(t, account, event, moment(2026, time.September, 19))
	if outcome != billing.OutcomeDuplicate {
		t.Fatalf("redelivery produced %s", outcome)
	}
	if encoded(t, again) != before {
		t.Fatal("redelivery changed the ledger")
	}
}

// The rule the ordering exists for: a payment that occurred before a
// cancellation cannot restore the renewal right the cancellation withdrew, even
// when it is delivered afterwards. It is recorded and applied to nothing.
func TestStalePaymentCannotRestoreWithdrawnRenewal(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	account, _ = apply(t, account, withdrawal("evt-0003", billing.EventCancellationRecorded, moment(2026, time.October, 1)), moment(2026, time.October, 1))

	stale := payment("evt-0004", moment(2026, time.September, 25), secondTerm, bought, billing.ProrationNone)
	account, outcome := apply(t, account, stale, moment(2026, time.October, 2))
	if outcome != billing.OutcomeStale {
		t.Fatalf("stale payment produced %s", outcome)
	}
	if account.Renewal != billing.RenewalStopped || len(account.Issues) != 1 {
		t.Fatalf("stale payment restored renewal %s with %d issues", account.Renewal, len(account.Issues))
	}
	if !account.AppliedThrough.Equal(moment(2026, time.October, 1)) {
		t.Fatalf("stale payment moved the applied-through time to %v", account.AppliedThrough)
	}
	last := account.Processed[len(account.Processed)-1]
	if last.Event != "evt-0004" || last.Outcome != billing.OutcomeStale {
		t.Fatalf("stale payment was not recorded: %+v", last)
	}
}

// An instant is recorded to the second, so "older than the last event applied"
// is not on its own enough: a payment occurring in the same second as the
// cancellation that withdrew the renewal carries no later information and must
// not restore it either.
func TestPaymentInTheSameSecondAsACancellationCannotRestoreRenewal(t *testing.T) {
	withdrawn := moment(2026, time.October, 1)
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	account, _ = apply(t, account, withdrawal("evt-0003", billing.EventCancellationRecorded, withdrawn), withdrawn)

	account, outcome := apply(t, account, payment("evt-0004", withdrawn, secondTerm, bought, billing.ProrationNone), moment(2026, time.October, 2))
	if outcome != billing.OutcomeStale || account.Renewal != billing.RenewalStopped {
		t.Fatalf("a payment of the withdrawal's own second produced %s leaving renewal %s", outcome, account.Renewal)
	}
	if len(account.Issues) != 1 {
		t.Fatalf("it issued anyway: %d issues", len(account.Issues))
	}

	later := withdrawn.Add(time.Second)
	account, outcome = apply(t, account, payment("evt-0005", later, secondTerm, bought, billing.ProrationNone), moment(2026, time.October, 3))
	if outcome != billing.OutcomeIssued || account.Renewal != billing.RenewalOpen {
		t.Fatalf("a payment one second later produced %s leaving renewal %s", outcome, account.Renewal)
	}
}

// A late event that can only withdraw is applied whenever it arrives: arriving
// late cannot make a cancellation wrong. It leaves the applied-through time
// where it was, so it never makes a later grant look stale.
func TestLateWithdrawalStillStopsRenewal(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))

	account, outcome := apply(t, account, withdrawal("evt-0003", billing.EventRefundRecorded, moment(2026, time.September, 12)), moment(2026, time.September, 20))
	if outcome != billing.OutcomeRenewalStopped || account.Renewal != billing.RenewalStopped {
		t.Fatalf("late refund produced %s leaving renewal %s", outcome, account.Renewal)
	}
	if !account.AppliedThrough.Equal(moment(2026, time.September, 18)) {
		t.Fatalf("late refund moved the applied-through time to %v", account.AppliedThrough)
	}
	if len(account.Issues) != 1 {
		t.Fatal("a refund withdrew an issue the ledger had already made")
	}
}

// Recovery: a chargeback stops renewal, and a later verified payment is what
// resumes it. Nothing else does.
func TestVerifiedPaymentAfterAChargebackResumesRenewal(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	account, _ = apply(t, account, withdrawal("evt-0003", billing.EventChargebackRecorded, moment(2026, time.October, 1)), moment(2026, time.October, 1))
	if account.Renewal != billing.RenewalStopped {
		t.Fatalf("chargeback left renewal %s", account.Renewal)
	}

	account, outcome := apply(t, account, payment("evt-0004", moment(2027, time.September, 1), secondTerm, bought, billing.ProrationNone), moment(2027, time.September, 1))
	if outcome != billing.OutcomeIssued || account.Renewal != billing.RenewalOpen {
		t.Fatalf("payment after a chargeback produced %s leaving renewal %s", outcome, account.Renewal)
	}
}

// An approved net-terms customer receives a bounded provisional entitlement
// from an invoice alone, and it is never paid access: it does not move the
// paid-through term, and it may not overlap a term already issued.
func TestApprovedNetTermsGrantsAProvisionalIssueThatIsNotPaidAccess(t *testing.T) {
	account := opened(t)
	account, outcome := apply(t, account, invoice("evt-0001", moment(2026, time.September, 10), billing.NetTermsApproved, firstTerm, bought), moment(2026, time.September, 10))
	provisional, _ := account.LatestIssue()
	if outcome != billing.OutcomeProvisional || provisional.Kind != billing.IssueProvisional {
		t.Fatalf("approved net terms produced %s of kind %s", outcome, provisional.Kind)
	}
	if _, paid := account.PaidThrough(); paid {
		t.Fatal("a provisional issue recorded a paid-through term")
	}

	overlapping := invoice("evt-0002", moment(2026, time.September, 20), billing.NetTermsApproved,
		billing.Term{Starts: moment(2026, time.December, 1), Ends: moment(2027, time.December, 1), GraceDays: 0}, reduced)
	refuse(t, account, overlapping, moment(2026, time.September, 20), billing.ErrProvisionalOverlap)

	settled := payment("evt-0003", moment(2026, time.September, 25), firstTerm, bought, billing.ProrationNone)
	account, outcome = apply(t, account, settled, moment(2026, time.September, 25))
	latest, _ := account.LatestIssue()
	if outcome != billing.OutcomeIssued || latest.Sequence != 2 || latest.Kind != billing.IssuePaid {
		t.Fatalf("settling the invoice produced %s at %+v", outcome, latest)
	}
}

// A mid-term increase is a paid upgrade, and the decision requires proration to
// be explicit: an increase that does not state settled proration is refused
// rather than granted and reconciled later.
func TestMidTermIncreaseNeedsSettledProration(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))

	midTerm := billing.Term{Starts: moment(2027, time.March, 1), Ends: firstTerm.Ends, GraceDays: 14}
	larger := billing.Quantities{AuthorSeats: 6, DevicesPerSeat: 2, RunnerInstances: 2}
	refuse(t, account, payment("evt-0003", moment(2027, time.March, 1), midTerm, larger, billing.ProrationNone), moment(2027, time.March, 1), billing.ErrProrationUnsettled)

	account, outcome := apply(t, account, payment("evt-0004", moment(2027, time.March, 1), midTerm, larger, billing.ProrationSettled), moment(2027, time.March, 1))
	upgraded, _ := account.LatestIssue()
	if outcome != billing.OutcomeIssued || upgraded.Quantities != larger || upgraded.Sequence != 2 {
		t.Fatalf("settled upgrade produced %s at %+v", outcome, upgraded)
	}
}

// A downgrade applies at renewal and never before: the term already paid for
// keeps the quantities it was issued with, and the next issue may not exceed
// what the downgrade left.
func TestDowngradeAppliesAtRenewalAndNotBefore(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))

	account, outcome := apply(t, account, downgrade("evt-0003", moment(2026, time.October, 1), reduced), moment(2026, time.October, 1))
	current, _ := account.LatestIssue()
	if outcome != billing.OutcomeScheduled || current.Quantities != bought || len(account.Issues) != 1 {
		t.Fatalf("a scheduled downgrade changed the paid term: %s %+v", outcome, current)
	}

	refuse(t, account, payment("evt-0004", moment(2027, time.September, 1), secondTerm, bought, billing.ProrationNone), moment(2027, time.September, 1), billing.ErrDowngradeExceeded)

	account, outcome = apply(t, account, payment("evt-0005", moment(2027, time.September, 1), secondTerm, reduced, billing.ProrationNone), moment(2027, time.September, 1))
	renewed, _ := account.LatestIssue()
	if outcome != billing.OutcomeIssued || renewed.Quantities != reduced {
		t.Fatalf("renewal after a downgrade produced %s at %+v", outcome, renewed)
	}

	account, outcome = apply(t, account, payment("evt-0006", moment(2028, time.September, 1),
		billing.Term{Starts: moment(2028, time.September, 18), Ends: moment(2029, time.September, 18), GraceDays: 14}, bought, billing.ProrationNone), moment(2028, time.September, 1))
	if outcome != billing.OutcomeIssued {
		t.Fatalf("the downgrade was applied a second time: %s", outcome)
	}
}

// An event that is not this account's, one that predates the account, and one
// processed before it occurred are each refused by name, and none of them
// touches the ledger.
func TestLedgerRefusesEventsItCannotHaveProcessed(t *testing.T) {
	account := opened(t)
	elsewhere := payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone)
	elsewhere.Account = "other-hospital"
	refuse(t, account, elsewhere, moment(2026, time.September, 18), billing.ErrDifferentAccount)

	refuse(t, account, payment("evt-0003", moment(2026, time.August, 1), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18), billing.ErrBeforeAccount)
	refuse(t, account, payment("evt-0004", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 10), billing.ErrNotYetOccurred)

	backdated := payment("evt-0005", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone)
	account, _ = apply(t, account, backdated, moment(2026, time.September, 18))
	earlier := billing.Term{Starts: moment(2026, time.September, 2), Ends: moment(2027, time.September, 2), GraceDays: 14}
	refuse(t, account, payment("evt-0006", moment(2026, time.September, 19), earlier, bought, billing.ProrationNone), moment(2026, time.September, 19), billing.ErrTermPrecedesIssue)
}

// The ledger is bounded, and the bound is a refusal with a name rather than a
// document that quietly outgrows its size limit.
func TestLedgerRefusesEventsPastItsBound(t *testing.T) {
	account := opened(t)
	at := moment(2026, time.September, 2)
	for i := range billing.MaxLedgerEntries {
		account.Processed = append(account.Processed, billing.Processed{
			Event:    "evt-" + strconv.Itoa(i+1),
			Occurred: at, Applied: at, Outcome: billing.OutcomeNoGrant,
		})
	}
	account.AppliedThrough = at
	refuse(t, account, payment("evt-9999", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18), billing.ErrLedgerFull)
}

// The ledger round-trips through its own reader deterministically, refuses a
// member this release does not define, and names nothing about the customer's
// work: the encoded bytes below are every member the contract has.
func TestLedgerEncodesEveryMemberItHasAndNothingAboutEvidence(t *testing.T) {
	account := opened(t)
	account, _ = apply(t, account, invoice("evt-0001", moment(2026, time.September, 10), billing.NetTermsNone, firstTerm, bought), moment(2026, time.September, 10))
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	account, _ = apply(t, account, downgrade("evt-0003", moment(2026, time.October, 1), reduced), moment(2026, time.October, 1))

	want := `{"schema":"readmit-billing-account/v1","organization":"example-hospital",` +
		`"opened":"2026-09-01T00:00:00Z","applied_through":"2026-10-01T00:00:00Z","renewal":"open",` +
		`"issues":[{"sequence":1,"kind":"paid","plan":"example-plan","event":"evt-0002",` +
		`"applied":"2026-09-18T00:00:00Z","term":{"starts":"2026-09-18T00:00:00Z","ends":"2027-09-18T00:00:00Z","grace_days":14},` +
		`"quantities":{"author_seats":3,"devices_per_seat":2,"runner_instances":1}}],` +
		`"downgrades":[{"event":"evt-0003","scheduled":"2026-10-01T00:00:00Z",` +
		`"quantities":{"author_seats":2,"devices_per_seat":2,"runner_instances":1}}],` +
		`"processed":[{"event":"evt-0001","occurred":"2026-09-10T00:00:00Z","applied":"2026-09-10T00:00:00Z","outcome":"no-grant"},` +
		`{"event":"evt-0002","occurred":"2026-09-18T00:00:00Z","applied":"2026-09-18T00:00:00Z","outcome":"issued"},` +
		`{"event":"evt-0003","occurred":"2026-10-01T00:00:00Z","applied":"2026-10-01T00:00:00Z","outcome":"scheduled"}]}` + "\n"
	if got := encoded(t, account); got != want {
		t.Fatalf("ledger bytes:\n got %s\nwant %s", got, want)
	}

	read, err := billing.DecodeAccount([]byte(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if encoded(t, read) != want {
		t.Fatal("a ledger did not round-trip through its own reader")
	}
	altered := strings.Replace(want, `"renewal":"open"`, `"renewal":"open","case":"incident-4821"`, 1)
	if _, err := billing.DecodeAccount([]byte(altered)); err == nil || err.Error() != "invalid billing account ledger" {
		t.Fatalf("unknown member: %v", err)
	}
}

// Claims join the two halves: the purchased term and quantities come from the
// ledger, the assignment comes from the administration side, and the v2 signer
// validates both. An assignment larger than the purchased scope is refused
// there rather than signed here.
func TestClaimsCarryTheLedgerTermAndTheCallersAssignment(t *testing.T) {
	account := opened(t)
	if _, err := account.Claims(billing.Names{ID: "ENT-0002"}, moment(2026, time.September, 18)); !errors.Is(err, billing.ErrNoIssue) {
		t.Fatalf("an empty ledger issued claims: %v", err)
	}
	account, _ = apply(t, account, payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))

	names := billing.Names{
		ID:           "ENT-0002",
		Authors:      []entitlement.Assignment{{Author: "a.nguyen", Devices: []string{"lt-0091", "ws-0413"}}},
		Authorities:  []entitlement.Authority{{ID: "ci-pool-main", Instances: 1}},
		Capabilities: []string{"replay", "synth"},
	}
	claims, err := account.Claims(names, moment(2026, time.September, 18))
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if claims.Organization != "example-hospital" || claims.Plan != "example-plan" || claims.Sequence != 1 {
		t.Fatalf("claims do not carry the ledger: %+v", claims)
	}
	if claims.Authors.Seats != 3 || claims.Authors.DevicesPerSeat != 2 || claims.Runners.Instances != 1 {
		t.Fatalf("claims do not carry the purchased quantities: %+v", claims)
	}
	if !claims.NotBefore.Equal(firstTerm.Starts) || !claims.Expires.Equal(firstTerm.Ends) || claims.GraceDays != 14 {
		t.Fatalf("claims do not carry the purchased term: %+v", claims)
	}
	private, _ := testKeys(t)
	if _, err := entitlement.SignV2(claims, testKeyID, private); err != nil {
		t.Fatalf("the ledger produced claims the v2 signer refuses: %v", err)
	}

	crowded := names
	crowded.Authors = []entitlement.Assignment{
		{Author: "a.nguyen", Devices: []string{"ws-0413"}},
		{Author: "b.okafor", Devices: []string{"ws-0512"}},
		{Author: "c.silva", Devices: []string{"ws-0613"}},
		{Author: "d.tran", Devices: []string{"ws-0714"}},
	}
	overClaimed, err := account.Claims(crowded, moment(2026, time.September, 18))
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if _, err := entitlement.SignV2(overClaimed, testKeyID, private); err == nil ||
		err.Error() != "more authors are named than the entitlement grants seats" {
		t.Fatalf("signing an over-assigned scope: %v", err)
	}
}

// Every member of the ledger is checked when it is read, so a hand-edited or
// truncated ledger is refused by name rather than half-interpreted.
func TestLedgerRefusesMalformedRecords(t *testing.T) {
	at := moment(2026, time.September, 18)

	for _, c := range []struct {
		name  string
		alter func(billing.Account) billing.Account
		want  string
	}{
		{"unknown version", func(a billing.Account) billing.Account { a.Schema = "readmit-billing-account/v2"; return a },
			"unsupported billing document version"},
		{"empty organization", func(a billing.Account) billing.Account { a.Organization = ""; return a },
			"organization: must not be empty"},
		{"local opening time", func(a billing.Account) billing.Account {
			a.Opened = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.FixedZone("CDT", -5*3600))
			return a
		}, "opening time: must be UTC"},
		{"applied through before opening", func(a billing.Account) billing.Account {
			a.AppliedThrough = moment(2026, time.August, 1)
			return a
		}, "an account applies nothing from before it was opened"},
		{"unknown renewal state", func(a billing.Account) billing.Account { a.Renewal = "paused"; return a },
			"renewal: not one of open, stopped"},
		{"issue sequence with a gap", func(a billing.Account) billing.Account {
			a.Issues[0].Sequence = 2
			return a
		}, "issue sequences run from 1 without a gap"},
		{"unknown issue kind", func(a billing.Account) billing.Account { a.Issues[0].Kind = "trial"; return a },
			"issue kind: not one of paid, provisional"},
		{"issue without a plan", func(a billing.Account) billing.Account { a.Issues[0].Plan = ""; return a },
			"plan: must not be empty"},
		{"issue without its event", func(a billing.Account) billing.Account { a.Issues[0].Event = ""; return a },
			"issuing event identifier: must not be empty"},
		{"issue without an issue time", func(a billing.Account) billing.Account { a.Issues[0].Applied = time.Time{}; return a },
			"issue time: must be recorded"},
		{"issue term that ends before it starts", func(a billing.Account) billing.Account {
			a.Issues[0].Term.Ends = moment(2026, time.September, 1)
			return a
		}, "a term ends after it starts"},
		{"issue granting no devices per seat", func(a billing.Account) billing.Account {
			a.Issues[0].Quantities.DevicesPerSeat = 0
			return a
		}, "devices per seat must be between 1 and 2"},
		{"downgrade without its event", func(a billing.Account) billing.Account {
			a.Downgrades = []billing.Downgrade{{Scheduled: at, Quantities: reduced}}
			return a
		}, "scheduling event identifier: must not be empty"},
		{"downgrade without a time", func(a billing.Account) billing.Account {
			a.Downgrades = []billing.Downgrade{{Event: "evt-0003", Quantities: reduced}}
			return a
		}, "downgrade time: must be recorded"},
		{"downgrades out of order", func(a billing.Account) billing.Account {
			a.Downgrades = []billing.Downgrade{
				{Event: "evt-0003", Scheduled: moment(2026, time.October, 1), Quantities: reduced},
				{Event: "evt-0004", Scheduled: moment(2026, time.September, 20), Quantities: reduced},
			}
			return a
		}, "scheduled downgrades are recorded in the order they arrived"},
		{"processed event with an unknown outcome", func(a billing.Account) billing.Account {
			a.Processed[0].Outcome = "refunded"
			return a
		}, "outcome: not one of issued, provisional, no-grant, scheduled, renewal-stopped, duplicate, stale"},
		{"processed event without an identifier", func(a billing.Account) billing.Account {
			a.Processed[0].Event = ""
			return a
		}, "processed event identifier: must not be empty"},
		{"processed event without a time", func(a billing.Account) billing.Account {
			a.Processed[0].Occurred = time.Time{}
			return a
		}, "event time: must be recorded"},
		{"processed events out of order", func(a billing.Account) billing.Account {
			a.Processed = append(a.Processed, billing.Processed{
				Event: "evt-0003", Occurred: moment(2026, time.September, 2), Applied: moment(2026, time.September, 2),
				Outcome: billing.OutcomeNoGrant,
			})
			return a
		}, "processed events are recorded in the order they were processed"},
		{"the same event processed twice", func(a billing.Account) billing.Account {
			a.Processed = append(a.Processed, a.Processed[0])
			return a
		}, "an event is processed once"},
		{"processed before it occurred", func(a billing.Account) billing.Account {
			a.Processed[0].Occurred = moment(2026, time.September, 20)
			return a
		}, "a billing event cannot be processed before it occurred"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := billing.EncodeAccount(c.alter(soundLedger(t))); err == nil || err.Error() != c.want {
				t.Fatalf("got %v, want %s", err, c.want)
			}
		})
	}
}

// The ledger is not signed, but it is the issuer's record of what it decided,
// so a document its reader accepts must re-encode to itself too.
func FuzzBillingAccount(f *testing.F) {
	account, err := billing.Open("example-hospital", moment(2026, time.September, 1))
	if err != nil {
		f.Fatalf("open: %v", err)
	}
	account, _, err = account.Apply(payment("evt-0002", moment(2026, time.September, 18), firstTerm, bought, billing.ProrationNone), moment(2026, time.September, 18))
	if err != nil {
		f.Fatalf("apply: %v", err)
	}
	seed, err := billing.EncodeAccount(account)
	if err != nil {
		f.Fatalf("encode: %v", err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schema":"readmit-billing-account/v1","organization":"example-hospital",` +
		`"opened":"2026-09-01T00:00:00Z","applied_through":"2026-09-01T00:00:00Z","renewal":"open",` +
		`"issues":[],"downgrades":[],"processed":[]}`))
	f.Add([]byte(`{"schema":"readmit-billing-account/v1"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := billing.DecodeAccount(data)
		if err != nil {
			return
		}
		encoded, err := billing.EncodeAccount(decoded)
		if err != nil {
			t.Fatalf("an accepted ledger could not be encoded: %v", err)
		}
		again, err := billing.DecodeAccount(encoded)
		if err != nil {
			t.Fatalf("an encoded ledger was not accepted: %v", err)
		}
		second, err := billing.EncodeAccount(again)
		if err != nil || string(second) != string(encoded) {
			t.Fatalf("encoding is not stable: %v", err)
		}
	})
}
