// Package envelope implements the HAPPI/1.3 envelope: the JSON object
// passed on stdin to a HAPPI runtime.
//
// HAPPI spec: a HAPPI envelope has five required+optional fields:
//
//	v     (required, any happi/1.x — see happi.IsAccepted)
//	id    (required, caller-assigned, opaque, unique per call)
//	cmd   (required, dot-path)
//	args  (optional, array of arbitrary values)
//	flags (optional, named values)
//	auth  (optional, credentials — scrubbed from logs)
//
// Forward-compatible: unknown fields are forwarded unchanged via the
// Extra map. This package does not enforce semantics beyond schema —
// it only validates structure, version, and required fields.
//
// Reference: happi.md, axiom 4 (the polyglot form IS the spec).
package envelope

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/CodeTonight-SA/hal-go/happi"
)

// Version is the protocol version New stamps on envelopes it builds.
const Version = happi.Version

// KnownVersions are the protocol minors that existed when this library was
// written. Informational only: acceptance is decided by happi.IsAccepted,
// which admits any happi/1.x, so a future minor is not rejected merely for
// being absent here. See the happi package for why the policy is a
// major-version rule rather than a fixed enumeration.
var KnownVersions = happi.KnownVersions

// Envelope is a HAPPI/1.0 envelope. Marshal produces the canonical wire
// form; Unmarshal preserves unknown fields in Extra so they round-trip.
type Envelope struct {
	V     string         `json:"v"`
	ID    string         `json:"id"`
	Cmd   string         `json:"cmd"`
	Args  []any          `json:"args,omitempty"`
	Flags map[string]any `json:"flags,omitempty"`
	Auth  map[string]any `json:"auth,omitempty"`
	Extra map[string]any `json:"-"` // unknown fields forwarded unchanged
}

// New builds a minimal envelope with the canonical version.
func New(id, cmd string) Envelope {
	return Envelope{V: Version, ID: id, Cmd: cmd}
}

// Validate returns nil if the envelope satisfies the HAPPI schema.
// It checks: v is one of AcceptedVersions, id is a non-empty string, cmd is
// a non-empty string. Unknown fields are tolerated by design.
func (e Envelope) Validate() error {
	if !happi.IsAccepted(e.V) {
		return fmt.Errorf("envelope: unsupported version %q (accepted: %s)",
			e.V, happi.AcceptedList())
	}
	if e.ID == "" {
		return errors.New("envelope: missing or empty 'id'")
	}
	if e.Cmd == "" {
		return errors.New("envelope: missing or empty 'cmd'")
	}
	return nil
}

// MarshalJSON serialises the envelope, merging Extra fields back into
// the top-level object so unknown fields round-trip unchanged.
func (e Envelope) MarshalJSON() ([]byte, error) {
	out := map[string]any{"v": e.V, "id": e.ID, "cmd": e.Cmd}
	if len(e.Args) > 0 {
		out["args"] = e.Args
	}
	if len(e.Flags) > 0 {
		out["flags"] = e.Flags
	}
	if len(e.Auth) > 0 {
		out["auth"] = e.Auth
	}
	for k, v := range e.Extra {
		if _, taken := out[k]; !taken {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses a HAPPI envelope, extracting known fields and
// preserving unknown fields in Extra.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("envelope: invalid JSON: %w", err)
	}
	known := map[string]bool{"v": true, "id": true, "cmd": true, "args": true, "flags": true, "auth": true}
	if err := decodeKnown(raw, e); err != nil {
		return err
	}
	e.Extra = collectExtra(raw, known)
	return nil
}

// decodeKnown unmarshals the six known fields. Split out to keep
// UnmarshalJSON's cyclomatic complexity below the 15 ceiling.
func decodeKnown(raw map[string]json.RawMessage, e *Envelope) error {
	type assign struct {
		key string
		dst any
	}
	for _, a := range []assign{
		{"v", &e.V}, {"id", &e.ID}, {"cmd", &e.Cmd},
		{"args", &e.Args}, {"flags", &e.Flags}, {"auth", &e.Auth},
	} {
		if msg, ok := raw[a.key]; ok {
			if err := json.Unmarshal(msg, a.dst); err != nil {
				return fmt.Errorf("envelope: field %q: %w", a.key, err)
			}
		}
	}
	return nil
}

// collectExtra builds the Extra map from raw fields not in known.
func collectExtra(raw map[string]json.RawMessage, known map[string]bool) map[string]any {
	var extra map[string]any
	for k, msg := range raw {
		if known[k] {
			continue
		}
		var v any
		if err := json.Unmarshal(msg, &v); err == nil {
			if extra == nil {
				extra = map[string]any{}
			}
			extra[k] = v
		}
	}
	return extra
}
