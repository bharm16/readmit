package commercial

import (
	"context"
	"crypto/ed25519"
	"errors"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/billing"
	"github.com/bharm16/readmit/internal/entitlement"
)

func (a Account) checkLedger(ledger billing.Account) error {
	if err := validate(a); err != nil {
		return err
	}
	if _, err := billing.EncodeAccount(ledger); err != nil {
		return errors.New("invalid billing account")
	}
	if ledger.Organization != a.Organization {
		return errors.New("billing account belongs to another organization")
	}
	if len(a.Issues) > 0 {
		latest, ok := ledger.LatestIssue()
		if !ok || latest.Sequence < a.Issues[len(a.Issues)-1].BillingSequence {
			return errors.New("billing account predates administrative issuance")
		}
	}
	return nil
}

// RecordInvoice associates an opaque invoice reference with an authenticated
// invoice event already recorded by billing. A reference never proves payment.
func (a Account) RecordInvoice(ledger billing.Account, invoice Invoice) (Account, error) {
	if err := a.checkLedger(ledger); err != nil {
		return a, err
	}
	found := slices.ContainsFunc(ledger.Processed, func(p billing.Processed) bool {
		return p.Event == invoice.Event && (p.Outcome == billing.OutcomeNoGrant || p.Outcome == billing.OutcomeProvisional)
	})
	if !found {
		return a, errors.New("invoice needs a recorded invoice event")
	}
	next := a
	next.Invoices = append(slices.Clone(a.Invoices), invoice)
	if err := validate(next); err != nil {
		return a, err
	}
	return next, nil
}

// Issue replaces assignments within the latest purchased/provisional scope.
// Only this API should issue for an account once administration begins: mixing
// independent issuers can reuse sequences. The host must commit next durably
// under an organization lock/CAS before delivering data, retaining the previous
// revision on failure. Retrying delivery uses Export, never another Issue.
// Signing keys are supplied by the vendor's credential boundary, never stored.
func (a Account) Issue(ctx context.Context, ledger billing.Account, names billing.Names, keyID string, key ed25519.PrivateKey, at time.Time) (next Account, data []byte, err error) {
	if err = ctx.Err(); err != nil {
		return a, nil, err
	}
	if err = a.checkLedger(ledger); err != nil {
		return a, nil, err
	}
	if len(a.Issues) >= MaxEntries {
		return a, nil, errors.New("administration issuance history is full")
	}
	if _, err = a.Export(names.ID); err == nil {
		return a, nil, errors.New("entitlement identifier was already issued; export the retained document")
	}
	claims, err := ledger.Claims(names, at)
	if err != nil {
		return a, nil, err
	}
	latest, _ := ledger.LatestIssue()
	if claims.Authors.DevicesPerSeat != 2 {
		return a, nil, errors.New("commercial authors require two devices per seat")
	}
	if !at.Before(claims.Expires) {
		return a, nil, errors.New("cannot reissue an ended purchased term")
	}
	if at.Before(latest.Applied) {
		return a, nil, errors.New("issuance predates the billing decision")
	}
	if len(a.Issues) > 0 {
		previous, _ := entitlement.DecodeV2(a.Issues[len(a.Issues)-1].Entitlement)
		if at.Before(previous.Entitlement.Issued) {
			return a, nil, errors.New("issuance time moved backwards")
		}
		if claims.Sequence <= previous.Entitlement.Sequence {
			claims.Sequence = previous.Entitlement.Sequence + 1
		}
	}
	// v2 forbids backdating. A mid-term transfer starts now and preserves the
	// purchased expiry and grace; it never extends the paid-through term.
	if at.After(claims.NotBefore) {
		claims.NotBefore = at
	}
	doc, err := entitlement.SignV2(claims, keyID, key)
	if err != nil {
		return a, nil, err
	}
	data, err = entitlement.EncodeV2(doc)
	if err != nil {
		return a, nil, err
	}
	next = a
	next.Issues = append(slices.Clone(a.Issues), Issuance{BillingSequence: latest.Sequence, Entitlement: slices.Clone(data)})
	if _, err = Encode(next); err != nil {
		return a, nil, err
	}
	if err = ctx.Err(); err != nil {
		return a, nil, err
	}
	return next, data, nil
}

// Export recovers the exact signed bytes retained for delivery. Expiry,
// cancellation and replacement do not remove an already issued document.
func (a Account) Export(id string) ([]byte, error) {
	if err := validate(a); err != nil {
		return nil, err
	}
	for _, issue := range a.Issues {
		doc, _ := entitlement.DecodeV2(issue.Entitlement)
		if doc.Entitlement.ID == id {
			return slices.Clone(issue.Entitlement), nil
		}
	}
	return nil, errors.New("entitlement is not in administrative history")
}

// Status is an in-memory report, not a new persisted contract. SupportIncluded
// is the adopted service scope during an actual annual paid term, not a legal
// approval or a promise that a staffed support operation has launched.
type Status struct {
	Renewal             billing.Renewal
	PaidThrough         time.Time
	SupportIncluded     bool
	SupportHours        string
	FirstResponseTarget string
	OfflineRevocation   string
}

func (a Account) Status(ledger billing.Account, at time.Time) (Status, error) {
	if err := a.checkLedger(ledger); err != nil {
		return Status{}, err
	}
	if err := entitlement.ValidateInstant(at); err != nil {
		return Status{}, err
	}
	s := Status{Renewal: ledger.Renewal, SupportHours: "Monday-Friday 09:00-17:00 America/Chicago", FirstResponseTarget: "two business days; no guaranteed resolution or 24/7 incident SLA", OfflineRevocation: "effective only when updated trust or entitlement reaches each installation; restored copies remain outside enforcement"}
	s.PaidThrough, _ = ledger.PaidThrough()
	for _, issue := range ledger.Issues {
		if issue.Kind == billing.IssuePaid && !at.Before(issue.Applied) && !at.Before(issue.Term.Starts) && at.Before(issue.Term.Ends) && issue.Term.Ends.Equal(issue.Term.Starts.AddDate(1, 0, 0)) {
			s.SupportIncluded = true
		}
	}
	return s, nil
}
