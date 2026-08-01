package runtime

import (
	"strings"
	"testing"

	"github.com/CodeTonight-SA/hal-go/cite"
)

// cite.verify — the deterministic provenance floor.

const citeSource = "The Limitation Act 1980 bars the claim after six years."
const groundedQuote = "bars the claim after six years"

// Quotes that are NOT present in citeSource, in several shapes: a changed
// number, a reversed meaning, a changed year, and unrelated text. Each is the
// kind of thing a language model produces when it paraphrases a source and
// presents the result as a quotation.
var fabricatedQuotes = []string{
	"bars the claim after three years",
	"permits the claim after six years",
	"The Limitation Act 1990 bars the claim",
	"an entirely unrelated sentence",
}

func citeEnvelope(quote string, strict bool) string {
	strictFlag := ""
	if strict {
		strictFlag = `,"strict":true`
	}
	return `{"v":"happi/1.3","id":"cv","cmd":"cite.verify","flags":{
		"sources":[{"id":"s1","text":"` + citeSource + `"}],
		"citations":[{"id":"c1","source_id":"s1","quote":"` + quote + `"}]` +
		strictFlag + `}}`
}

// firstCitation pulls the single citation record out of a cite.verify result.
func firstCitation(t *testing.T, events []map[string]any) map[string]any {
	t.Helper()
	usage, ok := lastEvent(events)["usage"].(map[string]any)
	if !ok {
		t.Fatalf("last event carries no usage: %v", lastEvent(events))
	}
	return usage["citations"].([]any)[0].(map[string]any)
}

func TestAVerbatimQuoteIsVerifiedWithUsableOffsets(t *testing.T) {
	events, failed := dispatch(t, citeEnvelope(groundedQuote, false))
	if failed {
		t.Fatal("a grounded citation must not fail")
	}
	citation := firstCitation(t, events)
	if citation["status"] != cite.StatusVerified {
		t.Fatalf("status = %v, want verified", citation["status"])
	}
	// The offsets must be usable: slicing the source with them returns the
	// quote. An offset that does not round-trip is worse than no offset,
	// because a caller will happily highlight the wrong span.
	start := int(citation["start"].(float64))
	end := int(citation["end"].(float64))
	if got := string([]rune(citeSource)[start:end]); got != groundedQuote {
		t.Errorf("offsets select %q, want the quote itself", got)
	}
}

func TestAFabricatedQuoteCanNeverBeVerified(t *testing.T) {
	// The entire guarantee of cite.verify. A caller uses this to decide
	// whether a claim is grounded, so a false positive here is the one
	// failure that actually matters.
	forEach(t, fabricatedQuotes, func(t *testing.T, fabricated string) {
		events, _ := dispatch(t, citeEnvelope(fabricated, false))
		if status := firstCitation(t, events)["status"]; status != cite.StatusNotFound {
			t.Errorf("%q resolved to %v — a fabricated quote must be not_found",
				fabricated, status)
		}
	})
}

func TestStrictModeTurnsAnUnverifiedCitationIntoAFailedBuild(t *testing.T) {
	events, failed := dispatch(t, citeEnvelope("bars the claim after three years", true))
	if !failed {
		t.Fatal("strict mode must exit non-zero on an unproven citation")
	}
	last := lastEvent(events)
	if last["code"] != "runtime_error" {
		t.Errorf("code = %v, want runtime_error", last["code"])
	}
	if !strings.Contains(last["message"].(string), "NOT verbatim") {
		t.Errorf("message = %v, want it to name the problem", last["message"])
	}
}

func TestStrictModePassesWhenEverythingIsGrounded(t *testing.T) {
	if _, failed := dispatch(t, citeEnvelope(groundedQuote, true)); failed {
		t.Error("strict mode must succeed when every citation is grounded")
	}
}

func TestGroundingRateReportsTheProportionVerified(t *testing.T) {
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"cv","cmd":"cite.verify","flags":{
		"sources":[{"id":"s1","text":"`+citeSource+`"}],
		"citations":[
			{"id":"c1","source_id":"s1","quote":"bars the claim after six years"},
			{"id":"c2","source_id":"s1","quote":"bars the claim after three years"}]}}`)
	usage := lastEvent(events)["usage"].(map[string]any)
	if rate := usage["grounding_rate"].(float64); rate != 0.5 {
		t.Errorf("grounding_rate = %v, want 0.5", rate)
	}
	tally := usage["tally"].(map[string]any)
	if tally["verified"].(float64) != 1 || tally["not_found"].(float64) != 1 {
		t.Errorf("tally = %v, want 1 verified and 1 not_found", tally)
	}
}

func TestACitationNamingAnUnknownSourceIsNotFound(t *testing.T) {
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"cv","cmd":"cite.verify","flags":{
		"sources":[{"id":"s1","text":"`+citeSource+`"}],
		"citations":[{"id":"c1","source_id":"nope","quote":"x"}]}}`)
	if status := firstCitation(t, events)["status"]; status != cite.StatusNotFound {
		t.Errorf("status = %v, want not_found for an unknown source", status)
	}
}

func TestSourceDigestCountsCharactersNotBytes(t *testing.T) {
	// The reference and hal-py both report code points. A byte count would
	// differ for any non-ASCII source, and would differ silently.
	const text = "café — naïve"
	events, _ := dispatch(t, `{"v":"happi/1.3","id":"cv","cmd":"cite.verify","flags":{
		"sources":[{"id":"s1","text":"`+text+`"}],
		"citations":[{"id":"c1","source_id":"s1","quote":"café"}]}}`)
	usage := lastEvent(events)["usage"].(map[string]any)
	digest := usage["sources"].(map[string]any)["s1"].(map[string]any)
	if got := int(digest["chars"].(float64)); got != len([]rune(text)) {
		t.Errorf("chars = %d, want %d (runes, not bytes)", got, len([]rune(text)))
	}
}

// --- VerifyQuote as a pure function ----------------------------------------

func TestWhitespaceDifferencesMatchAsFuzzy(t *testing.T) {
	status, _, _ := cite.VerifyQuote("bars   the claim", "x bars the claim y")
	if status != cite.StatusFuzzy {
		t.Errorf("status = %v, want fuzzy", status)
	}
}

func TestFuzzyOffsetsIndexTheOriginalSource(t *testing.T) {
	// The typographic folding rewrites multi-byte characters to single-byte
	// ASCII, which shortens the string in BYTES while preserving the rune
	// count. Offsets are therefore rune offsets. If this ever regressed to
	// byte offsets, the returned span would silently drift by however many
	// typographic characters preceded the match — and only for text
	// containing them, which is the hardest kind of bug to notice.
	const source = "so it's well‐known now"
	status, start, end := cite.VerifyQuote("it’s well-known", source)
	if status != cite.StatusFuzzy {
		t.Fatalf("status = %v, want fuzzy", status)
	}
	if got := string([]rune(source)[start:end]); got != "it's well‐known" {
		t.Errorf("offsets select %q, want the matched span", got)
	}
}

func TestAnEmptyQuoteIsNotFoundRatherThanMatchingEverything(t *testing.T) {
	// An empty needle matches at position 0 in a naive substring search,
	// which would report an empty citation as verified — the worst possible
	// false positive for a provenance check.
	if status, _, _ := cite.VerifyQuote("", "any text at all"); status != cite.StatusNotFound {
		t.Errorf("empty quote = %v, want not_found", status)
	}
	if status, _, _ := cite.VerifyQuote("   ", "any text at all"); status != cite.StatusNotFound {
		t.Errorf("whitespace-only quote = %v, want not_found", status)
	}
}
