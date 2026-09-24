// Package receiver is a deliberately bounded SIU test fixture, not a production
// receiver. One foreground session serializes messages and exports observations.
package receiver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

// Room remains for already-buffered frames at shutdown and an ACK containing a
// bounded control ID and rejection reason. Completed sessions stay within the
// case bundle's independent source, byte, and occurrence limits.
const MaxMessages = 4000
const frameReserve = 16 << 10

// MaxConnections bounds how many peers one capture serves at once. Each one
// becomes its own case source, so the ceiling is well inside the case
// contract's own source limit and every accepted connection can be retained.
const MaxConnections = 64

type Config struct {
	Mode            observation.Mode
	OutputPath      string
	ObservationPath string
	MaxFrameBytes   int
	IdleTimeout     time.Duration
	MaxMessages     int // zero means run until cancellation or a session limit
	// InProcess marks a session whose live observation is read back only by
	// the process serving it, which retains every copy it keeps through its
	// own synced write, as redaction's fixture proofs, report trials and the
	// guided sample's practice runs do. Each snapshot is still installed
	// whole, by rename, before its ACK, but it is not flushed to the device
	// first, so the ACK does not wait on the disk. A session another process
	// reads, such as listen's, always flushes.
	InProcess bool
	// Durability is Durable unless the session's case and first snapshot are
	// Scratch: written inside a throwaway workspace, as a report trial's are,
	// whose owner removes it before answering and keeps no copy except
	// through its own synced write. Neither is then flushed; whether each
	// later snapshot is flushed before its ACK remains InProcess's choice.
	Durability artifactdir.Durability
}

// syncLedger flushes each snapshot of a ledger that another process reads,
// before the rename that makes the snapshot visible. It is the shared durable
// sync, so a test observing artifactdir's syncs sees these too.
var syncLedger = artifactdir.Durable.Sync

type Receiver struct {
	recorder
	config           Config
	profile          receiverProfile
	startedAt        time.Time
	snapshot         observation.Snapshot
	served           bool
	observationLimit int
	flush            func(*os.File) error
}

func New(config Config) (*Receiver, error) {
	if config.Mode != observation.Fixed && config.Mode != observation.Defective {
		return nil, errors.New("receiver mode must be fixed or defective")
	}
	if err := validBounds(config.MaxFrameBytes, config.IdleTimeout, config.MaxMessages); err != nil {
		return nil, err
	}
	if config.OutputPath == "" || config.ObservationPath == "" {
		return nil, errors.New("receiver requires new case and observation destinations")
	}
	output, err := artifactpath.Destination(config.OutputPath)
	if err != nil {
		return nil, err
	}
	observed, err := artifactpath.Destination(config.ObservationPath)
	if err != nil {
		return nil, err
	}
	if output == observed {
		return nil, errors.New("case and observation destinations must differ")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return nil, errors.New("case destination must be new")
	}
	config.OutputPath, config.ObservationPath = output, observed
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, errors.New("cannot initialize receiver session")
	}
	profile, err := decodeProfile(fixtureProfileJSON)
	if err != nil {
		return nil, err
	}
	r := &Receiver{config: config, profile: profile, startedAt: time.Now().UTC(), observationLimit: observation.MaxBytes, flush: syncLedger}
	if config.InProcess {
		r.flush = nil
	}
	r.snapshot = observation.Snapshot{Schema: observation.Schema, Profile: profile.Name, SessionID: hex.EncodeToString(entropy[:]), Mode: config.Mode, Consistent: true, Processed: []observation.Occurrence{}, Records: []observation.Record{}}
	if err := observation.CreateWithDurability(config.ObservationPath, r.snapshot, config.Durability); err != nil {
		return nil, err
	}
	// Case-insensitive filesystems can alias distinct resolved leaf spellings.
	// Confirm exclusivity again before returning a receiver that can send ACKs;
	// remove only the observation we just installed if it occupies both names.
	if outputInfo, err := os.Lstat(output); !os.IsNotExist(err) {
		if observedInfo, observedErr := os.Lstat(observed); err == nil && observedErr == nil && os.SameFile(outputInfo, observedInfo) {
			if err := os.Remove(observed); err != nil {
				return nil, errors.New("cannot remove conflicting startup observation")
			}
		}
		return nil, errors.New("case destination is no longer new; receiver was not started")
	}
	return r, nil
}

// Serve owns and closes listener. Cancellation interrupts blocked accepts,
// reads, and writes, then finalizes recorded evidence. Abrupt process termination
// cannot finalize in-memory wire evidence; the last observation remains usable
// only after its session and occurrence list have been checked by the caller.
func (r *Receiver) Serve(ctx context.Context, listener net.Listener) (*bundle.Bundle, error) {
	if r.served {
		return nil, errors.New("receiver session has already been served")
	}
	r.served = true
	// The fixture is deliberately one connection at a time. Concurrent capture
	// is the generic collector's contract, not this bounded SIU fixture's.
	err := r.sessions(ctx, listener, r.connection, limits{maxFrameBytes: r.config.MaxFrameBytes, maxMessages: r.config.MaxMessages, maxConnections: 1})
	b, writeErr := bundle.WriteRecordedWithDurability(r.config.OutputPath, r.sources(), r.startedAt, r.snapshot, r.config.Durability)
	if writeErr != nil {
		return nil, errors.Join(err, writeErr)
	}
	return b, err
}

