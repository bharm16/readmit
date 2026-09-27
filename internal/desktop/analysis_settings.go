package desktop

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/operation"
)

// Analysis settings are named diagnosis configurations: the
// readmit-diagnose-config/v1 document `readmit diagnose --config` reads,
// saved whole as one revision of a catalog object, or discovered where a
// person placed or imported one. A configuration naming a profile or ruleset
// this release does not define is still a valid document: it stays exactly
// as imported and is reported as unsupported, never changed into one this
// release runs.

// AnalysisSettingsSummary is one named configuration: the profile and
// ruleset it selects, by their contract names and by the name a person reads,
// how many rules and namespace mappings it declares, and whether this
// release defines that profile and ruleset as a pair.
type AnalysisSettingsSummary struct {
	Profile     string `json:"profile"`
	ProfileName string `json:"profile_name"`
	Ruleset     string `json:"ruleset"`
	Rules       int    `json:"rules"`
	Namespaces  int    `json:"namespaces"`
	Supported   bool   `json:"supported"`
}

// profileNames are the names a person reads for the profiles this release
// defines. Any other profile is named by its own token.
var profileNames = map[string]string{
	diagnose.Profile:          "SIU",
	diagnose.LifecycleProfile: "Lifecycle",
	diagnose.OrderProfile:     "Orders",
}

// profileName is the name a person reads for one profile token.
func profileName(profile string) string {
	if name, ok := profileNames[profile]; ok {
		return name
	}
	return profile
}

// supportedPair reports whether this release defines the profile and ruleset
// a configuration selects as one pair.
func supportedPair(config diagnose.Config) bool {
	for _, builtin := range diagnosisBuiltins {
		if defined := builtin.config(); defined.Profile == config.Profile && defined.Ruleset == config.Ruleset {
			return true
		}
	}
	return false
}

// encodeAnalysisSettings writes a configuration as the document it is saved
// as: deterministic, indented, one trailing newline.
func encodeAnalysisSettings(config diagnose.Config) ([]byte, error) {
	data, err := json.Marshal(config, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New("the diagnosis configuration cannot be encoded")
	}
	return append(data, '\n'), nil
}

// validateAnalysisSettings validates a configuration through the strict
// parser `readmit diagnose --config` applies and answers the one file a save
// stages. A draft that declares no schema is this contract's.
func validateAnalysisSettings(config diagnose.Config) ([]catalog.Staged, *diagnose.Config, []FieldProblem) {
	if config.Schema == "" {
		config.Schema = diagnose.ConfigSchema
	}
	if config.Namespaces == nil {
		config.Namespaces = []diagnose.Namespace{}
	}
	data, err := encodeAnalysisSettings(config)
	if err == nil {
		config, err = diagnose.ParseConfig(data)
	}
	if err != nil {
		return nil, nil, []FieldProblem{{Field: "analysis_settings", Problem: err.Error()}}
	}
	return []catalog.Staged{{Role: "config", File: "config.json", Data: data}}, &config, nil
}

// readAnalysisSettingsFile reads one saved or discovered configuration.
func readAnalysisSettingsFile(path string) (diagnose.Config, error) {
	data, err := boundedFile(path, diagnose.MaxConfigBytes)
	if err != nil {
		return diagnose.Config{}, err
	}
	return diagnose.ParseConfig(data)
}

func readAnalysisSettings(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	config, err := readAnalysisSettingsFile(paths[primaryRole(AnalysisSettingsItem)])
	if err != nil {
		return view{}, err
	}
	return view{summary: ItemSummary{AnalysisSettings: &AnalysisSettingsSummary{Profile: config.Profile, ProfileName: profileName(config.Profile),
		Ruleset: config.Ruleset, Rules: len(config.Rules), Namespaces: len(config.Namespaces), Supported: supportedPair(config)}}}, nil
}

// settingsOf reads the configuration an available analysis-settings object
// declares now.
func (c *loadedCatalog) settingsOf(item catalog.Item) (diagnose.Config, error) {
	paths, availability, reason := c.backing(item)
	if availability != ItemAvailable {
		return diagnose.Config{}, errors.New(reason)
	}
	return readAnalysisSettingsFile(paths[primaryRole(AnalysisSettingsItem)])
}

// AnalysisProfileRef chooses the one configuration an analysis runs under:
// a built-in selection by the id the vocabulary publishes, or a saved
// analysis-settings object. Exactly one is named; nothing is chosen for a
// person.
type AnalysisProfileRef struct {
	Builtin  string   `json:"builtin,omitzero"`
	Settings *ItemRef `json:"settings,omitzero"`
}

