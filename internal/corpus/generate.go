// Package corpus writes the declared performance corpus a scan is measured
// against, and the two documents that make such a measurement mean something:
// the generator inputs the corpus was written from, and the benchmark a run of
// it produced.
//
// The corpus is generated, never committed. It depends only on its declared
// inputs — a seed, a base time, a generator version and a profile version, the
// same four inputs synth declares — so the same declarations reproduce the same
// bytes and the recorded digest says whether they did. It is streamed rather
// than assembled: one message is formatted at a time into a reused buffer, so
// writing a corpus of a million messages costs what one message costs.
//
// Nothing here is security-sensitive randomness. The PCG stream of
// math/rand/v2 is a declared generator input, kept separate from crypto/rand,
// exactly as docs/stack.md separates them.
package corpus

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/synth"
)

// The two contract versions this package owns. A new member of either is a new
// version string with a reader for every older one, never an added member and
// never an in-place migration.
const (
	ManifestSchema  = "readmit-corpus/v1"
	BenchmarkSchema = "readmit-benchmark/v1"
)

const (
	// GeneratorVersion names this corpus generator. The PCG stream, the draw
	// order and the message shape belong to it: changing any of them requires a
	// new implemented version, not a different label.
	GeneratorVersion = "readmit-corpus-v1"
	// ProfileVersion is the one named fixture profile docs/stack.md declares,
	// the same profile synth generates against. It is a readmit-supported
	// profile, not a claim of HL7 v2.5.1 conformance.
	ProfileVersion = synth.ProfileVersion
)

// MaxMessages bounds one generated corpus, and MaxManifestBytes bounds the
// documents this package reads. The message bound leaves the million-message
// envelope proposed in #25 inside it; a corpus past either bound is refused,
// never truncated. The bytes a corpus may occupy are importer.MaxStreamBytes,
// so a corpus this package writes is one a scan can read back.
const (
	MaxMessages      = 1 << 20
	MaxManifestBytes = 64 << 10
)

// ErrCancelled reports a generation the caller cancelled. A cancelled
// generation writes no manifest, and Write removes the partial corpus it
// created, so a cancellation never leaves a corpus nothing describes.
var ErrCancelled = errors.New("corpus generation cancelled")

// Inputs are the complete declarations one corpus is written from. Generator
// carries the four provenance inputs synth declares; Messages says how many
// messages to write; Plan says how they are framed, which is the same
// readmit-import-plan/v1 a scan of the corpus is declared under. Every member
// is required: nothing here has a likely value.
type Inputs struct {
	Generator bundle.GeneratorInputs `json:"generator"`
	Messages  int                    `json:"messages"`
	Plan      importer.Plan          `json:"plan"`
}

// Validate reports the first reason a corpus cannot be written.
func (i Inputs) Validate() error {
	if i.Generator.GeneratorVersion != GeneratorVersion {
		return errors.New("unsupported corpus generator version; supported: " + GeneratorVersion)
	}
	if i.Generator.ProfileVersion != ProfileVersion {
		return errors.New("unsupported corpus profile version; supported: " + ProfileVersion)
	}
	// The message count is bounded before the base time, because the base-time
	// check spans the corpus and an unbounded count would overflow that span
	// and be reported as a fault in the time instead of in the count.
	if i.Messages < 1 || i.Messages > MaxMessages {
		return errors.New("a corpus holds between 1 and " + strconv.Itoa(MaxMessages) + " messages")
	}
	base := i.Generator.BaseTime.UTC()
	if base.IsZero() || base.Year() < 1 || base.Nanosecond() != 0 || base.Add(time.Duration(i.Messages)*time.Minute+24*time.Hour+30*time.Minute).Year() > 9999 {
		return errors.New("base time must be a nonzero whole second with the complete corpus within years 0001 through 9999")
	}
	if err := i.Plan.Validate(); err != nil {
		return err
	}
	if len(i.Plan.Members) != 0 {
		return errors.New("a corpus is one stream; declared members select the entries of a folder or archive import")
	}
	switch i.Plan.Framing {
	case importer.MLLPFraming:
	case importer.BatchFraming:
		if i.Plan.BatchBoundary != importer.SegmentStart {
			return errors.New("a batch corpus is written at the segment-start boundary")
		}
	default:
		return errors.New("a corpus is written with mllp or batch framing; raw framing declares one message per stream")
	}
	if i.Plan.Terminator != hl7.CR {
		return errors.New("a corpus is written with a cr segment terminator")
	}
	if i.Plan.Encoding != importer.USASCII && i.Plan.Encoding != importer.UTF8 {
		return errors.New("a corpus is written in us-ascii or utf-8")
	}
	return nil
}

