package events

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// sampleNDJSON exercises all 7 event types in a single stream so we
// can prove the type-discrimination is exhaustive.
const sampleNDJSON = `{"v":"happi/1.0","id":"r1","type":"started","ts":0}
{"v":"happi/1.0","id":"r1","type":"delta","ts":1,"text":"hello"}
{"v":"happi/1.0","id":"r1","type":"tool_call","ts":2,"name":"read","input":{"path":"/tmp/x"},"call_id":"c1"}
{"v":"happi/1.0","id":"r1","type":"tool_result","ts":3,"name":"read","output":"data","call_id":"c1"}
{"v":"happi/1.0","id":"r1","type":"sub_request","ts":4,"envelope":{"v":"happi/1.0","id":"sub1","cmd":"version"}}
{"v":"happi/1.0","id":"r1","type":"completed","ts":5,"usage":{"input_tokens":10,"output_tokens":20}}
`

func TestDecoderParsesAllSevenTypes(t *testing.T) {
	d := NewDecoder(strings.NewReader(sampleNDJSON))
	wantTypes := []string{"started", "delta", "tool_call", "tool_result", "sub_request", "completed"}
	gotTypes := make([]string, 0, len(wantTypes))
	for {
		ev, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		gotTypes = append(gotTypes, ev.EventType())
		if ev.EnvelopeID() != "r1" {
			t.Errorf("EnvelopeID = %q, want r1", ev.EnvelopeID())
		}
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %v", len(gotTypes), len(wantTypes), gotTypes)
	}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Errorf("event %d: type %q, want %q", i, gotTypes[i], wantTypes[i])
		}
	}
}

func TestDeltaCarriesText(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/1.0","id":"x","type":"delta","ts":1,"text":"chunk-of-text"}` + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	delta, ok := ev.(*Delta)
	if !ok {
		t.Fatalf("type assertion: got %T, want *Delta", ev)
	}
	if delta.Text != "chunk-of-text" {
		t.Errorf("Text = %q, want chunk-of-text", delta.Text)
	}
	if delta.TS != 1 {
		t.Errorf("TS = %d, want 1", delta.TS)
	}
}

func TestErrorEventCarriesCodeAndMessage(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/1.0","id":"x","type":"error","ts":99,"code":"parse_error","message":"bad input"}` + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	e, ok := ev.(*Error)
	if !ok {
		t.Fatalf("type assertion: got %T, want *Error", ev)
	}
	if e.Code != "parse_error" || e.Message != "bad input" {
		t.Errorf("got code=%q message=%q, want parse_error/bad input", e.Code, e.Message)
	}
}

func TestCompletedUsageIsOptional(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/1.0","id":"x","type":"completed","ts":5}` + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	c, ok := ev.(*Completed)
	if !ok {
		t.Fatalf("type assertion: got %T, want *Completed", ev)
	}
	if c.Usage != nil {
		t.Errorf("Usage = %v, want nil for missing field", c.Usage)
	}
}

func TestRejectsUnknownEventType(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/1.0","id":"x","type":"nonexistent","ts":0}` + "\n"))
	_, err := d.Next()
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("error = %q, want contains \"unknown type\"", err.Error())
	}
}

func TestRejectsBadVersion(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/0.5","id":"x","type":"started","ts":0}` + "\n"))
	_, err := d.Next()
	if err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestSkipsBlankLines(t *testing.T) {
	stream := `{"v":"happi/1.0","id":"x","type":"started","ts":0}

{"v":"happi/1.0","id":"x","type":"completed","ts":1}
`
	d := NewDecoder(strings.NewReader(stream))
	count := 0
	for {
		_, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		count++
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (blank lines should be skipped)", count)
	}
}

func TestSubRequestRecursionPreserved(t *testing.T) {
	d := NewDecoder(strings.NewReader(`{"v":"happi/1.0","id":"r1","type":"sub_request","ts":0,"envelope":{"v":"happi/1.0","id":"sub","cmd":"version","args":["x"]}}` + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	sub, ok := ev.(*SubRequest)
	if !ok {
		t.Fatalf("type assertion: got %T, want *SubRequest", ev)
	}
	// The embedded envelope must be preserved as raw JSON for recursive
	// dispatch — this is HAPPI axiom 3 (sub_request recurses).
	if !strings.Contains(string(sub.Envelope), `"cmd":"version"`) {
		t.Errorf("envelope payload lost: %s", string(sub.Envelope))
	}
}
