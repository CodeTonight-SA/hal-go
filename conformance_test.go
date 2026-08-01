// Package halgo conformance tests verify that the Go client and the
// canonical bash happi.md runtime agree on the wire form.
//
// The runtime is resolved from $HAPPI_MD, then the local install. It is not
// bundled with this repo, so an environment without one sees a clean skip
// rather than a failure. Fetch the published reference to enable these:
//
//	curl -fsSL "https://gist.githubusercontent.com/LaurieScheepers/2483d5c218c21ecc931130bcee7dee83/raw/happi.md" -o /tmp/happi.md
//	HAPPI_MD=/tmp/happi.md go test ./...
package halgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/CodeTonight-SA/hal-go/client"
	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
	"github.com/CodeTonight-SA/hal-go/happi"
	"github.com/CodeTonight-SA/hal-go/internal/testutil"
)

// referenceRuntimeFor resolves the runtime path, skipping when absent.
func referenceRuntimeFor(t *testing.T) string {
	t.Helper()
	return testutil.RequireReferenceRuntime(t)
}

// TestConformanceVersion asserts dispatching {cmd:version} via bash
// happi.md run produces the canonical started/delta/completed sequence
// with delta.text carrying the runtime's own protocol version.
func TestConformanceVersion(t *testing.T) {
	c := client.New("bash", referenceRuntimeFor(t), "run")
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

	// The runtime reports ITS version, which may be newer than this library.
	if !happi.IsAccepted(delta) {
		t.Errorf("delta = %q, want one of %s", delta, happi.AcceptedList())
	}
	if len(typesSeen) < 3 {
		t.Errorf("event count = %d (%v), want at least started/delta/completed", len(typesSeen), typesSeen)
	}
}
