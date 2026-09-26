// Package capability owns the coverage ledger the application-completion
// program (#244) is tracked against: one checked, versioned, strict-JSON
// document mapping every customer capability the product actually ships —
// the runnable command tree, the bound application methods, the
// customer-hub service routes and the commercial-portal operations — to the
// owning ticket, the screen and action that answer it, the shared backend
// operation behind both entry points, and the executable tests that prove it.
// The ledger is data, not code: siblings extend it by adding rows under the
// same contract, and the checks beside each surface fail when a new
// capability ships without one.
//
// Two versions of the contract are read. readmit-capability-ledger/v1 names
// its tests in prose; readmit-capability-ledger/v2 names the backend
// operation, the canonical documents it reads and writes, its prerequisites,
// and the interaction test and shared-engine parity test as references
// [Ledger.Check] resolves against the source tree, and types the disposition
// of a row that is not customer work. A v1 document still reads under v1's
// own rules; it cannot pass [Ledger.Check], because prose is not a reference
// anything can resolve.
package capability

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/strictdoc"
)

// The two ledger contracts this release reads. A new member means a new
// version string and a reader for every version, never an added member in
// one of these.
const (
	SchemaV1 = "readmit-capability-ledger/v1"
	SchemaV2 = "readmit-capability-ledger/v2"
)

// Surface kinds one row can name. A row's source is the runnable fact it
// covers, spelled the way that surface spells it: a command path for the CLI,
// the bound method for the application, the method and route or the binary's
// operation for the customer hub, the account event or vendor operation for
// the commercial portal, and the Makefile target or tools script for the
// repository's own tooling. The portal and tooling kinds are v2 member
// values; v1 has neither.
const (
	KindCLI     = "cli"
	KindDesktop = "desktop"
	KindHub     = "hub"
	KindPortal  = "portal"
	KindTooling = "tooling"
)

// The closed set of reasons a v2 row may be disposed rather than covered.
// Each one names a class of surface that is not a person's workflow in this
// product at all; none of them is a place to park customer work that has no
// screen yet — that row stays open under an owning issue instead.
const (
	// DispositionDeveloperTooling is build, test and measurement tooling for
	// developing readmit itself.
	DispositionDeveloperTooling = "developer-tooling"
	// DispositionVendorIssuance is the vendor's protected signing, issuance,
	// billing and account-administration machinery, which runs in the
	// vendor's service and never on a customer machine.
	DispositionVendorIssuance = "vendor-issuance"
	// DispositionMachineInterface is an address or process one program uses
	// to talk to another — an enrollment handshake only a runner identity may
	// call, a runner-host daemon — that no person operates; the configuration
	// a person prepares for it is covered where that person prepares it.
	DispositionMachineInterface = "machine-interface"
	// DispositionNotServed is a declared address the service deliberately
	// refuses for every caller, so no capability stands behind it.
	DispositionNotServed = "not-served"
	// DispositionSuperseded is an earlier address or binding still served
	// while the application reaches the same capability through a later one:
	// an earlier contract version of a service route kept for existing
	// clients, or a bound method no component calls any longer because a
	// later method answers the same capability. Its successor is the source
	// of the implemented row of the same surface that answers the capability
	// now, and Validate refuses one that names anything else.
	DispositionSuperseded = "superseded"
)

var dispositionKinds = []string{
	DispositionDeveloperTooling,
	DispositionVendorIssuance,
	DispositionMachineInterface,
	DispositionNotServed,
	DispositionSuperseded,
}

// Row maps one capability to its owner and to the screen, action, backend
// operation and tests that answer it. A row with a disposition records why a
// surface is not customer work at all, so the exemption is itself reviewed
// data rather than a loophole a reader skips.
type Row struct {
	ID         string
	Source     string
	Kind       string
	Capability string
	Owner      string
	Screen     string
	Action     string
	// Backend is the shared operation the entry points reach, Inputs and
	// Outputs the canonical documents it reads and writes, and Prerequisites
	// the admissions and authorities it needs before it may run. v2 only.
	Backend       Symbol
	Inputs        []string
	Outputs       []string
	Prerequisites []string
	// GUITest is the frontend interaction test that drives the screen.
	// ParityTest is the Go test that proves the engine behind it: for a cli
	// or desktop row, that the application reaches the same shared operation
	// and readers the command line does; for a hub row, the service's own
	// test of the route or operation through its real handler; for a portal
	// row, the vendor ledger's test of what the operation does to an account.
	// v2 names both on every implemented row and neither on an open one.
	GUITest    FrontendTest
	ParityTest GoTest
	// Test is v1's prose description of its tests. It is empty under v2.
	Test        string
	Implemented bool
	Disposition Disposition
}

