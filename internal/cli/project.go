package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/spf13/cobra"
)

// evidenceState is what `project show` reports about one registered case. A
// case is only ever `verified` when the shared reader accepted the evidence and
// its identity is still the identity the project recorded. Nothing else passes.
type evidenceState string

const (
	evidenceVerified   evidenceState = "verified"
	evidenceChanged    evidenceState = "changed"
	evidenceUnreadable evidenceState = "unreadable"
	evidenceMissing    evidenceState = "missing"
)

func projectCommand(ran *bool) *cobra.Command {
	command := &cobra.Command{
		Use:   "project",
		Short: "Create and manage an interface investigation project",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("project requires a subcommand: init, settings, add, update, revise, note, or show")
		},
	}
	command.AddCommand(projectInit(ran), projectSettings(ran), projectAdd(ran), projectUpdate(ran), projectRevise(ran), projectNote(ran), projectShow(ran))
	return command
}

func projectInit(ran *bool) *cobra.Command {
	var output, title, owner string
	var versions []string
	command := &cobra.Command{
		Use:   "init --output NEW_DIRECTORY --title TITLE --interface-version ID",
		Short: "Create a new project directory and its first document",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*ran = true
			if output == "" {
				return errors.New("project init requires --output with a new directory")
			}
			document := project.Document{
				Schema:            project.Schema,
				Settings:          project.Settings{Title: title, DefaultOwner: owner},
				InterfaceVersions: versions,
			}
			if len(versions) > 0 {
				document.Settings.DefaultInterfaceVersion = versions[0]
			}
			created, err := project.Create(output, document)
			if err != nil {
				return err
			}
			return writeProject(cmd.OutOrStdout(), "Project created: "+created.Document.Settings.Title, created.Document)
		},
	}
	command.Flags().StringVar(&output, "output", "", "New project directory (never overwrite)")
	command.Flags().StringVar(&title, "title", "", "Project title")
	command.Flags().StringVar(&owner, "owner", "", "Default owner every case inherits")
	command.Flags().StringArrayVar(&versions, "interface-version", nil, "Declare an interface version; the first is the project default")
	return command
}

func projectSettings(ran *bool) *cobra.Command {
	var title, owner, defaultVersion string
	var versions []string
	command := &cobra.Command{
		Use:   "settings PROJECT",
		Short: "Change project-level settings and declare interface versions",
		Args:  projectOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			document := opened.Document
			if cmd.Flags().Changed("title") {
				document.Settings.Title = title
			}
			if cmd.Flags().Changed("owner") {
				document.Settings.DefaultOwner = owner
			}
			for _, version := range versions {
				if !slices.Contains(document.InterfaceVersions, version) {
					document.InterfaceVersions = append(slices.Clip(slices.Clone(document.InterfaceVersions)), version)
				}
			}
			if cmd.Flags().Changed("default-interface-version") {
				document.Settings.DefaultInterfaceVersion = defaultVersion
			}
			if err := opened.Save(document); err != nil {
				return err
			}
			return writeProject(cmd.OutOrStdout(), "Project settings updated: "+document.Settings.Title, document)
		},
	}
	command.Flags().StringVar(&title, "title", "", "Project title")
	command.Flags().StringVar(&owner, "owner", "", "Default owner every case inherits")
	command.Flags().StringArrayVar(&versions, "interface-version", nil, "Declare a further interface version")
	command.Flags().StringVar(&defaultVersion, "default-interface-version", "", "Declared interface version new cases inherit")
	return command
}

