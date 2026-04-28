package pipeline

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/embedding"
	"github.com/felixgeelhaar/mnemos/internal/store"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

// SemanticDedupePlan is the result of a similarity scan: a list of
// merge proposals describing which claim should absorb which others,
// and the highest similarity that justified each merge. Returned
// without writing so callers can preview (--dry-run) before applying.
type SemanticDedupePlan struct {
	// Merges keyed by canonical (winner) claim id. Each entry holds
	// the duplicate ids that should be folded into it.
	Merges []SemanticMerge
	// Threshold the plan was built against, echoed back for clarity
	// in user-facing output.
	Threshold float64
	// ClaimsScanned is the population the scan considered (claims that
	// have an embedding under the current entity_type='claim'). Claims
	// without an embedding are skipped — they cannot be compared.
	ClaimsScanned int
	// SkippedNoEmbedding is how many claims were excluded because they
	// have no embedding row. Surfaced so users know the scan was
	// partial and can run `mnemos reembed` first if they care.
	SkippedNoEmbedding int
}

// SemanticMerge describes one absorption: a winner claim id that
// should absorb the listed duplicates (and inherit their evidence
// links). MaxSimilarity is the strongest pairwise similarity inside
// the cluster, useful for sorting the dry-run output by confidence.
type SemanticMerge struct {
	WinnerID      string
	DuplicateIDs  []string
	MaxSimilarity float32
}

// PlanSemanticDedupe scans every claim with an embedding, groups
// claims whose pairwise cosine similarity is at or above threshold,
// and selects a winner per cluster — the highest trust_score, with
// the earliest CreatedAt as a deterministic tiebreaker. Returns the
// plan without modifying the database. Callers (typically
// `mnemos dedup`) can then either present it (--dry-run) or pass it
// to ApplySemanticDedupe.
//
// Memory: holds every claim embedding in memory at once — at 768
// dims × 4 bytes that's ~3 MB per 1000 claims, fine for the
// local-first scale Mnemos targets. If/when we need to scale past
// hundreds of thousands of claims this should move to a vector
// index (sqlite-vss with cgo, or a separate process).
func PlanSemanticDedupe(ctx context.Context, conn *store.Conn, threshold float64) (SemanticDedupePlan, error) {
	if threshold <= 0 || threshold > 1 {
		return SemanticDedupePlan{}, fmt.Errorf("threshold must be in (0, 1]; got %v", threshold)
	}

	allClaims, err := conn.Claims.ListAll(ctx)
	if err != nil {
		return SemanticDedupePlan{}, fmt.Errorf("list claims: %w", err)
	}
	if len(allClaims) < 2 {
		return SemanticDedupePlan{Threshold: threshold, ClaimsScanned: len(allClaims)}, nil
	}

	stored, err := conn.Embeddings.ListByEntityType(ctx, "claim")
	if err != nil {
		return SemanticDedupePlan{}, fmt.Errorf("list claim embeddings: %w", err)
	}
	vecByID := make(map[string][]float32, len(stored))
	for _, rec := range stored {
		vecByID[rec.EntityID] = rec.Vector
	}

	// Index claims that actually have an embedding. We need both the
	// vector and the rest of the claim metadata (trust + created_at
	// for tiebreaking).
	pool := make([]indexedClaim, 0, len(allClaims))
	for _, c := range allClaims {
		v, ok := vecByID[c.ID]
		if !ok || len(v) == 0 {
			continue
		}
		pool = append(pool, indexedClaim{claim: c, vec: v})
	}
	skipped := len(allClaims) - len(pool)

	// Union-Find over the pool. Each cluster collapses to one root;
	// after the pass we walk roots → members to build merges.
	parent := make([]int, len(pool))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Pairwise similarity. O(n^2) but with a small constant — for the
	// 10k-claim ceiling this is ~50M cosine ops, still under a second
	// on a modern laptop.
	maxSim := make(map[[2]int]float32)
	for i := 0; i < len(pool); i++ {
		for j := i + 1; j < len(pool); j++ {
			if len(pool[i].vec) != len(pool[j].vec) {
				continue
			}
			sim, err := embedding.CosineSimilarity(pool[i].vec, pool[j].vec)
			if err != nil {
				continue
			}
			if float64(sim) >= threshold {
				union(i, j)
				maxSim[[2]int{i, j}] = sim
			}
		}
	}

	// Group members by their root.
	clusters := make(map[int][]int)
	for i := range pool {
		r := find(i)
		clusters[r] = append(clusters[r], i)
	}

	merges := make([]SemanticMerge, 0)
	for _, members := range clusters {
		if len(members) < 2 {
			continue
		}
		winnerIdx := pickWinner(pool, members)
		dupes := make([]string, 0, len(members)-1)
		for _, m := range members {
			if m == winnerIdx {
				continue
			}
			dupes = append(dupes, pool[m].claim.ID)
		}
		var clusterMax float32
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				key := [2]int{members[i], members[j]}
				if members[i] > members[j] {
					key = [2]int{members[j], members[i]}
				}
				if s, ok := maxSim[key]; ok && s > clusterMax {
					clusterMax = s
				}
			}
		}
		merges = append(merges, SemanticMerge{
			WinnerID:      pool[winnerIdx].claim.ID,
			DuplicateIDs:  dupes,
			MaxSimilarity: clusterMax,
		})
	}

	return SemanticDedupePlan{
		Merges:             merges,
		Threshold:          threshold,
		ClaimsScanned:      len(pool),
		SkippedNoEmbedding: skipped,
	}, nil
}

