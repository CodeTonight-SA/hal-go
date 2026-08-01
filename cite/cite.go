// Package cite implements the HAPPI/1.3 cite.verify surface: deterministic
// citation provenance.
//
// A cited quote is verbatim-present in its source, or it is not — a
// deterministic string match, not a judgement. A fabricated quote can never
// earn StatusVerified or StatusFuzzy; it resolves to StatusNotFound.
//
// This package builds the envelope and decodes the provenance record the
// runtime returns in completed.usage. The verification itself happens in the
// runtime, never here — a client that graded its own citations would be
// marking its own homework.
//
// What it proves: the quote is verbatim in the SUPPLIED source. What it does
// NOT prove: that the source is authentic, or that the quote supports the
// claim. Those stay the caller's responsibility.
package cite

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/CodeTonight-SA/hal-go/envelope"
)

// Cmd is the runtime command this package targets.
const Cmd = "cite.verify"

// Citation statuses.
const (
	// StatusVerified — quote located by exact substring match.
	StatusVerified = "verified"
	// StatusFuzzy — located after length-preserving normalisation of
	// whitespace and typographic variants (unicode dashes, smart quotes, nbsp).
	StatusFuzzy = "fuzzy"
	// StatusNotFound — absent from its source. Fabricated quotes land here.
	StatusNotFound = "not_found"
)

// ErrNoSources and ErrNoCitations guard the two required inputs.
var (
	ErrNoSources   = errors.New("cite: at least one source is required")
	ErrNoCitations = errors.New("cite: at least one citation is required")
)

// Source is a document a citation may quote.
type Source struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Citation is a claim's exact quoted words and which source they come from.
type Citation struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Quote    string `json:"quote"`
}

// Result is the per-citation verdict. Start/End are -1 when not found.
type Result struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Status   string `json:"status"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

// Grounded reports whether the quote was located (exact or normalised).
func (r Result) Grounded() bool {
	return r.Status == StatusVerified || r.Status == StatusFuzzy
}

// SourceDigest is the per-source hash the runtime reports.
type SourceDigest struct {
	SHA256 string `json:"sha256"`
	Chars  int    `json:"chars"`
}

// Provenance is the record returned in completed.usage. The per-source
// sha256 plus per-citation offsets are exactly what an IDR or memory-chain
// leaf needs to sign.
type Provenance struct {
	Sources       map[string]SourceDigest `json:"sources"`
	Citations     []Result                `json:"citations"`
	Tally         map[string]int          `json:"tally"`
	GroundingRate float64                 `json:"grounding_rate"`
}

// NotFound returns the citations absent from their source.
func (p Provenance) NotFound() []Result {
	return p.filter(func(r Result) bool { return r.Status == StatusNotFound })
}

// Verified returns the citations located by exact match.
func (p Provenance) Verified() []Result {
	return p.filter(func(r Result) bool { return r.Status == StatusVerified })
}

func (p Provenance) filter(keep func(Result) bool) []Result {
	var out []Result
	for _, r := range p.Citations {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// FullyGrounded reports whether every citation was located in its source.
// False for an empty record: nothing was proven, so nothing is grounded.
func (p Provenance) FullyGrounded() bool {
	return len(p.Citations) > 0 && len(p.NotFound()) == 0
}

// New builds a cite.verify envelope.
//
// With strict set, the runtime emits an error event (and exits non-zero) if
// ANY citation is not found — the build-gate form. Otherwise it emits
// completed and the caller reads GroundingRate.
func New(id string, sources []Source, citations []Citation, strict bool) (envelope.Envelope, error) {
	if len(sources) == 0 {
		return envelope.Envelope{}, ErrNoSources
	}
	if len(citations) == 0 {
		return envelope.Envelope{}, ErrNoCitations
	}
	flags := map[string]any{
		"sources":   sources,
		"citations": citations,
	}
	if strict {
		flags["strict"] = true
	}
	env := envelope.New(id, Cmd)
	env.Flags = flags
	return env, nil
}

// DecodeProvenance parses the provenance record out of a completed event's
// usage field. It rejects an unrecognised citation status rather than
// silently treating it as ungrounded.
func DecodeProvenance(usage map[string]any) (Provenance, error) {
	var p Provenance
	if usage == nil {
		return p, errors.New("cite: completed event carried no usage record")
	}
	raw, err := json.Marshal(usage)
	if err != nil {
		return p, fmt.Errorf("cite: re-encoding usage: %w", err)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("cite: decoding provenance: %w", err)
	}
	for _, c := range p.Citations {
		switch c.Status {
		case StatusVerified, StatusFuzzy, StatusNotFound:
		default:
			return Provenance{}, fmt.Errorf("cite: unknown citation status %q", c.Status)
		}
	}
	return p, nil
}
