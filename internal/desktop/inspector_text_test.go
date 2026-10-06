package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func sourceTokens(window *desktop.RawWindow) []desktop.RawToken {
	var tokens []desktop.RawToken
	for _, line := range window.Lines {
		tokens = append(tokens, line.Tokens...)
	}
	return tokens
}

func TestLinkedInspectorTokensNavigateExactRepeatedCustomDelimiterParts(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	message := "MSH*%~\\&*A*B*C*D*20260101**ADT%A01*id*P*2.5.1\rPV1*******123%SAME&ONE\rPV1*******456%SAME&TWO~789%SAME&THREE\r"
	b := writeCase(t, root, "tokens", framed(message))
	request := desktop.InspectRequest{Workspace: root, Case: "tokens", Identity: b.Identity, Occurrence: b.Events[0].ID, ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: ownedCompositionReference(t, root)}
	view := app.InspectOccurrence(request).Inspection
	if view == nil || view.ReadableWindow == nil || len(view.ReadableWindow.Lines) != 3 {
		t.Fatalf("missing numbered source: %+v", view)
	}
	want := []string{"PV1[1]-7[1].2.1", "PV1[2]-7[1].2.1", "PV1[2]-7[2].2.1"}
	seen := 0
	for _, token := range sourceTokens(view.ReadableWindow) {
		if token.Text != "SAME" {
			continue
		}
		if seen >= len(want) || token.Path != want[seen] || token.Role != "" {
			t.Fatalf("text matching or guessed syntax role: %+v", token)
		}
		request.Path = token.Path
		next := app.InspectOccurrence(request).Inspection
		if next == nil || next.Selected.Start != token.Start || next.Selected.End != token.End || next.ReadableWindow.Selected != "SAME" {
			t.Fatalf("token did not resolve its original bytes: %+v", next)
		}
		visible := false
		for _, row := range next.Grid.Rows {
			visible = visible || row.Node.Path == token.Path
		}
		if !visible {
			t.Fatal("raw selection did not expand the grid ancestry")
		}
		marked := 0
		for _, candidate := range sourceTokens(next.ReadableWindow) {
			if candidate.Selected && candidate.Text != "" {
				marked++
				if candidate.Start != token.Start || candidate.End != token.End {
					t.Fatalf("marked another occurrence: %+v", candidate)
				}
			}
		}
		if marked != 1 {
			t.Fatalf("expected only the clicked occurrence marked; got %d", marked)
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("found %d exact occurrences", seen)
	}
	request.MaskPHI = true
	masked := app.InspectOccurrence(request).Inspection
	body, _ := json.Marshal(masked)
	if strings.Contains(string(body), "SAME") || strings.Contains(string(body), "THREE") {
		t.Fatal("source token projection bypassed PHI masking")
	}
	for _, token := range sourceTokens(masked.ReadableWindow) {
		if token.Path == want[2] && token.Role != "punctuation" && (token.Text != "####" || !token.Selected) {
			t.Fatalf("masked selection lost its path: %+v", token)
		}
	}
}

func TestLinkedInspectorEmptyNullAndEscapesKeepSourceMeaning(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	message := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\rPID||\"\"|A\\F\\B^C\r"
	b := writeCase(t, root, "empty-tokens", framed(message))
	request := desktop.InspectRequest{Workspace: root, Case: "empty-tokens", Identity: b.Identity, Occurrence: b.Events[0].ID, ByteOffset: -1, RawOffset: -1, Reveal: true}
	for _, tc := range []struct {
		path, selected string
		empty          bool
	}{{"PID[1]-1", "", true}, {"PID[1]-2", `""`, false}, {"PID[1]-3[1].1", `A\F\B`, false}, {"PID[1]-9", "", false}} {
		request.Path = tc.path
		view := app.InspectOccurrence(request).Inspection
		var selected strings.Builder
		empty := 0
		for _, token := range sourceTokens(view.ReadableWindow) {
			if token.Selected {
				selected.WriteString(token.Text)
				if token.Empty {
					empty++
				}
			}
		}
		if selected.String() != tc.selected || (empty == 1) != tc.empty {
			t.Fatalf("%s selected %q with %d empty markers", tc.path, selected.String(), empty)
		}
	}
	request.Reveal = false
	if view := app.InspectOccurrence(request).Inspection; view.ReadableWindow != nil {
		t.Fatal("hidden inspection disclosed a source projection")
	}
}

func TestLinkedInspectorNumberingAndEscapingAcrossCRLFWindows(t *testing.T) {
	app := desktop.New(nil, desktop.ShellDocuments{})
	file := filepath.Join(t.TempDir(), "numbered.hl7")
	header := "MSH|^~\\&|A|B|C|D|20260101||ADT^A01|id|P|2.5.1\r\n"
	// Put CR at the end of the first byte window and LF at the next start.
	message := header + "NTE|1||" + strings.Repeat("x", desktop.InspectorRawWindow-len(header)-len("NTE|1||")-1) + "\r\nPID|1||ID||A^B\r\n"
	if err := os.WriteFile(file, []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	listing := app.ListFileMessages(desktop.FileMessagesRequest{File: file, Format: "raw", Terminator: "crlf"})
	request := desktop.FileInspectRequest{File: file, Format: "raw", Terminator: "crlf", Expect: listing.SHA256, RawOffset: desktop.InspectorRawWindow, ByteOffset: -1, Reveal: true}
	view := app.InspectFileMessage(request).Inspection
	if view == nil || view.ReadableWindow == nil || len(view.ReadableWindow.Lines) != 1 || view.ReadableWindow.Lines[0].Number != 3 {
		t.Fatalf("CRLF boundary created or renumbered a physical line: %+v", view)
	}
	var text strings.Builder
	for _, token := range sourceTokens(view.ReadableWindow) {
		text.WriteString(token.Text)
	}
	if text.String() != "PID|1||ID||A^B" {
		t.Fatalf("window changed source text: %q", text.String())
	}
}
