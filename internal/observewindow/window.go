// Package observewindow defines what a trustworthy observation window is,
// independently of the source that is observed. It holds five source-neutral
// contracts: the identity of what was observed, the watermark a window opens
// at, the completion rule that decides when observation may stop, the statuses
// that name missing and ambiguous evidence, and the explicit handling of state
// that already existed before the window opened.
//
// It collects nothing. A downstream HL7 capture, a file export, an approved
// HTTP API and a read-only database query each observe their own source and
// report what they saw; this package decides whether what they reported is
// evidence. Keeping the decision here means one rule has one implementation and
// a collector cannot widen it by construction.
//
// The rule it exists to enforce is that failed collection never becomes a
// passing absence assertion. A collector that was disabled, one that returned
// stale data, one whose connection was lost, one whose capture was truncated
// and one whose source answered ambiguously all produce zero trustworthy
// records, and zero trustworthy records is not the same observation as an
// observed empty state. Every one of those is an execution error here, and only
// a window that completed can support a claim that something is absent.
//
// Statuses compose with the durable run vocabulary rather than replacing it:
// a window that did not complete is an execution error, a cancelled window is
// cancellation and a window that ran out of time is a timeout, never a negative
// application result.
package observewindow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"regexp"
	"time"
)

// WindowSchema is the contract a declared observation window carries. It is a
// new document beside readmit-observation/v1, which stays frozen: the fixture
// receiver's ledger snapshot is one source's evidence, and no member of it
// changes to describe a window over some other source.
const WindowSchema = "readmit-observation-window/v1"

const (
	// MaxWindowBytes bounds the document a command reads before decoding it.
	MaxWindowBytes = 64 << 10
	// maxDuration matches the bound a replay target's timeouts are held to, so
	// a window cannot declare a wait no operator would sit through.
	maxDuration = 5 * time.Minute
	// maxSamples bounds one window's sampling. An unbounded poll is refused
	// rather than truncated, because a truncated poll is not a completed one.
	maxSamples = 1024
	// maxRecords bounds what one window may declare in scope.
	maxRecords = 1 << 20
	// maxPosition bounds an opaque watermark position.
	maxPosition = 256
)

// Declared watermark kinds. A watermark says where in the source's own ordering
// the window opens; only the source's collector knows how to compare two
// positions, so the position itself stays opaque here.
const (
	// DeclaredPosition names a position in the source's ordering that the
	// operator recorded before the window opened.
	DeclaredPosition = "declared-position"
	// CollectionStart makes the window's own opening instant the watermark.
	CollectionStart = "collection-start"
	// NoWatermark is an explicit declaration that the source offers no
	// ordering. No sample can then be classified as predating the window, so
	// nothing separates earlier evidence from evidence this run produced
	// except a recorded baseline.
	NoWatermark = "none"
)

// Declared handling of state that existed before the window opened. State that
// was already there is never evidence that this run produced it, so which of
// these three an operator declared is recorded and carried into every verdict.
const (
	// DeclaredEmpty is an operator's claim that nothing was in scope before
	// the window opened. It is recorded as the claim it is; readmit does not
	// verify that a source was reset.
	DeclaredEmpty = "declared-empty"
	// RecordedBaseline requires an observation of the source taken before the
	// window opened. Only a baseline that was itself observed counts.
	RecordedBaseline = "recorded-baseline"
	// UnknownPreExisting is an explicit declaration that what was already
	// there is not known. A window may still complete, and it can still show
	// that something is absent, but it can never attribute a record to the run.
	UnknownPreExisting = "unknown"
)

// Source names what was observed. Every member is an operator-chosen label, not
// a value read out of a message: a source identity is the name of a system in
// scope, never patient data.
type Source struct {
	// Kind is the collector family this window is written for. It is a label,
	// not a dispatch: this package implements no collector and treats every
	// kind the same way. A collector that does not support a declared kind
	// reports an unsupported sample rather than observing nothing.
	Kind string `json:"kind"`
	// Identity names the one system or dataset in view.
	Identity string `json:"identity"`
	// Scope names the subset of that source the window covers. An absence
	// claim is only ever a claim about this scope.
	Scope string `json:"scope"`
}

// Watermark is the window's start state in the source's own ordering.
type Watermark struct {
	Kind string `json:"kind"`
	// Position is opaque: only the source's collector knows how to compare it
	// with an observation's position. It is required by declared-position and
	// refused by the other two kinds, so an ignored position cannot look like
	// a respected one.
	Position string `json:"position"`
}

// PreExisting declares how state from before the window is handled.
type PreExisting struct {
	Declaration string `json:"declaration"`
	// BaselineIdentity is the digest of the baseline observation an operator
	// recorded, required by recorded-baseline and refused otherwise. It
	// identifies a baseline; it does not authenticate one.
	BaselineIdentity string `json:"baseline_identity"`
}

