package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/spf13/cobra"
)

// installedStore is whichever entitlement store version a directory holds.
// A v1 store is read by the v1 reader and a v2 store by the v2 reader; neither
// is migrated, and a version neither reads is reported as exactly that.
type installedStore interface {
	id() string
	schema() string
	// activated names what this store activated as: a device under v1, a
	// named author on a device under v2.
	activated() string
	released() time.Time
	export(destination string) error
	renew(data []byte, trust entitlement.Trust) error
	release(at time.Time) error
	// report verifies the installed entitlement for what it activated as and
	// writes the grant under its own version's report.
	report(out io.Writer, headline string, trust entitlement.Trust, require string) error
}

func openStore(path string) (installedStore, error) {
	v1, err := entitlement.Open(path)
	if err == nil {
		return storeV1{v1}, nil
	}
	if !errors.Is(err, entitlement.ErrUnsupportedVersion) {
		return nil, err
	}
	v2, err := entitlement.OpenV2(path)
	if err != nil {
		return nil, err
	}
	return storeV2{v2}, nil
}

type storeV1 struct{ *entitlement.Store }

func (s storeV1) id() string                                       { return s.Claims.ID }
func (s storeV1) schema() string                                   { return entitlement.Schema }
func (s storeV1) activated() string                                { return s.Activation.Device }
func (s storeV1) released() time.Time                              { return s.Activation.Released }
func (s storeV1) export(destination string) error                  { _, err := s.Export(destination); return err }
func (s storeV1) renew(data []byte, trust entitlement.Trust) error { return s.Renew(data, trust) }
func (s storeV1) release(at time.Time) error                       { return s.Release(at) }
func (s storeV1) report(out io.Writer, headline string, trust entitlement.Trust, require string) error {
	grant, err := s.Grant(trust)
	if err != nil {
		return err
	}
	return writeGrant(out, headline, grant, s.Activation.Device, require, activation(s.Store))
}

type storeV2 struct{ *entitlement.StoreV2 }

func (s storeV2) id() string                                       { return s.Claims.ID }
func (s storeV2) schema() string                                   { return entitlement.SchemaV2 }
func (s storeV2) activated() string                                { return s.Activation.Author + " on " + s.Activation.Device }
func (s storeV2) released() time.Time                              { return s.Activation.Released }
func (s storeV2) export(destination string) error                  { _, err := s.Export(destination); return err }
func (s storeV2) renew(data []byte, trust entitlement.Trust) error { return s.Renew(data, trust) }
func (s storeV2) release(at time.Time) error                       { return s.Release(at) }
func (s storeV2) report(out io.Writer, headline string, trust entitlement.Trust, require string) error {
	grant, err := s.Grant(trust)
	if err != nil {
		return err
	}
	installed := "imported " + licenseTimestamp(s.Activation.Imported)
	if !s.Activation.Released.IsZero() {
		installed = "released " + licenseTimestamp(s.Activation.Released)
	}
	return writeGrantV2(out, headline, grant, s.Activation.Author, s.Activation.Device, require, installed)
}

