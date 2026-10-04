package desktop_test

import (
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/interfacespec"
	"github.com/bharm16/readmit/internal/valuemap"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSpecAndMapDiscoveryDistinguishesCurrentInvalidFutureAndUnretainedFilesWithoutBackfill(t *testing.T) {
	app, ctx, pack := profileProject(t)
	profile := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: desktop.ProfileItem, IntentID: "discovery-profile", Draft: desktop.ItemDraft{Name: "Owned profile", Profile: &desktop.ProfileDraft{Profile: localProfile(t), Pack: &pack}}})
	if profile.Saved == nil {
		t.Fatal(profile)
	}
	spec := app.SaveInterfaceSpec(desktop.InterfaceSpecSaveRequest{Context: ctx, IntentID: "discovery-spec", Name: "Owned specification", Profile: *profile.Saved, Documents: []interfacespec.Documentation{}})
	mapping := app.SaveValueMap(desktop.ValueMapSaveRequest{Context: ctx, IntentID: "discovery-map", Draft: mapDraft()})
	if spec.State != desktop.Completed || mapping.State != desktop.Completed {
		t.Fatal(spec, mapping)
	}
	specRaw, err := interfacespec.Encode(*spec.Spec)
	if err != nil {
		t.Fatal(err)
	}
	mapRaw, err := valuemap.Encode(*mapping.Map)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind      desktop.ItemKind
		schema    string
		raw       []byte
		published desktop.ItemRef
	}{{desktop.InterfaceSpecItem, interfacespec.Schema, specRaw, *spec.Ref}, {desktop.ValueMapItem, valuemap.Schema, mapRaw, *mapping.Ref}} {
		t.Run(string(row.kind), func(t *testing.T) {
			prefix := string(row.kind)
			unchanged := map[string][]byte{prefix + "-unretained.json": row.raw, prefix + "-foreign.json": []byte(strings.Replace(string(row.raw), spec.Spec.Project, "0123456789abcdef01234567", 1)), prefix + "-future.json": []byte(strings.Replace(string(row.raw), row.schema, strings.TrimSuffix(row.schema, "v1")+"v999", 1)), prefix + "-invalid.json": []byte(strings.Replace(string(row.raw), `"schema":`, `"executable":null,"schema":`, 1))}
			for name, raw := range unchanged {
				if err := os.WriteFile(filepath.Join(ctx.Project, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(filepath.Join(ctx.Project, catalog.Folder, "catalog.json"))
			if err != nil {
				t.Fatal(err)
			}
			listed := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: row.kind})
			if listed.State != desktop.Completed || listed.Page == nil {
				t.Fatal(listed)
			}
			rows := map[string]desktop.CatalogItem{}
			for _, item := range listed.Page.Items {
				rows[item.Ref.ID] = item
			}
			unretained := rows[catalog.DiscoveredID(string(row.kind), prefix+"-unretained.json")]
			if unretained.Availability != desktop.ItemUnreadable || !strings.Contains(unretained.Reason, "retained catalog revision") || unretained.Ref.Revision != "" || len(unretained.Capabilities) != 0 {
				t.Fatalf("unpublished file offered dead revision control: %+v", unretained)
			}
			invalid := rows[catalog.DiscoveredID(string(row.kind), prefix+"-invalid.json")]
			if invalid.Availability != desktop.ItemUnreadable || strings.Contains(invalid.Reason, "version this release") {
				t.Fatalf("known malformed current schema mislabeled future: %+v", invalid)
			}
			future := rows[catalog.DiscoveredID(string(row.kind), prefix+"-future.json")]
			if future.Availability != desktop.ItemUnsupported || !strings.Contains(future.Reason, "version this release") || future.Ref.Revision != "" || len(future.Capabilities) != 0 {
				t.Fatalf("future declaration lost truthful opaque state: %+v", future)
			}
			foreign := rows[catalog.DiscoveredID(string(row.kind), prefix+"-foreign.json")]
			if foreign.Availability != desktop.ItemUnreadable || !strings.Contains(foreign.Reason, "another interface project") || len(foreign.Capabilities) != 0 {
				t.Fatalf("foreign interface declaration offered an active owner: %+v", foreign)
			}
			published := rows[row.published.ID]
			if published.Availability != desktop.ItemAvailable || published.Ref.Revision != "1" || !slices.Contains(published.Capabilities, desktop.OpenAction) {
				t.Fatalf("published current revision lost its owner: %+v", published)
			}
			after, err := os.ReadFile(filepath.Join(ctx.Project, catalog.Folder, "catalog.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("read-only discovery backfilled catalog or invented revision")
			}
			for name, raw := range unchanged {
				held, err := os.ReadFile(filepath.Join(ctx.Project, name))
				if err != nil || string(held) != string(raw) {
					t.Fatal("opaque source file changed", name, err)
				}
			}
		})
	}
}
