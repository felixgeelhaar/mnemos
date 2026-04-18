package domain

import (
	"errors"
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

// Supported ClaimStatus values.
const (
	ClaimStatusActive     ClaimStatus = "active"
	ClaimStatusContested  ClaimStatus = "contested"
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
	case ClaimStatusActive, ClaimStatusContested, ClaimStatusDeprecated:
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
