package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/spf13/cobra"
)

func scenarioCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{Use: "scenario", Short: "Design an interface workflow as a sequence of lifecycle events"}
	preview := &cobra.Command{
		Use: "preview SCENARIO", Short: "Show a designed workflow step by step, with the outcome its profile gives each step",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("scenario preview requires exactly one scenario document")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			document, err := readInputFile(args[0], scenario.MaxBytes)
			if err != nil {
				return err
			}
			timeline, err := scenario.PreviewDocument(document)
			if err != nil {
				return err
			}
			return printTimeline(cmd.OutOrStdout(), timeline)
		},
	}
	var output string
	generate := &cobra.Command{
		Use: "generate PLAN --output NEW_DIRECTORY", Short: "Generate deterministic synthetic workflow streams with complete inputs",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("scenario generate requires one generator plan")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if output == "" {
				return errors.New("scenario generate requires --output")
			}
			data, err := readInputFile(args[0], scenariogen.MaxBytes)
			if err != nil {
				return err
			}
			if _, err := scenariogen.Write(cmd.Context(), output, data); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "Generated synthetic workflow streams; generation.json retains inputs and intended arrivals. No external outcomes verified.")
			return err
		},
	}
	generate.Flags().StringVar(&output, "output", "", "New directory for streams and their generator record")
	library := &cobra.Command{Use: "check-library LIBRARY EXPECTATIONS", Short: "Check a pinned scenario against independent fixture expectations", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		*ran = true
		l, err := readInputFile(args[0], scenariolibrary.MaxBytes)
		if err != nil {
			return err
		}
		e, err := readInputFile(args[1], scenariolibrary.MaxBytes)
		if err != nil {
			return err
		}
		result, err := scenariolibrary.Check(cmd.Context(), l, e)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Fixture checks passed: %d streams, %d fields. External target outcomes: unverified.\n", result.Streams, result.Fields)
		return err
	}}
	command.AddCommand(preview, generate, library)
	return command
}

// printTimeline writes the preview. It names the scenario-local subject of
// each step and never the identifier that subject declares, so previewing a
// workflow discloses nothing a message would carry.
func printTimeline(out io.Writer, timeline scenario.Timeline) error {
	subjects := make([]string, 0, len(timeline.Subjects))
	for _, subject := range timeline.Subjects {
		subjects = append(subjects, fmt.Sprintf("%s (%s, %s)", subject.ID, subject.Kind, subject.InitialState))
	}
	header := fmt.Sprintf("Scenario: %s (version %s)\nProfile: %s\nBase time: %s\nSubjects: %s\nSteps: %d designed; %d accepted, %d refused\n\n",
		timeline.Scenario.ID, timeline.Scenario.Version, timeline.Profile,
		timeline.BaseTime.Format(time.RFC3339), strings.Join(subjects, "; "),
		len(timeline.Steps), timeline.Accepted, timeline.Refused)
	if _, err := io.WriteString(out, header); err != nil {
		return errors.New("cannot write the scenario preview")
	}
	width := len("Subject")
	for _, step := range timeline.Steps {
		if size := len(acting(step)); size > width {
			width = size
		}
	}
	rows := [][]string{{"#", "When", "Event", "Subject", "Expected", "Outcome"}}
	for _, step := range timeline.Steps {
		rows = append(rows, []string{
			fmt.Sprint(step.Ordinal), step.At.Format(time.RFC3339), string(step.Event),
			acting(step), string(step.Expect), outcome(step),
		})
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(out, "%3s  %-20s  %-5s  %-*s  %-8s  %s\n",
			row[0], row[1], row[2], width, row[3], row[4], row[5]); err != nil {
			return errors.New("cannot write the scenario preview")
		}
	}
	return nil
}

// acting names the subject a step acts on, and the surviving identity when the
// step is a merge.
func acting(step scenario.Occurrence) string {
	if step.Into == "" {
		return step.Subject
	}
	return step.Subject + " into " + step.Into
}

// outcome states what the profile did with the step: the transition it took,
// or the state it left alone and why.
func outcome(step scenario.Occurrence) string {
	if step.Reason == "" {
		return fmt.Sprintf("%s -> %s (%s)", step.From, step.To, step.Description)
	}
	return fmt.Sprintf("%s unchanged; %s", step.From, step.Reason)
}