func (r *Receiver) connection(ctx context.Context, connection net.Conn, state *stream) (bool, error) {
	bounds := limits{maxFrameBytes: r.config.MaxFrameBytes, maxMessages: r.config.MaxMessages, maxConnections: 1}
	reader, _ := mllp.NewReader(deadlineReader{connection, &r.recorder, r.config.IdleTimeout}, r.config.MaxFrameBytes)
	var refused []byte
	// A coalesced TCP read may include frames beyond --max-messages, and a
	// refusal drains the frame its peer had already begun. Both are retained as
	// received evidence, but never claimed as processed.
	defer func() { r.close(state, append(reader.Buffered(), refused...)) }()
	for {
		admitted, err := r.admitFrame(state, bounds)
		if err != nil {
			return false, err
		}
		if !admitted {
			// Either this session has read every frame it was asked for, or
			// the ones still to come are promised to peers already waiting.
			// Only the first of those finishes the session.
			return r.complete(ctx, r.config.MaxMessages), nil
		}
		if !r.awaiting(state, connection) {
			return true, nil
		}
		// The slot in a declared message limit is claimed only once this peer
		// has actually begun the next frame, as it is for the collector. This
		// fixture serves one connection at a time, so no peer can be starved by
		// a slot held while waiting; both share one recorder, and a reservation
		// means the same thing in each.
		if err := reader.Await(); err != nil {
			r.engaged(state)
			return false, nil
		}
		if !r.reserveFrame(bounds) {
			// With one connection at a time only a controlled stop can refuse a
			// claim here, and it is handled the same way it is for the
			// collector: closing over a frame still in the receive queue would
			// reset the connection instead of ending it, so the refused frame
			// is drained into the evidence rather than left in the socket.
			buffered := reader.Buffered()
			refused = append(buffered, r.drainRefused(connection, bounds.maxFrameBytes+frameReserve-len(buffered))...)
			r.engaged(state)
			return r.complete(ctx, r.config.MaxMessages), nil
		}
		raw, readErr := reader.ReadFrame()
		r.engaged(state)
		if readErr != nil {
			r.releaseFrame()
			raw = append(raw, reader.Buffered()...)
			if len(raw) > 0 {
				r.retain(state, raw, bundle.Inbound)
			}
			// A malformed, oversized, idle, or disconnected peer cannot mutate
			// the ledger. Its consumed prefix survives in the final bundle.
			return false, nil
		}
		id := r.retain(state, raw, bundle.Inbound)
		ordinal := r.nextFrame()
		r.snapshot.Consistent = false
		if err := observation.Install(r.config.ObservationPath, r.snapshot, r.flush); err != nil {
			return false, err
		}
		request, processErr := r.parseRequest(raw)
		candidate := r.snapshot
		candidate.Records = slices.Clone(r.snapshot.Records)
		candidate.Processed = slices.Clone(r.snapshot.Processed)
		if request != nil {
			if processErr == nil {
				processErr = r.apply(&candidate, request)
			}
			candidate.Processed = append(candidate.Processed, observation.Occurrence{OccurrenceID: id, ControlID: request.controlID})
		}
		// Preflight the candidate while consistent is false, whose JSON is one
		// byte larger than true. The last committed snapshot must remain valid
		// both while busy and at finalization, even on a resource-limit exit.
		encoded, encodeErr := observation.Encode(candidate)
		if encodeErr != nil || len(encoded) > r.observationLimit {
			r.snapshot.Consistent = true
			if restoreErr := observation.Install(r.config.ObservationPath, r.snapshot, r.flush); restoreErr != nil {
				r.snapshot.Consistent = false
				return false, errors.Join(encodeErr, restoreErr)
			}
			return false, errors.New("receiver session reached observation limit; prior ledger retained")
		}
		candidate.Consistent = true
		if err := observation.Install(r.config.ObservationPath, candidate, r.flush); err != nil {
			return false, err
		}
		r.snapshot = candidate
		if request != nil {
			code, reason := "AA", ""
			if processErr != nil {
				code, reason = "AR", processErr.Error()
			}
			ack := acknowledgement(request, code, reason, ordinal, r.snapshot.SessionID, id)
			if err := connection.SetWriteDeadline(time.Now().Add(r.config.IdleTimeout)); err != nil {
				return false, errors.New("cannot set receiver write deadline")
			}
			sent, err := writeAll(connection, ack)
			if sent > 0 {
				r.retain(state, ack[:sent], bundle.Outbound)
			}
			if err != nil {
				return r.complete(ctx, r.config.MaxMessages), nil
			}
		}
		if r.complete(ctx, r.config.MaxMessages) {
			return true, nil
		}
		if request == nil {
			return false, nil
		}
	}
}

func writeAll(writer io.Writer, data []byte) (int, error) {
	total := 0
	for total < len(data) {
		n, err := writer.Write(data[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
