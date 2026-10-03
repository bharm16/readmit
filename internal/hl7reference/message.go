package hl7reference

import (
	"errors"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileeval"
)

type MessageContext struct {
	Status                 string                `json:"status"`
	Reason                 string                `json:"reason"`
	MessageCode            string                `json:"message_code"`
	Event                  string                `json:"event"`
	DeclaredStructure      string                `json:"declared_structure"`
	DeclaredStructureState hl7.State             `json:"declared_structure_state"`
	ResolvedStructure      string                `json:"resolved_structure"`
	MessageKey             string                `json:"message_key"`
	StructureKey           string                `json:"structure_key"`
	Placement              profileeval.Placement `json:"placement"`
}

func (c *Catalog) MessageContext(version, code, event, declared string, state hl7.State) MessageContext {
	context := MessageContext{Status: "unknown", MessageCode: code, Event: event, DeclaredStructure: declared, DeclaredStructureState: state, Placement: profileeval.Placement{State: "unknown", Reason: "No matched reference structure.", Groups: []profileeval.PlacementGroup{}}}
	if version != c.document.Edition {
		context.Status = "unsupported_edition"
		context.Reason = "The selected catalog does not describe the message edition."
		return context
	}
	if code == "" || event == "" {
		context.Reason = "Message code or trigger declaration is missing; no structure is guessed."
		return context
	}
	message, ok := c.records["message/"+code+"/"+event]
	if !ok {
		context.Reason = "The catalog has no sourced mapping for this message code and event."
		return context
	}
	context.MessageKey = message.Key
	if len(message.Structures) != 1 {
		context.Status = "ambiguous"
		context.Reason = "The edition does not resolve this event to one unique structure."
		return context
	}
	expected := message.Structures[0]
	if state == hl7.Present && declared != "" && declared != expected {
		context.Status = "contradictory"
		context.Reason = "The transmitted structure contradicts the edition's message/event mapping."
		return context
	}
	if state == hl7.Present && declared != "" {
		context.Status = "declared"
	} else {
		context.Status = "inferred"
		context.Reason = "Resolved from the edition's event mapping; the transmitted MSH-9.3 state is unchanged."
	}
	if _, ok := c.records["structure/"+expected]; !ok {
		context.Status = "unknown"
		context.Reason = "The mapped structure is unavailable in the selected catalog."
		return context
	}
	context.ResolvedStructure = expected
	context.StructureKey = "structure/" + expected
	return context
}

func (c *Catalog) Sequence(key string) []profileeval.Node {
	record, ok := c.records[key]
	if !ok || record.Kind != "structure" {
		return []profileeval.Node{}
	}
	return cloneSequence(record.Sequence)
}
func cloneSequence(nodes []profileeval.Node) []profileeval.Node {
	out := slices.Clone(nodes)
	for i := range out {
		out[i].Children = cloneSequence(out[i].Children)
	}
	return out
}
func validateSequence(nodes []profileeval.Node, depth int, count *int) error {
	if depth > 16 {
		return errors.New("reference structure exceeds depth bound")
	}
	for _, n := range nodes {
		*count++
		if *count > 4000 || n.Name == "" || len(n.Name) > 128 || n.Min < 0 || n.Min > 9999 {
			return errors.New("invalid bounded reference structure node")
		}
		if n.Max != "*" {
			high, err := strconv.Atoi(n.Max)
			if err != nil || high < n.Min || high > 9999 {
				return errors.New("invalid reference cardinality")
			}
		}
		if n.Segment != "" && !code.MatchString(n.Segment) {
			return errors.New("invalid reference segment node")
		}
		if n.Segment != "" && len(n.Children) > 0 || n.Segment == "" && len(n.Children) == 0 {
			return errors.New("invalid reference group contents")
		}
		if err := validateSequence(n.Children, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
