package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Execute is the only replay operation that accesses the network. A new run is
// reserved before dialing; transport failures are finalized evidence in Events,
// not returned errors. Errors indicate invalid input or failure to store evidence.
// There is one dial operation, one outstanding message, and no reconnect/retry.
func Execute(ctx context.Context, plan *Plan, output string) (*Run, error) {
	return ExecuteWithPolicy(ctx, plan, output, nil, nil)
}

// ExecuteWithPolicy checks the actual destination and records its decision before
// opening a connection. A selected policy requires a recorder; a failed record
// prevents sending. Execute uses the same boundary with loopback-only defaults.
func ExecuteWithPolicy(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error) (*Run, error) {
	return ExecuteObserved(ctx, plan, output, policy, record, nil)
}

// Observer is a synchronous durability boundary. Errors halt execution, retaining
// incomplete evidence. BeforeSend runs before any payload write; Sent runs after
// Write and before waiting for an ACK; Recorded runs after the event is synced.
// Implementations must not call back into execution or mutate supplied values.
type Observer interface {
	BeforeSend(occurrence string) error
	Sent(occurrence string, raw []byte) error
	Recorded(event Event) error
}

func ExecuteObserved(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error, observer Observer) (*Run, error) {
	return executeObservedPolicy(ctx, plan, output, policy, record, nil, observer)
}

func executeWithPolicy(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error, resolve sendpolicy.Resolver) (*Run, error) {
	return executeObservedPolicy(ctx, plan, output, policy, record, resolve, nil)
}

func executeObservedPolicy(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error, resolve sendpolicy.Resolver, observer Observer) (*Run, error) {
	if plan == nil || len(plan.messages) == 0 {
		return nil, errors.New("replay requires a prepared plan")
	}
	if policy != nil && record == nil {
		return nil, errors.New("a selected send policy requires a decision recorder")
	}
	duration, _ := time.ParseDuration(plan.target.ConnectTimeout)
	decision, err := destination.Decide(ctx, destination.Request{
		Purpose: destination.Send, Address: plan.target.Address, Classification: string(plan.target.Environment().Classification),
		Policy: policy, Budget: duration, Record: record, Resolve: resolve,
	})
	if err != nil {
		return nil, err
	}
	route, admitted := decision.Route()
	if !admitted {
		return nil, errors.New("the send was refused by policy before anything was sent: " + string(decision.Reason))
	}
	// File persistence is not network connection time. DNS consumes the
	// connection budget; recording and reserving evidence do not.
	return executeObserved(ctx, plan, output, observer, func(ctx context.Context, p *Plan) (net.Conn, *TransportError) {
		return connect(ctx, p, route)
	})
}

func execute(ctx context.Context, plan *Plan, output string, dial func(context.Context, *Plan) (net.Conn, *TransportError)) (*Run, error) {
	return executeObserved(ctx, plan, output, nil, dial)
}

func executeObserved(ctx context.Context, plan *Plan, output string, observer Observer, dial func(context.Context, *Plan) (net.Conn, *TransportError)) (*Run, error) {
	if plan == nil || len(plan.messages) == 0 {
		return nil, errors.New("replay requires a prepared plan")
	}
	run, writer, err := begin(plan, output)
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	var connection net.Conn
	var reader *mllp.Reader
	var stop func() bool
	defer func() {
		if stop != nil {
			stop()
		}
		if connection != nil {
			_ = connection.Close()
		}
	}()
	halted := false
	for i, message := range plan.messages {
		event := &run.Events[i]
		var sent, received []byte
		if !halted {
			started := time.Now()
			if ctx.Err() != nil {
				phase := "dial"
				if connection != nil {
					phase = "write"
				}
				setFailure(event, phase, context.Canceled, ctx)
			} else {
				if connection == nil {
					connection, event.TransportError = dial(ctx, plan)
					if event.TransportError != nil {
						event.Outcome = outcomeFor(event.TransportError.Class)
					} else {
						stop = context.AfterFunc(ctx, func() { _ = connection.Close() })
						reader, _ = mllp.NewReader(connection, plan.target.MaxACKBytes)
					}
				}
				if event.TransportError == nil {
					if observer != nil {
						if err := observer.BeforeSend(event.OutboundOccurrence); err != nil {
							return nil, err
						}
						// A cancellation during persistence stops the pending send.
						if ctx.Err() != nil {
							return nil, ctx.Err()
						}
					}
					duration, _ := time.ParseDuration(plan.target.MessageTimeout)
					// The message timeout bounds one message and its
					// acknowledgement on the network, so the same window is
					// armed for the write and again for the read, never past
					// the run's own deadline.
					armWindow := func(at time.Time) error {
						if limit, ok := ctx.Deadline(); ok && limit.Before(at) {
							at = limit
						}
						return connection.SetDeadline(at)
					}
					deadline := time.Now().Add(duration)
					if err := armWindow(deadline); err != nil {
						setFailure(event, "write", err, ctx)
					} else {
						n, writeErr := writeAll(connection, message.wire)
						sent = bytes.Clone(message.wire[:n])
						var windowErr error
						if observer != nil {
							persisting := time.Now()
							if err := observer.Sent(event.OutboundOccurrence, bytes.Clone(sent)); err != nil {
								return nil, err
							}
							// Persisting what was sent is local durability, not
							// network time. Move the window on by exactly what
							// it took, so a healthy receiver is never reported
							// as an uncertain delivery because of this sender's
							// own storage. A failed write has no acknowledgement
							// to wait for.
							if writeErr == nil {
								windowErr = armWindow(deadline.Add(time.Since(persisting)))
							}
						}
						if n > 0 {
							event.Delivery = "uncertain"
						}
						switch {
						case writeErr != nil:
							setFailure(event, "write", writeErr, ctx)
						case windowErr != nil:
							setFailure(event, "read", windowErr, ctx)
						default:
							frame, readErr := reader.ReadFrame()
							extra := reader.Buffered()
							received = append(frame, extra...)
							if readErr != nil {
								setFailure(event, "read", readErr, ctx)
								if errors.Is(readErr, mllp.ErrFraming) || errors.Is(readErr, mllp.ErrFrameTooLarge) {
									event.TransportError.Class = "invalid_ack"
									event.Outcome = ProtocolError
								}
							} else {
								event.ACK = correlate(frame, message.controlID)
								if len(extra) > 0 || event.ACK.Correlation != "matched" {
									event.Outcome = ProtocolError
									event.TransportError = &TransportError{Phase: "ack", Class: "invalid_ack"}
								} else {
									event.Delivery = "acknowledged"
									event.Outcome = ackOutcome(event.ACK.Code)
								}
							}
						}
					}
				}
			}
			event.ElapsedNS = max(1, time.Since(started).Nanoseconds())
			halted = event.TransportError != nil
		}
		if err := writer.record(run, i, sent, received); err != nil {
			return nil, err
		}
		if observer != nil {
			copyEvent := *event
			copyEvent.ControlID = bytes.Clone(event.ControlID)
			copyEvent.ACK.ControlID = bytes.Clone(event.ACK.ControlID)
			if event.TransportError != nil {
				copied := *event.TransportError
				copyEvent.TransportError = &copied
			}
			if err := observer.Recorded(copyEvent); err != nil {
				return nil, err
			}
		}
	}
	if connection != nil {
		_ = connection.Close()
	}
	if stop != nil {
		stop()
	}
	if err := writer.finish(run); err != nil {
		return nil, err
	}
	return run, nil
}

