// Package sqlite implements store.Store on a single SQLite database.
//
// Each entity table keeps the columns it filters, orders or dedupes on, plus a data column
// holding the full JSON of the domain value. Reads decode data; writes keep both in sync.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/bobbyrc/foundry/internal/store"
)

var _ store.Store = (*Store)(nil)

// Store is a store.Store backed by one SQLite database. It is safe for concurrent use: the pool
// holds a single connection, so all statements are serialised.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating it if needed, and applies pending migrations. The
// path ":memory:" opens a private in-memory database.
func Open(path string) (*Store, error) {
	memory := path == ":memory:"
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
	if memory {
		// Shared-cache memory DBs are named; the UUID keeps each Open private.
		dsn = "file:mem" + uuid.NewString() + "?mode=memory&cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if memory {
		for _, p := range []string{
			"PRAGMA busy_timeout = 5000",
			"PRAGMA journal_mode = WAL",
			"PRAGMA foreign_keys = ON",
			"PRAGMA synchronous = NORMAL",
		} {
			if _, err := db.ExecContext(ctx, p); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("sqlite %s: %w", p, err)
			}
		}
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// execer is the query surface shared by *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// withTx runs fn in a transaction, committing when fn returns nil and rolling back otherwise.
// fn's error is returned unwrapped.
func withTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// newID returns a time-ordered UUIDv7.
func newID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// nowUTC returns the current time in UTC, the zone every stored time is read back in.
func nowUTC() time.Time {
	return time.Now().UTC()
}

// nanos stores a time as unix nanoseconds for indexing; the zero time maps to 0.
func nanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// fromNanos is the inverse of nanos.
func fromNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// col is one indexed column and its value.
type col struct {
	name string
	val  any
}

// saveRow writes one row: the given columns plus doc as JSON in the data column. With conflict
// set, a row with the same conflict key is overwritten; otherwise a duplicate key is an error.
func saveRow(ctx context.Context, ex execer, table string, conflict []string, doc any, cols []col) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", table, err)
	}
	names := make([]string, 0, len(cols)+1)
	args := make([]any, 0, len(cols)+1)
	for _, c := range cols {
		names = append(names, c.name)
		args = append(args, c.val)
	}
	names = append(names, "data")
	args = append(args, string(data))

	var q strings.Builder
	fmt.Fprintf(&q, "INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(names, ", "), placeholders(len(names)))
	if len(conflict) > 0 {
		q.WriteString(" ON CONFLICT(" + strings.Join(conflict, ", ") + ") DO UPDATE SET ")
		var sets []string
		for _, n := range names {
			if !slices.Contains(conflict, n) {
				sets = append(sets, n+" = excluded."+n)
			}
		}
		q.WriteString(strings.Join(sets, ", "))
	}
	if _, err := ex.ExecContext(ctx, q.String(), args...); err != nil {
		return fmt.Errorf("write %s: %w", table, err)
	}
	return nil
}

// queryOne runs a query returning one data column and decodes it. notFound names the target in
// the error; a missing row wraps store.ErrNotFound.
func queryOne[T any](ctx context.Context, ex execer, notFound, query string, args ...any) (T, error) {
	var zero T
	var data string
	err := ex.QueryRowContext(ctx, query, args...).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, fmt.Errorf("%s: %w", notFound, store.ErrNotFound)
	}
	if err != nil {
		return zero, fmt.Errorf("load %s: %w", notFound, err)
	}
	return decodeJSON[T](data)
}

// queryDocs runs a query returning data columns and decodes each row. The result is never nil.
func queryDocs[T any](ctx context.Context, ex execer, query string, args ...any) ([]T, error) {
	return queryRows(ctx, ex, func(r rowScanner) (T, error) {
		var zero T
		var data string
		if err := r.Scan(&data); err != nil {
			return zero, err
		}
		return decodeJSON[T](data)
	}, query, args...)
}

// queryRows runs a query and maps each row with scan. The result is never nil.
func queryRows[T any](ctx context.Context, ex execer, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rows: %w", err)
	}
	return out, nil
}

// rowScanner is the part of *sql.Row and *sql.Rows that scan callbacks use.
type rowScanner interface {
	Scan(dest ...any) error
}

func decodeJSON[T any](data string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return v, fmt.Errorf("decode %T: %w", v, err)
	}
	return v, nil
}

// entity binds a domain type to its table. Its methods cover the common CRUD shape; the
// per-type files add the queries specific to each table.
type entity[T any] struct {
	name  string              // singular, for error messages
	table string              // table name
	id    func(*T) *string    // pointer to the ID field
	stamp func(*T, time.Time) // sets UpdatedAt; nil when the type has none
	cols  func(T) []col       // indexed columns, including id
}

// insert adds a new row; a duplicate ID is an error.
func (e entity[T]) insert(ctx context.Context, ex execer, v T) error {
	return saveRow(ctx, ex, e.table, nil, v, e.cols(v))
}

// save writes v over the row with its ID, inserting it if absent.
func (e entity[T]) save(ctx context.Context, ex execer, v T) error {
	return saveRow(ctx, ex, e.table, []string{"id"}, v, e.cols(v))
}

// get loads the row with id.
func (e entity[T]) get(ctx context.Context, ex execer, id string) (T, error) {
	return queryOne[T](ctx, ex, fmt.Sprintf("%s %s", e.name, id),
		"SELECT data FROM "+e.table+" WHERE id = ?", id)
}

// update loads the row, applies fn to a copy, stamps it, and saves it, all in one transaction.
// fn cannot change the ID. An error from fn aborts the update.
func (e entity[T]) update(ctx context.Context, db *sql.DB, id string, fn func(*T) error) (T, error) {
	var out T
	err := withTx(ctx, db, func(tx *sql.Tx) error {
		v, err := e.get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := fn(&v); err != nil {
			return fmt.Errorf("update %s %s: %w", e.name, id, err)
		}
		*e.id(&v) = id
		if e.stamp != nil {
			e.stamp(&v, nowUTC())
		}
		if err := e.save(ctx, tx, v); err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// cond accumulates WHERE clauses and their arguments in order.
type cond struct {
	clauses []string
	args    []any
}

func (c *cond) add(clause string, args ...any) {
	c.clauses = append(c.clauses, clause)
	c.args = append(c.args, args...)
}

// in adds "column IN (...)", or "NOT IN" when not is set. vals must be non-empty.
func (c *cond) in(column string, not bool, vals []any) {
	op := " IN ("
	if not {
		op = " NOT IN ("
	}
	c.add(column+op+placeholders(len(vals))+")", vals...)
}

// where returns " WHERE ..." or "" when there are no clauses.
func (c *cond) where() string {
	if len(c.clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(c.clauses, " AND ")
}

// placeholders returns n comma-separated "?" markers.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// toAny converts a typed slice to the []any that query arguments take.
func toAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// limitOr returns n, or def when n is zero or negative.
func limitOr(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}
