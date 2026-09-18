package evidencesource_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/evidencesource"
)

// FuzzEvidenceSourceDeclaration exercises the one reader this package adds: the
// strict decode that refuses unknown members and unknown versions, and the
// bounds and kind rules every declaration is held to. No input may panic, and
// an accepted declaration must be one a collection could actually run: a kind
// this release dispatches on, a quota nothing can already exceed by
// construction, a retry the collector can apply, and the members of exactly one
// kind — so a declaration that names a remote address nothing dials, or a local
// root nothing opens, is never read as though it had been honoured.
func FuzzEvidenceSourceDeclaration(f *testing.F) {
	directory := strings.Replace(declaredDirectorySource, "ROOT", "/evidence/exports", 1)
	transfer := strings.NewReplacer("COMMAND", "/usr/local/libexec/readmit-sftp", "ARGUMENT", "--host").Replace(declaredTransferSource)
	for _, seed := range []string{
		directory,
		transfer,
		declaredAPISource,
		strings.Replace(transfer, `"arguments": ["--host"],`, `"arguments": ["--host"], "secrets_file": "secrets.json", "credential": "lab-sftp",`, 1),
		strings.Replace(directory, `"kind": "directory"`, `"kind": "transfer"`, 1),
		strings.Replace(directory, `"attempts": 2`, `"attempts": 1`, 1),
		strings.Replace(directory, `"max_entry_bytes": 4096`, `"max_entry_bytes": 33554432`, 1),
		strings.Replace(directory, "source/v1", "source/v2", 1),
		strings.Replace(transfer, `"classification": "nonproduction"`, `"classification": "production"`, 1),
		`{}`,
		`{"schema":"readmit-source/v1"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		source, err := evidencesource.Decode(data)
		if err != nil {
			return
		}
		switch source.Kind {
		case evidencesource.Directory:
			if source.Root == "" || source.Address != "" || source.Command != "" || source.Declared() {
				t.Fatalf("accepted a directory source carrying another kind's members: %+v", source)
			}
			if source.Reaches() {
				t.Fatalf("a directory source reaches no destination: %+v", source)
			}
		case evidencesource.Transfer:
			if source.Command == "" || source.Address == "" || source.Classification == "" || source.Root != "" {
				t.Fatalf("accepted a transfer source that cannot be reached: %+v", source)
			}
			if (source.SecretsFile == "") != (source.Credential == "") {
				t.Fatalf("accepted half a credential reference: %+v", source)
			}
		case evidencesource.API:
			if source.Address == "" || source.Classification == "" || source.Root != "" || source.Command != "" {
				t.Fatalf("accepted an api source carrying another kind's members: %+v", source)
			}
		default:
			t.Fatalf("accepted a kind nothing dispatches on: %q", source.Kind)
		}
		// A quota a collection could never satisfy would refuse every source
		// under it, which is a declaration error rather than a refusal.
		quota := source.Quota
		if quota.MaxEntries < 1 || quota.MaxEntryBytes < 1 || quota.MaxEntryBytes > quota.MaxTotalBytes {
			t.Fatalf("accepted a quota nothing can satisfy: %+v", quota)
		}
		if quota.MaxEntries > evidencesource.MaxEntries || quota.MaxEntryBytes > evidencesource.MaxEntryBytes || quota.MaxTotalBytes > evidencesource.MaxTotalBytes {
			t.Fatalf("accepted a quota past this release's own bounds: %+v", quota)
		}
		// An accepted retry declaration is one the collector can apply: at
		// least one attempt, and a wait it will not sit through forever.
		if source.Retry.Attempts < 1 || source.Retry.Attempts > evidencesource.MaxAttempts {
			t.Fatalf("accepted a retry declaration the collector cannot apply: %+v", source.Retry)
		}
		if wait := source.Retry.Wait(); wait < 0 || wait > evidencesource.MaxBackoff {
			t.Fatalf("accepted a backoff past its bound: %v", wait)
		}
		if source.Identity().Name != source.Name || source.Identity().Scope != source.Scope {
			t.Fatalf("an accepted source does not name itself: %+v", source.Identity())
		}
	})
}
