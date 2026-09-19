package commercial_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"github.com/bharm16/readmit/internal/billing"
	"github.com/bharm16/readmit/internal/commercial"
	"github.com/bharm16/readmit/internal/entitlement"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestContactsRoundTripAndRefusal(t *testing.T) {
	a, err := commercial.New("hospital", []commercial.Contact{{Role: "administrator", Address: "admin@example.invalid"}, {Role: "billing", Address: "billing@example.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := commercial.Encode(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commercial.Decode(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"role":"administrator"`), []byte(`"role":"administrator","raw_evidence":"forbidden"`), 1),
		bytes.Replace(data, []byte(`"invoices":[]`), []byte(`"invoices":null`), 1),
		bytes.Replace(data, []byte(`"invoices":[],`), nil, 1),
	} {
		if _, err := commercial.Decode(bad); err == nil {
			t.Fatal("accepted invalid document")
		}
	}
	if _, err := a.WithContacts([]commercial.Contact{{Role: "administrator", Address: "Display <admin@example.invalid>"}}); err == nil {
		t.Fatal("accepted display text")
	}
}

func TestTransferRenewalAndCancelledPaidSupport(t *testing.T) {
	a, _ := commercial.New("hospital", []commercial.Contact{{Role: "administrator", Address: "admin@example.invalid"}})
	at := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	ledger, _ := billing.Open("hospital", at)
	invoice := billing.Event{ID: "invoice-event", Type: billing.EventInvoiceIssued, Occurred: at, Account: "hospital", Plan: "annual", Term: billing.Term{Starts: at, Ends: at.AddDate(1, 0, 0), GraceDays: 14}, Quantities: billing.Quantities{AuthorSeats: 1, DevicesPerSeat: 2, RunnerInstances: 1}, Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone}
	ledger, _, err := ledger.Apply(invoice, at)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.RecordInvoice(ledger, commercial.Invoice{ID: "invoice-1", Event: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(nil)
	names := billing.Names{ID: "ent-1", Authors: []entitlement.Assignment{{Author: "analyst", Devices: []string{"a", "b"}}}, Authorities: []entitlement.Authority{{ID: "pool", Instances: 1}}, Capabilities: []string{"replay"}}
	if _, _, err := a.Issue(context.Background(), ledger, names, "vendor", key, at); err == nil {
		t.Fatal("invoice granted")
	}
	payment := invoice
	payment.ID = "payment-1"
	payment.Type = billing.EventPaymentCompleted
	ledger, _, err = ledger.Apply(payment, at)
	if err != nil {
		t.Fatal(err)
	}
	a, data, err := a.Issue(context.Background(), ledger, names, "vendor", key, at)
	if err != nil {
		t.Fatal(err)
	}
	first, err := entitlement.DecodeV2(data)
	if err != nil || first.Entitlement.Sequence != 1 {
		t.Fatal(err)
	}
	names.ID = "ent-2"
	names.Authors[0].Devices = []string{"b", "c"}
	a, data, err = a.Issue(context.Background(), ledger, names, "vendor", key, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	transfer, _ := entitlement.DecodeV2(data)
	if transfer.Entitlement.Sequence != 2 || transfer.Entitlement.NotBefore != at.Add(time.Hour) || transfer.Entitlement.Expires != at.AddDate(1, 0, 0) {
		t.Fatal("transfer changed purchased term or failed to advance")
	}
	saved, _ := commercial.Encode(a)
	restored, err := commercial.Decode(saved)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.Export("ent-2")
	if err != nil || !bytes.Equal(data, recovered) {
		t.Fatal("recovery changed signed output")
	}
	if _, _, err := a.Issue(context.Background(), ledger, names, "vendor", key, at.Add(2*time.Hour)); err == nil {
		t.Fatal("duplicate issuance id reused")
	}
	cancel := billing.Event{ID: "cancel", Type: billing.EventCancellationRecorded, Occurred: at.Add(2 * time.Hour), Account: "hospital", Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone}
	ledger, _, err = ledger.Apply(cancel, cancel.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	status, err := a.Status(ledger, cancel.Occurred)
	if err != nil || !status.SupportIncluded || status.Renewal != billing.RenewalStopped {
		t.Fatalf("status %+v: %v", status, err)
	}
	status, err = a.Status(ledger, at.AddDate(1, 0, 0))
	if err != nil || status.SupportIncluded {
		t.Fatal("support extended past paid term")
	}
	payment.ID = "payment-2"
	payment.Occurred = at.AddDate(1, 0, 0)
	payment.Term.Starts = payment.Occurred
	payment.Term.Ends = payment.Occurred.AddDate(1, 0, 0)
	ledger, _, err = ledger.Apply(payment, payment.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	names.ID = "ent-3"
	a, data, err = a.Issue(context.Background(), ledger, names, "vendor", key, payment.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	renewal, _ := entitlement.DecodeV2(data)
	if renewal.Entitlement.Sequence != 3 {
		t.Fatal("billing sequence collided with transfer")
	}
}

func TestAdministrationRefusesWithoutMutatingAndInstallsOffline(t *testing.T) {
	at := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	a, _ := commercial.New("hospital", []commercial.Contact{{Role: "administrator", Address: "admin@example.invalid"}})
	ledger, _ := billing.Open("hospital", at)
	ledger, _, err := ledger.Apply(billing.Event{ID: "paid", Type: billing.EventPaymentCompleted, Occurred: at, Account: "hospital", Plan: "annual", Term: billing.Term{Starts: at, Ends: at.AddDate(1, 0, 0), GraceDays: 14}, Quantities: billing.Quantities{AuthorSeats: 1, DevicesPerSeat: 2, RunnerInstances: 1}, Proration: billing.ProrationNone, NetTerms: billing.NetTermsNone}, at)
	if err != nil {
		t.Fatal(err)
	}
	public, key, _ := ed25519.GenerateKey(nil)
	trust := entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "vendor", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}}}
	names := billing.Names{ID: "ent-1", Authors: []entitlement.Assignment{{Author: "analyst", Devices: []string{"a", "b"}}}, Authorities: []entitlement.Authority{{ID: "pool", Instances: 1}}, Capabilities: []string{"replay"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		mutate func(*billing.Names, *billing.Account)
		at     time.Time
	}{
		{"cancelled", ctx, func(*billing.Names, *billing.Account) {}, at},
		{"third-device", context.Background(), func(n *billing.Names, _ *billing.Account) {
			n.Authors = []entitlement.Assignment{{Author: "analyst", Devices: []string{"a", "b", "c"}}}
		}, at},
		{"extra-author", context.Background(), func(n *billing.Names, _ *billing.Account) {
			n.Authors = append(slices.Clone(n.Authors), entitlement.Assignment{Author: "other", Devices: []string{}})
		}, at},
		{"extra-runner", context.Background(), func(n *billing.Names, _ *billing.Account) {
			n.Authorities = []entitlement.Authority{{ID: "pool", Instances: 2}}
		}, at},
		{"foreign-account", context.Background(), func(_ *billing.Names, l *billing.Account) { l.Organization = "other" }, at},
		{"expired", context.Background(), func(*billing.Names, *billing.Account) {}, at.AddDate(1, 0, 0)},
		{"before-purchase", context.Background(), func(*billing.Names, *billing.Account) {}, at.Add(-time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := commercial.Encode(a)
			n, l := names, ledger
			tc.mutate(&n, &l)
			refused, data, err := a.Issue(tc.ctx, l, n, "vendor", key, tc.at)
			after, _ := commercial.Encode(refused)
			if err == nil || data != nil || !bytes.Equal(before, after) {
				t.Fatal("refusal mutated state or produced document")
			}
		})
	}
	a, data, err := a.Issue(context.Background(), ledger, names, "vendor", key, at)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := entitlement.VerifyV2(data, trust)
	if err != nil || grant.Assigned("analyst", "b") != nil {
		t.Fatal(err)
	}
	if _, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "wrong"), data, trust, "analyst", "c", at); err == nil {
		t.Fatal("wrong device installed")
	}
	store, err := entitlement.ImportV2(filepath.Join(t.TempDir(), "store"), data, trust, "analyst", "b", at)
	if err != nil {
		t.Fatal(err)
	}
	names.ID = "ent-2"
	names.Authors = []entitlement.Assignment{{Author: "analyst", Devices: []string{"b", "c"}}}
	a, data, err = a.Issue(context.Background(), ledger, names, "vendor", key, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Renew(data, trust); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Export(filepath.Join(t.TempDir(), "export.json")); err != nil {
		t.Fatal(err)
	}
	stale, _ := billing.Open("hospital", at)
	if _, _, err := a.Issue(context.Background(), stale, names, "vendor", key, at.Add(2*time.Hour)); err == nil {
		t.Fatal("stale ledger accepted")
	}
	if _, err := a.RecordInvoice(ledger, commercial.Invoice{ID: "invented", Event: "paid"}); err == nil {
		t.Fatal("payment relabelled invoice")
	}
}
