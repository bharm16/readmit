// Package observesource is the first source-specific collector behind the
// source-neutral contracts in internal/observewindow. It observes two kinds of
// external output: a bounded JSON, CSV, XML or text export on disk, and a
// bounded read of an approved HTTP API.
//
// It decides nothing about what an observation means. A collector's whole job
// here is to report honestly what one attempt saw — an observation, or the one
// way collection failed — and internal/observewindow applies the declared
// completion rule to those reports. Implementing that package's slots rather
// than inventing parallel ones is the point: a second collector for a database
// or a downstream capture fills in the same Sample, EvidenceIdentity and
// Correlation, so one rule keeps one implementation.
//
// The rule this package must not break is that failed collection never becomes
// a passing absence assertion. A collector an operator disabled, a read of
// state older than the window's freshness bound, an export larger than its
// declared bound, a lost connection, a refused or unauthorized HTTP read and a
// response with no single reading each report the failure they were, never zero
// records. Unknown and unsupported are not pass, and a timeout is not a
// negative application result.
//
// Every observation states how old the material it read is. A cached HTTP
// response carrying an age, and an export whose file has not changed since
// before the window opened, are both stale evidence rather than current
// evidence that happens to be quiet.
//
// Nothing here parses an envelope itself. A declared export is divided by
// internal/importer's own readers, under the bounds and refusals a mapping
// recipe is already held to, so an HL7 payload carried in a record keeps its
// bytes and no second parser can disagree with the first.
//
// What it reads is a bounded document rather than a stream, and the bound is
// the point: a source past the operator's declared bound is refused as
// truncated, so there is nothing to read incrementally that would not already
// be an error. importer.Scan divides an HL7 stream under a declared import
// plan, which is a different reading of different bytes; it reads no envelope
// and accepts sources this contract deliberately refuses.
//
// Following [ADR-0006], an HTTP credential is a *reference*: the declared store
// kind, the endpoint it is scoped to, and the absolute path of the program that
// prints the value. readmit never stores, writes or renders a value, and the
// one mechanism that reads one is [secret.Locator.Read].
//
// [ADR-0006]: ../../docs/adr/0006-credentials-are-referenced-never-stored.md
package observesource

