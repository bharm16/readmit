package tests

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// The desktop import operation and the import command have to agree on what a
// mixed container becomes: the same totals, the same quarantine reasons, and
// the same retained bytes. A timestamp in the receipt is the only thing allowed
// to differ, because each run records when it wrote.
func TestDesktopImportMatchesCLIOnMixedValidMalformedAndDuplicateEvidence(t *testing.T) {
	t.Run("duplicate bytes stay distinct and non-members stay excluded", func(t *testing.T) {
		folder := t.TempDir()
		valid := importFixture("AAA")
		for name, content := range map[string]string{
			"a.hl7":      valid,
			"a-copy.hl7": valid,
			"notes.md":   "operator notes, not evidence",
		} {
			if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		plan := writeDocument(t, t.TempDir(), "plan.json",
			`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".hl7"]}`)
		comparePlanImport(t, plan, nil, []string{folder}, nil)
	})

	t.Run("a malformed frame is quarantined without repairing its bytes", func(t *testing.T) {
		folder := t.TempDir()
		content := "\x0b" + importFixture("ONE") + "\x1c\r" + "\x0bTRUNCATED FRAME"
		if err := os.WriteFile(filepath.Join(folder, "session.mllp"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		plan := writeDocument(t, t.TempDir(), "plan.json",
			`{"schema":"readmit-import-plan/v1","framing":"mllp","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".mllp"]}`)
		comparePlanImport(t, plan, nil, []string{folder}, nil)
	})

	t.Run("duplicate names inside an archive are refused before a case exists", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "duplicate-names.zip")
		writeNamedArchive(t, archive, []namedEntry{
			{name: "same.hl7", content: importFixture("ONE")},
			{name: "same.hl7", content: importFixture("TWO")},
		})
		planPath := writeDocument(t, dir, "plan.json",
			`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"unknown","members":[".hl7"]}`)
		plan := decodeImportPlan(t, planPath)

		_, opErr := operation.ImportPlanPreview(context.Background(), plan, nil, nil, []string{archive})
		if opErr == nil || !strings.Contains(opErr.Error(), "one relative path of regular file bytes") {
			t.Fatalf("operation preview accepted duplicate archive names: %v", opErr)
		}
		stdout, stderr, cliErr := run(t, "import", "--plan", planPath, "--archive", archive, "--preview")
		if cliErr == nil || stdout != "" || !strings.Contains(stderr, "one relative path of regular file bytes") {
			t.Fatalf("cli preview accepted duplicate archive names: %v %s %s", cliErr, stdout, stderr)
		}

		caseDir := filepath.Join(dir, "refused.case")
		receipt := filepath.Join(dir, "refused.json")
		_, _, opErr = operation.ImportPlanCommit(context.Background(), plan, nil, nil, []string{archive}, caseDir, receipt)
		if opErr == nil || !strings.Contains(opErr.Error(), "one relative path of regular file bytes") {
			t.Fatalf("operation commit accepted duplicate archive names: %v", opErr)
		}
		for _, path := range []string{caseDir, receipt} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("a refused import left %s behind", filepath.Base(path))
			}
		}
	})
}

func comparePlanImport(t *testing.T, planPath string, files, folders, archives []string) {
	t.Helper()
	plan := decodeImportPlan(t, planPath)
	ctx := context.Background()
	opPreview, err := operation.ImportPlanPreview(ctx, plan, files, folders, archives)
	if err != nil {
		t.Fatalf("operation preview: %v", err)
	}
	stdout, stderr, err := run(t, importArgs(planPath, files, folders, archives, "", "")...)
	if err != nil || stderr != "" {
		t.Fatalf("cli preview: %v %s", err, stderr)
	}
	cliPreview := readStrictOutput[importer.Preview](t, stdout)
	if opPreview.Totals != cliPreview.Totals {
		t.Fatalf("preview totals differ: operation %+v cli %+v", opPreview.Totals, cliPreview.Totals)
	}

	opDir := t.TempDir()
	opCase := filepath.Join(opDir, "case")
	opReceiptPath := filepath.Join(opDir, "receipt.json")
	_, opReceipt, err := operation.ImportPlanCommit(ctx, plan, files, folders, archives, opCase, opReceiptPath)
	if err != nil {
		t.Fatalf("operation commit: %v", err)
	}
	cliDir := t.TempDir()
	cliCase := filepath.Join(cliDir, "case")
	cliReceiptPath := filepath.Join(cliDir, "receipt.json")
	if _, stderr, err = run(t, importArgs(planPath, files, folders, archives, cliCase, cliReceiptPath)...); err != nil || stderr != "" {
		t.Fatalf("cli commit: %v %s", err, stderr)
	}
	cliReceipt := readStrictOutput[importer.Receipt](t, string(mustRead(t, cliReceiptPath)))
	if opReceipt.Totals != cliReceipt.Totals {
		t.Fatalf("receipt totals differ: operation %+v cli %+v", opReceipt.Totals, cliReceipt.Totals)
	}
	if !slices.Equal(quarantineReasons(opReceipt.Quarantined), quarantineReasons(cliReceipt.Quarantined)) {
		t.Fatalf("quarantine reasons differ: operation %+v cli %+v", opReceipt.Quarantined, cliReceipt.Quarantined)
	}
	if opBytes, cliBytes := retainedBytes(t, opCase), retainedBytes(t, cliCase); !slices.Equal(opBytes, cliBytes) {
		t.Fatalf("retained bytes differ: operation %q cli %q", opBytes, cliBytes)
	}
}

func importArgs(plan string, files, folders, archives []string, output, receipt string) []string {
	args := []string{"import", "--plan", plan}
	for _, path := range files {
		args = append(args, "--file", path)
	}
	for _, path := range folders {
		args = append(args, "--folder", path)
	}
	for _, path := range archives {
		args = append(args, "--archive", path)
	}
	if output == "" {
		args = append(args, "--preview")
		return args
	}
	return append(args, "--output", output, "--receipt", receipt)
}

func decodeImportPlan(t *testing.T, path string) importer.Plan {
	t.Helper()
	plan, err := importer.DecodePlan(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func quarantineReasons(items []importer.Quarantined) []string {
	reasons := make([]string, 0, len(items))
	for _, item := range items {
		reasons = append(reasons, item.Reason)
	}
	slices.Sort(reasons)
	return reasons
}

func retainedBytes(t *testing.T, caseDir string) []string {
	t.Helper()
	opened, err := bundle.Open(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	payloads := make([]string, 0, len(opened.Events))
	for _, event := range opened.Events {
		raw, err := opened.Raw(event.ID)
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, string(raw))
	}
	slices.Sort(payloads)
	return payloads
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type namedEntry struct {
	name    string
	content string
}

func writeNamedArchive(t *testing.T, path string, entries []namedEntry) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		member, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
