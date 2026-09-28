package desktop

import (
	"path/filepath"
	"strconv"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fixturereset"
)

// A reset's "Check empty observation" names one of the project's named
// observations. In a draft the action names the observation by its catalog
// identity; the saved plan, which the command line reads as it is, names the
// file its source was saved as, and a reset reads the latest completed
// collection of the observation's current revision.

// sourceMember is the project entry the current revision of an observation
// saved its source as, or empty.
func sourceMember(item catalog.Item) string {
	if current := item.Current(); current != nil {
		for _, member := range current.Members {
			if member.Role == "source" {
				return member.Path
			}
		}
	}
	return ""
}

// observationSaving is the observation of the project any revision of which
// saved its source as entry.
func (c *loadedCatalog) observationSaving(entry string) (catalog.Item, bool) {
	entry = filepath.ToSlash(filepath.Clean(entry))
	for _, item := range c.document.Items {
		if item.Kind != string(ObservationItem) || c.removed(item) {
			continue
		}
		for _, revision := range item.Revisions {
			for _, member := range revision.Members {
				if member.Role == "source" && member.Path == entry {
					return item, true
				}
			}
		}
	}
	return catalog.Item{}, false
}

// namedObservationSources resolves each collection_empty action of a draft's
// plan, which names an observation by its identity, to the entry the
// observation's current revision saved its source as.
func (s draftScope) namedObservationSources(plan *fixturereset.Plan) []FieldProblem {
	problems := []FieldProblem{}
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.Operator != fixturereset.CollectionEmpty {
			continue
		}
		field := "reset.actions." + strconv.Itoa(i) + ".observation"
		if !s.holdsObservation(action.Observation) {
			problems = append(problems, FieldProblem{Field: field, Problem: "choose one of the project's observations"})
			continue
		}
		entry := sourceMember(s.loaded.document.Items[s.loaded.document.Find(action.Observation)])
		if entry == "" {
			problems = append(problems, FieldProblem{Field: field, Problem: "that observation has no saved source; save it first"})
			continue
		}
		action.Observation = entry
	}
	return problems
}

// observationIdentities names each collection_empty action of a saved plan
// by the observation whose source it reads, as an editor shows it.
func (c *loadedCatalog) observationIdentities(plan *fixturereset.Plan) {
	plan.Actions = append([]fixturereset.Action(nil), plan.Actions...)
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.Operator != fixturereset.CollectionEmpty {
			continue
		}
		if item, found := c.observationSaving(action.Observation); found {
			action.Observation = item.ID
		}
	}
}
