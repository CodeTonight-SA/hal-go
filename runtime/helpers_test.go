package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Shared helpers for the runtime tests.
//
// Membership questions go through typeSet, which builds a map once per
// stream, rather than through a repeated linear scan. Order questions go
// through order, which builds a position map once. Both are O(n) per stream
// and answer in constant time thereafter, so a test that asks several
// questions about one stream does not rescan it for each.

// dispatch runs one envelope and returns the parsed events plus whether the
// stream ended in an error.
func dispatch(t *testing.T, envelopeJSON string) ([]map[string]any, bool) {
	t.Helper()
	var out bytes.Buffer
	err := Dispatch([]byte(envelopeJSON), &out)
	return parseEvents(t, out.String()), err != nil
}

func parseEvents(t *testing.T, stream string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("emitted a line that is not JSON: %q", line)
		}
		events = append(events, event)
	}
	return events
}

func typesOf(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e["type"].(string))
	}
	return out
}

// typeSet maps each event type present to how many times it occurs.
func typeSet(events []map[string]any) map[string]int {
	counts := make(map[string]int, len(events))
	for _, e := range events {
		counts[e["type"].(string)]++
	}
	return counts
}

// order maps each event type to the index of its FIRST occurrence, so
// ordering assertions are map lookups rather than repeated scans.
func order(events []map[string]any) map[string]int {
	positions := make(map[string]int, len(events))
	for i, e := range events {
		typ := e["type"].(string)
		if _, seen := positions[typ]; !seen {
			positions[typ] = i
		}
	}
	return positions
}

func hasType(events []map[string]any, want string) bool {
	return typeSet(events)[want] > 0
}

// countOutcomes reports how many completed/error events a stream carries.
func countOutcomes(events []map[string]any) int {
	counts := typeSet(events)
	return counts["completed"] + counts["error"]
}

// deltaTexts collects the text of every delta event, in order.
func deltaTexts(events []map[string]any) []string {
	var texts []string
	for _, e := range events {
		if e["type"] == "delta" {
			texts = append(texts, e["text"].(string))
		}
	}
	return texts
}

// lastEvent is the outcome (or terminator) of a stream.
func lastEvent(events []map[string]any) map[string]any {
	return events[len(events)-1]
}

func versionEnvelope(version string) string {
	return `{"v":"` + version + `","id":"v","cmd":"version"}`
}

// forEach runs check once per item. Iteration lives here so each test body
// states the property being checked rather than the mechanics of walking a
// table.
func forEach(t *testing.T, items []string, check func(t *testing.T, item string)) {
	t.Helper()
	for _, item := range items {
		check(t, item)
	}
}

// allCarryVersion reports whether every event was stamped with want.
func allCarryVersion(events []map[string]any, want string) bool {
	for _, e := range events {
		if e["v"] != want {
			return false
		}
	}
	return true
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
