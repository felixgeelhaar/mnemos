package relate

import (
	"strings"
	"unicode"
)

// detectEntityRoleDivergence flags two claims as contradicting when
// they share a role/subject token but assign it to different
// proper-noun-shaped values.
//
// Example: "The CEO is Alice" vs "The CEO is Bob" share `ceo` but
// disagree on the entity (Alice vs Bob). The token-overlap path
// rejects these as too short for value-divergence; this path catches
// them.
//
// Heuristic:
//   - Both claims must, after stop-word removal, fit in ≤4 content
//     tokens (the heuristic targets short copular statements; longer
//     claims are handled by detectValueDivergence and the polarity
//     path).
//   - They must share ≥1 content token (the role/subject anchor).
//   - Each must have ≥1 token that the other doesn't, and at least
//     one of those differing tokens must be a proper-noun-shaped
//     literal in the original text (capitalized, alphabetic).
//
// Out of scope: cross-language proper-noun resolution, alias
// reconciliation ("Alice Smith" vs "Alice"), pronoun resolution.
func detectEntityRoleDivergence(aText, bText string, aTokens, bTokens map[string]struct{}) bool {
	la, lb := len(aTokens), len(bTokens)
	if la == 0 || lb == 0 {
		return false
	}
	// Restrict the shape to very short copular claims (e.g. "X is Y").
	// Anything longer is covered by detectValueDivergence and the
	// polarity path; relaxing this caused false positives like
	// "Python is used for data science" vs "Go is used for backend
	// services" being flagged as conflicts because both contained
	// proper-noun-shaped tokens.
	if la > 3 || lb > 3 {
		return false
	}

	overlap := contentOverlap(aTokens, bTokens)
	if overlap < 1 {
		return false
	}
	onlyA := la - overlap
	onlyB := lb - overlap
	if onlyA < 1 || onlyB < 1 {
		return false
	}

	aProperNouns := properNounTokens(aText)
	bProperNouns := properNounTokens(bText)
	if len(aProperNouns) == 0 && len(bProperNouns) == 0 {
		// No capitalized non-leading tokens on either side — likely
		// not a named-entity claim. Fall back to other detectors.
		return false
	}

	// At least one of the differing-tokens-on-each-side must be a
	// proper noun (capitalized in the source) — otherwise we'd flag
	// "the cat is small" vs "the cat is fast" as a contradiction,
	// which is a value-divergence call, not an entity one.
	if !hasProperNounDivergence(aProperNouns, bProperNouns) {
		return false
	}

	return true
}

// properNounTokens returns the lowercased + stemmed forms of tokens
// that look like proper nouns in the source text. Heuristic:
//   - capitalized first letter, alphabetic only
//   - not a stop-word once lowercased (this also handles
//     sentence-initial "The"/"A" without dropping legitimate
//     sentence-initial proper nouns like "Alice")
func properNounTokens(text string) map[string]struct{} {
	out := map[string]struct{}{}
	words := strings.Fields(text)
	for _, w := range words {
		trimmed := strings.Trim(w, ",.;:!?()[]{}\"'")
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if !unicode.IsUpper(runes[0]) {
			continue
		}
		// All remaining runes must be letters; mixed alpha-numeric
		// like "API2" is more likely a code identifier than a
		// proper noun.
		alpha := true
		for _, r := range runes[1:] {
			if !unicode.IsLetter(r) {
				alpha = false
				break
			}
		}
		if !alpha {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, isStop := stopWords[lower]; isStop {
			continue
		}
		out[stemWord(lower)] = struct{}{}
	}
	return out
}

// hasProperNounDivergence reports whether the two proper-noun token
// sets disagree — i.e. each contains at least one token the other
// doesn't.
func hasProperNounDivergence(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	uniqueA := false
	for tok := range a {
		if _, ok := b[tok]; !ok {
			uniqueA = true
			break
		}
	}
	uniqueB := false
	for tok := range b {
		if _, ok := a[tok]; !ok {
			uniqueB = true
			break
		}
	}
	return uniqueA && uniqueB
}
