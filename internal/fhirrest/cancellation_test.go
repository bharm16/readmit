package fhirrest_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/fhirrest"
)

func TestFHIRRESTCanceledSecondPageFinalizesRetainedProjection(t *testing.T) {
	for _, cancelSecond := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel-second-%t", cancelSecond), func(t *testing.T) {
			f := newServer(t)
			f.pages = []string{searchPage("BASE/Patient?identifier=lab%7Cshared&page=2", match("p1", "1")), searchPage("", match("p2", "1"))}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls, pageTwo atomic.Int32
			handler := f.s.Config.Handler
			f.s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Query().Get("page") == "2" {
					pageTwo.Add(1)
					if cancelSecond {
						cancel()
						<-r.Context().Done()
						return
					}
				}
				handler.ServeHTTP(w, r)
			})
			spec := f.spec("GET", "/Patient?identifier=lab%7Cshared", nil)
			spec.Projection = projection()
			raw, e := json.Marshal(spec)
			if e != nil {
				t.Fatal(e)
			}
			plan, e := fhirrest.Prepare(raw, f.policy)
			if e != nil {
				t.Fatal(e)
			}
			output := filepath.Join(t.TempDir(), "result")
			result, e := plan.Execute(ctx, admission(plan), nil, output, nil)
			if e != nil {
				t.Fatal(e)
			}
			wantState, wantCoverage, wantProjection, wantPages := "succeeded", "complete", "complete", 2
			if cancelSecond {
				wantState, wantCoverage, wantProjection, wantPages = "time-limit", "incomplete", "collection-unknown", 1
			}
			if result.State != wantState || result.ExecutionState != wantState || result.Search.Coverage != wantCoverage || result.Search.Pages != wantPages || len(result.Projections) != wantPages {
				t.Fatalf("retained page lost during finalization: state=%s execution=%s coverage=%s pages=%d projections=%d", result.State, result.ExecutionState, result.Search.Coverage, result.Search.Pages, len(result.Projections))
			}
			if calls.Load() != 3 || pageTwo.Load() != 1 || len(result.Attempts) != 3 {
				t.Fatalf("unexpected retry after cancellation: calls=%d page2=%d attempts=%d", calls.Load(), pageTwo.Load(), len(result.Attempts))
			}
			for _, projection := range result.Projections {
				if projection.Status != wantProjection || len(projection.Rows) != 1 {
					t.Fatal("retained projection status/rows", projection.Status, len(projection.Rows))
				}
			}
			f.s.Close()
			moved := filepath.Join(t.TempDir(), "moved")
			if e := os.Rename(output, moved); e != nil {
				t.Fatal(e)
			}
			retained, _ := json.Marshal(result, json.Deterministic(true))
			for range 2 {
				opened, e := fhirrest.Open(context.Background(), moved)
				if e != nil {
					t.Fatal("offline reopen after cancellation", e)
				}
				encoded, e := json.Marshal(opened, json.Deterministic(true))
				if e != nil || !bytes.Equal(encoded, retained) {
					t.Fatal("offline reconstruction differed", e)
				}
			}
			if calls.Load() != 3 {
				t.Fatal("offline finalization contacted target")
			}
		})
	}
}
