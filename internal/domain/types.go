package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// InputType represents the format classification of an ingested input.
type InputType string

// Supported InputType values.
const (
	InputTypeText       InputType = "text"
	InputTypeJSON       InputType = "json"
	InputTypeCSV        InputType = "csv"
	InputTypeMD         InputType = "md"
	InputTypeTranscript InputType = "transcript"
)

// ClaimType categorises a claim as a fact, hypothesis, or decision.
type ClaimType string

// Supported ClaimType values.
const (
	ClaimTypeFact       ClaimType = "fact"
	ClaimTypeHypothesis ClaimType = "hypothesis"
	ClaimTypeDecision   ClaimType = "decision"
)

// ClaimStatus represents the lifecycle state of a claim.
type ClaimStatus string

// Supported ClaimStatus values. The lifecycle reads as:
//
//	active → contested (when a contradicting claim lands)
//	contested → resolved (when an operator picks a winner)
//	contested → deprecated (when the loser of a resolution is retired)
//	any → deprecated (when a claim is manually withdrawn)
//
// Status transitions are recorded in claim_status_history — see
// ClaimRepository.ListStatusHistoryByClaimID.
const (
	ClaimStatusActive     ClaimStatus = "active"
	ClaimStatusContested  ClaimStatus = "contested"
	ClaimStatusResolved   ClaimStatus = "resolved"
	ClaimStatusDeprecated ClaimStatus = "deprecated"
)

// RelationshipType describes how two claims are related.
type RelationshipType string

// Supported RelationshipType values.
//
// The original pair (supports / contradicts) expresses logical agreement
// between claims. The causal+outcome family extends the graph to express
// real-world dynamics: which action produced which observed state, which
// hypothesis was validated or refuted by which outcome, and which
// synthesised lesson was derived from which raw claim. The graph is
// directional; semantics live on the From -> To direction.
const (
	RelationshipTypeSupports    RelationshipType = "supports"
	RelationshipTypeContradicts RelationshipType = "contradicts"

	// RelationshipTypeCauses asserts that From caused To (cause -> effect).
	RelationshipTypeCauses RelationshipType = "causes"
	// RelationshipTypeCausedBy is the inverse of Causes (effect -> cause)
	// stored explicitly so reverse traversal stays a single index lookup.
	RelationshipTypeCausedBy RelationshipType = "caused_by"
	// RelationshipTypeActionOf links an action claim to the outcome claim
	// it produced (action -> outcome). Used by the action+outcome layer.
	RelationshipTypeActionOf RelationshipType = "action_of"
	// RelationshipTypeOutcomeOf is the inverse of ActionOf (outcome -> action).
	RelationshipTypeOutcomeOf RelationshipType = "outcome_of"
	// RelationshipTypeValidates asserts From validates To, e.g. an outcome
	// claim validates a hypothesis claim it was meant to test.
	RelationshipTypeValidates RelationshipType = "validates"
	// RelationshipTypeRefutes asserts From refutes To, the negative
	// counterpart to Validates.
	RelationshipTypeRefutes RelationshipType = "refutes"
	// RelationshipTypeDerivedFrom links a synthesised claim (typically a
	// lesson or playbook step) back to the raw claim it was generalised
	// from, preserving provenance through the synthesis layer.
	RelationshipTypeDerivedFrom RelationshipType = "derived_from"
)

// IsValidRelationshipType reports whether t is a recognised relationship
// type. Validation paths use this rather than open-coding the switch so
// future additions only need to update the const block plus this helper.
func IsValidRelationshipType(t RelationshipType) bool {
	switch t {
	case RelationshipTypeSupports,
		RelationshipTypeContradicts,
		RelationshipTypeCauses,
		RelationshipTypeCausedBy,
		RelationshipTypeActionOf,
		RelationshipTypeOutcomeOf,
		RelationshipTypeValidates,
		RelationshipTypeRefutes,
		RelationshipTypeDerivedFrom:
		return true
	}
	return false
}

