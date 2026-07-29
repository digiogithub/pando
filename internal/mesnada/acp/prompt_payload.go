package acp

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/digiogithub/pando/internal/imageopt"
)

const (
	// promptShrinkThresholdBytes is the payload size above which inline images
	// are re-encoded before the message is forwarded. acp-go-sdk >= v0.15.1 no
	// longer dies on large messages, so this is now an optimization (bandwidth,
	// latency and provider tokens) and a safety net for older SDK versions.
	promptShrinkThresholdBytes = 10 * 1024 * 1024

	// acpMaxLineBytes is our hard ceiling for one JSON-RPC line. Lines above it
	// are dropped with an error response instead of being buffered. It stays
	// below the SDK's own default cap so anything we accept, it accepts too.
	acpMaxLineBytes = 128 * 1024 * 1024

	// promptShrinkTargetBytes is the payload size we aim for when an oversized
	// prompt is re-encoded before being forwarded to the SDK connection.
	promptShrinkTargetBytes = 6 * 1024 * 1024

	// promptShrinkLongSidePx caps the longest side of a re-encoded image. It
	// matches the vision tier used by every provider we support, so nothing is
	// lost that the provider would not have downscaled anyway.
	promptShrinkLongSidePx = 1568
)

// errLineTooLong is returned by readJSONRPCLine when a line exceeds
// acpMaxLineBytes. The remainder of the line is discarded so the reader stays
// in sync with the stream.
var errLineTooLong = errors.New("json-rpc line exceeds maximum size")

// readJSONRPCLine reads one newline-terminated line with no bufio.Scanner size
// cap. Unlike a Scanner, an oversized line does not terminate the reader: the
// rest of the line is discarded and errLineTooLong is returned so the caller
// can answer the request and keep serving the session.
func readJSONRPCLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(chunk) > 0 {
			if !tooLong && len(line)+len(chunk) > max {
				tooLong = true
				// Keep a prefix so the caller can still recover the request id.
				if len(line) < 4096 {
					keep := 4096 - len(line)
					if keep > len(chunk) {
						keep = len(chunk)
					}
					line = append(line, chunk[:keep]...)
				}
			} else if !tooLong {
				line = append(line, chunk...)
			}
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			break
		}
		return nil, err
	}
	if tooLong {
		return line, errLineTooLong
	}
	return line, nil
}

var requestIDPattern = regexp.MustCompile(`"id"\s*:\s*("(?:[^"\\]|\\.)*"|-?\d+)`)

// peekRequestID extracts the JSON-RPC id from a (possibly truncated) message
// prefix so an oversized or unusable request can still be answered.
func peekRequestID(prefix []byte) json.RawMessage {
	m := requestIDPattern.FindSubmatch(prefix)
	if m == nil {
		return nil
	}
	return json.RawMessage(m[1])
}

// shrinkPromptImages re-encodes the inline image blocks of a session/prompt
// payload so the serialized line fits within target bytes. It returns the
// rewritten line, or an error when the payload cannot be reduced (no images,
// undecodable data, ...). The JSON is re-marshalled from a generic map, which
// is safe here: JSON-RPC carries no meaningful key ordering.
func shrinkPromptImages(line []byte, target int) ([]byte, error) {
	var msg map[string]any
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("shrink: unparseable payload: %w", err)
	}
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		return nil, errors.New("shrink: no params object")
	}
	blocks, _ := params["prompt"].([]any)
	if len(blocks) == 0 {
		return nil, errors.New("shrink: no prompt blocks")
	}

	images := make([]map[string]any, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := block["type"].(string); t != "image" {
			continue
		}
		if data, _ := block["data"].(string); data != "" {
			images = append(images, block)
		}
	}
	if len(images) == 0 {
		return nil, errors.New("shrink: no inline images to reduce")
	}

	// Leave room for the surrounding JSON and text blocks.
	budget := (target * 3 / 4) / len(images)
	if budget < 64*1024 {
		budget = 64 * 1024
	}

	shrunk := 0
	for _, block := range images {
		encoded, _ := block["data"].(string)
		mime, _ := block["mimeType"].(string)
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		out, outMIME, _, err := imageopt.Normalize(raw, mime, imageopt.Options{
			AutoResize:     true,
			MaxLongSidePx:  promptShrinkLongSidePx,
			MaxBase64Bytes: budget,
		})
		if err != nil {
			continue
		}
		block["data"] = base64.StdEncoding.EncodeToString(out)
		if outMIME != "" {
			block["mimeType"] = outMIME
		}
		shrunk++
	}
	if shrunk == 0 {
		return nil, errors.New("shrink: no image could be re-encoded")
	}

	rewritten, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("shrink: re-marshal failed: %w", err)
	}
	return rewritten, nil
}
