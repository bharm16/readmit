// Package receiver is a deliberately bounded SIU test fixture, not a production
// receiver. One foreground session serializes messages and exports observations.
package receiver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

// Room remains for already-buffered frames at shutdown and an ACK containing a
// bounded control ID and rejection reason. Completed sessions stay within the
// case bundle's independent source, byte, and occurrence limits.
const MaxMessages = 4000
const frameReserve = 16 << 10

type Config struct {
	Mode            observation.Mode
	OutputPath      string
	ObservationPath string
	MaxFrameBytes   int
	IdleTimeout     time.Duration
	MaxMessages     int // zero means run until cancellation or a session limit
}

type Receiver struct {
	config                       Config
	profile                      receiverProfile
	startedAt                    time.Time
	snapshot                     observation.Snapshot
	inputs                       []bundle.Input
	totalBytes, received, events int
	chunks                       []evidenceChunk
	served                       bool
}

func New(config Config) (*Receiver, error) {
	if config.Mode != observation.Fixed && config.Mode != observation.Defective {
		return nil, errors.New("receiver mode must be fixed or defective")
	}
	if config.MaxFrameBytes < 1 || config.MaxFrameBytes > bundle.MaxSourceBytes-frameReserve || config.IdleTimeout <= 0 || config.MaxMessages < 0 || config.MaxMessages > MaxMessages {
		return nil, errors.New("invalid receiver frame, timeout, or message limit")
	}
	if config.OutputPath == "" || config.ObservationPath == "" {
		return nil, errors.New("receiver requires new case and observation destinations")
	}
	output, err := filepath.Abs(config.OutputPath)
	if err != nil {
		return nil, errors.New("cannot resolve case destination")
	}
	observed, err := filepath.Abs(config.ObservationPath)
	if err != nil {
		return nil, errors.New("cannot resolve observation destination")
	}
	if output == observed {
		return nil, errors.New("case and observation destinations must differ")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return nil, errors.New("case destination must be new")
	}
	info, err := os.Stat(filepath.Dir(output))
	if err != nil || !info.IsDir() {
		return nil, errors.New("case parent must be an existing directory")
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, errors.New("cannot initialize receiver session")
	}
	profile, err := decodeProfile(fixtureProfileJSON)
	if err != nil {
		return nil, err
	}
	r := &Receiver{config: config, profile: profile, startedAt: time.Now().UTC()}
	r.snapshot = observation.Snapshot{Schema: observation.Schema, Profile: profile.Name, SessionID: hex.EncodeToString(entropy[:]), Mode: config.Mode, Consistent: true, Processed: []observation.Occurrence{}, Records: []observation.Record{}}
	if err := observation.Create(config.ObservationPath, r.snapshot); err != nil {
		return nil, err
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
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	err := r.serve(ctx, listener)
	b, writeErr := bundle.WriteRecorded(r.config.OutputPath, r.inputs, r.startedAt, r.snapshot)
	if writeErr != nil {
		return nil, errors.Join(err, writeErr)
	}
	return b, err
}

func (r *Receiver) serve(ctx context.Context, listener net.Listener) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("receiver cannot accept connection")
		}
		if len(r.inputs) >= bundle.MaxSources {
			connection.Close()
			return errors.New("receiver session reached source limit")
		}
		stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
		done, err := r.connection(ctx, connection)
		stop()
		connection.Close()
		if err != nil {
			return err
		}
		if done || ctx.Err() != nil {
			return nil
		}
	}
}