// Input represents a raw document or data source submitted for ingestion.
type Input struct {
	ID        string
	Type      InputType
	Format    string
	Metadata  map[string]string
	CreatedAt time.Time
}

// Event represents a single timestamped piece of knowledge extracted from an input.
type Event struct {
	ID            string
	RunID         string
	SchemaVersion string
	Content       string
	SourceInputID string
	Timestamp     time.Time
	Metadata      map[string]string
	IngestedAt    time.Time
	CreatedBy     string // user id of the actor that created this event; "<system>" for unattributed
}

// Claim represents an assertion derived from one or more events,
// carrying a type, confidence score, and lifecycle status.
type Claim struct {
	ID         string
	Text       string
	Type       ClaimType
	Confidence float64
	Status     ClaimStatus
	CreatedAt  time.Time
	CreatedBy  string  // user id of the actor that created this claim; "<system>" for unattributed
	TrustScore float64 // derived from confidence × corroboration × freshness; computed by internal/trust

	// ValidFrom is when the claim's content first became true. Defaults
	// to the source event's timestamp at insert time; see internal/pipeline.
	// A zero value means "valid since before the system started tracking".
	ValidFrom time.Time
	// ValidTo is when the claim stopped being true (a successor claim
	// took its place). Zero value means "currently valid / still in
	// force". Set by `mnemos resolve --supersedes` or by future
	// auto-supersession detection.
	ValidTo time.Time

	// LastVerified ticks forward each time `mnemos verify` (or the
	// MCP equivalent) re-confirms the claim against fresh evidence.
	// Zero value means "never explicitly verified" — the freshness
	// factor falls back to the latest evidence event's timestamp.
	LastVerified time.Time
	// VerifyCount counts every successful re-verification. Used as a
	// secondary trust input when ranking near-tied claims.
	VerifyCount int
	// HalfLifeDays optionally overrides the global trust freshness
	// half-life on a per-claim basis. Zero falls back to the
	// internal/trust default. Useful for facts whose decay profile
	// genuinely differs from the project default — e.g. a SLA that
	// becomes stale in 7 days vs an architectural decision good for
	// a year.
	HalfLifeDays float64

	// Scope optionally narrows the claim to a specific operational
	// context (service, env, team). Empty scope (the zero value)
	// means "applies everywhere". The query engine filters by scope
	// when Answer is requested with a non-empty filter; synthesis
	// already routes through claim->action->lesson scope upstream.
	Scope Scope
}

// IsValidAt reports whether the claim was in force at instant t.
// A claim is in force while ValidFrom ≤ t and (ValidTo is zero OR t < ValidTo).
// Zero ValidFrom counts as "valid from the beginning of time" so legacy
// rows that predate v0.8 still answer "yes" to current queries.
func (c Claim) IsValidAt(t time.Time) bool {
	if !c.ValidFrom.IsZero() && t.Before(c.ValidFrom) {
		return false
	}
	if !c.ValidTo.IsZero() && !t.Before(c.ValidTo) {
		return false
	}
	return true
}

// IsSuperseded reports whether the claim has been replaced by another
// (i.e., ValidTo is set). Useful for filtering history-aware queries.
func (c Claim) IsSuperseded() bool {
	return !c.ValidTo.IsZero()
}

// ClaimEvidence links a Claim to the Event that supports it.
type ClaimEvidence struct {
	ClaimID string
	EventID string
}

// Relationship represents a directed edge between two claims.
type Relationship struct {
	ID          string
	Type        RelationshipType
	FromClaimID string
	ToClaimID   string
	CreatedAt   time.Time
	CreatedBy   string // user id of the actor that created this relationship
}

// CompilationJob tracks the state of an asynchronous compilation task.
type CompilationJob struct {
	ID        string
	Kind      string
	Status    string
	Scope     map[string]string
	StartedAt time.Time
	UpdatedAt time.Time
	Error     string
}

