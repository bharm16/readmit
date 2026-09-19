package suite_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/suite"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/durablerun"
)

func TestPublicCISuiteReportsPrivateGateAndRetainsEvidence(t *testing.T) {
	for _, code := range []string{"AA", "AE"} {
		t.Run(code, func(t *testing.T) {
			dir, doc := fixture(t, peer(t, func(c net.Conn) { ack(c, code) }))
			doc.Owner = "PRIVATE-PATIENT-OWNER"
			doc.Environments[0].Site = "PRIVATE-PATIENT-SITE"
			write(t, filepath.Join(dir, "suite.json"), doc)
			out := filepath.Join(dir, "ci-run")
			var stdout, stderr bytes.Buffer
			err := cli.Execute("test", []string{"suite", "ci", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--send"}, &stdout, &stderr)
			want := 0
			if code == "AE" {
				want = 1
			}
			if cli.ExitCode(err) != want {
				t.Fatalf("exit %d: %v %s", cli.ExitCode(err), err, stderr.String())
			}
			var report struct {
				Schema   string `json:"schema"`
				ExitCode int    `json:"exit_code"`
			}
			if e := json.Unmarshal(stdout.Bytes(), &report); e != nil || report.Schema != "readmit-suite-ci/v1" || report.ExitCode != want {
				t.Fatalf("%s %v", stdout.String(), e)
			}
			for _, name := range []string{"ci.json", "junit.xml"} {
				raw, e := os.ReadFile(filepath.Join(out, name))
				if e != nil {
					t.Fatal(e)
				}
				if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "booking") || strings.Contains(string(raw), dir) {
					t.Fatal("CI output disclosed local metadata")
				}
				if name == "junit.xml" {
					var v struct {
						XMLName  xml.Name `xml:"testsuite"`
						Failures int      `xml:"failures,attr"`
					}
					if e := xml.Unmarshal(raw, &v); e != nil || v.Failures != want {
						t.Fatalf("%s %v", raw, e)
					}
				}
			}
			recovery, e := durablerun.Recover(filepath.Join(out, "runs", "booking-one"))
			if e != nil || !recovery.Terminal || recovery.SafeToRepeat {
				t.Fatalf("%+v %v", recovery, e)
			}
			stdout.Reset()
			stderr.Reset()
			err = cli.Execute("test", []string{"suite", "ci", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--send"}, &stdout, &stderr)
			if cli.ExitCode(err) != 2 || strings.Contains(stderr.String(), dir) {
				t.Fatal("existing output was restarted or disclosed")
			}
		})
	}
}

func TestCIGateExclusionsAndInvalidDeclarationsNeverPass(t *testing.T) {
	for _, exclusion := range []string{"", "quarantined", "disabled", "unsupported", "skipped", "uncovered", "invalid"} {
		t.Run(exclusion, func(t *testing.T) {
			dir, _ := fixture(t, peer(t, func(c net.Conn) { ack(c, "AA") }))
			prepared, e := suite.Prepare(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "preview"))
			if e != nil {
				t.Fatal(e)
			}
			policy := coveragePolicy(t, prepared.Directory)
			raw, _ := os.ReadFile(policy)
			declaration, e := suite.DecodeCoverage(raw)
			if e != nil {
				t.Fatal(e)
			}
			declaration.Requirements = declaration.Requirements[:1]
			if exclusion == "uncovered" {
				declaration.Requirements[0].Jobs = []string{}
			} else if exclusion != "" && exclusion != "invalid" {
				declaration.Exclusions = []suite.Exclusion{{Job: "booking-one", State: exclusion, Reason: "PRIVATE reason", Expires: "2099-01-01T00:00:00Z"}}
			}
			write(t, policy, declaration)
			if exclusion == "invalid" {
				os.WriteFile(policy, []byte(`{"schema":"PRIVATE"}`), 0600)
			}
			out := filepath.Join(dir, "ci")
			var stdout, stderr bytes.Buffer
			err := cli.Execute("test", []string{"suite", "ci", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--requirements", policy, "--send"}, &stdout, &stderr)
			want := 2
			if exclusion == "" {
				want = 0
			}
			if cli.ExitCode(err) != want {
				t.Fatalf("%v %s %s", err, stdout.String(), stderr.String())
			}
			r, e := suite.DecodeCI(stdout.Bytes())
			if e != nil || (exclusion != "" && r.Coverage != "failed") {
				t.Fatalf("%+v %v", r, e)
			}
			if strings.Contains(stdout.String()+stderr.String(), "PRIVATE") {
				t.Fatal("CI output disclosed exclusion or error")
			}
			if exclusion == "invalid" {
				if _, e := os.Stat(out); !os.IsNotExist(e) {
					t.Fatal("invalid coverage sent")
				}
			}
		})
	}
}

func TestCICancelRetainsUncertaintyAndNeverRetries(t *testing.T) {
	received := make(chan struct{})
	dir, doc := fixture(t, peer(t, func(c net.Conn) {
		r, _ := mllp.NewReader(c, 1<<20)
		if _, e := r.ReadFrame(); e == nil {
			close(received)
			io.Copy(io.Discard, c)
		}
	}))
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-received:
			cancel()
		case <-ctx.Done():
		}
	}()
	out := filepath.Join(dir, "cancel")
	r := suite.RunCI(ctx, suite.CIRequest{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out})
	if r.ExitCode != 2 || r.Skipped != 1 {
		t.Fatalf("%+v", r)
	}
	recovery, e := durablerun.Recover(filepath.Join(out, "runs", "setup-one"))
	if e != nil || recovery.Uncertain != 1 || recovery.SafeToRepeat {
		t.Fatalf("%+v %v", recovery, e)
	}
	if r = suite.RunCI(t.Context(), suite.CIRequest{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out}); r.ExitCode != 2 {
		t.Fatal("interrupted CI retried")
	}
}

func TestCIErrorsAreMachineReadableWithoutPatientValues(t *testing.T) {
	for _, extra := range [][]string{{}, {"--send"}, {"--send", "--deadline", "PRIVATE"}, {"--send", "--previous", "PRIVATE"}, {"--send", "--promotion", "PRIVATE"}} {
		var out, diagnostic bytes.Buffer
		args := append([]string{"suite", "ci", "PRIVATE", "--environment", "PRIVATE", "--output", filepath.Join(t.TempDir(), "out")}, extra...)
		err := cli.Execute("test", args, &out, &diagnostic)
		r, e := suite.DecodeCI(out.Bytes())
		if cli.ExitCode(err) != 2 || e != nil || r.State != "error" || strings.Contains(out.String()+diagnostic.String(), "PRIVATE") {
			t.Fatalf("%v %v %s %s", err, e, out.String(), diagnostic.String())
		}
	}
}

func TestCISummaryRejectsUnknownNullDuplicateAndContradictoryStates(t *testing.T) {
	valid := `{"schema":"readmit-suite-ci/v1","state":"passed","exit_code":0,"jobs":1,"executed":1,"skipped":0,"coverage":"not_requested"}`
	for _, raw := range []string{strings.Replace(valid, `"jobs":1`, `"jobs":0`, 1), strings.Replace(valid, `"executed":1`, `"executed":0`, 1), strings.Replace(valid, `"exit_code":0`, `"exit_code":2`, 1), strings.Replace(valid, `"coverage":"not_requested"`, `"coverage":"failed"`, 1), strings.Replace(valid, `"skipped":0`, `"skipped":null`, 1), strings.Replace(valid, `"skipped":0`, `"skipped":0,"skipped":0`, 1), strings.Replace(valid, `"skipped":0,`, ``, 1), strings.Replace(valid, `"skipped":0`, `"skipped":0,"unknown":1`, 1)} {
		if _, e := suite.DecodeCI([]byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestCIPublicDeadlineAndSkippedJobsNeverPass(t *testing.T) {
	dir, doc := fixture(t, peer(t, func(c net.Conn) {
		r, _ := mllp.NewReader(c, 1<<20)
		if _, e := r.ReadFrame(); e == nil {
			io.Copy(io.Discard, c)
		}
	}))
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	var stdout, stderr bytes.Buffer
	out := filepath.Join(dir, "deadline")
	err := cli.Execute("test", []string{"suite", "ci", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--send", "--deadline", "100ms"}, &stdout, &stderr)
	r, e := suite.DecodeCI(stdout.Bytes())
	if cli.ExitCode(err) != 2 || e != nil || r.Skipped < 1 {
		t.Fatalf("%+v %v %v", r, e, err)
	}
	if raw, e := os.ReadFile(filepath.Join(out, "junit.xml")); e != nil || !bytes.Contains(raw, []byte(`failures="1"`)) {
		t.Fatalf("%s %v", raw, e)
	}
}

func TestCICrashHelper(t *testing.T) {
	path := os.Getenv("READMIT_CI_CRASH_FIXTURE")
	if path == "" {
		return
	}
	err := cli.Execute("test", []string{"suite", "ci", filepath.Join(path, "suite.json"), "--environment", "east", "--output", filepath.Join(path, "crashed"), "--send"}, os.Stdout, os.Stderr)
	os.Exit(cli.ExitCode(err))
}

func TestCIPublicProcessCrashRetainsEvidenceWithoutRetry(t *testing.T) {
	received := make(chan struct{})
	dir, _ := fixture(t, peer(t, func(c net.Conn) {
		r, _ := mllp.NewReader(c, 1<<20)
		if _, e := r.ReadFrame(); e == nil {
			close(received)
			io.Copy(io.Discard, c)
		}
	}))
	cmd := exec.Command(os.Args[0], "-test.run=^TestCICrashHelper$")
	cmd.Env = append(os.Environ(), "READMIT_CI_CRASH_FIXTURE="+dir)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("CI child never sent")
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	cmd.Wait()
	out := filepath.Join(dir, "crashed")
	recovery, e := durablerun.Recover(filepath.Join(out, "runs", "booking-one"))
	if e != nil || recovery.Terminal || recovery.Uncertain != 1 || recovery.SafeToRepeat {
		t.Fatalf("%+v %v", recovery, e)
	}
	for _, name := range []string{"ci.json", "junit.xml"} {
		if _, e := os.Stat(filepath.Join(out, name)); !os.IsNotExist(e) {
			t.Fatal("crash fabricated summary")
		}
	}
	var stdout, stderr bytes.Buffer
	err := cli.Execute("test", []string{"suite", "ci", filepath.Join(dir, "suite.json"), "--environment", "east", "--output", out, "--send"}, &stdout, &stderr)
	if cli.ExitCode(err) != 2 {
		t.Fatal("crashed CI restarted")
	}
}
