package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/spf13/cobra"
)

// diagnoseReviewCommand records what a person decided about one diagnosis and
// what those decisions promote to. It runs no rule and produces no finding: the
// diagnosis it reads is unchanged, and so is the case.
func diagnoseReviewCommand() *cobra.Command {
	var casePath, decisionsPath, output string
	cmd := &cobra.Command{
		Use:         "review DIAGNOSIS --case case --decisions file --output new_directory",
		Annotations: declare(capabilityAuthor),
		Short:       "Record analyst decisions about findings and promote confirmed ones to draft assertions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if casePath == "" || decisionsPath == "" || output == "" {
				return usage("diagnose review requires --case, --decisions, and --output with a new directory")
			}
			reportPath, err := artifactpath.Directory(args[0])
			if err != nil {
				return errors.New("cannot open the diagnosis report directory")
			}
			reportData, err := readInputFile(filepath.Join(reportPath, "report.json"), findingreview.MaxReportBytes)
			if err != nil {
				return err
			}
			report, err := findingreview.ParseReport(reportData)
			if err != nil {
				return err
			}
			decisionsData, err := readInputFile(decisionsPath, findingreview.MaxDecisionsBytes)
			if err != nil {
				return err
			}
			decisions, err := findingreview.ParseDecisions(decisionsData)
			if err != nil {
				return err
			}
			source, err := bundle.Open(casePath)
			if err != nil {
				return err
			}
			entry := filepath.Base(filepath.Clean(casePath))
			record, err := findingreview.Review(findingreview.Reviewed{Report: report, Identity: digestOf(reportData), Case: source, Entry: entry}, decisions, digestOf(decisionsData))
			if err != nil {
				return err
			}
			resolvedOutput, err := reviewOutputOutsideInputs(casePath, reportPath, output)
			if err != nil {
				return err
			}
			jsonData, err := findingreview.JSON(record)
			if err != nil {
				return errors.New("cannot encode finding review")
			}
			if err := writeNewReportDirectory(resolvedOutput, "review", outputFile{"review.json", jsonData}, outputFile{"review.md", findingreview.Markdown(record)}); err != nil {
				return err
			}
			confirmed, promoted, unsupported := reviewCounts(record)
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Review complete: %d findings, %d confirmed, %d draft assertions, %d not expressible as a test. Unreviewed findings promote nothing. JSON and Markdown reviews written.\n", len(record.Findings), confirmed, promoted, unsupported)
			return err
		},
	}
	cmd.Flags().StringVar(&casePath, "case", "", "The verified case directory the diagnosis was run over")
	cmd.Flags().StringVar(&decisionsPath, "decisions", "", "Explicit readmit-finding-decisions/v1 JSON document recording the analyst's verdicts")
	cmd.Flags().StringVar(&output, "output", "", "New directory for review.json and review.md (never overwrite)")
	return cmd
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// reviewCounts totals what the review decided, so the one line a person sees
// states how much of the diagnosis became a test and how much did not.
func reviewCounts(record findingreview.Record) (confirmed, promoted, unsupported int) {
	for _, status := range record.Findings {
		if status.Verdict == findingreview.Confirmed {
			confirmed++
		}
		if status.Promotion == nil {
			continue
		}
		promoted += len(status.Promotion.Expectations)
		unsupported += len(status.Promotion.Unsupported)
	}
	return confirmed, promoted, unsupported
}

// A review directory inside the case would invalidate the verified immutable
// evidence, and one inside the diagnosis would put a judgment where a machine's
// findings are. Resolve both and compare filesystem identity, not names.
func reviewOutputOutsideInputs(casePath, reportPath, output string) (string, error) {
	caseInfo, err := os.Stat(casePath)
	if err != nil {
		return "", errors.New("cannot inspect the reviewed case directory")
	}
	reportInfo, err := os.Stat(reportPath)
	if err != nil {
		return "", errors.New("cannot inspect the diagnosis report directory")
	}
	return artifactpath.Destination(output, caseInfo, reportInfo)
}
