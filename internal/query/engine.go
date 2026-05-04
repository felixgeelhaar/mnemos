package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/llm"
	"github.com/felixgeelhaar/mnemos/internal/ports"
	"github.com/felixgeelhaar/mnemos/internal/trust"
)

type eventLister interface {
	ports.EventRepository
	ListAll(ctx context.Context) ([]domain.Event, error)
	ListByRunID(ctx context.Context, runID string) ([]domain.Event, error)
}

// Engine answers natural-language questions by ranking events, resolving claims,
// and detecting contradictions.
type Engine struct {
	events          eventLister
	claims          ports.ClaimRepository
	relationships   ports.RelationshipRepository
	embeddings      ports.EmbeddingRepository
	embedClient     embedding.Client
	llmClient       llm.Client
	eventTextSearch ports.TextSearcher
	claimTextSearch ports.TextSearcher
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

// WithTextSearch wires the v0.10 hybrid retrieval path: a BM25
// keyword index over events and another over claims. When both are
// set, the engine combines BM25 with cosine similarity (when also
// available) into a hybrid relevance score; when only BM25 is set,
// it replaces the legacy token-overlap fallback. Either argument may
// be nil to opt out of that side.
func (e Engine) WithTextSearch(events, claims ports.TextSearcher) Engine {
	e.eventTextSearch = events
	e.claimTextSearch = claims
	return e
}

// AnswerOptions tunes a query without requiring callers that just want the
// default behavior to learn a new constructor signature. Hops controls
// graph-expansion of the directly-retrieved claim set: 0 means no expansion,
// N means follow up to N supports/contradicts edges from the seed claims.
// MinTrust filters out claims whose computed trust_score (see internal/trust)
// is strictly below the threshold; 0 disables the filter.
//
// AsOf enables point-in-time queries against the temporal-validity layer
// (see domain.Claim.IsValidAt). When non-zero, only claims that were in
// force at that instant are returned; when zero, the engine substitutes
// time.Now() so the default is "what is currently true". IncludeHistory
// disables temporal filtering entirely — callers see superseded claims
// alongside current ones, useful for `--history` / audit views.
type AnswerOptions struct {
	Hops           int
	MinTrust       float64
	AsOf           time.Time
	// RecordedAsOf is the ingestion-time axis. When non-zero, the
	// engine drops claims with CreatedAt > RecordedAsOf so the
	// response reproduces what the store knew as of that timestamp.
	// Zero value disables the filter (the common case).
	RecordedAsOf   time.Time
	IncludeHistory bool
	// AllowedClaimIDs, when non-nil, restricts the answer set to
	// claims whose id is in the map. Used by `query --entity` to
	// constrain results to a single entity's claims without
	// rewriting the retrieval pipeline. nil disables the filter
	// (the common case).
	AllowedClaimIDs map[string]struct{}
	// HopKinds, when non-empty, restricts hop expansion to relationship
	// edges of these types. Empty means "follow every kind", preserving
	// pre-causal behaviour. Used by `query --kind causes,validates ...`
	// to walk a single semantic family of edges (e.g. only the causal
	// graph, not contradictions).
	HopKinds []domain.RelationshipType
	// Scope, when non-empty, narrows the answer to claims whose
	// per-claim Scope matches the supplied filter. Empty fields in
	// the filter act as wildcards (Scope.Matches semantics): a
	// filter of {Service:"payments"} matches any claim with
	// Service="payments" regardless of Env/Team.
	Scope domain.Scope
}

// Answer searches all stored events for the best answer to the given question.
func (e Engine) Answer(question string) (domain.Answer, error) {
	return e.AnswerWithOptions(question, AnswerOptions{})
}

// AnswerWithOptions is the configurable form of Answer. The plain Answer
// method delegates here with a zero-value AnswerOptions so existing callers
// see no behavior change.
func (e Engine) AnswerWithOptions(question string, opts AnswerOptions) (domain.Answer, error) {
	ctx := context.Background()
	allEvents, err := e.events.ListAll(ctx)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load events for query: %w", err)
	}
	return e.answerWithEvents(ctx, question, allEvents, opts)
}

