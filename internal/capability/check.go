package capability

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Check resolves every reference a v2 ledger names against the repository
// tree: each backend operation is a function or method a non-test Go file of
// its package declares, each canonical input and output is a contract some
// non-test Go file spells, each interaction test is a test of that exact
// title in its frontend file, and each parity test is a top-level Go test
// function of its package. What resolves is that each reference exists;
// which documents an operation reads and which test proves it are the
// reviewed data of the row. It reports every reference that does not resolve, not only the
// first, so one run lists the whole repair. A v1 ledger names its tests in
// prose, which nothing can resolve, so it never passes.
func (l Ledger) Check(repository fs.FS) error {
	if l.Schema != SchemaV2 {
		return errors.New("a " + l.Schema + " ledger names its tests in prose; only a " + SchemaV2 + " ledger's references can be checked")
	}
	source := &sourceTree{fsys: repository}
	contracts, err := source.contracts()
	if err != nil {
		return err
	}
	var problems []error
	for _, row := range l.Rows {
		for _, document := range append(slices.Clone(row.Inputs), row.Outputs...) {
			if document != RawHL7 && !contracts[document] {
				problems = append(problems, errors.New("row "+row.ID+": canonical document "+document+" is not a contract any Go source spells"))
			}
		}
		if row.Kind != KindTooling {
			declared, err := source.declarations(row.Backend.Package)
			if err != nil {
				problems = append(problems, errors.New("row "+row.ID+": backend package "+row.Backend.Package+": "+err.Error()))
			} else if !declared[row.Backend.Name] {
				problems = append(problems, errors.New("row "+row.ID+": backend "+row.Backend.Package+"."+row.Backend.Name+" is not declared there"))
			}
		}
		if !row.Implemented {
			continue
		}
		titles, err := source.titles(row.GUITest.File)
		if err != nil {
			problems = append(problems, errors.New("row "+row.ID+": interaction test file "+row.GUITest.File+": "+err.Error()))
		} else if !titles[row.GUITest.Name] {
			problems = append(problems, errors.New("row "+row.ID+": "+row.GUITest.File+" holds no test titled "+strconv.Quote(row.GUITest.Name)))
		}
		tests, err := source.tests(row.ParityTest.Package)
		if err != nil {
			problems = append(problems, errors.New("row "+row.ID+": parity test package "+row.ParityTest.Package+": "+err.Error()))
		} else if !tests[row.ParityTest.Name] {
			problems = append(problems, errors.New("row "+row.ID+": parity test "+row.ParityTest.Package+"."+row.ParityTest.Name+" is not declared there"))
		}
	}
	return errors.Join(problems...)
}

// The commercial portal's surface, as the source tree declares it. The hosted
// checkout and customer portal are the merchant of record's service, outside
// this repository; what this repository holds of the portal is the closed set
// of account events it carries and the vendor issuers that answer them.
const (
	// portalEvents declares EventType, the closed set of account operations
	// the portal carries: a purchase or renewal, an invoice, a scheduled
	// downgrade, a cancellation, a refund and a chargeback.
	portalEvents = "internal/billing"
	portalEvent  = "EventType"
)

// portalVendor are the vendor's issuers behind the portal: organization
// administration and trial issuance. Every exported operation either declares
// is part of the surface.
var portalVendor = []string{"internal/commercial", "internal/trial"}

// portalSigners are the contract packages whose exported Sign functions are
// the vendor's signing: entitlements and the payment events the portal
// restates. Verification beside them is customer work and is not listed.
var portalSigners = []string{"internal/entitlement", "internal/billing"}

// jsonPlumbing are the encoding/json interface methods a type declares so its
// document decodes strictly. They are how a document is read, reached only
// through the package's own Decode, and not operations of their own. The
// package-level Encode and Decode are operations: they are how the vendor's
// host persists and recovers the account state the other operations move.
var jsonPlumbing = []string{"MarshalJSON", "UnmarshalJSON", "MarshalJSONTo", "UnmarshalJSONFrom"}

