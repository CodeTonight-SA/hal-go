// Package events implements the HAPPI/1.3 event types and an NDJSON
// Decoder that streams them off a runtime's stdout.
//
// Per axiom 2: seven event types are sufficient for streaming. All known
// LLM-provider streaming semantics map onto them. v1.1 adds two
// non-streaming records — IDR and Context — emitted AFTER the outcome
// event, so the outcome is not always the last event in a stream.
//
// This package provides one concrete struct per type plus a typed Event
// interface so callers can use Go's type switch:
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

	"github.com/CodeTonight-SA/hal-go/happi"
)

// Version is the protocol version this library emits.
const Version = happi.Version

// KnownVersions are the protocol minors that existed when this library was
// written. Informational only: the Decoder accepts any happi/1.x via
// happi.IsAccepted, because a runtime stamps events with its own version and
// is free to advance ahead of this library. See the happi package.
var KnownVersions = happi.KnownVersions

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

// IDRRef is the audit reference carried by an IDR event.
type IDRRef struct {
	SHA256        string   `json:"sha256"`
	ModelVersions []string `json:"model_versions"`
	CID           *string  `json:"cid,omitempty"`
	BlockAnchor   any      `json:"block_anchor,omitempty"`
}

// IDR is the Intent Decision Record for one dispatch (v1.1). Emitted after
// the outcome event when flags.audit=true, or via cmd "idr.emit". Its
// SHA256 hashes (envelope JSON + event NDJSON), so it answers "what exactly
// happened in this run?".
type IDR struct {
	base
	Ref IDRRef `json:"idr_ref"`
}

// ContextRef is the memory-chain reference carried by a Context event.
type ContextRef struct {
	SHA256             string   `json:"sha256"`
	Kind               string   `json:"kind"`
	ModelVersions      []string `json:"model_versions"`
	PredecessorContext *string  `json:"predecessor_context,omitempty"`
	SnapshotRef        *string  `json:"snapshot_ref,omitempty"`
}

// Context is a replayable memory-chain link — the structural twin of IDR
// (v1.1). Its SHA256 content-addresses the decision BODY, excluding the
// volatile id/ts/audit fields, so it answers "what is the identity of this
// belief, regardless of when it was recorded?". Emitted via cmd
// "context.append".
type Context struct {
	base
	Ref ContextRef `json:"context_ref"`
}

// Context kinds per happi.md CONTEXT section.
const (
	KindContextDelta     = "context-delta"
	KindContextSnapshot  = "context-snapshot"
	KindContextSupersede = "context-supersede"
)

// Unknown is an event whose type this library does not recognise.
//
// The spec makes tolerating these mandatory rather than optional: "unknown
// event types/fields are ignored, never rejected". A newer runtime may
// introduce event types that did not exist when this library was written, and
// a consumer is required to forward them inertly rather than fail.
//
// Fields carries everything the event held, so a caller that DOES understand
// the new type can still read it. Unknown is deliberately neither an outcome
// nor a post-terminator, so it can never be mistaken for the end of a stream.
type Unknown struct {
	base
	Fields map[string]any
}

// decodeUnknown wraps an unrecognised event, preserving its contents.
func decodeUnknown(line []byte, typ string) (Event, error) {
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		return nil, fmt.Errorf("events: decode unknown %s: %w", typ, err)
	}
	ev := &Unknown{Fields: fields}
	if err := json.Unmarshal(line, &ev.base); err != nil {
		return nil, fmt.Errorf("events: decode unknown %s: %w", typ, err)
	}
	return ev, nil
}

// known is the dispatch table from HAPPI type strings to constructors.
// O(1) lookup keeps Decoder.Next's cyclomatic complexity flat. A type absent
// from this table is NOT an error — see parseLine and Unknown.
var known = map[string]func() Event{
	"started":     func() Event { return &Started{} },
	"delta":       func() Event { return &Delta{} },
	"tool_call":   func() Event { return &ToolCall{} },
	"tool_result": func() Event { return &ToolResult{} },
	"sub_request": func() Event { return &SubRequest{} },
	"completed":   func() Event { return &Completed{} },
	"error":       func() Event { return &Error{} },
	"idr":         func() Event { return &IDR{} },
	"context":     func() Event { return &Context{} },
}

// outcomeTypes report the result of a dispatch. Exactly one per stream.
var outcomeTypes = map[string]struct{}{
	"completed": {}, "error": {},
}

// postTerminatorTypes are non-streaming records emitted AFTER the outcome
// event (v1.1). Their presence does not make a stream malformed.
var postTerminatorTypes = map[string]struct{}{
	"idr": {}, "context": {},
}

// IsOutcome reports whether ev carries the result of the dispatch.
//
// Note (v1.1+): the outcome is NOT always the LAST event — an audited run
// ends "...completed, idr". Callers that stop at the outcome should still
// drain the stream, and must not treat a trailing record as a violation.
func IsOutcome(ev Event) bool {
	_, ok := outcomeTypes[ev.EventType()]
	return ok
}

// IsPostTerminator reports whether ev is an idr or context record.
func IsPostTerminator(ev Event) bool {
	_, ok := postTerminatorTypes[ev.EventType()]
	return ok
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
	if !happi.IsAccepted(peek.V) {
		return nil, fmt.Errorf("events: unsupported version %q (accepted: %s)",
			peek.V, happi.AcceptedList())
	}
	ctor, ok := known[peek.Type]
	if !ok {
		// Unknown types are tolerated, not rejected. The spec requires it:
		// "unknown event types/fields are ignored, never rejected". Rejecting
		// them would also undo the forward-compatible version policy — there
		// is no value in accepting a happi/1.9 stream if the first event type
		// introduced in 1.9 kills the decode.
		return decodeUnknown(line, peek.Type)
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
