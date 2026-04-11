package store

import (
	"database/sql"
	"fmt"
	"time"
)

// ListSessionsOpts filters ListSessions.
type ListSessionsOpts struct {
	Cwd   string
	Limit int
}

// CreateSession inserts a new session row and returns the populated struct.
// The CWD is stored verbatim; callers should pass an absolute path.
func (d *DB) CreateSession(cwd string) (*Session, error) {
	s := &Session{
		ID:        NewSessionID(),
		Version:   3,
		Cwd:       cwd,
		CreatedAt: time.Now().UnixMilli(),
	}
	_, err := d.db.Exec(
		`INSERT INTO sessions (id, version, cwd, parent_session, created_at, name)
		 VALUES (?, ?, ?, NULL, ?, NULL)`,
		s.ID, s.Version, s.Cwd, s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create session: %w", err)
	}
	return s, nil
}

// GetSession retrieves a session by ID. Returns sql.ErrNoRows for unknown IDs.
func (d *DB) GetSession(id string) (*Session, error) {
	var (
		s             Session
		parentSession sql.NullString
		name          sql.NullString
	)
	err := d.db.QueryRow(
		`SELECT id, version, cwd, parent_session, created_at, name
		 FROM sessions WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.Version, &s.Cwd, &parentSession, &s.CreatedAt, &name)
	if err != nil {
		return nil, err
	}
	if parentSession.Valid {
		s.ParentSession = parentSession.String
	}
	if name.Valid {
		s.Name = name.String
	}
	return &s, nil
}

// ListSessions returns sessions ordered by created_at descending. Optional
// filters: exact-match cwd, and a LIMIT.
func (d *DB) ListSessions(opts ListSessionsOpts) ([]Session, error) {
	query := `SELECT id, version, cwd, parent_session, created_at, name FROM sessions`
	var args []any
	if opts.Cwd != "" {
		query += ` WHERE cwd = ?`
		args = append(args, opts.Cwd)
	}
	query += ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var (
			s             Session
			parentSession sql.NullString
			name          sql.NullString
		)
		if err := rows.Scan(&s.ID, &s.Version, &s.Cwd, &parentSession, &s.CreatedAt, &name); err != nil {
			return nil, err
		}
		if parentSession.Valid {
			s.ParentSession = parentSession.String
		}
		if name.Valid {
			s.Name = name.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateSessionName sets a human-readable label on a session.
func (d *DB) UpdateSessionName(id, name string) error {
	res, err := d.db.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("store: update session name: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSession removes a session and cascades to its entries.
func (d *DB) DeleteSession(id string) error {
	res, err := d.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
