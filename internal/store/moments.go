package store

import (
	"context"
	"database/sql"
	"strings"

	"jeviensaider/internal/model"
)

const momentCols = `id, event_id, title, color, fill, starts_at, ends_at`

func scanMoment(row interface{ Scan(...any) error }) (model.Moment, error) {
	var m model.Moment
	var start, end string
	err := row.Scan(&m.ID, &m.EventID, &m.Title, &m.Color, &m.Fill, &start, &end)
	m.StartsAt, m.EndsAt = parseTime(start), parseTime(end)
	return m, err
}

func listMoments(ctx context.Context, q querier, eventID int64) ([]model.Moment, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+momentCols+` FROM moments WHERE event_id = ? ORDER BY starts_at, ends_at, id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Moment
	for rows.Next() {
		m, err := scanMoment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMoments returns the moments of an event, by start and then by end.
func (s *Store) ListMoments(ctx context.Context, eventID int64) ([]model.Moment, error) {
	return listMoments(ctx, s.db, eventID)
}

// GetMoment returns a moment, checking that it belongs to the event.
func (s *Store) GetMoment(ctx context.Context, eventID, id int64) (model.Moment, error) {
	m, err := scanMoment(s.db.QueryRowContext(ctx,
		`SELECT `+momentCols+` FROM moments WHERE id = ? AND event_id = ?`, id, eventID))
	return m, notFound(err)
}

// CreateMoment inserts a moment in m.EventID and sets m.ID. It returns
// ErrTooManyMoments at MaxMoments, and ErrMomentExists if the event has a
// moment with the same start and end.
func (s *Store) CreateMoment(ctx context.Context, m *model.Moment) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM moments WHERE event_id = ?`, m.EventID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxMoments {
			return ErrTooManyMoments
		}
		id, err := insertMoment(ctx, tx, *m)
		if err != nil {
			return err
		}
		m.ID = id
		return nil
	})
}

// insertMoment adds a moment and returns its ID.
func insertMoment(ctx context.Context, q querier, m model.Moment) (int64, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO moments (event_id, title, color, fill, starts_at, ends_at) VALUES (?, ?, ?, ?, ?, ?)`,
		m.EventID, m.Title, m.Color, m.Fill, fmtTime(m.StartsAt), fmtTime(m.EndsAt))
	if err != nil {
		return 0, momentError(err)
	}
	return res.LastInsertId()
}

// UpdateMoment saves the title, the color, the fill and the times of a moment of m.EventID.
func (s *Store) UpdateMoment(ctx context.Context, m model.Moment) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE moments SET title = ?, color = ?, fill = ?, starts_at = ?, ends_at = ? WHERE id = ? AND event_id = ?`,
		m.Title, m.Color, m.Fill, fmtTime(m.StartsAt), fmtTime(m.EndsAt), m.ID, m.EventID)
	return affectedOne(res, momentError(err))
}

// DeleteMoment deletes a moment of the event.
func (s *Store) DeleteMoment(ctx context.Context, eventID, id int64) error {
	return affectedOne(s.db.ExecContext(ctx,
		`DELETE FROM moments WHERE id = ? AND event_id = ?`, id, eventID))
}

// momentError turns the unique constraint error into ErrMomentExists.
func momentError(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrMomentExists
	}
	return err
}
