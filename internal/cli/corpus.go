package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/spf13/cobra"
)

// progressInterval bounds how often a running command reports progress on the
// diagnostic stream. A scan of a million records reports on a clock, not once
// per batch, so the diagnostics stay bounded however long the stream is.
const progressInterval = time.Second

// reporter throttles progress to the diagnostic stream. It writes counts only:
// no file name, no declaration and no byte of the stream ever reaches it, so
// there is nothing here for --show-values to hide.
type reporter struct {
	out     io.Writer
	enabled bool
	last    time.Time
}

func newReporter(out io.Writer, enabled bool) *reporter {
	return &reporter{out: out, enabled: enabled}
}

// line writes one progress line when enough time has passed since the last one.
// The first line is always written, so a run that ends quickly still reports
// once, and a run that does not end still reports at a bounded rate.
func (r *reporter) line(format string, values ...any) {
	if !r.enabled {
		return
	}
	now := time.Now()
	if !r.last.IsZero() && now.Sub(r.last) < progressInterval {
		return
	}
	r.last = now
	fmt.Fprintf(r.out, format, values...)
}

func corpusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "corpus",
		Annotations: declare(capabilityFree),
		Short:       "Generate and stream the declared performance corpus",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usage("corpus requires a subcommand: generate or scan")
		},
	}
	cmd.AddCommand(corpusGenerateCommand(), corpusScanCommand())
	return cmd
}

func corpusGenerateCommand() *cobra.Command {
	var seed uint64
	var messages int
	var baseTime, generatorVersion, profileVersion, output, manifest string
	var progress bool
	var declared declaration
	cmd := &cobra.Command{
		Use:         "generate --output NEW_FILE --manifest NEW_FILE --seed N --base-time INSTANT --generator-version readmit-corpus-v1 --profile-version readmit-siu-v1 --messages N --framing mllp --terminator cr --encoding us-ascii --direction inbound",
		Annotations: declareInterruptible(capabilityAuthor),
		Short:       "Write a reproducible performance corpus and the manifest that names its inputs",
		Args:        cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, flag := range []string{"seed", "base-time", "generator-version", "profile-version", "messages", "output", "manifest"} {
				if !cmd.Flags().Changed(flag) {
					return usage("corpus generate requires --seed, --base-time, --generator-version, --profile-version, --messages, --output, and --manifest")
				}
			}
			base, err := time.Parse(time.RFC3339, baseTime)
			if err != nil || !synthBaseTimePattern.MatchString(baseTime) {
				return usage("base time must be a whole-second RFC3339 timestamp with an explicit timezone")
			}
			plan, err := declared.plan(cmd, "")
			if err != nil {
				return err
			}
			inputs := corpus.Inputs{
				Generator: bundle.GeneratorInputs{Seed: seed, BaseTime: base, GeneratorVersion: generatorVersion, ProfileVersion: profileVersion},
				Messages:  messages,
				Plan:      plan,
			}
			ctx := cmd.Context()
			throttle := newReporter(cmd.ErrOrStderr(), progress)
			written, err := corpus.Write(ctx, output, manifest, inputs, func(p corpus.Progress) {
				throttle.line("generating: messages=%d bytes=%d\n", p.Messages, p.Bytes)
			})
			if err != nil {
				return err
			}
			return renderCorpus(cmd.OutOrStdout(), written)
		},
	}
	cmd.Flags().Uint64Var(&seed, "seed", 0, "Explicit deterministic seed, including zero")
	cmd.Flags().StringVar(&baseTime, "base-time", "", "Declared whole-second scenario time in RFC3339 (never inferred from the clock)")
	cmd.Flags().StringVar(&generatorVersion, "generator-version", "", "Implemented generator version: "+corpus.GeneratorVersion)
	cmd.Flags().StringVar(&profileVersion, "profile-version", "", "Implemented fixture profile: "+corpus.ProfileVersion)
	cmd.Flags().IntVar(&messages, "messages", 0, "Declared number of messages to write")
	cmd.Flags().StringVar(&output, "output", "", "New corpus file (never overwrite)")
	cmd.Flags().StringVar(&manifest, "manifest", "", "New readmit-corpus/v1 manifest file written after the corpus (never overwrite)")
	cmd.Flags().BoolVar(&progress, "progress", false, "Report bounded counts to standard error while the corpus is written")
	addDeclarationFlags(cmd, &declared, false)
	return cmd
}

