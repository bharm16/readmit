package desktop

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// ConnectionExampleSchema is an importable connection example: the named
// objects one supported test topology needs — environments with their
// approved destinations and reset, a capture listener, observations with
// their typed projection, identifier mapping and completion — each written in
// the member contracts its editor saves, with every customer-specific value
// an explicitly declared placeholder. Importing one asks for each
// placeholder's value and saves each object through the same validation its
// editor's Save uses.
const ConnectionExampleSchema = "readmit-connection-example/v1"

// The topologies a connection example declares.
var exampleTopologies = []string{"v2-engine-v2-capture", "v2-application-fhir", "fhir-native-downstream"}

// placeholderToken is how an example marks a value the administrator
// supplies: angle brackets around an upper-case name, such as <ENGINE_HOST>.
var placeholderToken = regexp.MustCompile(`<[A-Z][A-Z0-9_]{0,62}>`)

var exampleObjectID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

var exampleNumber = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`)

// maxExampleBytes bounds one example document.
const maxExampleBytes = 256 << 10

// PlaceholderKind is what an example's placeholder stands for.
type PlaceholderKind string

const (
	// TextPlaceholder replaces the placeholder inside a string value.
	TextPlaceholder PlaceholderKind = "text"
	// FilePlaceholder is a whole string value naming a file on this computer,
	// chosen with the file dialog.
	FilePlaceholder PlaceholderKind = "file"
	// NumberPlaceholder is a whole value, written as the placeholder's token
	// in a string and imported as a whole number, such as a port.
	NumberPlaceholder PlaceholderKind = "number"
	// CasePlaceholder is a whole string value naming a case of the open
	// project, such as the capture a listener retained.
	CasePlaceholder PlaceholderKind = "case"
)

var placeholderKinds = []PlaceholderKind{TextPlaceholder, FilePlaceholder, NumberPlaceholder, CasePlaceholder}

// ConnectionExample is the strict envelope of an example file. Objects are
// read as ExampleObject once each number placeholder stands in as a number.
type ConnectionExample struct {
	Schema       string               `json:"schema"`
	Topology     string               `json:"topology"`
	Name         string               `json:"name"`
	Placeholders []ExamplePlaceholder `json:"placeholders"`
	Objects      jsontext.Value       `json:"objects"`
}

// ExamplePlaceholder declares one value the administrator supplies: its
// token, the short label the import asks with, and its kind.
type ExamplePlaceholder struct {
	Token string          `json:"token"`
	Label string          `json:"label"`
	Kind  PlaceholderKind `json:"kind"`
}

// ExampleObject is one named object the example saves: an environment, an
// observation or a capture source, by an identity local to the example. An
// observation names the environment it reads by that local identity, and an
// environment its observation.
type ExampleObject struct {
	ID    string       `json:"id"`
	Kind  ItemKind     `json:"kind"`
	Draft ExampleDraft `json:"draft"`
}

// ExampleDraft is the example's own frozen shape of an object: exactly the
// members an environment, observation or capture listener carries, each in
// its own published contract. It never follows the window's draft type.
type ExampleDraft struct {
	Name        string              `json:"name"`
	Environment *replay.Target      `json:"environment,omitzero"`
	FHIR        *FHIRConnection     `json:"fhir,omitzero"`
	SendPolicy  *sendpolicy.Policy  `json:"policy,omitzero"`
	ResetPlan   *fixturereset.Plan  `json:"reset,omitzero"`
	Links       *EnvironmentLinks   `json:"links,omitzero"`
	Observation *ExampleObservation `json:"observation,omitzero"`
	Source      *ExampleListener    `json:"source,omitzero"`
}

// ExampleObservation is an observation's source, window and connected setup.
// A FHIR observation declares only its connected setup.
type ExampleObservation struct {
	Source    *observesource.Source `json:"source,omitzero"`
	Window    *observewindow.Window `json:"window,omitzero"`
	Connected *ConnectedObservation `json:"connected"`
}

// ExampleListener is a capture source that is an MLLP listener.
type ExampleListener struct {
	Type     CaptureSourceType `json:"type"`
	Listener *ListenerSettings `json:"listener"`
}

// itemDraft is the window's draft of the object, which its editor's Save
// validates.
func (d ExampleDraft) itemDraft() ItemDraft {
	draft := ItemDraft{Name: d.Name, Environment: d.Environment, FHIR: d.FHIR, SendPolicy: d.SendPolicy, ResetPlan: d.ResetPlan, Links: d.Links}
	if d.Observation != nil {
		observation := &ObservationDraft{Connected: d.Observation.Connected}
		if d.Observation.Source != nil {
			observation.Source = *d.Observation.Source
		}
		if d.Observation.Window != nil {
			observation.Window = *d.Observation.Window
		}
		draft.Observation = observation
	}
	if d.Source != nil {
		draft.Source = &CaptureSourceDraft{Type: d.Source.Type, Listener: d.Source.Listener}
	}
	return draft
}

// ConnectionExampleRequest names a chosen example file by its full path.
// Without Import it reads what the example declares. With Import it imports
// it with the values given for its placeholders, by token; IntentID
// identifies one import submission and is reused for its retries.
type ConnectionExampleRequest struct {
	Context  RequestContext    `json:"context"`
	Path     string            `json:"path"`
	Import   bool              `json:"import"`
	Values   map[string]string `json:"values"`
	IntentID string            `json:"intent_id,omitzero"`
}

// ExampleObjectSummary names one object an example saves.
type ExampleObjectSummary struct {
	ID   string   `json:"id"`
	Kind ItemKind `json:"kind"`
	Name string   `json:"name"`
}

// ConnectionExampleSummary is what an example declares, for the import sheet.
type ConnectionExampleSummary struct {
	Name         string                 `json:"name"`
	Topology     string                 `json:"topology"`
	Placeholders []ExamplePlaceholder   `json:"placeholders"`
	Objects      []ExampleObjectSummary `json:"objects"`
}

// ConnectionExampleResult answers one read or import. Saved lists every
// object published, in the example's order; a refusal part way names its
// problems and keeps what was saved before it.
type ConnectionExampleResult struct {
	State    State                     `json:"state"`
	Reason   string                    `json:"reason,omitzero"`
	Context  RequestContext            `json:"context"`
	Example  *ConnectionExampleSummary `json:"example,omitzero"`
	Saved    []ItemRef                 `json:"saved"`
	Problems []FieldProblem            `json:"problems"`
}

func (r *ConnectionExampleResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ImportConnectionExample reads a connection example the person chose and,
// when asked to import it, saves the example's objects in order — capture
// sources, environments, observations naming the environments just saved,
// then each environment's link to its observation — each through the
// validation its editor's Save uses. A missing or unusable value saves
// nothing and is answered at its field. It contacts nothing, resolves no
// credential and installs nothing.
func (a *App) ImportConnectionExample(request ConnectionExampleRequest) ConnectionExampleResult {
	return run(a, false, false, func(ctx context.Context) ConnectionExampleResult {
		result := ConnectionExampleResult{Context: request.Context, Saved: []ItemRef{}, Problems: []FieldProblem{}}
		if !filepath.IsAbs(request.Path) {
			result.refuse(Failed, "choose the example with the file dialog")
			return result
		}
		if request.Import && (request.IntentID == "" || len(request.IntentID) > 64) {
			result.refuse(Failed, "an import names its submission")
			return result
		}
		raw, err := readChosenFile(request.Path, maxExampleBytes)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				result.refuse(PermissionDenied, "this account cannot read the chosen example")
				return result
			}
			result.refuse(Failed, "the chosen example cannot be read")
			return result
		}
		example, objects, err := decodeConnectionExample(raw)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		summary := &ConnectionExampleSummary{Name: example.Name, Topology: example.Topology, Placeholders: example.Placeholders, Objects: []ExampleObjectSummary{}}
		for _, object := range objects {
			summary.Objects = append(summary.Objects, ExampleObjectSummary{ID: object.ID, Kind: object.Kind, Name: object.Draft.Name})
		}
		result.Example = summary
		if !request.Import {
			result.State = Completed
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		values := map[string]string{}
		for _, placeholder := range example.Placeholders {
			value := request.Values[placeholder.Token]
			field := "values." + placeholder.Token
			switch {
			case placeholder.Kind == CasePlaceholder:
				entry := ""
				if index := loaded.document.Find(value); value != "" && index >= 0 && loaded.document.Items[index].Kind == string(CaseItem) && !loaded.removed(loaded.document.Items[index]) {
					entry = loaded.document.Items[index].Entry
				}
				if entry == "" {
					result.Problems = append(result.Problems, FieldProblem{Field: field, Problem: "Choose a case of this project"})
					continue
				}
				values[placeholder.Token] = filepath.ToSlash(entry)
			case placeholder.Kind == NumberPlaceholder && !exampleNumber.MatchString(value):
				result.Problems = append(result.Problems, FieldProblem{Field: field, Problem: "Enter " + strings.ToLower(placeholder.Label) + " as a whole number"})
			case placeholder.Kind == FilePlaceholder && (!exampleValue(value) || !filepath.IsAbs(value)):
				result.Problems = append(result.Problems, FieldProblem{Field: field, Problem: "Choose " + strings.ToLower(placeholder.Label)})
			case !exampleValue(value):
				result.Problems = append(result.Problems, FieldProblem{Field: field, Problem: "Enter " + strings.ToLower(placeholder.Label)})
			default:
				values[placeholder.Token] = value
			}
		}
		if len(result.Problems) > 0 {
			result.refuse(Failed, "enter every value the example needs; nothing was saved")
			return result
		}
		objects, err = substituteExample(raw, values, example.Placeholders)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		a.saveExample(ctx, request.Context, request.IntentID, objects, &result)
		return result
	})
}

// saveExample publishes the example's objects in dependency order and
// records each in result, stopping at the first refusal.
func (a *App) saveExample(ctx context.Context, context RequestContext, intent string, objects []ExampleObject, result *ConnectionExampleResult) {
	saved := map[string]ItemRef{}
	order := map[ItemKind]int{SourceItem: 0, EnvironmentItem: 1, ObservationItem: 2}
	sorted := slices.Clone(objects)
	slices.SortStableFunc(sorted, func(x, y ExampleObject) int { return order[x.Kind] - order[y.Kind] })
	save := func(object ExampleObject, draft ItemDraft, phase string, base *ItemRef) bool {
		request := SaveItemRequest{Context: context, Kind: object.Kind, Draft: draft, IntentID: intent + ":" + object.ID + phase}
		if base != nil {
			request.Item, request.BaseRevision = base.ID, base.Revision
		}
		answer := a.saveItem(ctx, request)
		if answer.Outcome != SavedOutcome || answer.Saved == nil {
			for _, problem := range answer.Problems {
				result.Problems = append(result.Problems, FieldProblem{Field: object.ID + "." + problem.Field, Problem: problem.Problem})
			}
			reason := answer.Reason
			if reason == "" {
				reason = "the save did not complete"
			}
			result.refuse(Failed, object.Draft.Name+": "+reason)
			return false
		}
		saved[object.ID] = *answer.Saved
		return true
	}
	linked := []ExampleObject{}
	for _, object := range sorted {
		draft := object.Draft.itemDraft()
		switch object.Kind {
		case EnvironmentItem:
			if draft.Links != nil && draft.Links.Observation != "" {
				linked = append(linked, object)
				links := *draft.Links
				links.Observation = ""
				draft.Links = &links
			}
		case ObservationItem:
			if connected := draft.Observation.Connected; connected != nil && connected.Environment != "" {
				environment := *connected
				environment.Environment = saved[connected.Environment].ID
				draft.Observation.Connected = &environment
			}
		}
		if !save(object, draft, "", nil) {
			return
		}
		result.Saved = append(result.Saved, saved[object.ID])
	}
	for _, object := range linked {
		opened := a.openSavedDraft(ctx, context, saved[object.ID])
		if opened == nil {
			result.refuse(Failed, object.Draft.Name+": the saved environment could not be read again to link its observation")
			return
		}
		links := EnvironmentLinks{Schema: EnvironmentLinksSchema}
		if opened.Links != nil {
			links = *opened.Links
		}
		links.Observation = saved[object.Draft.Links.Observation].ID
		opened.Links = &links
		base := saved[object.ID]
		if !save(object, *opened, ":link", &base) {
			return
		}
		for i, ref := range result.Saved {
			if ref.ID == base.ID {
				result.Saved[i] = saved[object.ID]
			}
		}
	}
	result.State = Completed
}

// openSavedDraft reads a just-saved environment's current draft inside the
// slot the import holds.
func (a *App) openSavedDraft(ctx context.Context, context RequestContext, ref ItemRef) *ItemDraft {
	loaded, declined := a.loadCatalog(ctx, context, false)
	if loaded == nil || declined.state != "" {
		return nil
	}
	index := loaded.document.Find(ref.ID)
	if index < 0 {
		return nil
	}
	record := loaded.document.Items[index]
	draft := ItemDraft{Name: record.Name}
	if loaded.environmentDraft(record, &draft) != nil {
		return nil
	}
	return &draft
}

// exampleValue reports a value an administrator may give a placeholder: one
// line of 1 to 1024 bytes that is not itself a placeholder.
func exampleValue(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 1024 || strings.ContainsAny(value, "<>") {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

// decodeConnectionExample reads an example strictly: its declared topology,
// placeholders and objects, each object holding only the members its kind
// saves, every local reference resolving, and every placeholder token in its
// values declared and every declared one used.
func decodeConnectionExample(raw []byte) (*ConnectionExample, []ExampleObject, error) {
	var example ConnectionExample
	if err := json.Unmarshal(raw, &example, json.RejectUnknownMembers(true)); err != nil {
		return nil, nil, errors.New("the chosen file is not a connection example this release reads")
	}
	if example.Schema != ConnectionExampleSchema {
		return nil, nil, errors.New("the chosen file is not a " + ConnectionExampleSchema + " connection example")
	}
	invalid := func(reason string) (*ConnectionExample, []ExampleObject, error) {
		return nil, nil, errors.New("the connection example is invalid: " + reason)
	}
	if !slices.Contains(exampleTopologies, example.Topology) {
		return invalid("it declares no supported topology")
	}
	if strings.TrimSpace(example.Name) == "" || len(example.Name) > 200 {
		return invalid("it has no name")
	}
	declared := map[string]bool{}
	if len(example.Placeholders) == 0 || len(example.Placeholders) > 32 {
		return invalid("it declares between 1 and 32 placeholders")
	}
	for _, p := range example.Placeholders {
		if placeholderToken.FindString(p.Token) != p.Token || declared[p.Token] || strings.TrimSpace(p.Label) == "" || len(p.Label) > 60 || !slices.Contains(placeholderKinds, p.Kind) {
			return invalid("placeholder " + p.Token + " is not one marked value with a label and a kind")
		}
		declared[p.Token] = true
	}
	// The objects are read as the import will save them, each number
	// placeholder standing in as a number, so their structure is checked
	// before any value is asked for.
	stand := map[string]string{}
	for _, p := range example.Placeholders {
		stand[p.Token] = p.Token
		if p.Kind == NumberPlaceholder {
			stand[p.Token] = "0"
		}
	}
	objects, err := substituteExample(raw, stand, example.Placeholders)
	if err != nil {
		return invalid("its objects are not ones this release saves")
	}
	if len(objects) == 0 || len(objects) > 8 {
		return invalid("it holds between 1 and 8 objects")
	}
	kinds := map[string]ItemKind{}
	for _, object := range objects {
		if !exampleObjectID.MatchString(object.ID) || kinds[object.ID] != "" {
			return invalid("object " + object.ID + " has no distinct identity")
		}
		kinds[object.ID] = object.Kind
		if strings.TrimSpace(object.Draft.Name) == "" {
			return invalid("object " + object.ID + " has no name")
		}
	}
	for _, object := range objects {
		d := object.Draft
		environment := d.Environment != nil || d.FHIR != nil || d.SendPolicy != nil || d.ResetPlan != nil || d.Links != nil
		switch object.Kind {
		case EnvironmentItem:
			if (d.Environment == nil) == (d.FHIR == nil) || d.Observation != nil || d.Source != nil {
				return invalid("environment " + object.ID + " declares one v2 or FHIR connection and nothing else")
			}
			if d.Links != nil && d.Links.Observation != "" && kinds[d.Links.Observation] != ObservationItem {
				return invalid("environment " + object.ID + " links an observation the example does not hold")
			}
		case ObservationItem:
			if d.Observation == nil || d.Observation.Connected == nil || environment || d.Source != nil {
				return invalid("observation " + object.ID + " declares one connected observation and nothing else")
			}
			if c := d.Observation.Connected; c.Environment != "" && kinds[c.Environment] != EnvironmentItem {
				return invalid("observation " + object.ID + " reads an environment the example does not hold")
			}
		case SourceItem:
			if d.Source == nil || d.Source.Type != MLLPListenerSource || d.Source.Listener == nil || environment || d.Observation != nil {
				return invalid("source " + object.ID + " declares one capture listener and nothing else")
			}
		default:
			return invalid("object " + object.ID + " is not an environment, observation or capture source")
		}
	}
	used := map[string]bool{}
	for _, token := range placeholderToken.FindAllString(string(raw), -1) {
		used[token] = true
	}
	for token := range used {
		if !declared[token] {
			return invalid("placeholder " + token + " is not declared")
		}
	}
	for _, p := range example.Placeholders {
		if !used[p.Token] {
			return invalid("placeholder " + p.Token + " is declared and never used")
		}
		if p.Kind != TextPlaceholder && strings.Count(string(raw), p.Token) != strings.Count(string(raw), `"`+p.Token+`"`) {
			return invalid("placeholder " + p.Token + " is a whole value wherever it is used")
		}
	}
	return &example, objects, nil
}

// substituteExample replaces each placeholder inside the example's string
// values with its value and decodes the objects again strictly, so a value
// is only ever a string where the example put its placeholder, and a number
// placeholder only ever a whole number.
func substituteExample(raw []byte, values map[string]string, placeholders []ExamplePlaceholder) ([]ExampleObject, error) {
	numbers := map[string]bool{}
	for _, p := range placeholders {
		numbers[p.Token] = p.Kind == NumberPlaceholder
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, errors.New("the chosen file is not a connection example this release reads")
	}
	var walk func(any) any
	walk = func(value any) any {
		switch v := value.(type) {
		case string:
			if numbers[v] {
				number, _ := strconv.Atoi(values[v])
				return number
			}
			return placeholderToken.ReplaceAllStringFunc(v, func(token string) string { return values[token] })
		case []any:
			for i := range v {
				v[i] = walk(v[i])
			}
		case map[string]any:
			for key, member := range v {
				v[key] = walk(member)
			}
		}
		return value
	}
	document, _ := tree.(map[string]any)
	if document == nil {
		return nil, errors.New("the chosen file is not a connection example this release reads")
	}
	encoded, err := json.Marshal(walk(document["objects"]))
	if err != nil {
		return nil, errors.New("the example's values cannot be applied")
	}
	var objects []ExampleObject
	if json.Unmarshal(encoded, &objects, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("the example's values cannot be applied; check each value is the kind its label asks for")
	}
	return objects, nil
}
