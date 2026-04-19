package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/felixgeelhaar/mnemos/internal/domain"
)

// UserRepository persists and retrieves user identities.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository returns a UserRepository backed by the given database.
func NewUserRepository(db *sql.DB) UserRepository {
	return UserRepository{db: db}
}

// Create inserts a new user. Returns an error if the email is already
// taken (the schema's UNIQUE constraint on email enforces this).
func (r UserRepository) Create(ctx context.Context, u domain.User) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, name, email, status, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Name, u.Email, string(u.Status), u.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert user %s: %w", u.ID, err)
	}
	return nil
}

// GetByID returns the user with the given ID, or an error wrapping
// sql.ErrNoRows if not found.
func (r UserRepository) GetByID(ctx context.Context, id string) (domain.User, error) {
	return r.scanOne(ctx, `WHERE id = ?`, id)
}

// GetByEmail returns the user with the given email, or an error wrapping
// sql.ErrNoRows if not found. Email match is exact.
func (r UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.scanOne(ctx, `WHERE email = ?`, email)
}

// List returns all users in created_at order (oldest first). Both
// active and revoked users are returned; callers filter as needed.
func (r UserRepository) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, email, status, created_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer closeRows(rows)

	users := make([]domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateStatus changes a user's status (e.g., active → revoked). Soft
// delete: the row stays so historical created_by references remain
// resolvable.
func (r UserRepository) UpdateStatus(ctx context.Context, id string, status domain.UserStatus) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET status = ? WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update user status %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %s: %w", id, sql.ErrNoRows)
	}
	return nil
}

func (r UserRepository) scanOne(ctx context.Context, where string, args ...any) (domain.User, error) {
	//nolint:gosec // G202: where clause is one of two literal constants from internal callers, never user input
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, email, status, created_at FROM users `+where, args...)

	var (
		u         domain.User
		statusStr string
		createdAt string
	)
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &statusStr, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, fmt.Errorf("user %v: %w", args, sql.ErrNoRows)
		}
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("parse user created_at: %w", err)
	}
	u.Status = domain.UserStatus(statusStr)
	u.CreatedAt = t
	return u, nil
}

// scanUser is the row-scanner counterpart for List. Kept separate from
// scanOne because *sql.Rows and *sql.Row aren't interchangeable.
func scanUser(rows *sql.Rows) (domain.User, error) {
	var (
		u         domain.User
		statusStr string
		createdAt string
	)
	if err := rows.Scan(&u.ID, &u.Name, &u.Email, &statusStr, &createdAt); err != nil {
		return domain.User{}, fmt.Errorf("scan user row: %w", err)
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return domain.User{}, fmt.Errorf("parse user created_at: %w", err)
	}
	u.Status = domain.UserStatus(statusStr)
	u.CreatedAt = t
	return u, nil
}