func (r *Receiver) connection(ctx context.Context, connection net.Conn) (bool, error) {
	input := bundle.Input{Options: hl7.Options{Format: hl7.MLLP}, Observations: make(map[int]bundle.Observation)}
	r.chunks = nil
	reader, _ := mllp.NewReader(deadlineReader{connection, r.config.IdleTimeout}, r.config.MaxFrameBytes)
	defer func() {
		// A coalesced TCP read may include frames beyond --max-messages. They
		// are retained as received evidence, but never claimed as processed.
		r.retainBuffered(&input, reader.Buffered())
		if len(input.Data) > 0 {
			r.frameObservations(&input)
			r.events += len(input.Observations)
			r.inputs = append(r.inputs, input)
		}
	}()
	for {
		// bufio reads at most 4096 bytes ahead: fewer than 1400 minimum-size
		// frames. Reserve those slots even if a peer sends only bad frames.
		if r.events+len(input.Observations) > bundle.MaxEvents-1400 {
			return false, errors.New("receiver session reached occurrence limit")
		}
		if len(input.Data)+r.config.MaxFrameBytes+frameReserve > bundle.MaxSourceBytes || r.totalBytes+r.config.MaxFrameBytes+frameReserve > bundle.MaxEvidenceBytes {
			return false, errors.New("receiver session reached evidence byte limit")
		}
		if r.received >= MaxMessages {
			return false, errors.New("receiver session reached message limit")
		}
		raw, readErr := reader.ReadFrame()
		if readErr != nil {
			raw = append(raw, reader.Buffered()...)
			if len(raw) > 0 {
				r.retain(&input, raw, bundle.Inbound)
			}
			// A malformed, oversized, idle, or disconnected peer cannot mutate
			// the ledger. Its consumed prefix survives in the final bundle.
			return false, nil
		}
		id := r.retain(&input, raw, bundle.Inbound)
		r.received++
		r.snapshot.Consistent = false
		if err := observation.Write(r.config.ObservationPath, r.snapshot); err != nil {
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
		if _, err := observation.Encode(candidate); err != nil {
			r.snapshot.Consistent = true
			if restoreErr := observation.Write(r.config.ObservationPath, r.snapshot); restoreErr != nil {
				r.snapshot.Consistent = false
				return false, errors.Join(err, restoreErr)
			}
			return false, errors.New("receiver session reached observation limit; prior ledger retained")
		}
		candidate.Consistent = true
		if err := observation.Write(r.config.ObservationPath, candidate); err != nil {
			return false, err
		}
		r.snapshot = candidate
		if request != nil {
			code, reason := "AA", ""
			if processErr != nil {
				code, reason = "AR", processErr.Error()
			}
			ack := acknowledgement(request, code, reason, r.received)
			if err := connection.SetWriteDeadline(time.Now().Add(r.config.IdleTimeout)); err != nil {
				return false, errors.New("cannot set receiver write deadline")
			}
			sent, err := writeAll(connection, ack)
			if sent > 0 {
				r.retain(&input, ack[:sent], bundle.Outbound)
			}
			if err != nil {
				return r.config.MaxMessages > 0 && r.received >= r.config.MaxMessages || ctx.Err() != nil, nil
			}
		}
		if r.config.MaxMessages > 0 && r.received >= r.config.MaxMessages {
			return true, nil
		}
		if ctx.Err() != nil {
			return true, nil
		}
		if request == nil {
			return false, nil
		}
	}
}

func (r *Receiver) retain(input *bundle.Input, raw []byte, direction bundle.Direction) string {
	sequence := len(input.Observations) + 1
	now := time.Now().UTC()
	input.Data = append(input.Data, raw...)
	input.Observations[sequence] = bundle.Observation{Direction: direction, ObservedAt: &now}
	r.chunks = append(r.chunks, evidenceChunk{start: len(input.Data) - len(raw), end: len(input.Data), observation: input.Observations[sequence]})
	r.totalBytes += len(raw)
	return fmt.Sprintf("s%04d-e%06d", len(r.inputs)+1, sequence)
}

func (r *Receiver) retainBuffered(input *bundle.Input, data []byte) {
	reader, _ := mllp.NewReader(bytes.NewReader(data), len(data)+1)
	for {
		raw, err := reader.ReadFrame()
		if err != nil {
			raw = append(raw, reader.Buffered()...)
		}
		if len(raw) > 0 {
			r.retain(input, raw, bundle.Inbound)
		}
		if err != nil {
			return
		}
	}
}

type deadlineReader struct {
	net.Conn
	idle time.Duration
}

func (r deadlineReader) Read(data []byte) (int, error) {
	if err := r.SetReadDeadline(time.Now().Add(r.idle)); err != nil {
		return 0, err
	}
	return r.Conn.Read(data)
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