// AnswerForRun searches events belonging to the specified run for the best answer.
func (e Engine) AnswerForRun(question, runID string) (domain.Answer, error) {
	return e.AnswerForRunWithOptions(question, runID, AnswerOptions{})
}

// AnswerForRunWithOptions is the configurable form of AnswerForRun.
func (e Engine) AnswerForRunWithOptions(question, runID string, opts AnswerOptions) (domain.Answer, error) {
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
	return e.answerWithEvents(ctx, question, events, opts)
}

func (e Engine) answerWithEvents(ctx context.Context, question string, allEvents []domain.Event, opts AnswerOptions) (domain.Answer, error) {
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

	// Entity scope: if the caller restricted the answer to claims
	// linked to a specific entity (--entity in the CLI), drop
	// everything else before ranking. The map is small (one entity's
	// worth of claim ids); the filter is O(claims).
	if opts.AllowedClaimIDs != nil {
		filtered := make([]domain.Claim, 0, len(claims))
		for _, c := range claims {
			if _, ok := opts.AllowedClaimIDs[c.ID]; ok {
				filtered = append(filtered, c)
			}
		}
		claims = filtered
	}

	// Scope filter: narrow the candidate set to claims whose Scope
	// matches the caller's filter before any ranking. Empty filter
	// is a no-op so single-tenant deployments see no change.
	if !opts.Scope.IsEmpty() {
		filtered := make([]domain.Claim, 0, len(claims))
		for _, c := range claims {
			if c.Scope.Matches(opts.Scope) {
				filtered = append(filtered, c)
			}
		}
		claims = filtered
	}

	// Filter out low-trust claims before ranking — saves work on the
	// cosine pass and prevents low-trust noise from displacing
	// high-trust answers in the top-N.
	if opts.MinTrust > 0 {
		filtered := make([]domain.Claim, 0, len(claims))
		for _, c := range claims {
			if c.TrustScore >= opts.MinTrust {
				filtered = append(filtered, c)
			}
		}
		claims = filtered
	}

	// Temporal filter: by default, exclude claims that have been
	// superseded (valid_to in the past). Callers asking for history
	// (--include-history) opt out; --at <date> queries swap the
	// cutoff for a point-in-time check.
	if !opts.IncludeHistory {
		asOf := opts.AsOf
		if asOf.IsZero() {
			asOf = time.Now().UTC()
		}
		filtered := make([]domain.Claim, 0, len(claims))
		for _, c := range claims {
			if c.IsValidAt(asOf) {
				filtered = append(filtered, c)
			}
		}
		claims = filtered
	}

	// Ingestion-time filter (the second axis of the bi-temporal
	// model). Drop rows recorded after RecordedAsOf so the response
	// reproduces what the store knew at that timestamp. Independent
	// of the validity filter — a claim that was valid yesterday but
	// recorded today returns under (AsOf=yesterday, RecordedAsOf=now)
	// and disappears under (AsOf=yesterday, RecordedAsOf=yesterday).
	if !opts.RecordedAsOf.IsZero() {
		filtered := make([]domain.Claim, 0, len(claims))
		for _, c := range claims {
			if !c.CreatedAt.After(opts.RecordedAsOf) {
				filtered = append(filtered, c)
			}
		}
		claims = filtered
	}

	// Re-rank claims by semantic similarity when embeddings are available.
	claims = e.rankClaimsByCosine(ctx, q, claims)

	// Boost claims matching the question's intent (e.g., "decisions" → decision type).
	claims = boostClaimsByQuestionIntent(q, claims)

	// Track hop distance per claim — direct claims are hop 0; expansion
	// fills in 1..opts.Hops for claims reached via supports/contradicts edges.
	hopDistance := make(map[string]int, len(claims))
	for _, c := range claims {
		hopDistance[c.ID] = 0
	}
	if opts.Hops > 0 {
		expanded, err := e.expandClaimsByHops(ctx, claims, opts.Hops, hopDistance, opts.HopKinds)
		if err != nil {
			// Hop expansion is additive — log via the standard error path
			// rather than failing the whole answer.
			return domain.Answer{}, fmt.Errorf("expand claims by %d hops: %w", opts.Hops, err)
		}
		claims = append(claims, expanded...)
	}

	contradictions, err := collectContradictions(ctx, e.relationships, claims)
	if err != nil {
		return domain.Answer{}, fmt.Errorf("load contradictions for query: %w", err)
	}

	provenance := e.computeClaimProvenance(ctx, claims, topEvents)
	narratives := e.buildClaimNarratives(ctx, claims)

	answerText := e.generateAnswer(ctx, q, claims, contradictions, len(topEvents), provenance, narratives)
	if opts.Hops > 0 {
		expandedCount := 0
		for _, c := range claims {
			if hopDistance[c.ID] > 0 {
				expandedCount++
			}
		}
		if expandedCount > 0 {
			answerText += fmt.Sprintf(" Expanded %d additional claim(s) via supports/contradicts edges (up to %d hop(s)).", expandedCount, opts.Hops)
		}
	}

	stale := computeStaleClaims(claims, time.Now().UTC())

	return domain.Answer{
		AnswerText:       answerText,
		Claims:           claims,
		Contradictions:   contradictions,
		TimelineEventIDs: eventIDs,
		ClaimProvenance:  provenance,
		ClaimHopDistance: hopDistance,
		StaleClaimIDs:    stale,
	}, nil
}

