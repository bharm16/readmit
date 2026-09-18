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
			return errors.New("project requires a subcommand: init, settings, add, update, or show")
		},
	}
	command.AddCommand(projectInit(ran), projectSettings(ran), projectAdd(ran), projectUpdate(ran), projectShow(ran))
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
			entry, err := verifiedCase(opened.Root, args[1])
			if err != nil {
				return err
			}
			entry.Title = title
			entry.InterfaceVersion = version
			entry.Owner = owner
			entry.Status = project.Status(status)
			entry.Tags = tags
			entry.Incidents = incidents
			document, err := project.AddCase(opened.Document, entry)
			if err != nil {
				return err
			}
			if err := opened.Save(document); err != nil {
				return err
			}
			return writeCase(cmd.OutOrStdout(), "Case registered: "+entry.Name, registered(document, entry.Name))
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
				change.Tags = &tags
			}
			if cmd.Flags().Changed("incident") {
				change.Incidents = &incidents
			}
			if change.Empty() {
				return errors.New("project update requires at least one change")
			}
			opened, err := project.Open(args[0])
			if err != nil {
				return err
			}
			document, err := project.UpdateCase(opened.Document, args[1], change)
			if err != nil {
				return err
			}
			if err := opened.Save(document); err != nil {
				return err
			}
			return writeCase(cmd.OutOrStdout(), "Case updated: "+args[1], registered(document, args[1]))
		},
	}
	command.Flags().StringVar(&title, "title", "", "Case title")
	command.Flags().StringVar(&version, "interface-version", "", "Declared interface version")
	command.Flags().StringVar(&owner, "owner", "", "Case owner")
	command.Flags().StringVar(&status, "status", "", "Case status: open, investigating, resolved, or closed")
	command.Flags().StringArrayVar(&tags, "tag", nil, "Replace the tags with these; repeat for more")
	command.Flags().StringArrayVar(&incidents, "incident", nil, "Replace the linked incidents with these; repeat for more")
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
			w := bufio.NewWriter(cmd.OutOrStdout())
			writeSettings(w, "Project: "+opened.Document.Settings.Title, opened.Document)
			for _, entry := range opened.Document.Cases {
				fmt.Fprintf(w, "  %s evidence=%s identity=%s schema=%s provenance=%s interface=%s status=%s owner=%s\n",
					entry.Name, evidenceFor(opened.Root, entry), entry.Identity, entry.Schema, entry.Provenance, entry.InterfaceVersion, entry.Status, absent(entry.Owner))
				fmt.Fprintf(w, "    title: %s\n    tags: %s\n    incidents: %s\n", entry.Title, list(entry.Tags), list(entry.Incidents))
			}
			if err := w.Flush(); err != nil {
				return errors.New("cannot write project output")
			}
			return nil
		},
	}
}

// verifiedCase reads the evidence facts a project records from the bundle
// itself. The provenance mode is the one the verified manifest declares, so it
// is never inferred from the directory name or supplied on the command line.
func verifiedCase(root, name string) (project.Case, error) {
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return project.Case{}, errors.New("a case must be named by one directory entry of the project")
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return project.Case{}, errors.New("the case could not be verified as complete, unmodified evidence")
	}
	return project.Case{
		Name:       name,
		Identity:   opened.Identity,
		Schema:     opened.Manifest.Schema,
		Provenance: string(opened.Manifest.Provenance.Mode),
	}, nil
}

// evidenceFor re-verifies one registered case through the same reader that
// accepted it. A recorded identity is reported exactly as recorded whatever
// this finds: `show` reports, and never rewrites what a project recorded.
func evidenceFor(root string, entry project.Case) evidenceState {
	path, err := artifactpath.Child(root, entry.Name)
	if err != nil {
		return evidenceMissing
	}
	opened, err := bundle.Open(path)
	if err != nil {
		return evidenceUnreadable
	}
	if opened.Identity != entry.Identity {
		return evidenceChanged
	}
	return evidenceVerified
}

func registered(document project.Document, name string) project.Case {
	index := slices.IndexFunc(document.Cases, func(c project.Case) bool { return c.Name == name })
	return document.Cases[index]
}

func writeProject(out io.Writer, headline string, document project.Document) error {
	w := bufio.NewWriter(out)
	writeSettings(w, headline, document)
	if err := w.Flush(); err != nil {
		return errors.New("cannot write project output")
	}
	return nil
}

func writeSettings(w io.Writer, headline string, document project.Document) {
	fmt.Fprintf(w, "%s\nDocument: %s\nInterface versions: %s\nDefault interface version: %s\nDefault owner: %s\nCases: %d\n",
		headline, document.Schema, list(document.InterfaceVersions), absent(document.Settings.DefaultInterfaceVersion), absent(document.Settings.DefaultOwner), len(document.Cases))
}

func writeCase(out io.Writer, headline string, entry project.Case) error {
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "%s\nIdentity: %s\nSchema: %s\nProvenance: %s\nInterface version: %s\nTitle: %s\nStatus: %s\nOwner: %s\nTags: %s\nIncidents: %s\n",
		headline, entry.Identity, entry.Schema, entry.Provenance, entry.InterfaceVersion, entry.Title, entry.Status, absent(entry.Owner), list(entry.Tags), list(entry.Incidents))
	if err := w.Flush(); err != nil {
		return errors.New("cannot write project output")
	}
	return nil
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
		return errors.New("project subcommand requires a project directory and one case name")
	}
	return nil
}
