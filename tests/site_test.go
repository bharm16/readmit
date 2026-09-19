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
