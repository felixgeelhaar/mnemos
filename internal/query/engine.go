package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/ports"
)

type eventLister interface {
	ports.EventRepository
	ListAll(ctx context.Context) ([]domain.Event, error)
	ListByRunID(ctx context.Context, runID string) ([]domain.Event, error)
}

// Engine answers natural-language questions by ranking events, resolving claims,
// and detecting contradictions.
type Engine struct {
	events        eventLister
	claims        ports.ClaimRepository
	relationships ports.RelationshipRepository
	embeddings    ports.EmbeddingRepository
	embedClient   embedding.Client
	llmClient     llm.Client
}

// NewEngine returns an Engine wired to the given event, claim, and relationship stores.
func NewEngine(events eventLister, claims ports.ClaimRepository, relationships ports.RelationshipRepository) Engine {
	return Engine{events: events, claims: claims, relationships: relationships}
}

// WithEmbeddings configures semantic search support on the engine.
// When both an embedding repository and client are set, queries use cosine
// similarity against stored event embeddings instead of token overlap.
func (e Engine) WithEmbeddings(repo ports.EmbeddingRepository, client embedding.Client) Engine {
	e.embeddings = repo
	e.embedClient = client
	return e
}

// WithLLM configures LLM-grounded answer generation. When set, the engine
// uses the LLM to synthesize answers from retrieved claims instead of using
// a fixed template. Falls back to template answers on LLM failure.
func (e Engine) WithLLM(client llm.Client) Engine {
	e.llmClient = client
	return e
}

// Answer searches all stored events for the best answer to the given question.
func (e Engine) Answer(question string) (domain.Answer, error) {
	ctx := context.Background()
	allEvents, err := e.events.ListAll(ctx)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load events for query: %w", err)
	}
	return e.answerWithEvents(ctx, question, allEvents)
}

// AnswerForRun searches events belonging to the specified run for the best answer.
func (e Engine) AnswerForRun(question, runID string) (domain.Answer, error) {
	ctx := context.Background()
	if strings.TrimSpace(runID) == "" {
		return domain.Answer{}, fmt.Errorf("run id is required")
	}
	events, err := e.events.ListByRunID(ctx, runID)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load events for run: %w", err)
	}
	if len(events) == 0 {
		return domain.Answer{AnswerText: fmt.Sprintf("No events found for run %q.", runID)}, nil
	}
	return e.answerWithEvents(ctx, question, events)
}

func (e Engine) answerWithEvents(ctx context.Context, question string, allEvents []domain.Event) (domain.Answer, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return domain.Answer{}, fmt.Errorf("query question is required")
	}
	if len(allEvents) == 0 {
		return domain.Answer{AnswerText: "No ingested events yet."}, nil
	}

	topEvents := e.rankEventsWithFallback(ctx, q, allEvents, 5)
	if len(topEvents) == 0 {
		return domain.Answer{
			AnswerText: fmt.Sprintf("I have %d events in the knowledge base, but none are relevant to %q. Try a different question or use --embed for semantic search.", len(allEvents), q),
		}, nil
	}
	eventIDs := make([]string, 0, len(topEvents))
	for _, event := range topEvents {
		eventIDs = append(eventIDs, event.ID)
	}

	claims, err := e.claims.ListByEventIDs(ctx, eventIDs)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load claims for query: %w", err)
	}

	// Re-rank claims by semantic similarity when embeddings are available.
	claims = e.rankClaimsByCosine(ctx, q, claims)

	// Boost claims matching the question's intent (e.g., "decisions" → decision type).
	claims = boostClaimsByQuestionIntent(q, claims)

	contradictions, err := collectContradictions(ctx, e.relationships, claims)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load contradictions for query: %w", err)
	}

	answerText := e.generateAnswer(ctx, q, claims, contradictions, len(topEvents))
	return domain.Answer{
		AnswerText:       answerText,
		Claims:           claims,
		Contradictions:   contradictions,
		TimelineEventIDs: eventIDs,
	}, nil
}

// rankEventsWithFallback tries cosine similarity first (if embeddings are available),
// then falls back to token-overlap ranking.
func (e Engine) rankEventsWithFallback(ctx context.Context, question string, events []domain.Event, limit int) []domain.Event {
	if e.embeddings != nil && e.embedClient != nil {
		result, err := e.rankEventsByCosine(ctx, question, events, limit)
		if err == nil && len(result) > 0 {
			return result
		}
		// Fall through to token overlap on error.
	}
	return rankEvents(question, events, limit)
}

