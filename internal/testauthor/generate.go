package testauthor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testrunner"
)

// maxEntries bounds the workspace scan that offers the targets of a workspace.
// It is the bound the shell lists a folder under, so a folder too large to list
// is too large to choose a target from, rather than being scanned anyway.
const maxEntries = 1024

// ErrCannotWrite is the one refusal a caller separates from the rest: the spec
// could not be created at all. Everything else Save refuses is a statement
// about the draft or the workspace and has the same remedy whoever is asking;
// this one may mean the account cannot write where it was pointed.
var ErrCannotWrite = errors.New("cannot write test spec; destination must be new and parent writable")

// Target is one configuration of a workspace a test can be pointed at, as the
// shared reader reports it. Classification is what the configuration records
// about the environment itself, so a person choosing a target sees it before
// they choose rather than when a run refuses. A file that declares the contract
// and that the reader refuses is listed with the reason, never hidden.
type Target struct {
	Name           string `json:"name"`
	Schema         string `json:"schema"`
	Environment    string `json:"environment,omitzero"`
	Classification string `json:"classification,omitzero"`
	Reason         string `json:"reason,omitzero"`
}

// Resolution is what a draft means over the evidence and the workspace: the
// stage to answer next, everything still unanswered, the initial state the
// chosen boundary fixes, the selected occurrences in the order the case records
// them, the targets this workspace offers, and what the expectations answered
// so far decide and leave undecided.
type Resolution struct {
	Stage    string   `json:"stage"`
	Missing  []string `json:"missing"`
	Setup    string   `json:"setup,omitzero"`
	Messages []string `json:"messages"`
	Targets  []Target `json:"targets"`
	Coverage Coverage `json:"coverage"`
}

// Saved is the spec that was written: the entry of the workspace holding it and
// the identity `readmit test` records for those exact bytes.
type Saved struct {
	Output   string `json:"output"`
	Identity string `json:"identity"`
}

// Setup reports the initial state a boundary fixes, and nothing for a boundary
// that has not been answered. The two words are readmit-test/v1's own.
func Setup(boundary string) string {
	switch boundary {
	case testrunner.LedgerBoundary:
		return testrunner.EmptyLedger
	case testrunner.ACKBoundary:
		return testrunner.OperatorDeclared
	}
	return ""
}

// Stages is every stage of the flow, in the order it asks them. The order is
// not a preference: a boundary decides whether an observation source is read at
// all and which expectations a test can make.
func Stages() []string {
	names := make([]string, 0, len(stageRules))
	for _, rule := range stageRules {
		names = append(names, rule.name)
	}
	return names
}

// Missing reports every stage this draft has not answered, in the order the
// flow asks them. A stage that does not apply to the chosen boundary is not
// missing: the ACK boundary reads no observation document.
func Missing(draft Draft) []string {
	missing := make([]string, 0, len(stageRules))
	for _, rule := range stageRules {
		if !rule.answered(draft) {
			missing = append(missing, rule.name)
		}
	}
	return missing
}

// Resolve reports what one draft means over the verified case and the open
// workspace. It reads; it writes nothing and generates nothing.
//
// The evidence is checked again here rather than trusted from the answer that
// named it: every selected occurrence must be a message this case holds, and a
// target that has been chosen must be one the shared reader accepts and one
// this product will replay to at all.
func Resolve(root string, source *bundle.Bundle, draft Draft) (Resolution, error) {
	if err := check(draft); err != nil {
		return Resolution{}, err
	}
	if source == nil || draft.Case.Identity != source.Identity {
		return Resolution{}, errors.New("this draft was authored against different evidence; reopen the case before answering it")
	}
	selected := make(map[string]bool, len(draft.Messages))
	for _, id := range draft.Messages {
		selected[id] = true
	}
	// The occurrences are reported in the order the case records them, which is
	// the order a run sends them in, rather than the order they were clicked.
	ordered := make([]string, 0, len(draft.Messages))
	for _, event := range source.Events {
		if !selected[event.ID] {
			continue
		}
		if event.Kind != bundle.Message {
			return Resolution{}, errors.New("a test sends messages; an acknowledgement and an occurrence nothing decoded cannot be sent")
		}
		delete(selected, event.ID)
		ordered = append(ordered, event.ID)
	}
	if len(selected) != 0 {
		return Resolution{}, errors.New("a selected occurrence is not in this case")
	}
	targets, err := Targets(root)
	if err != nil {
		return Resolution{}, err
	}
	if err := chosen(targets, draft.Target); err != nil {
		return Resolution{}, err
	}
	missing := Missing(draft)
	stage := ""
	if len(missing) > 0 {
		stage = missing[0]
	}
	return Resolution{Stage: stage, Missing: missing, Setup: Setup(draft.Boundary), Messages: ordered, Targets: targets, Coverage: Cover(draft)}, nil
}

