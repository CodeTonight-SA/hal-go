package cite

import (
	"strings"
	"testing"
	"time"
)

// Adversarial input, raised by an external review council.

func TestAQuoteOfRegexMetacharactersIsMatchedLiterally(t *testing.T) {
	// The tolerant pass builds a regexp from the quote, so leaving the quote
	// unescaped would let a caller inject a pattern instead of searching for
	// the text they actually wrote.
	const source = "literal (a+)+$ here"
	status, start, end := VerifyQuote("(a+)+$", source)
	if status != StatusVerified {
		t.Fatalf("status = %v, want verified", status)
	}
	if got := string([]rune(source)[start:end]); got != "(a+)+$" {
		t.Errorf("offsets select %q, want the literal metacharacters", got)
	}
}

func TestMetacharacterQuoteAbsentFromSourceIsNotFound(t *testing.T) {
	// Escaping must not turn into matching nothing, nor into matching
	// everything.
	if status, _, _ := VerifyQuote(".*", "no wildcards here"); status != StatusNotFound {
		t.Errorf("status = %v, want not_found", status)
	}
}

func TestAPathologicalQuoteDoesNotHang(t *testing.T) {
	// Go's regexp is RE2 and cannot backtrack catastrophically, so this pins
	// the property rather than guarding against a known failure mode.
	quote := strings.Repeat("a ", 400)
	source := strings.Repeat("a ", 399) + "b"
	started := time.Now()
	status, _, _ := VerifyQuote(quote, source)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("tolerant pass took %v on adversarial input", elapsed)
	}
	if status != StatusNotFound {
		t.Errorf("status = %v, want not_found", status)
	}
}

func TestAnOverLongQuoteSkipsFuzzingButStillMatchesExactly(t *testing.T) {
	// Past MaxFlexibleTokens the tolerant pass is skipped, but the exact pass
	// still runs — so a genuinely present long quotation is still verified,
	// and an absent one errs toward not_found rather than a false verified.
	longQuote := strings.TrimSpace(strings.Repeat("word ", MaxFlexibleTokens+88))
	if status, _, _ := VerifyQuote(longQuote, "prefix "+longQuote+" suffix"); status != StatusVerified {
		t.Errorf("status = %v, want verified (the exact pass must still run)", status)
	}
	if status, _, _ := VerifyQuote(longQuote, "nothing like it here"); status != StatusNotFound {
		t.Errorf("status = %v, want not_found", status)
	}
}

func TestGoAndPythonAgreeOnTheTokenLimit(t *testing.T) {
	// hal-py uses the same limit. A quote that one runtime fuzzes and the
	// other refuses would make the two disagree about whether a claim is
	// grounded, which matters more than the specific number.
	if MaxFlexibleTokens != 512 {
		t.Errorf("MaxFlexibleTokens = %d, want 512 to match hal-py", MaxFlexibleTokens)
	}
}

func TestFuzzyOffsetsSurviveTypographicFolding(t *testing.T) {
	// The folding rewrites multi-byte characters to single-byte ASCII, which
	// shortens the string in BYTES while preserving the rune count. If the
	// offset conversion regressed to byte offsets, this span would drift.
	const source = "so it's well‐known now"
	status, start, end := VerifyQuote("it’s well-known", source)
	if status != StatusFuzzy {
		t.Fatalf("status = %v, want fuzzy", status)
	}
	if got := string([]rune(source)[start:end]); got != "it's well‐known" {
		t.Errorf("offsets select %q, want the matched span", got)
	}
}
