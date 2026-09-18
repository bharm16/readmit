package receiver

import (
	"bytes"

	"github.com/bharm16/readmit/internal/bundle"
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
		end := len(input.Data)
		if input.Data[start] == 0x0b {
			length := bytes.IndexByte(input.Data[start+1:], 0x1c)
			candidate := start + 1 + length
			if length >= 0 && candidate+1 < len(input.Data) && input.Data[candidate+1] == '\r' {
				end = candidate + 2
			}
		}
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