// writeGrantV2 reports one verified v2 entitlement the way writeGrant reports a
// v1 one: every line is data the document declares or a state decided from it.
// The author line reports an assignment only when one was asked about.
func writeGrantV2(out io.Writer, headline string, grant entitlement.GrantV2, author, device, require, installed string) error {
	claims := grant.Claims
	at := licenseNow()
	selected := "not selected"
	switch {
	case author != "" && device != "":
		if err := grant.Assigned(author, device); err != nil {
			return err
		}
		selected = author + " on " + device + " (assigned)"
	case author != "":
		assignment, err := grant.Author(author)
		if err != nil {
			return err
		}
		selected = author + " (" + list(assignment.Devices) + ")"
	case device != "":
		return errors.New("a v2 entitlement assigns a device to a named author; select it with --author and --device together")
	}
	if require != "" {
		if err := grant.Allows(require, at); err != nil {
			return err
		}
	}
	return writeLicense(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nDocument: %s\nOrganization: %s\nPlan: %s\nIssue sequence: %d\nSigned by: %s (%s, %s)\n",
			headline, entitlement.SchemaV2, claims.Organization, claims.Plan, claims.Sequence, grant.KeyID, entitlement.Algorithm, grant.KeyStatus)
		fmt.Fprintf(w, "Term: %s to %s\nGrace: %s\nState: %s\n",
			licenseTimestamp(claims.NotBefore), licenseTimestamp(claims.Expires), grace(claims.GraceDays, claims.GraceEnds()), grant.StateAt(at))
		fmt.Fprintf(w, "Authors: %d seats, %d devices per seat\nAssignments: %s\nRunners: %d instances\nAuthorities: %s\nCapabilities: %s\nAuthor: %s\n",
			claims.Authors.Seats, claims.Authors.DevicesPerSeat, assignments(claims.Authors.Assignments),
			claims.Runners.Instances, authorities(claims.Runners.Authorities), list(claims.Capabilities), selected)
		writeGrantTrailer(w, installed, require)
	})
}

func assignments(named []entitlement.Assignment) string {
	if len(named) == 0 {
		return "none"
	}
	lines := make([]string, len(named))
	for i, assignment := range named {
		lines[i] = assignment.Author + " (" + list(assignment.Devices) + ")"
	}
	return strings.Join(lines, ", ")
}

func authorities(named []entitlement.Authority) string {
	if len(named) == 0 {
		return "none"
	}
	lines := make([]string, len(named))
	for i, authority := range named {
		lines[i] = authority.ID + " (" + strconv.Itoa(authority.Instances) + ")"
	}
	return strings.Join(lines, ", ")
}

// licenseRunner is the runner authority's side of a v2 entitlement: a local
// admission record the organization keeps, where execution instances are
// admitted against the granted capacity, released, renewed and reconciled.
func licenseRunner() *cobra.Command {
	command := &cobra.Command{
		Use:         "runner",
		Short:       "Admit and release execution instances against a v2 entitlement's runner capacity",
		Args:        cobra.NoArgs,
		Annotations: declare(capabilityFree),
		RunE: func(_ *cobra.Command, _ []string) error {
			return usage("license runner requires a subcommand: init, admit, renew, release, reconcile, or show")
		},
	}
	command.AddCommand(runnerInit(), runnerAdmit(), runnerRenew(), runnerRelease(), runnerReconcile(), runnerShow())
	return command
}

func runnerInit() *cobra.Command {
	var trustPath, authority, output string
	command := &cobra.Command{
		Use:         "init ENTITLEMENT --trust TRUST_STORE --authority ID --output NEW_FILE",
		Annotations: declare(capabilityFree),
		Short:       "Start an empty admission record for one runner authority the entitlement names",
		Args:        licenseOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return usage("license runner init requires --output with a new file")
			}
			if authority == "" {
				return usage("license runner init requires --authority with the identifier this entitlement names")
			}
			grant, err := readGrantV2(args[0], trustPath)
			if err != nil {
				return err
			}
			record, err := entitlement.CreateAdmissions(output, grant, authority)
			if err != nil {
				return err
			}
			return writeAdmissions(cmd.OutOrStdout(), "Admission record created: "+authority, record, grant)
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&authority, "authority", "", "Runner authority identifier, which the entitlement must name")
	command.Flags().StringVar(&output, "output", "", "New admission record file (never overwrite)")
	return command
}

