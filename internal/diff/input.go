package diff

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runresult"
)

type occurrence struct {
	ref   Reference
	doc   *hl7.Document
	index int
}

type evidence struct {
	summary     InputSummary
	items       []*occurrence
	source      *bundle.Bundle
	run         *replay.Run
	unsupported []Unsupported
}

// errDurableRun is how diff refuses a durable run directory, which this report
// does not compare: the sentence it always gave, because such a directory
// holds no manifest at its root.
var errDurableRun = errors.New("cannot read diff artifact manifest")

func open(input Input, boundary Boundary) (*evidence, error) {
	if input.Opened == nil {
		info, err := os.Stat(input.Path)
		if err != nil {
			return nil, errors.New("cannot inspect diff input")
		}
		if info.Mode().IsRegular() {
			return openFile(input, boundary)
		}
		if !info.IsDir() {
			return nil, errors.New("diff input must be a regular file or artifact directory")
		}
	}
	if input.Format != "" && input.Format != "auto" || input.Terminator != "" && input.Terminator != "auto" {
		return nil, errors.New("artifact inputs use their recorded parsing declarations")
	}
	if input.Opened != nil {
		return fromEvidence(input.Opened, boundary)
	}
	// runresult names the directory by its resolved children, but each
	// verified reader enforces its own root symlink contract against the
	// original path. A durable run is refused before it is opened.
	family, err := runresult.Name(input.Path, "diff")
	if err != nil {
		return nil, err
	}
	if family == runresult.JobFamily {
		return nil, errDurableRun
	}
	opened, err := runresult.OpenAs(input.Path, family)
	if err != nil {
		return nil, err
	}
	return fromEvidence(opened, boundary)
}

// fromEvidence reads the payloads one opened case, run or result retained.
func fromEvidence(opened *runresult.Evidence, boundary Boundary) (*evidence, error) {
	targetIdentity := ""
	if opened.Target != nil {
		targetIdentity = opened.Target.Identity
	}
	switch opened.Family {
	case runresult.CaseFamily:
		return fromCase(opened.Case, boundary)
	case runresult.RunFamily:
		e, err := fromRun(opened.Run, boundary)
		if err != nil {
			return nil, err
		}
		e.summary.TargetIdentity = targetIdentity
		return e, nil
	case runresult.ResultFamily:
		artifact := opened.Artifact
		if artifact.Run == nil {
			return &evidence{summary: InputSummary{Kind: "result", Identity: artifact.Identity, ResultStatus: string(artifact.Result.Status), ResultBoundary: artifact.Result.ObservationBoundary, Payloads: "no run evidence"}, unsupported: []Unsupported{{Code: "result_without_run"}}}, nil
		}
		e, err := fromRun(artifact.Run, boundary)
		if err != nil {
			return nil, err
		}
		e.summary.Kind, e.summary.Identity = "result", artifact.Identity
		e.summary.ResultStatus = string(artifact.Result.Status)
		e.summary.ResultBoundary = artifact.Result.ObservationBoundary
		e.summary.TargetIdentity = targetIdentity
		return e, nil
	}
	return nil, errDurableRun
}

func openFile(input Input, boundary Boundary) (*evidence, error) {
	data, err := readFile(input.Path, hl7.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	e := &evidence{summary: InputSummary{Kind: "file", Identity: digest(data), Payloads: "provided HL7 message payloads"}}
	doc, parseErr := hl7.Parse(data, hl7.Options{Format: input.Format, Terminator: input.Terminator})
	if parseErr != nil {
		e.items = []*occurrence{{ref: Reference{Occurrence: "m000001", Kind: "unparsed", PayloadState: "unparsed"}}}
	} else {
		for i := range doc.Messages {
			item := &occurrence{ref: Reference{Occurrence: fmt.Sprintf("m%06d", i+1), PayloadState: "complete"}, doc: doc, index: i}
			item.ref.Kind = kind(item)
			if boundary == ACKs && item.ref.Kind != "ack" {
				e.summary.Excluded++
				continue
			}
			e.items = append(e.items, item)
		}
	}
	e.summary.Occurrences = len(e.items)
	return e, nil
}

func fromCase(b *bundle.Bundle, boundary Boundary) (*evidence, error) {
	e := &evidence{source: b, summary: InputSummary{Kind: "case", Identity: b.Identity, Payloads: "stored source messages"}}
	if boundary == ACKs {
		e.summary.Payloads = "stored source ACKs"
	}
	for _, event := range b.Events {
		if event.Kind != bundle.Unparsed && (boundary == Messages && event.Kind == bundle.Acknowledgement || boundary == ACKs && event.Kind == bundle.Message) {
			e.summary.Excluded++
			continue
		}
		item := &occurrence{ref: Reference{Occurrence: event.ID, Kind: string(event.Kind), PayloadState: "complete"}}
		if event.Kind == bundle.Unparsed {
			item.ref.PayloadState = "unparsed"
		} else {
			doc, err := b.Document(event)
			if err != nil {
				return nil, err
			}
			item.doc = doc
		}
		e.items = append(e.items, item)
	}
	e.summary.Occurrences = len(e.items)
	return e, nil
}

func fromRun(r *replay.Run, boundary Boundary) (*evidence, error) {
	e := &evidence{run: r, summary: InputSummary{Kind: "run", Identity: r.Identity, SourceIdentity: r.Manifest.SourceBundleIdentity, Payloads: "actual sent message bytes"}}
	if boundary == ACKs {
		e.summary.Payloads = "actual received ACK bytes"
	}
	for _, event := range r.Events {
		ref := event.Sent
		kind := "message"
		if boundary == ACKs {
			ref, kind = event.Received, "ack"
		}
		raw, err := r.Raw(ref)
		if err != nil {
			return nil, err
		}
		item := &occurrence{ref: Reference{Occurrence: event.OutboundOccurrence, SourceOccurrence: event.SourceOccurrence, Kind: kind, PayloadState: "complete", Outcome: string(event.Outcome), Delivery: event.Delivery}}
		switch {
		case len(raw) == 0:
			item.ref.PayloadState = "no_payload"
		case boundary == Messages && event.Sent.Size != event.Intended.Size:
			item.ref.PayloadState = "partial_sent"
		default:
			// Run payloads are MLLP-framed wire bytes, not verified case
			// occurrences: a payload that does not parse is recorded as
			// unparsed rather than refused.
			item.doc, err = hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
			if err != nil || len(item.doc.Messages) != 1 {
				item.ref.PayloadState, item.doc = "unparsed", nil
			}
		}
		e.items = append(e.items, item)
	}
	e.summary.Occurrences = len(e.items)
	return e, nil
}

func kind(item *occurrence) string {
	m := item.doc.Messages[item.index]
	messageType := item.doc.Bytes(m.Segments[0].Field(9).Span)
	if bytes.Equal(bytes.SplitN(messageType, []byte{m.Delimiters.Component}, 2)[0], []byte("ACK")) {
		return "ack"
	}
	for _, segment := range m.Segments {
		if segment.ID == "MSA" {
			return "ack"
		}
	}
	return "message"
}

func readFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("diff input must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open diff input file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("diff input must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("cannot read diff input within size limit")
	}
	return data, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
