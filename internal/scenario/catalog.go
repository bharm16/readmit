package scenario

import (
	"errors"
	"slices"
)

// EventAvailability reports one trigger event relative to a chosen lifecycle
// profile. When Available is false, Reason names why the event cannot be
// authored under that profile — never a substitute workflow.
type EventAvailability struct {
	Event       Event  `json:"event"`
	Description string `json:"description"`
	Kind        Kind   `json:"kind"`
	Profile     string `json:"profile"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitzero"`
}

// KindAvailability reports one subject kind a profile carries, with the states
// a subject of that kind may begin in.
type KindAvailability struct {
	Kind   Kind    `json:"kind"`
	States []State `json:"states"`
}

// ProfileCatalog is one implemented lifecycle profile and what it declares.
type ProfileCatalog struct {
	Name   ProfileName         `json:"name"`
	Kinds  []KindAvailability  `json:"kinds"`
	Events []EventAvailability `json:"events"`
	Schema string              `json:"schema"`
	Order  bool                `json:"order"`
}

// Catalog is the closed set of supported lifecycle profiles, events and kinds
// the shared engine implements. The desktop authors from this list; an event
// missing here is unavailable by name rather than silently remapped.
type Catalog struct {
	Profiles         []ProfileCatalog    `json:"profiles"`
	GeneratorVersion string              `json:"generator_version"`
	AllEvents        []EventAvailability `json:"all_events"`
}

// SupportedCatalog returns every implemented lifecycle profile and the events
// each one declares, plus the cross-profile unavailable set so a UI can show
// why an ADT event cannot be used on an SIU sequence (and vice versa).
func SupportedCatalog() Catalog {
	names := append(slices.Clone(implemented), ORMLifecycle, ORULifecycle)
	catalog := Catalog{
		GeneratorVersion: "readmit-scenario-generator-v1",
		Profiles:         make([]ProfileCatalog, 0, len(names)),
	}
	owners := map[Event]EventAvailability{}
	for _, name := range names {
		bound, ok := resolveProfile(name)
		if !ok {
			continue
		}
		entry := ProfileCatalog{
			Name:   name,
			Schema: schemaFor(name),
			Order:  isOrderProfile(name),
			Kinds:  kindList(bound),
			Events: make([]EventAvailability, 0, len(bound.events)),
		}
		for event, transition := range bound.events {
			item := EventAvailability{
				Event: event, Description: transition.description,
				Kind: transition.kind, Profile: string(name), Available: true,
			}
			entry.Events = append(entry.Events, item)
			if _, seen := owners[event]; !seen {
				owners[event] = item
			}
		}
		slices.SortFunc(entry.Events, compareEvents)
		catalog.Profiles = append(catalog.Profiles, entry)
	}
	for _, name := range names {
		bound, ok := resolveProfile(name)
		if !ok {
			continue
		}
		for event, owner := range owners {
			if _, declared := bound.events[event]; declared {
				continue
			}
			catalog.AllEvents = append(catalog.AllEvents, EventAvailability{
				Event: event, Description: owner.Description, Kind: owner.Kind,
				Profile: string(name), Available: false,
				Reason: "profile " + string(name) + " declares no event " + string(event) +
					"; it belongs to " + owner.Profile,
			})
		}
	}
	slices.SortFunc(catalog.AllEvents, compareEventsByProfile)
	return catalog
}

// EventsFor returns the events available under one profile together with the
// unavailable events from every other profile, each carrying its refusal reason.
func EventsFor(named ProfileName) ([]EventAvailability, error) {
	bound, ok := resolveProfile(named)
	if !ok {
		return nil, errors.New("a scenario names one implemented lifecycle profile; supported: " +
			string(ADTLifecycle) + ", " + string(SIULifecycle) + ", " +
			string(ORMLifecycle) + ", " + string(ORULifecycle))
	}
	available := make([]EventAvailability, 0, len(bound.events)+16)
	for event, transition := range bound.events {
		available = append(available, EventAvailability{
			Event: event, Description: transition.description,
			Kind: transition.kind, Profile: string(named), Available: true,
		})
	}
	for _, item := range SupportedCatalog().AllEvents {
		if item.Profile == string(named) {
			available = append(available, item)
		}
	}
	slices.SortFunc(available, compareEvents)
	return available, nil
}

func resolveProfile(named ProfileName) (profile, bool) {
	if bound, ok := profiles[named]; ok {
		return bound, true
	}
	if bound, ok := orderProfiles[named]; ok {
		return bound, true
	}
	return profile{}, false
}

func isOrderProfile(named ProfileName) bool {
	_, ok := orderProfiles[named]
	return ok
}

func schemaFor(named ProfileName) string {
	if isOrderProfile(named) {
		return OrderSchema
	}
	return Schema
}

func kindList(bound profile) []KindAvailability {
	kinds := make([]KindAvailability, 0, len(bound.states))
	for kind, states := range bound.states {
		list := make([]State, 0, len(states))
		for state := range states {
			list = append(list, state)
		}
		slices.Sort(list)
		kinds = append(kinds, KindAvailability{Kind: kind, States: list})
	}
	slices.SortFunc(kinds, func(a, b KindAvailability) int {
		if a.Kind < b.Kind {
			return -1
		}
		if a.Kind > b.Kind {
			return 1
		}
		return 0
	})
	return kinds
}

func compareEvents(a, b EventAvailability) int {
	if a.Available != b.Available {
		if a.Available {
			return -1
		}
		return 1
	}
	if a.Event < b.Event {
		return -1
	}
	if a.Event > b.Event {
		return 1
	}
	return 0
}

func compareEventsByProfile(a, b EventAvailability) int {
	if a.Profile != b.Profile {
		if a.Profile < b.Profile {
			return -1
		}
		return 1
	}
	if a.Event < b.Event {
		return -1
	}
	if a.Event > b.Event {
		return 1
	}
	return 0
}
