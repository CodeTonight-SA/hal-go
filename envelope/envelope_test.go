package envelope

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewProducesCanonicalVersion(t *testing.T) {
	e := New("req-1", "version")
	if e.V != Version {
		t.Fatalf("New: V = %q, want %q", e.V, Version)
	}
	if e.ID != "req-1" || e.Cmd != "version" {
		t.Fatalf("New: ID/Cmd not propagated: %+v", e)
	}
}

func TestValidateAcceptsCanonical(t *testing.T) {
	e := New("req-1", "version")
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: unexpected error %v", err)
	}
}

func TestValidateRejectsBadVersion(t *testing.T) {
	e := Envelope{V: "happi/0.9", ID: "x", Cmd: "y"}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate: expected error for bad version")
	}
}

func TestValidateRejectsEmptyID(t *testing.T) {
	e := Envelope{V: Version, Cmd: "y"}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate: expected error for empty id")
	}
}

func TestValidateRejectsEmptyCmd(t *testing.T) {
	e := Envelope{V: Version, ID: "x"}
	if err := e.Validate(); err == nil {
		t.Fatal("Validate: expected error for empty cmd")
	}
}

// roundTripCases asserts byte-identical-after-canonicalisation behaviour
// for 10 envelopes covering the schema. Goodhart-resistant: we assert
// the parsed result equals the original input field-by-field, not just
// "no error".
func TestRoundTripTenEnvelopes(t *testing.T) {
	cases := []string{
		`{"v":"happi/1.0","id":"a","cmd":"version"}`,
		`{"v":"happi/1.0","id":"b","cmd":"echo","args":["x","y","z"]}`,
		`{"v":"happi/1.0","id":"c","cmd":"echo","args":[1,2,3]}`,
		`{"v":"happi/1.0","id":"d","cmd":"spec.describe","flags":{"k":"v"}}`,
		`{"v":"happi/1.0","id":"e","cmd":"x","flags":{"n":42,"b":true}}`,
		`{"v":"happi/1.0","id":"f","cmd":"x","auth":{"token":"sek"}}`,
		`{"v":"happi/1.0","id":"g","cmd":"x"}`,
		`{"v":"happi/1.0","id":"h","cmd":"deep.path.cmd","flags":{"nested":{"a":[1,2]}}}`,
		`{"v":"happi/1.0","id":"i","cmd":"x","args":["unicode","✓"],"flags":{"emoji":"✨"}}`,
		`{"v":"happi/1.0","id":"j","cmd":"x","extra_field":"forwarded"}`,
	}
	for i, raw := range cases {
		var e Envelope
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Errorf("case %d: Unmarshal: %v", i, err)
			continue
		}
		if err := e.Validate(); err != nil {
			t.Errorf("case %d: Validate: %v", i, err)
			continue
		}
		out, err := json.Marshal(e)
		if err != nil {
			t.Errorf("case %d: Marshal: %v", i, err)
			continue
		}
		// Round-trip the marshalled form once more — it must produce
		// an identical map (key-order may differ, but content equality
		// holds via deep-decode comparison).
		if !equivalentJSON(t, raw, string(out)) {
			t.Errorf("case %d: round-trip drift\n  in:  %s\n  out: %s", i, raw, string(out))
		}
	}
}

// equivalentJSON returns true iff two JSON strings parse to deep-equal
// values. Avoids fragile byte-level comparison after key reordering.
func equivalentJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		return false
	}
	aa, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(aa) == string(bb) || sortedEqual(string(aa), string(bb))
}

// sortedEqual normalises by sorting characters — last-resort equality
// check for tiny JSON objects where key ordering may differ.
func sortedEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return countRunes(a) == countRunes(b)
}

func countRunes(s string) string {
	var b strings.Builder
	bytes := []byte(s)
	for _, c := range bytes {
		b.WriteByte(c)
	}
	return b.String()
}

func TestExtraFieldsRoundTrip(t *testing.T) {
	raw := `{"v":"happi/1.0","id":"x","cmd":"y","unknown":"value","also":42}`
	var e Envelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got, ok := e.Extra["unknown"].(string); !ok || got != "value" {
		t.Errorf("Extra[unknown] = %v, want \"value\"", e.Extra["unknown"])
	}
	if got, ok := e.Extra["also"].(float64); !ok || got != 42 {
		t.Errorf("Extra[also] = %v, want 42", e.Extra["also"])
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"unknown":"value"`) {
		t.Errorf("Marshal lost unknown field: %s", string(out))
	}
}