// ClaimStatusTransition records a single status change on a claim. An
// ordered series of these forms a claim's lifecycle history: when a claim
// first appears as active, when it becomes contested, when it resolves or
// is deprecated, and why.
type ClaimStatusTransition struct {
	ClaimID    string
	FromStatus ClaimStatus // empty string means "initial state, no prior"
	ToStatus   ClaimStatus
	ChangedAt  time.Time
	Reason     string // free-form human context: "auto: contradiction detected", "resolved via mnemos resolve", etc.
	ChangedBy  string // user id of the actor that triggered the transition
}

// SystemUser is the sentinel actor recorded on rows that were written by
// internal pipelines or pre-A.2 data (no real user identity attached).
// Treated specially by the audit and narrative output paths so it reads
// as "system" rather than as an unknown user id.
const SystemUser = "<system>"

// EntityType classifies a first-class node in the knowledge graph.
// Entities exist independently of the claims that mention them so we
// can answer "what do we know about X?" without scanning every claim.
// The set is intentionally small: a longer ontology trades user
// confusion for a marginal gain in retrieval. Future versions can add
// types when there's clear demand.
type EntityType string

// Canonical EntityType values. The set is intentionally small; new
// types should be added only when a real corpus needs them.
const (
	EntityTypePerson  EntityType = "person"
	EntityTypeOrg     EntityType = "org"
	EntityTypeProject EntityType = "project"
	EntityTypeProduct EntityType = "product"
	EntityTypePlace   EntityType = "place"
	EntityTypeConcept EntityType = "concept"
)

// Entity is a canonicalised noun-phrase that appears across one or more
// claims. The (NormalizedName, Type) pair is the dedup key; Name keeps
// the human-readable casing.
type Entity struct {
	ID             string
	Name           string
	NormalizedName string // lower-cased, whitespace-collapsed; the dedup key
	Type           EntityType
	CreatedAt      time.Time
	CreatedBy      string
}

// ClaimEntity links a Claim to an Entity. The Role field describes how
// the entity participates: "subject" (the claim is *about* this entity),
// "object" (the entity is acted on or referenced), "mention" (a passing
// reference, the default). Querying by entity returns claims regardless
// of role; the field is informational for now.
type ClaimEntity struct {
	ClaimID  string
	EntityID string
	Role     string
}

// NormalizeEntityName produces the dedup key for an entity name:
// lower-case, trimmed, internal whitespace collapsed. Kept in domain
// rather than storage because both extraction and querying need to
// produce the same key from raw input.
func NormalizeEntityName(name string) string {
	out := strings.ToLower(strings.TrimSpace(name))
	// Collapse runs of whitespace to a single space so "Felix  Geelhaar"
	// and "Felix Geelhaar" hash to the same canonical form.
	parts := strings.Fields(out)
	return strings.Join(parts, " ")
}

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

// Supported UserStatus values.
const (
	UserStatusActive  UserStatus = "active"
	UserStatusRevoked UserStatus = "revoked"
)

// User represents a human or service identity that can authenticate
// against the Mnemos registry. Tokens are issued to users; every
// audit-bearing action records the issuing user as created_by.
//
// Scopes is the authorisation list embedded into tokens issued for
// this user. Empty scopes is treated as the legacy default (full
// access via "*") so pre-F.3 users keep working — F.3 added the
// column with a default of '["*"]', and unmarshalled empty slices
// are interpreted the same way at issuance time.
type User struct {
	ID        string
	Name      string
	Email     string
	Status    UserStatus
	Scopes    []string
	CreatedAt time.Time
}