// PortalSurface lists every commercial-portal source the tree declares, in
// the spelling a portal row's source uses: "portal event <type>" for each
// account event the portal carries, and "portal <package>.<Operation>" or
// "portal <package>.<Type>.<Operation>" for each exported vendor operation.
// The portal's completeness check compares this list with the ledger in both
// directions, the way the command tree, the facade and the route inventory
// are compared.
func PortalSurface(repository fs.FS) ([]string, error) {
	source := &sourceTree{fsys: repository}
	var surface []string
	files, err := source.parse(portalEvents, false)
	if err != nil {
		return nil, errors.New("portal events " + portalEvents + ": " + err.Error())
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				if kind, ok := value.Type.(*ast.Ident); !ok || kind.Name != portalEvent {
					continue
				}
				for _, literal := range value.Values {
					basic, ok := literal.(*ast.BasicLit)
					if !ok || basic.Kind != token.STRING {
						return nil, errors.New("portal event " + portalEvent + " constants are string literals")
					}
					name, err := strconv.Unquote(basic.Value)
					if err != nil {
						return nil, err
					}
					surface = append(surface, "portal event "+name)
				}
			}
		}
	}
	if len(surface) == 0 {
		return nil, errors.New(portalEvents + " declares no " + portalEvent + " constants")
	}
	for _, directory := range portalVendor {
		files, err := source.parse(directory, false)
		if err != nil {
			return nil, errors.New("portal vendor " + directory + ": " + err.Error())
		}
		for _, file := range files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if !ok || !function.Name.IsExported() {
					continue
				}
				name := function.Name.Name
				if function.Recv != nil {
					receiver := receiverType(function.Recv)
					if !ast.IsExported(receiver) || slices.Contains(jsonPlumbing, name) {
						continue
					}
					name = receiver + "." + name
				}
				surface = append(surface, "portal "+file.Name.Name+"."+name)
			}
		}
	}
	for _, directory := range portalSigners {
		files, err := source.parse(directory, false)
		if err != nil {
			return nil, errors.New("portal signer " + directory + ": " + err.Error())
		}
		for _, file := range files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if ok && function.Recv == nil && function.Name.IsExported() && strings.HasPrefix(function.Name.Name, "Sign") {
					surface = append(surface, "portal "+file.Name.Name+"."+function.Name.Name)
				}
			}
		}
	}
	slices.Sort(surface)
	return slices.Compact(surface), nil
}

// ToolingSurface lists the repository's own tooling in the spelling a tooling
// row's source uses: "make <target>" for each target the Makefile declares and
// "tools/<script>" for each tools script that is not itself a test. None of it
// is customer work; the rows record that explicitly so a customer workflow can
// never shelter behind a developer entry point.
func ToolingSurface(repository fs.FS) ([]string, error) {
	makefile, err := fs.ReadFile(repository, "Makefile")
	if err != nil {
		return nil, errors.New("cannot read the Makefile: " + err.Error())
	}
	var surface []string
	for _, match := range makeTarget.FindAllSubmatch(makefile, -1) {
		surface = append(surface, "make "+string(match[1]))
	}
	entries, err := fs.ReadDir(repository, "tools")
	if err != nil {
		return nil, errors.New("cannot read the tools directory: " + err.Error())
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasSuffix(name, ".py") && !strings.HasPrefix(name, "test_") {
			surface = append(surface, "tools/"+name)
		}
	}
	if len(surface) == 0 {
		return nil, errors.New("the repository declares no tooling")
	}
	slices.Sort(surface)
	return slices.Compact(surface), nil
}

// makeTarget matches a rule's target at the start of a Makefile line; special
// targets such as .PHONY begin with a dot and variable assignments carry no
// bare colon after the name, so neither matches.
var makeTarget = regexp.MustCompile(`(?m)^([A-Za-z][A-Za-z0-9_-]*):([^=]|$)`)

// sourceTree reads one repository tree once per directory and file, however
// many rows name the same package.
type sourceTree struct {
	fsys       fs.FS
	declared   memo
	testFuncs  memo
	testTitles memo
}

// memo is one lookup's answer per directory or file.
type memo map[string]answer

type answer struct {
	names map[string]bool
	err   error
}

func (m *memo) get(key string, read func() (map[string]bool, error)) (map[string]bool, error) {
	if *m == nil {
		*m = memo{}
	}
	if known, ok := (*m)[key]; ok {
		return known.names, known.err
	}
	names, err := read()
	(*m)[key] = answer{names, err}
	return names, err
}

// declarations is every function and Type.Method a package's non-test Go
// files declare.
func (s *sourceTree) declarations(directory string) (map[string]bool, error) {
	return s.declared.get(directory, func() (map[string]bool, error) {
		files, err := s.parse(directory, false)
		names := map[string]bool{}
		for _, file := range files {
			for _, decl := range file.Decls {
				if function, ok := decl.(*ast.FuncDecl); ok {
					if function.Recv == nil {
						names[function.Name.Name] = true
					} else {
						names[receiverType(function.Recv)+"."+function.Name.Name] = true
					}
				}
			}
		}
		return names, err
	})
}

