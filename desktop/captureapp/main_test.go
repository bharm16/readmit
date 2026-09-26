//go:build darwin

package main

import (
	"math"
	"testing"
)

func TestSnapshotAdmission(t *testing.T) {
	for _, name := range []string{"../escape", "", "a/b", "a\\b"} {
		if validSnapshot(name, 0, 0, 1100, 760) {
			t.Fatalf("accepted unsafe name %q", name)
		}
	}
	for _, rect := range [][4]float64{{0, 0, 1101, 760}, {-1, 0, 20, 20}, {0, 0, 0, 20}, {0, 0, 20, math.NaN()}, {0, 0, math.Inf(1), 20}} {
		if validSnapshot("capture", rect[0], rect[1], rect[2], rect[3]) {
			t.Fatalf("accepted invalid rectangle %v", rect)
		}
	}
	if !validSnapshot("0001-component", 0, 0, 1100, 760) {
		t.Fatal("refused viewport")
	}
	if !validSnapshot("0002-crop", 100, 100, 200, 200) {
		t.Fatal("refused component crop")
	}
}
