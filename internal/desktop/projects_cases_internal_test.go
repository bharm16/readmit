package desktop

import (
	"strconv"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
)

// Past its bound the click memory forgets its oldest click, never all of
// them: a repeat of a recent click is still recognized.
func TestTheClickMemoryForgetsItsOldestClickFirst(t *testing.T) {
	var intents metadataIntents
	for i := range catalog.MaxIntents + 1 {
		intents.record("click-"+strconv.Itoa(i), "digest")
	}
	if _, repeated := intents.admit("click-0", "digest"); repeated {
		t.Fatal("the oldest click is still held past the bound")
	}
	if admitted, repeated := intents.admit("click-"+strconv.Itoa(catalog.MaxIntents), "digest"); !admitted || !repeated {
		t.Fatal("the newest click was forgotten")
	}
	if admitted, _ := intents.admit("click-1", "other"); admitted {
		t.Fatal("a recent click took different content")
	}
}
