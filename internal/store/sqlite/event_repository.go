package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) EventRepository {
	return EventRepository{db: db}
}

func (r EventRepository) Append(event domain.Event) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal event metadata: %w", err)
	}

	const insert = `
INSERT INTO events (id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err = r.db.Exec(
		insert,
		event.ID,
		event.SchemaVersion,
		event.Content,
		event.SourceInputID,
		event.Timestamp.UTC().Format(time.RFC3339Nano),
		string(metadata),
		event.IngestedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func (r EventRepository) GetByID(id string) (domain.Event, error) {
	const query = `
SELECT id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
WHERE id = ?`

	event, err := scanEvent(r.db.QueryRow(query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, fmt.Errorf("event %s not found", id)
	}
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
	const query = `
SELECT id, schema_version, content, source_input_id, timestamp, metadata_json, ingested_at
FROM events
ORDER BY timestamp ASC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list all events: %w", err)
	}
	defer rows.Close()

	events := make([]domain.Event, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all events rows: %w", err)
	}

	return events, nil
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
