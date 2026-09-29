package desktop

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/testrunner"
)

// A run origin records the publication selected by the reviewed action,
// beside the project's catalog and outside immutable engine evidence. It is
// written before execution and binds its references to the retained input.
// Equal specs in different publications do not establish the same origin.
type runOriginRecord struct {
	Schema          string   `json:"schema"`
	Entry           string   `json:"entry"`
	Source          ItemRef  `json:"source"`
	Name            string   `json:"name"`
	Environment     *ItemRef `json:"environment,omitzero"`
	EnvironmentName string   `json:"environment_name,omitzero"`
	InputDigest     string   `json:"input_digest"`
}

const runOriginSchema = "readmit-run-origin/v1"

func runOriginPath(root, entry string) string {
	return filepath.Join(root, ".readmit", "run-origins", keyDigest(entry)+".json")
}

func specDigest(spec testrunner.Spec) string {
	data, err := json.Marshal(spec, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return digestOf(data)
}

func recordRunOrigin(run *runBinding, review *RunReview) error {
	if review.Test == nil && run.kind != SuiteRunKind {
		return nil
	}
	record := runOriginRecord{Schema: runOriginSchema, Entry: run.output, Name: review.Name,
		Environment: review.Environment, EnvironmentName: review.EnvironmentName}
	if run.kind == SuiteRunKind {
		record.Source = run.source
		record.InputDigest = digestOf(run.suiteData)
	} else {
		record.Source = *review.Test
		record.InputDigest = run.specDigest
	}
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return err
	}
	path := runOriginPath(run.root, run.output)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return shellDocument.Create(path, data)
}

func (c *loadedCatalog) recordedRunOrigin(path, inputDigest string) (runOriginRecord, bool, error) {
	entry := c.entryOf(path)
	originPath := runOriginPath(c.root, entry)
	var record runOriginRecord
	if _, err := os.Lstat(originPath); errors.Is(err, fs.ErrNotExist) {
		return record, false, nil
	}
	data, err := boundedFile(originPath, 16<<10)
	if err != nil || json.Unmarshal(data, &record, json.RejectUnknownMembers(true)) != nil ||
		record.Schema != runOriginSchema || record.Entry != entry || record.InputDigest != inputDigest {
		return runOriginRecord{}, false, errors.New("the selected historical publication could not be verified")
	}
	index := c.document.Find(record.Source.ID)
	if index < 0 || c.document.Items[index].Kind != string(record.Source.Kind) {
		return runOriginRecord{}, false, errors.New("the selected historical publication is no longer in the project")
	}
	return record, true, nil
}

func (c *loadedCatalog) originOfRun(path string, spec testrunner.Spec) runOrigin {
	record, held, err := c.recordedRunOrigin(path, specDigest(spec))
	if err != nil {
		return runOrigin{reason: err.Error()}
	}
	if held && record.Source.Kind == TestItem {
		return runOrigin{test: &record.Source, environment: record.Environment, environmentName: record.EnvironmentName}
	}
	if held {
		return runOrigin{reason: "the selected historical publication has a different kind"}
	}
	return c.originOf(spec)
}
