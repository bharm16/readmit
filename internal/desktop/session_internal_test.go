package desktop

import (
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/project"
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
		if len(session.Drafts) > MaxDrafts {
			t.Fatal("accepted an unbounded number of retained drafts")
		}
		// Nothing accepted here may be a location the shell would not open or
		// working text the project would not store: a retained session is
		// restored into the window, so an accepted one has to be restorable.
		for _, folder := range []string{session.View.Workspace, session.View.Run} {
			if folder != "" && (!filepath.IsAbs(folder) || !printable(folder, maxRootBytes)) {
				t.Fatal("accepted a relative, oversized or unprintable folder")
			}
		}
		if session.View.Case != "" && (session.View.Workspace == "" || !printable(session.View.Case, maxEntryBytes)) {
			t.Fatal("accepted a case with no workspace or past its bound")
		}
		for i, draft := range session.Drafts {
			if err := project.ValidateNote(draft.Note); err != nil {
				t.Fatalf("accepted working text the project would refuse: %v", err)
			}
			if !filepath.IsAbs(draft.Project) {
				t.Fatal("accepted a draft of a relative project folder")
			}
			if i > 0 && compareDrafts(session.Drafts[i-1], draft) >= 0 {
				t.Fatal("accepted unsorted or duplicated drafts")
			}
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
		if restored.View != session.View || len(restored.Drafts) != len(session.Drafts) {
			t.Fatal("a retained session did not read back as it was written")
		}
		// The decoder normalizes nothing: a document that declared no draft
		// decodes to no draft, and turning that into the empty list the facade
		// hands out is the reader's job, not the decoder's.
		if len(session.Drafts) == 0 && len(restored.Drafts) != 0 {
			t.Fatal("the decoder invented a draft list")
		}
	})
}
