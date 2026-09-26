package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/desktop/hubadmin"
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

func TestHubAdministrationJourneyBindingMatchesTheShellBinding(t *testing.T) {
	production := bind(new(hubadmin.Admin))
	bridge, err := newBridge(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hubadmin.Admin.Preview", "hubadmin.Admin.CancelPreview"} {
		if _, ok := production[name]; !ok {
			t.Errorf("shell has no %s binding", name)
		}
		if _, ok := bridge.methods[name]; !ok {
			t.Errorf("journey bridge has no %s binding", name)
		}
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
	chosen := t.TempDir()
	d.script(answer{kind: "folder", title: "Open workspace", paths: []string{chosen}})
	d.script(answer{kind: "files"})
	d.script(answer{kind: "folder", title: "Choose a folder for the new project", paths: []string{t.TempDir()}})
	if folder, err := d.ChooseFolder("Open workspace"); folder != chosen || err != nil {
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
	if _, err := empty.ChooseFolder("Open workspace"); err == nil || empty.report().Shown[0].Problem != "no answer was scripted for this dialog" {
		t.Fatalf("an unscripted dialog was not refused and recorded: %v, %+v", err, empty.report())
	}
}

// The scripted dialogs give only what the host's dialogs give. A folder dialog
// returns a folder that exists when it is answered, never a file or a folder
// that is not there yet; a save dialog names an entry of a folder that exists,
// which need not exist itself — or may, when the person confirmed replacing
// it — and creates nothing. An answer no host dialog could give is consumed,
// refused and recorded, so the journey that scripted it fails.
func TestDialogsGiveOnlyWhatTheHostDialogsGive(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(file, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(root, "new-backup")
	d := &scriptedDialogs{}
	for _, a := range []answer{
		{kind: "folder", paths: []string{root}},
		{kind: "save", title: "New backup folder", paths: []string{named}},
		{kind: "save", paths: []string{file}},
		{kind: "save"},
		{kind: "folder", paths: []string{named}},
		{kind: "folder", paths: []string{file}},
		{kind: "save", paths: []string{filepath.Join(root, "missing", "new-backup")}},
		{kind: "save", paths: []string{"new-backup"}},
	} {
		d.script(a)
	}
	if folder, err := d.ChooseFolder("Open workspace"); folder != root || err != nil {
		t.Fatalf("an existing folder answered %q, %v", folder, err)
	}
	if path, err := d.ChooseDestination("New backup folder"); path != named || err != nil {
		t.Fatalf("a new name in an existing folder answered %q, %v", path, err)
	}
	if _, err := os.Lstat(named); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("naming a new folder created something: %v", err)
	}
	if path, err := d.ChooseDestination("Choose a new folder for the portable review"); path != file || err != nil {
		t.Fatalf("an existing entry the person confirmed replacing answered %q, %v", path, err)
	}
	if path, err := d.ChooseDestination("Choose a new folder for the portable review"); path != "" || err != nil {
		t.Fatalf("a dismissed save dialog answered %q, %v", path, err)
	}
	for _, impossible := range []func() (string, error){
		func() (string, error) { return d.ChooseFolder("Choose a folder for the new project") },
		func() (string, error) { return d.ChooseFolder("Open workspace") },
		func() (string, error) { return d.ChooseDestination("New backup folder") },
		func() (string, error) { return d.ChooseDestination("New backup folder") },
	} {
		if path, err := impossible(); path != "" || err == nil {
			t.Fatalf("an answer no host dialog gives was taken: %q, %v", path, err)
		}
	}
	report := d.report()
	if report.Unanswered != 0 {
		t.Fatalf("unanswered = %d; an impossible answer is consumed, not left for the next dialog", report.Unanswered)
	}
	var problems []string
	for _, shown := range report.Shown {
		if shown.Problem != "" {
			problems = append(problems, shown.Kind+": "+shown.Problem)
		}
	}
	want := []string{
		"folder: a folder dialog returns only a folder that already exists, and the scripted answer is not one",
		"folder: a folder dialog returns only a folder that already exists, and the scripted answer is not one",
		"save: a save dialog names an entry of a folder that already exists, and the scripted answer's folder does not",
		"save: a save dialog returns an absolute path, and the scripted answer is not one",
	}
	if strings.Join(problems, "\n") != strings.Join(want, "\n") {
		t.Fatalf("recorded problems %q, want %q", problems, want)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Fatalf("the scripted dialogs changed what the root holds: %v, %v", entries, err)
	}
}

func TestControlRequestsRefuseMalformedAnswers(t *testing.T) {
	b := &bridge{methods: bind(&bound{}), dialogs: &scriptedDialogs{}}
	answers := exchange(t, b,
		`{"id":1,"op":"dialog","dialog":"printer","paths":["/x"]}`,
		`{"id":2,"op":"dialog","dialog":"folder","paths":["/a","/b"]}`,
		`{"id":3,"op":"write"}`,
		`{"id":4,"op":"dialogs"}`,
		`{"id":5,"op":"dialog","dialog":"folder","titel":"Open workspace","paths":["/x"]}`,
		`not a request`,
		`{"id":6,"op":"dialog","dialog":"save","paths":["/a/new","/b/new"]}`,
	)
	for _, id := range []int64{1, 2, 3, 6} {
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

// A hub's certificates are written inside the root: a synthetic authority, a
// server identity for 127.0.0.1 and a client identity, each chaining to that
// authority for its own use and for nothing else. A folder outside the root
// is refused.
func TestHubCertificatesChainForLoopbackAndStayInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if err := hubCertificates(root, "hub"); err != nil {
		t.Fatal(err)
	}
	read := func(name string) *x509.Certificate {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, "hub", name))
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			t.Fatalf("%s holds no PEM block", name)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return certificate
	}
	pool := x509.NewCertPool()
	pool.AddCert(read("ca.pem"))
	server := read("server.pem")
	if _, err := server.Verify(x509.VerifyOptions{Roots: pool, DNSName: "127.0.0.1", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("the server identity does not serve 127.0.0.1: %v", err)
	}
	client := read("client.pem")
	if _, err := client.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("the client identity does not authenticate a client: %v", err)
	}
	if _, err := client.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Error("the client identity also serves")
	}
	for _, name := range []string{"server-key.pem", "client-key.pem"} {
		info, err := os.Stat(filepath.Join(root, "hub", name))
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is not private: %v %v", name, info.Mode(), err)
		}
	}
	if err := hubCertificates(root, "../escaped"); err == nil {
		t.Error("wrote certificates outside the root")
	}
}
