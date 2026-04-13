package relate

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// Engine detects relationships between claims using token-overlap heuristics.
type Engine struct {
	now    func() time.Time
	nextID func() (string, error)
}

// NewEngine returns an Engine with default clock and ID generation.
func NewEngine() Engine {
	return Engine{
		now:    time.Now,
		nextID: newRelationshipID,
	}
}

// stopWords are common English words filtered before computing token overlap
// to avoid false-positive relationships on trivial shared words.
var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "shall": {},
	"should": {}, "may": {}, "might": {}, "must": {}, "can": {}, "could": {},
	"to": {}, "of": {}, "in": {}, "for": {}, "on": {}, "with": {}, "at": {},
	"by": {}, "from": {}, "as": {}, "into": {}, "through": {}, "during": {},
	"before": {}, "after": {}, "above": {}, "below": {}, "between": {},
	"and": {}, "but": {}, "or": {}, "nor": {}, "so": {}, "yet": {},
	"it": {}, "its": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"i": {}, "we": {}, "you": {}, "he": {}, "she": {}, "they": {}, "me": {},
	"us": {}, "him": {}, "her": {}, "them": {}, "my": {}, "our": {}, "your": {},
	"his": {}, "their": {},
}

// negationWords are used for polarity detection.
var negationWords = map[string]struct{}{
	"not": {}, "no": {}, "never": {}, "without": {}, "cannot": {},
	"cant": {}, "can't": {}, "dont": {}, "don't": {}, "doesnt": {}, "doesn't": {},
	"didnt": {}, "didn't": {}, "isnt": {}, "isn't": {}, "arent": {}, "aren't": {},
	"wasnt": {}, "wasn't": {}, "werent": {}, "weren't": {}, "wont": {}, "won't": {},
	"wouldnt": {}, "wouldn't": {}, "shouldnt": {}, "shouldn't": {},
	"impossible": {}, "unlikely": {}, "disagree": {}, "rejected": {}, "declined": {},
	"failed": {},
}

// minContentTokenOverlap is the minimum number of content (non-stop) tokens
// that must overlap for two claims to be considered related.
const minContentTokenOverlap = 2

// minOverlapRatio is the minimum fraction of the shorter claim's content tokens
// that must overlap with the longer claim for a relationship to be inferred.
const minOverlapRatio = 0.3

// Detect compares all claim pairs and returns inferred relationships.
func (e Engine) Detect(claims []domain.Claim) ([]domain.Relationship, error) {
	rels := make([]domain.Relationship, 0)
	now := e.now().UTC()

	// Pre-compute normalized content tokens and polarity for each claim.
	type analyzed struct {
		tokens map[string]struct{}
		neg    bool
	}
	cache := make([]analyzed, len(claims))
	for i := range claims {
		tokens, neg := contentTokensAndPolarity(claims[i].Text)
		cache[i] = analyzed{tokens: tokens, neg: neg}
	}

	for i := 0; i < len(claims); i++ {
		if len(cache[i].tokens) == 0 {
			continue
		}
		for j := i + 1; j < len(claims); j++ {
			if len(cache[j].tokens) == 0 {
				continue
			}

			relType, ok := inferRelationship(cache[i].tokens, cache[i].neg, cache[j].tokens, cache[j].neg)
			if !ok {
				continue
			}

			id, err := e.nextID()
			if err != nil {
				return nil, err
			}

			rels = append(rels, domain.Relationship{
				ID:          id,
				Type:        relType,
				FromClaimID: claims[i].ID,
				ToClaimID:   claims[j].ID,
				CreatedAt:   now,
			})
		}
	}

	return rels, nil
}

// contentTokensAndPolarity splits text into content tokens (stop words removed)
// and detects whether the text contains negation.
func contentTokensAndPolarity(text string) (map[string]struct{}, bool) {
	words := strings.Fields(strings.ToLower(text))
	tokens := make(map[string]struct{}, len(words))
	neg := false
	for _, w := range words {
		w = strings.Trim(w, ",.;:!?()[]{}\"'")
		if w == "" {
			continue
		}
		if _, ok := negationWords[w]; ok {
			neg = true
			continue
		}
		if _, ok := stopWords[w]; ok {
			continue
		}
		tokens[stemWord(w)] = struct{}{}
	}
	return tokens, neg
}

// stemWord applies minimal suffix stripping to reduce inflection noise.
func stemWord(word string) string {
	if len(word) > 5 && strings.HasSuffix(word, "ed") {
		return strings.TrimSuffix(word, "ed")
	}
	if len(word) > 5 && strings.HasSuffix(word, "es") {
		return strings.TrimSuffix(word, "es")
	}
	if len(word) > 4 && strings.HasSuffix(word, "s") {
		return strings.TrimSuffix(word, "s")
	}
	return word
}

func inferRelationship(aTokens map[string]struct{}, aNeg bool, bTokens map[string]struct{}, bNeg bool) (domain.RelationshipType, bool) {
	overlap := contentOverlap(aTokens, bTokens)
	if overlap < minContentTokenOverlap {
		return "", false
	}

	// Check overlap ratio against the shorter claim's token count.
	shorter := len(aTokens)
	if len(bTokens) < shorter {
		shorter = len(bTokens)
	}
	if shorter == 0 {
		return "", false
	}
	ratio := float64(overlap) / float64(shorter)
	if ratio < minOverlapRatio {
		return "", false
	}

	if aNeg != bNeg {
		return domain.RelationshipTypeContradicts, true
	}
	return domain.RelationshipTypeSupports, true
}

func contentOverlap(a, b map[string]struct{}) int {
	count := 0
	for token := range a {
		if _, ok := b[token]; ok {
			count++
		}
	}
	return count
}

func newRelationshipID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "rl_" + hex.EncodeToString(buf), nil
}
