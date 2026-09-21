package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The preview site under site/ and the support matrix are public claims about
// this repository. These tests keep them honest mechanically: no script, form,
// image or external resource on any page; every link to a repository file
// resolves; and every test a claim cites still exists by that name.

const repositoryBlob = "https://github.com/bharm16/readmit/blob/main/"

var (
	attributeLinks = regexp.MustCompile(`(?i)\b(?:href|src)="([^"]*)"`)
	markdownLinks  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	citedTests     = regexp.MustCompile("`(Test[A-Z][A-Za-z0-9]*)`")
)

func repositoryFileExists(t *testing.T, relative string) bool {
	t.Helper()
	if strings.Contains(relative, "..") {
		return false
	}
	_, err := os.Stat(filepath.Join("..", filepath.FromSlash(relative)))
	return err == nil
}

func TestPreviewSiteIsStaticAndEveryLinkResolves(t *testing.T) {
	pages, err := filepath.Glob("../site/*.html")
	if err != nil || len(pages) == 0 {
		t.Fatalf("no site pages: %v", err)
	}
	for _, page := range pages {
		data, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		html := string(data)
		lower := strings.ToLower(html)
		for _, forbidden := range []string{"<script", "<form", "<iframe", "<img", "<video", "<audio", "<object", "<embed", "<input", "@import", "url("} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains %q; the preview site is plain HTML with one stylesheet", filepath.Base(page), forbidden)
			}
		}
		if !strings.Contains(html, `<link rel="stylesheet" href="style.css">`) || strings.Count(lower, "<link") != 1 {
			t.Errorf("%s must load exactly one stylesheet, style.css", filepath.Base(page))
		}
		for _, match := range attributeLinks.FindAllStringSubmatch(html, -1) {
			target := match[1]
			switch {
			case target == "style.css":
			case strings.HasPrefix(target, "#"):
			case strings.HasPrefix(target, repositoryBlob):
				relative, _, _ := strings.Cut(strings.TrimPrefix(target, repositoryBlob), "#")
				if !repositoryFileExists(t, relative) {
					t.Errorf("%s links to %s, which is not a file in this repository", filepath.Base(page), target)
				}
			case strings.HasPrefix(target, "https://github.com/bharm16/readmit/releases"), strings.HasPrefix(target, "https://github.com/bharm16/readmit/issues"), strings.HasPrefix(target, "https://github.com/bharm16/readmit/tree/main/"), target == "https://github.com/bharm16/readmit", strings.HasPrefix(target, "https://go.dev/"):
			case strings.Contains(target, "://") || strings.HasPrefix(target, "//"):
				t.Errorf("%s reaches outside the site to %s", filepath.Base(page), target)
			default:
				local, _, _ := strings.Cut(target, "#")
				if _, err := os.Stat(filepath.Join("../site", local)); err != nil {
					t.Errorf("%s links to a missing site page %s", filepath.Base(page), target)
				}
			}
		}
	}
	css, err := os.ReadFile("../site/style.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"@import", "url(", "http"} {
		if strings.Contains(strings.ToLower(string(css)), forbidden) {
			t.Errorf("style.css contains %q; the stylesheet fetches nothing", forbidden)
		}
	}
	if entries, _ := os.ReadDir("../site/images"); len(entries) > 0 {
		t.Error("site/images holds files, but no page shows a screenshot; either is a claim the other must match")
	}
}

