package fhirrest

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
)

var fileName = regexp.MustCompile(`^(intent-[0-9]{4}\.json|attempt-[0-9]{4}\.json|response-[0-9]{4}\.bin)$`)

func Open(ctx context.Context, directory string) (Result, error) {
	evidence, e := OpenEvidence(ctx, directory)
	if e != nil {
		return Result{}, e
	}
	return evidence.Result(), nil
}

func verify(ctx context.Context, files map[string][]byte) (Result, error) {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ResultSchema, files) {
		return Result{}, refused
	}
	p, e := Prepare(files["plan.json"], files["policy.json"])
	if e != nil {
		return Result{}, e
	}
	var result Result
	if json.Unmarshal(files["result.json"], &result, json.RejectUnknownMembers(true)) != nil || result.Schema != ResultSchema || result.Plan != p.identity || result.Meaning != "HTTP-evidence-not-workflow-success" || len(result.Attempts) == 0 || len(result.Attempts) > 257 {
		return Result{}, refused
	}
	if !slices.Contains([]string{"succeeded", "not-modified", "unknown", "protocol-invalid", "unsupported", "unauthorized", "forbidden", "not-found", "conflict", "throttled", "unavailable", "pending", "rejected", "rejected-transaction", "partial-failure", "refused", "delivery-uncertain", "response-withheld", "capability-refused", "capability-changed", "byte-limit", "row-limit", "page-limit", "next-link-refused", "paging-cycle", "time-limit"}, result.State) {
		return Result{}, refused
	}
	if result.ExecutionState == "unknown" || !slices.Contains([]string{"succeeded", "not-modified", "protocol-invalid", "unsupported", "unauthorized", "forbidden", "not-found", "conflict", "throttled", "unavailable", "pending", "rejected", "rejected-transaction", "partial-failure", "refused", "delivery-uncertain", "response-withheld", "capability-refused", "capability-changed", "byte-limit", "row-limit", "page-limit", "next-link-refused", "paging-cycle", "time-limit"}, result.ExecutionState) {
		return Result{}, refused
	}
	bodies := map[int][]byte{}
	expected := p.capabilityRequest()
	lastURL := ""
	retry := 0
	pages := 0
	seen := map[string]bool{}
	totalBytes := 0
	totalEntries := 0
	known := map[string]bool{"plan.json": true, "policy.json": true, "result.json": true, "identity.sha256": true}
	var actor networkaction.Actor
	for i, record := range result.Attempts {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		name := fmt.Sprintf("attempt-%04d.json", i)
		intentName := fmt.Sprintf("intent-%04d.json", i)
		known[name] = true
		known[intentName] = true
		var a Attempt
		var intent Intent
		if json.Unmarshal(files[name], &a, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files[intentName], &intent, json.RejectUnknownMembers(true)) != nil || !rawEqual(a, record) || a.Index != i || a.Started.IsZero() || a.Completed.Before(a.Started) || !rawEqual(a.Request, expected) {
			return Result{}, refused
		}
		phase := "interaction"
		if i == 0 {
			phase = "capabilities"
		}
		action, e := networkaction.PrepareRuntimeHTTPV2(encode(a.Request), p.policy)
		if e != nil || a.Phase != phase || !rawEqual(intent, Intent{Index: i, Binding: action.Binding(), Request: a.Request, Phase: phase}) {
			return Result{}, refused
		}
		if a.Receipt.Schema == "" && !rawEqual(a.Receipt, networkaction.RuntimeReceipt{}) {
			return Result{}, refused
		}
		if a.Receipt.Schema != "" {
			if a.Receipt.Schema != networkaction.RuntimeReceiptSchema || a.Receipt.Binding != action.Binding() || !networkaction.RecordedActor(a.Receipt.Actor) {
				return Result{}, refused
			}
			if actor != (networkaction.Actor{}) && actor != a.Receipt.Actor {
				return Result{}, refused
			}
			actor = a.Receipt.Actor
			if !slices.Contains([]string{"refused", "uncertain", "responded"}, a.Receipt.State) || !a.Receipt.Actor.Expires.After(a.Started) {
				return Result{}, refused
			}
			if a.Receipt.State == "responded" || a.Receipt.State == "uncertain" {
				if !a.Receipt.Decision.Allowed || a.Receipt.Decision.Schema != "readmit-network-operation/v1" || a.Receipt.Decision.Reason != "approved" || a.Receipt.Decision.Operation != a.Request.HTTP.Operation {
					return Result{}, refused
				}
			}
			if a.Receipt.State == "responded" && (a.Receipt.HTTPStatus < 100 || a.Receipt.HTTPStatus > 599) || a.Receipt.State != "responded" && a.Receipt.HTTPStatus != 0 {
				return Result{}, refused
			}
		}
		outcome := Outcome{State: "refused", Payload: "unavailable", Entries: []Outcome{}}
		if a.Receipt.State == "uncertain" {
			outcome.State = "delivery-uncertain"
		}
		if a.Response == "" && a.Receipt.State == "responded" {
			outcome.State = "response-withheld"
			outcome.Payload = "private-response"
		}
		for key, value := range a.Headers {
			if !slices.Contains([]string{"Content-Type", "Date", "Age", "Link", "Location", "ETag", "Last-Modified", "Retry-After", "Preference-Applied"}, key) || len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
				return Result{}, refused
			}
		}
		body := []byte{}
		if a.Response != "" {
			if a.Response != fmt.Sprintf("response-%04d.bin", i) || a.Receipt.State != "responded" {
				return Result{}, refused
			}
			var ok bool
			body, ok = files[a.Response]
			if !ok || dataset.Digest(body) != a.SHA256 {
				return Result{}, refused
			}
			known[a.Response] = true
			request, e := parsedRequest(p.spec.Base, a.Request)
			if e != nil {
				return Result{}, refused
			}
			outcome = classify(p.spec.Base, request, a.Receipt.HTTPStatus, a.Headers, body, p.spec.Prior)
		} else if a.SHA256 != "" || len(a.Headers) > 0 || a.Receipt.HTTPStatus != 0 && a.Receipt.State != "responded" {
			return Result{}, refused
		}
		if !rawEqual(outcome, a.Outcome) {
			return Result{}, refused
		}
		bodies[i] = body
		totalBytes += len(body)
		if i == 0 {
			if len(result.Attempts) > 1 {
				if outcome.State != "succeeded" {
					return Result{}, refused
				}
				current, ce := capable(p.spec.Base, body, p.request)
				baseline, _ := capable(p.spec.Base, p.spec.Capability, p.request)
				if ce != nil || requiredCapabilityIdentity(current, p.request) != requiredCapabilityIdentity(baseline, p.request) {
					return Result{}, refused
				}
			}
			expected = p.spec.HTTP
			continue
		}
		if lastURL == a.Request.HTTP.URL {
			retry++
		} else {
			retry = 1
			if seen[a.Request.HTTP.URL] {
				return Result{}, refused
			}
			seen[a.Request.HTTP.URL] = true
		}
		lastURL = a.Request.HTTP.URL
		if retry > p.spec.Retry.MaxAttempts {
			return Result{}, refused
		}
		if outcome.State != "succeeded" && outcome.State != "not-modified" {
			if i+1 < len(result.Attempts) && (!p.request.SafeRead || !retryable(a)) {
				return Result{}, refused
			}
			expected = a.Request
			continue
		}
		if p.request.Kind != "search" {
			if i+1 < len(result.Attempts) {
				return Result{}, refused
			}
			continue
		}
		pages++
		entries, next, e := pageLinks(p.spec.Base, body)
		if e != nil {
			if errors.Is(e, errDuplicateNextLink) {
				return Result{}, refused
			}
			return Result{}, e
		}
		totalEntries += entries
		if i+1 < len(result.Attempts) {
			if next == "" || pages >= p.spec.Budget.Pages || totalEntries > p.spec.Budget.Rows || totalBytes > p.spec.Budget.Bytes {
				return Result{}, refused
			}
			expected, e = pageRequest(p.spec.Base, body, p.request.Resource, p.spec.HTTP, p.spec.PagePrivateKey, next)
			if e != nil {
				return Result{}, refused
			}
		}
		if i == len(result.Attempts)-1 && (result.ExecutionState == "succeeded") && next != "" {
			return Result{}, refused
		}
	}
	if len(files) != len(known) {
		return Result{}, refused
	}
	for name := range files {
		if !known[name] {
			return Result{}, refused
		}
	}
	if result.ExecutionState == "succeeded" || result.ExecutionState == "not-modified" {
		last := result.Attempts[len(result.Attempts)-1]
		if len(result.Attempts) < 2 || last.Outcome.State != "succeeded" && last.Outcome.State != "not-modified" || totalBytes > p.spec.Budget.Bytes || totalEntries > p.spec.Budget.Rows || pages > p.spec.Budget.Pages {
			return Result{}, refused
		}
	}
	last := result.Attempts[len(result.Attempts)-1]
	if result.ExecutionState == "succeeded" && last.Outcome.State != "succeeded" || result.ExecutionState == "not-modified" && last.Outcome.State != "not-modified" {
		return Result{}, refused
	}
	if slices.Contains([]string{"protocol-invalid", "unsupported", "unauthorized", "forbidden", "not-found", "conflict", "throttled", "unavailable", "pending", "rejected", "rejected-transaction", "partial-failure", "refused", "delivery-uncertain", "response-withheld"}, result.ExecutionState) && result.ExecutionState != last.Outcome.State {
		return Result{}, refused
	}
	if result.ExecutionState == "byte-limit" && totalBytes <= p.spec.Budget.Bytes || result.ExecutionState == "row-limit" && totalEntries <= p.spec.Budget.Rows {
		return Result{}, refused
	}
	rebuilt := result
	derive(ctx, p, &rebuilt, bodies)
	if !rawEqual(rebuilt, result) {
		return Result{}, refused
	}
	return result, nil
}

// Inspect never resumes a request. An intent without a sealed result remains
// incomplete, including a process lost after the target committed a write.
func Inspect(directory string) (string, error) {
	layout := family.Layout
	layout.RequiredFiles = []string{"plan.json", "policy.json"}
	files, e := artifactdir.Read(directory, layout)
	if e != nil {
		return "", refused
	}
	if _, ok := files["identity.sha256"]; ok {
		r, e := verify(context.Background(), files)
		return r.State, e
	}
	if _, e := Prepare(files["plan.json"], files["policy.json"]); e != nil {
		return "", e
	}
	for name := range files {
		if strings.HasPrefix(name, "intent-") {
			return "delivery-uncertain", nil
		}
	}
	return "not-started", nil
}
