package provider

// copilot_sse.go registers a custom SSE decoder with a large buffer for the
// GitHub Copilot API. GPT-5+ models (Responses API) can emit SSE events that
// exceed the default 64 KB bufio.Scanner limit, causing a
// "bufio.Scanner: token too long" error. We fix this at init time by
// registering a text/event-stream decoder backed by a 10 MB buffer.

import (
	"bufio"
	"bytes"
	"io"

	"github.com/openai/openai-go/packages/ssestream"
)

const sseMaxTokenSize = 10 * 1024 * 1024 // 10 MB

func init() {
	ssestream.RegisterDecoder("text/event-stream", newLargeBufferSSEDecoder)
}

func newLargeBufferSSEDecoder(rc io.ReadCloser) ssestream.Decoder {
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, sseMaxTokenSize), sseMaxTokenSize)
	return &largeBufferSSEDecoder{rc: rc, scn: scanner}
}

type largeBufferSSEDecoder struct {
	evt ssestream.Event
	rc  io.ReadCloser
	scn *bufio.Scanner
	err error
}

func (s *largeBufferSSEDecoder) Next() bool {
	if s.err != nil {
		return false
	}

	event := ""
	data := bytes.NewBuffer(nil)

	for s.scn.Scan() {
		txt := s.scn.Bytes()

		// Empty line → dispatch event
		if len(txt) == 0 {
			s.evt = ssestream.Event{
				Type: event,
				Data: data.Bytes(),
			}
			return true
		}

		name, value, _ := bytes.Cut(txt, []byte(":"))

		// Consume optional space after colon
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		switch string(name) {
		case "":
			// comment line, ignore
			continue
		case "event":
			event = string(value)
		case "data":
			_, s.err = data.Write(value)
			if s.err != nil {
				return false
			}
			_, s.err = data.WriteRune('\n')
			if s.err != nil {
				return false
			}
		}
	}

	if s.scn.Err() != nil {
		s.err = s.scn.Err()
	}

	return false
}

func (s *largeBufferSSEDecoder) Event() ssestream.Event { return s.evt }
func (s *largeBufferSSEDecoder) Close() error           { return s.rc.Close() }
func (s *largeBufferSSEDecoder) Err() error             { return s.err }
