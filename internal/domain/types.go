package domain

import (
	"errors"
	"strings"
	"time"
)

type InputType string

const (
	InputTypeText       InputType = "text"
	InputTypeJSON       InputType = "json"
	InputTypeCSV        InputType = "csv"
	InputTypeMD         InputType = "md"
	InputTypeTranscript InputType = "transcript"
)

type ClaimType string

const (
	ClaimTypeFact       ClaimType = "fact"
	ClaimTypeHypothesis ClaimType = "hypothesis"
	ClaimTypeDecision   ClaimType = "decision"
)

type ClaimStatus string

const (
	ClaimStatusActive     ClaimStatus = "active"
	ClaimStatusContested  ClaimStatus = "contested"
	ClaimStatusDeprecated ClaimStatus = "deprecated"
)

type RelationshipType string

const (
	RelationshipTypeSupports    RelationshipType = "supports"
	RelationshipTypeContradicts RelationshipType = "contradicts"
)

type Input struct {
	ID        string
	Type      InputType
	Format    string
	Metadata  map[string]string
	CreatedAt time.Time
}

type Event struct {
	ID            string
	SchemaVersion string
	Content       string
	SourceInputID string
	Timestamp     time.Time
	Metadata      map[string]string
	IngestedAt    time.Time
}

type Claim struct {
	ID         string
	Text       string
	Type       ClaimType
	Confidence float64
	Status     ClaimStatus
	CreatedAt  time.Time
}

type ClaimEvidence struct {
	ClaimID string
	EventID string
}

type Relationship struct {
	ID          string
	Type        RelationshipType
	FromClaimID string
	ToClaimID   string
	CreatedAt   time.Time
}

type CompilationJob struct {
	ID        string
	Kind      string
	Status    string
	Scope     map[string]string
	StartedAt time.Time
	UpdatedAt time.Time
	Error     string
}

type Answer struct {
	AnswerText       string
	Claims           []Claim
	Contradictions   []Relationship
	TimelineEventIDs []string
}

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

func (e ClaimEvidence) Validate() error {
	if strings.TrimSpace(e.ClaimID) == "" {
		return errors.New("claim evidence claim_id is required")
	}
	if strings.TrimSpace(e.EventID) == "" {
		return errors.New("claim evidence event_id is required")
	}
	return nil
}
