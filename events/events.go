// Package events implements the seven HAPPI/1.0 event types and an
// NDJSON Decoder that streams them off a runtime's stdout.
//
// Per HAPPI/1.0 axiom 2: seven event types are sufficient. All known
// LLM-provider streaming semantics map onto them. This package provides
// one concrete struct per type plus a typed Event interface so callers
// can use Go's type switch:
//
//	for ev := range stream {
//	    switch e := ev.(type) {
//	    case events.Started:    ...
//	    case events.Delta:      print(e.Text)
//	    case events.Completed:  break
//	    case events.Error:      log.Fatal(e.Code, e.Message)
//	    }
//	}
//
// Reference: happi.md, "Events (stdout)" section.
package events

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Version is the HAPPI protocol version literal that event.V must equal.
const Version = "happi/1.0"

// Event is the typed HAPPI event interface. Every event carries v, id,
// type, ts. Concrete types add type-specific fields.
type Event interface {
	EventType() string
	EnvelopeID() string
	Timestamp() int64
}

// base holds the four fields every HAPPI event has. Embedded in each
// concrete type to satisfy Event without duplication (DRY).
type base struct {
	V    string `json:"v"`
	ID   string `json:"id"`
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}

func (b base) EventType() string  { return b.Type }
func (b base) EnvelopeID() string { return b.ID }
func (b base) Timestamp() int64   { return b.TS }

// Started signals dispatch began.
type Started struct{ base }

// Delta is a streamed content chunk.
type Delta struct {
	base
	Text string `json:"text"`
}

// ToolCall signals the runtime is invoking a tool.
type ToolCall struct {
	base
	Name   string `json:"name"`
	Input  any    `json:"input"`
	CallID string `json:"call_id"`
}

// ToolResult signals a tool returned.
type ToolResult struct {
	base
	Name   string `json:"name"`
	Output any    `json:"output"`
	CallID string `json:"call_id"`
}

// SubRequest signals a child HAPPI envelope was dispatched. Per axiom 3
// the protocol is fractal — the embedded envelope follows the same
// schema and can recurse without depth limit.
type SubRequest struct {
	base
	Envelope json.RawMessage `json:"envelope"`
}

// Completed signals success. Usage is optional and provider-specific.
type Completed struct {
	base
	Usage map[string]any `json:"usage,omitempty"`
}

// Error signals failure. The runtime exits non-zero after emitting one.
type Error struct {
	base
	Code    string `json:"code"`
	Message string `json:"message"`
}

// known is the dispatch table from HAPPI type strings to constructors.
// O(1) lookup keeps Decoder.Next's cyclomatic complexity flat.
var known = map[string]func() Event{
	"started":     func() Event { return &Started{} },
	"delta":       func() Event { return &Delta{} },
	"tool_call":   func() Event { return &ToolCall{} },
	"tool_result": func() Event { return &ToolResult{} },
	"sub_request": func() Event { return &SubRequest{} },
	"completed":   func() Event { return &Completed{} },
	"error":       func() Event { return &Error{} },
}

// Decoder parses NDJSON event lines from an io.Reader and emits typed
// Event values. One JSON object per line. Lines exceeding 1 MiB are
// rejected; callers needing larger deltas should construct their own.
type Decoder struct {
	scanner *bufio.Scanner
}

// maxLineBytes caps a single NDJSON line. 1 MiB accommodates very large
// delta chunks without unbounded memory.
const maxLineBytes = 1 << 20

// NewDecoder wraps r and returns a streaming HAPPI event decoder.
func NewDecoder(r io.Reader) *Decoder {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxLineBytes)
	return &Decoder{scanner: s}
}

// Next reads one event. Returns (nil, io.EOF) at end of stream. Any
// parse error is wrapped and returned — callers may choose to abort
// or continue. The Decoder remains usable across non-EOF errors.
func (d *Decoder) Next() (Event, error) {
	if !d.scanner.Scan() {
		if err := d.scanner.Err(); err != nil {
			return nil, fmt.Errorf("events: scanner: %w", err)
		}
		return nil, io.EOF
	}
	line := d.scanner.Bytes()
	if len(line) == 0 {
		return d.Next() // skip blank lines per NDJSON convention
	}
	return parseLine(line)
}

// parseLine decodes one JSON object into a typed Event. Two-pass: first
// peek at the type field, then decode into the concrete struct.
// Extracted to keep Decoder.Next under cyclomatic-complexity ceiling.
func parseLine(line []byte) (Event, error) {
	var peek struct {
		Type string `json:"type"`
		V    string `json:"v"`
	}
	if err := json.Unmarshal(line, &peek); err != nil {
		return nil, fmt.Errorf("events: invalid JSON line: %w", err)
	}
	if peek.V != Version {
		return nil, fmt.Errorf("events: unsupported version %q (want %q)", peek.V, Version)
	}
	ctor, ok := known[peek.Type]
	if !ok {
		return nil, fmt.Errorf("events: unknown type %q", peek.Type)
	}
	ev := ctor()
	if err := json.Unmarshal(line, ev); err != nil {
		return nil, fmt.Errorf("events: decode %s: %w", peek.Type, err)
	}
	return ev, nil
}

// ErrNoTerminator indicates a stream ended without a completed/error
// event. Per HAPPI contract, every stream must end with one or the other.
var ErrNoTerminator = errors.New("events: stream ended without completed or error")
