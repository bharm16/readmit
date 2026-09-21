// Package capability owns the coverage ledger the application-completion
// program (#244) is tracked against: one checked, versioned, strict-JSON
// document mapping every customer capability the product actually ships —
// the runnable command tree, the bound application methods, and the
// customer-hub service routes — to the owning ticket and the screen, action
// and test that answer it. The ledger is data, not code: siblings extend it
// by adding rows under the same contract, and the checks beside each surface
// fail when a new capability ships without one.
package capability

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/strictdoc"
)

// Schema is the only ledger contract this release reads. A new member means a
// new version string and a reader for both, never an added member here.
const Schema = "readmit-capability-ledger/v1"

// Surface kinds one row can name. A row's source is the runnable fact it
// covers, spelled the way that surface spells it: a command path for the CLI,
// the bound method for the application, the method and route for the service.
const (
	KindCLI     = "cli"
	KindDesktop = "desktop"
	KindHub     = "hub"
)

// Row maps one customer capability to its owner and its eventual or actual
// screen, action and test. A row with a disposition records why a surface is
// not customer work at all, so the exemption is itself reviewed data rather
// than a loophole a reader skips.
type Row struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Kind        string `json:"kind"`
	Capability  string `json:"capability"`
	Owner       string `json:"owner"`
	Screen      string `json:"screen,omitzero"`
	Action      string `json:"action,omitzero"`
	Test        string `json:"test,omitzero"`
	Implemented bool   `json:"implemented"`
	Disposition string `json:"disposition,omitzero"`
}

// Ledger is the whole document.
type Ledger struct {
	Schema string `json:"schema"`
	// Program names the tracking issue the ledger serves, so the document
	// states what it is coverage for rather than assuming it.
	Program string `json:"program"`
	Rows    []Row  `json:"rows"`
}

var ownerPattern = regexp.MustCompile(`^bharm16/readmit#[0-9]+$`)

// Validate reports the first reason the ledger cannot be read as coverage.
// Every row needs an owning ticket; a row without a disposition needs the
// screen, action and test that answer it; two rows never cover one source.
func Validate(ledger Ledger) error {
	if ledger.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if strings.TrimSpace(ledger.Program) == "" {
		return errors.New("the ledger names the program it covers")
	}
	if len(ledger.Rows) == 0 {
		return errors.New("the ledger holds at least one row")
	}
	ids := make(map[string]bool, len(ledger.Rows))
	sources := make(map[string]bool, len(ledger.Rows))
	for _, row := range ledger.Rows {
		if strings.TrimSpace(row.ID) == "" {
			return errors.New("every row has an id")
		}
		if ids[row.ID] {
			return errors.New("two rows share the id " + row.ID)
		}
		if row.Source == "" {
			return errors.New("row " + row.ID + " names the runnable source it covers")
		}
		key := row.Kind + " " + row.Source
		if sources[key] {
			return errors.New("two rows cover " + key)
		}
		sources[key] = true
		switch row.Kind {
		case KindCLI, KindDesktop, KindHub:
		default:
			return errors.New("row " + row.ID + " names a surface the ledger tracks")
		}
		if strings.TrimSpace(row.Capability) == "" {
			return errors.New("row " + row.ID + " says what the capability is")
		}
		if !ownerPattern.MatchString(row.Owner) {
			return errors.New("row " + row.ID + " names its owning issue as bharm16/readmit#N")
		}
		if strings.TrimSpace(row.Disposition) != "" {
			// A disposition is the whole answer for a row that is not
			// customer work; it cannot half-claim a screen as well.
			if row.Screen != "" || row.Action != "" || row.Test != "" || row.Implemented {
				return errors.New("row " + row.ID + " is either disposed or covered, not both")
			}
			continue
		}
		for _, field := range []struct{ name, value string }{
			{"screen", row.Screen}, {"action", row.Action}, {"test", row.Test},
		} {
			if strings.TrimSpace(field.value) == "" {
				return errors.New("row " + row.ID + " names its " + field.name)
			}
		}
	}
	return nil
}

// ErrUnsupportedVersion reports a ledger written under a contract version this
// release does not read.
var ErrUnsupportedVersion = errors.New("unsupported capability ledger version")

var ledgerDocument = strictdoc.Document{
	MaxBytes:    4 << 20,
	Schema:      Schema,
	Required:    []string{"program", "rows"},
	Invalid:     "invalid capability ledger",
	TooLarge:    "capability ledger exceeds its size limit",
	MustDeclare: "a capability ledger declares its contract version",
	Unsupported: ErrUnsupportedVersion,
	Requires:    "a capability ledger declares the program it covers and its rows",
}

// Decode reads the ledger strictly: unknown members and unknown versions are
// errors, and there is no migration and no repair.
func Decode(data []byte) (Ledger, error) {
	var ledger Ledger
	if err := ledgerDocument.Decode(data, &ledger); err != nil {
		return Ledger{}, err
	}
	if err := Validate(ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

// Covered reports whether one source of a surface has a row.
func (l Ledger) Covered(kind, source string) bool {
	return slices.ContainsFunc(l.Rows, func(row Row) bool {
		return row.Kind == kind && row.Source == source
	})
}
