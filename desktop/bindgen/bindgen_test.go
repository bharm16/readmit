package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/desktop/bindgen/internal/sample"
)

func repository(t *testing.T) (module, root string) {
	t.Helper()
	module, err := desktopModule()
	if err != nil {
		t.Fatal(err)
	}
	return module, filepath.Dir(module)
}

// The committed declarations are exactly what the Go types declare. A facade
// method, a member, its optionality or a vocabulary value that changed in Go
// without the file being regenerated fails here, and so does anything written
// into the file by hand: a member only the TypeScript side declares would be a
// request member Wails' encoding/json drops without a word.
func TestGeneratedBindingsAreCurrent(t *testing.T) {
	module, root := repository(t)
	generated, err := generate(root, bound, facadeNames)
	if err != nil {
		t.Fatal(err)
	}
	again, err := generate(root, bound, facadeNames)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, again) {
		t.Fatal("two generations from the same Go types differ")
	}
	committed, err := os.ReadFile(filepath.Join(module, filepath.FromSlash(output)))
	if err != nil {
		t.Fatal(err)
	}
	if difference := drift(generated, committed); difference != "" {
		t.Fatalf("%s is not what the Go types declare: %s\nregenerate it with \"go run ./bindgen\" in desktop", output, difference)
	}
}

// drift names the first line where the committed declarations differ from the
// generated ones, or nothing when they are the same bytes.
func drift(generated, committed []byte) string {
	if bytes.Equal(generated, committed) {
		return ""
	}
	want := strings.Split(string(generated), "\n")
	got := strings.Split(string(committed), "\n")
	for i := range max(len(want), len(got)) {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return fmt.Sprintf("line %d is %q where the Go types declare %q", i+1, g, w)
		}
	}
	return "the files differ only in their line endings"
}

// Drift in either direction is caught: a member or method Go declares and the
// file lacks, one the file declares and Go does not, and a member whose
// optionality differs.
func TestDriftIsCaughtInEitherDirection(t *testing.T) {
	_, root := repository(t)
	generated, err := generate(root, bound, facadeNames)
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for name, edit := range map[string]func(string) string{
		"a request member only TypeScript declares": func(s string) string {
			return strings.Replace(s, "export interface TestRequest {\n", "export interface TestRequest {\n  misspelled?: string;\n", 1)
		},
		"a member Go declares, left out": func(s string) string {
			return strings.Replace(s, "export interface ShellResult {\n  state: State;\n", "export interface ShellResult {\n", 1)
		},
		"an optional member declared required": func(s string) string {
			return strings.Replace(s, "export interface ShellResult {\n  state: State;\n  reason?: string;\n", "export interface ShellResult {\n  state: State;\n  reason: string;\n", 1)
		},
		"a required member declared optional": func(s string) string {
			return strings.Replace(s, "export interface ShellResult {\n  state: State;\n", "export interface ShellResult {\n  state?: State;\n", 1)
		},
		"a bound method left out": func(s string) string {
			return strings.Replace(s, "  Shell(): Promise<ShellResult>;\n", "", 1)
		},
		"a vocabulary value only TypeScript declares": func(s string) string {
			return strings.Replace(s, `export type Theme = "system" | "light" | "dark";`, `export type Theme = "system" | "light" | "dark" | "sepia";`, 1)
		},
	} {
		edited := edit(text)
		if edited == text {
			t.Fatalf("%s: the edit did not apply to the generated declarations", name)
		}
		if drift(generated, []byte(edited)) == "" {
			t.Errorf("%s is not caught", name)
		}
	}
}