// rankEventsByCosine embeds the question and ranks events by cosine similarity
// against their stored embeddings.
func (e Engine) rankEventsByCosine(ctx context.Context, question string, events []domain.Event, limit int) ([]domain.Event, error) {
	stored, err := e.embeddings.ListByEntityType(ctx, "event")
	if err != nil || len(stored) == 0 {
		return nil, fmt.Errorf("no embeddings available")
	}

	// Build lookup from entity_id → vector.
	vecByID := make(map[string][]float32, len(stored))
	for _, rec := range stored {
		vecByID[rec.EntityID] = rec.Vector
	}

	// Check that at least some of the candidate events have embeddings.
	hasAny := false
	for _, ev := range events {
		if _, ok := vecByID[ev.ID]; ok {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil, fmt.Errorf("no matching embeddings for candidate events")
	}

	// Embed the question.
	qVectors, err := e.embedClient.Embed(ctx, []string{question})
	if err != nil || len(qVectors) == 0 {
		return nil, fmt.Errorf("embed question: %w", err)
	}
	qVec := qVectors[0]

	type scored struct {
		event domain.Event
		score float32
	}
	scoredEvents := make([]scored, 0, len(events))
	for _, ev := range events {
		vec, ok := vecByID[ev.ID]
		if !ok {
			continue
		}
		sim, err := embedding.CosineSimilarity(qVec, vec)
		if err != nil {
			continue // dimension mismatch — skip this event
		}
		scoredEvents = append(scoredEvents, scored{event: ev, score: sim})
	}

	sort.Slice(scoredEvents, func(i, j int) bool {
		if scoredEvents[i].score == scoredEvents[j].score {
			return scoredEvents[i].event.Timestamp.After(scoredEvents[j].event.Timestamp)
		}
		return scoredEvents[i].score > scoredEvents[j].score
	})

	out := make([]domain.Event, 0, min(limit, len(scoredEvents)))
	for i := 0; i < len(scoredEvents) && i < limit; i++ {
		out = append(out, scoredEvents[i].event)
	}
	return out, nil
}

// rankClaimsByCosine reorders claims by cosine similarity to the question when
// claim embeddings and an embedding client are available. Returns the original
// order on any error or when embeddings are not configured.
func (e Engine) rankClaimsByCosine(ctx context.Context, question string, claims []domain.Claim) []domain.Claim {
	if len(claims) <= 1 || e.embeddings == nil || e.embedClient == nil {
		return claims
	}

	stored, err := e.embeddings.ListByEntityType(ctx, "claim")
	if err != nil || len(stored) == 0 {
		return claims
	}

	vecByID := make(map[string][]float32, len(stored))
	for _, rec := range stored {
		vecByID[rec.EntityID] = rec.Vector
	}

	// Check that at least some claims have embeddings.
	hasAny := false
	for _, cl := range claims {
		if _, ok := vecByID[cl.ID]; ok {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return claims
	}

	qVectors, err := e.embedClient.Embed(ctx, []string{question})
	if err != nil || len(qVectors) == 0 {
		return claims
	}
	qVec := qVectors[0]

	type scored struct {
		claim domain.Claim
		score float32
		idx   int // original index for stable ordering
	}
	scoredClaims := make([]scored, 0, len(claims))
	for i, cl := range claims {
		vec, ok := vecByID[cl.ID]
		if !ok {
			// Keep claims without embeddings at their original position with a low score.
			scoredClaims = append(scoredClaims, scored{claim: cl, score: -1, idx: i})
			continue
		}
		sim, err := embedding.CosineSimilarity(qVec, vec)
		if err != nil {
			scoredClaims = append(scoredClaims, scored{claim: cl, score: -1, idx: i})
			continue
		}
		scoredClaims = append(scoredClaims, scored{claim: cl, score: sim, idx: i})
	}

	sort.Slice(scoredClaims, func(i, j int) bool {
		if scoredClaims[i].score == scoredClaims[j].score {
			return scoredClaims[i].idx < scoredClaims[j].idx
		}
		return scoredClaims[i].score > scoredClaims[j].score
	})

	result := make([]domain.Claim, 0, len(scoredClaims))
	for _, sc := range scoredClaims {
		result = append(result, sc.claim)
	}
	return result
}

// inferQuestionIntent returns a preferred claim type based on question keywords,
// or empty string if no clear intent is detected.
func inferQuestionIntent(question string) domain.ClaimType {
	q := strings.ToLower(question)
	decisionWords := []string{"decision", "decide", "chose", "choose", "pick", "selected", "approve", "commit"}
	hypothesisWords := []string{"risk", "might", "could", "possibly", "hypothesis", "maybe", "uncertain", "assume"}
	factWords := []string{"what happened", "did we", "how many", "status", "metric", "measure"}

	for _, w := range decisionWords {
		if strings.Contains(q, w) {
			return domain.ClaimTypeDecision
		}
	}
	for _, w := range hypothesisWords {
		if strings.Contains(q, w) {
			return domain.ClaimTypeHypothesis
		}
	}
	for _, w := range factWords {
		if strings.Contains(q, w) {
			return domain.ClaimTypeFact
		}
	}
	return ""
}

// boostClaimsByQuestionIntent reorders claims so those matching the question's
// intent (decision/hypothesis/fact) appear first. Preserves relative order
// within each group.
func boostClaimsByQuestionIntent(question string, claims []domain.Claim) []domain.Claim {
	intent := inferQuestionIntent(question)
	if intent == "" || len(claims) <= 1 {
		return claims
	}

	matched := make([]domain.Claim, 0)
	other := make([]domain.Claim, 0)
	for _, c := range claims {
		if c.Type == intent {
			matched = append(matched, c)
		} else {
			other = append(other, c)
		}
	}
	if len(matched) == 0 {
		return claims
	}
	return append(matched, other...)
}

// BM25 parameters tuned for short-to-medium technical documents.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// docTokens returns all tokens (including duplicates) from text, normalized.
func docTokens(text string) []string {
	out := []string{}
	for _, token := range strings.Fields(strings.ToLower(text)) {
		token = strings.Trim(token, ",.;:!?()[]{}\"'")
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

// rankEvents scores events against the question using BM25, a standard
// information retrieval algorithm that accounts for term frequency,
// inverse document frequency, and document length normalization.
func rankEvents(question string, events []domain.Event, limit int) []domain.Event {
	if len(events) == 0 {
		return nil
	}

	qTokens := docTokens(question)
	if len(qTokens) == 0 {
		return nil
	}

	// Build document frequency for BM25 IDF.
	df := map[string]int{}
	docLens := make([]int, len(events))
	totalLen := 0
	for i, event := range events {
		tokens := docTokens(event.Content)
		docLens[i] = len(tokens)
		totalLen += len(tokens)
		seen := map[string]struct{}{}
		for _, t := range tokens {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	avgDocLen := float64(totalLen) / float64(len(events))
	n := float64(len(events))

	// Deduplicate query tokens (BM25 treats each query term once).
	qUnique := map[string]struct{}{}
	for _, t := range qTokens {
		qUnique[t] = struct{}{}
	}

	type scored struct {
		event domain.Event
		score float64
	}
	scoredEvents := make([]scored, 0, len(events))
	for i, event := range events {
		tokens := docTokens(event.Content)
		tf := map[string]int{}
		for _, t := range tokens {
			tf[t]++
		}

		s := 0.0
		docLen := float64(docLens[i])
		for qt := range qUnique {
			freq := tf[qt]
			if freq == 0 {
				continue
			}
			dfQT := df[qt]
			// BM25 IDF: log((N - df + 0.5) / (df + 0.5) + 1)
			idf := math.Log((n-float64(dfQT)+0.5)/(float64(dfQT)+0.5) + 1)
			numerator := float64(freq) * (bm25K1 + 1)
			denominator := float64(freq) + bm25K1*(1-bm25B+bm25B*docLen/avgDocLen)
			s += idf * numerator / denominator
		}

		if s > 0 {
			scoredEvents = append(scoredEvents, scored{event: event, score: s})
		}
	}

	if len(scoredEvents) == 0 {
		return nil
	}

	sort.Slice(scoredEvents, func(i, j int) bool {
		if scoredEvents[i].score == scoredEvents[j].score {
			return scoredEvents[i].event.Timestamp.After(scoredEvents[j].event.Timestamp)
		}
		return scoredEvents[i].score > scoredEvents[j].score
	})

	out := make([]domain.Event, 0, min(limit, len(scoredEvents)))
	for i := 0; i < len(scoredEvents) && i < limit; i++ {
		out = append(out, scoredEvents[i].event)
	}
	return out
}

func collectContradictions(ctx context.Context, repo ports.RelationshipRepository, claims []domain.Claim) ([]domain.Relationship, error) {
	seen := map[string]struct{}{}
	result := make([]domain.Relationship, 0)
	for _, claim := range claims {
		rels, err := repo.ListByClaim(ctx, claim.ID)
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			if rel.Type != domain.RelationshipTypeContradicts {
				continue
			}
			if _, ok := seen[rel.ID]; ok {
				continue
			}
			seen[rel.ID] = struct{}{}
			result = append(result, rel)
		}
	}
	return result, nil
}

// generateAnswer produces the answer text. When an LLM client is configured,
// it synthesizes a grounded answer from the retrieved claims. Falls back to
// the template-based answer on LLM failure or when no client is set.
func (e Engine) generateAnswer(ctx context.Context, question string, claims []domain.Claim, contradictions []domain.Relationship, eventCount int) string {
	if e.llmClient == nil || len(claims) == 0 {
		return buildAnswerText(question, claims, contradictions, eventCount)
	}

	answer, err := e.groundedAnswer(ctx, question, claims, contradictions)
	if err != nil {
		// Fall back to template on any LLM error.
		return buildAnswerText(question, claims, contradictions, eventCount)
	}
	return answer
}

const groundedSystemPrompt = `You are Mnemos, an evidence-backed knowledge engine. Answer the user's question using ONLY the provided claims as evidence.

Rules:
1. Cite claims by their number (e.g., [1], [2]) when referencing them.
2. If claims contradict each other, explicitly acknowledge the contradiction.
3. Do not add information not present in the claims.
4. Be concise — 2-4 sentences.
5. If the claims do not address the question, say so.`

func (e Engine) groundedAnswer(ctx context.Context, question string, claims []domain.Claim, contradictions []domain.Relationship) (string, error) {
	var b strings.Builder
	b.WriteString("Question: ")
	b.WriteString(question)
	b.WriteString("\n\nClaims:\n")
	for i, cl := range claims {
		fmt.Fprintf(&b, "[%d] %s (type: %s, confidence: %.2f, status: %s)\n", i+1, cl.Text, cl.Type, cl.Confidence, cl.Status)
	}
	if len(contradictions) > 0 {
		b.WriteString("\nContradictions:\n")
		for _, rel := range contradictions {
			fmt.Fprintf(&b, "- Claim %s contradicts claim %s\n", rel.FromClaimID, rel.ToClaimID)
		}
	}

	resp, err := e.llmClient.Complete(ctx, []llm.Message{
		{Role: llm.RoleSystem, Content: groundedSystemPrompt},
		{Role: llm.RoleUser, Content: b.String()},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

func buildAnswerText(question string, claims []domain.Claim, contradictions []domain.Relationship, eventCount int) string {
	if len(claims) == 0 {
		return fmt.Sprintf("I could not find claims yet for %q. Try running extract/relate first.", question)
	}

	parts := []string{}
	parts = append(parts, fmt.Sprintf("For %q, the strongest signal is: %s.", question, claims[0].Text))

	if len(claims) > 1 {
		parts = append(parts, fmt.Sprintf("Other relevant claim: %s.", claims[1].Text))
	}

	if len(contradictions) > 0 {
		parts = append(parts, fmt.Sprintf("I also found %d contradiction(s), so this topic is contested.", len(contradictions)))
	} else {
		parts = append(parts, "No contradictions were found in the current claim set.")
	}

	parts = append(parts, fmt.Sprintf("Context used %d event(s) and %d claim(s).", eventCount, len(claims)))
	return strings.Join(parts, " ")
}

func tokenSet(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	for _, token := range strings.Fields(strings.ToLower(text)) {
		token = strings.Trim(token, ",.;:!?()[]{}\"'")
		if token == "" {
			continue
		}
		tokens[token] = struct{}{}
	}
	return tokens
}
