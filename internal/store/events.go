package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"jeviensaider/internal/model"
)

const eventCols = `id, slug, title, description, location, date, admin_token, status, created_at, admin_email, notify_signups, COALESCE(retention_days, 0)`

func scanEvent(row interface{ Scan(...any) error }) (*model.Event, error) {
	var e model.Event
	var created string
	if err := row.Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Location, &e.Date, &e.AdminToken, &e.Status, &created, &e.AdminEmail, &e.NotifySignups, &e.RetentionDays); err != nil {
		return nil, notFound(err)
	}
	e.CreatedAt = parseTime(created)
	return &e, nil
}

// Slugify turns a title into a lowercase ASCII slug ("Fête de l'école" -> "fete-de-l-ecole").
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// drop accents
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 40 {
		out = strings.TrimRight(out[:40], "-")
	}
	if out == "" {
		out = "evenement"
	}
	return out
}

// CreateEvent inserts e, filling in ID, Slug, AdminToken and Status.
func (s *Store) CreateEvent(ctx context.Context, e *model.Event) error {
	return insertEvent(ctx, s.db, e)
}

// insertEvent inserts e with a new slug and admin token, as a draft unless
// e.Status is set, and fills in its ID.
func insertEvent(ctx context.Context, q querier, e *model.Event) error {
	e.Slug = Slugify(e.Title) + "-" + shortID()
	e.AdminToken = Token()
	if e.Status == "" {
		e.Status = model.StatusDraft
	}
	res, err := q.ExecContext(ctx,
		`INSERT INTO events (slug, title, description, location, date, admin_token, status, admin_email, notify_signups, retention_days) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Slug, e.Title, e.Description, e.Location, e.Date, e.AdminToken, e.Status, e.AdminEmail, e.NotifySignups, nullInt(e.RetentionDays))
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	e.CreatedAt = time.Now().UTC()
	return err
}

func (s *Store) EventBySlug(ctx context.Context, slug string) (*model.Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE slug = ?`, slug))
}

func (s *Store) EventByAdminToken(ctx context.Context, token string) (*model.Event, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE admin_token = ?`, token))
}

// UpdateEvent saves the editable fields of e (title, description, location,
// date, status, organiser e-mail, notification setting and retention days).
func (s *Store) UpdateEvent(ctx context.Context, e *model.Event) error {
	return affectedOne(s.db.ExecContext(ctx,
		`UPDATE events SET title = ?, description = ?, location = ?, date = ?, status = ?, admin_email = ?, notify_signups = ?, retention_days = ? WHERE id = ?`,
		e.Title, e.Description, e.Location, e.Date, e.Status, e.AdminEmail, e.NotifySignups, nullInt(e.RetentionDays), e.ID))
}

func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	return affectedOne(s.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id))
}

// PurgeVolunteers deletes every volunteer (and so every sign-up) of an event.
func (s *Store) PurgeVolunteers(ctx context.Context, eventID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM volunteers WHERE event_id = ?`, eventID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DuplicateEvent copies the structure (stands, slots, moments and pledge
// lists, no volunteers or pledges) of src into a new draft event. If both
// events have a date, slots and moments are moved by the same number of days.
// Drop-off times of pledge lists are times of the day, so they stay as they
// are.
func (s *Store) DuplicateEvent(ctx context.Context, src *model.Event, title, date string) (*model.Event, error) {
	dst := &model.Event{
		Title: title, Date: date, Description: src.Description, Location: src.Location,
		AdminEmail: src.AdminEmail, NotifySignups: src.NotifySignups, RetentionDays: src.RetentionDays,
	}
	var shift time.Duration
	if a, b := src.ParsedDate(), dst.ParsedDate(); !a.IsZero() && !b.IsZero() {
		shift = b.Sub(a)
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := insertEvent(ctx, tx, dst); err != nil {
			return err
		}
		stands, err := listStands(ctx, tx, src.ID)
		if err != nil {
			return err
		}
		slots, err := listSlots(ctx, tx, src.ID)
		if err != nil {
			return err
		}
		moments, err := listMoments(ctx, tx, src.ID)
		if err != nil {
			return err
		}
		newID := map[int64]int64{}
		for _, st := range stands {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO stands (event_id, name, description, position, color, fill_row) VALUES (?, ?, ?, ?, ?, ?)`,
				dst.ID, st.Name, st.Description, st.Position, st.Color, st.FillRow)
			if err != nil {
				return err
			}
			if newID[st.ID], err = res.LastInsertId(); err != nil {
				return err
			}
		}
		for _, sl := range slots {
			sl.StartsAt, sl.EndsAt = sl.StartsAt.Add(shift), sl.EndsAt.Add(shift)
			if err := insertSlot(ctx, tx, newID[sl.StandID], sl); err != nil {
				return err
			}
		}
		for _, m := range moments {
			m.EventID = dst.ID
			m.StartsAt, m.EndsAt = m.StartsAt.Add(shift), m.EndsAt.Add(shift)
			if _, err := insertMoment(ctx, tx, m); err != nil {
				return err
			}
		}
		// The ORDER BY gives the new lists IDs in the same order as the old ones.
		_, err = tx.ExecContext(ctx,
			`INSERT INTO pledge_lists (event_id, name, description, unit, drop_off, min_items, max_items)
			 SELECT ?, name, description, unit, drop_off, min_items, max_items FROM pledge_lists WHERE event_id = ? ORDER BY id`,
			dst.ID, src.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return dst, nil
}

// EventSummary is an event with counts, for the command-line admin tool.
// Items is the sum of the quantities of its pledges.
type EventSummary struct {
	model.Event
	Stands, Slots, Volunteers, Signups int
	PledgeLists, Pledges, Items        int
	LastDay                            time.Time
}

// ListEventSummaries returns every event, most recent last day first.
func (s *Store) ListEventSummaries(ctx context.Context) ([]EventSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id, e.slug, e.title, e.description, e.location, e.date, e.admin_token, e.status, e.created_at, e.admin_email, e.notify_signups, COALESCE(e.retention_days, 0),
		(SELECT count(*) FROM stands st WHERE st.event_id = e.id),
		(SELECT count(*) FROM slots sl JOIN stands st ON st.id = sl.stand_id WHERE st.event_id = e.id),
		(SELECT count(*) FROM volunteers v WHERE v.event_id = e.id),
		(SELECT count(*) FROM signups sg JOIN volunteers v ON v.id = sg.volunteer_id WHERE v.event_id = e.id),
		(SELECT count(*) FROM pledge_lists pl WHERE pl.event_id = e.id),
		(SELECT count(*) FROM pledges p JOIN pledge_lists pl ON pl.id = p.list_id WHERE pl.event_id = e.id),
		(SELECT COALESCE(sum(p.quantity), 0) FROM pledges p JOIN pledge_lists pl ON pl.id = p.list_id WHERE pl.event_id = e.id),
		`+lastDayExpr+` AS last_day
		FROM events e ORDER BY last_day DESC, e.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventSummary
	for rows.Next() {
		var e EventSummary
		var created, last string
		if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Location, &e.Date, &e.AdminToken, &e.Status, &created, &e.AdminEmail, &e.NotifySignups, &e.RetentionDays,
			&e.Stands, &e.Slots, &e.Volunteers, &e.Signups, &e.PledgeLists, &e.Pledges, &e.Items, &last); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(created)
		e.LastDay, _ = time.ParseInLocation(model.DateLayout, last, time.Local)
		out = append(out, e)
	}
	return out, rows.Err()
}
