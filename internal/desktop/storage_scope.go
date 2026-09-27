package desktop

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/protect"
)

// An archive or a deletion names what it concerns beyond the files it copies:
// the retention the project declares for what it holds, and the related work
// that refers to it. Two declarations are read. A protected transfer package
// declares a minimum hold: while it is within the retention it declares, it
// is not deleted — protect.Discard refuses it, and deleting the project or
// case holding it would bypass that refusal — so it blocks Delete source,
// with no override. A search index declares a maximum: past its end it is
// not used or rebuilt, and it never blocks a deletion, so it is shown only.

// RetentionKind is what declared one retention a storage review names.
type RetentionKind string

const (
	// TransferPackageRetention is a protected transfer package's declared
	// retention: while it lasts, the package blocks deletion.
	TransferPackageRetention RetentionKind = "transfer-package"
	// SearchIndexRetention is the retention a case's search index declared
	// for what it retains: shown, never blocking.
	SearchIndexRetention RetentionKind = "search-index"
)

// RetentionHold is one retention the source of an archive or a deletion
// declares. Entry is where it is declared, relative to the project folder;
// Case is the case entry a search index is of. State is what the declaration
// says now — within-retention, past-retention or not-declared — and Until the
// end it declares, RFC 3339 in UTC, empty when it declares none (a search
// index retained indefinitely is within-retention with no end). Blocks says
// it refuses Delete source. A package whose declaration cannot be read has no
// state, says why in Problem, and blocks: its retention is not known.
type RetentionHold struct {
	Kind    RetentionKind          `json:"kind"`
	Entry   string                 `json:"entry"`
	Case    string                 `json:"case,omitzero"`
	State   protect.RetentionState `json:"state,omitzero"`
	Until   string                 `json:"until,omitzero"`
	Blocks  bool                   `json:"blocks,omitzero"`
	Problem string                 `json:"problem,omitzero"`
}

// RelatedWork is how many objects of one kind refer to the source of an
// archive or a deletion: for a project, what it holds; for a case, the
// analyses, reports and variants about it.
type RelatedWork struct {
	Kind  ItemKind `json:"kind"`
	Count int      `json:"count"`
}

// retentionOf reads every retention the project at root declares for what it
// holds: every protected transfer package inside it — inside the case folder
// entry alone, when a case is named — and every search index beside it, or
// only those of the case with identity. It changes nothing.
func retentionOf(root, entry, identity string, now time.Time) ([]RetentionHold, error) {
	holds := []RetentionHold{}
	walked := root
	if entry != "" {
		walked = filepath.Join(root, entry)
	}
	err := filepath.WalkDir(walked, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("cannot read the project folder for its retention declarations")
		}
		if !d.IsDir() {
			return nil
		}
		descriptor := filepath.Join(path, protect.DescriptorName)
		if info, err := os.Lstat(descriptor); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		hold := RetentionHold{Kind: TransferPackageRetention, Entry: filepath.ToSlash(relative)}
		held, _, err := protect.ReadPackage(path)
		switch {
		case err != nil:
			hold.Problem, hold.Blocks = "the package's retention cannot be read: "+err.Error(), true
		default:
			hold.State = held.Retention(now)
			hold.Blocks = hold.State == protect.WithinRetention
			if !held.RetainUntil.IsZero() {
				hold.Until = held.RetainUntil.UTC().Format(time.RFC3339)
			}
		}
		holds = append(holds, hold)
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	opened, err := project.Open(root)
	if err != nil {
		return nil, err
	}
	cases := map[string]string{}
	for _, registered := range opened.Document.Cases {
		cases[registered.Identity] = registered.Name
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("cannot read the project folder for its retention declarations")
	}
	for _, found := range entries {
		if !found.Type().IsRegular() {
			continue
		}
		path, err := artifactpath.File(root, found.Name())
		if err != nil || !declaresIndex(path) {
			continue
		}
		document, err := index.Open(path)
		if err != nil || identity != "" && document.Case.Identity != identity {
			continue
		}
		hold := RetentionHold{Kind: SearchIndexRetention, Entry: found.Name(), Case: cases[document.Case.Identity], State: protect.WithinRetention}
		if document.Policy.RetainUntil != nil {
			hold.Until = document.Policy.RetainUntil.UTC().Format(time.RFC3339)
		}
		if document.Usable(now) != nil {
			hold.State = protect.PastRetention
		}
		holds = append(holds, hold)
	}
	return holds, nil
}

// blockingHold is the first retention that refuses a deletion, as the
// refusal words it, or empty when none does.
func blockingHold(holds []RetentionHold) string {
	for _, hold := range holds {
		switch {
		case hold.Blocks && hold.Problem != "":
			return hold.Entry + " is a protected transfer package whose retention cannot be read; nothing is deleted"
		case hold.Blocks:
			return hold.Entry + " is declared retained until " + hold.Until + "; nothing is deleted"
		}
	}
	return ""
}

// holdParts are the retention declarations a binding covers, so a changed
// declaration makes a review stale.
func holdParts(holds []RetentionHold) []string {
	parts := make([]string, 0, len(holds))
	for _, hold := range holds {
		parts = append(parts, string(hold.Kind)+"\x00"+hold.Entry+"\x00"+string(hold.State)+"\x00"+hold.Until+"\x00"+hold.Problem)
	}
	return parts
}

// relatedOf counts the objects of the project that refer to the source: every
// object the project holds, by kind, or, for the case with id, the objects
// about it.
func relatedOf(loaded *loadedCatalog, id string) []RelatedWork {
	counts := map[ItemKind]int{}
	for _, item := range loaded.document.Items {
		kind := ItemKind(item.Kind)
		if loaded.removed(item) || id != "" && (item.ID == id || kind == CaseItem) {
			continue
		}
		if id == "" {
			counts[kind]++
			continue
		}
		read := loaded.read(item)
		if relatedTo(read, id) || read.Summary.Variant != nil && read.Summary.Variant.Parent != nil && read.Summary.Variant.Parent.ID == id {
			counts[kind]++
		}
	}
	related := []RelatedWork{}
	for kind, count := range counts {
		related = append(related, RelatedWork{Kind: kind, Count: count})
	}
	slices.SortFunc(related, func(x, y RelatedWork) int { return cmp.Compare(x.Kind, y.Kind) })
	return related
}