func projectAdd(ran *bool) *cobra.Command {
	var title, owner, status, version string
	var tags, incidents []string
	command := &cobra.Command{
		Use:   "add PROJECT CASE --title TITLE",
		Short: "Register a verified case bundle of the project directory",
		Args:  projectTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			revisions, err := project.ReadRevisions(opened.Root)
			if err != nil {
				return err
			}
			facts, _, err := verifiedEvidence(opened.Root, args[1])
			if err != nil {
				return err
			}
			entry := project.Case{Name: facts.name, Identity: facts.identity, Schema: facts.schema, Provenance: facts.provenance}
			entry.Title = title
			entry.InterfaceVersion = version
			entry.Owner = owner
			entry.Status = project.Status(status)
			if entry.Tags, err = declaredValues(tags); err != nil {
				return err
			}
			if entry.Incidents, err = declaredValues(incidents); err != nil {
				return err
			}
			document, stored, err := project.AddCase(opened.Document, revisions, entry)
			if err != nil {
				return err
			}
			if err := opened.Save(document); err != nil {
				return err
			}
			return writeCase(cmd.OutOrStdout(), "Case registered: "+stored.Name, stored)
		},
	}
	command.Flags().StringVar(&title, "title", "", "Case title")
	command.Flags().StringVar(&version, "interface-version", "", "Declared interface version; the project default when absent")
	command.Flags().StringVar(&owner, "owner", "", "Case owner; the project default when absent")
	command.Flags().StringVar(&status, "status", "", "Case status: open, investigating, resolved, or closed")
	command.Flags().StringArrayVar(&tags, "tag", nil, "Case tag; repeat for more")
	command.Flags().StringArrayVar(&incidents, "incident", nil, "Linked incident reference; repeat for more")
	return command
}

func projectUpdate(ran *bool) *cobra.Command {
	var title, owner, status, version string
	var tags, incidents []string
	command := &cobra.Command{
		Use:   "update PROJECT CASE",
		Short: "Change the title, tags, ownership, status, or linked incidents of a registered case",
		Args:  projectTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			var change project.Change
			if cmd.Flags().Changed("title") {
				change.Title = &title
			}
			if cmd.Flags().Changed("owner") {
				change.Owner = &owner
			}
			if cmd.Flags().Changed("status") {
				value := project.Status(status)
				change.Status = &value
			}
			if cmd.Flags().Changed("interface-version") {
				change.InterfaceVersion = &version
			}
			if cmd.Flags().Changed("tag") {
				declared, err := declaredValues(tags)
				if err != nil {
					return err
				}
				change.Tags = &declared
			}
			if cmd.Flags().Changed("incident") {
				declared, err := declaredValues(incidents)
				if err != nil {
					return err
				}
				change.Incidents = &declared
			}
			if change.Empty() {
				return errors.New("project update requires at least one change")
			}
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			document, stored, err := project.UpdateCase(opened.Document, args[1], change)
			if err != nil {
				return err
			}
			if err := opened.Save(document); err != nil {
				return err
			}
			return writeCase(cmd.OutOrStdout(), "Case updated: "+stored.Name, stored)
		},
	}
	command.Flags().StringVar(&title, "title", "", "Case title")
	command.Flags().StringVar(&version, "interface-version", "", "Declared interface version")
	command.Flags().StringVar(&owner, "owner", "", "Case owner")
	command.Flags().StringVar(&status, "status", "", "Case status: open, investigating, resolved, or closed")
	command.Flags().StringArrayVar(&tags, "tag", nil, `Replace the tags with these; repeat for more, or pass "" alone to clear them`)
	command.Flags().StringArrayVar(&incidents, "incident", nil, `Replace the linked incidents with these; repeat for more, or pass "" alone to clear them`)
	return command
}

func projectRevise(ran *bool) *cobra.Command {
	var parent string
	command := &cobra.Command{
		Use:   "revise PROJECT REVISION --parent NAME",
		Short: "Register derived evidence as a revision of a registered case or revision",
		Args:  projectTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			if parent == "" {
				return errors.New("project revise requires --parent naming the case or revision it was derived from")
			}
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			revisions, err := project.ReadRevisions(opened.Root)
			if err != nil {
				return err
			}
			// Both the revision and its parent are re-verified through the
			// shared reader before any lineage is recorded, so a parent whose
			// evidence no longer matches what the project recorded is reported
			// rather than quietly re-identified under a new revision.
			facts, derivation, err := verifiedEvidence(opened.Root, args[1])
			if err != nil {
				return err
			}
			ancestor, _, err := verifiedEvidence(opened.Root, parent)
			if err != nil {
				return err
			}
			entry := project.Revision{
				Name:       facts.name,
				Identity:   facts.identity,
				Schema:     facts.schema,
				Provenance: facts.provenance,
				Operation: project.Operation{
					Name:           derivation,
					Parent:         ancestor.name,
					ParentIdentity: ancestor.identity,
				},
			}
			updated, stored, err := project.AddRevision(opened.Document, revisions, entry)
			if err != nil {
				return err
			}
			if err := project.WriteRevisions(opened.Root, updated); err != nil {
				return err
			}
			return writeRevision(cmd.OutOrStdout(), "Revision registered: "+stored.Name, stored)
		},
	}
	command.Flags().StringVar(&parent, "parent", "", "Registered case or revision this evidence was derived from")
	return command
}

