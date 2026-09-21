package desktop_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/index"
)

// Opt-in measurements, not timing assertions: a busy machine must not turn a
// correctness test into a false failure or a proposed envelope into a pass.
func TestPerformanceEnvelope(t *testing.T) {
	if os.Getenv("READMIT_PERFORMANCE") != "1" {
		t.Skip("set READMIT_PERFORMANCE=1 for local measurements")
	}
	root := t.TempDir()
	wire := strings.Repeat(framed(gridBooking), 10000)
	opened := writeCase(t, root, "scale", wire)
	name := writeIndex(t, root, "scale.index.json", opened, nil)
	document, err := index.Build(context.Background(), opened, index.Policy{Fields: []string{patientField}, Retention: index.RetainValues}, indexedAt())
	if err != nil {
		t.Fatal(err)
	}
	app := desktop.New(&chooser{}, "", "", "", "")
	t.Logf("fixture messages=10000 bytes=%d sha256=%x; repeated synthetic gridBooking; warm OS cache, no race", len(wire), sha256.Sum256([]byte(wire)))
	measure := func(label string, operation func()) {
		operation() // warm-up excluded; report every measured sample.
		samples := make([]float64, 20)
		for i := range samples {
			started := time.Now()
			operation()
			samples[i] = float64(time.Since(started).Nanoseconds()) / 1e6
		}
		ordered := append([]float64(nil), samples...)
		sort.Float64s(ordered)
		t.Logf("%s samples_ms=%v nearest_rank_p95_ms=%.3f", label, samples, ordered[18])
	}
	measure("warm indexed exact search", func() {
		got, e := document.Search(indexedAt(), index.Query{Match: index.Equals, Term: []byte("MRN-1^^^READMIT^MR")})
		if e != nil || len(got.Hits) != 10000 {
			t.Fatalf("search hits=%d error=%v", len(got.Hits), e)
		}
	})
	measure("facade grid navigation (not UI paint)", func() {
		got := app.OpenGrid(root, "scale", name, 9800, 200)
		if got.State != desktop.Completed || got.Grid == nil || len(got.Grid.Rows) != 200 || got.Grid.Total != 10000 {
			t.Fatal(fmt.Sprintf("grid state=%s", got.State))
		}
	})
	measure("facade workspace search", func() {
		if got := app.Search(root, "scale"); got.State != desktop.Completed {
			t.Fatalf("search state=%s", got.State)
		}
	})
}
