package hub_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestConnectedSuiteCustomerCLIPropagatesActualPassFailureAndOfflineProof(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, c, request, authority := provisionConnectedSuite(t, f)
	binary := journeyExecutable(t, "READMIT_ACCEPTANCE_BINARY", "..", "readmit")
	configPath := filepath.Join(h.Root, "runner.json")
	connectedlab.WriteJSON(t, configPath, c)
	operation := testlicense.New(t)
	document, e := os.ReadFile("../docs/customer-ci.md")
	if e != nil {
		t.Fatal(e)
	}
	examples := []string{}
	for _, line := range strings.Split(string(document), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci `) && strings.Contains(line, "--runner-config") {
			examples = append(examples, line)
		}
	}
	if len(examples) != 3 || examples[0] != examples[1] || examples[0] != examples[2] {
		t.Fatal("POSIX/GitHub/Azure examples do not invoke the same single connected suite command")
	}
	for index, mode := range []string{"encounter", ""} {
		h.Lab.SetDownstream(mode)
		output := filepath.Join(h.Root, "ci-"+string(rune('a'+index)))
		instance := "pipeline-" + string(rune('a'+index))
		command := exec.Command(binary, "--operation-policy", operation, "suite", "ci", request.Path, "--environment", request.Environment, "--output", output, "--runner-config", configPath, "--authority", authority, "--promotion", request.Promotion, "--promotion-identity", request.PromotionIdentity, "--revision", request.Revision, "--instance", instance, "--send", "--deadline", "2m")
		if runtime.GOOS != "windows" {
			command = exec.Command("sh", "-c", examples[0])
			command.Env = append(os.Environ(), "READMIT_BIN="+binary, "OPERATION_POLICY="+operation, "SUITE_FILE="+request.Path, "SUITE_ENVIRONMENT="+request.Environment, "RUN_DIRECTORY="+output, "RUNNER_CONFIG="+configPath, "RUNNER_AUTHORITY="+authority, "PROMOTION_FILE="+request.Promotion, "PROMOTION_IDENTITY="+request.PromotionIdentity, "TARGET_REVISION="+request.Revision, "DISPATCH_ID="+instance)
		}
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		want := 0
		if mode == "" {
			want = 1
		}
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		result, decodeErr := suite.DecodeCI(stdout.Bytes())
		if code != want || decodeErr != nil || result.ExitCode != want || result.Jobs != 1 || result.Executed != 1 || h.Lab.Creates.Load() != 2 {
			t.Fatalf("actual CLI expected %d: exit%d, %+v %v; stdout=%s stderr=%s", want, code, result, decodeErr, stdout.String(), stderr.String())
		}
		for _, private := range []string{"RUNNER-1", "NATIVE-RUNNER-1", h.Root, h.Lab.Base(), f.token} {
			if strings.Contains(stdout.String()+stderr.String(), private) {
				t.Fatal("CI output leaked private data")
			}
		}
		opened, e := customerrunner.InspectConnectedCI(t.Context(), output)
		if e != nil || opened.ExitCode != want {
			t.Fatal("linked CI proof cannot be inspected", e)
		}
		app := desktop.New(nil, desktop.ShellDocuments{Folder: t.TempDir()})
		view := app.InspectCIResults(output)
		if view.State != desktop.Completed || view.CI == nil || view.CI.ExitCode != want {
			t.Fatal("normal facade CI import did not read actual linked proof", view)
		}
		junit, e := os.ReadFile(filepath.Join(output, "junit.xml"))
		if e != nil || !strings.Contains(string(junit), "saved-suite-gate") {
			t.Fatal("fixed aggregate JUnit absent", e)
		}
		manifest, e := os.ReadFile(filepath.Join(output, "manifest.json"))
		if e != nil {
			t.Fatal(e)
		}
		var link struct {
			Execution string `json:"execution"`
		}
		if json.Unmarshal(manifest, &link) != nil || len(link.Execution) != 64 {
			t.Fatal("CI manifest omitted actual execution identity")
		}
		inspect := exec.Command(binary, "suite", "inspect", output)
		if _, e = inspect.Output(); e != nil {
			t.Fatal("public offline inspection failed", e)
		}
	}
}
