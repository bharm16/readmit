package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/secret"
)

// A named observation is one saved object: its source and the window it is
// collected through and, when its source presents one, the links member
// that names the project credential it presents. Every member is published
// in one revision, or none is. A refusal of one member of the source or the
// window is answered at that member, so an editor shows it beside the field
// that holds it.

// ObservationLinksSchema is the contract of an observation's links member.
const ObservationLinksSchema = "readmit-observation-links/v1"

// ObservationLinks is what an observation names beside its source: the
// project credential reference its HTTPS or database source presents.
type ObservationLinks struct {
	Schema     string `json:"schema"`
	Credential string `json:"credential"`
}

// fieldProblem is the problem a reader's refusal is, at the member it names
// under prefix, or at prefix itself when it names none.
func fieldProblem(prefix string, err error) FieldProblem {
	var at *observewindow.FieldError
	if errors.As(err, &at) {
		return FieldProblem{Field: prefix + "." + at.Field, Problem: at.Err.Error()}
	}
	return FieldProblem{Field: prefix, Problem: err.Error()}
}

// validateObservationDraft validates a whole observation draft: the named
// credential it presents resolved into its source, the source and the window
// through the readers a collection reads them with, the database adapter
// against the ones this release has, and the two together.
func validateObservationDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *ObservationDraft, []FieldProblem) {
	problems := []FieldProblem{}
	observation := *draft.Observation
	source := observation.Source
	if observation.Credential != "" {
		resolved, problem := scope.presentCredential(source, observation.Credential)
		if problem != nil {
			problems = append(problems, *problem)
		} else {
			source = resolved
		}
	}
	if problem := scope.distinctSource(&source, &observation.Window); problem != nil {
		problems = append(problems, *problem)
	}
	if source.Database != nil {
		if support := supportFor(source); support.Qualification == "unknown" {
			problems = append(problems, FieldProblem{Field: "observation.source.database.driver", Problem: source.Database.Driver + " is not available in this release"})
		}
	}
	encoded, err := observesource.EncodeSource(source)
	if err == nil {
		_, err = observesource.DecodeSource(encoded)
	}
	if err != nil && len(problems) == 0 {
		problems = append(problems, fieldProblem("observation.source", err))
	}
	window := observation.Window
	if window.Schema == "" {
		window.Schema = observewindow.WindowSchema
	}
	windowData, windowErr := observewindow.EncodeWindow(window)
	if windowErr == nil {
		_, windowErr = observewindow.DecodeWindow(windowData)
	}
	if windowErr != nil {
		problems = append(problems, fieldProblem("observation.window", windowErr))
	}
	if err == nil && windowErr == nil && source.Observes != window.Source {
		problems = append(problems, FieldProblem{Field: "observation.window.source", Problem: operation.ErrObservationPairMismatch.Error()})
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	staged := []catalog.Staged{{Role: "source", File: "source.json", Data: encoded}, {Role: "window", File: "window.json", Data: windowData}}
	if observation.Credential != "" {
		links, err := encodeMember(ObservationLinks{Schema: ObservationLinksSchema, Credential: observation.Credential})
		if err != nil {
			return nil, nil, []FieldProblem{{Field: "observation.credential", Problem: err.Error()}}
		}
		staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: links})
	}
	return staged, &ObservationDraft{Source: source, Window: window, Credential: observation.Credential}, problems
}

// distinctSource keeps each observation's collections its own. Collections
// and a reset's empty check find an observation by the kind, identity and
// scope it reads, so two observations never read the same three. A new
// observation that would is given an identity derived from its save's intent,
// the same for every retry of that save; a saved one is refused at its scope.
func (s draftScope) distinctSource(source *observesource.Source, window *observewindow.Window) *FieldProblem {
	if s.loaded == nil {
		return nil
	}
	taken := map[observewindow.Source]string{}
	for _, item := range s.loaded.document.Items {
		// The item this very submission published, on an earlier try, is
		// the one being saved again.
		ours := s.intent != "" && slices.ContainsFunc(item.Revisions, func(revision catalog.Revision) bool { return revision.Intent == s.intent })
		if item.Kind != string(ObservationItem) || item.ID == s.item || ours || s.loaded.removed(item) {
			continue
		}
		if other, err := s.loaded.observationOf(item); err == nil {
			taken[other.Source.Observes] = cmp.Or(item.Name, "another observation")
		}
	}
	name, held := taken[source.Observes]
	if !held {
		return nil
	}
	if s.item == "" && s.intent != "" {
		digest := sha256.Sum256([]byte(s.intent))
		source.Observes.Identity = "observation-" + hex.EncodeToString(digest[:6])
		window.Source.Identity = source.Observes.Identity
		if _, again := taken[source.Observes]; !again {
			return nil
		}
	}
	return &FieldProblem{Field: "observation.source.source.scope", Problem: name + " already reads this source and scope; choose another scope"}
}

