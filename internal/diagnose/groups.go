package diagnose

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
)

const GroupsSchema = "readmit-diagnosis-groups/v1"
const MaxGroupCases = 16

// GroupsReport is a derived presentation. Cases retain the complete unchanged
// diagnoses; grouping never substitutes a representative for a member.
// This is an output-only contract, not an accepted input or evidence artifact.
type GroupsReport struct {
	Schema string         `json:"schema"`
	Scope  string         `json:"scope"`
	Cases  []Report       `json:"cases"`
	Groups []FindingGroup `json:"groups"`
}

type FindingReference struct {
	CaseIdentity string `json:"case_identity"`
	FindingID    string `json:"finding_id"`
}

type OccurrenceReference struct {
	CaseIdentity string `json:"case_identity"`
	Occurrence   string `json:"occurrence"`
}

type FindingGroup struct {
	Signature       string                `json:"signature"`
	RuleID          string                `json:"rule_id"`
	Members         []FindingReference    `json:"members"`
	Representatives []FindingReference    `json:"representatives"`
	Occurrences     []OccurrenceReference `json:"occurrences"`
}

type fieldState struct {
	Field string `json:"field"`
	State string `json:"state"`
}

type findingSignature struct {
	Config         string       `json:"config"`
	Rule           string       `json:"rule"`
	Profile        string       `json:"profile"`
	Ruleset        string       `json:"ruleset"`
	Classification string       `json:"classification"`
	Summary        string       `json:"summary"`
	Evidence       []fieldState `json:"evidence"`
}

type groupAccumulator struct {
	index               int
	representativeCases map[string]bool
	occurrences         map[OccurrenceReference]bool
}

// GroupCases re-evaluates verified evidence, not untrusted saved diagnosis text.
// Equal signatures mean the same diagnostic shape, never the same root cause.
// Cancellation is checked between bounded case evaluations and while grouping.
func GroupCases(ctx context.Context, paths []string, config Config) (GroupsReport, error) {
	if len(paths) == 0 || len(paths) > MaxGroupCases {
		return GroupsReport{}, errors.New("diagnosis grouping requires 1 to 16 cases")
	}
	result := GroupsReport{Schema: GroupsSchema, Scope: "Counts describe findings and distinct case-local occurrences in these selected captures only, never population-wide rates. Equal signatures describe diagnostic shape, not a shared root cause. Representatives are the first finding in each case for that signature; every finding and unsupported item remains in Cases. Windows can overlap, and identical occurrences in different cases are not independent events.", Cases: []Report{}, Groups: []FindingGroup{}}
	seen := map[string]bool{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return GroupsReport{}, err
		}
		report, err := Run(path, config)
		if err != nil {
			return GroupsReport{}, err
		}
		if seen[report.CaseIdentity] {
			return GroupsReport{}, errors.New("diagnosis grouping refuses duplicate case identities")
		}
		seen[report.CaseIdentity] = true
		result.Cases = append(result.Cases, report)
	}
	slices.SortFunc(result.Cases, func(a, b Report) int { return cmp.Compare(a.CaseIdentity, b.CaseIdentity) })
	groups := map[string]*groupAccumulator{}
	for _, report := range result.Cases {
		for _, finding := range report.Findings {
			if err := ctx.Err(); err != nil {
				return GroupsReport{}, err
			}
			shape := findingSignature{Config: report.ConfigSHA256, Rule: finding.RuleID, Profile: finding.Profile, Ruleset: finding.Ruleset, Classification: finding.Classification, Summary: finding.Summary, Evidence: []fieldState{}}
			for _, e := range finding.Evidence {
				shape.Evidence = append(shape.Evidence, fieldState{Field: e.Field, State: string(e.State)})
			}
			encoded, err := json.Marshal(shape, json.Deterministic(true))
			if err != nil {
				return GroupsReport{}, errors.New("cannot encode diagnosis signature")
			}
			digest := sha256.Sum256(encoded)
			signature := hex.EncodeToString(digest[:])
			accumulator, ok := groups[signature]
			if !ok {
				accumulator = &groupAccumulator{index: len(result.Groups), representativeCases: map[string]bool{}, occurrences: map[OccurrenceReference]bool{}}
				groups[signature] = accumulator
				result.Groups = append(result.Groups, FindingGroup{Signature: signature, RuleID: finding.RuleID, Members: []FindingReference{}, Representatives: []FindingReference{}, Occurrences: []OccurrenceReference{}})
			}
			group := &result.Groups[accumulator.index]
			ref := FindingReference{CaseIdentity: report.CaseIdentity, FindingID: finding.ID}
			group.Members = append(group.Members, ref)
			if !accumulator.representativeCases[report.CaseIdentity] {
				group.Representatives = append(group.Representatives, ref)
				accumulator.representativeCases[report.CaseIdentity] = true
			}
			for _, e := range finding.Evidence {
				occ := OccurrenceReference{CaseIdentity: report.CaseIdentity, Occurrence: e.Occurrence}
				if !accumulator.occurrences[occ] {
					group.Occurrences = append(group.Occurrences, occ)
					accumulator.occurrences[occ] = true
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return GroupsReport{}, err
	}
	return result, nil
}

func GroupsJSON(report GroupsReport) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("grouped diagnosis exceeds 64 MiB; select fewer cases")
	}
	return append(data, '\n'), nil
}