// tests is every top-level Test function a package's test files declare.
func (s *sourceTree) tests(directory string) (map[string]bool, error) {
	return s.testFuncs.get(directory, func() (map[string]bool, error) {
		files, err := s.parse(directory, true)
		names := map[string]bool{}
		for _, file := range files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if ok && function.Recv == nil && goTestPattern.MatchString(function.Name.Name) {
					names[function.Name.Name] = true
				}
			}
		}
		return names, err
	})
}

// contractRoots are the directories whose Go source declares readmit's
// contracts; contractSkipped are the trees under them that hold no product
// source: the frontend and its dependencies, fixtures, and build output.
var (
	contractRoots   = []string{"cmd", "internal", "hub", "desktop"}
	contractSkipped = []string{"desktop/frontend", "hub/build"}
)

// contracts is every readmit contract identifier a non-test Go file of the
// tree spells as a string literal.
func (s *sourceTree) contracts() (map[string]bool, error) {
	found := map[string]bool{}
	for _, root := range contractRoots {
		err := fs.WalkDir(s.fsys, root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || slices.Contains(contractSkipped, name) {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			data, err := fs.ReadFile(s.fsys, name)
			if err != nil {
				return err
			}
			var scan scanner.Scanner
			fileSet := token.NewFileSet()
			scan.Init(fileSet.AddFile(name, -1, len(data)), data, nil, 0)
			for {
				_, kind, literal := scan.Scan()
				if kind == token.EOF {
					return nil
				}
				if kind != token.STRING {
					continue
				}
				if value, err := strconv.Unquote(literal); err == nil && contractPattern.MatchString(value) {
					found[value] = true
				}
			}
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New("cannot read the contracts under " + root + ": " + err.Error())
		}
	}
	return found, nil
}

// frontendTitle matches one test(...) or it(...) call whose title is a plain
// double-quoted literal, which is how every interaction test is titled. A
// computed title cannot be named by a ledger row, so it is not matched.
var frontendTitle = regexp.MustCompile(`(?:^|[^A-Za-z0-9_$.])(?:test|it)\(\s*"((?:[^"\\\n]|\\.)*)"`)

// titles is every interaction test title one frontend test file declares.
func (s *sourceTree) titles(file string) (map[string]bool, error) {
	return s.testTitles.get(file, func() (map[string]bool, error) {
		data, err := fs.ReadFile(s.fsys, file)
		if err != nil {
			return nil, errors.New("not a file in the repository")
		}
		names := map[string]bool{}
		for _, match := range frontendTitle.FindAllStringSubmatch(string(data), -1) {
			names[match[1]] = true
		}
		if len(names) == 0 {
			return nil, errors.New("declares no test")
		}
		return names, nil
	})
}

// parse reads the Go files directly inside one package directory: its test
// files when tests is set, and its other files otherwise. Build constraints
// are not evaluated, so a declaration in a platform-specific file counts on
// every platform.
func (s *sourceTree) parse(directory string, tests bool) ([]*ast.File, error) {
	entries, err := fs.ReadDir(s.fsys, directory)
	if err != nil {
		return nil, errors.New("not a directory in the repository")
	}
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") != tests {
			continue
		}
		data, err := fs.ReadFile(s.fsys, path.Join(directory, name))
		if err != nil {
			return nil, errors.New("cannot read " + name)
		}
		file, err := parser.ParseFile(fileSet, name, data, parser.SkipObjectResolution)
		if err != nil {
			return nil, errors.New("cannot parse " + err.Error())
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, errors.New("holds no Go files of that kind")
	}
	return files, nil
}

// receiverType is the bare type name of a method receiver: T for T, *T, T[P]
// and *T[P].
func receiverType(list *ast.FieldList) string {
	if list == nil || len(list.List) == 0 {
		return ""
	}
	expression := list.List[0].Type
	for {
		switch typed := expression.(type) {
		case *ast.StarExpr:
			expression = typed.X
		case *ast.IndexExpr:
			expression = typed.X
		case *ast.IndexListExpr:
			expression = typed.X
		case *ast.ParenExpr:
			expression = typed.X
		case *ast.Ident:
			return typed.Name
		default:
			return ""
		}
	}
}