// chosen holds an answered target to what this product will actually replay to.
// A production classification refuses every send, in replay's Prepare, before a
// plan exists; refusing it here means a person is told while they are choosing
// rather than after they have saved a test that can never run.
func chosen(targets []Target, name string) error {
	if name == "" {
		return nil
	}
	at := slices.IndexFunc(targets, func(t Target) bool { return t.Name == name })
	if at < 0 {
		return errors.New("a target is one entry of the open workspace declaring a target configuration this release reads")
	}
	if targets[at].Reason != "" {
		return errors.New("that target configuration is not one this release reads")
	}
	if sendpolicy.RefusesEverySend(targets[at].Classification) {
		return errors.New("that configuration records the production classification; readmit does not replay to a production-classified environment")
	}
	return nil
}

// Targets reports the target configurations one workspace folder offers, read
// through the same reader `readmit test` resolves a spec's target with. It
// opens no connection and reads no credential.
func Targets(root string) ([]Target, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("the workspace folder cannot be read")
	}
	if len(entries) > maxEntries {
		return nil, errors.New("the folder holds more entries than this release lists")
	}
	targets := make([]Target, 0)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || artifactpath.EntryName(entry.Name()) != nil {
			continue
		}
		declared, ok := declaredTarget(root, entry)
		if !ok {
			continue
		}
		targets = append(targets, declared)
	}
	return targets, nil
}

