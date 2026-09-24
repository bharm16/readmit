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
const MaxGroupsReportBytes = 64 << 20

// GroupsReport is a derived presentation. Cases retain the complete unchanged
// diagnoses; grouping never substitutes a representative for a member.
// This is an output-only evaluation contract: a saved copy can be reopened for
// display, but cannot be used as an input diagnosis or evidence artifact.
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
	report, _, err := GroupCasesWithIdentities(ctx, paths, config)
	return report, err
}

// GroupCasesWithIdentities also returns the verified identities in input order.
// A caller that knows the workspace entry for each input can label a live
// grouping without opening the case a second time. These identities are never
// members of the readmit-diagnosis-groups/v1 document beyond its Cases.
func GroupCasesWithIdentities(ctx context.Context, paths []string, config Config) (GroupsReport, []string, error) {
	if len(paths) == 0 || len(paths) > MaxGroupCases {
		return GroupsReport{}, nil, errors.New("diagnosis grouping requires 1 to 16 cases")
	}
	result := GroupsReport{Schema: GroupsSchema, Scope: "Counts describe findings and distinct case-local occurrences in these selected captures only, never population-wide rates. Equal signatures describe diagnostic shape, not a shared root cause. Representatives are the first finding in each case for that signature; every finding and unsupported item remains in Cases. Windows can overlap, and identical occurrences in different cases are not independent events.", Cases: []Report{}, Groups: []FindingGroup{}}
	identities := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return GroupsReport{}, nil, err
		}
		report, err := Run(path, config)
		if err != nil {
			return GroupsReport{}, nil, err
		}
		if seen[report.CaseIdentity] {
			return GroupsReport{}, nil, errors.New("diagnosis grouping refuses duplicate case identities")
		}
		seen[report.CaseIdentity] = true
		identities = append(identities, report.CaseIdentity)
		result.Cases = append(result.Cases, report)
	}
	slices.SortFunc(result.Cases, func(a, b Report) int { return cmp.Compare(a.CaseIdentity, b.CaseIdentity) })
	groups := map[string]*groupAccumulator{}
	for _, report := range result.Cases {
		for _, finding := range report.Findings {
			if err := ctx.Err(); err != nil {
				return GroupsReport{}, nil, err
			}
			shape := findingSignature{Config: report.ConfigSHA256, Rule: finding.RuleID, Profile: finding.Profile, Ruleset: finding.Ruleset, Classification: finding.Classification, Summary: finding.Summary, Evidence: []fieldState{}}
			for _, e := range finding.Evidence {
				shape.Evidence = append(shape.Evidence, fieldState{Field: e.Field, State: string(e.State)})
			}
			encoded, err := json.Marshal(shape, json.Deterministic(true))
			if err != nil {
				return GroupsReport{}, nil, errors.New("cannot encode diagnosis signature")
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
		return GroupsReport{}, nil, err
	}
	return result, identities, nil
}

func GroupsJSON(report GroupsReport) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxGroupsReportBytes {
		return nil, errors.New("grouped diagnosis exceeds 64 MiB; select fewer cases")
	}
	return append(data, '\n'), nil
}

// ParseGroups reads a retained grouping for display. It does not make a
// grouping eligible for finding review: that operation still reads only one
// readmit-diagnosis/v1 report through its separate reader.
func ParseGroups(data []byte) (GroupsReport, error) {
	// GroupsJSON checks the encoded payload before appending one newline.
	if len(data) > MaxGroupsReportBytes+1 {
		return GroupsReport{}, errors.New("grouped diagnosis exceeds 64 MiB; select fewer cases")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return GroupsReport{}, errors.New("invalid diagnosis grouping report")
	}
	if declared.Schema != GroupsSchema {
		return GroupsReport{}, errors.New("diagnosis grouping report declares a contract version this release does not read")
	}
	var report GroupsReport
	if json.Unmarshal(data, &report, json.RejectUnknownMembers(true)) != nil ||
		len(report.Cases) == 0 || len(report.Cases) > MaxGroupCases || report.Groups == nil || report.Scope == "" {
		return GroupsReport{}, errors.New("invalid diagnosis grouping report")
	}
	findings := make(map[FindingReference]bool)
	cases := make(map[string]bool)
	for _, c := range report.Cases {
		if c.Schema != Schema || c.CaseIdentity == "" || cases[c.CaseIdentity] {
			return GroupsReport{}, errors.New("invalid diagnosis grouping report")
		}
		cases[c.CaseIdentity] = true
		for _, finding := range c.Findings {
			ref := FindingReference{CaseIdentity: c.CaseIdentity, FindingID: finding.ID}
			if finding.ID == "" || findings[ref] {
				return GroupsReport{}, errors.New("invalid diagnosis grouping report")
			}
			findings[ref] = true
		}
	}
	for _, group := range report.Groups {
		if group.Signature == "" || group.RuleID == "" || len(group.Members) == 0 ||
			group.Representatives == nil || group.Occurrences == nil {
			return GroupsReport{}, errors.New("invalid diagnosis grouping report")
		}
		members := make(map[FindingReference]bool)
		for _, ref := range group.Members {
			if !findings[ref] || members[ref] {
				return GroupsReport{}, errors.New("invalid diagnosis grouping report")
			}
			members[ref] = true
		}
		for _, ref := range group.Representatives {
			if !members[ref] {
				return GroupsReport{}, errors.New("invalid diagnosis grouping report")
			}
		}
		for _, ref := range group.Occurrences {
			if !cases[ref.CaseIdentity] || ref.Occurrence == "" {
				return GroupsReport{}, errors.New("invalid diagnosis grouping report")
			}
		}
	}
	return report, nil
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
