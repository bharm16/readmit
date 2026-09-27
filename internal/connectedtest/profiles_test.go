package connectedtest_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/profileeval"
	"os"
	"testing"
)

func TestProfileChecksUseExactRetainedPins(t *testing.T) {
	raw, files := example(t)
	var d connectedtest.Test
	_ = json.Unmarshal(raw, &d)
	for _, entry := range []struct{ id, file, schema string }{{"profile", "local-profile.json", "readmit-local-profile/v1"}, {"pack", "profile-pack.json", "readmit-profile-pack/v1"}} {
		b, err := os.ReadFile("../../testdata/fixtures/" + entry.file)
		if err != nil {
			t.Fatal(err)
		}
		files[entry.file] = b
		d.Profiles = append(d.Profiles, connectedtest.Reference{Project: "lab", ID: entry.id, File: entry.file, Schema: entry.schema, SHA256: connectedtest.Digest(b)})
	}
	raw, _ = json.Marshal(d)
	p, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := connectedtest.EvaluateProfiles(context.Background(), p, "profile", "pack", profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	files["local-profile.json"] = []byte("changed externally")
	after, err := connectedtest.EvaluateProfiles(context.Background(), p, "profile", "pack", profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Profile.SHA256 != after.Profile.SHA256 || before.Operator != after.Operator {
		t.Fatal("saved profile pins moved")
	}
	if _, err := connectedtest.EvaluateProfiles(context.Background(), p, "missing", "pack", profileeval.Options{}); err == nil {
		t.Fatal("unresolved profile pin accepted")
	}
}