// computeStaleClaims returns the ids of claims whose freshness factor
// has decayed below the trust floor. The reference timestamp is the
// later of LastVerified and ValidFrom (validFrom is set from the
// source event timestamp by the pipeline, so it doubles as a
// "latest evidence" proxy when LastVerified is unset). Claims
// without any usable timestamp are treated as not-stale rather than
// flagged with a false signal.
func computeStaleClaims(claims []domain.Claim, now time.Time) []string {
	if len(claims) == 0 {
		return nil
	}
	out := make([]string, 0)
	for _, c := range claims {
		if trust.IsStale(c.ValidFrom, c.LastVerified, now, c.HalfLifeDays, 0) {
			out = append(out, c.ID)
		}
	}
	return out
}

// expandClaimsByHops does a BFS through the relationship graph from the
// given seed claims, returning the newly-discovered claims (not the seeds
// themselves). hopDistance is mutated in place: each newly-seen claim is
// recorded with its hop distance from the seed set. Termination: when the
// frontier of newly-discovered IDs is empty or maxHops is reached.
func (e Engine) expandClaimsByHops(ctx context.Context, seed []domain.Claim, maxHops int, hopDistance map[string]int, kinds []domain.RelationshipType) ([]domain.Claim, error) {
	if maxHops <= 0 || len(seed) == 0 {
		return nil, nil
	}
	frontier := make([]string, 0, len(seed))
	for _, c := range seed {
		frontier = append(frontier, c.ID)
	}

	allowed := make(map[domain.RelationshipType]struct{}, len(kinds))
	for _, k := range kinds {
		allowed[k] = struct{}{}
	}

	var expanded []domain.Claim
	for hop := 1; hop <= maxHops && len(frontier) > 0; hop++ {
		rels, err := e.relationships.ListByClaimIDs(ctx, frontier)
		if err != nil {
			return nil, fmt.Errorf("list relationships for hop %d: %w", hop, err)
		}
		nextIDs := map[string]struct{}{}
		for _, rel := range rels {
			if len(allowed) > 0 {
				if _, ok := allowed[rel.Type]; !ok {
					continue
				}
			}
			for _, neighbor := range []string{rel.FromClaimID, rel.ToClaimID} {
				if _, seen := hopDistance[neighbor]; seen {
					continue
				}
				nextIDs[neighbor] = struct{}{}
			}
		}
		if len(nextIDs) == 0 {
			break
		}
		ids := make([]string, 0, len(nextIDs))
		for id := range nextIDs {
			ids = append(ids, id)
		}
		newClaims, err := e.claims.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("load hop-%d claims: %w", hop, err)
		}
		for _, c := range newClaims {
			hopDistance[c.ID] = hop
		}
		expanded = append(expanded, newClaims...)
		frontier = ids
	}
	return expanded, nil
}

