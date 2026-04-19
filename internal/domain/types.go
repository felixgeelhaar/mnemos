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
const (
	RelationshipTypeSupports    RelationshipType = "supports"
	RelationshipTypeContradicts RelationshipType = "contradicts"
)

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
type User struct {
	ID        string
	Name      string
	Email     string
	Status    UserStatus
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

// EmbeddingRecord holds a stored vector embedding with its metadata.
type EmbeddingRecord struct {
	EntityID   string
	EntityType string
	Vector     []float32
	Model      string
	Dimensions int
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