// Disposed reports whether the row records a disposition instead of a screen.
func (r Row) Disposed() bool {
	return r.Disposition.Kind != "" || strings.TrimSpace(r.Disposition.Reason) != "" || r.Disposition.Successor != ""
}

// Symbol names one Go function or method: a package directory relative to
// the repository root, and a name that is either a function or Type.Method.
type Symbol struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

// FrontendTest names one frontend interaction test by its file, relative to
// the repository root, and its exact title.
type FrontendTest struct {
	File string `json:"file"`
	Name string `json:"name"`
}

// GoTest names one top-level Go test function by its package directory,
// relative to the repository root, and its name.
type GoTest struct {
	Package string `json:"package"`
	Name    string `json:"name"`
}

// Disposition is why a row is not customer work. Under v1 only Reason is
// set; under v2 Kind is one of the closed disposition kinds, and a
// superseded row names the source of the implemented row of its own surface
// that answers its capability now as Successor.
type Disposition struct {
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	Successor string `json:"successor,omitzero"`
}

// Ledger is the whole document in either version.
type Ledger struct {
	Schema string
	// Program names the tracking issue the ledger serves, so the document
	// states what it is coverage for rather than assuming it.
	Program string
	Rows    []Row
}

// ErrUnsupportedVersion reports a ledger written under a contract version this
// release does not read.
var ErrUnsupportedVersion = errors.New("unsupported capability ledger version")