import (
	"encoding/json/v2"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Schema is the contract a declared observation source carries. It is a new
// document beside readmit-observation-window/v1 and
// readmit-observation-completion/v1, both of which stay frozen: what makes an
// observation trustworthy is source-neutral, and how one source is reached is
// not. A new member here means a new version string with a reader for every
// older one, never an added member and never an in-place migration.
const Schema = "readmit-observation-source/v1"

const (
	// MaxSourceBytes bounds the document a command reads before decoding it.
	MaxSourceBytes = 64 << 10
	// MaxReadBytes is the ceiling a declared read bound may name. It is the
	// per-source evidence bound a case bundle holds, so an observation can
	// never read more of one source than readmit would retain of it.
	MaxReadBytes = 16 << 20
	// MaxCABytes bounds an explicitly configured certificate authority file.
	MaxCABytes = 1 << 20
	// maxAttempts bounds retries. A read that has not answered after a handful
	// of bounded attempts is unavailable, and retrying further is waiting for a
	// convenient answer rather than observing one.
	maxAttempts = 4
	// maxRetryDelay bounds the wait between attempts.
	maxRetryDelay = 5 * time.Second
	// maxTimeout bounds one read, matching the bound a replay target's
	// timeouts are held to.
	maxTimeout = 5 * time.Minute
	// maxFreshness bounds the age an operator may still call current. A week
	// is generous for a nightly export and refuses a declaration that would
	// accept anything.
	maxFreshness = 168 * time.Hour
	// maxURLBytes bounds a declared endpoint.
	maxURLBytes = 2048
	// maxHeaderBytes bounds the request header name a credential is presented
	// in.
	maxHeaderBytes = 64
	// maxHostBytes bounds a verified server name, at the longest a host name
	// can be.
	maxHostBytes = 253
	// maxPathBytes bounds a declared file or certificate reference.
	maxPathBytes = 4096
)

// The two source kinds this collector supports. A window declaring any other
// kind is reported as unsupported, which is an execution error; it is never
// reported as an observation of nothing.
const (
	// FileExport is a bounded export file on disk.
	FileExport = "file-export"
	// HTTPAPI is a bounded read of an approved HTTP API over TLS.
	HTTPAPI = "http-api"
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. It is reported, never migrated in place.
var ErrUnsupportedVersion = errors.New("unsupported observation source document version")

// Freshness is how old the state an observation read may be and still describe
// the window. There is no default: an operator states what current means for
// this source, because readmit cannot know it.
type Freshness struct {
	MaxAge string `json:"max_age"`
}

// Extraction is the declared schema of the source's output: the envelope its
// records are carried in, and where the key that identifies one record sits.
// It reuses the envelope half of a mapping recipe exactly, so an export readmit
// can import is an export readmit can observe, read by the same reader.
type Extraction struct {
	Envelope importer.Envelope `json:"envelope"`
	Encoding importer.Encoding `json:"encoding"`
	// Exactly one dialect is declared, and it is the one Envelope names.
	CSV  *importer.CSVDialect      `json:"csv,omitzero"`
	Text *importer.TextDialect     `json:"text,omitzero"`
	JSON *importer.DocumentDialect `json:"json,omitzero"`
	XML  *importer.DocumentDialect `json:"xml,omitzero"`
	// RecordKey locates the value that identifies one observed record. It is
	// the whole of what this collector reads out of a record: a key it can
	// count, compare between samples, and correlate what the run produced
	// against. No field value is retained, so nothing patient-identifying
	// crosses into a completion record.
	RecordKey importer.Locator `json:"record_key"`
}

// Retry is the bounded, declared retry of one read. Only a read is retried, and
// only a condition that produced no answer at all: a retry that turned an
// uncertain read into a confident one would be the conflation these contracts
// exist to prevent, so a stale, ambiguous, truncated or refused answer is
// reported as it stands rather than attempted again.
type Retry struct {
	// Attempts is how many further attempts one read may make, so zero means
	// the read is attempted exactly once.
	Attempts int    `json:"attempts"`
	Delay    string `json:"delay"`
}

// Credential is the authentication reference an HTTP observation presents. It
// carries no value and never will: readmit registers where a credential lives
// and how to read it back, and a value exists only inside the one command that
// resolved it.
//
// Command and Arguments are a locator, never a credential. Arguments are
// visible to every process on the machine; readmit never puts a value there and
// never prints back the arguments it was given.
type Credential struct {
	Store secret.Store `json:"store"`
	// Address is the host and numeric port this credential is scoped to. It is
	// compared byte for byte with the endpoint actually declared, so nothing
	// resolves a name to widen the reach of a stored credential.
	Address string `json:"address"`
	// Header is the request header the resolved value is presented in.
	Header    string   `json:"header"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}

// Locator is where this credential's value is read back from. readmit holds no
// credential; it holds the locator, exactly as internal/secret does, and there
// is one mechanism for obtaining a value rather than two.
func (c Credential) Locator() secret.Locator {
	return secret.Locator{Command: c.Command, Arguments: c.Arguments}
}

// File is a bounded export on disk.
type File struct {
	// Path is the export, resolved against the directory the source document
	// itself lives in rather than the caller's working directory.
	Path string `json:"path"`
	// MaxBytes bounds what one read may take. An export larger than this is a
	// truncated observation, never a completed one: what was read would be a
	// prefix of the source rather than the source.
	MaxBytes int `json:"max_bytes"`
}

// HTTP is a bounded read of one approved API endpoint over TLS.
type HTTP struct {
	// URL is one absolute https endpoint. There is no plaintext mode and no
	// redirect following: a read that arrived somewhere else answered about
	// something else.
	URL string `json:"url"`
	// Classification is the class recorded for the environment this endpoint
	// belongs to, in the spellings readmit-target records. It is a claim, never
	// an authorization, and the destination decision is made by
	// internal/sendpolicy against destinations an operator approved explicitly.
	Classification string `json:"classification"`
	// CAFile is an explicitly configured certificate authority replacing the
	// system roots, resolved against the source document's own directory.
	// Verification is always on and there is no way to turn it off.
	CAFile string `json:"ca_file"`
	// ServerName is the name the certificate is verified against when it is
	// not the endpoint's own host.
	ServerName string `json:"server_name"`
	Timeout    string `json:"timeout"`
	MaxBytes   int    `json:"max_bytes"`
	Retry      Retry  `json:"retry"`
	// Credential is null where the endpoint needs none.
	Credential *Credential `json:"credential"`
}

// Source is one declared observation source: which system and scope it names,
// whether its collector is enabled, how old its state may be, how its output is
// read, and exactly one of the two ways it is reached.
//
// It is explicitly selected, never discovered: readmit has no default source,
// no implicit file and no environment variable that supplies one.
type Source struct {
	Schema string `json:"schema"`
	// Observes is the source this document says how to reach. It must be the
	// source the window declares, so a collector never quietly observes
	// something other than the source that was asked about.
	Observes observewindow.Source `json:"source"`
	// Enabled is stated rather than assumed. A disabled collector reports that
	// no state could be obtained, which is an execution error; it never reports
	// that the source held nothing.
	Enabled    bool       `json:"enabled"`
	Freshness  Freshness  `json:"freshness"`
	Extraction Extraction `json:"extraction"`
	// Exactly one of File and HTTP is declared, and it is the one the source
	// kind names. The other is explicitly null.
	File *File `json:"file"`
	HTTP *HTTP `json:"http"`
}

// UnmarshalJSON requires every member explicitly, so an omitted enablement or
// freshness bound cannot decode into a permissive zero value, and then
// re-decodes rejecting unknown members so a document authored against a later
// contract is never read as though this one had always allowed it. The two
// transports are required as explicit nulls where there is none, which a typed
// decode alone would accept as absent members.
func (s *Source) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema     *string               `json:"schema"`
		Observes   *observewindow.Source `json:"source"`
		Enabled    *bool                 `json:"enabled"`
		Freshness  *Freshness            `json:"freshness"`
		Extraction *Extraction           `json:"extraction"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("an observation source declares its contract version")
	}
	if *required.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if required.Observes == nil || required.Enabled == nil || required.Freshness == nil || required.Extraction == nil {
		return errors.New("an observation source requires a source, an explicit enablement, a freshness bound, and an extraction declaration")
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("invalid observation source JSON")
	}
	_, file := members["file"]
	_, endpoint := members["http"]
	if !file || !endpoint {
		return errors.New("an observation source declares both a file and an http member, explicitly null for the one it does not use")
	}
	type plainSource Source
	var value plainSource
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid observation source JSON")
	}
	*s = Source(value)
	return nil
}

