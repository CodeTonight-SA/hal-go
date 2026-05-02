package anthropic

import (
	"io"
	"testing"

	"github.com/CodeTonight-SA/hal-go/events"
)

// fakeStream is a minimal Stream implementation for unit testing the
// Adapter without pulling in any provider SDK.
type fakeStream struct {
	chunks []Chunk
	idx    int
	usage  Usage
	err    error
}

func (f *fakeStream) Next() (Chunk, error) {
	if f.err != nil && f.idx >= len(f.chunks) {
		return Chunk{}, f.err
	}
	if f.idx >= len(f.chunks) {
		return Chunk{}, io.EOF
	}
	c := f.chunks[f.idx]
	f.idx++
	return c, nil
}

func (f *fakeStream) Usage() Usage { return f.usage }

func TestAdapterEmitsStartedDeltasCompleted(t *testing.T) {
	a := New("env-1", func() int64 { return 7 })
	s := &fakeStream{
		chunks: []Chunk{{Text: "hello"}, {Text: " "}, {Text: "world"}},
		usage:  Usage{InputTokens: 5, OutputTokens: 3},
	}
	out := a.Adapt(s)
	if len(out) != 5 {
		t.Fatalf("event count = %d, want 5 (started + 3 deltas + completed)", len(out))
	}
	if _, ok := out[0].(*events.Started); !ok {
		t.Errorf("event 0: %T, want *Started", out[0])
	}
	want := []string{"hello", " ", "world"}
	for i, w := range want {
		d, ok := out[i+1].(*events.Delta)
		if !ok {
			t.Fatalf("event %d: %T, want *Delta", i+1, out[i+1])
		}
		if d.Text != w {
			t.Errorf("delta %d text = %q, want %q", i, d.Text, w)
		}
	}
	c, ok := out[4].(*events.Completed)
	if !ok {
		t.Fatalf("event 4: %T, want *Completed", out[4])
	}
	if c.Usage["input_tokens"] != 5 || c.Usage["output_tokens"] != 3 {
		t.Errorf("usage = %v, want input_tokens=5 output_tokens=3", c.Usage)
	}
}

func TestAdapterEmitsErrorOnNonEOF(t *testing.T) {
	a := New("env-2", nil)
	s := &fakeStream{
		chunks: []Chunk{{Text: "partial"}},
		err:    errBoom{},
	}
	out := a.Adapt(s)
	last := out[len(out)-1]
	e, ok := last.(*events.Error)
	if !ok {
		t.Fatalf("last event: %T, want *Error", last)
	}
	if e.Code != "runtime_error" {
		t.Errorf("error code = %q, want runtime_error", e.Code)
	}
}

func TestAdapterEmptyUsageOmitted(t *testing.T) {
	a := New("env-3", nil)
	s := &fakeStream{chunks: []Chunk{{Text: "x"}}}
	out := a.Adapt(s)
	c, ok := out[len(out)-1].(*events.Completed)
	if !ok {
		t.Fatalf("last event: %T, want *Completed", out[len(out)-1])
	}
	if c.Usage != nil {
		t.Errorf("Usage = %v, want nil for zero counts", c.Usage)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