// buildClaimNarratives returns a per-claim lifecycle sentence for claims
// that have non-trivial history (at least one real status transition after
// the initial insert). Claims whose status never changed from their first
// recording have no narrative — there's no story to tell.
//
// Format example:
//
//	"First recorded as active on 2026-04-12; became contested on 2026-04-15
//	 (auto: contradiction detected); resolved on 2026-04-18 (evidence
//	 review by jane)."
//
// This is the narrative layer from issue #6 — turning the claim_status_history
// rows into a prose explanation so the query answer carries a temporal
// summary instead of just a current snapshot.
func (e Engine) buildClaimNarratives(ctx context.Context, claims []domain.Claim) map[string]string {
	if len(claims) == 0 {
		return nil
	}
	out := make(map[string]string, len(claims))
	// Only narrate the top few claims — a query for 50 claims shouldn't
	// dump 50 timelines into the answer.
	limit := 3
	if len(claims) < limit {
		limit = len(claims)
	}
	for i := 0; i < limit; i++ {
		c := claims[i]
		hist, err := e.claims.ListStatusHistoryByClaimID(ctx, c.ID)
		if err != nil || len(hist) == 0 {
			continue
		}
		// Narrative is only interesting when the status actually changed at
		// some point. A single initial-insert row (from_status="") has
		// nothing to tell beyond the current status snapshot, which the
		// main answer already shows.
		hasRealTransition := false
		for _, t := range hist {
			if t.FromStatus != "" {
				hasRealTransition = true
				break
			}
		}
		if !hasRealTransition {
			continue
		}
		out[c.ID] = formatNarrative(hist)
	}
	return out
}

func formatNarrative(hist []domain.ClaimStatusTransition) string {
	if len(hist) == 0 {
		return ""
	}
	var b strings.Builder
	for i, t := range hist {
		switch {
		case i == 0 && t.FromStatus == "":
			// Fresh history: we saw the insert.
			fmt.Fprintf(&b, "First recorded as %s on %s", t.ToStatus, t.ChangedAt.Format("2006-01-02"))
		case i == 0:
			// Backfilled / pre-existing: first recorded transition was
			// from an already-known status. Phrase it as an update rather
			// than as an initial creation.
			fmt.Fprintf(&b, "Transitioned from %s to %s on %s", t.FromStatus, t.ToStatus, t.ChangedAt.Format("2006-01-02"))
		default:
			fmt.Fprintf(&b, "; became %s on %s", t.ToStatus, t.ChangedAt.Format("2006-01-02"))
		}
		if t.Reason != "" {
			fmt.Fprintf(&b, " (%s)", t.Reason)
		}
	}
	b.WriteString(".")
	return b.String()
}

// computeClaimProvenance builds a per-claim origin map: "local" for claims
// whose evidence events have no pulled_from_registry metadata, or the
// registry URL when at least one evidence event was pulled. The first
// non-local origin wins because the question users ask is "where did this
// originate?" — once a claim is known to have a remote source, that's the
// load-bearing fact.
//
// Failures (e.g. evidence lookup error) silently yield an empty map; the
// engine never blocks an answer on provenance attribution.
func (e Engine) computeClaimProvenance(ctx context.Context, claims []domain.Claim, topEvents []domain.Event) map[string]string {
	if len(claims) == 0 {
		return nil
	}
	claimIDs := make([]string, 0, len(claims))
	for _, c := range claims {
		claimIDs = append(claimIDs, c.ID)
	}
	evidence, err := e.claims.ListEvidenceByClaimIDs(ctx, claimIDs)
	if err != nil || len(evidence) == 0 {
		return nil
	}

	// eventOrigin: event id → "local" or "<registry-url>"
	eventOrigin := make(map[string]string, len(topEvents))
	for _, ev := range topEvents {
		if reg, ok := ev.Metadata["pulled_from_registry"]; ok && reg != "" {
			eventOrigin[ev.ID] = reg
		} else {
			eventOrigin[ev.ID] = "local"
		}
	}

	out := make(map[string]string, len(claimIDs))
	for _, link := range evidence {
		origin, ok := eventOrigin[link.EventID]
		if !ok {
			continue // evidence event not in our top set; skip
		}
		existing, seen := out[link.ClaimID]
		if !seen || (existing == "local" && origin != "local") {
			out[link.ClaimID] = origin
		}
	}
	return out
}

