package store

import (
	"context"
	"database/sql"
	"fmt"

	"jeviensaider/internal/model"
)

// PledgeMaxError is the error that UpdatePledgeList returns when the new
// maximum of a list is below the items that people already promised.
type PledgeMaxError struct {
	Total int // the sum of the quantities on the list
}

func (e *PledgeMaxError) Error() string {
	return fmt.Sprintf("pledge maximum is below the current total of %d items", e.Total)
}

// pledgeListCols reads a NULL minimum or maximum as 0, which means unset.
const pledgeListCols = `id, event_id, name, description, unit, drop_off, COALESCE(min_items, 0), COALESCE(max_items, 0)`

func scanPledgeList(row interface{ Scan(...any) error }) (model.PledgeList, error) {
	var l model.PledgeList
	err := row.Scan(&l.ID, &l.EventID, &l.Name, &l.Description, &l.Unit, &l.DropOff, &l.MinItems, &l.MaxItems)
	return l, err
}

// ListPledgeLists returns the pledge lists of an event, oldest first.
func (s *Store) ListPledgeLists(ctx context.Context, eventID int64) ([]model.PledgeList, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+pledgeListCols+` FROM pledge_lists WHERE event_id = ? ORDER BY id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PledgeList
	for rows.Next() {
		l, err := scanPledgeList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func getPledgeList(ctx context.Context, q querier, eventID, id int64) (model.PledgeList, error) {
	l, err := scanPledgeList(q.QueryRowContext(ctx,
		`SELECT `+pledgeListCols+` FROM pledge_lists WHERE id = ? AND event_id = ?`, id, eventID))
	return l, notFound(err)
}

// GetPledgeList returns one list of an event. It returns ErrNotFound if the
// list belongs to another event.
func (s *Store) GetPledgeList(ctx context.Context, eventID, id int64) (model.PledgeList, error) {
	return getPledgeList(ctx, s.db, eventID, id)
}

// CreatePledgeList checks l and appends it to the lists of its event. It fills
// in the ID of l.
func (s *Store) CreatePledgeList(ctx context.Context, l *model.PledgeList) error {
	if err := l.Check(); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pledge_lists WHERE event_id = ?`, l.EventID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxPledgeLists {
			return ErrTooManyPledgeLists
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO pledge_lists (event_id, name, description, unit, drop_off, min_items, max_items) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			l.EventID, l.Name, l.Description, l.Unit, l.DropOff, nullInt(l.MinItems), nullInt(l.MaxItems))
		if err != nil {
			return err
		}
		l.ID, err = res.LastInsertId()
		return err
	})
}

// UpdatePledgeList checks l and saves its name, description, unit, drop-off
// time, minimum and maximum. It returns a *PledgeMaxError if the maximum is
// below the items that people already promised.
func (s *Store) UpdatePledgeList(ctx context.Context, l model.PledgeList) error {
	if err := l.Check(); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if l.MaxItems > 0 {
			var total int
			// The join on event_id keeps the total of another event's list private.
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(p.quantity), 0) FROM pledges p JOIN pledge_lists pl ON pl.id = p.list_id
				 WHERE pl.id = ? AND pl.event_id = ?`, l.ID, l.EventID).Scan(&total); err != nil {
				return err
			}
			if total > l.MaxItems {
				return &PledgeMaxError{Total: total}
			}
		}
		return affectedOne(tx.ExecContext(ctx,
			`UPDATE pledge_lists SET name = ?, description = ?, unit = ?, drop_off = ?, min_items = ?, max_items = ? WHERE id = ? AND event_id = ?`,
			l.Name, l.Description, l.Unit, l.DropOff, nullInt(l.MinItems), nullInt(l.MaxItems), l.ID, l.EventID))
	})
}

// DeletePledgeList deletes a list with its pledges.
func (s *Store) DeletePledgeList(ctx context.Context, eventID, id int64) error {
	return affectedOne(s.db.ExecContext(ctx, `DELETE FROM pledge_lists WHERE id = ? AND event_id = ?`, id, eventID))
}
