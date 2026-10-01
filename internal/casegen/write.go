package casegen

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// Written is one installed generation: its record bytes and each case's
// entry and bundle identity, in record order.
type Written struct {
	Record []byte
	Cases  []Case
}

// Placement is where a generation is installed in its folder: the record's
// entry, each case's entry, and the cases the folder already holds by content.
type Placement struct {
	Record string
	Entry  func(Case) string
	// Held is each case bundle the folder already holds, by the key CaseKey
	// gives its content. A case with a held key is not written again: the
	// record names the case held, so one content is one case.
	Held     map[string]Held
	Progress func(Progress)
}

// Held is one case bundle a folder holds: its entry and bundle identity.
type Held struct {
	Entry    string
	Identity string
}

// CaseKey names the content of case i of the record: the provenance its
// bundle records and its messages in order. Two cases with one key are one
// case bundle, whatever else their records say.
func (r Record) CaseKey(i int) string {
	type content struct {
		Seed      uint64
		BaseTime  string
		Generator string
		Profile   string
		Messages  []string
	}
	c := content{Seed: r.Ancestry.Seed, BaseTime: r.Ancestry.BaseTime, Generator: r.GeneratorVersion, Profile: r.Ancestry.Profile.ID + "+" + r.Ancestry.Profile.Version}
	for _, o := range r.Cases[i].Occurrences {
		c.Messages = append(c.Messages, o.SHA256)
	}
	return digest(mustCanonical(c))
}

// Write installs the generation into dir, an existing folder: each case as a
// new case bundle at the entry the placement gives it, unless the folder
// already holds that content, and then the record, installed last. Each case
// holds one source per occurrence, in arrival order, so each message is one
// payload of its bundle. Every entry written must be new; nothing existing is
// overwritten or resumed. A cancellation or failure retains what was written,
// without a record, and a retry writes new entries beside it. An entry names a
// case by its row and variant, never by anything the case's bytes depend on.
func (g *Generation) Write(ctx context.Context, dir string, placement Placement) (Written, error) {
	if err := ctx.Err(); err != nil {
		return Written{}, err
	}
	record := placement.Record
	if !validEntry(record) {
		return Written{}, errors.New("the generation record is named by one new entry of the folder")
	}
	r := g.Record
	r.Cases = append([]Case(nil), r.Cases...)
	held := map[string]Held{}
	for key, h := range placement.Held {
		held[key] = h
	}
	base := g.scenario.BaseTime.UTC()
	messages := 0
	for i := range r.Cases {
		if err := ctx.Err(); err != nil {
			return Written{}, err
		}
		key := r.CaseKey(i)
		if h, ok := held[key]; ok {
			r.Cases[i].Entry, r.Cases[i].Identity = h.Entry, h.Identity
		} else {
			name := placement.Entry(r.Cases[i])
			if !validEntry(name) || name == record {
				return Written{}, errors.New("each case is named by one new entry of the folder")
			}
			inputs := make([]bundle.Input, 0, len(g.messages[i]))
			for _, body := range g.messages[i] {
				inputs = append(inputs, bundle.Input{Data: body, Options: hl7.Options{Format: hl7.Raw, Terminator: hl7.CR}})
			}
			written, err := bundle.WriteContext(ctx, filepath.Join(dir, name), inputs, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{
				Seed: r.Request.Seed, BaseTime: base, GeneratorVersion: Version, ProfileVersion: g.profileID}})
			if err != nil {
				return Written{}, err
			}
			r.Cases[i].Entry, r.Cases[i].Identity = name, written.Identity
			held[key] = Held{Entry: name, Identity: written.Identity}
		}
		messages += len(g.messages[i])
		if placement.Progress != nil {
			placement.Progress(Progress{Stage: Writing, Cases: len(r.Cases), Done: i + 1, Messages: messages})
		}
	}
	encoded, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return Written{}, errors.New("cannot encode the generation record")
	}
	encoded = append(encoded, '\n')
	if err := ctx.Err(); err != nil {
		return Written{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Written{}, errors.New("cannot open the generation folder; written cases are retained")
	}
	defer root.Close()
	document := artifactdir.Document{MaxBytes: MaxRecordBytes, CreateByLink: true, Staging: artifactdir.StagingName("." + record + ".incomplete"), Errors: artifactdir.DocumentErrors{
		Create:  errors.New("cannot write the generation record; written cases are retained"),
		Write:   errors.New("cannot write the generation record; written cases are retained"),
		Install: errors.New("cannot finish the generation record; written cases are retained"),
		Sync:    errors.New("cannot sync the generation folder; a power loss could still lose it"),
	}}
	if err := document.CreateIn(root, record, encoded); err != nil {
		return Written{}, err
	}
	return Written{Record: encoded, Cases: r.Cases}, nil
}

// WriteDirectory installs the generation into the new directory path names:
// each case as ROW-VARIANT and the record as generation.json, installed last.
// The directory must not exist and its parent must.
func (g *Generation) WriteDirectory(ctx context.Context, path string) (Written, error) {
	path, err := artifactpath.Destination(path)
	if err != nil {
		return Written{}, err
	}
	if err := ctx.Err(); err != nil {
		return Written{}, err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return Written{}, errors.New("the generation destination must be new with an existing writable parent")
	}
	written, err := g.Write(ctx, path, Placement{Record: "generation.json", Entry: func(c Case) string { return c.Row + "-" + c.Variant }})
	if err != nil {
		return Written{}, err
	}
	// The record anchors each member in the generation folder; the parent
	// must also anchor the newly created generation folder before success.
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Written{}, errors.New("cannot sync the generation's parent folder; a power loss could still lose it")
	}
	defer parent.Close()
	if err := artifactdir.SyncDirectory(parent, "."); err != nil {
		return Written{}, errors.New("cannot sync the generation's parent folder; a power loss could still lose it")
	}
	return written, nil
}

// validEntry is one plain entry name of a folder.
func validEntry(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && len(name) <= 128 && name[0] != '.'
}

// MaxRecordBytes bounds a generation record a reader reads.
const MaxRecordBytes = 64 << 20

// ReadRecord reads a generation record exactly as written.
func ReadRecord(data []byte) (Record, error) {
	var r Record
	if len(data) > MaxRecordBytes {
		return Record{}, errors.New("a generation record exceeds 64 MiB")
	}
	if err := json.Unmarshal(data, &r, json.RejectUnknownMembers(true)); err != nil || r.Schema != RecordSchema || r.State != "complete" {
		return Record{}, errors.New("not a complete " + RecordSchema + " record")
	}
	return r, nil
}