// presentCredential resolves the project credential an observation names
// into the reference its source declares: registered for a source endpoint
// and scoped to exactly the endpoint the source reads. An HTTPS source keeps
// the header it presents the credential in.
func (s draftScope) presentCredential(source observesource.Source, name string) (observesource.Source, *FieldProblem) {
	problem := func(text string) (observesource.Source, *FieldProblem) {
		return source, &FieldProblem{Field: "observation.credential", Problem: text}
	}
	endpoint := ""
	switch {
	case source.HTTP != nil:
		address, err := source.HTTP.Endpoint()
		if err != nil {
			return source, &FieldProblem{Field: "observation.source.http.url", Problem: err.Error()}
		}
		endpoint = address
	case source.Database != nil:
		endpoint = source.Database.Address
	default:
		return problem("only an HTTPS or database source presents a credential")
	}
	if s.root == "" {
		return problem("the project registers no credentials")
	}
	document, err := operation.ReadSecrets(filepath.Join(s.root, ProjectSecrets))
	if err != nil {
		return problem("the project registers no credentials")
	}
	reference, err := secret.Find(document, name)
	switch {
	case err != nil:
		return problem(err.Error())
	case reference.Purpose != secret.SourceEndpoint:
		return problem("that credential is registered for another purpose; an observation presents a source endpoint credential")
	case reference.Address != endpoint:
		return problem("that credential is scoped to " + reference.Address + ", not the endpoint this source reads")
	}
	resolved := source
	if source.HTTP != nil {
		http := *source.HTTP
		header := ""
		if http.Credential != nil {
			header = http.Credential.Header
		}
		http.Credential = &observesource.Credential{Store: reference.Store, Address: reference.Address, Header: header,
			Command: reference.Command, Arguments: slices.Clone(reference.Arguments)}
		resolved.HTTP = &http
	} else {
		database := *source.Database
		database.Credential = observesource.DatabaseCredential{Store: reference.Store, Address: reference.Address, Purpose: "database-observation",
			Command: reference.Command, Arguments: slices.Clone(reference.Arguments)}
		resolved.Database = &database
	}
	return resolved, nil
}

// decodeObservationLinks reads one observation links member exactly as
// written.
func decodeObservationLinks(data []byte) (ObservationLinks, error) {
	var links ObservationLinks
	if len(data) > maxLinksBytes || json.Unmarshal(data, &links, json.RejectUnknownMembers(true)) != nil || links.Schema != ObservationLinksSchema ||
		links.Credential == "" {
		return ObservationLinks{}, errors.New("the observation's links cannot be read")
	}
	return links, nil
}

func readObservationLinks(path string) (ObservationLinks, error) {
	data, err := boundedFile(path, maxLinksBytes)
	if err != nil {
		return ObservationLinks{}, err
	}
	return decodeObservationLinks(data)
}

// verifyObservation reads a staged observation revision through the readers
// a collection reads each of its documents with, together.
func verifyObservation(files map[string]string) error {
	if _, _, err := operation.ValidateObservationPair(files["source"], files["window"]); err != nil {
		return err
	}
	if path, held := files["links"]; held {
		if _, err := readObservationLinks(path); err != nil {
			return err
		}
	}
	return nil
}

// withheldArguments is a draft of a source whose credential a named project
// reference supplies: the locator's arguments stay in the project's
// secrets entry and never reach the window; a save resolves them again.
func withheldArguments(draft *ObservationDraft) {
	switch {
	case draft.Source.HTTP != nil && draft.Source.HTTP.Credential != nil:
		http := *draft.Source.HTTP
		credential := *http.Credential
		credential.Arguments = []string{}
		http.Credential = &credential
		draft.Source.HTTP = &http
	case draft.Source.Database != nil:
		database := *draft.Source.Database
		database.Credential.Arguments = []string{}
		draft.Source.Database = &database
	}
}

// ObservationFieldsRequest names the source an editor holds, saved or not,
// whose export's fields a record key can be chosen from.
type ObservationFieldsRequest struct {
	Context RequestContext       `json:"context"`
	Source  observesource.Source `json:"source"`
}

// ObservationFieldsResult lists the fields one export offers a record key,
// in the export's order, or says why it cannot.
type ObservationFieldsResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Fields  []string       `json:"fields"`
}

func (r *ObservationFieldsResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ObservationFields reads the export a file-export source names, a path
// relative to the project or one chosen on this machine, within the source's
// read bound, and lists the fields its declared extraction offers a record
// key: each column of a CSV header or each member of the first JSON record
// that holds a string or a number. It is a deliberate local read: nothing is
// collected, nothing is reached over a network and nothing is written.
func (a *App) ObservationFields(request ObservationFieldsRequest) ObservationFieldsResult {
	return run(a, false, false, func(ctx context.Context) ObservationFieldsResult {
		result := ObservationFieldsResult{Context: request.Context, Fields: []string{}}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		source := request.Source
		if source.File == nil || source.Extraction == nil || source.File.Path == "" {
			result.refuse(Failed, "only a file export with a chosen input file lists its fields")
			return result
		}
		path := source.File.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		limit := min(max(source.File.MaxBytes, 1), observesource.MaxReadBytes)
		data, err := operation.ReadInputFile(path, limit)
		if err != nil {
			result.refuse(Failed, "the input file cannot be read within its maximum size")
			return result
		}
		fields, err := source.Extraction.Shape().Fields(data)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		for _, field := range fields {
			result.Fields = append(result.Fields, field[0])
		}
		result.State = Completed
		if len(result.Fields) == 0 {
			result.State = Empty
		}
		return result
	})
}
