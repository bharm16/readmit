package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const databaseSourceDocument = `{"schema":"readmit-observation-source/v3","source":{"kind":"database-query","identity":"scheduling-archive","scope":"appointments"},"enabled":false,"freshness":{"max_age":"1h"},"extraction":null,"file":null,"http":null,"capture":null,"database":{"driver":"postgresql","address":"127.0.0.1:5432","classification":"nonproduction","name":"synthetic","username":"observer","ca_file":"","server_name":"localhost","credential":{"store":"customer-managed","address":"127.0.0.1:5432","purpose":"database-observation","command":"/unavailable/provider","arguments":[]},"view":["public","observed"],"record_key":"appointment","key_type":"text","filters":[],"limits":null}}`

func TestObserveDatabaseDisabledAndResetCredentialsAreNeverAbsence(t *testing.T) {
	for _, reset := range []bool{false, true} {
		document := databaseSourceDocument
		if reset {
			document = strings.Replace(document, "database-observation", "database-reset", 1)
		}
		directory, window, source := collectDocuments(t, document)
		if err := os.WriteFile(window, []byte(strings.Replace(collectWindowDocument, "file-export", "database-query", 1)), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := run(t, "observe", "collect", source, "--window", window, "--out", filepath.Join(directory, "completion.json"), "--snapshot", filepath.Join(directory, "snapshot"), "--json")
		if reset {
			if exitCode(t, err) != 1 || !strings.Contains(stderr, "reset credentials are refused") {
				t.Fatalf("reset reference was not refused: %v %s", err, stderr)
			}
		} else {
			if exitCode(t, err) != 2 || !strings.Contains(stdout, `"status":"missing"`) {
				t.Fatalf("disabled became an observation: %v %s", err, stdout)
			}
		}
	}
}