// Rule is the completion rule: when observation of an eventually consistent
// source may stop. It is deliberately not "stop as soon as the expected value
// appears". A window stops when the observed state has held still for the
// declared quiet period across the declared number of samples, or when the
// deadline passes without that having happened, which is an error.
type Rule struct {
	// Deadline bounds the whole window, measured from when it opened.
	Deadline string `json:"deadline"`
	// QuietPeriod is how long the observed state must hold still.
	QuietPeriod string `json:"quiet_period"`
	// StableSamples is how many consecutive identical observations that quiet
	// period must span. It is at least two: one reading cannot tell a settled
	// source from one caught mid-write.
	StableSamples int `json:"stable_samples"`
	// MaxRecords bounds the scope one window may observe. A source holding
	// more is a truncated window, never a completed one.
	MaxRecords int `json:"max_records"`
	// MaxSamples bounds how many observations a collector may take.
	MaxSamples int `json:"max_samples"`
}

// Window is one declared observation window. It is explicitly selected, never
// discovered: readmit has no default window and no environment variable that
// supplies one.
type Window struct {
	Schema      string      `json:"schema"`
	Source      Source      `json:"source"`
	Watermark   Watermark   `json:"watermark"`
	PreExisting PreExisting `json:"pre_existing_state"`
	Completion  Rule        `json:"completion"`
}

var (
	labelPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// UnmarshalJSON requires every member explicitly, so an omitted watermark or
// pre-existing-state declaration cannot decode into a permissive zero value,
// and then re-decodes rejecting unknown members so a window authored against a
// later contract is never read as though this one had always allowed it.
func (w *Window) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema      *string      `json:"schema"`
		Source      *Source      `json:"source"`
		Watermark   *Watermark   `json:"watermark"`
		PreExisting *PreExisting `json:"pre_existing_state"`
		Completion  *Rule        `json:"completion"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.Source == nil || required.Watermark == nil || required.PreExisting == nil || required.Completion == nil {
		return errors.New("an observation window requires a schema, source, watermark, pre-existing-state declaration, and completion rule")
	}
	type plainWindow Window
	var value plainWindow
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation window JSON")
	}
	*w = Window(value)
	return nil
}

// UnmarshalJSON on each nested type applies its own presence check and its own
// unknown-member refusal rather than inheriting the enclosing document's.
func (s *Source) UnmarshalJSON(data []byte) error {
	var required struct {
		Kind     *string `json:"kind"`
		Identity *string `json:"identity"`
		Scope    *string `json:"scope"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Kind == nil || required.Identity == nil || required.Scope == nil {
		return errors.New("an observation source requires a kind, identity, and scope")
	}
	type plainSource Source
	var value plainSource
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation source")
	}
	*s = Source(value)
	return nil
}

