package relate

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type Engine struct {
	now    func() time.Time
	nextID func() (string, error)
}

func NewEngine() Engine {
	return Engine{
		now:    time.Now,
		nextID: newRelationshipID,
	}
}

func (e Engine) Detect(claims []domain.Claim) ([]domain.Relationship, error) {
	rels := make([]domain.Relationship, 0)
	now := e.now().UTC()

	for i := 0; i < len(claims); i++ {
		for j := i + 1; j < len(claims); j++ {
			relType, ok := inferRelationship(claims[i].Text, claims[j].Text)
			if !ok {
				continue
			}

			id, err := e.nextID()
			if err != nil {
				return nil, err
			}

			rels = append(rels, domain.Relationship{
				ID:          id,
				Type:        relType,
				FromClaimID: claims[i].ID,
				ToClaimID:   claims[j].ID,
				CreatedAt:   now,
			})
		}
	}

	return rels, nil
}

func inferRelationship(a, b string) (domain.RelationshipType, bool) {
	aNorm := normalizeText(a)
	bNorm := normalizeText(b)
	if aNorm == "" || bNorm == "" {
		return "", false
	}

	if sharedTokenCount(aNorm, bNorm) == 0 {
		return "", false
	}

	aNeg := containsNegation(aNorm)
	bNeg := containsNegation(bNorm)
	if aNeg != bNeg {
		return domain.RelationshipTypeContradicts, true
	}

	return domain.RelationshipTypeSupports, true
}

func normalizeText(s string) string {
	parts := strings.Fields(strings.ToLower(s))
	return strings.Join(parts, " ")
}

func containsNegation(s string) bool {
	negations := []string{" not ", " never ", " no ", " without ", " cannot ", " can't "}
	padded := " " + s + " "
	for _, n := range negations {
		if strings.Contains(padded, n) {
			return true
		}
	}
	return false
}

func sharedTokenCount(a, b string) int {
	aSet := map[string]struct{}{}
	for _, token := range strings.Fields(a) {
		aSet[token] = struct{}{}
	}
	count := 0
	for _, token := range strings.Fields(b) {
		if _, ok := aSet[token]; ok {
			count++
		}
	}
	return count
}

func newRelationshipID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "rl_" + hex.EncodeToString(buf), nil
}
