package reportshare

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/bharm16/readmit/internal/bundle"
	"slices"
)

// SelectedMessagesSchema identifies an original-byte selection receipt. A
// receipt records scope, order and source identity, never replay equivalence.
const SelectedMessagesSchema = "readmit-selected-messages/v1"
const MaxSelectedMessages = 1024
const MaxSelectedMessageBytes = 8 << 20

// SelectedOccurrence locates one exact retained payload in the output.
type SelectedOccurrence struct {
	Occurrence string `json:"occurrence"`
	Offset     int    `json:"offset"`
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256"`
	Kind       string `json:"kind"`
}

// SelectedOriginal is one bounded original-byte output in explicit order.
// Data contains no added separator, framing, normalization or transformation.
type SelectedOriginal struct {
	SourceIdentity string
	Occurrences    []SelectedOccurrence
	Data           []byte
}

func SelectOriginal(source *bundle.Bundle, ids []string) (*SelectedOriginal, error) {
	if source == nil || len(ids) == 0 || len(ids) > MaxSelectedMessages {
		return nil, errors.New("choose between 1 and 1024 retained occurrences; an empty selection exports nothing")
	}
	out := &SelectedOriginal{SourceIdentity: source.Identity, Occurrences: []SelectedOccurrence{}}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("an occurrence may be selected only once")
		}
		seen[id] = true
		index := slices.IndexFunc(source.Events, func(e bundle.Event) bool { return e.ID == id })
		if index < 0 {
			return nil, errors.New("a selected occurrence is not in the retained source")
		}
		raw, err := source.Raw(id)
		if err != nil {
			return nil, errors.New("a selected occurrence cannot be read")
		}
		if len(out.Data)+len(raw) > MaxSelectedMessageBytes {
			return nil, errors.New("the selected original payloads exceed the 8 MiB export limit; select a smaller scope")
		}
		sum := sha256.Sum256(raw)
		out.Occurrences = append(out.Occurrences, SelectedOccurrence{Occurrence: id, Offset: len(out.Data), Bytes: len(raw), SHA256: hex.EncodeToString(sum[:]), Kind: string(source.Events[index].Kind)})
		out.Data = append(out.Data, raw...)
	}
	out.Data = bytes.Clone(out.Data)
	return out, nil
}
