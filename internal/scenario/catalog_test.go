package scenario_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/scenario"
)

func TestSupportedCatalogListsEveryLifecycleAndMarksCrossProfileEventsUnavailable(t *testing.T) {
	catalog := scenario.SupportedCatalog()
	if catalog.GeneratorVersion != "readmit-scenario-generator-v1" {
		t.Fatalf("generator version: %q", catalog.GeneratorVersion)
	}
	if len(catalog.Profiles) != 4 {
		t.Fatalf("expected four lifecycle profiles, got %d", len(catalog.Profiles))
	}
	byName := map[scenario.ProfileName]scenario.ProfileCatalog{}
	for _, profile := range catalog.Profiles {
		byName[profile.Name] = profile
	}
	for _, name := range []scenario.ProfileName{
		scenario.ADTLifecycle, scenario.SIULifecycle, scenario.ORMLifecycle, scenario.ORULifecycle,
	} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing profile %s", name)
		}
	}
	adt := byName[scenario.ADTLifecycle]
	if len(adt.Events) == 0 || adt.Schema != scenario.Schema {
		t.Fatalf("ADT catalog: %+v", adt)
	}
	events, err := scenario.EventsFor(scenario.SIULifecycle)
	if err != nil {
		t.Fatal(err)
	}
	var sawS12, sawA01 bool
	for _, event := range events {
		switch event.Event {
		case "S12":
			sawS12 = true
			if !event.Available {
				t.Fatalf("S12 must be available on SIU: %+v", event)
			}
		case "A01":
			sawA01 = true
			if event.Available || event.Reason == "" {
				t.Fatalf("A01 must be unavailable on SIU with a reason: %+v", event)
			}
		}
	}
	if !sawS12 || !sawA01 {
		t.Fatalf("SIU events incomplete: sawS12=%v sawA01=%v", sawS12, sawA01)
	}
	if _, err := scenario.EventsFor("readmit-unknown-lifecycle-v1"); err == nil {
		t.Fatal("unknown profile must be refused")
	}
}
