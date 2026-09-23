package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bound stands in for the facade: the same kinds of method signature, bound
// and called the way the bridge binds and calls the real one.
type bound struct{}

type echoed struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

func (*bound) Echo(text string, count int) echoed { return echoed{Text: text, Count: count} }
func (*bound) Refuse() error                      { return errors.New("refused on purpose") }
func (*bound) Pair() (int, error)                 { return 0, errors.New("pair failed") }
func (*bound) Nothing()                           {}
func (*bound) Panic()                             { panic("broken on purpose") }

// exchange writes each request line to a bridge over bound and returns every
// answer by its request id; a line that could not be read is answered under
// id -1. Calls settle on their own goroutines, so answers are collected until
// every request has had one.
func exchange(t *testing.T, b *bridge, requests ...string) map[int64]response {
	t.Helper()
	reader, writer := io.Pipe()
	go b.serve(strings.NewReader(strings.Join(requests, "\n")+"\n"), writer)
	decoder := json.NewDecoder(reader)
	answers := map[int64]response{}
	for range requests {
		var answer response
		if err := decoder.Decode(&answer); err != nil {
			t.Fatal(err)
		}
		answers[answer.ID] = answer
	}
	return answers
}

func TestBoundMethodsAreNamedAsWailsPublishesThem(t *testing.T) {
	methods := bind(&bound{})
	for _, name := range []string{"main.bound.Echo", "main.bound.Refuse", "main.bound.Pair", "main.bound.Nothing", "main.bound.Panic"} {
		if _, ok := methods[name]; !ok {
			t.Errorf("%s is not bound; bound: %v", name, methods)
		}
	}
	if len(methods) != 5 {
		t.Fatalf("bound %d methods, want 5", len(methods))
	}
}

// Every outcome a call can have is the one Wails' own dispatch gives it: a
// decoded result, a rejected argument list, a returned error, or nothing. The
// two calls Wails would never answer — an unbound name and a panic — are
// rejected, so a journey fails instead of waiting.
func TestCallsDecodeAndSettleAsWailsDispatchesThem(t *testing.T) {
	b := &bridge{methods: bind(&bound{}), dialogs: &scriptedDialogs{}}
	answers := exchange(t, b,
		`{"id":1,"op":"call","name":"main.bound.Echo","args":["hello",2]}`,
		`{"id":2,"op":"call","name":"main.bound.Echo","args":["hello"]}`,
		`{"id":3,"op":"call","name":"main.bound.Echo","args":["hello","two"]}`,
		`{"id":4,"op":"call","name":"main.bound.Refuse","args":[]}`,
		`{"id":5,"op":"call","name":"main.bound.Pair","args":[]}`,
		`{"id":6,"op":"call","name":"main.bound.Nothing","args":[]}`,
		`{"id":7,"op":"call","name":"main.bound.Missing","args":[]}`,
		`{"id":8,"op":"call","name":"main.bound.Panic","args":[]}`,
	)
	if got, ok := answers[1].Result.(map[string]any); !ok || got["text"] != "hello" || got["count"] != 2.0 || len(got) != 2 || answers[1].Error != nil {
		t.Errorf("a decoded call answered %v, %v", answers[1].Result, answers[1].Error)
	}
	if answers[2].Error != "error parsing arguments: received 1 arguments to method 'main.bound.Echo', expected 2" {
		t.Errorf("a short argument list answered %v", answers[2].Error)
	}
	if text, _ := answers[3].Error.(string); !strings.HasPrefix(text, "error parsing arguments: ") {
		t.Errorf("an argument of the wrong type answered %v", answers[3].Error)
	}
	if answers[4].Error != "refused on purpose" || answers[5].Error != "pair failed" {
		t.Errorf("returned errors answered %v and %v", answers[4].Error, answers[5].Error)
	}
	if answers[6].Result != nil || answers[6].Error != nil {
		t.Errorf("a method with no result answered %+v", answers[6])
	}
	if answers[7].Error != "journeybridge: method 'main.bound.Missing' not registered" {
		t.Errorf("an unbound method answered %v", answers[7].Error)
	}
	if answers[8].Error != "journeybridge: method 'main.bound.Panic' panicked: broken on purpose" {
		t.Errorf("a panicking method answered %v", answers[8].Error)
	}
}