func TestEveryPublishedClaimCitesAFileAndATestThatExist(t *testing.T) {
	defined := make(map[string]bool)
	for _, root := range []string{"../tests", "../internal", "../desktop"} {
		err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			for _, match := range regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`).FindAllStringSubmatch(string(data), -1) {
				defined[match[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, document := range []string{"../site/CLAIMS.md", "../docs/support-matrix.md", "../samples/synthetic-walkthrough/README.md"} {
		data, err := os.ReadFile(document)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, match := range markdownLinks.FindAllStringSubmatch(text, -1) {
			target := match[1]
			var relative string
			switch {
			case strings.HasPrefix(target, repositoryBlob):
				relative = strings.TrimPrefix(target, repositoryBlob)
			case strings.HasPrefix(target, "#"), strings.Contains(target, "://"):
				continue
			default:
				relative = filepath.ToSlash(filepath.Join(filepath.Dir(strings.TrimPrefix(document, "../")), target))
			}
			relative, _, _ = strings.Cut(relative, "#")
			if !repositoryFileExists(t, relative) {
				t.Errorf("%s links to %s, which does not exist", document, target)
			}
		}
		for _, match := range citedTests.FindAllStringSubmatch(text, -1) {
			if !defined[match[1]] {
				t.Errorf("%s cites %s, which no test file defines", document, match[1])
			}
		}
	}
}

// cobraBuiltins are the framework's own documentation commands; they ship
// with the executable but are not workflows the matrix or README catalogue.
var cobraBuiltins = map[string]bool{"help": true, "completion": true}

// workflowListed answers whether a published command is named by a backticked
// span that starts with it, so `backup create/verify/restore` answers for
// `backup` while `license runner init` does not answer for `runner`. A command
// name continues through letters, digits and dashes, so `replay-target`
// cannot masquerade as `replay`.
func workflowListed(text, name string) bool {
	return regexp.MustCompile("`" + regexp.QuoteMeta(name) + "(`|[^A-Za-z0-9-])").MatchString(text)
}

// rootWorkflows lists the top-level commands the executable itself publishes,
// which is the interface a reader can run; the help text is its own inventory.
func rootWorkflows(t *testing.T) []string {
	t.Helper()
	stdout, stderr, err := run(t, "--help")
	if err != nil || stderr != "" {
		t.Fatalf("help: %v %s", err, stderr)
	}
	var names []string
	listing := false
	for _, line := range strings.Split(stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "Available Commands:"):
			listing = true
		case listing && strings.HasPrefix(line, "  "):
			if fields := strings.Fields(line); len(fields) > 0 {
				names = append(names, fields[0])
			}
		case listing:
			if len(names) > 0 {
				return names
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("help named no commands:\n%s", stdout)
	}
	return names
}

// TestTheSupportMatrixCarriesEveryRegisteredWorkflow closes the matrix's own
// completeness claim in the direction no earlier check covered: every command
// the executable publishes has a row in the Workflows table, so silence is
// never mistaken for a status.
func TestTheSupportMatrixCarriesEveryRegisteredWorkflow(t *testing.T) {
	matrix, err := os.ReadFile("../docs/support-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	var table string
	if _, remainder, found := strings.Cut(string(matrix), "## Workflows"); found {
		table, _, _ = strings.Cut(remainder, "\n## ")
	} else {
		t.Fatal("support matrix has no Workflows table")
	}
	for _, name := range rootWorkflows(t) {
		if cobraBuiltins[name] {
			continue
		}
		if !workflowListed(table, name) {
			t.Errorf("workflow %q is published by the executable but has no row in the support matrix's Workflows table; the matrix is the single source the site draws its claims from", name)
		}
	}
}

// TestTheReadmeCataloguesEveryRegisteredWorkflow keeps the README's front door
// honest the same way the matrix is: every published command appears in the
// catalog, so the README can never list fewer workflows than the executable
// runs.
func TestTheReadmeCataloguesEveryRegisteredWorkflow(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	var catalog string
	if _, remainder, found := strings.Cut(string(readme), "## Available workflows"); found {
		catalog, _, _ = strings.Cut(remainder, "\n## ")
	} else {
		t.Fatal("README has no Available workflows catalog")
	}
	for _, name := range rootWorkflows(t) {
		if cobraBuiltins[name] {
			continue
		}
		if !workflowListed(catalog, name) {
			t.Errorf("workflow %q is published by the executable but the README catalog does not list it", name)
		}
	}
}

// TestPublishedVersionLiteralsAgreeWithTheSupportMatrix gives the one claim
// category the CLAIMS contract deliberately exempts — release currency — a
// single home: the matrix's "Latest published archive" row. Every prerelease
// version literal the README or the site prints must repeat that value, so the
// next tag is edited once and every straggler fails by name.
func TestPublishedVersionLiteralsAgreeWithTheSupportMatrix(t *testing.T) {
	matrix, err := os.ReadFile("../docs/support-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	row := ""
	for _, line := range strings.Split(string(matrix), "\n") {
		if strings.Contains(line, "Latest published archive") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("support matrix states no latest published archive")
	}
	// Published archives are prereleases today, so the tag shape this sweep
	// recognizes is the prerelease shape; the first stable tag is a deliberate
	// act that widens this expression beside the release row it changes.
	prereleaseTag := regexp.MustCompile(`v?(\d+\.\d+\.\d+-alpha\.\d+)\b`)
	published := prereleaseTag.FindStringSubmatch(row)
	if published == nil {
		t.Fatalf("the release row names no prerelease archive: %s", row)
	}
	swept, err := filepath.Glob("../site/*.html")
	if err != nil {
		t.Fatal(err)
	}
	swept = append(swept, "../site/CLAIMS.md", "../README.md")
	for _, document := range swept {
		data, err := os.ReadFile(document)
		if err != nil {
			t.Fatal(err)
		}
		for _, found := range prereleaseTag.FindAllStringSubmatch(string(data), -1) {
			if found[1] != published[1] {
				t.Errorf("%s prints %s, but the support matrix publishes %s; the matrix row is the single home of that value", document, found[1], published[1])
			}
		}
	}
}
