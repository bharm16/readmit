package observation_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/observation"
)

func initial() observation.Snapshot {
	return observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: "0123456789abcdef0123456789abcdef", Mode: observation.Fixed, Consistent: true, Processed: []observation.Occurrence{}, Records: []observation.Record{}}
}

func TestAtomicSnapshotsNeverExposeTornJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	snapshot := initial()
	if err := observation.Create(path, snapshot); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	stop := make(chan struct{})
	failed := make(chan error, 1)
	wait.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := observation.Read(path); err != nil {
				select {
				case failed <- err:
				default:
				}
				return
			}
		}
	})
	for i := 0; i < 60; i++ {
		snapshot.Consistent = i%2 == 0
		if err := observation.Write(path, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wait.Wait()
	select {
	case err := <-failed:
		t.Fatal(err)
	default:
	}
	before, _ := os.ReadFile(path)
	if err := observation.Create(path, initial()); err == nil {
		t.Fatal("replaced existing observation")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed create changed snapshot")
	}
}

func TestObservationDecoderRejectsInvalidHandoffs(t *testing.T) {
	data, err := observation.Encode(initial())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"consistent":true,`), nil, 1),
		bytes.Replace(data, []byte(`"records":[]`), []byte(`"records":null`), 1),
		bytes.Replace(data, []byte(`"schema":"readmit-observation/v1"`), []byte(`"schema":"readmit-observation/v9"`), 1),
		bytes.Replace(data, []byte(`"mode":"fixed"`), []byte(`"mode":"fixed","unknown":true`), 1),
	} {
		if _, err := observation.Decode(bad); err == nil {
			t.Fatalf("accepted invalid snapshot %s", bad)
		}
	}
}
