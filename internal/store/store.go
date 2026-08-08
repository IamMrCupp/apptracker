// Package store is the SQLite-backed persistence layer for apptracker.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// Kind discriminates the two tracker modes.
const (
	KindApplication = "application"
	KindNetworking  = "networking"
)

// ErrNotFound is returned when an entry id does not exist.
var ErrNotFound = errors.New("entry not found")

// Entry is a single tracked application or networking record.
type Entry struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Lane      string `json:"lane"`
	Entity    string `json:"entity"`  // Company / Contact
	Context   string `json:"context"` // Role / Context
	Date      string `json:"date"`    // ISO yyyy-mm-dd, free-form allowed
	Channel   string `json:"channel"`
	Comp      string `json:"comp"`     // comp quoted
	FollowUp  string `json:"followUp"` // follow up on (date)
	Status    string `json:"status"`
	Link      string `json:"link"`
	Notes     string `json:"notes"` // job description / context notes
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ValidKind reports whether k is a recognised tracker mode.
func ValidKind(k string) bool {
	return k == KindApplication || k == KindNetworking
}

// Store owns the database handle.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS entries (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	kind       TEXT NOT NULL CHECK (kind IN ('application','networking')),
	lane       TEXT NOT NULL DEFAULT '',
	entity     TEXT NOT NULL DEFAULT '',
	context    TEXT NOT NULL DEFAULT '',
	date       TEXT NOT NULL DEFAULT '',
	channel    TEXT NOT NULL DEFAULT '',
	comp       TEXT NOT NULL DEFAULT '',
	follow_up  TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT '',
	link       TEXT NOT NULL DEFAULT '',
	notes      TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_entries_kind ON entries(kind);
`

// Open opens (or creates) the SQLite database at path and applies the schema.
// Use ":memory:" for an ephemeral database (tests).
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite tolerates one writer; keep a single connection to avoid "database is locked".
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) stamp() string { return s.now().UTC().Format(time.RFC3339) }

// List returns entries, optionally filtered by kind (pass "" for all), newest first.
func (s *Store) List(ctx context.Context, kind string) ([]Entry, error) {
	q := `SELECT id,kind,lane,entity,context,date,channel,comp,follow_up,status,link,notes,created_at,updated_at
	      FROM entries`
	args := []any{}
	if kind != "" {
		q += " WHERE kind = ?"
		args = append(args, kind)
	}
	q += " ORDER BY id DESC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Lane, &e.Entity, &e.Context, &e.Date,
			&e.Channel, &e.Comp, &e.FollowUp, &e.Status, &e.Link, &e.Notes,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns a single entry by id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (Entry, error) {
	var e Entry
	err := s.db.QueryRowContext(ctx,
		`SELECT id,kind,lane,entity,context,date,channel,comp,follow_up,status,link,notes,created_at,updated_at
		 FROM entries WHERE id = ?`, id).
		Scan(&e.ID, &e.Kind, &e.Lane, &e.Entity, &e.Context, &e.Date,
			&e.Channel, &e.Comp, &e.FollowUp, &e.Status, &e.Link, &e.Notes,
			&e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return e, err
}

// Create inserts a new entry and returns it with id and timestamps populated.
func (s *Store) Create(ctx context.Context, e Entry) (Entry, error) {
	if !ValidKind(e.Kind) {
		return Entry{}, fmt.Errorf("invalid kind %q", e.Kind)
	}
	e.CreatedAt = s.stamp()
	e.UpdatedAt = e.CreatedAt
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO entries (kind,lane,entity,context,date,channel,comp,follow_up,status,link,notes,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.Kind, e.Lane, e.Entity, e.Context, e.Date, e.Channel, e.Comp,
		e.FollowUp, e.Status, e.Link, e.Notes, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return Entry{}, err
	}
	e.ID, err = res.LastInsertId()
	return e, err
}

// Update replaces the mutable fields of an existing entry.
func (s *Store) Update(ctx context.Context, id int64, e Entry) (Entry, error) {
	if !ValidKind(e.Kind) {
		return Entry{}, fmt.Errorf("invalid kind %q", e.Kind)
	}
	e.UpdatedAt = s.stamp()
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET kind=?,lane=?,entity=?,context=?,date=?,channel=?,comp=?,
		 follow_up=?,status=?,link=?,notes=?,updated_at=? WHERE id=?`,
		e.Kind, e.Lane, e.Entity, e.Context, e.Date, e.Channel, e.Comp,
		e.FollowUp, e.Status, e.Link, e.Notes, e.UpdatedAt, id)
	if err != nil {
		return Entry{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Entry{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Delete removes an entry by id.
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Clear removes all entries (optionally only of a given kind).
func (s *Store) Clear(ctx context.Context, kind string) error {
	if kind == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM entries`)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE kind = ?`, kind)
	return err
}

// ReplaceAll atomically clears every entry and inserts the given set.
// Used by JSON/CSV import to restore a full snapshot.
func (s *Store) ReplaceAll(ctx context.Context, entries []Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if _, err := tx.ExecContext(ctx, `DELETE FROM entries`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO entries (kind,lane,entity,context,date,channel,comp,follow_up,status,link,notes,created_at,updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, e := range entries {
		if !ValidKind(e.Kind) {
			return fmt.Errorf("entry %d: invalid kind %q", i, e.Kind)
		}
		now := s.stamp()
		if e.CreatedAt == "" {
			e.CreatedAt = now
		}
		if e.UpdatedAt == "" {
			e.UpdatedAt = now
		}
		if _, err := stmt.ExecContext(ctx, e.Kind, e.Lane, e.Entity, e.Context,
			e.Date, e.Channel, e.Comp, e.FollowUp, e.Status, e.Link, e.Notes,
			e.CreatedAt, e.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
