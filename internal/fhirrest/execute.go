package fhirrest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type Attempt struct {
	Index     int                             `json:"index"`
	Phase     string                          `json:"phase"`
	Request   networkaction.RuntimeHTTPSpecV2 `json:"request"`
	Receipt   networkaction.RuntimeReceipt    `json:"receipt"`
	Started   time.Time                       `json:"started"`
	Completed time.Time                       `json:"completed"`
	Headers   map[string]string               `json:"headers"`
	Response  string                          `json:"response,omitzero"`
	SHA256    string                          `json:"sha256,omitzero"`
	Outcome   Outcome                         `json:"outcome"`
}
type Intent struct {
	Index   int                             `json:"index"`
	Binding networkaction.Binding           `json:"binding"`
	Request networkaction.RuntimeHTTPSpecV2 `json:"request"`
	Phase   string                          `json:"phase"`
}
type Result struct {
	ExecutionState string           `json:"execution_state"`
	Schema         string           `json:"schema"`
	Plan           string           `json:"plan"`
	State          string           `json:"state"`
	Meaning        string           `json:"meaning"`
	Attempts       []Attempt        `json:"attempts"`
	Search         Search           `json:"search"`
	Projections    []fhirr4.Dataset `json:"projections"`
}
type Search struct {
	MatchOccurrences int       `json:"match_occurrences"`
	Coverage         string    `json:"coverage"`
	Consistency      string    `json:"consistency"`
	Pages            int       `json:"pages"`
	Matches          int       `json:"matches"`
	Includes         int       `json:"includes"`
	Outcomes         int       `json:"outcomes"`
	Overlaps         []Overlap `json:"overlaps"`
}
type Overlap struct {
	Identity     string `json:"identity"`
	FirstAttempt int    `json:"first_attempt"`
	Attempt      int    `json:"attempt"`
	FirstVersion string `json:"first_version"`
	Version      string `json:"version"`
	Changed      bool   `json:"changed"`
}

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR HTTP result", RequiredFiles: []string{"plan.json", "policy.json", "result.json", "identity.sha256"}, AllowFile: func(n string) bool {
	if n == "plan.json" || n == "policy.json" || n == "result.json" || n == "identity.sha256" {
		return true
	}
	return fileName.MatchString(n)
}, MaxFiles: 800, MaxFileBytes: 32 << 20, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

type derivedAuthority struct {
	root           networkaction.Authority
	binding, child networkaction.Binding
	actor          networkaction.Actor
}

