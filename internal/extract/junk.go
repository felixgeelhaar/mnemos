package extract

import (
	"regexp"
	"strings"
	"unicode"
)

// isJunkClaim returns true for claim text that has no standalone value
// as a fact. Catches the common LLM-extraction pollution from chat
// transcripts (#23): greetings, list-headers ending in a colon, status
// emojis, and one-word acknowledgements that the model promotes to
// "Fact" claims.
//
// The bar is intentionally low: we only reject text that no reasonable
// reader would call a fact in isolation. Borderline cases (short but
// content-bearing claims) pass through.
func isJunkClaim(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}

	stripped := stripDecorations(t)
	if stripped == "" {
		// All content was emoji / punctuation / whitespace.
		return true
	}

	if greetingRE.MatchString(stripped) {
		return true
	}
	if ackRE.MatchString(stripped) {
		return true
	}
	if emojiAckRE.MatchString(t) {
		return true
	}
	if isSectionLabel(stripped) {
		return true
	}
	if contentWordCount(stripped) < 2 {
		return true
	}
	return false
}

// greetingRE matches pure greetings/sign-offs even when followed by a
// name or punctuation. Anchored so partial matches inside longer text
// don't trigger ("good morning meeting starts at 9" should pass).
var greetingRE = regexp.MustCompile(`(?i)^(good\s+(morning|afternoon|evening|night)|hi|hey|hello|cheers|thanks|thank\s+you|bye|goodbye|farewell|greetings)([\s,.!?-]+\w+)?[\s.!?]*$`)

// ackRE matches single-word acknowledgements with optional punctuation.
var ackRE = regexp.MustCompile(`(?i)^(done|ok|okay|yes|no|sure|noted|confirmed|got\s+it|copy(\s+that)?|roger|agreed)[\s.!?]*$`)

// emojiAckRE matches text that begins with a status emoji and contains
// only an emoji + optional acknowledgement word.
var emojiAckRE = regexp.MustCompile(`^[\p{So}\p{Sm}\p{Sk}]+\s*(done|ok|okay|yes|noted|confirmed|got\s+it)?[\s.!?]*$`)

// isSectionLabel catches list-headers and section titles that end with
// a colon and have no verb-like content. "So you need:" and
// "The event details are:" both match. "We decided to use Postgres:" does
// not (longer than the threshold and contains content).
func isSectionLabel(text string) bool {
	if !strings.HasSuffix(text, ":") {
		return false
	}
	body := strings.TrimRight(text, ":")
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}
	// A section label is short and has no factual payload — a bare
	// colon-suffixed phrase the model lifted from formatted text.
	return contentWordCount(body) <= 4
}

// stripDecorations removes leading/trailing emoji and punctuation so
// that "✅ Done" and "Done." normalize to the same shape before the
// greeting/ack regexes run.
func stripDecorations(text string) string {
	out := strings.Builder{}
	for _, r := range text {
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sm, r) || unicode.Is(unicode.Sk, r) {
			continue
		}
		out.WriteRune(r)
	}
	return strings.TrimSpace(out.String())
}
