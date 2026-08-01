package runtime

import (
	"testing"

	"github.com/CodeTonight-SA/hal-go/happi"
)

// Version handling, stream shape, the audit terminator, and context.append.
// Citation verification lives in cite_test.go.

// A runtime newer than this library is the normal case, not an error.
var acceptedVersions = []string{
	"happi/1.0", "happi/1.1", "happi/1.2", "happi/1.3", "happi/1.7", "happi/1.42",
}

// A major bump signals a breaking change; anything unparseable is not a
// version at all.
var rejectedVersions = []string{
	"happi/0.9", "happi/2.0", "happi/", "1.3", "junk",
}

// Every distinct way this runtime can fail.
var failingEnvelopes = []string{
	`{"v":"happi/1.3","id":"a","cmd":"no.such.cmd"}`,
	`{"v":"happi/9.9","id":"b","cmd":"version"}`,
	`{"v":"happi/1.3","id":"c","cmd":"context.append"}`,
	`{"v":"happi/1.3","id":"d","cmd":"cite.verify","flags":{}}`,
	`not json at all`,
}

// --- version handling -------------------------------------------------------

func TestRuntimeReportsItsOwnVersionNotTheEnvelopes(t *testing.T) {
	// The single most important version property, and the one whose absence
	// caused every SDK breakage this release fixes: a runtime stamps events
	// with ITS version, so a v1.0 envelope still yields v1.3 events.
	events, failed := dispatch(t, versionEnvelope("happi/1.0"))
	if failed {
		t.Fatal("a happi/1.0 envelope must be accepted")
	}
	if !allCarryVersion(events, happi.Version) {
		t.Errorf("events = %v, want every event stamped %s", events, happi.Version)
	}
	if events[1]["text"] != happi.Version {
		t.Errorf("version delta = %v, want %v", events[1]["text"], happi.Version)
	}
}

func TestAcceptsAnyMinorOfTheSupportedMajor(t *testing.T) {
	forEach(t, acceptedVersions, func(t *testing.T, version string) {
		if _, failed := dispatch(t, versionEnvelope(version)); failed {
			t.Errorf("%s must be accepted: a runtime newer than this library "+
				"is the normal case, not an error", version)
		}
	})
}

func TestRejectsOtherMajorsAndMalformedVersions(t *testing.T) {
	forEach(t, rejectedVersions, func(t *testing.T, version string) {
		events, failed := dispatch(t, versionEnvelope(version))
		if !failed {
			t.Errorf("%s must be rejected", version)
		}
		if got := typesOf(events); len(got) != 1 || got[0] != "error" {
			t.Errorf("%s produced %v, want a single error event", version, got)
		}
	})
}

// --- stream shape -----------------------------------------------------------

func TestExactlyOneOutcomeEventPerStream(t *testing.T) {
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"o","cmd":"echo","args":["a","b"]}`)
	if got := countOutcomes(events); got != 1 {
		t.Errorf("got %d outcome events, want exactly 1", got)
	}
}

func TestEchoStreamsOneDeltaPerArgument(t *testing.T) {
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"e","cmd":"echo","args":["one","two"]}`)
	texts := deltaTexts(events)
	if len(texts) != 2 || texts[0] != "one" || texts[1] != "two" {
		t.Errorf("deltas = %v, want [one two]", texts)
	}
}

func TestUnknownCommandReportsUnsupportedCmd(t *testing.T) {
	events, failed := dispatch(t, `{"v":"happi/1.3","id":"u","cmd":"no.such.cmd"}`)
	if !failed {
		t.Error("an unknown command must exit non-zero")
	}
	if code := lastEvent(events)["code"]; code != "unsupported_cmd" {
		t.Errorf("code = %v, want unsupported_cmd", code)
	}
}

func TestEveryFailurePathEmitsAnErrorEventAndFails(t *testing.T) {
	// An error event and a non-zero exit must never come apart: a caller that
	// checks only one of them would otherwise miss half the failures.
	forEach(t, failingEnvelopes, func(t *testing.T, envelopeJSON string) {
		events, failed := dispatch(t, envelopeJSON)
		if emitted := hasType(events, "error"); failed != emitted {
			t.Errorf("for %s: exit-failure=%v but error-event=%v — these must agree",
				envelopeJSON, failed, emitted)
		}
	})
}

// --- audit ------------------------------------------------------------------

const auditedEcho = `{"v":"happi/1.1","id":"a","cmd":"echo","args":["x"],
	"flags":{"audit":true,"model_versions":["test-model"]}}`

func TestAuditAppendsAnIdrAfterTheOutcome(t *testing.T) {
	events, _ := dispatch(t, auditedEcho)
	kinds := typesOf(events)
	if kinds[len(kinds)-1] != "idr" {
		t.Fatalf("stream = %v, want idr last (it is a terminator)", kinds)
	}
	at := order(events)
	if at["completed"] > at["idr"] {
		t.Error("idr must follow the outcome, not precede it")
	}
}

