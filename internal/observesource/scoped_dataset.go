package observesource

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ScopedDatasetAcquisitionSchema = "readmit-dataset-acquisition/v2"

type scopedDatasetReader struct {
	plan      *networkaction.HTTPPlan
	authority networkaction.Authority
	output    string
	resolve   sendpolicy.Resolver
	maxAge    time.Duration
}

func (r *scopedDatasetReader) close() {}
func (r *scopedDatasetReader) read(ctx context.Context) attempt {
	taken := attempt{at: time.Now(), record: Evidence{Kind: HTTPAPI, Attempts: 1}}
	response, result, err := r.plan.Execute(ctx, r.authority, r.output, r.resolve)
	if err != nil {
		return failure(taken, observewindow.SampleFailed, "scoped source action did not complete")
	}
	taken.at = time.Now()
	taken.record.HTTPStatus = response.Status
	if response.Status != 200 || !result.ResponseRetained {
		return failure(taken, observewindow.SampleFailed, "source did not provide a retainable successful response")
	}
	raw := response.Body.Expose()
	taken.record.Bytes = len(raw)
	taken.evidence = map[string][]byte{"body": raw}
	if response.Header("Link") != "" {
		return failure(taken, observewindow.SampleTruncated, "linked HTTP result is not a complete dataset snapshot")
	}
	header := http.Header{}
	header.Set("Date", response.Header("Date"))
	header.Set("Age", response.Header("Age"))
	age, ok := responseAge(&http.Response{Header: header}, taken.at)
	if !ok || !dateState(&taken, taken.at.Add(-age), r.maxAge) {
		return failure(taken, observewindow.SampleStale, "source freshness could not be established")
	}
	taken.status = observewindow.Observed
	return taken
}
func scopedDatasetSource(request DatasetRequest) bool {
	if request.Network == nil || request.NetworkAuthority == nil || request.Source.HTTP == nil || request.Policy != nil {
		return false
	}
	s := request.Network.Declaration()
	source := request.Source.HTTP
	b := request.Network.Binding()
	if b.Source != request.Binding.Source || b.Operation != sendpolicy.ObservationRead || s.Method != "GET" || s.URL != source.URL || s.Classification != source.Classification || s.MaxBytes != request.Projection.Limits.MaxBytes || s.TimeoutMS != request.Projection.Limits.TimeoutMS || s.PrivateKey != nil {
		return false
	}
	u, err := url.Parse(source.URL)
	if err != nil {
		return false
	}
	name := source.ServerName
	if name == "" {
		name = u.Hostname()
	}
	if s.ServerName != name {
		return false
	}
	ca, err := authorities(source.CAFile)
	if err != nil || !bytes.Equal(ca, s.Authorities) {
		return false
	}
	if (source.Credential == nil) != (s.Credential == nil) {
		return false
	}
	if c := source.Credential; c != nil {
		if c.Command != s.Credential.Locator.Command || !slices.Equal(c.Arguments, s.Credential.Locator.Arguments) || c.Header != s.Credential.Header || s.Credential.Prefix != "" {
			return false
		}
	}
	return true
}
func datasetReaderFor(ctx context.Context, source Source, retention *snapshot, request DatasetRequest, output string) (reader, error) {
	if request.DatabaseNetwork != nil {
		return databaseScopedReader(ctx, request.DatabaseNetwork, request.NetworkAuthority, filepath.Join(output, "network"), request.Resolve)
	}
	if request.Network == nil {
		return newReader(ctx, source, retention, Options{Policy: request.Policy, Resolve: request.Resolve})
	}
	maxAge, _ := time.ParseDuration(source.Freshness.MaxAge)
	return &scopedDatasetReader{plan: request.Network, authority: request.NetworkAuthority, output: filepath.Join(output, "network"), resolve: request.Resolve, maxAge: maxAge}, nil
}
