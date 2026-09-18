package receiver

import (
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

type evidenceChunk struct {
	start, end  int
	observation bundle.Observation
}

// Reconcile transport chunks with the bundle's exact MLLP boundary policy. A
// truncated outbound ACK followed by buffered inbound bytes is one unparsed
// suffix, with unknown direction rather than a fabricated complete ACK.
func (r *Receiver) frameObservations(input *bundle.Input) {
	observations := make(map[int]bundle.Observation)
	for start, sequence := 0, 1; start < len(input.Data); sequence++ {
		end, _ := bundle.NextOccurrence(input.Data, start, hl7.MLLP)
		var observed bundle.Observation
		for _, chunk := range r.chunks {
			if chunk.end <= start || chunk.start >= end {
				continue
			}
			if observed.ObservedAt == nil {
				observed = chunk.observation
			} else if observed.Direction != chunk.observation.Direction {
				observed.Direction = bundle.Unknown
			}
		}
		observations[sequence] = observed
		start = end
	}
	input.Observations = observations
}
