// Command journeybridge serves the real internal/desktop facade to the
// frontend journey harness, so the production React tree can be driven with
// real user events against real Go over real files. It is a test program: it
// is never packaged, never bound into the shell, and opens no network
// connection of its own.
//
// It stands in for exactly two things the native shell supplies and nothing
// else. The first is Wails' call path: every exported method of the facade is
// reachable under the name Wails publishes ("desktop.App.<Method>"), arguments
// arrive as the JSON array the webview serializes, each one is decoded into
// the method's own parameter type with encoding/json as Wails decodes it, the
// argument count must match, each call runs on its own goroutine, and the
// result or error returns in Wails' callback shape. Where Wails would leave a
// call unanswered — a name nothing is bound under, or a method that panics —
// the bridge rejects it instead, so a journey fails rather than hangs. The
// second is the host's native file and folder dialogs, which a journey
// answers explicitly before the action that opens them — the way a person
// picks a folder — and which are refused and recorded when no answer was
// scripted.
//
// The facade is constructed by the same constructor the shell calls, over the
// same five local state documents, in a state directory under the journey's
// own temporary root. Closing standard input is closing the window: the
// process exits and the next bridge over the same root is the reopened
// application. Run with -license instead, it provisions the signed activation
// folder a vendor would have delivered, inside that root, and exits: that is
// never the running application's doing.
package main

import (
	"bufio"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/testlicense"
)

// request is one line the harness writes. Op selects what it is; only a call
// reaches the facade. The line is read strictly; each argument inside it is
// decoded later the way Wails decodes one.
type request struct {
	ID     int64            `json:"id"`
	Op     string           `json:"op"`
	Name   string           `json:"name,omitempty"`
	Args   []jsontext.Value `json:"args,omitempty"`
	Dialog string           `json:"dialog,omitempty"`
	Title  string           `json:"title,omitempty"`
	Paths  []string         `json:"paths,omitempty"`
}

// response mirrors Wails' callback message: a result or an error, never both.
// It is written with encoding/json, as Wails writes its callback.
type response struct {
	ID     int64 `json:"id"`
	Result any   `json:"result"`
	Error  any   `json:"error"`
}

// bridge holds the bound facade and the scripted dialogs.
type bridge struct {
	methods map[string]reflect.Value
	dialogs *scriptedDialogs
	out     *json.Encoder
	outMu   sync.Mutex
}

func main() {
	root := flag.String("root", "", "absolute temporary root the journey owns")
	license := flag.String("license", "", "provision a signed activation folder at this path inside root, then exit")
	flag.Parse()
	if *license != "" {
		policy, err := provision(*root, *license)
		if err != nil {
			fmt.Fprintln(os.Stderr, "journeybridge:", err)
			os.Exit(2)
		}
		fmt.Println(policy)
		return
	}
	// Protocol lines are the only thing written to standard output. Anything
	// else a package prints goes to standard error, where the harness keeps it.
	protocol := os.Stdout
	os.Stdout = os.Stderr
	b, err := newBridge(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journeybridge:", err)
		os.Exit(2)
	}
	b.serve(os.Stdin, protocol)
	// Standard input closed: the window closed. Calls still running are
	// abandoned exactly as a closing application abandons them.
	os.Exit(0)
}

// provision writes what a vendor delivers for a licensed journey — a newly
// signed test entitlement, its trust store and an activated operation policy —
// into a folder inside root, as its own process: the running application never
// provisions anything. It returns the operation policy's path.
func provision(root, folder string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(root, folder)
	}
	relative, err := filepath.Rel(root, filepath.Clean(folder))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("the activation folder must be inside the journey root")
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return "", err
	}
	return testlicense.Create(folder)
}

func checkRoot(root string) error {
	if !filepath.IsAbs(root) {
		return errors.New("the journey root must be an absolute path")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("the journey root must be an existing directory")
	}
	return nil
}

// newBridge constructs the facade the way the shell does, over state files
// inside root.
func newBridge(root string) (*bridge, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}
	state := filepath.Join(root, "shell-state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		return nil, err
	}
	dialogs := &scriptedDialogs{}
	app := desktop.NewWithOperationSelection(dialogs,
		filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
		filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"),
		filepath.Join(state, "operations.json"))
	return &bridge{methods: bind(app), dialogs: dialogs}, nil
}

// bind publishes every exported method of a bound struct pointer under the
// name Wails gives it: the package name the type is qualified by, the type
// name and the method name, joined by dots.
func bind(bound any) map[string]reflect.Value {
	value := reflect.ValueOf(bound)
	named := value.Type().Elem()
	pkg := strings.TrimSuffix(named.String(), "."+named.Name())
	methods := map[string]reflect.Value{}
	for i := range value.NumMethod() {
		methods[pkg+"."+named.Name()+"."+value.Type().Method(i).Name] = value.Method(i)
	}
	return methods
}

func (b *bridge) serve(in io.Reader, out io.Writer) {
	b.out = json.NewEncoder(out)
	lines := bufio.NewScanner(in)
	lines.Buffer(make([]byte, 64<<10), 64<<20)
	for lines.Scan() {
		// A request is the harness's own line, so a member it does not know is
		// a mistake, not something to ignore: a misspelled dialog title must
		// not quietly answer any dialog.
		var r request
		if err := jsonv2.Unmarshal(lines.Bytes(), &r, jsonv2.RejectUnknownMembers(true)); err != nil {
			b.reply(response{ID: -1, Error: "journeybridge: unreadable request: " + err.Error()})
			continue
		}
		if r.Op == "call" {
			// Wails answers each call on its own goroutine, so a cancel can
			// reach an operation that is still running.
			go func() { b.reply(b.call(r)) }()
			continue
		}
		b.reply(b.control(r))
	}
}