// Validate checks that a User has the minimum required fields. Email
// uniqueness is enforced at the storage layer.
func (u User) Validate() error {
	if strings.TrimSpace(u.ID) == "" {
		return errors.New("user id is required")
	}
	if strings.TrimSpace(u.Name) == "" {
		return errors.New("user name is required")
	}
	if strings.TrimSpace(u.Email) == "" {
		return errors.New("user email is required")
	}
	if u.Status == "" {
		return errors.New("user status is required")
	}
	switch u.Status {
	case UserStatusActive, UserStatusRevoked:
	default:
		return fmt.Errorf("invalid user status %q", u.Status)
	}
	for _, s := range u.Scopes {
		if strings.TrimSpace(s) == "" {
			return errors.New("user scope entries must be non-empty")
		}
	}
	return nil
}

// RevokedToken records that a particular JWT (identified by its jti
// claim) is no longer valid before its natural expiry. Auth middleware
// consults this denylist on every request. Rows older than expires_at
// can be purged because the token would have expired anyway.
type RevokedToken struct {
	JTI       string
	RevokedAt time.Time
	ExpiresAt time.Time
}

// AgentStatus mirrors UserStatus for non-human principals.
type AgentStatus string

// Supported AgentStatus values.
const (
	AgentStatusActive  AgentStatus = "active"
	AgentStatusRevoked AgentStatus = "revoked"
)

// Scope strings recognised by the auth middleware. "*" matches every
// scope. Resource-level scopes follow `<resource>:<verb>` so future
// additions stay grep-friendly.
const (
	ScopeWildcard           = "*"
	ScopeEventsWrite        = "events:write"
	ScopeClaimsWrite        = "claims:write"
	ScopeRelationshipsWrite = "relationships:write"
	ScopeEmbeddingsWrite    = "embeddings:write"
)

// Agent represents a non-human principal — a coding assistant, CI job,
// or other automated identity. Agents always have an owning user (so
// every action traces back to a human accountable party) and an
// explicit scope list. There is no "implicit *" for agents: tokens
// issued for an agent carry exactly the scopes recorded on the agent,
// nothing more.
//
// AllowedRuns optionally restricts the agent to a whitelist of run
// ids. Empty list means "every run is allowed" — F.x can extend with
// glob patterns later. The whitelist gates write paths that carry a
// run_id (today: events); claim/relationship/embedding writes
// indirectly inherit because the agent must be able to seed the
// underlying event first.
type Agent struct {
	ID          string
	Name        string
	OwnerID     string // user_id of the human accountable for this agent
	Scopes      []string
	AllowedRuns []string
	Status      AgentStatus
	CreatedAt   time.Time
}

// Validate enforces the minimum invariants for a persistable Agent.
// Scope strings are not validated against the constant list — agents
// may legitimately carry forward-compatible scopes the current binary
// doesn't yet recognise.
func (a Agent) Validate() error {
	if strings.TrimSpace(a.ID) == "" {
		return errors.New("agent id is required")
	}
	if strings.TrimSpace(a.Name) == "" {
		return errors.New("agent name is required")
	}
	if strings.TrimSpace(a.OwnerID) == "" {
		return errors.New("agent owner_id is required")
	}
	if a.Status == "" {
		return errors.New("agent status is required")
	}
	switch a.Status {
	case AgentStatusActive, AgentStatusRevoked:
	default:
		return fmt.Errorf("invalid agent status %q", a.Status)
	}
	for _, s := range a.Scopes {
		if strings.TrimSpace(s) == "" {
			return errors.New("agent scope entries must be non-empty")
		}
	}
	for _, r := range a.AllowedRuns {
		if strings.TrimSpace(r) == "" {
			return errors.New("agent allowed_runs entries must be non-empty")
		}
	}
	return nil
}

// EmbeddingRecord holds a stored vector embedding with its metadata.
type EmbeddingRecord struct {
	EntityID   string
	EntityType string
	Vector     []float32
	Model      string
	Dimensions int
	CreatedAt  time.Time // when this vector was last (re)written; used by audit-who principal scans
	CreatedBy  string    // user id of the actor that generated this embedding
}