// Decode reads the ledger strictly under whichever version it declares:
// unknown members and unknown versions are errors, and there is no migration
// and no repair.
func Decode(data []byte) (Ledger, error) {
	ledger, err := decodeVersion(data)
	if err != nil {
		return Ledger{}, err
	}
	if err := Validate(ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

func decodeVersion(data []byte) (Ledger, error) {
	var current ledgerV2
	err := documentV2.Decode(data, &current)
	if !errors.Is(err, ErrUnsupportedVersion) {
		return current.ledger(), err
	}
	var previous ledgerV1
	err = documentV1.Decode(data, &previous)
	return previous.ledger(), err
}

// Covered reports whether one source of a surface has a row.
func (l Ledger) Covered(kind, source string) bool {
	return slices.ContainsFunc(l.Rows, func(row Row) bool {
		return row.Kind == kind && row.Source == source
	})
}

var (
	ownerPattern    = regexp.MustCompile(`^bharm16/readmit#[0-9]+$`)
	symbolPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	goTestPattern   = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
	contractPattern = regexp.MustCompile(`^readmit-[a-z0-9]+(-[a-z0-9]+)*/v[0-9]+$`)
)

// RawHL7 is the one canonical input or output that is not a readmit
// contract: HL7 message bytes as a source delivered them.
const RawHL7 = "hl7"

// prerequisites is the closed set a row's prerequisites are drawn from. An
// empty list means the capability runs with no activation and no authority.
//
//   - author, execute: the local operation guard's author or execution
//     admission; execute-if-send and author-if-quota-change are the command
//     line's conditional forms of the same admissions, and author-if-approve
//     the desktop's reviewed-action form, where the final action is an
//     approval rather than a send or an export; hub-operation is the
//     guard's hub admission a licensed hub service takes for runner leases
//     and local schedule authoring.
//   - hub-certificate: the customer hub's verified mutual-TLS client
//     certificate alone; hub:<action> a signed-in hub principal whose project
//     role or scoped token grants that action.
//   - hub-operator: the hub host's own operator authority over its
//     configuration, metadata and storage.
//   - vendor: the vendor's issuing authority; portal-account the customer's
//     account in the merchant of record's hosted portal.
var prerequisites = []string{
	"author", "execute", "execute-if-send", "author-if-quota-change", "author-if-approve", "hub-operation",
	"hub-certificate",
	"hub:evidence.read", "hub:evidence.write", "hub:execution", "hub:approval",
	"hub:export", "hub:enrollment", "hub:admin", "hub:ownership",
	"hub-operator", "vendor", "portal-account",
}

// frontendTests is where every interaction test lives; a GUI reference names
// a component test file below it, or a journey that drives the production
// window against the real facade.
const frontendTests = "desktop/frontend/src/"

var frontendTestSuffixes = []string{".test.tsx", ".test.ts", ".journey.tsx"}

// Validate reports the first reason the ledger cannot be read as coverage.
// Every row needs an owning ticket; a row without a disposition needs the
// screen and action that answer it; two rows never cover one source. A v2
// row also names its backend operation, and exactly the implemented rows
// name their interaction and parity tests.
func Validate(ledger Ledger) error {
	if ledger.Schema != SchemaV1 && ledger.Schema != SchemaV2 {
		return ErrUnsupportedVersion
	}
	if strings.TrimSpace(ledger.Program) == "" {
		return errors.New("the ledger names the program it covers")
	}
	if len(ledger.Rows) == 0 {
		return errors.New("the ledger holds at least one row")
	}
	v2 := ledger.Schema == SchemaV2
	ids := make(map[string]bool, len(ledger.Rows))
	sources := make(map[string]bool, len(ledger.Rows))
	for _, row := range ledger.Rows {
		if strings.TrimSpace(row.ID) == "" {
			return errors.New("every row has an id")
		}
		if ids[row.ID] {
			return errors.New("two rows share the id " + row.ID)
		}
		ids[row.ID] = true
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
		case KindPortal, KindTooling:
			if !v2 {
				return errors.New("row " + row.ID + " names a surface only " + SchemaV2 + " tracks")
			}
		default:
			return errors.New("row " + row.ID + " names a surface the ledger tracks")
		}
		if strings.TrimSpace(row.Capability) == "" {
			return errors.New("row " + row.ID + " says what the capability is")
		}
		if !ownerPattern.MatchString(row.Owner) {
			return errors.New("row " + row.ID + " names its owning issue as bharm16/readmit#N")
		}
		var err error
		if v2 {
			err = validateV2(row)
		} else {
			err = validateV1(row)
		}
		if err != nil {
			return err
		}
	}
	return successorsImplemented(ledger)
}

// successorsImplemented refuses a superseded row whose successor is not an
// implemented row of its own surface, so the disposition cannot retire an
// address the application does not in fact reach some other way.
func successorsImplemented(ledger Ledger) error {
	for _, row := range ledger.Rows {
		if row.Disposition.Kind != DispositionSuperseded {
			continue
		}
		named := slices.ContainsFunc(ledger.Rows, func(successor Row) bool {
			return successor.Kind == row.Kind && successor.Source == row.Disposition.Successor &&
				successor.Implemented && successor.ID != row.ID
		})
		if !named {
			return errors.New("row " + row.ID + " is superseded, so its successor is the implemented " + row.Kind + " row that answers the capability now")
		}
	}
	return nil
}

func validateV1(row Row) error {
	if row.Disposed() {
		// A disposition is the whole answer for a row that is not
		// customer work; it cannot half-claim a screen as well.
		if row.Screen != "" || row.Action != "" || row.Test != "" || row.Implemented {
			return errors.New("row " + row.ID + " is either disposed or covered, not both")
		}
		return nil
	}
	for _, field := range []struct{ name, value string }{
		{"screen", row.Screen}, {"action", row.Action}, {"test", row.Test},
	} {
		if strings.TrimSpace(field.value) == "" {
			return errors.New("row " + row.ID + " names its " + field.name)
		}
	}
	return nil
}

func validateV2(row Row) error {
	if row.Kind == KindTooling {
		// A Makefile target or a tools script drives readmit from outside;
		// it has no Go operation of its own and is never customer work.
		if row.Backend != (Symbol{}) || row.Disposition.Kind != DispositionDeveloperTooling {
			return errors.New("row " + row.ID + " is repository tooling, so it names no backend and is disposed as developer tooling")
		}
	} else if !symbolPattern.MatchString(row.Backend.Name) || !repositoryPath(row.Backend.Package) {
		return errors.New("row " + row.ID + " names its backend operation as a package directory and a function or Type.Method")
	}
	for _, list := range [][]string{row.Inputs, row.Outputs} {
		if !distinct(list, func(document string) bool { return document == RawHL7 || contractPattern.MatchString(document) }) {
			return errors.New("row " + row.ID + " names each canonical input and output once, as a readmit contract or hl7")
		}
	}
	if !distinct(row.Prerequisites, func(prerequisite string) bool { return slices.Contains(prerequisites, prerequisite) }) {
		return errors.New("row " + row.ID + " names each prerequisite once, from the closed set")
	}
	if (row.Disposition.Kind == DispositionSuperseded) != (row.Disposition.Successor != "") {
		return errors.New("row " + row.ID + " names a successor exactly when it is superseded")
	}
	named := row.GUITest != (FrontendTest{}) || row.ParityTest != (GoTest{})
	if row.Disposed() {
		if !slices.Contains(dispositionKinds, row.Disposition.Kind) || strings.TrimSpace(row.Disposition.Reason) == "" {
			return errors.New("row " + row.ID + " gives its disposition one of the closed kinds and a reason")
		}
		// A disposition is the whole answer for a row that is not
		// customer work; it cannot half-claim a screen or a test as well.
		if row.Screen != "" || row.Action != "" || named || row.Implemented {
			return errors.New("row " + row.ID + " is either disposed or covered, not both")
		}
		return nil
	}
	if strings.TrimSpace(row.Screen) == "" || strings.TrimSpace(row.Action) == "" {
		return errors.New("row " + row.ID + " names its screen and action")
	}
	if !row.Implemented {
		// An open row has no screen to test yet; naming a test for it
		// would claim evidence the row itself says does not exist.
		if named {
			return errors.New("row " + row.ID + " is open, so it names no interaction or parity test")
		}
		return nil
	}
	file := row.GUITest.File
	if !repositoryPath(file) || !strings.HasPrefix(file, frontendTests) ||
		!slices.ContainsFunc(frontendTestSuffixes, func(suffix string) bool { return strings.HasSuffix(file, suffix) }) ||
		strings.TrimSpace(row.GUITest.Name) == "" {
		return errors.New("row " + row.ID + " is implemented, so it names a frontend interaction test file and title")
	}
	if !repositoryPath(row.ParityTest.Package) || !goTestPattern.MatchString(row.ParityTest.Name) {
		return errors.New("row " + row.ID + " is implemented, so it names a Go parity test by package directory and TestName")
	}
	return nil
}

// distinct reports that every entry is valid and none repeats.
func distinct(list []string, valid func(string) bool) bool {
	for i, entry := range list {
		if !valid(entry) || slices.Contains(list[:i], entry) {
			return false
		}
	}
	return true
}

// repositoryPath reports a clean, relative, slash-separated path inside the
// repository: no leading slash, no dot segments, no backslash.
func repositoryPath(path string) bool {
	return path != "" && path != "." && fs.ValidPath(path) && !strings.Contains(path, `\`)
}

// The v1 wire contract, unchanged: its rows name their tests in prose.
type ledgerV1 struct {
	Schema  string  `json:"schema"`
	Program string  `json:"program"`
	Rows    []rowV1 `json:"rows"`
}

type rowV1 struct {
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

func (l ledgerV1) ledger() Ledger {
	ledger := Ledger{Schema: l.Schema, Program: l.Program, Rows: make([]Row, 0, len(l.Rows))}
	for _, row := range l.Rows {
		ledger.Rows = append(ledger.Rows, Row{
			ID: row.ID, Source: row.Source, Kind: row.Kind, Capability: row.Capability, Owner: row.Owner,
			Screen: row.Screen, Action: row.Action, Test: row.Test, Implemented: row.Implemented,
			Disposition: Disposition{Reason: row.Disposition},
		})
	}
	return ledger
}

var documentV1 = strictdoc.Document{
	MaxBytes:    4 << 20,
	Schema:      SchemaV1,
	Required:    []string{"program", "rows"},
	Invalid:     "invalid capability ledger",
	TooLarge:    "capability ledger exceeds its size limit",
	MustDeclare: "a capability ledger declares its contract version",
	Unsupported: ErrUnsupportedVersion,
	Requires:    "a capability ledger declares the program it covers and its rows",
}

// The v2 wire contract. Every nested object is read presence-then-strict:
// a missing or null required member is refused before the strict pass, so a
// zero value never stands in for an omitted one.
type ledgerV2 struct {
	Schema  string  `json:"schema"`
	Program string  `json:"program"`
	Rows    []rowV2 `json:"rows"`
}

type rowV2 struct {
	ID            string         `json:"id"`
	Source        string         `json:"source"`
	Kind          string         `json:"kind"`
	Capability    string         `json:"capability"`
	Owner         string         `json:"owner"`
	Screen        string         `json:"screen,omitzero"`
	Action        string         `json:"action,omitzero"`
	Backend       *symbolV2      `json:"backend,omitzero"`
	Inputs        []string       `json:"inputs"`
	Outputs       []string       `json:"outputs"`
	Prerequisites []string       `json:"prerequisites"`
	GUITest       *guiTestV2     `json:"gui_test,omitzero"`
	ParityTest    *goTestV2      `json:"parity_test,omitzero"`
	Implemented   bool           `json:"implemented"`
	Disposition   *dispositionV2 `json:"disposition,omitzero"`
}

type (
	symbolV2      Symbol
	guiTestV2     FrontendTest
	goTestV2      GoTest
	dispositionV2 Disposition
)

var errInvalidRow = errors.New("invalid capability ledger row")

func (r *rowV2) UnmarshalJSON(data []byte) error {
	if err := present(data, "id", "source", "kind", "capability", "owner", "inputs", "outputs", "prerequisites", "implemented"); err != nil {
		return err
	}
	// An optional member is either absent or a value; an explicit null or
	// empty string is neither, and is refused rather than read as absent.
	if err := notNull(data, "screen", "action", "backend", "gui_test", "parity_test", "disposition"); err != nil {
		return err
	}
	type plain rowV2
	return json.Unmarshal(data, (*plain)(r), json.RejectUnknownMembers(true))
}

func (s *symbolV2) UnmarshalJSON(data []byte) error {
	type plain symbolV2
	return nested(data, (*plain)(s), "package", "name")
}

func (t *guiTestV2) UnmarshalJSON(data []byte) error {
	type plain guiTestV2
	return nested(data, (*plain)(t), "file", "name")
}

func (t *goTestV2) UnmarshalJSON(data []byte) error {
	type plain goTestV2
	return nested(data, (*plain)(t), "package", "name")
}

func (d *dispositionV2) UnmarshalJSON(data []byte) error {
	if err := notNull(data, "successor"); err != nil {
		return err
	}
	type plain dispositionV2
	return nested(data, (*plain)(d), "kind", "reason")
}

// nested reads one nested object presence-then-strict: every named member
// present, neither null nor an empty string, and no member the object does
// not declare. A present object that says nothing is refused rather than
// read as if it were absent.
func nested(data []byte, target any, members ...string) error {
	if err := present(data, members...); err != nil {
		return err
	}
	values, err := objectMembers(data)
	if err != nil {
		return err
	}
	for _, name := range members {
		if bytes.Equal(bytes.TrimSpace(values[name]), []byte(`""`)) {
			return errInvalidRow
		}
	}
	return json.Unmarshal(data, target, json.RejectUnknownMembers(true))
}

// present refuses an object missing one of the named members or carrying an
// explicit null for it.
func present(data []byte, names ...string) error {
	members, err := objectMembers(data)
	if err != nil {
		return err
	}
	for _, name := range names {
		value, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errInvalidRow
		}
	}
	return nil
}

// notNull refuses an explicit null, or an empty string, for any of the named
// optional members: an optional member is either absent or says something.
func notNull(data []byte, names ...string) error {
	members, err := objectMembers(data)
	if err != nil {
		return err
	}
	for _, name := range names {
		value, ok := members[name]
		if trimmed := bytes.TrimSpace(value); ok && (bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`))) {
			return errInvalidRow
		}
	}
	return nil
}