// connect opens the one connection a send makes, to the address its decision
// checked. One package owns the minimum version, the always-on verification
// and the explicit authority rule, so a send honours exactly what a diagnosis
// and a capture of the same configuration honour.
func connect(ctx context.Context, plan *Plan, route destination.Route) (net.Conn, *TransportError) {
	var security *destination.Security
	if plan.target.Transport != "plain" {
		security = &destination.Security{ServerName: plan.target.ServerName, Authorities: plan.ca}
	}
	connection, err := route.Open(ctx, security)
	if err == nil {
		return connection, nil
	}
	var failure *destination.Failure
	if !errors.As(err, &failure) {
		return nil, &TransportError{Phase: "tls", Class: "tls_verification"}
	}
	class := transportClass(failure.Kind)
	if failure.Phase == destination.Handshake && class != "timeout" && class != "cancelled" {
		class = "tls_handshake"
		if failure.Kind.Verification() {
			class = "tls_verification"
		}
	}
	return nil, &TransportError{Phase: string(failure.Phase), Class: class}
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

func setFailure(event *Event, phase string, err error, ctx context.Context) {
	class := transportClass(destination.Classify(ctx, err))
	event.TransportError = &TransportError{Phase: phase, Class: class}
	event.Outcome = outcomeFor(class)
}

// transportClass is the frozen readmit-run/v1 class of one transport failure.
func transportClass(kind destination.Kind) string {
	switch kind {
	case destination.Timeout:
		return "timeout"
	case destination.Cancelled:
		return "cancelled"
	case destination.ConnectionRefused:
		return "connection_refused"
	case destination.Disconnected, destination.ConnectionReset:
		return "disconnect"
	}
	return "network"
}

func outcomeFor(class string) Outcome {
	switch class {
	case "timeout":
		return Timeout
	case "cancelled":
		return Cancelled
	case "connection_refused":
		return ConnectionRefused
	case "disconnect":
		return Disconnect
	case "tls_verification", "tls_handshake":
		return TLSError
	case "invalid_ack":
		return ProtocolError
	default:
		return NetworkError
	}
}

func ackOutcome(code string) Outcome {
	switch code {
	case "AA":
		return Accepted
	case "AE":
		return ApplicationError
	default:
		return Rejected
	}
}

func correlate(raw, control []byte) ACK {
	ack := ACK{Correlation: "invalid", ControlID: []byte{}}
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(doc.Messages) != 1 {
		return ack
	}
	header := doc.Messages[0].Segments[0]
	for _, n := range []int{15, 16} {
		state := header.Field(n).State
		if state != hl7.Empty && state != hl7.Omitted {
			return ack
		}
	}
	kind, err := selectedBytes(doc, "MSH-9.1", true)
	if err != nil || string(kind) != "ACK" {
		return ack
	}
	count := 0
	for _, segment := range doc.Messages[0].Segments {
		if segment.ID == "MSA" {
			count++
			if len(segment.Field(1).Repetitions) != 1 || len(segment.Field(2).Repetitions) != 1 {
				return ack
			}
		}
	}
	if count != 1 {
		return ack
	}
	code, err := selectedBytes(doc, "MSA-1", true)
	if err != nil || string(code) != "AA" && string(code) != "AE" && string(code) != "AR" {
		return ack
	}
	id, err := selectedBytes(doc, "MSA-2", true)
	if err != nil {
		return ack
	}
	ack.Code, ack.ControlID = string(code), id
	ack.Correlation = "mismatched"
	if bytes.Equal(id, control) {
		ack.Correlation = "matched"
	}
	return ack
}
