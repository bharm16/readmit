package sharing

import (
	"context"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/report"
)

type Request struct{ Source, Kind, Private, Policy string }

// Candidate holds a snapshot, but Publish always regenerates it from current
// sources and policy. The human approves every output byte through Identity.
type Candidate struct {
	request Request
	raw     []byte
	policy  Policy
}

func (c *Candidate) Identity() string { return Digest(c.raw) }
func (c *Candidate) Bytes() []byte    { return append([]byte(nil), c.raw...) }
func readPolicy(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, ErrRefused
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrRefused
	}
	defer f.Close()
	info, e = f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, ErrRefused
	}
	raw, e := io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(raw) > 4096 {
		return nil, ErrRefused
	}
	return raw, nil
}
func Prepare(ctx context.Context, r Request) (*Candidate, error) {
	if ctx.Err() != nil {
		return nil, ErrRefused
	}
	policyBytes, e := readPolicy(r.Policy)
	if e != nil {
		return nil, ErrRefused
	}
	policy, e := DecodePolicy(policyBytes)
	if e != nil || !policy.Support {
		return nil, ErrRefused
	}
	s := Summary{Schema: Schema, SourceKind: r.Kind, PolicyIdentity: Digest(policyBytes), Scope: Scope, ExternalEquivalence: "declined"}
	switch r.Kind {
	case "retained-packet", "portable-review":
		path := r.Source
		if r.Kind == "portable-review" {
			v, e := report.OpenReview(ctx, path)
			if e != nil {
				return nil, ErrRefused
			}
			s.SourceIdentity = v.Identity
			path = filepath.Join(path, "packet")
		}
		p, e := report.OpenRetained(ctx, path)
		if e != nil {
			return nil, ErrRefused
		}
		if s.SourceIdentity == "" {
			s.SourceIdentity = p.Identity
		}
		s.InputCommitment = p.Manifest.Current.CaseIdentity
		s.SpecIdentity = p.Manifest.Current.SpecIdentity
		s.Outcome = p.Manifest.Current.Status
		if p.Manifest.Current.JournalIncomplete || p.Manifest.Current.DeliveryUncertain || !slices.Contains([]string{"not_recorded", "passed", "assertion_failed", "execution_error"}, p.Manifest.Current.RunState) {
			s.Outcome = "execution_error"
		}
	case "derived-review":
		review, e := redact.ValidateDisclosure(ctx, r.Source, r.Private)
		if e != nil {
			return nil, ErrRefused
		}
		s.SourceIdentity = review.Identity
		s.InputCommitment = review.InputCommitment
		s.SpecIdentity = review.DerivedSpecSHA256
		s.Outcome = "reviewed-extract-only"
	default:
		return nil, ErrRefused
	}
	raw, e := json.Marshal(s, json.Deterministic(true))
	if e != nil {
		return nil, ErrRefused
	}
	if _, e := Decode(raw); e != nil || len(raw) > policy.MaxBytes {
		return nil, ErrRefused
	}
	if ctx.Err() != nil {
		return nil, ErrRefused
	}
	return &Candidate{r, raw, policy}, nil
}

// Publish only writes a newly reserved private directory; no URL, socket,
// archive extraction or automatic transport is available. The complete marker
// is last; any interrupted directory is refused by Open and retry uses a new one.
func (c *Candidate) Publish(ctx context.Context, approval, output string) error {
	fresh, e := Prepare(ctx, c.request)
	if e != nil || approval != c.Identity() || fresh.Identity() != c.Identity() || !fresh.policy.Allows("local-file", len(fresh.raw)) {
		return ErrRefused
	}
	protected := []os.FileInfo{}
	for _, p := range []string{c.request.Source, c.request.Private} {
		if p != "" {
			info, e := os.Stat(p)
			if e != nil {
				return ErrRefused
			}
			protected = append(protected, info)
		}
	}
	path, e := artifactpath.Destination(output, protected...)
	if e != nil {
		return ErrRefused
	}
	if ctx.Err() != nil {
		return ErrRefused
	}
	if e = os.Mkdir(path, 0700); e != nil {
		return ErrRefused
	}
	write := func(name string, raw []byte) error {
		if ctx.Err() != nil {
			return ErrRefused
		}
		f, e := os.OpenFile(filepath.Join(path, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return ErrRefused
		}
		_, e = f.Write(raw)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil || ce != nil {
			return ErrRefused
		}
		return nil
	}
	if e = write("support.json", fresh.raw); e != nil {
		return e
	}
	// An event contains only fixed vocabulary and a commitment. Never a path,
	// username, arbitrary error, source label or source content.
	event := []byte(`{"schema":"readmit-sharing-event/v1","action":"local-support-publish","approval_kind":"local-byte-review","summary":"` + fresh.Identity() + `"}`)
	if e = write("event.json", event); e != nil {
		return e
	}
	final, e := Prepare(ctx, c.request)
	if e != nil || final.Identity() != fresh.Identity() {
		return ErrRefused
	}
	return write("identity.sha256", []byte(fresh.Identity()+"\n"))
}
func Open(path string) (Summary, error) {
	var zero Summary
	path, e := artifactpath.Directory(path)
	if e != nil {
		return zero, ErrRefused
	}
	entries, e := os.ReadDir(path)
	if e != nil || len(entries) != 3 {
		return zero, ErrRefused
	}
	data := map[string][]byte{}
	for _, entry := range entries {
		if entry.Name() != "support.json" && entry.Name() != "event.json" && entry.Name() != "identity.sha256" {
			return zero, ErrRefused
		}
		raw, e := readPolicy(filepath.Join(path, entry.Name()))
		if e != nil {
			return zero, ErrRefused
		}
		data[entry.Name()] = raw
	}
	s, e := Decode(data["support.json"])
	if e != nil || string(data["identity.sha256"]) != Digest(data["support.json"])+"\n" {
		return zero, ErrRefused
	}
	expected := `{"schema":"readmit-sharing-event/v1","action":"local-support-publish","approval_kind":"local-byte-review","summary":"` + Digest(data["support.json"]) + `"}`
	if string(data["event.json"]) != expected {
		return zero, ErrRefused
	}
	return s, nil
}
