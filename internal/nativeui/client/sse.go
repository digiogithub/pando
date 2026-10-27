package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
)

// SSEFrame is one complete Server-Sent Events frame.
type SSEFrame struct {
	// Event is the `event:` name; empty for an unnamed (message) event.
	Event string
	// Data is the `data:` lines joined with "\n".
	Data string
	// ID is the `id:` field, when present.
	ID string
}

// SSEParser reassembles SSE frames from arbitrary byte chunks, including
// chunks split inside a UTF-8 sequence or between "\r" and "\n". Pando writes
// `event: <name>\ndata: <json>\n\n` plus `: keepalive` comments; the parser
// implements the general format anyway (multi-line data, ids, CRLF).
type SSEParser struct {
	line  []byte
	event string
	data  []string
	id    string
}

// Push feeds a chunk and returns every frame it completed.
func (p *SSEParser) Push(chunk []byte) []SSEFrame {
	var out []SSEFrame
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			p.line = append(p.line, chunk...)
			break
		}
		p.line = append(p.line, chunk[:i]...)
		chunk = chunk[i+1:]
		line := p.line
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		out = p.handleLine(line, out)
		p.line = p.line[:0]
	}
	return out
}

// Finish flushes a trailing frame of a stream cut without its final blank
// line.
func (p *SSEParser) Finish() []SSEFrame {
	var out []SSEFrame
	if len(p.line) > 0 {
		line := bytes.TrimSuffix(p.line, []byte("\r"))
		out = p.handleLine(line, out)
		p.line = p.line[:0]
	}
	return p.dispatch(out)
}

func (p *SSEParser) handleLine(line []byte, out []SSEFrame) []SSEFrame {
	if len(line) == 0 {
		return p.dispatch(out)
	}
	if line[0] == ':' {
		return out // comment / keep-alive
	}
	text := string(line)
	field, value, found := strings.Cut(text, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.event = value
	case "data":
		p.data = append(p.data, value)
	case "id":
		p.id = value
	}
	return out
}

func (p *SSEParser) dispatch(out []SSEFrame) []SSEFrame {
	event, id := p.event, p.id
	p.event, p.id = "", ""
	if len(p.data) == 0 {
		return out
	}
	data := strings.Join(p.data, "\n")
	p.data = p.data[:0]
	return append(out, SSEFrame{Event: event, Data: data, ID: id})
}

// errStopSSE is returned by a frame handler to stop reading without error.
var errStopSSE = errors.New("stop sse")

// readSSE reads frames from r until EOF, ctx is done or fn returns an error.
// A handler returning errStopSSE ends the read with a nil error. It returns
// io.ErrUnexpectedEOF-free results: a clean EOF is nil.
func readSSE(ctx context.Context, r io.Reader, fn func(SSEFrame) error) error {
	var p SSEParser
	br := bufio.NewReaderSize(r, 32*1024)
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := br.Read(buf)
		if n > 0 {
			for _, f := range p.Push(buf[:n]) {
				if herr := fn(f); herr != nil {
					if errors.Is(herr, errStopSSE) {
						return nil
					}
					return herr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				for _, f := range p.Finish() {
					if herr := fn(f); herr != nil {
						if errors.Is(herr, errStopSSE) {
							return nil
						}
						return herr
					}
				}
				return nil
			}
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			return err
		}
	}
}
