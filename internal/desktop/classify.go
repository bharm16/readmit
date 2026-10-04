package desktop

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/protect"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
	"github.com/bharm16/readmit/internal/sharing"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

// maxSchemaSniffBytes bounds how far into a regular file the listing looks for
// the contract the file declares. Listing never verifies evidence and never
// reads a whole file: the fixed-name documents readmit writes declare their
// contract within this window, and anything that does not is listed as
// unsupported with the reason, exactly as before.
const maxSchemaSniffBytes = 4096

// schemaMarkerFiles are the fixed-name records a retained artifact directory
// holds beside its evidence. They locate what a directory claims to be the
// same way a project document locates a project: one canonical name, never
// concluded from an arbitrary file name. A durable run and a result are named
// before these, by the evidence opener's own rule. A prepared rerun's marker is
// read by its own reader here; the runnable files it names are not verified by
// listing.
var schemaMarkerFiles = []struct {
	name string
	kind Kind
}{
	{"review.json", ReviewArtifact},
	{diagnose.ReportName, DiagnosisArtifact},
	{"machine.json", CorrelationReviewArtifact},
	// A prepared suite directory retains the suite it compiled beside its
	// queue and generated specifications.
	{"suite.json", SuiteArtifact},
	// A sealed investigation packet and a portable review both carry their
	// manifest under one canonical name; the manifest's own contract refines
	// which of the two the directory is called. A completed derived export, a
	// local support bundle and an encrypted transfer package each carry their
	// own canonical record the same way.
	{"manifest.json", PacketArtifact},
	{"preparation.json", PreparedRerunArtifact},
	{"export-review.json", DerivedExportArtifact},
	{"support.json", SupportArtifact},
	{"transfer.json", TransferPackageArtifact},
}

// declaredSchemas are the contracts a regular file can declare that this
// release offers an applicable picker or a coherent context for. An entry
// whose contract is on this list is offered to the panels that apply; an
// entry whose contract is not is unsupported here, with the reason, exactly as
// every other entry this window does not open. The profile and pack versions
// come from the evaluator that reads them, so a new version is offered here
// as soon as it is readable.
var declaredSchemas = func() map[string]Kind {
	m := map[string]Kind{
		FHIRProfileSchema:          ProfileArtifact,
		FHIRProfilePackageSchema:   PackageArtifact,
		index.Schema:               IndexArtifact,
		replay.TargetSchema:        TargetArtifact,
		replay.TargetSchemaV2:      TargetArtifact,
		replay.TargetSchemaV3:      TargetArtifact,
		correlate.RulesSchema:      RulesArtifact,
		transform.PlanSchema:       PlanArtifact,
		testrunner.SpecSchema:      SpecArtifact,
		profilepackage.Schema:      PackageArtifact,
		sequenceanalysis.Schema:    AnalysisArtifact,
		secret.Schema:              SecretArtifact,
		sendpolicy.PolicySchema:    PolicyArtifact,
		fixturereset.PlanSchema:    ResetArtifact,
		fixturereset.OutcomeSchema: ResetArtifact,

		diff.PolicySchema:             NormalizationArtifact,
		diagnose.ConfigSchema:         DiagnoseConfigArtifact,
		findingreview.DecisionsSchema: DecisionsArtifact,

		// A suite document is what the durable-run panels execute a whole
		// environment of; its released-expectation references are the separate
		// pin set that makes one an approved suite. A released test version, a
		// coverage document and a promotion approval are the suite workflow's own
		// artifacts; the suite panel opens each through its own strict reader.
		suite.Schema:          SuiteArtifact,
		suite.ConnectedSchema: SuiteArtifact,
		suite.ReleasesSchema:  SuiteReleasesArtifact,
		expectation.Schema:    SuiteArtifact,
		suite.CoverageSchema:  SuiteArtifact,
		suite.PromotionSchema: SuiteArtifact,

		// A protection document registers references to keys readmit never holds,
		// and a sharing policy declares what a support summary may be prepared
		// for; the protection and support panels offer each through its own strict
		// reader.
		protect.Schema:         ProtectionArtifact,
		sharing.PolicySchema:   SharingPolicyArtifact,
		redact.PolicySchema:    RedactPolicyArtifact,
		redact.InventorySchema: RedactInventoryArtifact,
	}
	for _, schema := range profileeval.ProfileSchemas() {
		m[schema] = ProfileArtifact
	}
	for _, schema := range profileeval.PackSchemas() {
		m[schema] = PackArtifact
	}
	return m
}()

// classify reports what one workspace entry declares, beyond what the case
// reader and the two project documents already answer. A directory holding a
// fixed-name record of a retained artifact is named as that artifact; a
// regular file declaring a contract within the sniff bound is named as that
// contract's kind. Both are claims the listing makes and never verifications:
// opening the entry remains the verification step, and an entry classified
// here is not admitted anywhere by this alone. A prepared rerun's marker and
// checksum are read, without verifying or running its referenced evidence.
func classify(root, name string, isDir bool) (Kind, bool) {
	path := filepath.Join(root, name)
	if isDir {
		if connectedIndividualArtifact(path) {
			return JobArtifact, true
		}
		// A durable run carries its engine pin beside the journal and the
		// result directory it retains; a plain result holds only its result
		// record. Neither is read here.
		switch runresult.ExecutionFamily(path, runresult.RegularFile) {
		case runresult.JobFamily:
			return JobArtifact, true
		case runresult.ResultFamily:
			return ResultArtifact, true
		}
		for _, marker := range schemaMarkerFiles {
			if info, err := os.Lstat(filepath.Join(path, marker.name)); err == nil && info.Mode().IsRegular() {
				// A marker whose own contract declines the directory does not
				// decide it: a portable review holds a report.json rendering
				// and a manifest.json manifest, and the manifest is what names
				// it. The first marker that accepts the directory wins.
				if kind, known := refinedMarker(filepath.Join(path, marker.name), marker.kind); known {
					return kind, true
				}
			}
		}
		return UnsupportedArtifact, false
	}
	schema, ok := sniffSchema(path)
	if !ok {
		return UnsupportedArtifact, false
	}
	kind, known := declaredSchemas[schema]
	return kind, known
}

