package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"jeviensaider/internal/model"
)

// ErrPledgeQuantity means that a quantity is not from 1 to
// model.MaxPledgeQuantity.
var ErrPledgeQuantity = errors.New("pledge quantity must be from 1 to 20")

// PledgeFullError says that the quantity does not fit under the maximum.
// Free is the most that this person can hold on the list: the items left
// plus what they hold now, never below 0.
type PledgeFullError struct{ Free int }

func (e *PledgeFullError) Error() string {
	return fmt.Sprintf("only %d items are left on the pledge list", e.Free)
}

// PledgeRequest is what a volunteer submits to pledge items to a list.
type PledgeRequest struct {
	EventID, ListID        int64
	EditToken, Name, Phone string
	Quantity               int
	// Admin pledges are allowed whatever the event status. The maximum of
	// the list still applies.
	Admin bool
}

// PledgeResult reports what Pledge did.
type PledgeResult struct {
	List         model.PledgeList
	Volunteer    *model.Volunteer
	NewVolunteer bool
	Quantity     int
	Previous     int // the person's quantity before, 0 for a new pledge
}

// Pledge sets the quantity that a volunteer brings to a list. A second
// pledge of the same person changes the quantity. Everything happens in one
// IMMEDIATE transaction, so two people cannot take the last items. A refused
// pledge leaves no new volunteer behind.
func (s *Store) Pledge(ctx context.Context, req PledgeRequest) (*PledgeResult, error) {
	res := &PledgeResult{Quantity: req.Quantity}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if req.Quantity < 1 || req.Quantity > model.MaxPledgeQuantity {
			return ErrPledgeQuantity
		}
		if err := checkOpen(ctx, tx, SignupRequest{EventID: req.EventID, Admin: req.Admin}); err != nil {
			return err
		}
		var err error
		if res.List, err = getPledgeList(ctx, tx, req.EventID, req.ListID); err != nil {
			return err
		}
		signup := SignupRequest{EventID: req.EventID, EditToken: req.EditToken, Name: req.Name, Phone: req.Phone}
		if req.Admin && req.EditToken == "" {
			// The organizer has no token for the person. Reuse the volunteer
			// with the same name, so that a second pledge changes the
			// quantity and does not add a second person.
			if known, err := volunteerByName(ctx, tx, req.EventID, req.Name); err != nil {
				return err
			} else if known != nil {
				signup.EditToken = known.EditToken
				if signup.Phone == "" {
					signup.Phone = known.Phone
				}
			}
		}
		res.Volunteer, res.NewVolunteer, err = findOrCreateVolunteer(ctx, tx, signup)
		if err != nil {
			return err
		}
		var count, total int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(quantity), 0), COALESCE(SUM(CASE WHEN volunteer_id = ? THEN quantity END), 0)
			 FROM pledges WHERE list_id = ?`, res.Volunteer.ID, res.List.ID).
			Scan(&count, &total, &res.Previous); err != nil {
			return err
		}
		if res.Previous == 0 && count >= MaxPledgesPerList {
			return ErrTooManyPledges
		}
		if limit := res.List.MaxItems; limit > 0 && total-res.Previous+req.Quantity > limit {
			return &PledgeFullError{Free: max(limit-total+res.Previous, 0)}
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO pledges (list_id, volunteer_id, quantity) VALUES (?, ?, ?)
			 ON CONFLICT (list_id, volunteer_id) DO UPDATE SET quantity = excluded.quantity`,
			res.List.ID, res.Volunteer.ID, req.Quantity)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CancelPledge removes one of the volunteer's pledges and returns its list.
func (s *Store) CancelPledge(ctx context.Context, volunteerID, pledgeID int64) (listID int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`DELETE FROM pledges WHERE id = ? AND volunteer_id = ? RETURNING list_id`, pledgeID, volunteerID).Scan(&listID)
	return listID, notFound(err)
}

// AdminDeletePledge removes any pledge of the event and returns its list.
func (s *Store) AdminDeletePledge(ctx context.Context, eventID, pledgeID int64) (listID int64, err error) {
	err = s.db.QueryRowContext(ctx,
		`DELETE FROM pledges WHERE id = ? AND list_id IN (SELECT id FROM pledge_lists WHERE event_id = ?) RETURNING list_id`,
		pledgeID, eventID).Scan(&listID)
	return listID, notFound(err)
}

// ListPledges returns all pledges of an event with volunteer details,
// oldest first.
func (s *Store) ListPledges(ctx context.Context, eventID int64) ([]model.PledgeRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.list_id, p.volunteer_id, p.quantity, p.created_at, v.name, v.phone
		 FROM pledges p
		 JOIN pledge_lists pl ON pl.id = p.list_id
		 JOIN volunteers v ON v.id = p.volunteer_id
		 WHERE pl.event_id = ? ORDER BY p.created_at, p.id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PledgeRow
	for rows.Next() {
		var r model.PledgeRow
		var created string
		if err := rows.Scan(&r.ID, &r.ListID, &r.VolunteerID, &r.Quantity, &created, &r.Name, &r.Phone); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// volunteerByName returns the volunteer of the event with this name, ignoring
// case and spacing. It returns nil if there is none.
func volunteerByName(ctx context.Context, q querier, eventID int64, name string) (*model.Volunteer, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+volunteerCols+` FROM volunteers WHERE event_id = ? ORDER BY id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanVolunteer(rows)
		if err != nil {
			return nil, err
		}
		if sameName(v.Name, name) {
			return v, nil
		}
	}
	return nil, rows.Err()
}
