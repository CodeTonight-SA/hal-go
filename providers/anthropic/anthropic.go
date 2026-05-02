// Package anthropic is a provider adapter that maps Anthropic's
// streaming response shape onto the HAPPI/1.0 event stream.
//
// v0.1 SCOPE: This package defines the adapter INTERFACE and a
// stdlib-only reference implementation that translates an in-memory
// "completion" stream (a sequence of text chunks + final usage stats)
// into HAPPI events. The wiring to the official anthropic-sdk-go
// `messages.create(stream=true)` SDK is deferred to v0.2 — the SDK
// surface is intentionally not pulled in to keep the v0.1 dependency
// graph at zero external imports per the brief's "Zero external deps
// in core" requirement.
//
// See provider.go (v0.2) for the SDK adapter once dependencies are
// permitted. The interface here is what that adapter will satisfy.
//
// Reference: HAPPI possibilities §10.1 (Runtime Proliferation),
// happi.md axiom 2 (seven event types are sufficient).
package anthropic

import (
	"github.com/CodeTonight-SA/hal-go/events"
)

// Chunk is one streamed text chunk produced by a provider. The adapter
// emits one events.Delta per Chunk.
type Chunk struct {
	Text string
}

// Usage is the optional accounting payload emitted with completed.
// Mirrors Anthropic's usage shape but uses generic Go types so any
// future provider can populate it.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// Stream is a provider-agnostic streaming response. Implementations
// produce zero or more Chunks and one final Usage. The adapter wraps
// any Stream and emits the canonical HAPPI event sequence.
type Stream interface {
	// Next returns the next chunk, or io.EOF when the stream is done.
	Next() (Chunk, error)
	// Usage returns the final accounting after Next returns io.EOF.
	Usage() Usage
}

// Adapter wraps a provider Stream and produces HAPPI events. Each call
// to Adapt emits started -> (delta)* -> completed | error and returns
// the slice in order.
//
// EnvelopeID identifies which envelope this response belongs to.
type Adapter struct {
	EnvelopeID string
	clock      func() int64 // injectable for deterministic tests
}

// New returns an adapter for the given envelope id. clock defaults to
// a monotonic timestamp; test code injects a deterministic clock.
func New(envelopeID string, clock func() int64) *Adapter {
	return &Adapter{EnvelopeID: envelopeID, clock: clock}
}

// Adapt drains the stream and returns the canonical HAPPI event slice.
// Returns the events even on error so callers can inspect partial state.
func (a *Adapter) Adapt(s Stream) []events.Event {
	out := []events.Event{a.started()}
	for {
		ch, err := s.Next()
		if err != nil {
			return a.finishOnError(out, err, s)
		}
		out = append(out, a.delta(ch.Text))
	}
}

// finishOnError emits the trailing completed-or-error event. Extracted
// to keep Adapt's cyclomatic complexity below the gocyclo ceiling.
func (a *Adapter) finishOnError(prefix []events.Event, err error, s Stream) []events.Event {
	if err.Error() == "EOF" || isEOF(err) {
		return append(prefix, a.completed(s.Usage()))
	}
	return append(prefix, a.errorEvent("runtime_error", err.Error()))
}

// isEOF avoids importing io for a single sentinel comparison.
func isEOF(err error) bool {
	return err != nil && err.Error() == "EOF"
}

func (a *Adapter) ts() int64 {
	if a.clock == nil {
		return 0
	}
	return a.clock()
}

func (a *Adapter) started() events.Event {
	return &events.Started{}
}

func (a *Adapter) delta(text string) events.Event {
	return &events.Delta{Text: text}
}

func (a *Adapter) completed(u Usage) events.Event {
	usage := map[string]any{}
	if u.InputTokens > 0 {
		usage["input_tokens"] = u.InputTokens
	}
	if u.OutputTokens > 0 {
		usage["output_tokens"] = u.OutputTokens
	}
	c := &events.Completed{}
	if len(usage) > 0 {
		c.Usage = usage
	}
	return c
}

func (a *Adapter) errorEvent(code, message string) events.Event {
	return &events.Error{Code: code, Message: message}
}
