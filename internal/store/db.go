// Package store persists events, stands, slots and sign-ups in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strings"
	"time"

	"jeviensaider/internal/model"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Errors returned by the store. The web package turns them into messages
// for users (web.userMessage).
var (
	ErrNotFound      = errors.New("not found")
	ErrSlotFull      = errors.New("slot is full")
	ErrAlreadySigned = errors.New("already signed up for this slot")
	ErrEventNotOpen  = errors.New("event is not open for sign-ups")
	ErrTooManyStands = fmt.Errorf("event has the maximum of %d stands", MaxStands)
	ErrTooManySlots  = fmt.Errorf("event has the maximum of %d slots", MaxSlots)

	ErrTooManyMoments     = fmt.Errorf("event has the maximum of %d moments", MaxMoments)
	ErrMomentExists       = errors.New("event already has a moment at this time")
	ErrTooManyPledgeLists = fmt.Errorf("event has the maximum of %d pledge lists", MaxPledgeLists)
	ErrTooManyPledges     = fmt.Errorf("list has the maximum of %d pledges", MaxPledgesPerList)
)

// Per-event limits. They keep the planning page to a reasonable size: without
// them, one event could make every view of its page render megabytes.
// MaxPledgesPerList is a limit for each list, not for each event.
const (
	MaxStands         = 50
	MaxSlots          = 500
	MaxMoments        = 30
	MaxPledgeLists    = 10
	MaxPledgesPerList = 200
)

// querier is what queries need: a *sql.DB, or a *sql.Tx inside a
// transaction. Helpers take one so that both can share the same SQL.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies
// pending migrations.
func Open(path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate") // write transactions take the lock up front
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// One connection: SQLite has a single writer anyway, and this rules out
	// SQLITE_BUSY between our own connections. Traffic is tiny.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for i, name := range names {
		n := i + 1
		if n <= version {
			continue
		}
		body, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Token returns a random URL-safe secret (32 bytes of entropy).
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func shortID() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

func fmtTime(t time.Time) string { return t.Format(model.TimeLayout) }

func parseTime(s string) time.Time {
	for _, layout := range []string{model.TimeLayout, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// withTx runs fn in a transaction, committing if it returns nil.
func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func affectedOne(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
