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

// sitePageTitles is each page's browser title: the page's own name first,
// then the product, so open tabs stay distinguishable (#524).
var sitePageTitles = map[string]string{
	"index.html":       "Overview | readmit",
	"download.html":    "Download | readmit",
	"workflows.html":   "Workflows | readmit",
	"connectors.html":  "Sources | readmit",
	"limitations.html": "Limitations | readmit",
	"samples.html":     "Samples | readmit",
}

var (
	siteNavigation = regexp.MustCompile(`(?s)<nav aria-label="Main navigation">.*?</nav>`)
	siteHeadings   = regexp.MustCompile(`<h([1-6])\b`)
	siteTables     = regexp.MustCompile(`(?s)<div class="table-wrap" role="region" aria-labelledby="([^"]+)" tabindex="0">\s*<table>\s*<caption id="([^"]+)">([^<]+)</caption>\s*<thead>(.*?)</thead>\s*<tbody>(.*?)</tbody>\s*</table>\s*</div>`)
	siteHeaderCell = regexp.MustCompile(`<th scope="col">([^<]*)</th>`)
	siteCodeBlocks = regexp.MustCompile(`<pre\b[^>]*>`)
	siteLabelledBy = regexp.MustCompile(`^<pre tabindex="0" role="region" aria-labelledby="([^"]+)">$`)
)

func readSitePages(t *testing.T) map[string]string {
	t.Helper()
	pages := make(map[string]string)
	for name := range sitePageTitles {
		data, err := os.ReadFile(filepath.Join("../site", name))
		if err != nil {
			t.Fatal(err)
		}
		pages[name] = string(data)
	}
	found, _ := filepath.Glob("../site/*.html")
	if len(found) != len(sitePageTitles) {
		t.Fatalf("site has %d pages but %d titles are specified; a new page needs its title and navigation entry", len(found), len(sitePageTitles))
	}
	return pages
}

// TestPreviewSitePagesShareNavigationAndAKeyboardPathToTheirContent keeps the
// six pages one site: the same main navigation marking only the page itself as
// current, a page-first browser title, a skip link that is the first thing a
// keyboard reaches and lands on the main content, and one H1 with no heading
// level skipped beneath it.
func TestPreviewSitePagesShareNavigationAndAKeyboardPathToTheirContent(t *testing.T) {
	var shared string
	for name, html := range readSitePages(t) {
		if want := "<title>" + sitePageTitles[name] + "</title>"; strings.Count(html, "<title>") != 1 || !strings.Contains(html, want) {
			t.Errorf("%s: want the single title %s", name, want)
		}
		navs := siteNavigation.FindAllString(html, -1)
		if len(navs) != 1 || strings.Count(html, "<nav") != 1 {
			t.Errorf("%s: want exactly one navigation landmark named Main navigation", name)
			continue
		}
		current := `<a href="` + name + `" aria-current="page">`
		if strings.Count(navs[0], "aria-current") != 1 || !strings.Contains(navs[0], current) {
			t.Errorf("%s: the navigation must mark this page, and only this page, as current", name)
		}
		plain := strings.Replace(navs[0], ` aria-current="page"`, "", 1)
		if shared == "" {
			shared = plain
		} else if plain != shared {
			t.Errorf("%s: its navigation differs from the other pages'", name)
		}
		if !strings.Contains(html, "<body>\n<a class=\"skip-link\" href=\"#content\">Skip to content</a>\n<header>") {
			t.Errorf("%s: the first element of the body must be the Skip to content link", name)
		}
		if strings.Count(html, "<main") != 1 || !strings.Contains(html, `<main id="content" tabindex="-1">`) {
			t.Errorf("%s: the skip link's target must be the page's one main element", name)
		}
		levels := siteHeadings.FindAllStringSubmatch(html, -1)
		if len(levels) == 0 || levels[0][1] != "1" || strings.Count(html, "<h1") != 1 {
			t.Errorf("%s: want one H1, before any other heading", name)
			continue
		}
		previous := 1
		for _, level := range levels[1:] {
			depth := int(level[1][0] - '0')
			if depth > previous+1 {
				t.Errorf("%s: an H%d follows an H%d, skipping a level", name, depth, previous)
			}
			previous = depth
		}
	}
}