// UnmarshalJSON on each nested type applies its own presence check rather than
// inheriting the enclosing document's, so a member omitted inside one of them
// is an error rather than a zero value the reader treats as a declaration.

func (f *Freshness) UnmarshalJSON(data []byte) error {
	var required struct {
		MaxAge *string `json:"max_age"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.MaxAge == nil {
		return errors.New("a freshness bound states the greatest age an observation may still describe the window")
	}
	type plainFreshness Freshness
	var value plainFreshness
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid freshness bound")
	}
	*f = Freshness(value)
	return nil
}

func (e *Extraction) UnmarshalJSON(data []byte) error {
	var required struct {
		Envelope  *string           `json:"envelope"`
		Encoding  *string           `json:"encoding"`
		RecordKey *importer.Locator `json:"record_key"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Envelope == nil || required.Encoding == nil || required.RecordKey == nil {
		return errors.New("an extraction declaration requires an envelope, an encoding, and the locator of the record key")
	}
	type plainExtraction Extraction
	var value plainExtraction
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid extraction declaration")
	}
	*e = Extraction(value)
	return nil
}

func (f *File) UnmarshalJSON(data []byte) error {
	var required struct {
		Path     *string `json:"path"`
		MaxBytes *int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Path == nil || required.MaxBytes == nil {
		return errors.New("a file export declares its path and the bound on what one read may take")
	}
	type plainFile File
	var value plainFile
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid file export declaration")
	}
	*f = File(value)
	return nil
}

