// Package halgo conformance tests verify that the Go client and the
// canonical bash happi.md runtime agree on the wire form.
//
// SKIPPED if /Users/lauriescheepers/.hal/happi.md is missing — the
// reference runtime is V>>'s installed copy and not bundled with this
// repo. CI environments without the file should see a clean skip, not
// a failure.
package halgo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/CodeTonight-SA/hal-go/client"
	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
)

const referenceRuntime = "/Users/lauriescheepers/.hal/happi.md"

func skipIfNoRuntime(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(referenceRuntime); err != nil {
		t.Skipf("conformance: reference runtime %q not present (%v); skipping", referenceRuntime, err)
	}
}

// TestConformanceVersion asserts dispatching {cmd:version} via bash
// happi.md run produces the canonical started/delta/completed sequence
// with delta.text == "happi/1.0".
func TestConformanceVersion(t *testing.T) {
	skipIfNoRuntime(t)
	c := client.New("bash", referenceRuntime, "run")
	env := envelope.New("conformance-version-1", "version")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var typesSeen []string
	var delta string
	for ev := range stream {
		typesSeen = append(typesSeen, ev.EventType())
		if d, ok := ev.(*events.Delta); ok {
			delta = d.Text
		}
		if e, ok := ev.(*events.Error); ok {
			t.Fatalf("conformance error: %s/%s", e.Code, e.Message)
		}
	}

	if delta != "happi/1.0" {
		t.Errorf("delta = %q, want \"happi/1.0\"", delta)
	}
	if len(typesSeen) < 3 {
		t.Errorf("event count = %d (%v), want at least started/delta/completed", len(typesSeen), typesSeen)
	}
}