func projectNote(ran *bool) *cobra.Command {
	var title, body, subject string
	command := &cobra.Command{
		Use:   "note PROJECT NAME --title TITLE",
		Short: "Create or replace an editable note or draft beside the evidence",
		Args:  projectTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			revisions, err := project.ReadRevisions(opened.Root)
			if err != nil {
				return err
			}
			updated, stored, err := project.SetNote(opened.Document, revisions, project.Note{
				Name:    args[1],
				Subject: subject,
				Title:   title,
				Body:    body,
			})
			if err != nil {
				return err
			}
			if err := project.WriteRevisions(opened.Root, updated); err != nil {
				return err
			}
			return writeNote(cmd.OutOrStdout(), "Note saved: "+stored.Name, stored)
		},
	}
	command.Flags().StringVar(&title, "title", "", "Note title")
	command.Flags().StringVar(&body, "body", "", "Note body; a note may be a title alone while it is still a draft")
	command.Flags().StringVar(&subject, "subject", "", "Registered case or revision this note is about; absent makes it a project draft")
	return command
}

func projectShow(ran *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "show PROJECT",
		Short: "Show project settings and every registered case with its verified identity",
		Args:  projectOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			*ran = true
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			revisions, err := project.ReadRevisions(opened.Root)
			if err != nil {
				return err
			}
			w := bufio.NewWriter(cmd.OutOrStdout())
			writeSettings(w, "Project: "+opened.Document.Settings.Title, opened.Document)
			for _, entry := range opened.Document.Cases {
				fmt.Fprintf(w, "  %s evidence=%s identity=%s schema=%s provenance=%s interface=%s status=%s owner=%s\n",
					entry.Name, evidenceFor(opened.Root, evidence{name: entry.Name, identity: entry.Identity, schema: entry.Schema, provenance: entry.Provenance}),
					entry.Identity, entry.Schema, entry.Provenance, entry.InterfaceVersion, entry.Status, absent(entry.Owner))
				fmt.Fprintf(w, "    title: %s\n    tags: %s\n    incidents: %s\n", entry.Title, list(entry.Tags), list(entry.Incidents))
			}
			// The editable document is reported separately: a revision carries
			// lineage rather than case metadata, and a note is working text
			// that is not evidence at all.
			fmt.Fprintf(w, "Revisions: %d\n", len(revisions.Revisions))
			for _, entry := range revisions.Revisions {
				fmt.Fprintf(w, "  %s evidence=%s identity=%s schema=%s provenance=%s\n",
					entry.Name, evidenceFor(opened.Root, evidence{name: entry.Name, identity: entry.Identity, schema: entry.Schema, provenance: entry.Provenance}),
					entry.Identity, entry.Schema, entry.Provenance)
				fmt.Fprintf(w, "    operation=%s parent=%s parent_identity=%s\n", entry.Operation.Name, entry.Operation.Parent, entry.Operation.ParentIdentity)
			}
			fmt.Fprintf(w, "Notes: %d\n", len(revisions.Notes))
			for _, note := range revisions.Notes {
				fmt.Fprintf(w, "  %s subject=%s\n    title: %s\n", note.Name, absent(note.Subject), note.Title)
				writeBody(w, note.Body)
			}
			if err := w.Flush(); err != nil {
				return errors.New("cannot write project output")
			}
			return nil
		},
	}
}

// evidence is what a project records about one artifact: the four facts a
// registered case and a registered revision both keep exactly as the shared
// reader reported them. They travel as one value so that four same-typed
// strings cannot be silently transposed at a call site.
type evidence struct {
	name, identity, schema, provenance string
}

// verifiedEvidence reads those facts from the bundle itself, and the derivation
// its manifest declares when the evidence is the output of a transformation.
// The provenance mode and the derivation are the ones the verified manifest
// carries, so neither is ever inferred from the directory name or supplied on
// the command line.
func verifiedEvidence(root, name string) (evidence, string, error) {
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return evidence{}, "", errors.New("a case must be named by one directory entry of the project")
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return evidence{}, "", errors.New("the case could not be verified as complete, unmodified evidence")
	}
	return evidence{
		name:       name,
		identity:   opened.Identity,
		schema:     opened.Manifest.Schema,
		provenance: string(opened.Manifest.Provenance.Mode),
	}, opened.Manifest.Provenance.Derivation, nil
}

