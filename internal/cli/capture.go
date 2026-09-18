package cli

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/spf13/cobra"
)

type captureMetadata struct {
	Schema       string               `json:"schema"`
	Observations []captureObservation `json:"observations"`
}

type captureObservation struct {
	Source     int              `json:"source"`
	Sequence   int              `json:"sequence"`
	Direction  bundle.Direction `json:"direction"`
	ObservedAt *time.Time       `json:"observed_at"`
}

func captureCommand(ran *bool) *cobra.Command {
	var output, format, terminator, metadata string
	var showValues bool
	cmd := &cobra.Command{
		Use: "capture FILE... --output NEW_DIRECTORY", Short: "Import exact file evidence into a new case bundle",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || len(args) > bundle.MaxSources {
				return errors.New("capture requires between 1 and 128 input files")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if output == "" {
				return errors.New("capture requires --output with a new directory")
			}
			if cmd.Flags().Changed("metadata") && metadata == "" {
				return errors.New("metadata file cannot be empty")
			}
			importedAt := time.Now().UTC()
			inputs := make([]bundle.Input, len(args))
			total := 0
			for i, path := range args {
				data, err := readInputFile(path, bundle.MaxSourceBytes)
				if err != nil {
					return err
				}
				total += len(data)
				if total > bundle.MaxEvidenceBytes {
					return errors.New("bundle evidence exceeds 64 MiB")
				}
				absolute, err := filepath.Abs(path)
				if err != nil {
					return errors.New("cannot resolve input source location")
				}
				inputs[i] = bundle.Input{Path: absolute, Data: data, Options: hl7.Options{Format: hl7.Format(format), Terminator: hl7.Terminator(terminator)}, Observations: make(map[int]bundle.Observation)}
			}
			if metadata != "" {
				if err := applyMetadata(inputs, metadata); err != nil {
					return err
				}
			}
			b, err := bundle.Write(output, inputs, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &importedAt})
			if err != nil {
				return err
			}
			return renderBundle(cmd.OutOrStdout(), b, showValues, showValues)
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "New case bundle directory (never overwrite)")
	cmd.Flags().StringVar(&format, "format", "auto", "Input framing: auto, raw, or mllp")
	cmd.Flags().StringVar(&terminator, "terminator", "auto", "Segment terminator: auto, cr, lf, or crlf")
	cmd.Flags().StringVar(&metadata, "metadata", "", "Explicit per-occurrence observations in a readmit-capture/v1 JSON file")
	cmd.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display complete occurrence bytes as escaped strings")
	return cmd
}

func timelineCommand(ran *bool) *cobra.Command {
	var showValues bool
	cmd := &cobra.Command{
		Use: "timeline BUNDLE", Short: "Verify and show case events, independent times, and correlation gaps",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("timeline requires exactly one bundle directory")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			b, err := bundle.Open(args[0])
			if err != nil {
				return err
			}
			return renderBundle(cmd.OutOrStdout(), b, true, showValues)
		},
	}
	cmd.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display complete occurrence bytes as escaped strings")
	return cmd
}

func applyMetadata(inputs []bundle.Input, path string) error {
	data, err := readInputFile(path, 1<<20)
	if err != nil {
		return err
	}
	var metadata captureMetadata
	if err := json.Unmarshal(data, &metadata, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid capture metadata JSON")
	}
	if metadata.Schema != "readmit-capture/v1" {
		return errors.New("unsupported capture metadata schema version")
	}
	if len(metadata.Observations) > bundle.MaxEvents {
		return errors.New("too many capture observations")
	}
	for _, observation := range metadata.Observations {
		if observation.Source < 1 || observation.Source > len(inputs) || observation.Sequence < 1 {
			return errors.New("capture observation references an unknown source or occurrence")
		}
		observations := inputs[observation.Source-1].Observations
		if _, exists := observations[observation.Sequence]; exists {
			return errors.New("duplicate capture observation")
		}
		observations[observation.Sequence] = bundle.Observation{Direction: observation.Direction, ObservedAt: observation.ObservedAt}
	}
	return nil
}

