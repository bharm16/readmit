// Package commercial is the trusted vendor administrator's offline account
// boundary. It holds account contacts and invoice references, and reissues v2
// assignments against purchased billing scope. It performs no network or disk
// operations. Its caller authenticates administrators and serializes durable
// account updates before delivering any signed entitlement.
package commercial

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/mail"
	"slices"

	"github.com/bharm16/readmit/internal/entitlement"
)

const Schema = "readmit-account-admin/v1"
const MaxEntries = 128

var ErrInvalid = errors.New("invalid commercial administration document")

// Contact is an explicitly supplied business mailbox, never a support message
// or evidence attachment. Role is administrator, billing or support.
type Contact struct {
	Role    string `json:"role"`
	Address string `json:"address"`
}

// Invoice is an opaque provider reference linked to a recorded invoice event.
// It carries neither a payment status nor a URL: it cannot grant access or send
// someone to an unverified payment destination.
type Invoice struct {
	ID    string `json:"id"`
	Event string `json:"event"`
}

// Issuance retains the exact signed document for retry after interruption.
// BillingSequence orders purchases; the entitlement sequence orders purchases
// and administrative transfers together and must have a single writer.
type Issuance struct {
	BillingSequence int    `json:"billing_sequence"`
	Entitlement     []byte `json:"entitlement"`
}

type Account struct {
	Schema       string     `json:"schema"`
	Organization string     `json:"organization"`
	Contacts     []Contact  `json:"contacts"`
	Invoices     []Invoice  `json:"invoices"`
	Issues       []Issuance `json:"issues"`
}

func New(organization string, contacts []Contact) (Account, error) {
	a := Account{Schema: Schema, Organization: organization, Contacts: slices.Clone(contacts), Invoices: []Invoice{}, Issues: []Issuance{}}
	return a, validate(a)
}

// WithContacts replaces the complete contact list without changing the caller.
func (a Account) WithContacts(contacts []Contact) (Account, error) {
	if err := validate(a); err != nil {
		return a, err
	}
	next := a
	next.Contacts = slices.Clone(contacts)
	if err := validate(next); err != nil {
		return a, err
	}
	return next, nil
}

func validate(a Account) error {
	if a.Schema != Schema || entitlement.ValidateIdentifier(a.Organization) != nil || len(a.Contacts) == 0 || len(a.Contacts) > 3 || len(a.Invoices) > MaxEntries || len(a.Issues) > MaxEntries {
		return ErrInvalid
	}
	roles := map[string]bool{}
	for _, c := range a.Contacts {
		if !slices.Contains([]string{"administrator", "billing", "support"}, c.Role) || roles[c.Role] || len(c.Address) > 254 {
			return ErrInvalid
		}
		address, err := mail.ParseAddress(c.Address)
		if err != nil || address.Name != "" || address.Address != c.Address || bytes.ContainsAny([]byte(c.Address), "\r\n\x00") {
			return ErrInvalid
		}
		roles[c.Role] = true
	}
	if !roles["administrator"] {
		return ErrInvalid
	}
	ids, events := map[string]bool{}, map[string]bool{}
	for _, invoice := range a.Invoices {
		if entitlement.ValidateIdentifier(invoice.ID) != nil || entitlement.ValidateIdentifier(invoice.Event) != nil || ids[invoice.ID] || events[invoice.Event] {
			return ErrInvalid
		}
		ids[invoice.ID], events[invoice.Event] = true, true
	}
	lastSequence, lastBilling := 0, 0
	for _, issued := range a.Issues {
		d, err := entitlement.DecodeV2(issued.Entitlement)
		if err != nil || issued.BillingSequence < lastBilling || issued.BillingSequence < 1 || d.Entitlement.Organization != a.Organization || d.Entitlement.Sequence <= lastSequence || d.Entitlement.Authors.DevicesPerSeat != 2 {
			return ErrInvalid
		}
		lastSequence, lastBilling = d.Entitlement.Sequence, issued.BillingSequence
	}
	return nil
}

func Encode(a Account) ([]byte, error) {
	if err := validate(a); err != nil {
		return nil, err
	}
	data, err := json.Marshal(a, json.Deterministic(true))
	if err != nil || len(data)+1 > entitlement.MaxDocumentBytes {
		return nil, ErrInvalid
	}
	return append(data, '\n'), nil
}

func Decode(data []byte) (Account, error) {
	var a Account
	if len(data) > entitlement.MaxDocumentBytes || required(data, "schema", "organization", "contacts", "invoices", "issues") != nil {
		return a, ErrInvalid
	}
	if err := json.Unmarshal(data, &a, json.RejectUnknownMembers(true)); err != nil {
		return Account{}, ErrInvalid
	}
	return a, validate(a)
}

// Each custom decoder checks required presence before strict decoding; zero,
// omitted and explicit null are not interchangeable even inside an array.
func required(data []byte, names ...string) error {
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil || members == nil {
		return ErrInvalid
	}
	for _, name := range names {
		v, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrInvalid
		}
	}
	return nil
}

func (c *Contact) UnmarshalJSON(data []byte) error {
	if err := required(data, "role", "address"); err != nil {
		return err
	}
	type plain Contact
	return json.Unmarshal(data, (*plain)(c), json.RejectUnknownMembers(true))
}
func (i *Invoice) UnmarshalJSON(data []byte) error {
	if err := required(data, "id", "event"); err != nil {
		return err
	}
	type plain Invoice
	return json.Unmarshal(data, (*plain)(i), json.RejectUnknownMembers(true))
}
func (i *Issuance) UnmarshalJSON(data []byte) error {
	if err := required(data, "billing_sequence", "entitlement"); err != nil {
		return err
	}
	type plain Issuance
	return json.Unmarshal(data, (*plain)(i), json.RejectUnknownMembers(true))
}
