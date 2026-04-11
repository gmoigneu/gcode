package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AppendEntry inserts a new entry. parentID may be empty to start a new
// chain from the root of the session. data is JSON-encoded (unless it is
// already a json.RawMessage). Returns the inserted entry with its
// generated ID and timestamp.
func (d *DB) AppendEntry(sessionID, parentID string, entryType EntryType, data any) (*Entry, error) {
	raw, err := encodeEntryData(data)
	if err != nil {
		return nil, err
	}

	entry := &Entry{
		ID:        NewEntryID(),
		SessionID: sessionID,
		ParentID:  parentID,
		Type:      entryType,
		Timestamp: time.Now().UnixMilli(),
		Data:      raw,
	}

	var parent any
	if parentID != "" {
		parent = parentID
	}

	_, err = d.db.Exec(
		`INSERT INTO entries (id, session_id, parent_id, type, timestamp, data)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SessionID, parent, string(entry.Type), entry.Timestamp, []byte(entry.Data),
	)
	if err != nil {
		return nil, fmt.Errorf("store: append entry: %w", err)
	}
	return entry, nil
}

// encodeEntryData turns an arbitrary Go value into a json.RawMessage. It
// accepts nil, []byte, string, json.RawMessage, or any JSON-serialisable
// value.
func encodeEntryData(data any) (json.RawMessage, error) {
	if data == nil {
		return json.RawMessage(`null`), nil
	}
	switch v := data.(type) {
	case json.RawMessage:
		return v, nil
	case []byte:
		return json.RawMessage(v), nil
	case string:
		return json.RawMessage(v), nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("store: marshal entry data: %w", err)
	}
	return raw, nil
}

// GetEntry retrieves a single entry by ID.
func (d *DB) GetEntry(id string) (*Entry, error) {
	row := d.db.QueryRow(
		`SELECT id, session_id, parent_id, type, timestamp, data FROM entries WHERE id = ?`,
		id,
	)
	return scanEntry(row)
}

// GetEntries returns every entry for a session, ordered by timestamp.
func (d *DB) GetEntries(sessionID string) ([]Entry, error) {
	rows, err := d.db.Query(
		`SELECT id, session_id, parent_id, type, timestamp, data
		 FROM entries WHERE session_id = ? ORDER BY timestamp ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get entries: %w", err)
	}
	defer rows.Close()
	return collectEntries(rows)
}

// GetChildren returns the direct children of an entry.
func (d *DB) GetChildren(entryID string) ([]Entry, error) {
	rows, err := d.db.Query(
		`SELECT id, session_id, parent_id, type, timestamp, data
		 FROM entries WHERE parent_id = ? ORDER BY timestamp ASC`,
		entryID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get children: %w", err)
	}
	defer rows.Close()
	return collectEntries(rows)
}

// GetBranch returns the root-to-leaf chain of entries ending at the given
// entry. It uses a recursive CTE walking parent_id upwards, then returns
// the list reversed (root first).
func (d *DB) GetBranch(entryID string) ([]Entry, error) {
	rows, err := d.db.Query(
		`WITH RECURSIVE branch(id, session_id, parent_id, type, timestamp, data, depth) AS (
		    SELECT id, session_id, parent_id, type, timestamp, data, 0
		    FROM entries WHERE id = ?
		    UNION ALL
		    SELECT e.id, e.session_id, e.parent_id, e.type, e.timestamp, e.data, b.depth + 1
		    FROM entries e
		    JOIN branch b ON e.id = b.parent_id
		)
		SELECT id, session_id, parent_id, type, timestamp, data
		FROM branch
		ORDER BY depth DESC`,
		entryID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get branch: %w", err)
	}
	defer rows.Close()
	return collectEntries(rows)
}

// GetLeaves returns entries in a session that have no children. The list
// is ordered by timestamp descending (most recent leaves first).
func (d *DB) GetLeaves(sessionID string) ([]Entry, error) {
	rows, err := d.db.Query(
		`SELECT e.id, e.session_id, e.parent_id, e.type, e.timestamp, e.data
		 FROM entries e
		 WHERE e.session_id = ?
		 AND NOT EXISTS (SELECT 1 FROM entries c WHERE c.parent_id = e.id)
		 ORDER BY e.timestamp DESC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get leaves: %w", err)
	}
	defer rows.Close()
	return collectEntries(rows)
}

// GetBranchEntries returns the entries on the branch ending at fromID that
// are NOT ancestors of toID. It is used by branch summarisation to extract
// the divergent portion of a side branch.
func (d *DB) GetBranchEntries(fromID, toID string) ([]Entry, error) {
	from, err := d.GetBranch(fromID)
	if err != nil {
		return nil, err
	}
	to, err := d.GetBranch(toID)
	if err != nil {
		return nil, err
	}
	toSet := make(map[string]bool, len(to))
	for _, e := range to {
		toSet[e.ID] = true
	}
	var divergent []Entry
	for _, e := range from {
		if toSet[e.ID] {
			continue
		}
		divergent = append(divergent, e)
	}
	return divergent, nil
}

// ----- scanner helpers -----

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (*Entry, error) {
	var (
		e        Entry
		parentID sql.NullString
		data     []byte
	)
	if err := row.Scan(&e.ID, &e.SessionID, &parentID, &e.Type, &e.Timestamp, &data); err != nil {
		return nil, err
	}
	if parentID.Valid {
		e.ParentID = parentID.String
	}
	e.Data = append(json.RawMessage(nil), data...)
	return &e, nil
}

func collectEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}