// GroupsMarkdown puts representatives together for comparison, then includes
// complete diagnoses so no member, original byte span or unsupported item hides.
func GroupsMarkdown(report GroupsReport) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# Recurring diagnosis groups\n\n%s\n\nSelected cases: %d. Groups: %d.\n", report.Scope, len(report.Cases), len(report.Groups))
	findings := map[FindingReference]Finding{}
	windows := map[string]string{}
	for _, c := range report.Cases {
		windows[c.CaseIdentity] = c.Window.Description
		for _, f := range c.Findings {
			findings[FindingReference{CaseIdentity: c.CaseIdentity, FindingID: f.ID}] = f
		}
	}
	for _, g := range report.Groups {
		fmt.Fprintf(&out, "\n## %s\n\nSignature: `%s`\n\nCaptured findings: %d; affected case-local occurrences: %d.\n\n### Representative comparison\n", g.RuleID, g.Signature, len(g.Members), len(g.Occurrences))
		for _, ref := range g.Representatives {
			f := findings[ref]
			window := f.Window
			if window == "" {
				window = windows[ref.CaseIdentity]
			}
			fmt.Fprintf(&out, "\nCase `%s`, finding %s; %s; profile %s; ruleset %s.\n\n%s\n\nWindow: %s\n", ref.CaseIdentity, ref.FindingID, f.Classification, f.Profile, f.Ruleset, f.Summary, window)
			for _, e := range f.Evidence {
				fmt.Fprintf(&out, "- %s %s: %s", e.Occurrence, e.Field, e.State)
				if e.Offset != nil && e.Length != nil {
					fmt.Fprintf(&out, " (payload byte offset %d, length %d)", *e.Offset, *e.Length)
				}
				fmt.Fprintln(&out)
			}
		}
		fmt.Fprint(&out, "\n### All members\n\n")
		for _, ref := range g.Members {
			fmt.Fprintf(&out, "- Case `%s`, finding %s\n", ref.CaseIdentity, ref.FindingID)
		}
		fmt.Fprint(&out, "\n### All affected occurrences\n\n")
		for _, ref := range g.Occurrences {
			fmt.Fprintf(&out, "- Case `%s`, occurrence %s\n", ref.CaseIdentity, ref.Occurrence)
		}
	}
	fmt.Fprint(&out, "\n# Complete case diagnoses and unsupported coverage\n\n")
	for _, c := range report.Cases {
		out.Write(Markdown(c))
		fmt.Fprintln(&out)
	}
	return out.Bytes()
}