func (b *bridge) reply(r response) {
	b.outMu.Lock()
	defer b.outMu.Unlock()
	if err := b.out.Encode(r); err != nil {
		// A result Wails could not marshal ends the application; here it
		// ends the journey with the reason.
		fmt.Fprintln(os.Stderr, "journeybridge: unwritable response:", err)
		os.Exit(3)
	}
}

// call decodes the arguments, calls the method and shapes its outcome as
// Wails' bound-method dispatch does.
func (b *bridge) call(r request) (settled response) {
	defer func() {
		// Wails recovers a panicking method and never answers the call; the
		// journey would wait out its timeout. Answering with the panic fails it
		// with the reason instead.
		if failure := recover(); failure != nil {
			settled = response{ID: r.ID, Error: fmt.Sprintf("journeybridge: method '%s' panicked: %v", r.Name, failure)}
		}
	}()
	method, ok := b.methods[r.Name]
	if !ok {
		return response{ID: r.ID, Error: fmt.Sprintf("journeybridge: method '%s' not registered", r.Name)}
	}
	signature := method.Type()
	if len(r.Args) != signature.NumIn() {
		return response{ID: r.ID, Error: fmt.Sprintf("error parsing arguments: received %d arguments to method '%s', expected %d", len(r.Args), r.Name, signature.NumIn())}
	}
	args := make([]reflect.Value, len(r.Args))
	for i, raw := range r.Args {
		value := reflect.New(signature.In(i))
		if err := json.Unmarshal([]byte(raw), value.Interface()); err != nil {
			return response{ID: r.ID, Error: "error parsing arguments: " + err.Error()}
		}
		args[i] = value.Elem()
	}
	results := method.Call(args)
	var result any
	var failure error
	switch len(results) {
	case 1:
		if err, isError := results[0].Interface().(error); isError {
			failure = err
		} else {
			result = results[0].Interface()
		}
	case 2:
		result = results[0].Interface()
		if err, isError := results[1].Interface().(error); isError {
			failure = err
		}
	}
	if failure != nil {
		return response{ID: r.ID, Error: failure.Error()}
	}
	return response{ID: r.ID, Result: result}
}

// control answers the harness's own requests. None of them reaches the facade.
func (b *bridge) control(r request) response {
	switch r.Op {
	case "dialog":
		if r.Dialog != folderDialog && r.Dialog != filesDialog {
			return response{ID: r.ID, Error: "journeybridge: a dialog answer is for a folder or files dialog"}
		}
		if r.Dialog == folderDialog && len(r.Paths) > 1 {
			return response{ID: r.ID, Error: "journeybridge: a folder dialog chooses one folder"}
		}
		b.dialogs.script(answer{kind: r.Dialog, title: r.Title, paths: r.Paths})
		return response{ID: r.ID, Result: true}
	case "dialogs":
		return response{ID: r.ID, Result: b.dialogs.report()}
	}
	return response{ID: r.ID, Error: "journeybridge: unknown request " + r.Op}
}

// The two host dialogs the facade opens.
const (
	folderDialog = "folder"
	filesDialog  = "files"
)

// answer is one dialog outcome a journey scripted: the paths a person chose,
// or none for a dialog they dismissed. A title, when given, is the dialog the
// journey expects to answer.
type answer struct {
	kind  string
	title string
	paths []string
}

// shown is one dialog the application opened, and what came of it.
type shown struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Problem string `json:"problem,omitempty"`
}

// dialogReport is what the harness checks when a journey ends: every dialog
// the application opened, and every scripted answer nothing consumed.
type dialogReport struct {
	Shown      []shown `json:"shown"`
	Unanswered int     `json:"unanswered"`
}

// scriptedDialogs stands in for the host's native dialogs. Answers are
// consumed in the order they were scripted.
type scriptedDialogs struct {
	mu      sync.Mutex
	answers []answer
	shown   []shown
}

func (d *scriptedDialogs) script(a answer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.answers = append(d.answers, a)
}

func (d *scriptedDialogs) report() dialogReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	return dialogReport{Shown: append([]shown{}, d.shown...), Unanswered: len(d.answers)}
}

// take consumes the next answer for a dialog of this kind and title. A dialog
// nobody scripted, or one of another kind or title, is refused the way an
// unavailable dialog is, and recorded so the journey fails for it.
func (d *scriptedDialogs) take(kind, title string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	record := shown{Kind: kind, Title: title}
	var problem string
	switch {
	case len(d.answers) == 0:
		problem = "no answer was scripted for this dialog"
	case d.answers[0].kind != kind:
		problem = "the next scripted answer is for a " + d.answers[0].kind + " dialog"
	case d.answers[0].title != "" && d.answers[0].title != title:
		problem = "the next scripted answer is for the dialog titled " + d.answers[0].title
	}
	if problem != "" {
		record.Problem = problem
		d.shown = append(d.shown, record)
		return nil, errors.New(problem)
	}
	next := d.answers[0]
	d.answers = d.answers[1:]
	d.shown = append(d.shown, record)
	return next.paths, nil
}

// ChooseFolder answers the folder dialog. A dismissed dialog returns an empty
// folder and no error, as the native dialog does.
func (d *scriptedDialogs) ChooseFolder(title string) (string, error) {
	paths, err := d.take(folderDialog, title)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// ChooseFiles answers the file dialog. A dismissed dialog returns no files and
// no error, as the native dialog does.
func (d *scriptedDialogs) ChooseFiles(title, _, _ string) ([]string, error) {
	return d.take(filesDialog, title)
}
