package extract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felixgeelhaar/fortify/retry"
	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/llm"
)

// LLMEngine extracts claims using an LLM provider. It falls back to the
// rule-based Engine if the LLM call fails.
type LLMEngine struct {
	client   llm.Client
	fallback Engine
	now      func() time.Time
	nextID   func() (string, error)
	cacheDir string
}

// NewLLMEngine creates an LLM-powered extraction engine with rule-based
// fallback.
func NewLLMEngine(client llm.Client) LLMEngine {
	return LLMEngine{
		client:   client,
		fallback: NewEngine(),
		now:      time.Now,
		nextID:   newClaimID,
		cacheDir: filepath.Join("data", "cache", "llm-extraction"),
	}
}

// llmClaim is the JSON structure returned by the LLM.
type llmClaim struct {
	Text       string  `json:"text"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

// Extract processes events through the LLM to extract claims and evidence
// links. Falls back to rule-based extraction on LLM failure.
func (e LLMEngine) Extract(events []domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error) {
	// Collect non-empty event texts.
	var texts []string
	var sourceEvents []domain.Event
	for _, ev := range events {
		content := strings.TrimSpace(ev.Content)
		if content == "" {
			continue
		}
		texts = append(texts, content)
		sourceEvents = append(sourceEvents, ev)
	}

	if len(texts) == 0 {
		return nil, nil, nil
	}

	// Call the LLM.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: buildExtractionPrompt(texts)},
	}

	cacheKey := e.cacheKey(texts)
	if rawClaims, ok := e.loadCachedClaims(cacheKey); ok {
		return e.buildClaims(rawClaims, sourceEvents)
	}

	retrier := retry.New[llm.Response](retry.Config{
		MaxAttempts:   3,
		InitialDelay:  200 * time.Millisecond,
		MaxDelay:      time.Second,
		BackoffPolicy: retry.BackoffExponential,
		Jitter:        true,
		Logger:        slog.New(slog.NewJSONHandler(os.Stderr, nil)),
	})

	resp, err := retrier.Do(ctx, func(ctx context.Context) (llm.Response, error) {
		return e.client.Complete(ctx, messages)
	})
	if err != nil {
		// Fallback to rule-based extraction.
		return e.fallback.Extract(events)
	}

	rawClaims, err := parseLLMResponse(resp.Content)
	if err != nil {
		// Fallback on parse failure.
		return e.fallback.Extract(events)
	}

	if len(rawClaims) == 0 {
		e.storeCachedClaims(cacheKey, rawClaims)
		return nil, nil, nil
	}
	if cacheKey != "" {
		e.storeCachedClaims(cacheKey, rawClaims)
	}

	return e.buildClaims(rawClaims, sourceEvents)
}

// Convert LLM output to domain claims.

func (e LLMEngine) buildClaims(rawClaims []llmClaim, sourceEvents []domain.Event) ([]domain.Claim, []domain.ClaimEvidence, error) {
	claims := make([]domain.Claim, 0, len(rawClaims))
	evidence := make([]domain.ClaimEvidence, 0, len(rawClaims))
	seen := map[string]struct{}{}

	for _, rc := range rawClaims {
		text := strings.TrimSpace(rc.Text)
		if text == "" {
			continue
		}

		dedupeKey := normalizeForDedupe(text)
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

		claimType := parseLLMClaimType(rc.Type)
		confidence := clamp(rc.Confidence, 0.5, 0.95)

		claim := domain.Claim{
			ID:         claimID,
			Text:       text,
			Type:       claimType,
			Confidence: confidence,
			Status:     domain.ClaimStatusActive,
			CreatedAt:  e.now().UTC(),
		}
		if err := claim.Validate(); err != nil {
			continue // Skip invalid claims from LLM.
		}

		// Link claim to the best-matching source event.
		bestEvent := matchEventForClaim(text, sourceEvents)
		ce := domain.ClaimEvidence{ClaimID: claim.ID, EventID: bestEvent.ID}
		if err := ce.Validate(); err != nil {
			continue
		}

		claims = append(claims, claim)
		evidence = append(evidence, ce)
	}

	// Run contested detection on the final claim set.
	markContestedClaims(claims)

	return claims, evidence, nil
}

func (e LLMEngine) cacheKey(texts []string) string {
	if e.cacheDir == "" {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte(PromptVersion))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(strings.TrimSpace(os.Getenv("MNEMOS_LLM_PROVIDER"))))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(strings.TrimSpace(os.Getenv("MNEMOS_LLM_MODEL"))))
	for _, text := range texts {
		_, _ = h.Write([]byte("\n"))
		_, _ = h.Write([]byte(text))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (e LLMEngine) loadCachedClaims(key string) ([]llmClaim, bool) {
	if key == "" || e.cacheDir == "" {
		return nil, false
	}
	path := filepath.Join(e.cacheDir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var claims []llmClaim
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, false
	}
	return claims, true
}

func (e LLMEngine) storeCachedClaims(key string, claims []llmClaim) {
	if key == "" || e.cacheDir == "" {
		return
	}
	if err := os.MkdirAll(e.cacheDir, 0o750); err != nil {
		return
	}
	data, err := json.Marshal(claims)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(e.cacheDir, key+".json"), data, 0o600)
}

// ExtractClaims implements ports.ExtractionEngine.
func (e LLMEngine) ExtractClaims(events []domain.Event) ([]domain.Claim, error) {
	claims, _, err := e.Extract(events)
	return claims, err
}

// parseLLMResponse extracts the JSON claim array from the LLM response text.
func parseLLMResponse(content string) ([]llmClaim, error) {
	content = strings.TrimSpace(content)

	// Strip markdown fences if the LLM ignored our instruction.
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		// Remove first and last lines (fences).
		if len(lines) >= 3 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	content = strings.TrimSpace(content)

	var claims []llmClaim
	if err := json.Unmarshal([]byte(content), &claims); err != nil {
		return nil, fmt.Errorf("parse LLM claim JSON: %w", err)
	}
	return claims, nil
}

// parseLLMClaimType converts LLM string output to a domain ClaimType.
func parseLLMClaimType(raw string) domain.ClaimType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "decision":
		return domain.ClaimTypeDecision
	case "hypothesis":
		return domain.ClaimTypeHypothesis
	default:
		return domain.ClaimTypeFact
	}
}

// matchEventForClaim finds the event whose content best matches the claim text
// using token overlap. Falls back to the first event if no good match.
func matchEventForClaim(claimText string, events []domain.Event) domain.Event {
	if len(events) == 1 {
		return events[0]
	}

	claimNorm := normalizeForDedupe(claimText)
	best := events[0]
	bestScore := -1

	for _, ev := range events {
		evNorm := normalizeForDedupe(ev.Content)
		score := tokenOverlap(claimNorm, evNorm)
		if score > bestScore {
			bestScore = score
			best = ev
		}
	}

	return best
}
