package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
)

// runtimePath points at the canonical happi.md reference runtime. We
// dispatch through `bash /path/to/happi.md run` exactly as a downstream
// caller would. Path is hard-wired to the V>> machine's installed copy
// because that is the conformance reference for v0.1.
const runtimePath = "/Users/lauriescheepers/.hal/happi.md"

func TestDispatchVersionThroughBashRuntime(t *testing.T) {
	c := New("bash", runtimePath, "run")
	env := envelope.New("test-version-1", "version")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	gotStarted, gotDelta, gotCompleted := false, false, false
	var deltaText string
	for ev := range stream {
		switch e := ev.(type) {
		case *events.Started:
			gotStarted = true
		case *events.Delta:
			gotDelta = true
			deltaText = e.Text
		case *events.Completed:
			gotCompleted = true
		case *events.Error:
			t.Fatalf("unexpected error event: %s/%s", e.Code, e.Message)
		}
	}

	if !gotStarted {
		t.Error("missing started event")
	}
	if !gotDelta {
		t.Error("missing delta event")
	}
	if !gotCompleted {
		t.Error("missing completed event")
	}
	// Goodhart-resistant: assert the delta payload is the expected
	// HAPPI version literal, not just "got a delta".
	if deltaText != "happi/1.0" {
		t.Errorf("delta text = %q, want \"happi/1.0\"", deltaText)
	}
}

func TestDispatchEchoStreamsArgsInOrder(t *testing.T) {
	c := New("bash", runtimePath, "run")
	env := envelope.New("test-echo-1", "echo")
	env.Args = []any{"alpha", "beta", "gamma"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	deltas := make([]string, 0, 3)
	for ev := range stream {
		if d, ok := ev.(*events.Delta); ok {
			deltas = append(deltas, d.Text)
		}
	}

	want := []string{"alpha", "beta", "gamma"}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %v, want %v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Errorf("delta %d = %q, want %q", i, deltas[i], want[i])
		}
	}
}

func TestDispatchErrorOnUnsupportedCmd(t *testing.T) {
	c := New("bash", runtimePath, "run")
	env := envelope.New("test-err-1", "this.cmd.does.not.exist")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var gotError *events.Error
	for ev := range stream {
		if e, ok := ev.(*events.Error); ok {
			gotError = e
		}
	}
	if gotError == nil {
		t.Fatal("missing error event for unsupported cmd")
	}
	if gotError.Code != "unsupported_cmd" {
		t.Errorf("error code = %q, want unsupported_cmd", gotError.Code)
	}
}

func TestDispatchRejectsInvalidEnvelope(t *testing.T) {
	c := New("bash", runtimePath, "run")
	bad := envelope.Envelope{} // no v/id/cmd

	_, err := c.Dispatch(context.Background(), bad)
	if err == nil {
		t.Fatal("expected error for invalid envelope")
	}
}

func TestDispatchRejectsEmptyRuntime(t *testing.T) {
	c := &Client{}
	env := envelope.New("x", "y")
	_, err := c.Dispatch(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for empty Runtime")
	}
}

func TestDispatchRespectsContextCancellation(t *testing.T) {
	c := New("bash", runtimePath, "run")
	env := envelope.New("test-ctx-1", "version")

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	cancel() // immediate cancel

	// Drain — the channel must close shortly after cancel. If it doesn't
	// the dispatcher leaked a goroutine.
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-stream:
			if !ok {
				return // success: channel closed
			}
		case <-timeout:
			t.Fatal("channel did not close after ctx cancellation")
		}
	}
}

// TestNewParity asserts client.New accepts variadic argv. Cheap test
// that catches regressions in the API surface.
func TestNewParity(t *testing.T) {
	c := New("a", "b", "c")
	if len(c.Runtime) != 3 || c.Runtime[0] != "a" || c.Runtime[2] != "c" {
		t.Errorf("Runtime = %v, want [a b c]", c.Runtime)
	}
	// Sanity: errors package is wired (catches accidental import loss).
	_ = errors.New("ok")
}
