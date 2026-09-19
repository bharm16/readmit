package transform

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

// JSON encodes the preview deterministically as one
// readmit-transform-preview/v1 document. It reopens no evidence and re-decides
// nothing.
func JSON(preview Preview) ([]byte, error) {
	data, err := json.Marshal(preview, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode transformation preview")
	}
	return append(data, '\n'), nil
}

// Terminal renders the same preview model as JSON, including every relation the
// sequence broke and every position the transformation left alone. It prints no
// value byte: a row is a position, a state and the relation a rename assigned.
func Terminal(preview Preview) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "Contract: %s\nCase identity: %s\nRules SHA-256: %s\n",
		preview.Schema, preview.Case.Identity, preview.Plan.Rules)
	pinned := "none"
	if preview.Plan.Profile.ID != "" {
		pinned = preview.Plan.Profile.ID + " " + preview.Plan.Profile.Version
	}
	fmt.Fprintf(&out, "Pinned profile pack: %s\n", pinned)
	fmt.Fprintf(&out, "Occurrences: %d\nSequence entries: %d (%d copies)\nChanges: %d\nRelations: %d (%d preserved)\nUnsupported: %d\n",
		preview.Summary.Occurrences, preview.Summary.Entries, preview.Summary.Copies, preview.Summary.Changes,
		preview.Summary.Relations, preview.Summary.Preserved, preview.Summary.Unsupported)

	fmt.Fprintln(&out, "\nSequence:")
	for _, entry := range preview.Sequence {
		fmt.Fprintf(&out, "  %3d %s parent=%s source=%s", entry.Position, entry.ID, entry.Parent, entry.Source)
		if entry.Copy {
			fmt.Fprint(&out, " copy")
		}
		fmt.Fprintln(&out)
	}

	fmt.Fprintln(&out, "\nChanges (no value is shown; equal relation numbers receive equal values):")
	if len(preview.Changes) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, change := range preview.Changes {
		fmt.Fprintf(&out, "  %s %s %s was=%s", change.Entry, change.Operator, change.Selector, change.State)
		if change.Rule != "" {
			fmt.Fprintf(&out, " rule=%s relation=%d", change.Rule, change.Group)
		}
		fmt.Fprintf(&out, " bytes=%d\n", change.Length)
	}

	fmt.Fprintln(&out, "\nRelations:")
	if len(preview.Relations) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, relation := range preview.Relations {
		fmt.Fprintf(&out, "  rule=%s operator=%s linkage=%s preserved=%t", relation.Rule, relation.Operator, relation.Linkage, relation.Preserved)
		if relation.Reason != "" {
			fmt.Fprintf(&out, " reason=%s", relation.Reason)
		}
		fmt.Fprintf(&out, " occurrences=%s entries=%s\n", join(relation.Occurrences), join(relation.Entries))
	}

	fmt.Fprintln(&out, "\nDeclared profile support (only supported passes):")
	for _, combination := range preview.Profile {
		version, family := combination.Version, combination.Family
		if version == "" {
			version = "(none)"
		}
		if family == "" {
			family = "(none)"
		}
		fmt.Fprintf(&out, "  %s %s entries=%d parse=%s labels=%s structural=%s workflow=%s\n",
			version, family, combination.Entries, combination.Parse, combination.Labels,
			combination.Structural, combination.Workflow)
	}

	fmt.Fprintln(&out, "\nUnsupported (left exactly as the evidence has it):")
	if len(preview.Unsupported) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, item := range preview.Unsupported {
		fmt.Fprintf(&out, "  %s", item.Code)
		for _, part := range []struct{ name, value string }{{"parent", item.Parent}, {"rule", item.Rule}, {"field", item.Selector}} {
			if part.value != "" {
				fmt.Fprintf(&out, " %s=%s", part.name, part.value)
			}
		}
		fmt.Fprintf(&out, ": %s\n", item.Detail)
	}

	fmt.Fprintf(&out, "\n%s\n", preview.Scope)
	return out.Bytes()
}

func join(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ",")
}
