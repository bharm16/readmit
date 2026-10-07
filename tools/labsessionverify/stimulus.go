package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/networkaction"
)

type stimulusProvenance struct {
	Source        string `json:"source"`
	SourceSHA256  string `json:"source_sha256"`
	SourceBase64  string `json:"source_base64"`
	DerivedSHA256 string `json:"derived_sha256"`
}

func (d *driver) stimulus(name string) ([]byte, error) {
	directory := filepath.Join(d.state, "session-evidence", d.session.Generation)
	var records []stimulusProvenance
	if err := readJSON(filepath.Join(directory, "source-provenance.json"), &records); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(directory, "stimuli", name))
	if err != nil {
		return nil, err
	}
	matches := 0
	for _, record := range records {
		if record.Source != "testdata/integration-lab/"+name {
			continue
		}
		matches++
		source, e := base64.StdEncoding.DecodeString(record.SourceBase64)
		if e != nil || networkaction.Digest(source) != record.SourceSHA256 || networkaction.Digest(raw) != record.DerivedSHA256 || !bytes.Equal(raw, bytes.ReplaceAll(source, []byte("__GENERATION__"), []byte(d.session.Generation))) || !bytes.Contains(raw, []byte("\rZLG|"+d.session.Generation+"\r")) {
			return nil, errors.New("stimulus provenance differs from active generation")
		}
	}
	if matches != 1 {
		return nil, errors.New("stimulus source provenance missing or ambiguous")
	}
	return raw, nil
}
