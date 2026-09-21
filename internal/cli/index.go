package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/spf13/cobra"
)

// indefinite is the one word that declares an index has no retention end. It is
// spelled out rather than left to an omitted flag, so no index is ever kept
// forever because nobody said otherwise.
const indefinite = "indefinite"

func indexCommand() *cobra.Command {
	command := &cobra.Command{
		Use:         "index",
		Annotations: declare(capabilityFree),
		Short:       "Build and query a derived, disposable index of one case",
		Args:        cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("index requires a subcommand: build, show, or search")
		},
	}
	command.AddCommand(indexBuild(), indexShow(), indexSearch())
	return command
}

func indexBuild() *cobra.Command {
	var output, retain, until string
	var fields []string
	command := &cobra.Command{
		Use:         "build CASE --output NEW_FILE --field SELECTOR --retain FORM --retain-until WHEN",
		Annotations: declareInterruptible(capabilityAuthor),
		Short:       "Build an index of declared fields from canonical case evidence",
		Args:        indexOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("index build requires --output with a new file")
			}
			policy, err := declaredPolicy(fields, retain, until)
			if err != nil {
				return err
			}
			opened, err := bundle.Open(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			at := time.Now().UTC()
			document, err := index.Build(ctx, opened, policy, at)
			if err != nil {
				return err
			}
			if _, err := index.Write(output, document); err != nil {
				return err
			}
			return writeIndex(cmd.OutOrStdout(), "Index written", document, at)
		},
	}
	command.Flags().StringVar(&output, "output", "", "New index file (never overwrite)")
	command.Flags().StringArrayVar(&fields, "field", nil, "Retain this field selector; repeat for each field")
	command.Flags().StringVar(&retain, "retain", "", "Retention form: values, digests, or states")
	command.Flags().StringVar(&until, "retain-until", "", "RFC 3339 instant the index is retained until, or "+indefinite)
	return command
}

func indexShow() *cobra.Command {
	command := &cobra.Command{
		Use:         "show CASE INDEX",
		Annotations: declare(capabilityFree),
		Short:       "Report what an index retains, of which case, and until when",
		Args:        indexTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			document, err := openIndex(args[0], args[1])
			if err != nil {
				return err
			}
			return writeIndex(cmd.OutOrStdout(), "Index", document, time.Now().UTC())
		},
	}
	return command
}

func indexSearch() *cobra.Command {
	var field, equals, contains, state string
	var showValues bool
	command := &cobra.Command{
		Use:         "search CASE INDEX --equals VALUE",
		Annotations: declare(capabilityFree),
		Short:       "Find the occurrences of a case whose indexed fields match",
		Args:        indexTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := declaredQuery(cmd, field, equals, contains, state)
			if err != nil {
				return err
			}
			document, err := openIndex(args[0], args[1])
			if err != nil {
				return err
			}
			at := time.Now().UTC()
			result, err := document.Search(at, query)
			if err != nil {
				return err
			}
			return writeMatches(cmd.OutOrStdout(), document, query, result, at, showValues)
		},
	}
	command.Flags().StringVar(&field, "field", "", "Ask only of this indexed field selector")
	command.Flags().StringVar(&equals, "equals", "", "Match a value byte for byte")
	command.Flags().StringVar(&contains, "contains", "", "Match a value holding this text")
	command.Flags().StringVar(&state, "state", "", "Match a decoded state: present, empty, null, or omitted")
	command.Flags().BoolVar(&showValues, "show-values", false, "Explicitly display retained values as escaped byte strings")
	return command
}

// openIndex reads an index and the case it names together, and refuses the pair
// the moment they disagree. The evidence is opened through the same reader
// `timeline` uses and is never modified: a damaged, unreadable or stale index
// is reported as something to rebuild, and the case stays readable either way.
func openIndex(casePath, indexPath string) (index.Document, error) {
	opened, err := bundle.Open(casePath)
	if err != nil {
		return index.Document{}, err
	}
	document, err := index.Open(indexPath)
	if err != nil {
		return index.Document{}, err
	}
	if err := document.Describes(opened); err != nil {
		return index.Document{}, err
	}
	return document, nil
}

// declaredPolicy turns the three retention declarations into a policy. None has
// a default: an index that retained a field nobody named, in a form nobody
// chose, for a period nobody stated, would be exactly the implicit retention
// this command exists to prevent.
func declaredPolicy(fields []string, retain, until string) (index.Policy, error) {
	if len(fields) == 0 {
		return index.Policy{}, usage("index build requires --field naming each field to retain")
	}
	if retain == "" {
		return index.Policy{}, usage("index build requires --retain with values, digests, or states")
	}
	if until == "" {
		return index.Policy{}, usage("index build requires --retain-until with an RFC 3339 instant or %s", indefinite)
	}
	policy := index.Policy{Fields: make([]string, 0, len(fields)), Retention: index.Retention(retain)}
	for _, field := range fields {
		selector, err := hl7.ParseSelector(field)
		if err != nil {
			return index.Policy{}, err
		}
		policy.Fields = append(policy.Fields, selector.String())
	}
	if until != indefinite {
		instant, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return index.Policy{}, usage("a retention end is an RFC 3339 instant or %s", indefinite)
		}
		instant = instant.UTC()
		policy.RetainUntil = &instant
	}
	// The declarations are checked here as well as inside the builder, so an
	// incomplete one is reported before a case is opened rather than after.
	if err := index.ValidatePolicy(policy); err != nil {
		return index.Policy{}, err
	}
	return policy, nil
}