// suiteSchemaRoles are the suite workflow's flat documents by the contract
// each declares.
var suiteSchemaRoles = map[string]SuiteRole{
	suite.Schema:          SuiteDefinitionRole,
	suite.ConnectedSchema: SuiteDefinitionRole,
	suite.ReleasesSchema:  SuiteReleasesRole,
	expectation.Schema:    TestReleaseRole,
	suite.CoverageSchema:  SuiteCoverageRole,
	suite.PromotionSchema: SuitePromotionRole,
}

// suiteRole names which suite artifact one suite-kind entry declares: a
// directory named by its suite.json marker is a prepared suite, and a flat
// document is named by the contract it declares within the sniff bound. Like
// classify, it is a claim the listing makes; the entry's own reader still
// decides whether it is one.
func suiteRole(root, name string, isDir bool, kind Kind) SuiteRole {
	if kind != SuiteArtifact && kind != SuiteReleasesArtifact {
		return ""
	}
	if isDir {
		if kind == SuiteArtifact {
			return PreparedSuiteRole
		}
		return ""
	}
	schema, ok := sniffSchema(filepath.Join(root, name))
	if !ok {
		return ""
	}
	return suiteSchemaRoles[schema]
}

// refinedMarker lets a marker file's own declared contract refine what the
// directory is called, exactly as a flat file's declared contract already
// does. Two names carry more than one contract: review.json is an export
// review unless it declares the finding-review contract, so a finding review
// never lists as the export review it is not; report.json names a
// diagnosis only when it declares a diagnosis contract, so a directory
// holding some other report.json stays unsupported here, exactly as before
// this release read any report.json at all; and manifest.json names a sealed
// investigation packet, a portable review or a synthetic demonstration
// packet by the contract it declares, so any other manifest this release's
// packet panels do not open stays unsupported. Every other marker keeps its one
// kind, and nothing here verifies the directory — opening it still does.
func refinedMarker(path string, kind Kind) (Kind, bool) {
	switch kind {
	case ReviewArtifact:
		if schema, ok := sniffSchema(path); ok && schema == findingreview.Schema {
			return FindingReviewArtifact, true
		}
		return ReviewArtifact, true
	case DiagnosisArtifact:
		schema, ok := sniffSchema(path)
		if ok {
			switch schema {
			case diagnose.Schema:
				return DiagnosisArtifact, true
			case diagnose.GroupsSchema:
				return DiagnosisGroupsArtifact, true
			}
		}
		return UnsupportedArtifact, false
	case PacketArtifact:
		switch schema, ok := sniffSchema(path); {
		case ok && schema == suite.ConnectedExecutionSchema:
			return JobArtifact, true
		case ok && schema == report.RetainedSchema:
			return PacketArtifact, true
		case ok && (schema == report.ReviewSchema || schema == report.ReviewSchemaV3):
			return PortableReviewArtifact, true
		case ok && schema == report.Schema:
			return SyntheticPacketArtifact, true
		}
		return UnsupportedArtifact, false
	case PreparedRerunArtifact:
		if _, err := report.ReadPreparation(filepath.Dir(path)); err == nil {
			return PreparedRerunArtifact, true
		}
		return UnsupportedArtifact, false
	case DerivedExportArtifact:
		if schema, ok := sniffSchema(path); ok && schema == redact.ExportSchema {
			return DerivedExportArtifact, true
		}
		return UnsupportedArtifact, false
	case SupportArtifact:
		if schema, ok := sniffSchema(path); ok && schema == sharing.Schema {
			return SupportArtifact, true
		}
		return UnsupportedArtifact, false
	case TransferPackageArtifact:
		if schema, ok := sniffSchema(path); ok && schema == protect.TransferSchema {
			return TransferPackageArtifact, true
		}
		return UnsupportedArtifact, false
	}
	return kind, true
}

// sniffSchema reads a bounded window of a regular file and reports the
// contract a `schema` member declares within it. It is a claim about the
// bytes, not an acceptance of them: the readers that open each contract
// decide what a file really is, and this only decides what the listing calls
// it. Only what is a regular file once any link is followed is opened, so a
// FIFO or a link to one named as an entry is unsupported rather than a read
// that never returns.
func sniffSchema(path string) (string, bool) {
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	window := make([]byte, maxSchemaSniffBytes)
	read, err := io.ReadFull(file, window)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false
	}
	return schemaIn(window[:read])
}

// schemaIn is the schema the head of a JSON document declares.
func schemaIn(head []byte) (string, bool) {
	const member = `"schema"`
	at := bytes.Index(head, []byte(member))
	if at < 0 {
		return "", false
	}
	rest := head[at+len(member):]
	rest = bytes.TrimLeft(rest, " \t\r\n:")
	if len(rest) < 2 || rest[0] != '"' {
		return "", false
	}
	end := bytes.IndexByte(rest[1:], '"')
	if end < 0 {
		return "", false
	}
	return string(rest[1 : 1+end]), true
}
