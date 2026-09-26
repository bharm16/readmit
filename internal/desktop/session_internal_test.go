package desktop

import (
	"path/filepath"
	"testing"
)

func FuzzSession(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-desktop-session/v1","view":{"workspace":"/evidence","region":"evidence","case":"regression","run":"/evidence/job"},"drafts":[{"project":"/evidence","note":{"name":"triage","title":"First pass","body":"still writing"}}]}`))
	f.Add([]byte(`{"schema":"readmit-desktop-session/v1","view":{"workspace":"","region":"","case":"","run":""},"drafts":[]}`))
	f.Add([]byte(`{"schema":"readmit-desktop-session/v2","view":{"workspace":"","region":"","case":"","run":""},"drafts":[]}`))
	f.Add([]byte(`{"schema":"readmit-desktop-session/v1","view":{"workspace":"relative","region":"","case":"../out","run":""},"drafts":[],"extra":1}`))
	f.Add([]byte(`{"schema":"readmit-desktop-session/v1","view":{"workspace":"/w","region":"","case":"","run":""},"drafts":[{"project":"/p","note":{"name":"b","title":"B","body":""}},{"project":"/p","note":{"name":"a","title":"A","body":""}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxSessionBytes {
			return
		}
		session, err := decodeSession(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if session.Schema != SessionSchema {
			t.Fatal("accepted a working session under another contract version")
		}
		// Nothing accepted here may be a location the shell would not open: a
		// retained view is restored into the window, so an accepted one has to
		// be restorable.
		for _, folder := range []string{session.View.Workspace, session.View.Run} {
			if folder != "" && (!filepath.IsAbs(folder) || !printable(folder, maxRootBytes)) {
				t.Fatal("accepted a relative, oversized or unprintable folder")
			}
		}
		if session.View.Case != "" && (session.View.Workspace == "" || !printable(session.View.Case, maxEntryBytes)) {
			t.Fatal("accepted a case with no workspace or past its bound")
		}
		// Whatever the decoder accepts must round-trip through the writer, so a
		// session the shell retains is a session it can restore.
		encoded, err := encodeSession(session)
		if err != nil {
			t.Fatal("accepted a session the shell cannot retain")
		}
		restored, err := decodeSession(encoded)
		if err != nil {
			t.Fatalf("a retained session cannot be read back: %v", err)
		}
		if restored != session {
			t.Fatal("a retained session did not read back as it was written")
		}
	})
}
