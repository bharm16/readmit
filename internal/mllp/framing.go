// Package mllp reads bounded MLLP frames without depending on TCP read boundaries.
package mllp

import (
	"bufio"
	"errors"
	"io"
)

var (
	ErrFraming       = errors.New("invalid MLLP framing")
	ErrFrameTooLarge = errors.New("MLLP frame exceeds maximum payload size")
)

// Reader preserves the complete frame, including its delimiters. On failure,
// ReadFrame returns the consumed prefix so a caller can retain partial evidence.
// A framing error is terminal; the caller must not attempt resynchronization.
type Reader struct {
	reader *bufio.Reader
	max    int
}

func NewReader(reader io.Reader, maxPayloadBytes int) (*Reader, error) {
	if maxPayloadBytes < 1 {
		return nil, errors.New("maximum MLLP payload size must be positive")
	}
	return &Reader{reader: bufio.NewReader(reader), max: maxPayloadBytes}, nil
}

func (r *Reader) ReadFrame() ([]byte, error) {
	start, err := r.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	raw := []byte{start}
	if start != 0x0b {
		return raw, ErrFraming
	}
	for {
		value, err := r.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return raw, err
		}
		raw = append(raw, value)
		if value == 0x1c {
			last, err := r.reader.ReadByte()
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = io.ErrUnexpectedEOF
				}
				return raw, err
			}
			raw = append(raw, last)
			if last != '\r' {
				return raw, ErrFraming
			}
			return raw, nil
		}
		if value == 0x0b {
			return raw, ErrFraming
		}
		if len(raw)-1 > r.max {
			return raw, ErrFrameTooLarge
		}
	}
}

// Buffered returns already-read bytes following the last returned frame. It is
// used only at connection shutdown, to preserve evidence without another read.
func (r *Reader) Buffered() []byte {
	data := make([]byte, r.reader.Buffered())
	_, _ = io.ReadFull(r.reader, data)
	return data
}

func Frame(payload []byte) []byte {
	frame := make([]byte, 0, len(payload)+3)
	frame = append(frame, 0x0b)
	frame = append(frame, payload...)
	return append(frame, 0x1c, '\r')
}