func corpusScanCommand() *cobra.Command {
	var saved, report string
	var batchRecords, batchBytes, windowOffset, windowLimit int
	var progress bool
	var declared declaration
	cmd := &cobra.Command{
		Use:         "scan FILE --framing mllp --terminator cr --encoding us-ascii --direction inbound",
		Annotations: declareInterruptible(capabilityFree),
		Short:       "Stream one declared file in bounded parsing batches and report what it holds",
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := declared.plan(cmd, saved)
			if err != nil {
				return err
			}
			// The benchmark destination is checked before the stream is read,
			// so an unusable destination is reported in a moment rather than
			// after a long scan. Exclusive creation still owns the guarantee.
			if report != "" {
				reserved, err := artifactpath.Destination(report)
				if err != nil {
					return err
				}
				if _, err := os.Lstat(reserved); !os.IsNotExist(err) {
					return errors.New("the benchmark destination must be a new file")
				}
			}
			stream, err := corpus.Open(args[0])
			if err != nil {
				return err
			}
			defer stream.Close()
			ctx := cmd.Context()
			throttle := newReporter(cmd.ErrOrStderr(), progress)
			started := time.Now()
			result, scanErr := importer.Scan(ctx, stream, importer.ScanOptions{
				Plan:         plan,
				Window:       importer.Window{Offset: windowOffset, Limit: windowLimit},
				BatchRecords: batchRecords,
				BatchBytes:   batchBytes,
				Report: func(p importer.Progress) {
					throttle.line("scanning: bytes=%d records=%d occurrences=%d batches=%d\n", p.Bytes, p.Records, p.Occurrences, p.Batches)
				},
			})
			elapsed := time.Since(started)
			if scanErr != nil && !errors.Is(scanErr, importer.ErrScanCancelled) {
				return scanErr
			}
			bounds := corpus.Bounds{
				BatchRecords:  batchOrDefault(batchRecords, importer.MaxBatchRecords),
				BatchBytes:    batchOrDefault(batchBytes, importer.MaxBatchBytes),
				RecordBytes:   importer.MaxRecordBytes,
				ResidentBound: importer.ResidentBound,
			}
			cancelled := errors.Is(scanErr, importer.ErrScanCancelled)
			if err := renderScan(cmd.OutOrStdout(), plan, result, bounds, elapsed, cancelled); err != nil {
				return err
			}
			if cancelled {
				// A cancelled scan read part of a stream, so it measured part
				// of one. Publishing that as a benchmark would be publishing a
				// number nothing stands behind.
				return errors.New("scan cancelled; no benchmark was written")
			}
			if report == "" {
				return nil
			}
			encoded, err := corpus.EncodeBenchmark(corpus.NewBenchmark(plan, result, bounds, elapsed))
			if err != nil {
				return err
			}
			return writeNewFile(report, encoded,
				"cannot create the benchmark; destination must be new and writable",
				"cannot write the benchmark")
		},
	}
	cmd.Flags().StringVar(&saved, "plan", "", "Existing readmit-import-plan/v1 JSON file holding the declarations below")
	cmd.Flags().IntVar(&batchRecords, "batch-records", 0, "Records one parsing batch holds before it is decoded and released")
	cmd.Flags().IntVar(&batchBytes, "batch-bytes", 0, "Bytes one parsing batch holds before it is decoded and released")
	cmd.Flags().IntVar(&windowOffset, "window-offset", 0, "Scanned records to skip before the rendered window begins")
	cmd.Flags().IntVar(&windowLimit, "window-limit", 0, "Scanned records the rendered window holds; zero renders counts only")
	cmd.Flags().BoolVar(&progress, "progress", false, "Report bounded counts to standard error while the stream is read")
	cmd.Flags().StringVar(&report, "report", "", "New readmit-benchmark/v1 file for this run (never overwrite)")
	addDeclarationFlags(cmd, &declared, false)
	return cmd
}

func batchOrDefault(declared, fallback int) int {
	if declared == 0 {
		return fallback
	}
	return declared
}

// addDeclarationFlags registers the import plan declarations on a command that
// runs under one, so the flags cannot drift between two commands reading the
// same plan. containers is what tells the two apart: a command that reads
// containers imports the occurrences it divides them into and selects which
// entries of a folder or archive are members, and a command that reads one
// stream does neither, so it says so and does not offer the member flag.
func addDeclarationFlags(cmd *cobra.Command, declared *declaration, containers bool) {
	cmd.Flags().StringVar(&declared.framing, "framing", "", "Declared message framing: raw, mllp, or batch")
	cmd.Flags().StringVar(&declared.boundary, "batch-boundary", "", "Declared batch boundary, with --framing batch: segment-start or hl7-batch")
	cmd.Flags().StringVar(&declared.terminator, "terminator", "", "Declared segment terminator: cr, lf, or crlf")
	cmd.Flags().StringVar(&declared.encoding, "encoding", "", "Declared source encoding: utf-8, us-ascii, iso-8859-1, or unknown")
	occurrences := "every occurrence"
	if containers {
		occurrences = "every imported occurrence"
	}
	cmd.Flags().StringVar(&declared.direction, "direction", "", "Declared direction of "+occurrences+": unknown, inbound, or outbound")
	if containers {
		cmd.Flags().StringArrayVar(&declared.members, "member", nil, "One declared lowercase file-name suffix a folder or archive entry must end with; repeatable")
	}
}

