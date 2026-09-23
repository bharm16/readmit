//go:build !windows

package desktop_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
)

// plantedOutside is the marker a document outside the open workspace holds, so
// a reader that followed an entry out of the workspace would carry it back.
const plantedOutside = "PLANTED-OUTSIDE-WORKSPACE-111"

// hostileReadBudget is how much one refused call may allocate. Every bounded
// reader of this window refuses a document long before this; an unbounded read
// of the gigabyte-long sparse entry below allocates at least four times it.
const hostileReadBudget = 256 << 20

// notPathArguments are the bound operations whose string arguments name
// something other than a filesystem entry, and which may therefore complete
// when one of those strings happens to spell a hostile file name. Each still
// has to answer promptly, allocate within the budget and carry no planted
// value; only the refusal is not required of it.
var notPathArguments = []string{
	// The second argument is a search query, not an entry; a query that
	// matches nothing is a completed search.
	"Search",
}

// hostileEntry is one adversarial filesystem entry and whether an operation
// handed it as a document must refuse it. An empty directory can legitimately
// be an empty collection, so it is held only to the prompt, bounded answer.
type hostileEntry struct {
	name   string
	refuse bool
}

// Every operation the window binds whose arguments are only strings — every
// open, read, inspect, validate, verify and select of an operator-chosen
// document, folder or entry — is handed each hostile entry in each argument
// position: a FIFO that blocks any reader that opens it, a symbolic link to
// that FIFO, a sparse gigabyte-long document, a directory where a document is
// expected, and a relative entry that walks out of the open workspace to a
// document holding a planted value. The facade is the boundary, not the
// control that would have offered these entries, so the calls go straight to
// it. Every call has to answer within a deadline, allocate within a budget
// that no unbounded read of the long entry could meet, carry nothing read from
// outside the workspace, refuse every hostile document, and leave the outside
// document untouched. Every operation taking one request is handed the same
// entries in each of its string members, with the workspace open, and held to
// all of that except the refusal, because a member may name something other
// than an entry. The operations are enumerated rather than listed, so an
// operation added later is held to the same boundary without being named here.
func TestEveryPathOperationRefusesHostileEntriesPromptlyAndBoundedly(t *testing.T) {
	// A request member can be an address as well as an entry; a hostile
	// string must never become a name lookup that leaves this machine.
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("lookup refused")
	}}
	defer func() { net.DefaultResolver = previous }()
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	// A relative member an operation resolves against the process rather than
	// the workspace lands here, where nothing may be written.
	cwd := t.TempDir()
	t.Chdir(cwd)

	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, outside, "planted.json", `{"schema":"readmit-target/v3","name":"`+plantedOutside+`"}`)
	before := bytesUnder(t, outside)

	fifo := filepath.Join(root, "hostile-fifo.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fifo, filepath.Join(root, "hostile-link.json")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(root, "hostile-large.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	large.Close()
	if err := os.Mkdir(filepath.Join(root, "hostile-dir.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostile := []hostileEntry{
		{"hostile-fifo.json", true}, {"hostile-link.json", true}, {"hostile-large.json", true},
		{"hostile-dir.json", false}, {filepath.Join("..", "outside", "planted.json"), true},
	}

	// unblock releases a reader stuck opening the FIFO, so one failure is
	// reported as itself rather than hanging every call after it.
	unblock := func() {
		if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
	}

	// attempt calls one operation and holds it to the answers every hostile
	// entry requires of any operation: promptly, within the allocation
	// budget, and carrying nothing read outside the workspace. It reports the
	// result, or nothing when the operation had to be unblocked.
	attempt := func(call string, method reflect.Method, args []reflect.Value) (reflect.Value, []byte, bool) {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		allocated := memory.TotalAlloc
		answered := make(chan reflect.Value, 1)
		go func() { answered <- method.Func.Call(args)[0] }()
		var result reflect.Value
		select {
		case result = <-answered:
		case <-time.After(10 * time.Second):
			t.Errorf("%s blocked instead of refusing", call)
			unblock()
			select {
			case <-answered:
			case <-time.After(5 * time.Second):
			}
			return reflect.Value{}, nil, false
		}
		runtime.ReadMemStats(&memory)
		if grown := memory.TotalAlloc - allocated; grown > hostileReadBudget {
			t.Errorf("%s allocated %d MiB, so it read past any bound", call, grown>>20)
		}
		encoded, err := json.Marshal(result.Interface())
		if err != nil {
			t.Fatalf("%s: %v", call, err)
		}
		if strings.Contains(string(encoded), plantedOutside) {
			t.Errorf("%s carried a value read from outside the workspace", call)
		}
		return result, encoded, true
	}
	// forms are the ways an entry reaches an argument that is not the
	// workspace: as the entry name, and as the absolute path to it.
	forms := func(entry hostileEntry) []string {
		if strings.HasPrefix(entry.name, "..") {
			return []string{entry.name}
		}
		return []string{entry.name, filepath.Join(root, entry.name)}
	}

	bound := reflect.TypeOf(app)
	text := reflect.TypeOf("")
	texts := reflect.TypeOf([]string{})
	exercised, requests := 0, 0
	for i := 0; i < bound.NumMethod(); i++ {
		method := bound.Method(i)
		params := method.Type.NumIn() - 1
		if params < 1 || params > 3 || method.Type.NumOut() != 1 {
			continue
		}
		if _, carriesState := method.Type.Out(0).FieldByName("State"); !carriesState {
			continue
		}
		// An operation taking a request: every string or list-of-strings
		// member but the workspace is handed each hostile entry in turn, with
		// the workspace open. A member may name something other than an
		// entry, so completing is allowed here; blocking, unbounded reading
		// and reaching outside the workspace are not.
		if request := method.Type.In(1); params == 1 && request.Kind() == reflect.Struct {
			requests++
			for field := range request.NumField() {
				member := request.Field(field)
				if !member.IsExported() || member.Name == "Workspace" || (member.Type != text && member.Type != texts) {
					continue
				}
				for _, entry := range hostile {
					for _, form := range forms(entry) {
						value := reflect.New(request).Elem()
						if workspace := value.FieldByName("Workspace"); workspace.IsValid() && workspace.Type() == text {
							workspace.SetString(root)
						}
						if member.Type == text {
							value.Field(field).SetString(form)
						} else {
							value.Field(field).Set(reflect.ValueOf([]string{form}))
						}
						attempt(fmt.Sprintf("%s(%s = %s)", method.Name, member.Name, form), method, []reflect.Value{reflect.ValueOf(app), value})
					}
				}
			}
			continue
		}
		stringsOnly := true
		for p := 1; p <= params; p++ {
			stringsOnly = stringsOnly && method.Type.In(p) == text
		}
		if !stringsOnly {
			continue
		}
		exercised++
		for position := range params {
			for _, entry := range hostile {
				candidates := forms(entry)
				if position == 0 {
					candidates = []string{filepath.Join(root, entry.name)}
				}
				for _, form := range candidates {
					args := []reflect.Value{reflect.ValueOf(app)}
					for p := range params {
						switch {
						case p == position:
							args = append(args, reflect.ValueOf(form))
						case p == 0:
							args = append(args, reflect.ValueOf(root))
						default:
							args = append(args, reflect.ValueOf("absent-entry.json"))
						}
					}
					call := fmt.Sprintf("%s(argument %d = %s)", method.Name, position+1, form)
					result, encoded, answered := attempt(call, method, args)
					if !answered {
						continue
					}
					state := desktop.State(result.FieldByName("State").String())
					if entry.refuse && (state == desktop.Completed || state == desktop.Empty) && !slices.Contains(notPathArguments, method.Name) {
						t.Errorf("%s answered %s on a hostile entry instead of refusing it: %s", call, state, encoded)
					}
				}
			}
		}
	}
	if requests < 100 {
		t.Fatalf("only %d request-taking operations were exercised; the enumeration no longer reaches the window's operations", requests)
	}
	if exercised < 60 {
		t.Fatalf("only %d path-taking operations were exercised; the enumeration no longer reaches the window's readers", exercised)
	}
	for _, name := range notPathArguments {
		if _, ok := bound.MethodByName(name); !ok {
			t.Errorf("%s is exempted from refusing hostile entries but is not a bound operation", name)
		}
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("an operation changed a document outside the open workspace")
	}
	if written, err := os.ReadDir(cwd); err != nil || len(written) != 0 {
		t.Fatalf("an operation wrote outside the open workspace, relative to the process: %v %v", written, err)
	}
}