func objectMembers(data []byte) (map[string]jsontext.Value, error) {
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil || members == nil {
		return nil, errInvalidRow
	}
	return members, nil
}

func (l ledgerV2) ledger() Ledger {
	ledger := Ledger{Schema: l.Schema, Program: l.Program, Rows: make([]Row, 0, len(l.Rows))}
	for _, row := range l.Rows {
		converted := Row{
			ID: row.ID, Source: row.Source, Kind: row.Kind, Capability: row.Capability, Owner: row.Owner,
			Screen: row.Screen, Action: row.Action, Implemented: row.Implemented,
			Inputs: row.Inputs, Outputs: row.Outputs, Prerequisites: row.Prerequisites,
		}
		if row.Backend != nil {
			converted.Backend = Symbol(*row.Backend)
		}
		if row.GUITest != nil {
			converted.GUITest = FrontendTest(*row.GUITest)
		}
		if row.ParityTest != nil {
			converted.ParityTest = GoTest(*row.ParityTest)
		}
		if row.Disposition != nil {
			converted.Disposition = Disposition(*row.Disposition)
		}
		ledger.Rows = append(ledger.Rows, converted)
	}
	return ledger
}

var documentV2 = strictdoc.Document{
	MaxBytes:    4 << 20,
	Schema:      SchemaV2,
	Required:    []string{"program", "rows"},
	Invalid:     "invalid capability ledger",
	TooLarge:    "capability ledger exceeds its size limit",
	MustDeclare: "a capability ledger declares its contract version",
	Unsupported: ErrUnsupportedVersion,
	Requires:    "a capability ledger declares the program it covers and its rows",
}
