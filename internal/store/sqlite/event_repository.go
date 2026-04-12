package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
	"github.com/felixgeelhaar/mnemos/internal/store/sqlite/sqlcgen"
)

type EventRepository struct {
	db *sql.DB
	q  *sqlcgen.Queries
}

func NewEventRepository(db *sql.DB) EventRepository {
	return EventRepository{db: db, q: sqlcgen.New(db)}
}

func (r EventRepository) Append(event domain.Event) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal event metadata: %w", err)
	}

	err = r.q.CreateEvent(context.Background(), sqlcgen.CreateEventParams{
		ID:            event.ID,
		SchemaVersion: event.SchemaVersion,
		Content:       event.Content,
		SourceInputID: event.SourceInputID,
		Timestamp:     event.Timestamp.UTC().Format(time.RFC3339Nano),
		MetadataJson:  string(metadata),
		IngestedAt:    event.IngestedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func (r EventRepository) GetByID(id string) (domain.Event, error) {
	row, err := r.q.GetEventByID(context.Background(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, fmt.Errorf("event %s not found", id)
	}
	if err != nil {
		return domain.Event{}, err
	}

	event, err := mapSQLEvent(row)
	if err != nil {
		return domain.Event{}, err
	}
	return event, nil
}

func (r EventRepository) ListByIDs(ids []string) ([]domain.Event, error) {
	if len(ids) == 0 {
		return []domain.Event{}, nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
WHERE id IN (%s)`, strings.Join(placeholders, ","))

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events by ids: %w", err)
	}
	defer rows.Close()

	byID := map[string]domain.Event{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		byID[event.ID] = event
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events rows: %w", err)
	}

	ordered := make([]domain.Event, 0, len(ids))
	for _, id := range ids {
		event, ok := byID[id]
		if !ok {
			continue
		}
		ordered = append(ordered, event)
	}

	return ordered, nil
}

func (r EventRepository) ListAll() ([]domain.Event, error) {
	rows, err := r.q.ListAllEvents(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list all events: %w", err)
	}

	events := make([]domain.Event, 0, len(rows))
	for _, row := range rows {
		event, err := mapSQLEvent(row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func mapSQLEvent(row sqlcgen.Event) (domain.Event, error) {
	eventTimestamp, err := time.Parse(time.RFC3339Nano, row.Timestamp)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event timestamp: %w", err)
	}
	eventIngestedAt, err := time.Parse(time.RFC3339Nano, row.IngestedAt)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event ingested_at: %w", err)
	}
	metadata := map[string]string{}
	if err := json.Unmarshal([]byte(row.MetadataJson), &metadata); err != nil {
		return domain.Event{}, fmt.Errorf("unmarshal event metadata: %w", err)
	}

	return domain.Event{
		ID:            row.ID,
		SchemaVersion: row.SchemaVersion,
		Content:       row.Content,
		SourceInputID: row.SourceInputID,
		Timestamp:     eventTimestamp,
		Metadata:      metadata,
		IngestedAt:    eventIngestedAt,
	}, nil
}

type eventRowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(scanner eventRowScanner) (domain.Event, error) {
	var (
		event       domain.Event
		timestamp   string
		ingestedAt  string
		metadataRaw string
	)

	if err := scanner.Scan(
		&event.ID,
		&event.SchemaVersion,
		&event.Content,
		&event.SourceInputID,
		&timestamp,
		&metadataRaw,
		&ingestedAt,
	); err != nil {
		return domain.Event{}, err
	}

	eventTimestamp, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event timestamp: %w", err)
	}
	event.Timestamp = eventTimestamp

	eventIngestedAt, err := time.Parse(time.RFC3339Nano, ingestedAt)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event ingested_at: %w", err)
	}
	event.IngestedAt = eventIngestedAt

	metadata := map[string]string{}
	if err := json.Unmarshal([]byte(metadataRaw), &metadata); err != nil {
		return domain.Event{}, fmt.Errorf("unmarshal event metadata: %w", err)
	}
	event.Metadata = metadata

	return event, nil
}
