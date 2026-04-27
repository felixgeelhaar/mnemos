package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type fakeEventRepo struct {
	events []domain.Event
}

func (f fakeEventRepo) Append(_ context.Context, _ domain.Event) error { return nil }
func (f fakeEventRepo) GetByID(_ context.Context, _ string) (domain.Event, error) {
	return domain.Event{}, nil
}
func (f fakeEventRepo) ListByIDs(_ context.Context, _ []string) ([]domain.Event, error) {
	return nil, nil
}
func (f fakeEventRepo) ListAll(_ context.Context) ([]domain.Event, error) { return f.events, nil }
func (f fakeEventRepo) ListByRunID(_ context.Context, runID string) ([]domain.Event, error) {
	filtered := make([]domain.Event, 0)
	for _, event := range f.events {
		if event.RunID == runID {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

type fakeClaimRepo struct {
	claims   []domain.Claim
	evidence []domain.ClaimEvidence
}

func (f fakeClaimRepo) Upsert(_ context.Context, _ []domain.Claim) error { return nil }
func (f fakeClaimRepo) UpsertWithReason(_ context.Context, _ []domain.Claim, _ string) error {
	return nil
}
func (f fakeClaimRepo) UpsertWithReasonAs(_ context.Context, _ []domain.Claim, _, _ string) error {
	return nil
}
func (f fakeClaimRepo) UpsertEvidence(_ context.Context, _ []domain.ClaimEvidence) error {
	return nil
}
func (f fakeClaimRepo) ListAll(_ context.Context) ([]domain.Claim, error) { return f.claims, nil }
func (f fakeClaimRepo) SetValidity(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (f fakeClaimRepo) ListByEventIDs(_ context.Context, _ []string) ([]domain.Claim, error) {
	return f.claims, nil
}
func (f fakeClaimRepo) ListEvidenceByClaimIDs(_ context.Context, claimIDs []string) ([]domain.ClaimEvidence, error) {
	wanted := map[string]struct{}{}
	for _, id := range claimIDs {
		wanted[id] = struct{}{}
	}
	out := make([]domain.ClaimEvidence, 0, len(f.evidence))
	for _, e := range f.evidence {
		if _, ok := wanted[e.ClaimID]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f fakeClaimRepo) ListStatusHistoryByClaimID(_ context.Context, _ string) ([]domain.ClaimStatusTransition, error) {
	return nil, nil
}
func (f fakeClaimRepo) ListByIDs(_ context.Context, claimIDs []string) ([]domain.Claim, error) {
	wanted := map[string]struct{}{}
	for _, id := range claimIDs {
		wanted[id] = struct{}{}
	}
	out := make([]domain.Claim, 0, len(claimIDs))
	for _, c := range f.claims {
		if _, ok := wanted[c.ID]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeRelationshipRepo struct {
	rels map[string][]domain.Relationship
}

func (f fakeRelationshipRepo) Upsert(_ context.Context, _ []domain.Relationship) error { return nil }
func (f fakeRelationshipRepo) ListByClaim(_ context.Context, claimID string) ([]domain.Relationship, error) {
	return f.rels[claimID], nil
}
func (f fakeRelationshipRepo) ListByClaimIDs(_ context.Context, claimIDs []string) ([]domain.Relationship, error) {
	seen := map[string]struct{}{}
	out := make([]domain.Relationship, 0)
	for _, id := range claimIDs {
		for _, rel := range f.rels[id] {
			if _, dup := seen[rel.ID]; dup {
				continue
			}
			seen[rel.ID] = struct{}{}
			out = append(out, rel)
		}
	}
	return out, nil
}

func TestAnswerIncludesClaimsAndContradictions(t *testing.T) {
	now := time.Date(2026, 4, 12, 17, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_1", RunID: "run_1", Content: "Revenue decreased after launch", Timestamp: now},
		{ID: "ev_2", RunID: "run_2", Content: "Churn increased in Q2", Timestamp: now.Add(time.Minute)},
	}}

	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue decreased after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
		{ID: "cl_2", Text: "Revenue did not decrease after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
	}

	rels := fakeRelationshipRepo{rels: map[string][]domain.Relationship{
		"cl_1": {{ID: "rl_1", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_1", ToClaimID: "cl_2", CreatedAt: now}},
		"cl_2": {{ID: "rl_1", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_1", ToClaimID: "cl_2", CreatedAt: now}},
	}}

	engine := NewEngine(events, fakeClaimRepo{claims: claims}, rels)
	answer, err := engine.Answer("what happened to revenue after launch")
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}

	if len(answer.Claims) != 2 {
		t.Fatalf("Claims len = %d, want 2", len(answer.Claims))
	}
	if len(answer.Contradictions) != 1 {
		t.Fatalf("Contradictions len = %d, want 1", len(answer.Contradictions))
	}
	if len(answer.TimelineEventIDs) == 0 {
		t.Fatal("TimelineEventIDs should not be empty")
	}
	if !strings.Contains(answer.AnswerText, "strongest signal") {
		t.Fatalf("AnswerText = %q, expected strongest signal narrative", answer.AnswerText)
	}
	if !strings.Contains(answer.AnswerText, "contested") {
		t.Fatalf("AnswerText = %q, expected contradiction context", answer.AnswerText)
	}
}

func TestAnswerForRunScopesEvents(t *testing.T) {
	now := time.Date(2026, 4, 12, 18, 0, 0, 0, time.UTC)
	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_run_a", RunID: "run_a", Content: "Revenue decreased after launch", Timestamp: now},
		{ID: "ev_run_b", RunID: "run_b", Content: "Churn increased in Q2", Timestamp: now.Add(time.Minute)},
	}}
	claims := []domain.Claim{
		{ID: "cl_1", Text: "Revenue decreased after launch", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
	}
	engine := NewEngine(events, fakeClaimRepo{claims: claims}, fakeRelationshipRepo{rels: map[string][]domain.Relationship{}})

	answer, err := engine.AnswerForRun("what happened to revenue", "run_a")
	if err != nil {
		t.Fatalf("AnswerForRun() error = %v", err)
	}
	if len(answer.TimelineEventIDs) != 1 {
		t.Fatalf("TimelineEventIDs len = %d, want 1", len(answer.TimelineEventIDs))
	}
	if answer.TimelineEventIDs[0] != "ev_run_a" {
		t.Fatalf("TimelineEventIDs[0] = %q, want ev_run_a", answer.TimelineEventIDs[0])
	}
}

func TestAnswer_HopExpansionWalksRelationshipGraph(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	// Three claims chained through relationships:
	//   cl_seed --(supports)--> cl_one --(contradicts)--> cl_two
	// A query that finds only cl_seed via the events should, with hops=2,
	// expand to include cl_one (1 hop) and cl_two (2 hops).
	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_seed", RunID: "r", Content: "Seed event about cache eviction policy", Timestamp: now},
	}}
	allClaims := []domain.Claim{
		{ID: "cl_seed", Text: "Cache eviction is LRU", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.9, CreatedAt: now},
		{ID: "cl_one", Text: "LRU outperforms FIFO under our workload", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.85, CreatedAt: now},
		{ID: "cl_two", Text: "FIFO is simpler to reason about", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.7, CreatedAt: now},
	}
	repo := fakeClaimRepo{
		claims:   allClaims,
		evidence: []domain.ClaimEvidence{{ClaimID: "cl_seed", EventID: "ev_seed"}},
	}
	rels := fakeRelationshipRepo{rels: map[string][]domain.Relationship{
		"cl_seed": {{ID: "r1", Type: domain.RelationshipTypeSupports, FromClaimID: "cl_seed", ToClaimID: "cl_one", CreatedAt: now}},
		"cl_one": {
			{ID: "r1", Type: domain.RelationshipTypeSupports, FromClaimID: "cl_seed", ToClaimID: "cl_one", CreatedAt: now},
			{ID: "r2", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_one", ToClaimID: "cl_two", CreatedAt: now.Add(time.Minute)},
		},
		"cl_two": {{ID: "r2", Type: domain.RelationshipTypeContradicts, FromClaimID: "cl_one", ToClaimID: "cl_two", CreatedAt: now.Add(time.Minute)}},
	}}

	// ListByEventIDs returns only the seed claim (the others have no
	// evidence link), so without hops the answer would have one claim.
	repo.claims = []domain.Claim{allClaims[0]}
	// But ListByIDs (used during expansion) needs to find the others too —
	// stash them via a wrapper that knows both sets.
	wrapper := hopFakeClaimRepo{fakeClaimRepo: repo, all: allClaims}

	engine := NewEngine(events, wrapper, rels)

	// Hops = 0 → just the seed.
	noHops, err := engine.Answer("cache eviction policy")
	if err != nil {
		t.Fatalf("Answer(0 hops): %v", err)
	}
	if len(noHops.Claims) != 1 {
		t.Fatalf("0-hop claim count = %d, want 1", len(noHops.Claims))
	}

	// Hops = 2 → seed + 1-hop neighbor + 2-hop neighbor.
	withHops, err := engine.AnswerWithOptions("cache eviction policy", AnswerOptions{Hops: 2})
	if err != nil {
		t.Fatalf("Answer(2 hops): %v", err)
	}
	if len(withHops.Claims) != 3 {
		t.Fatalf("2-hop claim count = %d, want 3 (got: %+v)", len(withHops.Claims), withHops.Claims)
	}
	if withHops.ClaimHopDistance["cl_seed"] != 0 {
		t.Errorf("cl_seed hop distance = %d, want 0", withHops.ClaimHopDistance["cl_seed"])
	}
	if withHops.ClaimHopDistance["cl_one"] != 1 {
		t.Errorf("cl_one hop distance = %d, want 1", withHops.ClaimHopDistance["cl_one"])
	}
	if withHops.ClaimHopDistance["cl_two"] != 2 {
		t.Errorf("cl_two hop distance = %d, want 2", withHops.ClaimHopDistance["cl_two"])
	}
	if !strings.Contains(withHops.AnswerText, "Expanded 2 additional claim(s)") {
		t.Errorf("AnswerText missing expansion summary: %q", withHops.AnswerText)
	}

	// Hops = 1 → seed + only the 1-hop neighbor (cl_two should not appear).
	oneHop, err := engine.AnswerWithOptions("cache eviction policy", AnswerOptions{Hops: 1})
	if err != nil {
		t.Fatalf("Answer(1 hop): %v", err)
	}
	if len(oneHop.Claims) != 2 {
		t.Fatalf("1-hop claim count = %d, want 2", len(oneHop.Claims))
	}
	if _, expanded := oneHop.ClaimHopDistance["cl_two"]; expanded {
		t.Errorf("cl_two should not appear at hops=1")
	}
}

// hopFakeClaimRepo extends fakeClaimRepo so ListByIDs (used for
// hop-expansion) can find claims that aren't in the seed set. The
// embedded fakeClaimRepo promotes every other ports.ClaimRepository
// method, so adding new methods to that interface only requires
// touching the override list here.
type hopFakeClaimRepo struct {
	fakeClaimRepo
	all []domain.Claim
}

func (r hopFakeClaimRepo) ListByIDs(_ context.Context, ids []string) ([]domain.Claim, error) {
	wanted := map[string]struct{}{}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	out := make([]domain.Claim, 0, len(ids))
	for _, c := range r.all {
		if _, ok := wanted[c.ID]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func TestAnswer_NarrativeSurfacesStatusTransitions(t *testing.T) {
	now := time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev1", RunID: "r", Content: "Cache eviction policy.", Timestamp: now},
	}}
	claim := domain.Claim{
		ID: "cl_evo", Text: "Cache eviction is LRU",
		Type: domain.ClaimTypeDecision, Status: domain.ClaimStatusResolved,
		Confidence: 0.9, CreatedAt: now,
	}
	repo := narrativeFakeClaimRepo{
		fakeClaimRepo: fakeClaimRepo{
			claims:   []domain.Claim{claim},
			evidence: []domain.ClaimEvidence{{ClaimID: "cl_evo", EventID: "ev1"}},
		},
		history: map[string][]domain.ClaimStatusTransition{
			"cl_evo": {
				{ClaimID: "cl_evo", FromStatus: "", ToStatus: domain.ClaimStatusActive, ChangedAt: now, Reason: ""},
				{ClaimID: "cl_evo", FromStatus: domain.ClaimStatusActive, ToStatus: domain.ClaimStatusContested, ChangedAt: now.Add(72 * time.Hour), Reason: "auto: conflict with cl_fifo"},
				{ClaimID: "cl_evo", FromStatus: domain.ClaimStatusContested, ToStatus: domain.ClaimStatusResolved, ChangedAt: now.Add(144 * time.Hour), Reason: "evidence review"},
			},
		},
	}

	engine := NewEngine(events, repo, fakeRelationshipRepo{rels: map[string][]domain.Relationship{}})
	answer, err := engine.Answer("cache eviction policy")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if !strings.Contains(answer.AnswerText, "Evolution:") {
		t.Fatalf("missing Evolution section: %q", answer.AnswerText)
	}
	if !strings.Contains(answer.AnswerText, "First recorded as active") {
		t.Errorf("missing initial state in narrative")
	}
	if !strings.Contains(answer.AnswerText, "became contested") {
		t.Errorf("missing contested transition")
	}
	if !strings.Contains(answer.AnswerText, "became resolved") {
		t.Errorf("missing resolved transition")
	}
	if !strings.Contains(answer.AnswerText, "evidence review") {
		t.Errorf("missing reason text")
	}
}

type narrativeFakeClaimRepo struct {
	fakeClaimRepo
	history map[string][]domain.ClaimStatusTransition
}

func (r narrativeFakeClaimRepo) ListStatusHistoryByClaimID(_ context.Context, id string) ([]domain.ClaimStatusTransition, error) {
	return r.history[id], nil
}

func TestAnswer_AttributesProvenanceFromPulledEvent(t *testing.T) {
	now := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	events := fakeEventRepo{events: []domain.Event{
		{ID: "ev_local", RunID: "r", Content: "Local fact about cache eviction policy", Timestamp: now,
			Metadata: map[string]string{}},
		{ID: "ev_remote", RunID: "r", Content: "Remote claim about cache eviction policy", Timestamp: now.Add(time.Minute),
			Metadata: map[string]string{"pulled_from_registry": "https://reg.example.com"}},
	}}
	claims := []domain.Claim{
		{ID: "cl_local", Text: "We use LRU for cache eviction policy", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now},
		{ID: "cl_remote", Text: "Cache eviction policy is FIFO", Type: domain.ClaimTypeFact, Status: domain.ClaimStatusActive, Confidence: 0.8, CreatedAt: now.Add(time.Minute)},
	}
	repo := fakeClaimRepo{
		claims: claims,
		evidence: []domain.ClaimEvidence{
			{ClaimID: "cl_local", EventID: "ev_local"},
			{ClaimID: "cl_remote", EventID: "ev_remote"},
		},
	}

	engine := NewEngine(events, repo, fakeRelationshipRepo{rels: map[string][]domain.Relationship{}})
	answer, err := engine.Answer("cache eviction policy")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	if got := answer.ClaimProvenance["cl_local"]; got != "local" {
		t.Errorf("cl_local provenance = %q, want 'local'", got)
	}
	if got := answer.ClaimProvenance["cl_remote"]; got != "https://reg.example.com" {
		t.Errorf("cl_remote provenance = %q, want registry URL", got)
	}
	if !strings.Contains(answer.AnswerText, "from https://reg.example.com") &&
		!strings.Contains(answer.AnswerText, "from a connected registry") {
		t.Errorf("AnswerText does not surface provenance: %q", answer.AnswerText)
	}
}