func (a derivedAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if b != a.child {
		return networkaction.Actor{}, refused
	}
	actor, e := a.root.Check(ctx, a.binding)
	if e != nil || actor != a.actor || !networkaction.CurrentActor(actor) {
		return networkaction.Actor{}, refused
	}
	return actor, nil
}
func (p *Plan) capabilityRequest() networkaction.RuntimeHTTPSpecV2 {
	if p.spec.CapabilityHTTP != nil {
		return *p.spec.CapabilityHTTP
	}
	s := p.spec.HTTP
	s.HTTP.Method = "GET"
	s.HTTP.URL = p.spec.Base + "/metadata"
	s.HTTP.Operation = sendpolicy.FHIRMetadata
	s.HTTP.Body = nil
	s.HTTP.ContentType = ""
	s.Headers = networkaction.HTTPHeadersV2{}
	return s
}
func (p *Plan) Execute(ctx context.Context, authority networkaction.Authority, provider networkaction.RuntimeProvider, output string, resolve sendpolicy.Resolver) (Result, error) {
	result := Result{ExecutionState: "refused", Schema: ResultSchema, Plan: p.identity, State: "refused", Meaning: "HTTP-evidence-not-workflow-success", Attempts: []Attempt{}, Search: Search{Coverage: "not-search", Consistency: "not-declared", Overlaps: []Overlap{}}, Projections: []fhirr4.Dataset{}}
	if authority == nil {
		return result, refused
	}
	actor, e := authority.Check(ctx, p.binding)
	if e != nil || !networkaction.CurrentActor(actor) {
		return result, refused
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.spec.Budget.TimeoutMS)*time.Millisecond)
	defer cancel()
	w, e := artifactdir.Create(output, family, artifactdir.Durable)
	if e != nil {
		return result, refused
	}
	defer w.Close()
	if w.WriteFile("plan.json", p.raw) != nil || w.WriteFile("policy.json", p.policy) != nil {
		return result, refused
	}
	bodies := map[int][]byte{}
	bytesRead := 0
	execute := func(spec networkaction.RuntimeHTTPSpecV2, phase string) (Attempt, []byte, error) {
		i := len(result.Attempts)
		action, e := networkaction.PrepareRuntimeHTTPV2(encode(spec), p.policy)
		if e != nil {
			return Attempt{}, nil, refused
		}
		intent := Intent{Index: i, Binding: action.Binding(), Request: spec, Phase: phase}
		if w.WriteFile(fmt.Sprintf("intent-%04d.json", i), encode(intent)) != nil || w.Sync() != nil {
			return Attempt{}, nil, refused
		}
		a := Attempt{Index: i, Phase: phase, Request: spec, Started: time.Now().UTC(), Headers: map[string]string{}}
		response, receipt, requestErr := action.Execute(ctx, derivedAuthority{authority, p.binding, action.Binding(), actor}, resolve, provider)
		a.Completed = time.Now().UTC()
		a.Receipt = receipt
		body := []byte{}
		if requestErr == nil {
			body = response.Body.Expose()
			a.Headers = response.Metadata()
			a.Response = fmt.Sprintf("response-%04d.bin", i)
			a.SHA256 = dataset.Digest(body)
			if w.WriteFile(a.Response, body) != nil {
				return a, nil, refused
			}
			request, e := parsedRequest(p.spec.Base, spec)
			if e != nil {
				return a, nil, refused
			}
			a.Outcome = classify(p.spec.Base, request, response.Status, a.Headers, body, p.spec.Prior)
		} else {
			a.Outcome = Outcome{State: "refused", Payload: "unavailable", Entries: []Outcome{}}
			if receipt.State == "uncertain" {
				a.Outcome.State = "delivery-uncertain"
			}
			if receipt.State == "responded" {
				a.Outcome.State = "response-withheld"
				a.Outcome.Payload = "private-response"
			}
		}
		if w.WriteFile(fmt.Sprintf("attempt-%04d.json", i), encode(a)) != nil || w.Sync() != nil {
			return a, nil, refused
		}
		result.Attempts = append(result.Attempts, a)
		bodies[i] = body
		bytesRead += len(body)
		return a, body, nil
	}
	finish := func(state string) (Result, error) {
		if ctx.Err() != nil && (state == "succeeded" || state == "not-modified") {
			state = "time-limit"
		}
		result.ExecutionState = state
		// Acquisition cancellation stops network work, not reconstruction of bytes
		// already retained. This read-only pass has page/byte/node/value bounds
		// and performs no network or credential operations; offline Open must
		// derive the identical result under a fresh caller context.
		derive(context.WithoutCancel(ctx), p, &result, bodies)
		if w.WriteFile("result.json", encode(result)) != nil {
			return result, refused
		}
		if _, e := w.Seal(nil); e != nil {
			return result, refused
		}
		return result, nil
	}
	preflight, body, e := execute(p.capabilityRequest(), "capabilities")
	if e != nil {
		return result, e
	}
	if preflight.Outcome.State != "succeeded" {
		return finish("capability-refused")
	}
	current, ce := capable(p.spec.Base, body, p.request)
	baseline, _ := capable(p.spec.Base, p.spec.Capability, p.request)
	if ce != nil || requiredCapabilityIdentity(current, p.request) != requiredCapabilityIdentity(baseline, p.request) {
		return finish("capability-changed")
	}
	if bytesRead > p.spec.Budget.Bytes {
		return finish("byte-limit")
	}
	spec := p.spec.HTTP
	seen := map[string]bool{}
	page := 0
	totalEntries := 0
	for {
		if ctx.Err() != nil {
			return finish("time-limit")
		}
		if seen[spec.HTTP.URL] {
			return finish("paging-cycle")
		}
		seen[spec.HTTP.URL] = true
		var a Attempt
		for retry := 0; retry < p.spec.Retry.MaxAttempts; retry++ {
			if ctx.Err() != nil {
				return finish("time-limit")
			}
			a, body, e = execute(spec, "interaction")
			if e != nil {
				return result, e
			}
			if bytesRead > p.spec.Budget.Bytes {
				return finish("byte-limit")
			}
			if p.request.SafeRead && ctx.Err() != nil {
				return finish("time-limit")
			}
			if !p.request.SafeRead || retry+1 >= p.spec.Retry.MaxAttempts || !retryable(a) {
				break
			}
			if a.Receipt.HTTPStatus == 401 {
				renew, ok := provider.(interface{ ForgetToken() })
				if !ok {
					break
				}
				renew.ForgetToken()
			}
			delay, ok := retryDelay(a.Headers, p.spec.Retry.MaxDelayMS)
			if !ok {
				break
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return finish("time-limit")
			case <-timer.C:
			}
		}
		if a.Outcome.State != "succeeded" && a.Outcome.State != "not-modified" {
			return finish(a.Outcome.State)
		}
		if p.request.Kind != "search" {
			return finish(a.Outcome.State)
		}
		page++
		entries, next, e := pageLinks(p.spec.Base, body)
		if e != nil {
			return finish("protocol-invalid")
		}
		totalEntries += entries
		if totalEntries > p.spec.Budget.Rows {
			return finish("row-limit")
		}
		if next == "" {
			return finish("succeeded")
		}
		if page >= p.spec.Budget.Pages {
			return finish("page-limit")
		}
		spec, e = pageRequest(p.spec.Base, body, p.request.Resource, p.spec.HTTP, p.spec.PagePrivateKey, next)
		if e != nil {
			return finish("next-link-refused")
		}
	}
}
func retryable(a Attempt) bool {
	return a.Outcome.State == "delivery-uncertain" || a.Receipt.HTTPStatus == 401 || a.Receipt.HTTPStatus == 429 || a.Receipt.HTTPStatus == 503
}
func retryDelay(headers map[string]string, maxMS int64) (time.Duration, bool) {
	v := headers["Retry-After"]
	if v == "" {
		return 0, true
	}
	seconds, e := strconv.ParseInt(v, 10, 64)
	var d time.Duration
	if e == nil && seconds >= 0 && seconds <= 10 {
		d = time.Duration(seconds) * time.Second
	} else {
		at, e := http.ParseTime(v)
		if e != nil {
			return 0, false
		}
		d = time.Until(at)
		if d < 0 {
			d = 0
		}
	}
	if d > time.Duration(maxMS)*time.Millisecond {
		return 0, false
	}
	return d, true
}
func rawEqual(a, b any) bool { return bytes.Equal(encode(a), encode(b)) }