// AnalysisProfile is one configuration an analysis of a case could run
// under, and what the engine says about running it over that case before
// anything runs: Compatible when it evaluates the profile as selected, and
// otherwise the engine's own refusals. Refusals also lists a configured rule
// this release does not define, which is skipped while the rest run.
type AnalysisProfile struct {
	Builtin      string                 `json:"builtin,omitzero"`
	Settings     *ItemRef               `json:"settings,omitzero"`
	Name         string                 `json:"name"`
	Profile      string                 `json:"profile"`
	ProfileName  string                 `json:"profile_name"`
	Ruleset      string                 `json:"ruleset"`
	ConfigSHA256 string                 `json:"config_sha256"`
	Compatible   bool                   `json:"compatible"`
	Refusals     []diagnose.Unsupported `json:"refusals"`
}

// AnalysisProfilesResult lists every configuration offered for one case.
type AnalysisProfilesResult struct {
	State    State             `json:"state"`
	Reason   string            `json:"reason,omitzero"`
	Context  RequestContext    `json:"context"`
	Profiles []AnalysisProfile `json:"profiles"`
}

func (r *AnalysisProfilesResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ListAnalysisProfiles lists the built-in configurations and every saved or
// discovered analysis-settings object of the project, each checked against
// the case the request names through the engine's own preflight
// (diagnose.Check). It runs nothing and writes nothing.
func (a *App) ListAnalysisProfiles(request ItemRequest) AnalysisProfilesResult {
	return run(a, false, false, func(ctx context.Context) AnalysisProfilesResult {
		result := AnalysisProfilesResult{Context: request.Context, Profiles: []AnalysisProfile{}}
		loaded, entry, declined := a.caseEntry(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		path := filepath.Join(loaded.root, entry)
		for _, builtin := range diagnosisBuiltins {
			config := builtin.config()
			profile, err := checkedProfile(path, config, profileName(config.Profile))
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			profile.Builtin = builtin.id
			result.Profiles = append(result.Profiles, profile)
		}
		for _, item := range loaded.list(AnalysisSettingsItem) {
			if item.Availability != ItemAvailable {
				continue
			}
			config, err := loaded.settingsOf(loaded.document.Items[loaded.document.Find(item.Ref.ID)])
			if err != nil {
				continue
			}
			profile, err := checkedProfile(path, config, cmp.Or(item.Name, profileName(config.Profile)))
			if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
			ref := item.Ref
			profile.Settings = &ref
			result.Profiles = append(result.Profiles, profile)
		}
		result.State = Completed
		return result
	})
}

// checkedProfile is one configuration as the engine's preflight judges it
// over the case at path.
func checkedProfile(path string, config diagnose.Config, name string) (AnalysisProfile, error) {
	items, err := diagnose.Check(path, config)
	if err != nil {
		return AnalysisProfile{}, err
	}
	sha, err := diagnose.ConfigSHA256(config)
	if err != nil {
		return AnalysisProfile{}, err
	}
	return AnalysisProfile{Name: name, Profile: config.Profile, ProfileName: profileName(config.Profile), Ruleset: config.Ruleset,
		ConfigSHA256: sha, Compatible: !slices.ContainsFunc(items, diagnose.Refuses), Refusals: items}, nil
}

// analysisProfile resolves the one configuration a request chose.
func (c *loadedCatalog) analysisProfile(choice AnalysisProfileRef) (diagnose.Config, refusal) {
	switch {
	case choice.Builtin != "" && choice.Settings == nil:
		if config, ok := builtinConfig(choice.Builtin); ok {
			return config, refusal{}
		}
	case choice.Builtin == "" && choice.Settings != nil:
		index := c.document.Find(choice.Settings.ID)
		if index < 0 || c.document.Items[index].Kind != string(AnalysisSettingsItem) || c.removed(c.document.Items[index]) {
			return diagnose.Config{}, refusal{Failed, "the project holds no such analysis settings"}
		}
		record := c.document.Items[index]
		if choice.Settings.Revision != "" && choice.Settings.Revision != record.RevisionLabel() {
			return diagnose.Config{}, refusal{Failed, "the analysis settings changed since they were shown; look at them again"}
		}
		config, err := c.settingsOf(record)
		if err != nil {
			return diagnose.Config{}, refusal{Failed, err.Error()}
		}
		return config, refusal{}
	}
	return diagnose.Config{}, refusal{Failed, "an analysis runs under one named profile: a built-in selection (" + builtinNames() + ") or saved analysis settings"}
}

// configName is the name a person reads for the configuration an analysis
// ran under: the built-in selection's name, or the name of the available
// analysis settings whose current revision is that configuration. A
// configuration the project no longer offers has no name.
func (c *loadedCatalog) configName(sha string) string {
	if c.configNames == nil {
		c.configNames = map[string]string{}
		for _, builtin := range diagnosisBuiltins {
			config := builtin.config()
			if sha, err := diagnose.ConfigSHA256(config); err == nil {
				c.configNames[sha] = profileName(config.Profile)
			}
		}
		for _, record := range c.document.Items {
			if record.Kind != string(AnalysisSettingsItem) || c.removed(record) {
				continue
			}
			config, err := c.settingsOf(record)
			if err != nil {
				continue
			}
			if sha, err := diagnose.ConfigSHA256(config); err == nil && c.configNames[sha] == "" {
				c.configNames[sha] = cmp.Or(record.Name, profileName(config.Profile))
			}
		}
	}
	return c.configNames[sha]
}

// offeredConfigs are the configuration identities an analysis can run under
// now: every built-in selection and the current revision of every available
// analysis-settings object. An analysis made under any other configuration
// is history, not the current reading of its case.
func (c *loadedCatalog) offeredConfigs() map[string]bool {
	offered := map[string]bool{}
	for _, builtin := range diagnosisBuiltins {
		if sha, err := diagnose.ConfigSHA256(builtin.config()); err == nil {
			offered[sha] = true
		}
	}
	for _, record := range c.document.Items {
		if record.Kind != string(AnalysisSettingsItem) || c.removed(record) {
			continue
		}
		if config, err := c.settingsOf(record); err == nil {
			if sha, err := diagnose.ConfigSHA256(config); err == nil {
				offered[sha] = true
			}
		}
	}
	return offered
}

// ImportAnalysisSettings opens a readmit-diagnose-config/v1 file a person
// chose as a new, unsaved analysis-settings draft of the open project, named
// after the file. The file is read through the strict parser `readmit
// diagnose --config` applies, and its configuration is answered exactly as
// read: one naming a profile or ruleset this release does not define stays
// that, never changed into one it runs. Nothing is saved.
func (a *App) ImportAnalysisSettings(request RequestContext) ItemDraftResult {
	return run(a, true, false, func(ctx context.Context) ItemDraftResult {
		result := ItemDraftResult{Context: request}
		if loaded, declined := a.loadCatalog(ctx, request, false); loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		files, declined := a.chooseFiles(ctx, "Import analysis settings", "Analysis settings", "*.json")
		if len(files) == 0 {
			result.refuse(declined.state, declined.reason)
			return result
		}
		config, err := readAnalysisSettingsFile(files[0])
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		draft := ItemDraft{AnalysisSettings: &config}
		if name := strings.TrimSuffix(filepath.Base(files[0]), filepath.Ext(files[0])); catalog.ValidName(name) {
			draft.Name = name
		}
		result.State, result.New, result.Ref, result.Draft, result.Problems = Completed, true, &ItemRef{Kind: AnalysisSettingsItem}, &draft, []FieldProblem{}
		return result
	})
}

// ExportSettingsResult is where exported analysis settings were written.
type ExportSettingsResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Path    string         `json:"path,omitzero"`
}

func (r *ExportSettingsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExportAnalysisSettings writes the exact bytes of an analysis-settings
// object's current revision, the readmit-diagnose-config/v1 document `readmit
// diagnose --config` reads, to a new file the person names in the save
// dialog, and reads them back. The saved document is written as it is, never
// regenerated. A reference naming a revision that is no longer current is
// refused.
func (a *App) ExportAnalysisSettings(request ItemRequest) ExportSettingsResult {
	return run(a, true, true, func(ctx context.Context) ExportSettingsResult {
		result := ExportSettingsResult{Context: request.Context}
		if request.Ref.Kind != AnalysisSettingsItem {
			result.refuse(Failed, "only analysis settings are exported as analysis settings")
			return result
		}
		loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
		if request.Ref.Revision != "" && request.Ref.Revision != record.RevisionLabel() {
			result.refuse(Failed, "the analysis settings changed since they were shown; look at them again")
			return result
		}
		paths, availability, reason := loaded.backing(record)
		if availability != ItemAvailable {
			result.refuse(Failed, reason)
			return result
		}
		data, err := boundedFile(paths[primaryRole(AnalysisSettingsItem)], diagnose.MaxConfigBytes)
		if err == nil {
			_, err = diagnose.ParseConfig(data)
		}
		if err != nil {
			result.refuse(Failed, "these analysis settings cannot be read: "+err.Error())
			return result
		}
		name := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ':' {
				return '-'
			}
			return r
		}, cmp.Or(item.Name, "analysis settings"))
		named, declined := a.chooseNamedDestination(ctx, "Export analysis settings", name+".json")
		if named == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		destination, err := artifactpath.Destination(named)
		if err != nil {
			result.refuse(Failed, "analysis settings are exported outside retained evidence")
			return result
		}
		if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
			result.refuse(Failed, "a file is already there; name a new file for the analysis settings")
			return result
		}
		if err := operation.WriteNewFile(destination, data, "cannot create the file", "cannot write the file"); err != nil {
			result.refuse(Failed, "the analysis settings must be exported to a new file in a folder this account can write")
			return result
		}
		if written, err := os.ReadFile(destination); err != nil || !bytes.Equal(written, data) {
			result.refuse(Failed, "the exported analysis settings could not be verified after writing")
			return result
		}
		result.State, result.Path = Completed, destination
		return result
	})
}
