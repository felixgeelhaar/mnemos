package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/ports"
)

type eventLister interface {
	ports.EventRepository
	ListAll() ([]domain.Event, error)
	ListByRunID(runID string) ([]domain.Event, error)
}

// Engine answers natural-language questions by ranking events, resolving claims,
// and detecting contradictions.
type Engine struct {
	events        eventLister
	claims        ports.ClaimRepository
	relationships ports.RelationshipRepository
	embeddings    ports.EmbeddingRepository
	embedClient   embedding.Client
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

// Answer searches all stored events for the best answer to the given question.
func (e Engine) Answer(question string) (domain.Answer, error) {
	allEvents, err := e.events.ListAll()
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load events for query: %w", err)
	}
	return e.answerWithEvents(question, allEvents)
}

// AnswerForRun searches events belonging to the specified run for the best answer.
func (e Engine) AnswerForRun(question, runID string) (domain.Answer, error) {
	if strings.TrimSpace(runID) == "" {
		return domain.Answer{}, fmt.Errorf("run id is required")
	}
	events, err := e.events.ListByRunID(runID)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load events for run: %w", err)
	}
	if len(events) == 0 {
		return domain.Answer{AnswerText: fmt.Sprintf("No events found for run %q.", runID)}, nil
	}
	return e.answerWithEvents(question, events)
}

func (e Engine) answerWithEvents(question string, allEvents []domain.Event) (domain.Answer, error) {
	q := strings.TrimSpace(question)
	if q == "" {
		return domain.Answer{}, fmt.Errorf("query question is required")
	}
	if len(allEvents) == 0 {
		return domain.Answer{AnswerText: "No ingested events yet."}, nil
	}

	topEvents := e.rankEventsWithFallback(q, allEvents, 5)
	eventIDs := make([]string, 0, len(topEvents))
	for _, event := range topEvents {
		eventIDs = append(eventIDs, event.ID)
	}

	claims, err := e.claims.ListByEventIDs(eventIDs)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load claims for query: %w", err)
	}

	contradictions, err := collectContradictions(e.relationships, claims)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load contradictions for query: %w", err)
	}

	answerText := buildAnswerText(q, claims, contradictions, len(topEvents))
	return domain.Answer{
		AnswerText:       answerText,
		Claims:           claims,
		Contradictions:   contradictions,
		TimelineEventIDs: eventIDs,
	}, nil
}

// rankEventsWithFallback tries cosine similarity first (if embeddings are available),
// then falls back to token-overlap ranking.
func (e Engine) rankEventsWithFallback(question string, events []domain.Event, limit int) []domain.Event {
	if e.embeddings != nil && e.embedClient != nil {
		result, err := e.rankEventsByCosine(question, events, limit)
		if err == nil && len(result) > 0 {
			return result
		}
		// Fall through to token overlap on error.
	}
	return rankEvents(question, events, limit)
}

// rankEventsByCosine embeds the question and ranks events by cosine similarity
// against their stored embeddings.
func (e Engine) rankEventsByCosine(question string, events []domain.Event, limit int) ([]domain.Event, error) {
	stored, err := e.embeddings.ListByEntityType("event")
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
	qVectors, err := e.embedClient.Embed(context.Background(), []string{question})
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

func rankEvents(question string, events []domain.Event, limit int) []domain.Event {
	qTokens := tokenSet(question)
	type scored struct {
		event domain.Event
		score int
	}
	scoredEvents := make([]scored, 0, len(events))
	for _, event := range events {
		s := overlapScore(qTokens, tokenSet(event.Content))
		if s == 0 {
			continue
		}
		scoredEvents = append(scoredEvents, scored{event: event, score: s})
	}

	if len(scoredEvents) == 0 {
		fallback := make([]domain.Event, 0, min(limit, len(events)))
		for i := len(events) - 1; i >= 0 && len(fallback) < limit; i-- {
			fallback = append(fallback, events[i])
		}
		return fallback
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

func collectContradictions(repo ports.RelationshipRepository, claims []domain.Claim) ([]domain.Relationship, error) {
	seen := map[string]struct{}{}
	result := make([]domain.Relationship, 0)
	for _, claim := range claims {
		rels, err := repo.ListByClaim(claim.ID)
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

func overlapScore(a, b map[string]struct{}) int {
	score := 0
	for token := range a {
		if _, ok := b[token]; ok {
			score++
		}
	}
	return score
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