func (h *HTTP) UnmarshalJSON(data []byte) error {
	var required struct {
		URL            *string `json:"url"`
		Classification *string `json:"classification"`
		CAFile         *string `json:"ca_file"`
		ServerName     *string `json:"server_name"`
		Timeout        *string `json:"timeout"`
		MaxBytes       *int    `json:"max_bytes"`
		Retry          *Retry  `json:"retry"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.URL == nil || required.Classification == nil || required.CAFile == nil || required.ServerName == nil || required.Timeout == nil || required.MaxBytes == nil || required.Retry == nil {
		return errors.New("an http observation requires a url, a recorded classification, an explicit certificate authority and server name, a timeout, a read bound, and a retry declaration")
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		return errors.New("invalid http observation declaration")
	}
	if _, declared := members["credential"]; !declared {
		return errors.New("an http observation declares its credential, explicitly null where the endpoint needs none")
	}
	type plainHTTP HTTP
	var value plainHTTP
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid http observation declaration")
	}
	*h = HTTP(value)
	return nil
}

func (r *Retry) UnmarshalJSON(data []byte) error {
	var required struct {
		Attempts *int    `json:"attempts"`
		Delay    *string `json:"delay"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Attempts == nil || required.Delay == nil {
		return errors.New("a retry declaration states how many further attempts a read may make and how long it waits between them")
	}
	type plainRetry Retry
	var value plainRetry
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid retry declaration")
	}
	*r = Retry(value)
	return nil
}