func runnerAdmit() *cobra.Command {
	var trustPath, instance, lease string
	command := &cobra.Command{
		Use:         "admit RECORD ENTITLEMENT --trust TRUST_STORE --instance ID --lease DURATION",
		Annotations: declare(capabilityFree),
		Short:       "Admit one execution instance if the authority has a free instance",
		Args:        runnerTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			until, err := leaseEnd(lease)
			if err != nil {
				return err
			}
			if instance == "" {
				return usage("license runner admit requires --instance with the identifier of the instance to admit")
			}
			grant, err := readGrantV2(args[1], trustPath)
			if err != nil {
				return err
			}
			record, err := entitlement.OpenAdmissions(args[0])
			if err != nil {
				return err
			}
			if err := record.Admit(grant, instance, licenseNow(), until); err != nil {
				return err
			}
			return writeAdmissions(cmd.OutOrStdout(), "Instance admitted: "+instance, record, grant)
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&instance, "instance", "", "Execution instance identifier the organization chooses")
	command.Flags().StringVar(&lease, "lease", "", "How long the admission holds before it is stale unless renewed (for example 30m or 2h)")
	return command
}

func runnerRenew() *cobra.Command {
	var trustPath, instance, lease string
	command := &cobra.Command{
		Use:         "renew RECORD ENTITLEMENT --trust TRUST_STORE --instance ID --lease DURATION",
		Annotations: declare(capabilityFree),
		Short:       "Extend an admitted instance's lease inside the term; a stale instance reporting in becomes active again",
		Args:        runnerTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			until, err := leaseEnd(lease)
			if err != nil {
				return err
			}
			grant, err := readGrantV2(args[1], trustPath)
			if err != nil {
				return err
			}
			return settleInstance(cmd, args[0], instance, "Instance renewed: ", func(record *entitlement.Admissions) error {
				return record.Renew(grant, instance, licenseNow(), until)
			})
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	command.Flags().StringVar(&instance, "instance", "", "Execution instance identifier")
	command.Flags().StringVar(&lease, "lease", "", "How long the renewed admission holds before it is stale unless renewed again")
	return command
}

func runnerRelease() *cobra.Command {
	var instance string
	command := &cobra.Command{
		Use:         "release RECORD --instance ID",
		Annotations: declare(capabilityFree),
		Short:       "Record that an instance finished or was cancelled and hand its capacity back",
		Args:        licenseOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			return settleInstance(cmd, args[0], instance, "Instance released: ", func(record *entitlement.Admissions) error {
				return record.Release(instance, licenseNow())
			})
		},
	}
	command.Flags().StringVar(&instance, "instance", "", "Execution instance identifier")
	return command
}

func runnerReconcile() *cobra.Command {
	var instance string
	command := &cobra.Command{
		Use:         "reconcile RECORD --instance ID",
		Annotations: declare(capabilityFree),
		Short:       "Record that an operator established an instance is no longer running and settle its admission",
		Args:        licenseOneArgument,
		RunE: func(cmd *cobra.Command, args []string) error {
			return settleInstance(cmd, args[0], instance, "Instance reconciled: ", func(record *entitlement.Admissions) error {
				return record.Reconcile(instance, licenseNow())
			})
		},
	}
	command.Flags().StringVar(&instance, "instance", "", "Execution instance identifier")
	return command
}

func runnerShow() *cobra.Command {
	var trustPath string
	command := &cobra.Command{
		Use:         "show RECORD ENTITLEMENT --trust TRUST_STORE",
		Annotations: declare(capabilityFree),
		Short:       "Report what the authority holds against the capacity the entitlement grants it",
		Args:        runnerTwoArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			grant, err := readGrantV2(args[1], trustPath)
			if err != nil {
				return err
			}
			record, err := entitlement.OpenAdmissions(args[0])
			if err != nil {
				return err
			}
			return writeAdmissions(cmd.OutOrStdout(), "Admission record: "+record.Record.Authority, record, grant)
		},
	}
	command.Flags().StringVar(&trustPath, "trust", "", "Trust store of vendor signing keys to verify against")
	return command
}