func TestIdrHashIsDeterministicForTheSameEnvelope(t *testing.T) {
	first, _ := dispatch(t, auditedEcho)
	second, _ := dispatch(t, auditedEcho)
	refA := lastEvent(first)["idr_ref"].(map[string]any)
	refB := lastEvent(second)["idr_ref"].(map[string]any)
	if refA["sha256"] != refB["sha256"] {
		t.Error("the same envelope and event sequence must hash identically")
	}
}

func TestNoIdrWhenAuditIsNotRequested(t *testing.T) {
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"a","cmd":"echo","args":["x"]}`)
	if hasType(events, "idr") {
		t.Error("idr is opt-in; it must not appear without flags.audit")
	}
}

// --- context.append ---------------------------------------------------------

func TestContextIsEmittedAfterTheOutcome(t *testing.T) {
	// HAPPI.md calls context "a non-streaming terminator emitted after
	// completed/error" and names the reverse order as a falsifier. The
	// reference runtime emits it BEFORE, so this runtime fails the recorded
	// fixture for context.append rather than copy the violation. This test
	// pins the spec-correct order so nobody resolves that mismatch by
	// adopting the bug. Tracked as hal-conformance issue #2.
	events, failed := dispatch(t, `{"v":"happi/1.3","id":"c","cmd":"context.append",
		"args":["{\"decision\":\"d\",\"rationale\":\"r\"}"],
		"flags":{"kind":"context-delta","model_versions":["m"]}}`)
	if failed {
		t.Fatal("context.append with a valid body must succeed")
	}
	if !hasType(events, "context") {
		t.Fatalf("stream = %v, want a context event", typesOf(events))
	}
	at := order(events)
	if at["completed"] > at["context"] {
		t.Errorf("stream = %v, want completed BEFORE context", typesOf(events))
	}
}

func TestContentAddressIgnoresVolatileFields(t *testing.T) {
	body := map[string]any{"decision": "accept", "rationale": "because"}
	noisy := map[string]any{"decision": "accept", "rationale": "because",
		"id": "xyz", "ts": 999, "audit": true}
	a, _ := ContentAddr(body)
	b, _ := ContentAddr(noisy)
	if a != b {
		t.Error("id/ts/audit must not affect the address — that equality IS " +
			"the dedup coordinate")
	}
}

func TestContentAddressChangesWhenMeaningChanges(t *testing.T) {
	a, _ := ContentAddr(map[string]any{"decision": "accept"})
	b, _ := ContentAddr(map[string]any{"decision": "reject"})
	if a == b {
		t.Error("different decisions must have different addresses")
	}
}

func TestContentAddressMatchesTheCrossRuntimeValue(t *testing.T) {
	// Pinned against the value the reference runtime and hal-py both produce
	// for the body in conformance fixture 07. If Go drifts from this, context
	// chains written by Go stop de-duplicating against chains written by any
	// other runtime — the exact failure the content address exists to
	// prevent, and it would stay invisible until two chains had to merge.
	body := map[string]any{
		"decision":  "Accept happi/1.0 through happi/1.3 rather than one exact version",
		"rationale": "A runtime stamps events with its own version, never the envelope's",
	}
	got, err := ContentAddr(body)
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:84c0b0be810d70c7d1eb86e3609cbf0df9697a7a3b145e349332c8bb8133136f"
	if got != want {
		t.Errorf("ContentAddr = %s, want %s (cross-runtime divergence)", got, want)
	}
}

func TestCanonicalJSONDoesNotHTMLEscape(t *testing.T) {
	// encoding/json escapes <, > and & by default; Python does not. Left
	// unchecked, Go and Python would produce different hashes for any body
	// containing those characters — a divergence that shows up only for
	// certain content, which is the hardest kind to notice.
	canonical, err := canonicalJSON(map[string]any{"decision": "a < b && c > d"})
	if err != nil {
		t.Fatal(err)
	}
	// Assert the absence of the ESCAPE SEQUENCES, not of the characters
	// themselves. The decision text legitimately contains <, > and &, so a
	// check for those characters fails on correct output — which is exactly
	// what this assertion did on its first draft.
	if containsAny(canonical, `\u003c`, `\u003e`, `\u0026`) {
		t.Errorf("canonical JSON is HTML-escaped: %s", canonical)
	}
	// Pin the exact bytes, since this string is what gets hashed.
	if want := `{"decision":"a < b && c > d"}`; canonical != want {
		t.Errorf("canonical = %s, want %s", canonical, want)
	}
}

func TestContextAppendRejectsANonObjectBody(t *testing.T) {
	events, failed := dispatch(t, `{"v":"happi/1.3","id":"c","cmd":"context.append",
		"args":["\"just a string\""]}`)
	if !failed {
		t.Error("a non-object decision body must be rejected")
	}
	if code := lastEvent(events)["code"]; code != "parse_error" {
		t.Errorf("code = %v, want parse_error", code)
	}
}
