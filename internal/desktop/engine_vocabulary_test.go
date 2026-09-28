package desktop_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
)

// The Format step offers engine exports from the vocabulary, never from a
// list of its own: the vocabulary names exactly the engine, version, format
// and terminator combinations the engine export reader accepts, and a
// version it does not name is refused by preview in the reader's words.
func TestTheVocabularysEngineExportsAreExactlyTheOnesTheReaderAccepts(t *testing.T) {
	engines := workspaceApp(t).Shell().Shell.Vocabulary.ImportEngines
	names := map[string]string{}
	for _, engine := range engines {
		names[engine.Engine+" "+engine.Version] = engine.Name
	}
	if len(engines) != 2 || names["mirth 4.5.2"] != "Mirth Connect" || names["oie 4.6.0"] != "Open Integration Engine" {
		t.Fatalf("the vocabulary's engines: %+v", engines)
	}
	candidates := struct{ engines, versions, formats, terminators []string }{
		[]string{"mirth", "oie", "rhapsody"}, []string{"4.5.2", "4.6.0", "4.5.1", "4.6"},
		[]string{"raw", "message-xml", "xml"}, []string{"cr", "lf", "crlf", "\r"},
	}
	for _, engine := range candidates.engines {
		for _, version := range candidates.versions {
			for _, format := range candidates.formats {
				for _, terminator := range candidates.terminators {
					plan := engineexport.Plan{Schema: engineexport.Schema, Engine: engine, Version: version, Format: engineexport.Format(format), Terminator: hl7.Terminator(terminator)}
					offered := slices.ContainsFunc(engines, func(named engineexport.Engine) bool {
						return named.Engine == engine && named.Version == version && slices.Contains(named.Formats, engineexport.Format(format)) && slices.Contains(named.Terminators, hl7.Terminator(terminator))
					})
					if accepted := plan.Validate() == nil; accepted != offered {
						t.Errorf("%+v: the reader accepts it %v, the vocabulary offers it %v", plan, accepted, offered)
					}
				}
			}
		}
	}

	app := workspaceApp(t)
	export := filepath.Join(t.TempDir(), "export.hl7")
	if err := os.WriteFile(export, []byte(sampleImportHL7), 0o600); err != nil {
		t.Fatal(err)
	}
	unsupported := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.1", Format: "raw", Terminator: "cr"}
	preview := app.PreviewImport(desktop.ImportRequest{Mode: "engine", EnginePlan: &unsupported, Files: []string{export}})
	if preview.State != desktop.Failed || preview.Reason != engineexport.ErrUnsupported.Error() {
		t.Fatalf("an unsupported engine version previewed as %s %q", preview.State, preview.Reason)
	}
}
