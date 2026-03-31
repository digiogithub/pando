package provider

// copilot_sse.go provides a large-buffer SSE decoder for the GitHub Copilot
// Responses API (used by GPT-5+ models). The default bufio.Scanner in the
// openai-go ssestream package uses a 64 KB buffer, which is too small for the
// larger events emitted by GPT-5.x models, causing:
//   "bufio.Scanner: token too long"
//
// Fix strategy:
//   1. Register a custom "text/event-stream" decoder backed by a 10 MB buffer
//      via ssestream.RegisterDecoder (handles the common case).
//   2. Attach a middleware to every Copilot client that strips MIME type
//      parameters from the Content-Type header so the exact-match lookup in the
//      SDK always succeeds (handles edge cases like "text/event-stream; charset=utf-8").

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/http"

	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
)

const sseMaxTokenSize = 10 * 1024 * 1024 // 10 MB

func init() {
	ssestream.RegisterDecoder("text/event-stream", newLargeBufferSSEDecoder)
}

// sseNormalizeMimeMiddleware strips MIME type parameters from the
// Content-Type response header so that the ssestream decoder registry
// lookup (which does an exact string match) always finds our large-buffer
// decoder even when the server returns e.g. "text/event-stream; charset=utf-8".
var sseNormalizeMimeMiddleware option.Middleware = func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	resp, err := next(req)
	if err != nil || resp == nil {
		return resp, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		return resp, nil
	}
	mediaType, _, parseErr := mime.ParseMediaType(ct)
	if parseErr == nil && mediaType != ct {
		resp.Header.Set("Content-Type", mediaType)
	}
	return resp, nil
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
