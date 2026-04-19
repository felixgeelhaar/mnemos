package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// AgentRepository persists and retrieves non-human principals.
// Scopes are stored as a JSON-encoded array of strings: SQLite has no
// native list type and we want to keep the column scannable from
// shell/SQL one-liners without bespoke decoders.
type AgentRepository struct {
	db *sql.DB
}

// NewAgentRepository returns an AgentRepository backed by the given database.
func NewAgentRepository(db *sql.DB) AgentRepository {
	return AgentRepository{db: db}
}

// Create inserts a new agent. The owner must already exist (FK
// constraint); upstream callers should look the user up first to
// produce a friendlier error than the FK violation.
func (r AgentRepository) Create(ctx context.Context, a domain.Agent) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("invalid agent: %w", err)
	}
	scopesJSON, err := encodeScopes(a.Scopes)
	if err != nil {
		return err
	}
	runsJSON, err := encodeScopes(a.AllowedRuns) // same shape: []string → JSON
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO agents (id, name, owner_id, scopes_json, allowed_runs_json, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.OwnerID, scopesJSON, runsJSON, string(a.Status), a.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert agent %s: %w", a.ID, err)
	}
	return nil
}

// GetByID returns the agent with the given ID, or an error wrapping
// sql.ErrNoRows if not found.
func (r AgentRepository) GetByID(ctx context.Context, id string) (domain.Agent, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, scopes_json, allowed_runs_json, status, created_at FROM agents WHERE id = ?`, id)
	return scanAgentRow(row, id)
}

// List returns every agent in created_at order (oldest first).
func (r AgentRepository) List(ctx context.Context) ([]domain.Agent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, owner_id, scopes_json, allowed_runs_json, status, created_at FROM agents ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer closeRows(rows)

	out := make([]domain.Agent, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateStatus changes an agent's status (e.g., active → revoked).
// Soft delete: the row stays so historical attribution remains
// resolvable.
func (r AgentRepository) UpdateStatus(ctx context.Context, id string, status domain.AgentStatus) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update agent status %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %s: %w", id, sql.ErrNoRows)
	}
	return nil
}

// UpdateScopes replaces the agent's scope list. Existing tokens
// already in flight will continue to carry their issued scopes — only
// freshly-issued tokens see the new list.
func (r AgentRepository) UpdateScopes(ctx context.Context, id string, scopes []string) error {
	scopesJSON, err := encodeScopes(scopes)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET scopes_json = ? WHERE id = ?`, scopesJSON, id)
	if err != nil {
		return fmt.Errorf("update agent scopes %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %s: %w", id, sql.ErrNoRows)
	}
	return nil
}

// UpdateAllowedRuns replaces the agent's run whitelist. Same caveat
// as UpdateScopes: in-flight tokens keep their issued whitelist;
// only newly-issued tokens see the change.
func (r AgentRepository) UpdateAllowedRuns(ctx context.Context, id string, runs []string) error {
	runsJSON, err := encodeScopes(runs)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE agents SET allowed_runs_json = ? WHERE id = ?`, runsJSON, id)
	if err != nil {
		return fmt.Errorf("update agent allowed_runs %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %s: %w", id, sql.ErrNoRows)
	}
	return nil
}

func encodeScopes(scopes []string) (string, error) {
	if scopes == nil {
		scopes = []string{}
	}
	b, err := json.Marshal(scopes)
	if err != nil {
		return "", fmt.Errorf("encode agent scopes: %w", err)
	}
	return string(b), nil
}

type agentRowScanner interface {
	Scan(dest ...any) error
}

func scanAgentRow(row *sql.Row, id string) (domain.Agent, error) {
	a, err := scanAgent(rowOnce{row: row})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Agent{}, fmt.Errorf("agent %s: %w", id, sql.ErrNoRows)
	}
	return a, err
}

func scanAgent(s agentRowScanner) (domain.Agent, error) {
	var (
		a         domain.Agent
		scopesRaw string
		runsRaw   string
		statusStr string
		createdAt string
	)
	if err := s.Scan(&a.ID, &a.Name, &a.OwnerID, &scopesRaw, &runsRaw, &statusStr, &createdAt); err != nil {
		return domain.Agent{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("parse agent created_at: %w", err)
	}
	a.CreatedAt = t
	a.Status = domain.AgentStatus(statusStr)
	if err := json.Unmarshal([]byte(scopesRaw), &a.Scopes); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent scopes: %w", err)
	}
	if err := json.Unmarshal([]byte(runsRaw), &a.AllowedRuns); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent allowed_runs: %w", err)
	}
	return a, nil
}

// rowOnce adapts *sql.Row to the agentRowScanner interface so the
// per-row scan helper can serve both QueryRowContext and QueryContext
// callers without copy-paste.
type rowOnce struct{ row *sql.Row }

// Scan delegates to the wrapped *sql.Row so a Row participates in
// the agentRowScanner interface.
func (r rowOnce) Scan(dest ...any) error { return r.row.Scan(dest...) }