func (c *Credential) UnmarshalJSON(data []byte) error {
	var required struct {
		Store     *string   `json:"store"`
		Address   *string   `json:"address"`
		Header    *string   `json:"header"`
		Command   *string   `json:"command"`
		Arguments *[]string `json:"arguments"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Store == nil || required.Address == nil || required.Header == nil || required.Command == nil || required.Arguments == nil {
		return errors.New("a credential reference requires a declared store, the endpoint it is scoped to, the header it is presented in, and the absolute path and arguments of the program that reads it")
	}
	type plainCredential Credential
	var value plainCredential
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid credential reference")
	}
	*c = Credential(value)
	return nil
}

// Validate reports the first reason a declared source cannot be used. A source
// that could not be collected from is refused when it is read rather than
// discovered after a window has already been opened against it.
func (s Source) Validate() error {
	if s.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := s.Observes.Validate(); err != nil {
		return err
	}
	if _, err := boundedDuration(s.Freshness.MaxAge, maxFreshness); err != nil {
		return errors.New("a freshness bound is a positive duration of at most one week")
	}
	if err := s.Extraction.validate(); err != nil {
		return err
	}
	declared := 0
	for _, present := range []bool{s.File != nil, s.HTTP != nil} {
		if present {
			declared++
		}
	}
	if declared != 1 {
		return errors.New("an observation source declares exactly one of a file export and an http observation")
	}
	switch s.Observes.Kind {
	case FileExport:
		if s.File == nil {
			return errors.New("a file-export source declares a file export")
		}
		return s.File.validate()
	case HTTPAPI:
		if s.HTTP == nil {
			return errors.New("an http-api source declares an http observation")
		}
		return s.HTTP.validate()
	}
	return errors.New("an observation source kind is file-export or http-api")
}

// Shape is the envelope half of the extraction declaration, expressed as the
// shape internal/importer's own readers take.
func (e Extraction) Shape() importer.EnvelopeShape {
	return importer.EnvelopeShape{Envelope: e.Envelope, Encoding: e.Encoding, CSV: e.CSV, Text: e.Text, JSON: e.JSON, XML: e.XML}
}

func (e Extraction) validate() error {
	return e.Shape().Validate([]importer.Locator{e.RecordKey})
}

func (f File) validate() error {
	if f.Path == "" || len(f.Path) > maxPathBytes {
		return errors.New("a file export names one path")
	}
	if f.MaxBytes < 1 || f.MaxBytes > MaxReadBytes {
		return errors.New("a read bound is between 1 byte and 16 MiB")
	}
	return nil
}

func (h HTTP) validate() error {
	if _, err := h.Endpoint(); err != nil {
		return err
	}
	if h.Classification == "" {
		return errors.New("an http observation records the class of the environment its endpoint belongs to")
	}
	if len(h.CAFile) > maxPathBytes {
		return errors.New("a certificate authority reference names one path")
	}
	if len(h.ServerName) > maxHostBytes {
		return errors.New("a verified server name is one bounded host name")
	}
	if _, err := boundedDuration(h.Timeout, maxTimeout); err != nil {
		return errors.New("an http timeout is a positive duration of at most five minutes")
	}
	if h.MaxBytes < 1 || h.MaxBytes > MaxReadBytes {
		return errors.New("a read bound is between 1 byte and 16 MiB")
	}
	if err := h.Retry.validate(); err != nil {
		return err
	}
	if h.Credential == nil {
		return nil
	}
	return h.Credential.validate()
}

func (r Retry) validate() error {
	if r.Attempts < 0 || r.Attempts > maxAttempts {
		return errors.New("a read makes between 0 and 4 further attempts")
	}
	delay, err := time.ParseDuration(r.Delay)
	if err != nil || delay < 0 || delay > maxRetryDelay {
		return errors.New("a retry delay is a duration of at most five seconds")
	}
	return nil
}

func (c Credential) validate() error {
	if c.Store != secret.OSKeychain && c.Store != secret.CustomerManaged {
		return errors.New("a credential store is declared as os-keychain or customer-managed")
	}
	if err := endpointAddress(c.Address); err != nil {
		return errors.New("a credential reference is scoped to one explicit host and numeric port")
	}
	if err := headerName(c.Header); err != nil {
		return err
	}
	if err := c.Locator().Validate(); err != nil {
		return errors.New("credential " + err.Error())
	}
	return nil
}

// Endpoint is the host and numeric port a declared URL names, with the port
// https implies where the URL states none. It is what a destination decision
// and a credential's scope are both compared against, so neither can be
// checking a different endpoint than the one that is read.
func (h HTTP) Endpoint() (string, error) {
	if h.URL == "" || len(h.URL) > maxURLBytes {
		return "", errors.New("an http observation names one absolute https endpoint")
	}
	parsed, err := url.Parse(h.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Fragment != "" {
		return "", errors.New("an http observation names one absolute https endpoint without a fragment")
	}
	if parsed.User != nil {
		return "", errors.New("an http endpoint never carries credentials in its URL")
	}
	if parsed.Hostname() == "" {
		return "", errors.New("an http observation names one absolute https endpoint")
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	address := net.JoinHostPort(parsed.Hostname(), port)
	if err := endpointAddress(address); err != nil {
		return "", errors.New("an http endpoint names one explicit host and numeric port")
	}
	return address, nil
}

// BindCredential returns the locator this source's credential may be read
// through, bound to the endpoint the source itself declares. A reference scoped
// to another endpoint is refused rather than presented here, and no value is
// read: binding settles what a credential may be used for, never what it is.
func (h HTTP) BindCredential() (secret.Locator, string, error) {
	if h.Credential == nil {
		return secret.Locator{}, "", nil
	}
	endpoint, err := h.Endpoint()
	if err != nil {
		return secret.Locator{}, "", err
	}
	if h.Credential.Address != endpoint {
		return secret.Locator{}, "", errors.New("the credential reference is scoped to a different endpoint address")
	}
	if err := h.Credential.validate(); err != nil {
		return secret.Locator{}, "", err
	}
	return h.Credential.Locator(), h.Credential.Header, nil
}

// PolicyRequest is the question this endpoint asks internal/sendpolicy before
// anything is opened. An observation reads rather than sends, and it goes
// through the same one rule anyway: a class nobody recorded, a name resolving
// to several addresses and an address outside every approved destination each
// refuse the read. Labelling an endpoint a test endpoint is not proof it is
// safe to reach.
func (h HTTP) PolicyRequest() (sendpolicy.Request, error) {
	endpoint, err := h.Endpoint()
	if err != nil {
		return sendpolicy.Request{}, err
	}
	return sendpolicy.Request{Address: endpoint, Classification: h.Classification, Explicit: true}, nil
}

// headerName holds a request header to the token characters HTTP allows, so a
// declared header can never inject a second header or a request line.
func headerName(value string) error {
	if value == "" || len(value) > maxHeaderBytes {
		return errors.New("a credential is presented in one named request header")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return errors.New("a request header name uses only letters, digits, '-' and '_'")
		}
	}
	return nil
}

func boundedDuration(value string, limit time.Duration) (time.Duration, error) {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 || parsed > limit {
		return 0, errors.New("invalid duration")
	}
	return parsed, nil
}

// endpointAddress holds an address to one explicit host and numeric port. It is
// the same shape a credential reference is scoped to elsewhere in readmit, so
// the comparison that binds a credential compares like with like.
func endpointAddress(value string) error {
	refused := errors.New("must be an explicit host and numeric port")
	if value == "" || len(value) > maxPathBytes {
		return refused
	}
	host, port, err := net.SplitHostPort(value)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
		return refused
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return errors.New("must contain printable ASCII without spaces")
		}
	}
	return nil
}