// Each member follows what encoding/json puts on the wire: its tag's name or
// its field's, optional exactly when the tag omits it, null only for a pointer
// that is not omitted, and a vocabulary is the constants its package declares,
// however each is written.
func TestDeclarationsFollowEncodingJSON(t *testing.T) {
	_, root := repository(t)
	generated, err := generate(root, []object{{value: &sample.Object{}, facade: "SampleFacade"}}, names{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`/** desktop/bindgen/internal/sample.Mode */
export type SampleMode = "fast" | "careful" | "quoted" | "";
`,
		`/** desktop/bindgen/internal/sample.Request */
export interface SampleRequest {
  name: string;
  mode: SampleMode;
  limit?: number;
  note: string | null;
  hint?: string;
  tags: string[];
  counts: Record<string, number>;
  raw: unknown;
  at: string;
  bytes: string;
  any: unknown;
  free: string;
  Untagged: boolean;
  inline: { a: number };
  items: (SampleItem | null)[];
}
`,
		`export interface SampleItem {
  "content-label": string;
}
`,
		`/** desktop/bindgen/internal/sample/words.Result */
export interface WordsResult {
  word: string;
}
`,
		`/** The methods Wails binds for desktop/bindgen/internal/sample.Object. */
export interface SampleFacade {
  Choose(arg1: string, arg2: SampleMode): Promise<Record<string, number>>;
  Forget(arg1: string, arg2: SampleItem | null): Promise<void>;
  Read(workspace: string, limit: number): Promise<SampleResult>;
  Stop(): Promise<void>;
  Words(): Promise<WordsResult>;
  Write(request: SampleRequest): Promise<SampleResult>;
}
`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the declarations do not contain\n%s\ngenerated:\n%s", want, generated)
		}
	}
	if strings.Contains(string(generated), "Free =") || strings.Contains(string(generated), "hidden") || strings.Contains(string(generated), "Hidden") {
		t.Errorf("a string without constants, an unexported field or a field tagged - was declared:\n%s", generated)
	}
}

// A vocabulary is the constants that name a panel's choices for a plain Go
// string: the whole const block that declares the one constant named, so a
// choice added there reaches the panel, or exactly the constants listed.
func TestVocabulariesAreTheirGoConstants(t *testing.T) {
	_, root := repository(t)
	vocabularies := names{vocabularies: map[string][]string{
		"SampleChoice": {"desktop/bindgen/internal/sample.FirstChoice"},
		"SampleSome":   {"desktop/bindgen/internal/sample.FirstChoice", "desktop/bindgen/internal/sample.Fast"},
	}}
	generated, err := generate(root, []object{{value: &sample.Object{}, facade: "SampleFacade"}}, vocabularies)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`/** desktop/bindgen/internal/sample.FirstChoice and the constants declared with it */
export type SampleChoice = "first" | "quoted";
`,
		`/** desktop/bindgen/internal/sample.FirstChoice, desktop/bindgen/internal/sample.Fast */
export type SampleSome = "first" | "fast";
`,
	} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("the declarations do not contain\n%s\ngenerated:\n%s", want, generated)
		}
	}
	for vocabulary, want := range map[string]string{
		"desktop/bindgen/internal/sample.Nothing": "does not declare as a constant",
		"FirstChoice": "not a package's constant",
	} {
		_, err := generate(root, []object{{value: &sample.Object{}, facade: "SampleFacade"}}, names{vocabularies: map[string][]string{"SampleChoice": {vocabulary}}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want a refusal naming %q", vocabulary, err, want)
		}
	}
	_, err = generate(root, []object{{value: &sample.Object{}, facade: "SampleFacade"}}, names{vocabularies: map[string][]string{"SampleChoice": {"desktop/bindgen/internal/nothing.FirstChoice"}}})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a vocabulary of a package that does not exist: got %v", err)
	}
	_, err = generate(root, []object{{value: &sample.Object{}, facade: "SampleFacade"}}, names{vocabularies: map[string][]string{"SampleResult": {"desktop/bindgen/internal/sample.FirstChoice"}}})
	if err == nil || !strings.Contains(err.Error(), "named like a declared Go type") {
		t.Errorf("a vocabulary named like a declared type: got %v", err)
	}
}

// A shape bindgen cannot declare exactly is refused by name rather than
// guessed at.
func TestShapesThatCannotBeDeclaredExactlyAreRefused(t *testing.T) {
	_, root := repository(t)
	colliding := names{prefixes: map[string]string{"desktop/bindgen/internal/sample/words": "Sample"}}
	for _, refused := range []struct {
		value any
		names names
		want  string
	}{
		{&sample.Embedding{}, names{}, "embeds sample.Base"},
		{&sample.SelfMarshaling{}, names{}, "sample.Marshals marshals itself"},
		{&sample.Variadic{}, names{}, "cannot call a variadic method"},
		{&sample.Pair{}, names{}, "one result and an optional error"},
		{&sample.Channel{}, names{}, "cannot carry chan int"},
		{&sample.Quoting{}, names{}, `the json option "string" is not modeled`},
		{&sample.Colliding{}, colliding, "would both be declared as SampleResult"},
		{sample.Object{}, names{}, "a pointer to a struct"},
	} {
		_, err := generate(root, []object{{value: refused.value, facade: "F"}}, refused.names)
		if err == nil || !strings.Contains(err.Error(), refused.want) {
			t.Errorf("%T: got %v, want a refusal naming %q", refused.value, err, refused.want)
		}
	}
}