// settleInstance applies one settlement to a record and reports the
// admissions afterwards. Releasing and reconciling need no entitlement: they
// change what is held, never what was granted. Renewal reads one because it
// follows the term.
func settleInstance(cmd *cobra.Command, path, instance, headline string, apply func(*entitlement.Admissions) error) error {
	if instance == "" {
		return usage("license runner requires --instance with the identifier of an admitted instance")
	}
	record, err := entitlement.OpenAdmissions(path)
	if err != nil {
		return err
	}
	if err := apply(record); err != nil {
		return err
	}
	return writeLicense(cmd.OutOrStdout(), func(w io.Writer) {
		fmt.Fprintf(w, "%s%s\nRunner authority: %s\n", headline, instance, record.Record.Authority)
		writeAdmissionLines(w, record, licenseNow())
	})
}

func readGrantV2(path, trustPath string) (entitlement.GrantV2, error) {
	trust, err := readTrust(trustPath)
	if err != nil {
		return entitlement.GrantV2{}, err
	}
	data, err := readInputFile(path, entitlement.MaxDocumentBytes)
	if err != nil {
		return entitlement.GrantV2{}, err
	}
	return entitlement.VerifyV2(data, trust)
}

// leaseEnd turns a declared lease into the instant it ends. The lease is
// bounded by the contract; the caller chooses it, and readmit chooses no
// default.
func leaseEnd(lease string) (time.Time, error) {
	if lease == "" {
		return time.Time{}, usage("license runner requires --lease with how long the admission holds, for example 30m")
	}
	duration, err := time.ParseDuration(lease)
	if err != nil || duration < time.Second || duration%time.Second != 0 {
		return time.Time{}, usage("lease must be a whole number of seconds of at least 1s, for example 30m")
	}
	if duration > entitlement.MaxLease {
		return time.Time{}, entitlement.ErrLeaseTooLong
	}
	return licenseNow().Add(duration), nil
}

func writeAdmissions(out io.Writer, headline string, record *entitlement.Admissions, grant entitlement.GrantV2) error {
	at := licenseNow()
	held, err := record.Capacity(grant, at)
	if err != nil {
		return err
	}
	return writeLicense(out, func(w io.Writer) {
		fmt.Fprintf(w, "%s\nRunner authority: %s\nOrganization: %s\nEntitlement: %s (%s)\nCapacity: %d instances; %d active, %d stale, %d free\n",
			headline, record.Record.Authority, record.Record.Organization, grant.Claims.ID, entitlement.SchemaV2,
			held.Instances, held.Active, held.Stale, held.Free())
		writeAdmissionLines(w, record, at)
	})
}

func writeAdmissionLines(w io.Writer, record *entitlement.Admissions, at time.Time) {
	fmt.Fprint(w, "Admissions:")
	if len(record.Record.Admissions) == 0 {
		fmt.Fprint(w, " none")
	}
	fmt.Fprint(w, "\n")
	for _, admission := range record.Record.Admissions {
		switch admission.StateAt(at) {
		case entitlement.AdmissionActive:
			fmt.Fprintf(w, "  %s: active until %s\n", admission.Instance, licenseTimestamp(admission.LeaseUntil))
		case entitlement.AdmissionStale:
			fmt.Fprintf(w, "  %s: stale since %s (lease ended without release; renew or reconcile to settle it)\n", admission.Instance, licenseTimestamp(admission.LeaseUntil))
		case entitlement.AdmissionReleased:
			fmt.Fprintf(w, "  %s: released %s\n", admission.Instance, licenseTimestamp(admission.Released))
		case entitlement.AdmissionReconciled:
			fmt.Fprintf(w, "  %s: reconciled %s\n", admission.Instance, licenseTimestamp(admission.Reconciled))
		}
	}
	fmt.Fprint(w, "Authority: this record is the authority; a copy of it elsewhere is a second authority with the same capacity, which local verification cannot detect\n")
}

func runnerTwoArguments(_ *cobra.Command, args []string) error {
	if len(args) != 2 {
		return errors.New("license runner subcommand requires an admission record and one entitlement")
	}
	return nil
}