// rankEventsWithFallback chooses the best ranking strategy available:
//   - Hybrid (BM25 + cosine) when both signals are wired up.
//   - Either signal alone when only one is wired up.
//   - Legacy token-overlap ranker when neither is available
//     (in-memory test doubles and embedding-less, FTS-less DBs).
//
// Hybrid scoring rationale: BM25 catches lexical / proper-noun
// matches that embeddings miss; embeddings catch synonyms and
// paraphrases that BM25 misses. Combining them is a well-trodden
// retrieval technique that typically yields a +20–40% nDCG over
// either signal alone — see the "obvious choice" v0.10 design note.
func (e Engine) rankEventsWithFallback(ctx context.Context, question string, events []domain.Event, limit int) []domain.Event {
	cosScores := e.cosineEventScores(ctx, question, events)
	bm25Scores := e.bm25EventScores(ctx, question, len(events)+limit)
	if len(cosScores) == 0 && len(bm25Scores) == 0 {
		return rankEvents(question, events, limit)
	}
	return rankEventsByHybridScore(events, cosScores, bm25Scores, limit)
}

// cosineEventScores returns a map of event id → cosine similarity
// against the question embedding, or an empty map when embeddings
// aren't available (so the caller can detect "no signal").
func (e Engine) cosineEventScores(ctx context.Context, question string, events []domain.Event) map[string]float64 {
	if e.embeddings == nil || e.embedClient == nil || len(events) == 0 {
		return nil
	}
	stored, err := e.embeddings.ListByEntityType(ctx, "event")
	if err != nil || len(stored) == 0 {
		return nil
	}
	vecByID := make(map[string][]float32, len(stored))
	for _, rec := range stored {
		vecByID[rec.EntityID] = rec.Vector
	}
	hasAny := false
	for _, ev := range events {
		if _, ok := vecByID[ev.ID]; ok {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}
	qVectors, err := e.embedClient.Embed(ctx, []string{question})
	if err != nil || len(qVectors) == 0 {
		return nil
	}
	qVec := qVectors[0]
	out := make(map[string]float64, len(events))
	for _, ev := range events {
		vec, ok := vecByID[ev.ID]
		if !ok {
			continue
		}
		sim, err := embedding.CosineSimilarity(qVec, vec)
		if err != nil {
			continue
		}
		out[ev.ID] = float64(sim)
	}
	return out
}

// bm25EventScores returns event id → BM25 relevance from the FTS5
// index. Already sign-flipped so higher = better. Empty when no
// TextSearcher is wired or the search returns nothing.
func (e Engine) bm25EventScores(ctx context.Context, question string, limit int) map[string]float64 {
	if e.eventTextSearch == nil {
		return nil
	}
	hits, err := e.eventTextSearch.SearchByText(ctx, question, limit)
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make(map[string]float64, len(hits))
	for _, h := range hits {
		out[h.ID] = h.Score
	}
	return out
}

// rankEventsByHybridScore combines the two signal maps into one
// composite score and returns the top-`limit` events. The math is
// deliberately conservative: max-normalise each signal into [0, 1]
// independently, then take the equal-weighted sum. This avoids
// arbitrary weighting decisions while letting either signal dominate
// when the other is silent.
func rankEventsByHybridScore(events []domain.Event, cos, bm25 map[string]float64, limit int) []domain.Event {
	cosMax := maxScore(cos)
	bmMax := maxScore(bm25)

	type scored struct {
		event domain.Event
		score float64
	}
	out := make([]scored, 0, len(events))
	for _, ev := range events {
		var c, b float64
		if cosMax > 0 {
			c = cos[ev.ID] / cosMax
		}
		if bmMax > 0 {
			b = bm25[ev.ID] / bmMax
		}
		if c == 0 && b == 0 {
			// Neither signal saw this event — drop rather than
			// pretend it's a relevant hit.
			continue
		}
		// When only one signal is present, that score carries the
		// full weight; when both, equal-weighted average.
		switch {
		case cosMax > 0 && bmMax > 0:
			out = append(out, scored{ev, 0.5*c + 0.5*b})
		case bmMax > 0:
			out = append(out, scored{ev, b})
		default:
			out = append(out, scored{ev, c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].event.Timestamp.After(out[j].event.Timestamp)
		}
		return out[i].score > out[j].score
	})
	end := limit
	if end > len(out) {
		end = len(out)
	}
	result := make([]domain.Event, 0, end)
	for i := 0; i < end; i++ {
		result = append(result, out[i].event)
	}
	return result
}

func maxScore(m map[string]float64) float64 {
	max := 0.0
	for _, v := range m {
		if v > max {
			max = v
		}
	}
	return max
}

// cosineClaimScores mirrors cosineEventScores but for claim embeddings.
func (e Engine) cosineClaimScores(ctx context.Context, question string, claims []domain.Claim) map[string]float64 {
	if e.embeddings == nil || e.embedClient == nil || len(claims) == 0 {
		return nil
	}
	stored, err := e.embeddings.ListByEntityType(ctx, "claim")
	if err != nil || len(stored) == 0 {
		return nil
	}
	vecByID := make(map[string][]float32, len(stored))
	for _, rec := range stored {
		vecByID[rec.EntityID] = rec.Vector
	}
	hasAny := false
	for _, cl := range claims {
		if _, ok := vecByID[cl.ID]; ok {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}
	qVectors, err := e.embedClient.Embed(ctx, []string{question})
	if err != nil || len(qVectors) == 0 {
		return nil
	}
	qVec := qVectors[0]
	out := make(map[string]float64, len(claims))
	for _, cl := range claims {
		vec, ok := vecByID[cl.ID]
		if !ok {
			continue
		}
		sim, err := embedding.CosineSimilarity(qVec, vec)
		if err != nil {
			continue
		}
		out[cl.ID] = float64(sim)
	}
	return out
}

// bm25ClaimScores mirrors bm25EventScores but for the claims_fts index.
func (e Engine) bm25ClaimScores(ctx context.Context, question string, limit int) map[string]float64 {
	if e.claimTextSearch == nil {
		return nil
	}
	hits, err := e.claimTextSearch.SearchByText(ctx, question, limit)
	if err != nil || len(hits) == 0 {
		return nil
	}
	out := make(map[string]float64, len(hits))
	for _, h := range hits {
		out[h.ID] = h.Score
	}
	return out
}

// rankClaimsByCosine reorders claims by relevance to the question.
// Despite the legacy name, this is now the hybrid ranker: it
// combines BM25 (when a TextSearcher is wired) with cosine
// similarity (when embeddings are wired), max-normalising each
// signal into [0, 1] before equal-weighted summation. Claims with
// no signal at all retain their original relative position via the
// idx tiebreak so callers see "embedding-less" claims at the bottom
// rather than randomly shuffled.
func (e Engine) rankClaimsByCosine(ctx context.Context, question string, claims []domain.Claim) []domain.Claim {
	if len(claims) <= 1 {
		return claims
	}

	cos := e.cosineClaimScores(ctx, question, claims)
	bm := e.bm25ClaimScores(ctx, question, len(claims)+10)
	if len(cos) == 0 && len(bm) == 0 {
		return claims
	}

	cosMax := maxScore(cos)
	bmMax := maxScore(bm)

	type scored struct {
		claim domain.Claim
		score float64
		idx   int // original index for stable ordering of tied / signal-less claims
	}
	scoredClaims := make([]scored, 0, len(claims))
	for i, cl := range claims {
		var c, b float64
		if cosMax > 0 {
			c = cos[cl.ID] / cosMax
		}
		if bmMax > 0 {
			b = bm[cl.ID] / bmMax
		}
		var s float64
		switch {
		case cosMax > 0 && bmMax > 0:
			s = 0.5*c + 0.5*b
		case bmMax > 0:
			s = b
		case cosMax > 0:
			s = c
		}
		if s == 0 {
			s = -1 // signal-less claim; sinks below any positive hit but keeps original order
		}
		scoredClaims = append(scoredClaims, scored{claim: cl, score: s, idx: i})
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
func (e Engine) generateAnswer(ctx context.Context, question string, claims []domain.Claim, contradictions []domain.Relationship, eventCount int, provenance map[string]string, narratives map[string]string) string {
	if e.llmClient == nil || len(claims) == 0 {
		return buildAnswerText(question, claims, contradictions, eventCount, provenance, narratives)
	}

	answer, err := e.groundedAnswer(ctx, question, claims, contradictions)
	if err != nil {
		// Fall back to template on any LLM error.
		return buildAnswerText(question, claims, contradictions, eventCount, provenance, narratives)
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

func buildAnswerText(question string, claims []domain.Claim, contradictions []domain.Relationship, eventCount int, provenance map[string]string, narratives map[string]string) string {
	if len(claims) == 0 {
		return fmt.Sprintf("I could not find claims yet for %q. Try running extract/relate first.", question)
	}

	parts := []string{}
	parts = append(parts, fmt.Sprintf("For %q, the strongest signal is: %s%s.", question, claims[0].Text, provenanceSuffix(claims[0].ID, provenance)))
	if n, ok := narratives[claims[0].ID]; ok {
		parts = append(parts, "Evolution: "+n)
	}

	if len(claims) > 1 {
		parts = append(parts, fmt.Sprintf("Other relevant claim: %s%s.", claims[1].Text, provenanceSuffix(claims[1].ID, provenance)))
		if n, ok := narratives[claims[1].ID]; ok {
			parts = append(parts, "Evolution: "+n)
		}
	}

	if len(contradictions) > 0 {
		parts = append(parts, fmt.Sprintf("I also found %d contradiction(s), so this topic is contested.", len(contradictions)))
	} else {
		parts = append(parts, "No contradictions were found in the current claim set.")
	}

	if remoteCount := countRemoteClaims(claims, provenance); remoteCount > 0 {
		parts = append(parts, fmt.Sprintf("Context used %d event(s) and %d claim(s) (%d from a connected registry).", eventCount, len(claims), remoteCount))
	} else {
		parts = append(parts, fmt.Sprintf("Context used %d event(s) and %d claim(s).", eventCount, len(claims)))
	}
	return strings.Join(parts, " ")
}

// provenanceSuffix returns " (from <registry-url>)" for claims pulled from a
// registry, empty for local or unknown claims. Local claims aren't tagged
// because that's the unmarked default — flagging every local one would add
// noise to single-project queries.
func provenanceSuffix(claimID string, provenance map[string]string) string {
	if provenance == nil {
		return ""
	}
	origin, ok := provenance[claimID]
	if !ok || origin == "local" || origin == "" {
		return ""
	}
	return " (from " + origin + ")"
}

func countRemoteClaims(claims []domain.Claim, provenance map[string]string) int {
	if provenance == nil {
		return 0
	}
	n := 0
	for _, c := range claims {
		if origin := provenance[c.ID]; origin != "" && origin != "local" {
			n++
		}
	}
	return n
}
