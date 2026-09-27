package profileeval_test

import (
	"github.com/bharm16/readmit/internal/profileeval"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in: generated upstream material remains outside the repository until its
// exact redistribution review is approved. The normal suite uses owned fixtures.
func TestPinnedExtractionReadback(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the offline extractor output")
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		t.Run(version, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := profileeval.DecodePack(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Messages) == 0 {
				t.Fatal("empty normalized metadata")
			}
			if p.Metadata.Bundleable() == nil {
				t.Fatal("extractor approved rights")
			}
		})
	}
}
