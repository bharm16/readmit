package connectedrun

import (
	"strings"

	"github.com/bharm16/readmit/internal/connectedtest"
)

// Child evidence is never ignored merely because its parent intent or claimed
// result was removed. Such contradictory history cannot become unattempted work.
func validateFlowInventory(files map[string][]byte, plan *connectedtest.FlowPlan, inherited int) error {
	phases := plan.Document().Test.Phases
	if inherited < 0 || inherited > len(phases) {
		return invalid
	}
	known := map[string]int{}
	for i, p := range phases {
		known[p.ID] = i
	}
	for name := range files {
		var id string
		child, checkpoint := false, false
		switch {
		case strings.HasPrefix(name, "phases/"):
			parts := strings.SplitN(strings.TrimPrefix(name, "phases/"), "/", 2)
			if len(parts) != 2 {
				return invalid
			}
			id = parts[0]
			child = true
		case strings.HasPrefix(name, "intents/"):
			id = strings.TrimSuffix(strings.TrimPrefix(name, "intents/"), ".json")
			if name != "intents/"+id+".json" {
				return invalid
			}
		case strings.HasPrefix(name, "phase-"):
			id = strings.TrimSuffix(strings.TrimPrefix(name, "phase-"), ".json")
			if name != "phase-"+id+".json" {
				return invalid
			}
			checkpoint = true
		default:
			continue
		}
		at, ok := known[id]
		if !ok || at < inherited {
			return invalid
		}
		if child || checkpoint {
			if _, ok = files["intents/"+id+".json"]; !ok {
				return invalid
			}
		}
		if checkpoint {
			if _, ok = files["phases/"+id+"/identity.sha256"]; !ok {
				return invalid
			}
		}
	}
	return nil
}

func flowEmptyDirectory(directory string, files map[string][]byte) bool {
	parts := strings.Split(directory, "/")
	for i, part := range parts {
		if part == "phases" && i+1 < len(parts) {
			prefix := ""
			if i > 0 {
				prefix = strings.Join(parts[:i], "/") + "/"
			}
			_, ok := files[prefix+"intents/"+parts[i+1]+".json"]
			return ok
		}
	}
	return true
}
