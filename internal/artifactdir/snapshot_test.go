package artifactdir_test

import (
	"bytes"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

func TestCapturedSnapshotIsBoundedConfinedAndDetached(t *testing.T) {
	layout := artifactdir.Layout{Noun: "fixture", RequiredFiles: []string{"payload.bin"}, AllowFile: func(name string) bool { return name == "payload.bin" || name == "other.bin" }, MaxFiles: 2, MaxFileBytes: 3, MaxBytes: 4}
	source := map[string][]byte{"payload.bin": []byte{1, 2, 3}}
	captured, err := artifactdir.Snapshot(source, layout)
	if err != nil {
		t.Fatal(err)
	}
	source["payload.bin"][0] = 9
	if !bytes.Equal(captured["payload.bin"], []byte{1, 2, 3}) {
		t.Fatal("caller bytes rewrote captured evidence")
	}
	captured["payload.bin"][1] = 8
	if source["payload.bin"][1] != 2 {
		t.Fatal("captured bytes still alias the caller")
	}
	for name, invalid := range map[string]map[string][]byte{
		"missing":    {},
		"traversal":  {"payload.bin": {1}, "../other.bin": {2}},
		"unknown":    {"payload.bin": {1}, "unexpected.bin": {2}},
		"file-size":  {"payload.bin": {1, 2, 3, 4}},
		"total-size": {"payload.bin": {1, 2, 3}, "other.bin": {4, 5}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := artifactdir.Snapshot(invalid, layout); err == nil {
				t.Fatal("unsupported captured layout accepted")
			}
		})
	}
}