// Only timestamp-shaped MSH-7 bytes may appear in the default timeline. A
// malformed MSH-7 can contain arbitrary values, so its bytes require opt-in.
var declaredTimePattern = regexp.MustCompile(`^[0-9]{4}([0-9]{2}){0,5}(\.[0-9]{1,4})?([+-][0-9]{4})?(\^[YLDHMS])?$`)

func renderBundle(out io.Writer, b *bundle.Bundle, timeline, showValues bool) error {
	w := bufio.NewWriter(out)
	kinds := make(map[bundle.EventKind]int)
	links := make(map[bundle.LinkKind]int)
	unknownTimes := 0
	for _, event := range b.Events {
		kinds[event.Kind]++
		if event.ObservedAt == nil {
			unknownTimes++
		}
	}
	for _, link := range b.Correlations {
		links[link.Kind]++
	}
	fmt.Fprintf(w, "Bundle: %s\nSchema: %s\nProvenance: %s\nSources: %d\nOccurrences: %d\nMessages: %d\nACKs: %d\nUnparsed: %d\n", b.Identity, b.Manifest.Schema, b.Manifest.Provenance.Mode, len(b.Manifest.Sources), len(b.Events), kinds[bundle.Message], kinds[bundle.Acknowledgement], kinds[bundle.Unparsed])
	fmt.Fprintf(w, "Matched ACKs: %d\nUnmatched ACKs: %d\nAmbiguous ACKs: %d\nUnacknowledged messages: %d\nUnknown observed times: %d\n", links[bundle.Matched], links[bundle.UnmatchedACK], links[bundle.AmbiguousACK], links[bundle.Unacknowledged], unknownTimes)
	if snapshot := b.Observation; snapshot != nil {
		fmt.Fprintf(w, "Observation: %s\nObservation profile: %s\nReceiver mode: %s\nProcessed occurrences: %d\nLedger records: %d\nConsistent: %t\n", snapshot.Schema, snapshot.Profile, snapshot.Mode, len(snapshot.Processed), len(snapshot.Records), snapshot.Consistent)
		if showValues {
			data, err := json.Marshal(snapshot, json.Deterministic(true))
			if err != nil {
				return errors.New("cannot render observation")
			}
			fmt.Fprintf(w, "Observation values: %s\n", strconv.QuoteToASCII(string(data)))
		}
	}
	if timeline {
		fmt.Fprintln(w, "Timeline (source/sequence order; no inferred cross-source chronology):")
		for _, event := range b.Events {
			declared := "unknown (unparsed)"
			if event.Fields != nil {
				field := event.Fields.DeclaredTime
				declared = "unknown (" + string(field.State) + ")"
				if field.State == hl7.Present {
					value := b.Value(event.ID, field)
					declared = "uninterpreted (use --show-values)"
					if declaredTimePattern.Match(value) || showValues {
						declared = strconv.QuoteToASCII(string(value))
					}
				}
			}
			fmt.Fprintf(w, "  %s kind=%s direction=%s offset=%d bytes=%d observed=%s declared=%s imported=%s\n", event.ID, event.Kind, event.Direction, event.Offset, event.Payload.Size, timelineTime(event.ObservedAt), declared, timelineTime(event.ImportedAt))
			if event.ParseError != "" {
				fmt.Fprintf(w, "    unparsed: %s\n", event.ParseError)
			}
			if showValues {
				raw, _ := b.Raw(event.ID)
				fmt.Fprintf(w, "    raw=%s\n", strconv.QuoteToASCII(string(raw)))
			}
		}
		fmt.Fprintln(w, "Correlations (same-source, literal control-ID matching):")
		for _, link := range b.Correlations {
			fmt.Fprintf(w, "  %s ack=%s messages=%v\n", link.Kind, link.ACKID, link.MessageIDs)
		}
	}
	if err := w.Flush(); err != nil {
		return errors.New("cannot write bundle output")
	}
	return nil
}

func timelineTime(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	return t.Format(time.RFC3339Nano)
}
