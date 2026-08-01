package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
	"github.com/CodeTonight-SA/hal-go/happi"
	"github.com/CodeTonight-SA/hal-go/internal/testutil"
)

// runtime resolves the reference happi.md, skipping when none is present.
//
// This used to be a hardcoded absolute path to one developer's home
// directory. Off that machine the spawn produced no events at all, so the
// suite failed with "missing started event" — reporting a broken client
// when the real cause was a missing runtime.
func runtime(t *testing.T) string {
	t.Helper()
	return testutil.RequireReferenceRuntime(t)
}

func TestDispatchVersionThroughBashRuntime(t *testing.T) {
	c := New("bash", runtime(t), "run")
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
	// Goodhart-resistant: assert the delta payload is a real HAPPI version,
	// not just "got a delta". It must be the RUNTIME's version, which is
	// free to advance ahead of this library — so check membership in the
	// accepted set rather than pinning one literal.
	if !happi.IsAccepted(deltaText) {
		t.Errorf("delta text = %q, want one of %s", deltaText, happi.AcceptedList())
	}
}

func TestDispatchEchoStreamsArgsInOrder(t *testing.T) {
	c := New("bash", runtime(t), "run")
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
	c := New("bash", runtime(t), "run")
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
	c := New("bash", runtime(t), "run")
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
	c := New("bash", runtime(t), "run")
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
