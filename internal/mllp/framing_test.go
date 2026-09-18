package mllp_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/bharm16/readmit/internal/mllp"
)

type fragmented struct {
	data  []byte
	chunk int
}

func (r *fragmented) Read(data []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(data, r.data[:min(r.chunk, len(r.data))])
	r.data = r.data[n:]
	return n, nil
}

func TestFramingIgnoresFragmentAndCoalescingBoundaries(t *testing.T) {
	wire := []byte("\x0bfirst\x1c\r\x0bsecond\x1c\r")
	for _, size := range []int{1, 2, 3, 7, len(wire)} {
		reader, err := mllp.NewReader(&fragmented{data: bytes.Clone(wire), chunk: size}, 6)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"\x0bfirst\x1c\r", "\x0bsecond\x1c\r"} {
			frame, err := reader.ReadFrame()
			if err != nil || string(frame) != want {
				t.Fatalf("chunk=%d got=%q err=%v", size, frame, err)
			}
		}
		if _, err := reader.ReadFrame(); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
}

func TestFrameLimitsAndMalformedPrefixes(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		max    int
		want   error
		prefix string
	}{
		{"\x0b1234\x1c\r", 4, nil, "\x0b1234\x1c\r"},
		{"\x0b12345\x1c\r", 4, mllp.ErrFrameTooLarge, "\x0b12345"},
		{"missing-start", 16, mllp.ErrFraming, "m"},
		{"\x0bpartial", 16, io.ErrUnexpectedEOF, "\x0bpartial"},
		{"\x0ba\x1cX", 16, mllp.ErrFraming, "\x0ba\x1cX"},
		{"\x0ba\x0bb\x1c\r", 16, mllp.ErrFraming, "\x0ba\x0b"},
	} {
		reader, _ := mllp.NewReader(bytes.NewBufferString(tc.raw), tc.max)
		frame, err := reader.ReadFrame()
		if !errors.Is(err, tc.want) || string(frame) != tc.prefix {
			t.Fatalf("got %q %v, want %q %v", frame, err, tc.prefix, tc.want)
		}
	}
	if _, err := mllp.NewReader(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("accepted zero bound")
	}
}

func FuzzFraming(f *testing.F) {
	f.Add([]byte("\x0bMSH|^~\\&|||||||SIU^S12|C|P|2.5.1\r\x1c\r"))
	f.Add([]byte("\x0bpartial"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		reader, _ := mllp.NewReader(bytes.NewReader(data), 1024)
		var recovered []byte
		for {
			frame, err := reader.ReadFrame()
			recovered = append(recovered, frame...)
			if err != nil {
				recovered = append(recovered, reader.Buffered()...)
				break
			}
		}
		if !bytes.HasPrefix(data, recovered) {
			t.Fatal("framing changed received bytes")
		}
	})
}
