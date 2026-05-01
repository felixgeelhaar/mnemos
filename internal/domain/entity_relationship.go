package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// EntityType identifiers used by EntityRelationship for cross-entity
// edges (action ↔ outcome ↔ claim ↔ lesson ↔ playbook). Distinct from
// the canonicalised noun-phrase EntityType set used for entities; the
// two namespaces don't overlap because EntityRelationship is keyed by
// repository-table type, not graph-noun type.
const (
	RelEntityClaim    = "claim"
	RelEntityAction   = "action"
	RelEntityOutcome  = "outcome"
	RelEntityLesson   = "lesson"
	RelEntityPlaybook = "playbook"
	RelEntityDecision = "decision"
)

// EntityRelationship is a polymorphic edge between two entities of
// arbitrary type. The dedicated table sits alongside the claim-only
// relationships table so the existing graph traversal code stays
// untouched; cross-entity edges (action → outcome, outcome →
// belief-claim) live exclusively here.
//
// Kind reuses the RelationshipType set (causes, action_of,
// outcome_of, validates, refutes, derived_from). The FromType /
// ToType columns name the source/target table so a caller can
// dereference back to the originating row.
type EntityRelationship struct {
	ID        string
	Kind      RelationshipType
	FromID    string
	FromType  string
	ToID      string
	ToType    string
	CreatedAt time.Time
	CreatedBy string
}

// Validate enforces minimum invariants for persistence.
func (r EntityRelationship) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("entity_relationship id is required")
	}
	if strings.TrimSpace(r.FromID) == "" {
		return errors.New("entity_relationship from_id is required")
	}
	if strings.TrimSpace(r.ToID) == "" {
		return errors.New("entity_relationship to_id is required")
	}
	if r.FromID == r.ToID && r.FromType == r.ToType {
		return fmt.Errorf("entity_relationship %s self-references %s/%s", r.ID, r.FromType, r.FromID)
	}
	if !IsValidRelationshipType(r.Kind) {
		return fmt.Errorf("entity_relationship kind %q invalid", r.Kind)
	}
	if strings.TrimSpace(r.FromType) == "" {
		return errors.New("entity_relationship from_type is required")
	}
	if strings.TrimSpace(r.ToType) == "" {
		return errors.New("entity_relationship to_type is required")
	}
	return nil
}