// A dialog answers only what the journey scripted, in order, for the dialog it
// was scripted for. Anything else is refused the way an unavailable dialog is
// and recorded, so the journey fails for it instead of passing quietly.
func TestDialogsAnswerOnlyWhatWasScripted(t *testing.T) {
	d := &scriptedDialogs{}
	d.script(answer{kind: "folder", title: "Open a readmit workspace folder", paths: []string{"/chosen"}})
	d.script(answer{kind: "files"})
	d.script(answer{kind: "folder", title: "Choose a folder for the new project", paths: []string{"/elsewhere"}})
	if folder, err := d.ChooseFolder("Open a readmit workspace folder"); folder != "/chosen" || err != nil {
		t.Fatalf("a scripted folder answered %q, %v", folder, err)
	}
	if files, err := d.ChooseFiles("Choose evidence files to import", "", ""); len(files) != 0 || err != nil {
		t.Fatalf("a dismissed file dialog answered %v, %v", files, err)
	}
	if folder, err := d.ChooseFolder("Choose the license activation folder"); folder != "" || err == nil {
		t.Fatalf("a dialog with another title took the scripted answer: %q, %v", folder, err)
	}
	if _, err := d.ChooseFiles("Choose evidence files to import", "", ""); err == nil {
		t.Fatal("a file dialog took an answer scripted for a folder dialog")
	}
	report := d.report()
	if report.Unanswered != 1 {
		t.Fatalf("unanswered = %d, want the one answer nothing consumed", report.Unanswered)
	}
	var problems []string
	for _, shown := range report.Shown {
		if shown.Problem != "" {
			problems = append(problems, shown.Title+": "+shown.Problem)
		}
	}
	if len(report.Shown) != 4 || len(problems) != 2 {
		t.Fatalf("shown %d dialogs with problems %v", len(report.Shown), problems)
	}
	empty := &scriptedDialogs{}
	if _, err := empty.ChooseFolder("Open a readmit workspace folder"); err == nil || empty.report().Shown[0].Problem != "no answer was scripted for this dialog" {
		t.Fatalf("an unscripted dialog was not refused and recorded: %v, %+v", err, empty.report())
	}
}

func TestControlRequestsRefuseMalformedAnswers(t *testing.T) {
	b := &bridge{methods: bind(&bound{}), dialogs: &scriptedDialogs{}}
	answers := exchange(t, b,
		`{"id":1,"op":"dialog","dialog":"printer","paths":["/x"]}`,
		`{"id":2,"op":"dialog","dialog":"folder","paths":["/a","/b"]}`,
		`{"id":3,"op":"write"}`,
		`{"id":4,"op":"dialogs"}`,
		`{"id":5,"op":"dialog","dialog":"folder","titel":"Open a readmit workspace folder","paths":["/x"]}`,
		`not a request`,
	)
	for id := int64(1); id <= 3; id++ {
		if answers[id].Error == nil {
			t.Errorf("request %d was accepted: %+v", id, answers[id])
		}
	}
	if got, _ := json.Marshal(answers[4].Result); string(got) != `{"shown":[],"unanswered":0}` {
		t.Errorf("an idle report answered %s", got)
	}
	// A misspelled member and an unreadable line are both refused; neither
	// scripts an answer that would then fit any dialog.
	if text, _ := answers[-1].Error.(string); !strings.HasPrefix(text, "journeybridge: unreadable request: ") {
		t.Errorf("an unreadable request answered %+v", answers[-1])
	}
	if unanswered := b.dialogs.report().Unanswered; unanswered != 0 {
		t.Errorf("a refused request scripted %d answers", unanswered)
	}
}

// Provisioning writes a signed activation folder inside the journey's root
// and nowhere else.
func TestProvisioningStaysInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	policy, err := provision(root, "vendor-delivered")
	if err != nil {
		t.Fatal(err)
	}
	if policy != filepath.Join(root, "vendor-delivered", "operation-policy.json") {
		t.Fatalf("policy written at %s", policy)
	}
	for _, name := range []string{"entitlement.json", "trust.json", "operation-policy.json"} {
		if _, err := os.Stat(filepath.Join(root, "vendor-delivered", name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, outside := range []string{"..", "../escaped", filepath.Dir(root), root} {
		if _, err := provision(root, outside); err == nil {
			t.Errorf("provisioned outside the root at %s", outside)
		}
	}
	if _, err := provision("relative", "vendor-delivered"); err == nil {
		t.Error("provisioned under a relative root")
	}
	file := filepath.Join(root, "vendor-delivered", "trust.json")
	if _, err := provision(file, "vendor-delivered"); err == nil {
		t.Error("provisioned under a root that is a file")
	}
	if _, err := newBridge(file); err == nil {
		t.Error("a bridge started over a root that is a file")
	}
}

// Issues provisioned together share one key: each folder holds its own term,
// and every folder trusts the same key, so a later issue renews an earlier
// one. A malformed issue is refused, and none is written outside the root.
func TestIssuesShareOneKeyAndStayInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	var issues issueFlags
	for _, value := range []string{"expired,1,-48h,1", "renewed,2,24h,0"} {
		if err := issues.Set(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	policies, err := provisionIssues(root, issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 || policies[0] != filepath.Join(root, "expired", "operation-policy.json") || policies[1] != filepath.Join(root, "renewed", "operation-policy.json") {
		t.Fatalf("policies written at %v", policies)
	}
	first, err := os.ReadFile(filepath.Join(root, "expired", "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(root, "renewed", "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("the two issues trust different keys")
	}
	for _, value := range []string{"folder", "folder,0,24h,0", "folder,1,soon,0", "folder,1,24h,-1", ",1,24h,0"} {
		var refused issueFlags
		if err := refused.Set(value); err == nil {
			t.Errorf("accepted the malformed issue %q", value)
		}
	}
	var outside issueFlags
	if err := outside.Set("../escaped,1,24h,0"); err != nil {
		t.Fatal(err)
	}
	if _, err := provisionIssues(root, outside); err == nil {
		t.Error("provisioned an issue outside the root")
	}
}