// Progress is what a running generation reports: counts only, so a progress
// report carries no name, no declaration and no byte of the corpus.
type Progress struct {
	Messages int64
	Bytes    int64
}

// Summary is what one generation wrote. The digest is over the corpus bytes, so
// regenerating from the same inputs and comparing it is the whole of what
// "reproducible" means here.
type Summary struct {
	Messages int64
	Bytes    int64
	SHA256   string
}

// Generate streams one corpus to the writer and returns what it wrote. One
// message is formatted at a time into a reused buffer, so the resident cost is
// one message and the writer's own buffer whatever the message count is.
//
// A cancelled context stops between messages and returns ErrCancelled. Bytes
// already handed to the writer are already written: a cancellation stops the
// generation, it does not retract them, which is why Write owns removing the
// partial file it created rather than this function pretending it can.
func Generate(ctx context.Context, destination io.Writer, inputs Inputs, report func(Progress)) (Summary, error) {
	if err := inputs.Validate(); err != nil {
		return Summary{}, err
	}
	digest := sha256.New()
	out := bufio.NewWriterSize(io.MultiWriter(destination, digest), writeChunkBytes)
	// The PCG stream and the draw order belong to readmit-corpus-v1. Changing
	// either requires a new implemented generator version, not a new label.
	random := rand.New(rand.NewPCG(inputs.Generator.Seed, 0x726561646d697463))
	base := inputs.Generator.BaseTime.UTC()
	framed := inputs.Plan.Framing == importer.MLLPFraming
	summary := Summary{}
	message := make([]byte, 0, 512)
	for ordinal := 1; ordinal <= inputs.Messages; ordinal++ {
		if err := ctx.Err(); err != nil {
			out.Flush()
			return summary, ErrCancelled
		}
		message = appendMessage(message[:0], ordinal, base, random, framed)
		if _, err := out.Write(message); err != nil {
			return summary, errors.New("cannot write the corpus")
		}
		summary.Messages++
		summary.Bytes += int64(len(message))
		if summary.Bytes > importer.MaxStreamBytes {
			return summary, errors.New("a corpus exceeds the stream limit one scan reads")
		}
		if report != nil && ordinal%reportEvery == 0 {
			report(Progress{Messages: summary.Messages, Bytes: summary.Bytes})
		}
	}
	if err := out.Flush(); err != nil {
		return summary, errors.New("cannot write the corpus")
	}
	summary.SHA256 = hex.EncodeToString(digest.Sum(nil))
	if report != nil {
		report(Progress{Messages: summary.Messages, Bytes: summary.Bytes})
	}
	return summary, nil
}

const (
	// writeChunkBytes is one buffered write to the corpus destination.
	writeChunkBytes = 64 << 10
	// reportEvery bounds how often a generation reports progress, so a million
	// messages produce a bounded number of reports rather than a million.
	reportEvery = 4096
)

// appendMessage formats one SIU message of the declared fixture profile. Every
// value is drawn from the declared generator stream: nothing reads the clock,
// the environment or any file, so the same inputs produce the same bytes.
//
// Exactly four draws are taken per message, in this order, and the trigger is
// the first of them reduced over the trigger list. Four plain draws is what
// makes the stream reproducible by hand from the PCG algorithm alone, the way
// docs/synth-v1-vector.md reproduces synth's; a rejection-sampled range would
// make the draw count depend on the values.
//
// The positions are those of the readmit-siu-v1 fixture profile, not a claim to
// implement every SIU field or to validate general HL7 conformance.
func appendMessage(out []byte, ordinal int, base time.Time, random *rand.Rand, framed bool) []byte {
	choice := random.Uint64()
	patient := random.Uint64()
	placer := random.Uint64()
	filler := random.Uint64()
	trigger := triggers[choice%uint64(len(triggers))]
	declared := base.Add(time.Duration(ordinal) * time.Minute)
	appointment := declared.Add(24 * time.Hour)
	if framed {
		out = append(out, 0x0b)
	}
	out = fmt.Appendf(out, "MSH|^~\\&|READMIT|CORPUS|RECEIVER|READMIT|%s||SIU^%s|CORPUS-%08d|T|2.5.1\r"+
		"SCH|PLACER-%016X^READMIT|FILLER-%016X^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^%s^%s\r"+
		"PID|1||CORPUS-%016X^^^READMIT^MR||SYNTHETIC^PATIENT\r",
		hl7Time(declared), trigger, ordinal, placer, filler,
		hl7Time(appointment), hl7Time(appointment.Add(30*time.Minute)), patient)
	if framed {
		out = append(out, 0x1c, '\r')
	}
	return out
}

// triggers are the SIU trigger events one corpus message is drawn from.
var triggers = []string{"S12", "S13", "S14", "S15"}

func hl7Time(value time.Time) string { return value.UTC().Format("20060102150405-0700") }