// evidenceFor re-verifies one registered case or revision through the same
// reader that accepted it, and compares every evidence fact the project
// recorded with what the reader reported. A document that claims a provenance
// mode or a contract version the evidence does not carry is never reported as
// verified, so an edited document cannot make imported evidence look synthetic. What the
// project recorded is reported exactly as recorded whatever this finds: `show`
// reports, and never rewrites what a project recorded.
func evidenceFor(root string, recorded evidence) evidenceState {
	path, err := artifactpath.Child(root, recorded.name)
	if err != nil {
		return evidenceMissing
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return evidenceUnreadable
	}
	if opened.Identity != recorded.identity || opened.Manifest.Schema != recorded.schema ||
		string(opened.Manifest.Provenance.Mode) != recorded.provenance {
		return evidenceChanged
	}
	return evidenceVerified
}

// declaredValues reads one explicitly empty value as the empty set, which is
// how a set is cleared. An empty value beside real ones is a typo rather than a
// request to clear, so it is refused instead of being dropped silently.
func declaredValues(values []string) ([]string, error) {
	if len(values) == 1 && values[0] == "" {
		return nil, nil
	}
	if slices.Contains(values, "") {
		return nil, errors.New(`an empty value clears the set only when it is the only value given`)
	}
	return values, nil
}

// writeLines buffers one command's output and reports a single failure to write
// it, so every `project` command reports that failure the same way.
func writeLines(out io.Writer, render func(io.Writer)) error {
	w := bufio.NewWriter(out)
	render(w)
	if err := w.Flush(); err != nil {
		return errors.New("cannot write project output")
	}
	return nil
}

func writeProject(out io.Writer, headline string, document project.Document) error {
	return writeLines(out, func(w io.Writer) { writeSettings(w, headline, document) })
}

func writeSettings(w io.Writer, headline string, document project.Document) {
	fmt.Fprintf(w, "%s\nDocument: %s\nInterface versions: %s\nDefault interface version: %s\nDefault owner: %s\nCases: %d\n",
		headline, document.Schema, list(document.InterfaceVersions), absent(document.Settings.DefaultInterfaceVersion), absent(document.Settings.DefaultOwner), len(document.Cases))
}

func writeCase(out io.Writer, headline string, entry project.Case) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nIdentity: %s\nSchema: %s\nProvenance: %s\nInterface version: %s\nTitle: %s\nStatus: %s\nOwner: %s\nTags: %s\nIncidents: %s\n",
			headline, entry.Identity, entry.Schema, entry.Provenance, entry.InterfaceVersion, entry.Title, entry.Status, absent(entry.Owner), list(entry.Tags), list(entry.Incidents))
	})
}

func writeRevision(out io.Writer, headline string, entry project.Revision) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nIdentity: %s\nSchema: %s\nProvenance: %s\nOperation: %s\nParent: %s\nParent identity: %s\n",
			headline, entry.Identity, entry.Schema, entry.Provenance, entry.Operation.Name, entry.Operation.Parent, entry.Operation.ParentIdentity)
	})
}

func writeNote(out io.Writer, headline string, note project.Note) error {
	return writeLines(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nSubject: %s\nTitle: %s\n", headline, absent(note.Subject), note.Title)
		writeBody(w, note.Body)
	})
}

// writeBody prints working text one indented line at a time, so a note that
// runs to several lines stays under the note it belongs to.
func writeBody(w io.Writer, value string) {
	if value == "" {
		fmt.Fprint(w, "    body: none\n")
		return
	}
	fmt.Fprint(w, "    body:\n")
	for line := range strings.SplitSeq(value, "\n") {
		fmt.Fprintf(w, "      %s\n", line)
	}
}

// absent and list keep an unset value visibly unset, so a reader never mistakes
// an empty rendering for a value the project does not hold.
func absent(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func list(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func projectOneArgument(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("project subcommand requires exactly one project directory")
	}
	return nil
}

func projectTwoArguments(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errors.New("project subcommand requires a project directory and one name")
	}
	return nil
}
