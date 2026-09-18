package replay

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
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
	return executeObservedPolicy(ctx, plan, output, policy, record, sendpolicy.SystemResolver, observer)
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
	deadline := time.Now().Add(duration)
	decisionCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	decision := sendpolicy.Decide(decisionCtx, policy, sendpolicy.Request{
		Address: plan.target.Address, Classification: string(plan.target.Environment().Classification), Explicit: true,
	}, resolve)
	remaining := max(time.Duration(0), time.Until(deadline))
	cancel()
	// Retain before using the decision, and do not expose a mutable slice shared
	// with the recorder as the authorization used by the connector.
	address := ""
	if decision.Allowed && len(decision.ResolvedAddresses) == 1 {
		_, port, _ := net.SplitHostPort(plan.target.Address)
		address = net.JoinHostPort(decision.ResolvedAddresses[0], port)
	}
	if record != nil {
		if err := record(decision); err != nil {
			return nil, err
		}
	}
	if !decision.Allowed || address == "" {
		return nil, errors.New("the send was refused by policy before anything was sent: " + string(decision.Reason))
	}
	return executeObserved(ctx, plan, output, observer, func(ctx context.Context, p *Plan) (net.Conn, *TransportError) {
		// File persistence is not network connection time. DNS consumes the
		// connection budget; recording and reserving evidence do not.
		dialCtx, stop := context.WithTimeout(ctx, remaining)
		defer stop()
		return connect(dialCtx, p, address)
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
					deadline := time.Now().Add(duration)
					if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
						deadline = limit
					}
					if err := connection.SetDeadline(deadline); err != nil {
						setFailure(event, "write", err, ctx)
					} else {
						n, writeErr := writeAll(connection, message.wire)
						sent = bytes.Clone(message.wire[:n])
						if observer != nil {
							if err := observer.Sent(event.OutboundOccurrence, bytes.Clone(sent)); err != nil {
								return nil, err
							}
						}
						if n > 0 {
							event.Delivery = "uncertain"
						}
						if writeErr != nil {
							setFailure(event, "write", writeErr, ctx)
						} else {
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

func connect(ctx context.Context, plan *Plan, address string) (net.Conn, *TransportError) {
	duration, _ := time.ParseDuration(plan.target.ConnectTimeout)
	dialCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
	if err != nil {
		return nil, &TransportError{Phase: "dial", Class: errorClass(err, dialCtx)}
	}
	if plan.target.Transport == "plain" {
		return connection, nil
	}
	// One package owns the minimum version, the always-on verification and the
	// explicit authority rule, so a send honours exactly what a diagnosis and a
	// capture of the same configuration honour.
	config, err := transportsecurity.ClientConfig(VerifiedServerName(plan.target), plan.ca)
	if err != nil {
		_ = connection.Close()
		return nil, &TransportError{Phase: "tls", Class: "tls_verification"}
	}
	secured := tls.Client(connection, config)
	if err := secured.HandshakeContext(dialCtx); err != nil {
		_ = secured.Close()
		class := errorClass(err, dialCtx)
		if class != "timeout" && class != "cancelled" {
			class = "tls_handshake"
			var verification *tls.CertificateVerificationError
			if errors.As(err, &verification) {
				class = "tls_verification"
			}
		}
		return nil, &TransportError{Phase: "tls", Class: class}
	}
	return secured, nil
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
	class := errorClass(err, ctx)
	event.TransportError = &TransportError{Phase: phase, Class: class}
	event.Outcome = outcomeFor(class)
}

func errorClass(err error, ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if connectionRefused(err) {
		return "connection_refused"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrShortWrite) || connectionDisconnected(err) || errors.Is(err, net.ErrClosed) {
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
