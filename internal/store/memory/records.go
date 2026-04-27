package memory

import (
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// Stored records are the in-memory analogues of the SQLite rows.
// Storing domain.X directly would let callers mutate persisted state
// through a shared map slot — every write copies into a stored*
// variant and every read copies back out so the maps own the data.

type storedEvent struct {
	ID            string
	RunID         string
	SchemaVersion string
	Content       string
	SourceInputID string
	Timestamp     time.Time
	Metadata      map[string]string
	IngestedAt    time.Time
	CreatedBy     string
}

func (e storedEvent) toDomain() domain.Event {
	return domain.Event{
		ID:            e.ID,
		RunID:         e.RunID,
		SchemaVersion: e.SchemaVersion,
		Content:       e.Content,
		SourceInputID: e.SourceInputID,
		Timestamp:     e.Timestamp,
		Metadata:      copyStringMap(e.Metadata),
		IngestedAt:    e.IngestedAt,
		CreatedBy:     e.CreatedBy,
	}
}

func storedEventFromDomain(e domain.Event) storedEvent {
	return storedEvent{
		ID:            e.ID,
		RunID:         e.RunID,
		SchemaVersion: e.SchemaVersion,
		Content:       e.Content,
		SourceInputID: e.SourceInputID,
		Timestamp:     e.Timestamp.UTC(),
		Metadata:      copyStringMap(e.Metadata),
		IngestedAt:    e.IngestedAt.UTC(),
		CreatedBy:     actorOr(e.CreatedBy),
	}
}

type storedClaim struct {
	ID         string
	Text       string
	Type       domain.ClaimType
	Confidence float64
	Status     domain.ClaimStatus
	CreatedAt  time.Time
	CreatedBy  string
	TrustScore float64
	ValidFrom  time.Time
	ValidTo    time.Time
}

func (c storedClaim) toDomain() domain.Claim {
	return domain.Claim{
		ID:         c.ID,
		Text:       c.Text,
		Type:       c.Type,
		Confidence: c.Confidence,
		Status:     c.Status,
		CreatedAt:  c.CreatedAt,
		CreatedBy:  c.CreatedBy,
		TrustScore: c.TrustScore,
		ValidFrom:  c.ValidFrom,
		ValidTo:    c.ValidTo,
	}
}

func storedClaimFromDomain(c domain.Claim) storedClaim {
	validFrom := c.ValidFrom
	if validFrom.IsZero() {
		// Mirror sqlite's behaviour: when callers omit ValidFrom, the
		// claim's CreatedAt is the floor. Pipeline normally fills this
		// in earlier from the source event's timestamp.
		validFrom = c.CreatedAt
	}
	return storedClaim{
		ID:         c.ID,
		Text:       c.Text,
		Type:       c.Type,
		Confidence: c.Confidence,
		Status:     c.Status,
		CreatedAt:  c.CreatedAt.UTC(),
		CreatedBy:  actorOr(c.CreatedBy),
		TrustScore: c.TrustScore,
		ValidFrom:  validFrom.UTC(),
		ValidTo:    c.ValidTo.UTC(),
	}
}

type storedTransition struct {
	ClaimID    string
	FromStatus domain.ClaimStatus
	ToStatus   domain.ClaimStatus
	ChangedAt  time.Time
	Reason     string
	ChangedBy  string
}

type storedRelationship struct {
	ID          string
	Type        domain.RelationshipType
	FromClaimID string
	ToClaimID   string
	CreatedAt   time.Time
	CreatedBy   string
}

type embeddingKey struct {
	EntityID   string
	EntityType string
}

type storedEmbedding struct {
	EntityID   string
	EntityType string
	Vector     []float32
	Model      string
	Dimensions int
	CreatedAt  time.Time
	CreatedBy  string
}

type storedUser struct {
	ID        string
	Name      string
	Email     string
	Status    domain.UserStatus
	Scopes    []string
	CreatedAt time.Time
}

type storedRevokedToken struct {
	JTI       string
	RevokedAt time.Time
	ExpiresAt time.Time
}

type storedAgent struct {
	ID          string
	Name        string
	OwnerID     string
	Scopes      []string
	AllowedRuns []string
	Status      domain.AgentStatus
	CreatedAt   time.Time
}

type storedEntity struct {
	ID             string
	Name           string
	NormalizedName string
	Type           domain.EntityType
	CreatedAt      time.Time
	CreatedBy      string
}

func (e storedEntity) toDomain() domain.Entity {
	return domain.Entity{
		ID:             e.ID,
		Name:           e.Name,
		NormalizedName: e.NormalizedName,
		Type:           e.Type,
		CreatedAt:      e.CreatedAt,
		CreatedBy:      e.CreatedBy,
	}
}

// entityKey is the (normalized_name, type) tuple that mirrors the
// SQLite UNIQUE(normalized_name, type) index — the dedup contract
// for FindOrCreate.
type entityKey struct {
	NormalizedName string
	Type           domain.EntityType
}

// claimEntityKey is the (claim_id, entity_id, role) tuple that
// mirrors UNIQUE(claim_id, entity_id, role) on the link table.
type claimEntityKey struct {
	ClaimID  string
	EntityID string
	Role     string
}

type storedCompilationJob struct {
	ID        string
	Kind      string
	Status    string
	Scope     map[string]string
	StartedAt time.Time
	UpdatedAt time.Time
	Error     string
}

func (j storedCompilationJob) toDomain() domain.CompilationJob {
	return domain.CompilationJob{
		ID:        j.ID,
		Kind:      j.Kind,
		Status:    j.Status,
		Scope:     copyStringMap(j.Scope),
		StartedAt: j.StartedAt,
		UpdatedAt: j.UpdatedAt,
		Error:     j.Error,
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func copyFloat32Slice(in []float32) []float32 {
	if in == nil {
		return nil
	}
	out := make([]float32, len(in))
	copy(out, in)
	return out
}
