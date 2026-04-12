package extract

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type Engine struct {
	now    func() time.Time
	nextID func() (string, error)
}

var sentenceSplitRE = regexp.MustCompile(`[.!?\n]+`)

func NewEngine() Engine {
	return Engine{
		now:    time.Now,
		nextID: newClaimID,
	}
}

func (e Engine) Extract(events []domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error) {
	claims := make([]domain.Claim, 0, len(events))
	evidence := make([]domain.ClaimEvidence, 0, len(events))
	seen := map[string]struct{}{}

	for _, event := range events {
		content := strings.TrimSpace(event.Content)
		if content == "" {
			continue
		}

		candidates := splitCandidates(content)
		for _, candidate := range candidates {
			dedupeKey := normalizeForDedupe(candidate)
			if dedupeKey == "" {
				continue
			}
			if _, ok := seen[dedupeKey]; ok {
				continue
			}
			seen[dedupeKey] = struct{}{}

			claimID, err := e.nextID()
			if err != nil {
				return nil, nil, fmt.Errorf("generate claim id: %w", err)
			}

			claimType := inferClaimType(candidate)
			claim := domain.Claim{
				ID:         claimID,
				Text:       candidate,
				Type:       claimType,
				Confidence: inferConfidence(candidate, claimType),
				Status:     domain.ClaimStatusActive,
				CreatedAt:  e.now().UTC(),
			}
			if err := claim.Validate(); err != nil {
				return nil, nil, fmt.Errorf("validate extracted claim: %w", err)
			}

			ce := domain.ClaimEvidence{ClaimID: claim.ID, EventID: event.ID}
			if err := ce.Validate(); err != nil {
				return nil, nil, fmt.Errorf("validate claim evidence: %w", err)
			}

			claims = append(claims, claim)
			evidence = append(evidence, ce)
		}
	}

	markContestedClaims(claims)

	return claims, evidence, nil
}

func (e Engine) ExtractClaims(events []domain.Event) ([]domain.Claim, error) {
	claims, _, err := e.Extract(events)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func inferClaimType(text string) domain.ClaimType {
	lower := strings.ToLower(text)
	decisionSignals := []string{"decide", "decision", "we will", "approved", "chosen", "plan is", "we should"}
	hypothesisSignals := []string{"might", "maybe", "hypothesis", "assume", "could", "possibly", "likely", "expected"}

	decisionScore := scoreSignals(lower, decisionSignals)
	hypothesisScore := scoreSignals(lower, hypothesisSignals)

	if decisionScore >= hypothesisScore && decisionScore > 0 {
		return domain.ClaimTypeDecision
	}
	if hypothesisScore > 0 {
		return domain.ClaimTypeHypothesis
	}
	return domain.ClaimTypeFact
}

func inferConfidence(text string, claimType domain.ClaimType) float64 {
	lower := strings.ToLower(text)
	value := 0.8
	switch claimType {
	case domain.ClaimTypeDecision:
		value = 0.88
	case domain.ClaimTypeHypothesis:
		value = 0.62
	}

	if hasAny(lower, []string{"approximately", "about", "around", "maybe", "might", "likely"}) {
		value -= 0.08
	}
	if hasDigits(lower) {
		value += 0.05
	}
	if hasAny(lower, []string{"confirmed", "observed", "measured", "reported"}) {
		value += 0.04
	}

	return clamp(value, 0.5, 0.95)
}

func splitCandidates(content string) []string {
	raw := sentenceSplitRE.Split(content, -1)
	out := make([]string, 0, len(raw))
	for _, piece := range raw {
		candidate := strings.TrimSpace(piece)
		if len(candidate) < 4 {
			continue
		}
		out = append(out, candidate)
	}
	if len(out) == 0 {
		fallback := strings.TrimSpace(content)
		if fallback != "" {
			return []string{fallback}
		}
	}
	return out
}

func normalizeForDedupe(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer(",", "", ".", "", ";", "", ":", "", "!", "", "?", "", "\t", " ")
	normalized = replacer.Replace(normalized)
	normalized = strings.Join(strings.Fields(normalized), " ")
	return normalized
}

func markContestedClaims(claims []domain.Claim) {
	cores := make([]string, len(claims))
	negs := make([]bool, len(claims))
	for i := range claims {
		core, neg := polarityCore(normalizeForDedupe(claims[i].Text))
		cores[i] = core
		negs[i] = neg
	}

	for i := 0; i < len(claims); i++ {
		if cores[i] == "" {
			continue
		}
		for j := i + 1; j < len(claims); j++ {
			if cores[j] == "" {
				continue
			}
			if negs[i] == negs[j] {
				continue
			}
			if tokenOverlap(cores[i], cores[j]) < 2 {
				continue
			}
			claims[i].Status = domain.ClaimStatusContested
			claims[j].Status = domain.ClaimStatusContested
		}
	}
}

func polarityCore(text string) (string, bool) {
	negWords := map[string]struct{}{"not": {}, "no": {}, "never": {}, "without": {}, "cannot": {}, "cant": {}}
	stopWords := map[string]struct{}{"did": {}, "does": {}, "do": {}, "is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {}, "the": {}, "a": {}, "an": {}}
	words := strings.Fields(text)
	core := make([]string, 0, len(words))
	neg := false
	for _, w := range words {
		if _, ok := negWords[w]; ok {
			neg = true
			continue
		}
		if w == "didnt" || w == "didn't" || w == "isnt" || w == "isn't" {
			neg = true
			continue
		}
		if _, ok := stopWords[w]; ok {
			continue
		}
		core = append(core, stemWord(w))
	}
	return strings.Join(core, " "), neg
}

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

func scoreSignals(text string, signals []string) int {
	score := 0
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			score++
		}
	}
	return score
}

func hasAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func hasDigits(text string) bool {
	for _, r := range text {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func clamp(value, minValue, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}

func tokenOverlap(a, b string) int {
	aSet := map[string]struct{}{}
	for _, token := range strings.Fields(a) {
		aSet[token] = struct{}{}
	}
	overlap := 0
	for _, token := range strings.Fields(b) {
		if _, ok := aSet[token]; ok {
			overlap++
		}
	}
	return overlap
}

func newClaimID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "cl_" + hex.EncodeToString(buf), nil
}