// declaredQuery accepts exactly one question. Flags are counted rather than
// ranked, so asking two things at once is an error instead of one of them
// silently winning.
func declaredQuery(cmd *cobra.Command, field, equals, contains, state string) (index.Query, error) {
	var query index.Query
	asked := 0
	if cmd.Flags().Changed("equals") {
		asked, query.Match, query.Term = asked+1, index.Equals, []byte(equals)
	}
	if cmd.Flags().Changed("contains") {
		asked, query.Match, query.Term = asked+1, index.Contains, []byte(contains)
	}
	if cmd.Flags().Changed("state") {
		asked, query.Match, query.State = asked+1, index.State, hl7.State(state)
	}
	if asked != 1 {
		return index.Query{}, usage("index search asks exactly one of --equals, --contains, or --state")
	}
	if field != "" {
		selector, err := hl7.ParseSelector(field)
		if err != nil {
			return index.Query{}, err
		}
		query.Field = selector.String()
	}
	return query, nil
}

// writeIndex reports what an index declares. It never prints a retained value,
// an original source path, or the case directory it was built from.
func writeIndex(out io.Writer, heading string, document index.Document, at time.Time) error {
	w := bufio.NewWriter(out)
	indexSummary(w, heading, document, at)
	return flushIndex(w)
}

// indexSummary writes the summary into a writer the caller owns, so a command
// that prints matches after it does not open a second writer over the same
// output.
func indexSummary(w *bufio.Writer, heading string, document index.Document, at time.Time) {
	decoded, undecodable := 0, 0
	for _, record := range document.Records {
		if record.ParseError != "" {
			undecodable++
			continue
		}
		decoded++
	}
	retention := indefinite
	state := "active"
	if document.Policy.RetainUntil != nil {
		retention = document.Policy.RetainUntil.Format(time.RFC3339)
		if document.Usable(at) != nil {
			state = "ended"
		}
	}
	fmt.Fprintf(w, "%s: %s\nCase: %s\nCase contract: %s\nProvenance: %s\nRetention: %s\nRetain until: %s\nRetention state: %s\nFields: %s\nSources: %d\nRecords: %d\nDecoded occurrences: %d\nUndecodable occurrences: %d\nBuilt: %s\n",
		heading, document.Schema, document.Case.Identity, document.Case.Schema, document.Case.Provenance,
		document.Policy.Retention, retention, state, strings.Join(document.Policy.Fields, " "),
		len(document.Case.Sources), len(document.Records), decoded, undecodable,
		document.BuiltAt.Format(time.RFC3339))
}

func flushIndex(w *bufio.Writer) error {
	if err := w.Flush(); err != nil {
		return errors.New("cannot write index output")
	}
	return nil
}

// writeMatches reports the answer and, separately, what the index could not
// decide. An undecided value is not a miss: it is a present value whose
// retained prefix was shortened, and saying so keeps a query from reading as
// though the case does not hold it.
func writeMatches(out io.Writer, document index.Document, query index.Query, result index.Result, at time.Time, showValues bool) error {
	w := bufio.NewWriter(out)
	indexSummary(w, "Index", document, at)
	field := "every indexed field"
	if query.Field != "" {
		field = query.Field
	}
	// The undecodable count is already reported with the index it belongs to;
	// what a query adds is how many retained values it could not settle.
	fmt.Fprintf(w, "Query: %s\nField: %s\nMatches: %d\nUndecided: %d\n",
		query.Match, field, len(result.Hits), result.Undecided)
	for _, hit := range result.Hits {
		fmt.Fprintf(w, "  %s source=%s kind=%s direction=%s offset=%d bytes=%d observed=%s field=%s state=%s at=%d length=%d truncated=%t\n",
			hit.Record.ID, hit.Record.SourceID, hit.Record.Kind, hit.Record.Direction, hit.Record.Offset,
			hit.Record.Size, timelineTime(hit.Record.ObservedAt), hit.Value.Selector, hit.Value.State,
			hit.Value.Offset, hit.Value.Length, hit.Value.Truncated)
		if showValues && len(hit.Value.Bytes) > 0 {
			fmt.Fprintf(w, "    value=%s\n", strconv.QuoteToASCII(string(hit.Value.Bytes)))
		}
	}
	return flushIndex(w)
}

func indexOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("index build requires exactly one case bundle directory")
	}
	return nil
}

func indexTwoArguments(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errors.New("index subcommand requires a case bundle directory and one index file")
	}
	return nil
}