// Answer holds the result of a query, including supporting claims and contradictions.
type Answer struct {
	AnswerText       string
	Claims           []Claim
	Contradictions   []Relationship
	TimelineEventIDs []string
	// ClaimProvenance maps claim ID to a human-readable origin: "local"
	// for claims sourced from this project's events, or "<registry-url>"
	// for claims that reached the local DB via `mnemos pull`. Empty means
	// unknown — the engine fills this in when it can.
	ClaimProvenance map[string]string
	// ClaimHopDistance maps claim ID to the BFS hop distance from the
	// directly-retrieved claims. 0 means the claim came from the top-ranked
	// events; 1 means it was reached by following one supports/contradicts
	// edge from a hop-0 claim, etc. Empty when hop expansion was not
	// requested.
	ClaimHopDistance map[string]int
	// StaleClaimIDs lists claim ids whose freshness factor has
	// decayed below the trust floor — i.e. the most recent evidence
	// or last_verified signal is old enough that the claim should
	// not be acted on without re-verification. Empty when no claims
	// in the answer are stale; nil when the engine could not compute
	// staleness (e.g. timestamps absent).
	StaleClaimIDs []string
}

// Validate checks that the Claim has a non-empty ID and text, a confidence
// between 0 and 1, and a valid type and status.
func (c Claim) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("claim id is required")
	}
	if strings.TrimSpace(c.Text) == "" {
		return errors.New("claim text is required")
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return errors.New("claim confidence must be between 0 and 1")
	}
	switch c.Type {
	case ClaimTypeFact, ClaimTypeHypothesis, ClaimTypeDecision:
	default:
		return errors.New("claim type is invalid")
	}
	switch c.Status {
	case ClaimStatusActive, ClaimStatusContested, ClaimStatusResolved, ClaimStatusDeprecated:
	default:
		return errors.New("claim status is invalid")
	}
	return nil
}

// Validate checks that both ClaimID and EventID are non-empty.
func (e ClaimEvidence) Validate() error {
	if strings.TrimSpace(e.ClaimID) == "" {
		return errors.New("claim evidence claim_id is required")
	}
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("claim evidence event_id is required")
	}
	return nil
}

// MaxEventContentBytes caps a single event's Content field. Events
// are meant to be small paragraph-sized fragments of knowledge, not
// entire documents — the ingest pipeline normalises documents into
// sentence-level events. Anything much larger almost always means
// the caller didn't chunk properly, and an unbounded field wastes
// DB/index space plus makes extraction latency pathological. Keep
// this comfortably larger than a typical paragraph (2KB) so
// legitimate edge cases still fit.
const MaxEventContentBytes = 64 * 1024

// Validate checks that an Event has the minimum required fields and
// that Content stays within MaxEventContentBytes.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("event id is required")
	}
	if strings.TrimSpace(e.Content) == "" {
		return errors.New("event content is required")
	}
	if len(e.Content) > MaxEventContentBytes {
		return fmt.Errorf("event content is %d bytes, max is %d (chunk longer documents into multiple events)", len(e.Content), MaxEventContentBytes)
	}
	if strings.TrimSpace(e.SourceInputID) == "" {
		return errors.New("event source_input_id is required")
	}
	if e.Timestamp.IsZero() {
		return errors.New("event timestamp is required")
	}
	return nil
}

// Validate checks that a Relationship has the required fields and a
// valid type, and that it doesn't self-reference (a claim can't
// support or contradict itself — that's either a no-op or a bug).
func (r Relationship) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("relationship id is required")
	}
	if strings.TrimSpace(r.FromClaimID) == "" {
		return errors.New("relationship from_claim_id is required")
	}
	if strings.TrimSpace(r.ToClaimID) == "" {
		return errors.New("relationship to_claim_id is required")
	}
	if r.FromClaimID == r.ToClaimID {
		return fmt.Errorf("relationship %s self-references claim %s", r.ID, r.FromClaimID)
	}
	if !IsValidRelationshipType(r.Type) {
		return fmt.Errorf("relationship type %q invalid", r.Type)
	}
	return nil
}
