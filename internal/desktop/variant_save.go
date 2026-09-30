package desktop

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/transform"
)

// A variant is a new case derived from one the project registers, saved as
// one publication: the whole plan is validated first, the derived case is
// built in the catalog's staging area and read back, and only then is it
// published as a new entry of the project, registered as a revision of its
// source — lineage and association — and named by the catalog. A derived
// case is never listed without its association, and the case it came from is
// never touched.

// VariantDraft is a variant as its editor holds it: the registered case or
// revision it is derived from, and the reproducer plan applied to it.
type VariantDraft struct {
	Source ItemRef         `json:"source"`
	Plan   reproducer.Plan `json:"plan"`
}

// variantPrefix names the entries variants are published as.
const variantPrefix = "variant"

// variantSource is the registered case or revision a variant draft names,
// verified now: its entry and its bundle.
type variantSource struct {
	entry  string
	path   string
	bundle *bundle.Bundle
}

func readVariant(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(VariantItem)]
	info, err := os.Stat(path)
	if err != nil {
		return view{}, errors.New("the variant cannot be inspected")
	}
	if !info.IsDir() {
		data, err := boundedFile(path, 4<<20)
		if err != nil {
			return view{}, err
		}
		plan, err := transform.DecodePlan(data)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Variant: &VariantSummary{Form: "transform-plan", Parent: c.caseByIdentity(plan.Case)}}}, nil
	}
	if regular(filepath.Join(path, reproducer.ManifestName)) {
		manifest, err := reproducer.Open(path)
		if err != nil {
			return view{}, err
		}
		return view{summary: ItemSummary{Variant: &VariantSummary{Form: "reproducer", Parent: c.caseByIdentity(manifest.Parent.Identity)}}}, nil
	}
	// A derived case the project has not registered as a revision.
	if _, _, err := operation.VerifiedCase(c.root, item.Entry); err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{Variant: &VariantSummary{Form: "derived-case", Entry: item.Entry}}}, nil
}

// validateVariantDraft validates a variant's whole plan against its source and
// answers the plan document it is saved with.
func validateVariantDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	normalized := ItemDraft{Name: draft.Name}
	if draft.Variant == nil {
		return nil, normalized, []FieldProblem{{Field: "variant", Problem: "a variant is a plan applied to one registered case or revision"}}
	}
	if scope.item != "" {
		return nil, normalized, []FieldProblem{{Field: "item", Problem: "a variant is saved as a new case; derived evidence is never changed"}}
	}
	source, problem := resolveVariantSource(scope, draft.Variant.Source)
	if problem != nil {
		return nil, normalized, []FieldProblem{*problem}
	}
	plan := draft.Variant.Plan
	if plan.Case != source.bundle.Identity {
		return nil, normalized, []FieldProblem{{Field: "variant.plan", Problem: "the plan was made for different evidence than the source it names"}}
	}
	resolution, err := reproducer.Resolve(source.bundle, plan)
	if err == nil && len(resolution.Occurrences) == 0 {
		err = errors.New("a variant retains at least one occurrence")
	}
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "variant.plan", Problem: err.Error()}}
	}
	data, err := json.Marshal(plan, json.Deterministic(true))
	if err == nil {
		_, err = reproducer.DecodePlan(data)
	}
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "variant.plan", Problem: "the plan cannot be saved as a reproducer plan document"}}
	}
	normalized.Variant = &VariantDraft{Source: ItemRef{Kind: draft.Variant.Source.Kind, ID: draft.Variant.Source.ID}, Plan: plan}
	return []catalog.Staged{{Role: "plan", File: "plan.json", Data: append(data, '\n')}}, normalized, nil
}

// resolveVariantSource reads the source a variant names: a case or revision
// the project registers, whose evidence verifies now as what was registered.
func resolveVariantSource(scope draftScope, ref ItemRef) (variantSource, *FieldProblem) {
	refused := &FieldProblem{Field: "variant.source", Problem: "a variant is derived from a case or revision the project registers"}
	loaded := scope.loaded
	if loaded == nil || ref.Kind != CaseItem && ref.Kind != VariantItem {
		return variantSource{}, refused
	}
	index := loaded.document.Find(ref.ID)
	if index < 0 || loaded.document.Items[index].Kind != string(ref.Kind) || loaded.removed(loaded.document.Items[index]) {
		return variantSource{}, refused
	}
	entry := loaded.document.Items[index].Entry
	recorded, registered, revision := loaded.recordedAt(entry)
	if entry == "" || registered == nil && revision == nil {
		return variantSource{}, refused
	}
	path := filepath.Join(loaded.root, entry)
	opened, err := operation.OpenVerifiedCase(path, recorded.Identity)
	if err != nil {
		return variantSource{}, &FieldProblem{Field: "variant.source", Problem: err.Error()}
	}
	return variantSource{entry: entry, path: path, bundle: opened}, nil
}

// variantEntry is how a variant's derived case is built and what it owes:
// built from its source by the reproducer the command line runs, placed as
// the derived case alone, and registered as a revision of its source.
func variantEntry(source variantSource, plan reproducer.Plan) *catalog.Entry {
	return &catalog.Entry{Prefix: variantPrefix, Owes: source.entry, Build: func(path string) error {
		built := path + ".build"
		if err := os.RemoveAll(built); err != nil {
			return err
		}
		defer os.RemoveAll(built)
		if _, err := reproducer.Create(source.bundle, source.path, plan, built); err != nil {
			return err
		}
		return os.Rename(filepath.Join(built, reproducer.CaseName), path)
	}}
}

// verifyVariant reads a staged variant back: its plan through the plan
// reader, and its derived case through the case reader, as evidence the
// reproducer derived, which is not the case the plan names itself. That it
// was derived from that case is the build's, from the verified source.
func verifyVariant(files map[string]string) error {
	data, err := boundedFile(files["plan"], catalog.MaxMemberBytes)
	if err != nil {
		return err
	}
	plan, err := reproducer.DecodePlan(data)
	if err != nil {
		return err
	}
	derived, err := operation.OpenCase(files[catalog.EntryRole])
	if err != nil {
		return err
	}
	if derived.Manifest.Provenance.Mode != bundle.Derived || derived.Manifest.Provenance.Derivation != reproducer.Derivation || derived.Identity == plan.Case {
		return errors.New("the saved variant is not evidence derived by its plan")
	}
	return nil
}

// associateEntry records what a published entry of the project owes: a
// variant is registered as a revision of the case or revision it was derived
// from, once.
func associateEntry(root string) func(kind, entry, owes string) error {
	return func(kind, entry, owes string) error {
		if ItemKind(kind) == ReportItem {
			// A report's retained packet owes the project nothing more.
			return nil
		}
		if ItemKind(kind) != VariantItem {
			return errors.New("this release publishes no entry of this kind")
		}
		if revisions, err := project.ReadRevisions(root); err == nil {
			for _, registered := range revisions.Revisions {
				if registered.Name == entry {
					if registered.Operation.Parent != owes {
						return errors.New("the entry is registered as a revision of another case")
					}
					return nil
				}
			}
		}
		_, err := operation.RegisterRevision(root, entry, owes)
		return err
	}
}