// renderCorpus reports what a generation wrote. It names no path: the corpus
// and its manifest are the two files the person named on the command line.
func renderCorpus(out io.Writer, manifest corpus.Manifest) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "Corpus manifest: %s\nGenerator: %s\nProfile: %s\nSeed: %d\nBase time: %s\n",
		manifest.Schema, manifest.Inputs.Generator.GeneratorVersion, manifest.Inputs.Generator.ProfileVersion,
		manifest.Inputs.Generator.Seed, manifest.Inputs.Generator.BaseTime.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "Framing: %s\nTerminator: %s\nEncoding: %s\nDirection: %s\n",
		manifest.Inputs.Plan.Framing, manifest.Inputs.Plan.Terminator, manifest.Inputs.Plan.Encoding, manifest.Inputs.Plan.Direction)
	fmt.Fprintf(w, "Messages: %d\nBytes: %d\nDigest: %s\n", manifest.Inputs.Messages, manifest.Bytes, manifest.SHA256)
	if err := w.Flush(); err != nil {
		return errors.New("cannot write corpus output")
	}
	return nil
}

// renderScan reports one scan. Every line is a count, a declaration the person
// made, or a bound this release declares; no byte of the stream and no decoded
// value can reach it, so the command has no --show-values and needs none.
func renderScan(out io.Writer, plan importer.Plan, result importer.ScanResult, bounds corpus.Bounds, elapsed time.Duration, cancelled bool) error {
	w := bufio.NewWriter(out)
	boundary := string(plan.BatchBoundary)
	if boundary == "" {
		boundary = "not declared"
	}
	state := "completed"
	if cancelled {
		state = "cancelled"
	}
	fmt.Fprintf(w, "Scan plan: %s\nFraming: %s\nBatch boundary: %s\nTerminator: %s\nEncoding: %s\nDirection: %s\n",
		plan.Schema, plan.Framing, boundary, plan.Terminator, plan.Encoding, plan.Direction)
	fmt.Fprintf(w, "State: %s\nBytes: %d\nDigest: %s\nRecords: %d\nOccurrences: %d\nDecoded: %d\nUndecodable: %d\n",
		state, result.Bytes, result.SHA256, result.Records, result.Occurrences, result.Decoded, result.Undecodable)
	fmt.Fprintf(w, "Parsing batches: %d\nBatch bounds: %d records, %d bytes\nPeak resident bytes: %d\nResident bound: %d\n",
		result.Batches, bounds.BatchRecords, bounds.BatchBytes, result.PeakResidentBytes, bounds.ResidentBound)
	// A cancelled scan counted part of a stream, so it knows nothing about the
	// rest of it. The bounds are not asked about those counts at all: reporting
	// them as within the case bundle bounds would report an unread remainder as
	// a pass.
	if cancelled {
		fmt.Fprintln(w, "Case bounds: not evaluated; the scan was cancelled")
	} else if past := result.ExceedsCase(plan); len(past) == 0 {
		fmt.Fprintln(w, "Case bounds: within")
	} else {
		fmt.Fprintf(w, "Case bounds: exceeded %s\n", strings.Join(past, ", "))
	}
	if result.Window.Limit == 0 {
		fmt.Fprintln(w, "Window: none requested")
	} else {
		fmt.Fprintf(w, "Window: %d records from offset %d of %d\n", len(result.Rows), result.Window.Offset, result.Records)
		for _, row := range result.Rows {
			fmt.Fprintf(w, "  record %d offset %d size %d occurrences %d decoded %d undecodable %d\n",
				row.Ordinal, row.Offset, row.Size, row.Occurrences, row.Decoded, row.Undecodable)
		}
	}
	fmt.Fprintf(w, "Elapsed: %d ms\nTargets: %s\n", elapsed.Milliseconds(), corpus.TargetNote)
	if err := w.Flush(); err != nil {
		return errors.New("cannot write scan output")
	}
	return nil
}