func (m *Watermark) UnmarshalJSON(data []byte) error {
	var required struct {
		Kind     *string `json:"kind"`
		Position *string `json:"position"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Kind == nil || required.Position == nil {
		return errors.New("a watermark requires a kind and an explicit position, which is empty for a kind that has none")
	}
	type plainWatermark Watermark
	var value plainWatermark
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid watermark")
	}
	*m = Watermark(value)
	return nil
}

func (p *PreExisting) UnmarshalJSON(data []byte) error {
	var required struct {
		Declaration      *string `json:"declaration"`
		BaselineIdentity *string `json:"baseline_identity"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Declaration == nil || required.BaselineIdentity == nil {
		return errors.New("a pre-existing-state declaration requires a declaration and an explicit baseline identity, which is empty unless a baseline was recorded")
	}
	type plainPreExisting PreExisting
	var value plainPreExisting
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid pre-existing-state declaration")
	}
	*p = PreExisting(value)
	return nil
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	var required struct {
		Deadline      *string `json:"deadline"`
		QuietPeriod   *string `json:"quiet_period"`
		StableSamples *int    `json:"stable_samples"`
		MaxRecords    *int    `json:"max_records"`
		MaxSamples    *int    `json:"max_samples"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Deadline == nil || required.QuietPeriod == nil || required.StableSamples == nil || required.MaxRecords == nil || required.MaxSamples == nil {
		return errors.New("a completion rule requires a deadline, quiet period, stable sample count, record limit, and sample limit")
	}
	type plainRule Rule
	var value plainRule
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid completion rule")
	}
	*r = Rule(value)
	return nil
}

// Validate holds a window to its bounds. A window that cannot complete, such as
// one whose quiet period outlasts its deadline, is refused when it is read
// rather than discovered as a failure after a run has already been executed.
func (w Window) Validate() error {
	if w.Schema != WindowSchema {
		return errors.New("an observation window must declare " + WindowSchema)
	}
	if err := w.Source.Validate(); err != nil {
		return err
	}
	if err := w.Watermark.validate(); err != nil {
		return err
	}
	if err := w.PreExisting.validate(); err != nil {
		return err
	}
	return w.Completion.validate()
}

// Validate is shared by the declared window, the retained completion and the
// source-specific collectors, so the source a window may declare, the source a
// record may name and the source a collector says it reaches are one rule.
func (s Source) Validate() error {
	if !labelPattern.MatchString(s.Kind) || !labelPattern.MatchString(s.Identity) || !labelPattern.MatchString(s.Scope) {
		return errors.New("an observation source kind, identity, and scope are short printable labels")
	}
	return nil
}

func (m Watermark) validate() error {
	switch m.Kind {
	case DeclaredPosition:
		if m.Position == "" || len(m.Position) > maxPosition || !printable(m.Position) {
			return errors.New("a declared-position watermark requires one printable position of at most 256 bytes")
		}
	case CollectionStart, NoWatermark:
		if m.Position != "" {
			return errors.New("only a declared-position watermark carries a position")
		}
	default:
		return errors.New("a watermark kind is declared-position, collection-start, or none")
	}
	return nil
}

// validDeclaration is the one place that knows the three declarations, so a
// window and a retained record can never recognize different sets of them.
func validDeclaration(declaration string) bool {
	return declaration == DeclaredEmpty || declaration == RecordedBaseline || declaration == UnknownPreExisting
}

func (p PreExisting) validate() error {
	if !validDeclaration(p.Declaration) {
		return errors.New("a pre-existing-state declaration is declared-empty, recorded-baseline, or unknown")
	}
	if p.Declaration == RecordedBaseline {
		if !digestPattern.MatchString(p.BaselineIdentity) {
			return errors.New("a recorded baseline requires its lowercase hexadecimal SHA-256 identity")
		}
		return nil
	}
	if p.BaselineIdentity != "" {
		return errors.New("only a recorded baseline carries a baseline identity")
	}
	return nil
}

func (r Rule) validate() error {
	deadline, err := duration(r.Deadline)
	if err != nil {
		return errors.New("a completion deadline is a positive duration of at most five minutes")
	}
	quiet, err := duration(r.QuietPeriod)
	if err != nil {
		return errors.New("a quiet period is a positive duration of at most five minutes")
	}
	if quiet > deadline {
		return errors.New("a window whose quiet period outlasts its deadline can never complete")
	}
	// Two observations are the fewest that can show a source holding still.
	// One reading cannot tell a settled source from one caught mid-write, and
	// a rule that accepts one reading is a rule that stops at the first
	// convenient answer.
	if r.StableSamples < 2 || r.StableSamples > maxSamples {
		return errors.New("a completion rule requires between 2 and 1024 stable samples")
	}
	if r.MaxSamples < r.StableSamples || r.MaxSamples > maxSamples {
		return errors.New("a sample limit is at least the stable sample count and at most 1024")
	}
	if r.MaxRecords < 1 || r.MaxRecords > maxRecords {
		return errors.New("a record limit is between 1 and 1048576")
	}
	return nil
}

// Deadline and QuietPeriod are parsed again by callers that already validated
// the rule, so the parse cannot disagree with the one the reader performed.
func (r Rule) deadline() time.Duration    { d, _ := time.ParseDuration(r.Deadline); return d }
func (r Rule) quietPeriod() time.Duration { d, _ := time.ParseDuration(r.QuietPeriod); return d }

func duration(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 || d > maxDuration {
		return 0, errors.New("invalid duration")
	}
	return d, nil
}

func printable(value string) bool {
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

// Identity names the canonical form of this declared window. It correlates a
// completion with the window it was evaluated against; it does not authenticate
// a file, and two files that declare the same window share it.
func (w Window) Identity() string {
	data, err := EncodeWindow(w)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// EncodeWindow writes one validated window deterministically.
func EncodeWindow(w Window) ([]byte, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(w, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode observation window")
	}
	return data, nil
}

// DecodeWindow reads one declared window exactly as written.
func DecodeWindow(data []byte) (Window, error) {
	var window Window
	if len(data) > MaxWindowBytes {
		return window, errors.New("observation window exceeds size limit")
	}
	if err := json.Unmarshal(data, &window); err != nil {
		// Diagnostics must not echo the selected document's contents.
		return Window{}, errors.New("invalid observation window JSON")
	}
	if err := window.Validate(); err != nil {
		return Window{}, err
	}
	return window, nil
}
