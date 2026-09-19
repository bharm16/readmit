package profilelibrary_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/profilelibrary"
	"github.com/bharm16/readmit/internal/profilepack"
)

const fixtureRoot = "../../testdata/fixtures/"

// directory assembles one library directory from named fixtures, so a test
// says which packs are in the library and nothing else.
func directory(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		data, err := os.ReadFile(fixtureRoot + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func openLibrary(t *testing.T, names ...string) profilelibrary.Library {
	t.Helper()
	library, err := profilelibrary.Open(directory(t, names...))
	if err != nil {
		t.Fatalf("open library %v: %v", names, err)
	}
	return library
}

func TestOpenReadsEveryPackAndPublishesItsProvenance(t *testing.T) {
	library := openLibrary(t, "profile-pack.json", "profile-pack-adt.json")
	entries := library.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
	// Read in name order, so profile-pack-adt.json comes first.
	if entries[0].Pack != (profilepack.Identity{ID: "fixture-adt", Version: "1"}) ||
		entries[1].Pack != (profilepack.Identity{ID: "fixture-siu", Version: "1"}) {
		t.Fatalf("identities %+v", entries)
	}
	for _, entry := range entries {
		if entry.Provenance.Source.Name == "" || entry.Provenance.Source.Revision == "" ||
			entry.Provenance.Extraction.Method == "" || entry.Provenance.Extraction.ContentDigest == "" ||
			entry.Provenance.License.SPDX == "" || entry.Provenance.License.Notice == "" ||
			entry.Provenance.RightsReview.Status != profilepack.ReviewApproved {
			t.Errorf("%s publishes incomplete provenance %+v", entry.Pack.ID, entry.Provenance)
		}
	}
}

// TestOneCombinationIsAnsweredByOnePackAndNothingIsBorrowed is the library's
// one rule read as a query test: the pack that declares a combination answers
// it alone, the same HL7 version under two packs answers twice over without
// either borrowing from the other, and everything nobody declared is unknown.
func TestOneCombinationIsAnsweredByOnePackAndNothingIsBorrowed(t *testing.T) {
	library := openLibrary(t, "profile-pack.json", "profile-pack-adt.json")
	siu := profilepack.Identity{ID: "fixture-siu", Version: "1"}
	adt := profilepack.Identity{ID: "fixture-adt", Version: "1"}
	for _, tc := range []struct {
		version, family string
		level           profilepack.Level
		want            profilelibrary.Answer
	}{
		{"2.5.1", "SIU", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeSupported, Pack: siu}},
		{"2.5.1", "SIU", profilepack.LevelLabels, profilelibrary.Answer{Outcome: profilepack.OutcomeSupported, Pack: siu}},
		{"2.5.1", "SIU", profilepack.LevelStructural, profilelibrary.Answer{Outcome: profilepack.OutcomeUnsupported, Pack: siu}},
		{"2.5.1", "SIU", profilepack.LevelWorkflow, profilelibrary.Answer{Outcome: profilepack.OutcomeUnsupported, Pack: siu}},
		{"2.5.1", "ADT", profilepack.LevelLabels, profilelibrary.Answer{Outcome: profilepack.OutcomeUntested, Pack: siu}},
		// One HL7 version, two packs, two answers, neither borrowed.
		{"2.4", "SIU", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUntested, Pack: siu}},
		{"2.4", "SIU", profilepack.LevelLabels, profilelibrary.Answer{Outcome: profilepack.OutcomeUnsupported, Pack: siu}},
		{"2.4", "ADT", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeSupported, Pack: adt}},
		{"2.4", "ADT", profilepack.LevelLabels, profilelibrary.Answer{Outcome: profilepack.OutcomeSupported, Pack: adt}},
		{"2.8.2", "ADT", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUntested, Pack: adt}},
		// Nothing declares these, so nobody answers and no pack is named.
		{"2.8.2", "SIU", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		{"2.6", "ORU", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		{"2.5", "ADT", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		// Names outside the closed sets, and a level outside them, are unknown.
		{"2.9", "ADT", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		{"2.4", "adt", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		{"2.4", "ADT", profilepack.Level("semantic"), profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
		{"", "", profilepack.LevelParse, profilelibrary.Answer{Outcome: profilepack.OutcomeUnknown}},
	} {
		got := library.Support(tc.version, tc.family, tc.level)
		if got != tc.want {
			t.Errorf("Support(%q, %q, %q) = %+v, want %+v", tc.version, tc.family, tc.level, got, tc.want)
		}
		if got.Outcome.Passing() != (tc.want.Outcome == profilepack.OutcomeSupported) {
			t.Errorf("Support(%q, %q, %q) passing %v for %q", tc.version, tc.family, tc.level, got.Outcome.Passing(), got.Outcome)
		}
	}
}

// TestALabelNeverReachesACombinationAnotherPackCovers keeps one pack's labels
// content from filling another pack's gap at the same HL7 version.
func TestALabelNeverReachesACombinationAnotherPackCovers(t *testing.T) {
	library := openLibrary(t, "profile-pack.json", "profile-pack-adt.json")
	adt := profilepack.Identity{ID: "fixture-adt", Version: "1"}
	siu := profilepack.Identity{ID: "fixture-siu", Version: "1"}
	for _, tc := range []struct {
		version, family, segment string
		position                 int
		want                     profilelibrary.Label
	}{
		{"2.4", "ADT", "EVN", 1, profilelibrary.Label{Outcome: profilepack.OutcomeSupported, Name: "Event Type Code", Pack: adt}},
		// The same version and segment under the family the other pack
		// declares: that pack's labels are unsupported, so there is no name.
		{"2.4", "SIU", "EVN", 1, profilelibrary.Label{Outcome: profilepack.OutcomeUnsupported, Pack: siu}},
		{"2.5.1", "SIU", "SCH", 1, profilelibrary.Label{Outcome: profilepack.OutcomeSupported, Name: "Placer Appointment ID", Pack: siu}},
		// A supported combination may still leave a position unlabelled.
		{"2.5.1", "SIU", "SCH", 7, profilelibrary.Label{Outcome: profilepack.OutcomeSupported, Pack: siu}},
		// The 2.5.1 pack carries labels; the ADT combination is untested, so
		// none of them is handed out under it.
		{"2.5.1", "ADT", "SCH", 1, profilelibrary.Label{Outcome: profilepack.OutcomeUntested, Pack: siu}},
		// Nobody declares this combination at all.
		{"2.8.2", "SIU", "EVN", 1, profilelibrary.Label{Outcome: profilepack.OutcomeUnknown}},
	} {
		got := library.Label(tc.version, tc.family, tc.segment, tc.position)
		if got != tc.want {
			t.Errorf("Label(%q, %q, %q, %d) = %+v, want %+v", tc.version, tc.family, tc.segment, tc.position, got, tc.want)
		}
		if got.Name != "" && !got.Outcome.Passing() {
			t.Errorf("a name reached a combination whose labels are %q", got.Outcome)
		}
	}
}

// TestTheMatrixPublishesEveryCombinationCoveredOrNot checks the published
// matrix is the complete product of the closed sets, in the decided order, and
// that the combinations nothing covers are published as unknown rather than
// left out.
func TestTheMatrixPublishesEveryCombinationCoveredOrNot(t *testing.T) {
	library := openLibrary(t, "profile-pack.json", "profile-pack-adt.json")
	rows := library.Matrix()
	versions := profilepack.HL7Versions()
	families := profilepack.Families()
	if len(rows) != len(versions)*len(families) {
		t.Fatalf("published %d rows, want %d", len(rows), len(versions)*len(families))
	}
	declared := 0
	for i, row := range rows {
		wantVersion := versions[i/len(families)]
		wantFamily := families[i%len(families)]
		if row.HL7Version != wantVersion || row.Family != wantFamily {
			t.Fatalf("row %d is %s %s, want %s %s", i, row.HL7Version, row.Family, wantVersion, wantFamily)
		}
		unknown := row.Parse == profilepack.OutcomeUnknown && row.Labels == profilepack.OutcomeUnknown &&
			row.Structural == profilepack.OutcomeUnknown && row.Workflow == profilepack.OutcomeUnknown
		if unknown {
			if row.Pack != (profilepack.Identity{}) {
				t.Errorf("%s %s answers unknown and still names %+v", row.HL7Version, row.Family, row.Pack)
			}
			continue
		}
		declared++
		if row.Pack == (profilepack.Identity{}) {
			t.Errorf("%s %s answers %q with no pack named", row.HL7Version, row.Family, row.Parse)
		}
		if row.Structural != profilepack.OutcomeUnsupported || row.Workflow != profilepack.OutcomeUnsupported {
			t.Errorf("%s %s publishes structural %q workflow %q; no v1 pack carries either", row.HL7Version, row.Family, row.Structural, row.Workflow)
		}
	}
	if declared != 5 {
		t.Fatalf("the fixture library declares %d combinations, want 5", declared)
	}
}

// TestThePublishedMatrixIsTheOneTheBundledLibraryAnswers reads the table this
// release publishes and compares it against the library this release bundles,
// which holds no pack. The page cannot claim coverage the code does not give.
func TestThePublishedMatrixIsTheOneTheBundledLibraryAnswers(t *testing.T) {
	page, err := os.ReadFile("../../docs/profile-library.md")
	if err != nil {
		t.Fatalf("read the published page: %v", err)
	}
	var published [][]string
	for _, line := range strings.Split(string(page), "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 7 {
			continue
		}
		for i, cell := range cells {
			cells[i] = strings.Trim(strings.TrimSpace(cell), "`")
		}
		if !strings.HasPrefix(cells[0], "2.") {
			continue
		}
		published = append(published, cells)
	}
	rows := profilelibrary.Library{}.Matrix()
	if len(published) != len(rows) {
		t.Fatalf("the page publishes %d combinations, the library answers %d", len(published), len(rows))
	}
	for i, row := range rows {
		want := []string{row.HL7Version, row.Family, string(row.Parse), string(row.Labels), string(row.Structural), string(row.Workflow), "none"}
		if row.Pack != (profilepack.Identity{}) {
			want[6] = row.Pack.ID + " " + row.Pack.Version
		}
		for cell := range want {
			if published[i][cell] != want[cell] {
				t.Errorf("published row %d is %v, the library answers %v", i, published[i], want)
				break
			}
		}
	}
}

func TestTheZeroLibraryAnswersUnknownAndSatisfiesNoGate(t *testing.T) {
	var library profilelibrary.Library
	if entries := library.Entries(); len(entries) != 0 {
		t.Fatalf("entries %+v", entries)
	}
	for _, version := range profilepack.HL7Versions() {
		for _, family := range profilepack.Families() {
			for _, level := range profilepack.Levels() {
				if got := library.Support(version, family, level); got.Outcome != profilepack.OutcomeUnknown || got.Pack != (profilepack.Identity{}) {
					t.Fatalf("Support(%q, %q, %q) = %+v", version, family, level, got)
				}
			}
			if got := library.Label(version, family, "MSH", 9); got != (profilelibrary.Label{Outcome: profilepack.OutcomeUnknown}) {
				t.Fatalf("Label(%q, %q) = %+v", version, family, got)
			}
		}
	}
	if err := library.Bundleable(); err == nil {
		t.Fatal("a library holding no pack is not a library to bundle")
	}
}

// TestAPendingRightsReviewIsReadableAndNotBundleable is the gate: nothing
// software does completes a rights review, and a library is refused whole
// rather than in part.
func TestAPendingRightsReviewIsReadableAndNotBundleable(t *testing.T) {
	approved := openLibrary(t, "profile-pack.json", "profile-pack-adt.json")
	if err := approved.Bundleable(); err != nil {
		t.Fatalf("a library of approved packs is bundleable: %v", err)
	}
	pending := openLibrary(t, "profile-pack.json", "profile-pack-pending-review.json")
	if len(pending.Entries()) != 2 {
		t.Fatalf("a pending pack is still read: %+v", pending.Entries())
	}
	if got := pending.Support("2.6", "ORU", profilepack.LevelParse); got.Outcome != profilepack.OutcomeUntested {
		t.Fatalf("a pending pack still answers for its combination: %+v", got)
	}
	err := pending.Bundleable()
	if err == nil {
		t.Fatal("a library holding a pending rights review is not bundleable")
	}
	if !strings.Contains(err.Error(), "fixture-pending") {
		t.Errorf("the refusal does not name the pack holding it back: %v", err)
	}
}

func TestOpenRefusesEveryLibraryItCannotAnswerFrom(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T) string
		reason  string
	}{
		{
			name:    "two packs declare the same combination",
			prepare: func(t *testing.T) string { return directory(t, "profile-pack.json", "profile-pack-overlapping.json") },
			reason:  "both declare HL7 2.5.1 SIU",
		},
		{
			name: "two packs share one id",
			prepare: func(t *testing.T) string {
				root := directory(t, "profile-pack.json")
				data, err := os.ReadFile(fixtureRoot + "profile-pack.json")
				if err != nil {
					t.Fatal(err)
				}
				// Same id, a later version, and a combination nobody else
				// declares: only the repeated id refuses this library.
				second := strings.Replace(string(data), `"version": "1"`, `"version": "2"`, 1)
				second = strings.ReplaceAll(second, `"hl7_version": "2.5.1"`, `"hl7_version": "2.6"`)
				second = strings.Replace(second, `"hl7_version": "2.4"`, `"hl7_version": "2.7.1"`, 1)
				if err := os.WriteFile(filepath.Join(root, "second.json"), []byte(second), 0o600); err != nil {
					t.Fatal(err)
				}
				return root
			},
			reason: "holds fixture-siu more than once",
		},
		{
			name:    "a document the pack reader refuses",
			prepare: func(t *testing.T) string { return directory(t, "profile-pack.json", "profile-pack-refused.json") },
			reason:  "carries no structural or workflow content",
		},
		{
			name: "a file that is not a pack document",
			prepare: func(t *testing.T) string {
				root := directory(t, "profile-pack.json")
				if err := os.WriteFile(filepath.Join(root, "NOTES.txt"), []byte("notes"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root
			},
			reason: "named *.json and nothing else",
		},
		{
			name: "a subdirectory",
			prepare: func(t *testing.T) string {
				root := directory(t, "profile-pack.json")
				if err := os.Mkdir(filepath.Join(root, "archived"), 0o700); err != nil {
					t.Fatal(err)
				}
				return root
			},
			reason: "named *.json and nothing else",
		},
		{
			name: "more packs than a library holds",
			prepare: func(t *testing.T) string {
				root := t.TempDir()
				data, err := os.ReadFile(fixtureRoot + "profile-pack.json")
				if err != nil {
					t.Fatal(err)
				}
				for i := range profilelibrary.MaxPacks + 1 {
					name := filepath.Join(root, "pack-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".json")
					if err := os.WriteFile(name, data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return root
			},
			reason: "at most 32 packs",
		},
		{
			name: "a member longer than a pack may be",
			prepare: func(t *testing.T) string {
				root := directory(t, "profile-pack.json")
				oversized := make([]byte, profilepack.MaxPackBytes+1)
				for i := range oversized {
					oversized[i] = ' '
				}
				if err := os.WriteFile(filepath.Join(root, "oversized.json"), oversized, 0o600); err != nil {
					t.Fatal(err)
				}
				return root
			},
			reason: "exceeds the 4 MiB a profile pack may be",
		},
		{
			name:    "a directory that is not there",
			prepare: func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") },
			reason:  "cannot read a profile library directory",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refuses(t, tc.prepare(t), tc.reason)
		})
	}
}

// refuses opens one library directory, requires the refusal to state its
// reason, and requires that nothing partially read answers anything: a library
// is refused whole or not at all.
func refuses(t *testing.T, root, reason string) {
	t.Helper()
	library, err := profilelibrary.Open(root)
	if err == nil {
		t.Fatal("opened a library that cannot be answered from")
	}
	if !strings.Contains(err.Error(), reason) {
		t.Errorf("refusal %q does not state %q", err, reason)
	}
	if got := library.Support("2.5.1", "SIU", profilepack.LevelParse); got.Outcome != profilepack.OutcomeUnknown {
		t.Errorf("a refused library answered %+v", got)
	}
	if err := library.Bundleable(); err == nil {
		t.Error("a refused library is bundleable")
	}
}

func TestAnEmptyDirectoryIsAnEmptyLibrary(t *testing.T) {
	library, err := profilelibrary.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open an empty library: %v", err)
	}
	if entries := library.Entries(); len(entries) != 0 {
		t.Fatalf("entries %+v", entries)
	}
	for _, row := range library.Matrix() {
		if row.Parse != profilepack.OutcomeUnknown || row.Pack != (profilepack.Identity{}) {
			t.Fatalf("an empty library published %+v", row)
		}
	}
	if err := library.Bundleable(); err == nil {
		t.Fatal("an empty library is not a library to bundle")
	}
}