// indexedClaim pairs a claim with its embedding vector for the
// dedupe scan. Lifted out of PlanSemanticDedupe so pickWinner can
// accept the same shape without re-declaring the anonymous type.
type indexedClaim struct {
	claim domain.Claim
	vec   []float32
}

// pickWinner returns the index of the cluster member that should
// absorb the others. Highest trust_score wins; ties broken by
// earliest CreatedAt (older claim has accumulated more evidence /
// is more anchored in the knowledge base); final tiebreak is lex
// order on id for stable output.
func pickWinner(pool []indexedClaim, members []int) int {
	best := members[0]
	for _, m := range members[1:] {
		if pool[m].claim.TrustScore > pool[best].claim.TrustScore {
			best = m
			continue
		}
		if pool[m].claim.TrustScore < pool[best].claim.TrustScore {
			continue
		}
		if pool[m].claim.CreatedAt.Before(pool[best].claim.CreatedAt) {
			best = m
			continue
		}
		if pool[m].claim.CreatedAt.After(pool[best].claim.CreatedAt) {
			continue
		}
		if pool[m].claim.ID < pool[best].claim.ID {
			best = m
		}
	}
	return best
}

// ApplySemanticDedupe executes the plan against a SQLite-backed
// Conn. The merge surgery (UPDATE OR IGNORE / INSERT OR IGNORE on
// the relationships and claim_evidence tables) is SQLite-flavoured
// SQL; backends that don't expose a *sql.DB through Conn.Raw will
// receive a clear error so operators see the limitation rather
// than a partial write. Postgres and MySQL parity is tracked as a
// separate roady feature — the same semantics are achievable with
// each provider's idioms (ON CONFLICT DO NOTHING / INSERT IGNORE),
// they just need provider-specific bodies.
// duplicate's evidence/relationship/embedding row is rewritten to
// point at the winner, and the duplicate claim is deleted. All
// changes happen in a single transaction so a mid-run crash leaves
// the DB consistent.
func ApplySemanticDedupe(ctx context.Context, conn *store.Conn, plan SemanticDedupePlan) (int, error) {
	if len(plan.Merges) == 0 {
		return 0, nil
	}
	db, ok := conn.Raw.(*sql.DB)
	if !ok || db == nil {
		return 0, fmt.Errorf("apply semantic dedupe: backend does not expose *sql.DB; SQLite-only operation today")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlcgen.New(tx)

	merged := 0
	for _, m := range plan.Merges {
		for _, dupID := range m.DuplicateIDs {
			// Repoint evidence: each (dup, event_id) becomes
			// (winner, event_id). Use REPLACE-style upsert so we
			// don't double-insert when the winner already had the
			// same evidence link.
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO claim_evidence (claim_id, event_id)
				 SELECT ?, event_id FROM claim_evidence WHERE claim_id = ?`,
				m.WinnerID, dupID,
			); err != nil {
				return 0, fmt.Errorf("repoint evidence %s→%s: %w", dupID, m.WinnerID, err)
			}
			if err := q.DeleteClaimEvidenceByClaimID(ctx, dupID); err != nil {
				return 0, fmt.Errorf("delete evidence for %s: %w", dupID, err)
			}

			// Repoint relationships: dup as either endpoint becomes
			// the winner. Drop edges that would self-loop after the
			// rewrite (winner→winner is meaningless).
			if _, err := tx.ExecContext(ctx,
				`UPDATE OR IGNORE relationships SET from_claim_id = ? WHERE from_claim_id = ?`,
				m.WinnerID, dupID,
			); err != nil {
				return 0, fmt.Errorf("repoint rels.from %s→%s: %w", dupID, m.WinnerID, err)
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE OR IGNORE relationships SET to_claim_id = ? WHERE to_claim_id = ?`,
				m.WinnerID, dupID,
			); err != nil {
				return 0, fmt.Errorf("repoint rels.to %s→%s: %w", dupID, m.WinnerID, err)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM relationships
				 WHERE from_claim_id = ? AND to_claim_id = ?`,
				m.WinnerID, m.WinnerID,
			); err != nil {
				return 0, fmt.Errorf("drop self-loop for %s: %w", m.WinnerID, err)
			}
			// UPDATE OR IGNORE silently drops rows that would
			// violate the unique edge index. Clean up the orphans:
			// any rels still pointing at the duplicate after the
			// repoint above are conflicts that we accept losing.
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM relationships
				 WHERE from_claim_id = ? OR to_claim_id = ?`,
				dupID, dupID,
			); err != nil {
				return 0, fmt.Errorf("drop dangling rels for %s: %w", dupID, err)
			}

			// Status history and the duplicate's own embedding row
			// follow the claim into the bin — keeping them would
			// leave dangling references.
			if err := q.DeleteClaimStatusHistoryByClaimID(ctx, dupID); err != nil {
				return 0, fmt.Errorf("delete status history for %s: %w", dupID, err)
			}
			if err := q.DeleteEmbeddingByEntity(ctx, sqlcgen.DeleteEmbeddingByEntityParams{
				EntityID: dupID, EntityType: "claim",
			}); err != nil {
				return 0, fmt.Errorf("delete embedding for %s: %w", dupID, err)
			}
			if err := q.DeleteClaimByID(ctx, dupID); err != nil {
				return 0, fmt.Errorf("delete claim %s: %w", dupID, err)
			}
			merged++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit dedupe: %w", err)
	}
	return merged, nil
}
