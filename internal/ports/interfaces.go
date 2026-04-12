package ports

import "github.com/felixgeelhaar/mnemos/internal/domain"

type EventRepository interface {
	Append(domain.Event) error
	GetByID(string) (domain.Event, error)
	ListByIDs([]string) ([]domain.Event, error)
}

type ClaimRepository interface {
	Upsert([]domain.Claim) error
	ListByEventIDs([]string) ([]domain.Claim, error)
}

type RelationshipRepository interface {
	Upsert([]domain.Relationship) error
	ListByClaim(string) ([]domain.Relationship, error)
}

type ExtractionEngine interface {
	ExtractClaims([]domain.Event) ([]domain.Claim, error)
}

type QueryEngine interface {
	Answer(query string) (domain.Answer, error)
}