// declaredTarget reports what one file declares about itself. A file that does
// not declare the target contract is not a target and is passed over; one that
// does and that the reader refuses is reported with the reason, so a folder
// never silently omits a configuration a person is looking for.
func declaredTarget(root string, entry fs.DirEntry) (Target, bool) {
	path := artifactpath.JoinReference(root, entry.Name())
	info, err := entry.Info()
	if err != nil || info.Size() > 64<<10 {
		return Target{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Target{}, false
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &header) != nil || !isTargetSchema(header.Schema) {
		return Target{}, false
	}
	configuration, err := replay.ReadDeclaredTarget(path)
	if err != nil {
		return Target{Name: entry.Name(), Schema: header.Schema, Reason: "not a target configuration this release reads"}, true
	}
	environment := configuration.Environment()
	return Target{Name: entry.Name(), Schema: configuration.Schema, Environment: environment.Name, Classification: string(environment.Classification)}, true
}

func isTargetSchema(schema string) bool {
	return schema == replay.TargetSchema || schema == replay.TargetSchemaV2 || schema == replay.TargetSchemaV3
}

// Generate writes the draft as readmit-test/v1 bytes.
//
// The bytes are handed back only after the reader that executes them has
// accepted them, so what this returns is a spec `readmit test` reads rather
// than a document that merely looks like one. A draft that has not answered
// every stage its boundary asks is refused by naming the stage.
func Generate(draft Draft) ([]byte, error) {
	if err := check(draft); err != nil {
		return nil, err
	}
	if missing := Missing(draft); len(missing) > 0 {
		return nil, unanswered(draft, missing[0])
	}
	assertions := make([]testrunner.Assertion, 0, len(draft.Expectations))
	for _, expectation := range draft.Expectations {
		assertion := testrunner.Assertion{ID: expectation.ID, Operator: expectation.Operator, Message: expectation.Message, Selector: expectation.Selector}
		switch expectation.Operator {
		case LedgerCount:
			count := *expectation.Count
			assertion.Expected = testrunner.Value{Count: &count}
		case LedgerEquals:
			records := slices.Clone(*expectation.Records)
			assertion.Expected = testrunner.Value{Records: &records}
		case ACKFieldEquals:
			field := *expectation.Field
			assertion.Expected = testrunner.Value{Field: &field}
		}
		assertions = append(assertions, assertion)
	}
	spec := testrunner.Spec{
		Schema:      testrunner.SpecSchema,
		Name:        draft.Name,
		Input:       testrunner.Input{Case: draft.Case.Entry, Messages: slices.Clone(draft.Messages)},
		Target:      draft.Target,
		Setup:       testrunner.Setup{InitialState: Setup(draft.Boundary), ResetInstructions: draft.Reset},
		Observation: testrunner.Observation{Boundary: draft.Boundary, Path: draft.Observation},
		Assertions:  assertions,
	}
	data, err := json.Marshal(spec, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("this draft could not be written as a test spec")
	}
	data = append(data, '\n')
	if _, err := testrunner.DecodeSpec(data); err != nil {
		return nil, errors.New("this draft does not generate a test spec this release reads")
	}
	return data, nil
}

// unanswered names the stage a draft still has to answer, in the words the
// stage table holds: a person reading this is being asked a question rather
// than shown the name of a member. The expectations stage is the one that is
// unanswered for two different reasons, so it says which one.
func unanswered(draft Draft, stage string) error {
	if stage == StageExpectations && len(draft.Expectations) > 0 {
		return errors.New("a test at the appointment-ledger boundary states what the ledger should hold; add an expected record count or an exact ledger")
	}
	for _, rule := range stageRules {
		if rule.name == stage {
			return errors.New(rule.unanswered)
		}
	}
	return errors.New("this test has not answered every question this flow asks")
}

// Save writes the generated spec into one new entry of the workspace folder.
//
// The draft is resolved against the evidence and the workspace first, so every
// refusal the flow makes while a test is being answered is made again before
// anything is written rather than only where a person was asked. The
// destination must not exist and must be outside every retained artifact, so a
// spec can never be written into the evidence it names, and the file is read
// back and decoded again afterwards: the identity this reports is the identity
// of the bytes that are actually on disk. A failed write leaves no partial file
// behind.
func Save(root string, source *bundle.Bundle, draft Draft, output string) (Saved, error) {
	if _, err := Resolve(root, source, draft); err != nil {
		return Saved{}, err
	}
	data, err := Generate(draft)
	if err != nil {
		return Saved{}, err
	}
	if artifactpath.EntryName(output) != nil || !printable(output, MaxEntryBytes) {
		return Saved{}, errors.New("a test spec is written to one new entry of the open workspace")
	}
	if _, err := artifactpath.Child(root, draft.Case.Entry); err != nil {
		return Saved{}, errors.New("the case this test sends is not one directory entry of the open workspace")
	}
	// The target is resolved exactly as a spec resolves it: relative to the
	// document, through the reader that binds what it declares.
	if _, err := replay.ReadTarget(artifactpath.JoinReference(root, draft.Target)); err != nil {
		return Saved{}, errors.New("the target this test names is not a configuration this release reads")
	}
	destination, err := artifactpath.Destination(artifactpath.JoinReference(root, output))
	if err != nil {
		return Saved{}, errors.New("a test spec must be written outside every retained artifact of this workspace")
	}
	if err := write(destination, data); err != nil {
		return Saved{}, err
	}
	written, err := os.ReadFile(destination)
	if err != nil || string(written) != string(data) {
		return Saved{}, errors.New("the test spec could not be read back from the workspace")
	}
	if _, err := testrunner.DecodeSpec(written); err != nil {
		return Saved{}, errors.New("the test spec written to the workspace is not one this release reads")
	}
	sum := sha256.Sum256(written)
	return Saved{Output: output, Identity: hex.EncodeToString(sum[:])}, nil
}

// write creates the spec exclusively, so an entry that already exists is
// refused rather than replaced, and removes what it started on any failure, so
// an interrupted write leaves no half-written spec to be read as a whole one.
// A spec holds expected values a person typed, which are the same customer-local
// literals the evidence holds, so it is owner-readable.
func write(destination string, data []byte) error {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrCannotWrite
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("the test spec could not be written into the workspace")
	}
	return nil
}
