package artifactdir_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/report"
)

// A report keeps the ledger its trial fixtures write only as copies inside its
// packet, and those fixtures install the ledger without flushing it (#350). So
// every file the packet holds, the ledger copies among them, is synced holding
// its final bytes before the report is created, and a ledger copy that cannot
// be synced leaves no complete packet.
func TestEveryFileOfAReportPacketIsSyncedBeforeTheReportIsCreated(t *testing.T) {
	// The fixture and the sender write at once, so their syncs are recorded
	// under one lock.
	var mu sync.Mutex
	synced := map[string][]byte{}
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		data, err := os.ReadFile(path)
		mu.Lock()
		synced[filepath.Clean(path)] = data
		mu.Unlock()
		return err
	}))
	packet := filepath.Join(caseFolder(t), "packet")
	created, err := report.Create(t.Context(), report.Scenario, packet)
	if err != nil {
		t.Fatal(err)
	}
	for _, trial := range []string{"baseline", "post-fix"} {
		if _, err := os.Stat(filepath.Join(packet, trial, "observation.json")); err != nil {
			t.Fatalf("the packet holds no %s ledger copy: %v", trial, err)
		}
	}
	err = filepath.WalkDir(packet, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if written, ok := synced[filepath.Clean(path)]; !ok || !bytes.Equal(written, data) {
			relative, _ := filepath.Rel(packet, path)
			t.Errorf("the report was created before %s was synced holding its bytes", filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := report.Open(packet); err != nil || opened.Identity != created.Identity {
		t.Fatalf("the synced packet did not reopen as created: %v", err)
	}

	unsynced := filepath.Join(caseFolder(t), "packet")
	ledgerCopy := filepath.Join(unsynced, "baseline", "observation.json")
	t.Cleanup(artifactdir.ObserveFileSyncsForTest(func(path string) error {
		if filepath.Clean(path) == ledgerCopy {
			return errors.New("injected file sync failure")
		}
		return nil
	}))
	if _, err := report.Create(t.Context(), report.Scenario, unsynced); err == nil || !strings.Contains(err.Error(), "incomplete output retained") {
		t.Fatalf("a report whose ledger copy could not be synced was not refused as incomplete: %v", err)
	}
	if _, err := report.Open(unsynced); err == nil {
		t.Fatal("a packet whose ledger copy could not be synced opened as complete")
	}
}