// TestPreviewSiteTablesAreCaptionedScopedAndScrollable holds every table to
// the structure a screen reader and a narrow screen need: a specific caption
// that names the keyboard-focusable scroll region around it, column headers
// scoped to their columns, every row named by a row header, and no column
// hidden, so a source's status and a workflow's boundary are never dropped to
// make a page fit. Code blocks scroll inside a named, focusable region too.
func TestPreviewSiteTablesAreCaptionedScopedAndScrollable(t *testing.T) {
	for name, html := range readSitePages(t) {
		tables := siteTables.FindAllStringSubmatch(html, -1)
		if len(tables) != strings.Count(html, "<table") {
			t.Errorf("%s: %d tables, %d inside a captioned, labelled scroll region", name, strings.Count(html, "<table"), len(tables))
		}
		captions := make(map[string]bool)
		for _, table := range tables {
			region, id, caption, head, body := table[1], table[2], strings.TrimSpace(table[3]), table[4], table[5]
			if region != id || captions[caption] || caption == "" {
				t.Errorf("%s: table %q needs its own caption naming its scroll region", name, caption)
			}
			captions[caption] = true
			columns := siteHeaderCell.FindAllStringSubmatch(head, -1)
			if len(columns) == 0 || len(columns) != strings.Count(head, "<th") || strings.Contains(head, "<td") {
				t.Errorf("%s: table %q must head every column with a column-scoped header", name, caption)
			}
			rows := strings.Count(body, "<tr>")
			if rows == 0 || strings.Count(body, `<tr><th scope="row">`) != rows || strings.Count(body, "<th") != rows {
				t.Errorf("%s: every row of table %q must start with its own row header", name, caption)
			}
			for _, row := range strings.Split(body, "</tr>")[:rows] {
				if cells := strings.Count(row, "<th") + strings.Count(row, "<td"); cells != len(columns) {
					t.Errorf("%s: a row of table %q has %d cells for %d columns", name, caption, cells, len(columns))
				}
			}
			var headers []string
			for _, column := range columns {
				headers = append(headers, column[1])
			}
			switch name {
			case "connectors.html":
				if len(headers) < 2 || headers[0] != "Source" || headers[1] != "Status" {
					t.Errorf("%s: table %q must keep each source's status beside it, got %v", name, caption, headers)
				}
			case "workflows.html":
				if strings.Join(headers, ",") != "Workflow,Command,Boundary,Documentation" {
					t.Errorf("%s: table %q must keep its Boundary column, got %v", name, caption, headers)
				}
			}
		}
		for _, pre := range siteCodeBlocks.FindAllString(html, -1) {
			labelled := siteLabelledBy.FindStringSubmatch(pre)
			if labelled == nil || !strings.Contains(html, `id="`+labelled[1]+`"`) {
				t.Errorf("%s: code block %s must scroll inside a focusable region named by a heading or line on the page", name, pre)
			}
		}
	}
	// Inbound MLLP's transport scope stays beside its shorter label (#524 WB38).
	connectors, err := os.ReadFile("../site/connectors.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(connectors), `<th scope="row">Inbound MLLP <span class="qualifier">Plain · TLS · mutual TLS</span></th>`) {
		t.Error("connectors.html: Inbound MLLP must show Plain · TLS · mutual TLS beside its label")
	}
	css, err := os.ReadFile("../site/style.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(css)
	for _, required := range []string{".skip-link:focus", ":focus-visible", ".table-wrap { overflow-x: auto;"} {
		if !strings.Contains(style, required) {
			t.Errorf("style.css lacks %q", required)
		}
	}
	for _, hiding := range []string{"display: none", "visibility: hidden"} {
		if strings.Contains(style, hiding) {
			t.Errorf("style.css contains %q; no page content is hidden to make a page fit", hiding)
		}
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
