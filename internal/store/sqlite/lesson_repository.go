package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

// LessonRepository provides SQLite-backed storage for synthesised
// operational lessons.
type LessonRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

// NewLessonRepository returns a LessonRepository backed by db.
func NewLessonRepository(db *sql.DB) LessonRepository {
	return LessonRepository{db: db, q: sqlcgen.New(db)}
}

// Append upserts the lesson row. Re-appending the same id refreshes
// statement, confidence, derived_at, and last_verified — the
// synthesis layer relies on this to ratchet a lesson's confidence
// upward as new evidence accumulates.
func (r LessonRepository) Append(ctx context.Context, lesson domain.Lesson) error {
	if err := lesson.Validate(); err != nil {
		return fmt.Errorf("invalid lesson: %w", err)
	}
	source := lesson.Source
	if source == "" {
		source = "synthesize"
	}
	lastVerified := ""
	if !lesson.LastVerified.IsZero() {
		lastVerified = lesson.LastVerified.UTC().Format(time.RFC3339Nano)
	}
	if err := r.q.CreateLesson(ctx, sqlcgen.CreateLessonParams{
		ID:           lesson.ID,
		Statement:    lesson.Statement,
		ScopeService: lesson.Scope.Service,
		ScopeEnv:     lesson.Scope.Env,
		ScopeTeam:    lesson.Scope.Team,
		Trigger:      lesson.Trigger,
		Kind:         lesson.Kind,
		Confidence:   lesson.Confidence,
		DerivedAt:    lesson.DerivedAt.UTC().Format(time.RFC3339Nano),
		LastVerified: lastVerified,
		Source:       source,
		CreatedBy:    actorOr(lesson.CreatedBy),
	}); err != nil {
		return fmt.Errorf("insert lesson: %w", err)
	}
	if len(lesson.Evidence) > 0 {
		if err := r.AppendEvidence(ctx, lesson.ID, lesson.Evidence); err != nil {
			return err
		}
	}
	return nil
}

// GetByID returns the lesson with the given id.
func (r LessonRepository) GetByID(ctx context.Context, id string) (domain.Lesson, error) {
	row, err := r.q.GetLessonByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Lesson{}, fmt.Errorf("lesson %s not found", id)
	}
	if err != nil {
		return domain.Lesson{}, err
	}
	l, err := mapSQLLesson(row)
	if err != nil {
		return domain.Lesson{}, err
	}
	evidence, err := r.ListEvidence(ctx, id)
	if err != nil {
		return domain.Lesson{}, err
	}
	l.Evidence = evidence
	return l, nil
}

// ListByService returns lessons scoped to a single service, highest
// confidence first.
func (r LessonRepository) ListByService(ctx context.Context, service string) ([]domain.Lesson, error) {
	rows, err := r.q.ListLessonsByService(ctx, service)
	if err != nil {
		return nil, err
	}
	return r.hydrateLessons(ctx, rows)
}

// ListByTrigger returns lessons that share a trigger label, highest
// confidence first. Used by the playbook synthesis layer (Phase 6) to
// find the lessons backing a given trigger pattern.
func (r LessonRepository) ListByTrigger(ctx context.Context, trigger string) ([]domain.Lesson, error) {
	rows, err := r.q.ListLessonsByTrigger(ctx, trigger)
	if err != nil {
		return nil, err
	}
	return r.hydrateLessons(ctx, rows)
}

// ListAll returns every lesson, highest confidence first.
func (r LessonRepository) ListAll(ctx context.Context) ([]domain.Lesson, error) {
	rows, err := r.q.ListAllLessons(ctx)
	if err != nil {
		return nil, err
	}
	return r.hydrateLessons(ctx, rows)
}

// CountAll returns the total number of lessons stored.
func (r LessonRepository) CountAll(ctx context.Context) (int64, error) {
	return r.q.CountLessons(ctx)
}

// DeleteAll wipes lessons + lesson_evidence. Evidence is dropped first
// so the FK constraint stays happy on engines that enforce it.
func (r LessonRepository) DeleteAll(ctx context.Context) error {
	if err := r.q.DeleteAllLessonEvidence(ctx); err != nil {
		return fmt.Errorf("delete all lesson evidence: %w", err)
	}
	return r.q.DeleteAllLessons(ctx)
}

// AppendEvidence inserts (lesson_id, action_id) rows. Idempotent on
// the composite key — duplicate evidence collapses silently.
func (r LessonRepository) AppendEvidence(ctx context.Context, lessonID string, actionIDs []string) error {
	for _, aid := range actionIDs {
		if err := r.q.AppendLessonEvidence(ctx, sqlcgen.AppendLessonEvidenceParams{
			LessonID: lessonID,
			ActionID: aid,
		}); err != nil {
			return fmt.Errorf("append lesson evidence: %w", err)
		}
	}
	return nil
}

// ListEvidence returns the action ids backing a given lesson.
func (r LessonRepository) ListEvidence(ctx context.Context, lessonID string) ([]string, error) {
	rows, err := r.q.ListLessonEvidence(ctx, lessonID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ActionID)
	}
	return out, nil
}

func (r LessonRepository) hydrateLessons(ctx context.Context, rows []sqlcgen.Lesson) ([]domain.Lesson, error) {
	out := make([]domain.Lesson, 0, len(rows))
	for _, row := range rows {
		l, err := mapSQLLesson(row)
		if err != nil {
			return nil, err
		}
		ev, err := r.ListEvidence(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		l.Evidence = ev
		out = append(out, l)
	}
	return out, nil
}

func mapSQLLesson(row sqlcgen.Lesson) (domain.Lesson, error) {
	derived, err := time.Parse(time.RFC3339Nano, row.DerivedAt)
	if err != nil {
		return domain.Lesson{}, fmt.Errorf("parse lesson.derived_at: %w", err)
	}
	var lastVerified time.Time
	if row.LastVerified != "" {
		t, err := time.Parse(time.RFC3339Nano, row.LastVerified)
		if err != nil {
			return domain.Lesson{}, fmt.Errorf("parse lesson.last_verified: %w", err)
		}
		lastVerified = t
	}
	return domain.Lesson{
		ID:           row.ID,
		Statement:    row.Statement,
		Scope:        domain.LessonScope{Service: row.ScopeService, Env: row.ScopeEnv, Team: row.ScopeTeam},
		Trigger:      row.Trigger,
		Kind:         row.Kind,
		Confidence:   row.Confidence,
		DerivedAt:    derived,
		LastVerified: lastVerified,
		Source:       row.Source,
		CreatedBy:    row.CreatedBy,
	}, nil
}
