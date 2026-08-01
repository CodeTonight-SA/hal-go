package cite

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// VerifyQuote locates quote inside sourceText and reports how it was found.
//
// The ladder is deterministic and contains no judgement of any kind: an exact
// substring search, then a whitespace- and typography-tolerant search, then
// not found. That determinism IS the guarantee the whole command exists to
// provide — a quote that is not in the source cannot come back verified,
// because nothing in this path is capable of deciding it is "close enough".
//
// Offsets are RUNE offsets, not byte offsets. This is a deliberate departure
// from the usual Go convention and it exists for interoperability: the
// reference runtime and hal-py both index by code point, so returning byte
// offsets here would make the three implementations disagree on the numbers
// for any source containing a non-ASCII character — and disagree silently,
// since both answers look perfectly plausible in isolation.
//
// It also keeps the tolerant pass honest. fold below rewrites multi-byte
// typographic characters to single-byte ASCII, which SHORTENS the string in
// bytes while leaving the rune count untouched. Offsets taken from the folded
// string are therefore only meaningful once converted to rune counts; used
// directly as byte offsets they would point into the wrong place in the
// caller's original text, shifted by however many typographic characters
// happened to precede the match.
func VerifyQuote(quote, sourceText string) (status string, start, end int) {
	needle := strings.TrimSpace(quote)
	if needle == "" {
		return StatusNotFound, -1, -1
	}
	if at := strings.Index(sourceText, needle); at >= 0 {
		startRune := utf8.RuneCountInString(sourceText[:at])
		return StatusVerified, startRune, startRune + utf8.RuneCountInString(needle)
	}
	pattern := flexiblePattern(needle)
	if pattern == nil {
		return StatusNotFound, -1, -1
	}
	folded := fold(sourceText)
	loc := pattern.FindStringIndex(folded)
	if loc == nil {
		return StatusNotFound, -1, -1
	}
	// fold maps one rune to one rune, so a rune index into the folded string
	// is the same rune index into the original.
	return StatusFuzzy,
		utf8.RuneCountInString(folded[:loc[0]]),
		utf8.RuneCountInString(folded[:loc[1]])
}

// flexiblePattern builds a regexp matching the quote with any run of
// whitespace between its words. Returns nil if the quote has no word content.
func flexiblePattern(needle string) *regexp.Regexp {
	words := strings.Fields(fold(needle))
	if len(words) == 0 {
		return nil
	}
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, regexp.QuoteMeta(w))
	}
	compiled, err := regexp.Compile(strings.Join(quoted, `\s+`))
	if err != nil {
		return nil
	}
	return compiled
}

// typography maps characters that differ only in presentation onto a common
// form: the several dash and quote characters editors substitute, and the
// several non-breaking or thin spaces.
//
// Each entry maps ONE rune to ONE rune. That is what VerifyQuote relies on
// when it converts a folded-string offset back to a position in the caller's
// text: the rune COUNT is preserved even though the byte length is not. An
// entry that expanded one rune into several would break that conversion.
var typography = map[rune]rune{
	'‐': '-', '‑': '-', '‒': '-',
	'–': '-', '—': '-', '―': '-',
	'‘': '\'', '’': '\'', '‛': '\'',
	'“': '"', '”': '"', '‟': '"',
	' ': ' ', ' ': ' ', ' ': ' ', ' ': ' ',
}

// fold applies the typographic normalisation above.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		if replacement, ok := typography[r]; ok {
			return replacement
		}
		return r
	}, s)
}
