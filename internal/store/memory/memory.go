// Package memory implements an in-process [store] provider whose
// repositories live entirely in Go maps guarded by a single
// sync.RWMutex. It is designed for two use cases:
//
//  1. Fast, hermetic tests: opening a memory:// DSN replaces the
//     temp-SQLite-file pattern that the rest of the codebase still
//     uses, with no on-disk side effects.
//  2. Embedded use from Nous (the cognitive-stack coordinator) where
//     a calling process wants Mnemos in-process without standing up
//     a SQLite file.
//
// The provider implements every port-typed repository in
// [github.com/felixgeelhaar/mnemos/internal/ports] so a `Conn` opened
// here is a drop-in replacement for the SQLite Conn from a port-typed
// caller's perspective. Provider-specific extras (sql.DB raw handle,
// FTS5, sqlite-vss) are intentionally absent — callers that need
// those should open a sqlite:// DSN.
//
// See docs/adr/0001-multi-backend-storage.md for the contract.
package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store"
)

// Register the memory provider with the top-level store factory.
// Memory state is per-Open: each call to store.Open("memory://...")
// returns a fresh, empty Conn. The DSN's path/query is currently
// ignored beyond the scheme check; future work will honour
// ?namespace=foo by partitioning the maps.
func init() {
	store.Register("memory", openProvider)
}

// openProvider parses a memory:// DSN and returns a Conn with all
// port-typed repositories backed by a fresh shared state struct. The
// state lives only as long as the Conn — Close releases the
// reference, after which repositories tied to the Conn must not be
// used.
func openProvider(_ context.Context, dsn string) (*store.Conn, error) {
	if !strings.HasPrefix(dsn, "memory://") {
		return nil, fmt.Errorf("memory: not a memory dsn: %q", dsn)
	}
	st := newState()
	return &store.Conn{
		Events:        EventRepository{state: st},
		Claims:        ClaimRepository{state: st},
		Relationships: RelationshipRepository{state: st},
		Embeddings:    EmbeddingRepository{state: st},
		Users:         UserRepository{state: st},
		RevokedTokens: RevokedTokenRepository{state: st},
		Agents:        AgentRepository{state: st},
		Entities:      EntityRepository{state: st},
		Jobs:          CompilationJobRepository{state: st},
		Raw:           st,
		Closer:        func() error { st.clear(); return nil },
	}, nil
}

// state is the shared in-memory backing store. Every repository
// returned for a single Conn shares the same state pointer so writes
// through one repo are visible through another, mirroring SQLite's
// single-database semantics. The mutex protects every field; we
// favour a single coarse lock over per-field locks because the
// memory provider is for tests and embedding, not production
// throughput.
type state struct {
	mu sync.RWMutex

	events        map[string]storedEvent
	eventOrder    []string // insertion order, for ListAll
	claims        map[string]storedClaim
	claimOrder    []string
	statusHistory map[string][]storedTransition  // claim_id -> transitions in insertion order
	evidence      map[string]map[string]struct{} // claim_id -> set of event_ids (de-duped)
	relationships map[string]storedRelationship
	embeddings    map[embeddingKey]storedEmbedding
	users         map[string]storedUser
	userOrder     []string
	usersByEmail  map[string]string // email -> user_id
	revokedTokens map[string]storedRevokedToken
	agents        map[string]storedAgent
	agentOrder    []string
	entities      map[string]storedEntity
	entityOrder   []string                  // insertion order, for List
	entityByKey   map[entityKey]string      // (normalized_name, type) -> entity_id, dedup index
	claimEntities map[claimEntityKey]string // (claim_id, entity_id, role) -> role, dedup index
	jobs          map[string]storedCompilationJob
}

func newState() *state {
	return &state{
		events:        map[string]storedEvent{},
		claims:        map[string]storedClaim{},
		statusHistory: map[string][]storedTransition{},
		evidence:      map[string]map[string]struct{}{},
		relationships: map[string]storedRelationship{},
		embeddings:    map[embeddingKey]storedEmbedding{},
		users:         map[string]storedUser{},
		usersByEmail:  map[string]string{},
		revokedTokens: map[string]storedRevokedToken{},
		agents:        map[string]storedAgent{},
		entities:      map[string]storedEntity{},
		entityByKey:   map[entityKey]string{},
		claimEntities: map[claimEntityKey]string{},
		jobs:          map[string]storedCompilationJob{},
	}
}

// clear drops every collection. Called from Conn.Close so a closed
// Conn that's still reachable via a stale repo reference returns
// empty results rather than stale ones.
func (s *state) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = map[string]storedEvent{}
	s.eventOrder = nil
	s.claims = map[string]storedClaim{}
	s.claimOrder = nil
	s.statusHistory = map[string][]storedTransition{}
	s.evidence = map[string]map[string]struct{}{}
	s.relationships = map[string]storedRelationship{}
	s.embeddings = map[embeddingKey]storedEmbedding{}
	s.users = map[string]storedUser{}
	s.userOrder = nil
	s.usersByEmail = map[string]string{}
	s.revokedTokens = map[string]storedRevokedToken{}
	s.agents = map[string]storedAgent{}
	s.agentOrder = nil
	s.entities = map[string]storedEntity{}
	s.entityOrder = nil
	s.entityByKey = map[entityKey]string{}
	s.claimEntities = map[claimEntityKey]string{}
	s.jobs = map[string]storedCompilationJob{}
}

// actorOr mirrors sqlite.actorOr: an empty actor falls back to the
// SystemUser sentinel so internal write paths don't have to remember
// to set CreatedBy explicitly.
func actorOr(s string) string {
	if s == "" {
		return domain.SystemUser
	}
	return s
}
