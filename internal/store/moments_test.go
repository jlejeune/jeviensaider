package store

import (
	"testing"
	"time"

	"jeviensaider/internal/model"
)

func newMoment(t *testing.T, s *Store, eventID int64, title, from, to string) model.Moment {
	t.Helper()
	m := model.Moment{EventID: eventID, Title: title, StartsAt: at(from), EndsAt: at(to)}
	if err := s.CreateMoment(ctx, &m); err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	if m.ID == 0 {
		t.Fatalf("create %q: the ID was not set", title)
	}
	return m
}

func TestMomentsCRUD(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)

	goûter := newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")
	show := newMoment(t, s, ev.ID, "Spectacle des enfants", "2026-06-20 10:45", "2026-06-20 12:00")

	list, err := s.ListMoments(ctx, ev.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	if list[0].ID != show.ID || list[1].ID != goûter.ID {
		t.Errorf("order: got %q then %q, want the show first", list[0].Title, list[1].Title)
	}
	if !list[0].StartsAt.Equal(at("2026-06-20 10:45")) || !list[0].EndsAt.Equal(at("2026-06-20 12:00")) || list[0].EventID != ev.ID {
		t.Errorf("stored values: %+v", list[0])
	}

	show.Title, show.StartsAt, show.EndsAt = "Spectacle", at("2026-06-20 11:00"), at("2026-06-20 12:30")
	if err := s.UpdateMoment(ctx, show); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMoment(ctx, ev.ID, show.ID)
	if err != nil || got.Title != "Spectacle" || !got.StartsAt.Equal(at("2026-06-20 11:00")) || !got.EndsAt.Equal(at("2026-06-20 12:30")) {
		t.Errorf("after update: %+v %v", got, err)
	}

	if err := s.DeleteMoment(ctx, ev.ID, show.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMoment(ctx, ev.ID, show.ID); err != ErrNotFound {
		t.Errorf("get after delete: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteMoment(ctx, ev.ID, show.ID); err != ErrNotFound {
		t.Errorf("second delete: want ErrNotFound, got %v", err)
	}
}

func TestMomentsScopedToTheirEvent(t *testing.T) {
	s := openTest(t)
	ev1, _, _ := fixture(t, s, 0)
	ev2, _, _ := fixture(t, s, 0)
	m := newMoment(t, s, ev1.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")

	if _, err := s.GetMoment(ctx, ev2.ID, m.ID); err != ErrNotFound {
		t.Errorf("cross-event get: want ErrNotFound, got %v", err)
	}
	hijack := m
	hijack.EventID, hijack.Title = ev2.ID, "Piraté"
	if err := s.UpdateMoment(ctx, hijack); err != ErrNotFound {
		t.Errorf("cross-event update: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteMoment(ctx, ev2.ID, m.ID); err != ErrNotFound {
		t.Errorf("cross-event delete: want ErrNotFound, got %v", err)
	}

	got, err := s.GetMoment(ctx, ev1.ID, m.ID)
	if err != nil || got.Title != "Goûter" {
		t.Errorf("the moment must be unchanged: %+v %v", got, err)
	}
	if list, _ := s.ListMoments(ctx, ev2.ID); len(list) != 0 {
		t.Errorf("the other event lists %d moments, want 0", len(list))
	}
}

func TestMomentSameRange(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	other, _, _ := fixture(t, s, 0)
	first := newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")

	dup := model.Moment{EventID: ev.ID, Title: "Autre", StartsAt: first.StartsAt, EndsAt: first.EndsAt}
	if err := s.CreateMoment(ctx, &dup); err != ErrMomentExists {
		t.Errorf("same range: want ErrMomentExists, got %v", err)
	}

	second := newMoment(t, s, ev.ID, "Tirage", "2026-06-20 16:00", "2026-06-20 17:00")
	second.StartsAt, second.EndsAt = first.StartsAt, first.EndsAt
	if err := s.UpdateMoment(ctx, second); err != ErrMomentExists {
		t.Errorf("update into another range: want ErrMomentExists, got %v", err)
	}

	// Another event may use the same range.
	newMoment(t, s, other.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")
}

func TestMomentLimit(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	start := at("2026-06-20 00:00")
	for i := 0; i < MaxMoments; i++ {
		from := start.Add(time.Duration(i) * time.Hour)
		m := model.Moment{EventID: ev.ID, Title: "Moment", StartsAt: from, EndsAt: from.Add(time.Hour)}
		if err := s.CreateMoment(ctx, &m); err != nil {
			t.Fatalf("moment %d: %v", i+1, err)
		}
	}
	over := start.Add(MaxMoments * time.Hour)
	m := model.Moment{EventID: ev.ID, Title: "De trop", StartsAt: over, EndsAt: over.Add(time.Hour)}
	if err := s.CreateMoment(ctx, &m); err != ErrTooManyMoments {
		t.Errorf("moment %d: want ErrTooManyMoments, got %v", MaxMoments+1, err)
	}
}

func TestMomentsDuplicate(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	newMoment(t, s, ev.ID, "Spectacle des enfants", "2026-06-20 10:45", "2026-06-20 12:00")
	newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")

	dup, err := s.DuplicateEvent(ctx, ev, "Fête de l'école 2026 bis", "2026-06-27")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMoments(ctx, dup.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("copied moments: %v %v", list, err)
	}
	if list[0].Title != "Spectacle des enfants" || !list[0].StartsAt.Equal(at("2026-06-27 10:45")) || !list[0].EndsAt.Equal(at("2026-06-27 12:00")) {
		t.Errorf("first copy: %+v", list[0])
	}
	if list[1].Title != "Goûter" || !list[1].StartsAt.Equal(at("2026-06-27 15:00")) || list[1].EventID != dup.ID {
		t.Errorf("second copy: %+v", list[1])
	}
	if src, _ := s.ListMoments(ctx, ev.ID); len(src) != 2 || !src[0].StartsAt.Equal(at("2026-06-20 10:45")) {
		t.Errorf("the source moments must not change: %+v", src)
	}
}

func TestMomentsDeletedWithTheEvent(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")

	if err := s.DeleteEvent(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM moments WHERE event_id = ?`, ev.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d moments left after the event was deleted", n)
	}
}

func TestMomentColor(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)

	plain := newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")
	if got, err := s.GetMoment(ctx, ev.ID, plain.ID); err != nil || got.Color != "" {
		t.Errorf("a moment with no color: %+v %v", got, err)
	}

	m := model.Moment{EventID: ev.ID, Title: "Spectacle", Color: "#e8c93a", StartsAt: at("2026-06-20 10:45"), EndsAt: at("2026-06-20 12:00")}
	if err := s.CreateMoment(ctx, &m); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMoments(ctx, ev.ID)
	if err != nil || list[0].Color != "#e8c93a" || list[1].Color != "" {
		t.Fatalf("list colors: %+v %v", list, err)
	}

	for _, color := range []string{"", "#3d7cc9"} {
		m.Color = color
		if err := s.UpdateMoment(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got, err := s.GetMoment(ctx, ev.ID, m.ID); err != nil || got.Color != color {
			t.Errorf("after update to %q: %+v %v", color, got, err)
		}
	}
}

func TestMomentsDuplicateKeepsColors(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	colored := model.Moment{EventID: ev.ID, Title: "Spectacle", Color: "#e8c93a", StartsAt: at("2026-06-20 10:45"), EndsAt: at("2026-06-20 12:00")}
	if err := s.CreateMoment(ctx, &colored); err != nil {
		t.Fatal(err)
	}
	newMoment(t, s, ev.ID, "Goûter", "2026-06-20 15:00", "2026-06-20 16:00")

	dup, err := s.DuplicateEvent(ctx, ev, "Copie", "2026-06-27")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListMoments(ctx, dup.ID)
	if err != nil || len(list) != 2 || list[0].Color != "#e8c93a" || list[1].Color != "" {
		t.Errorf("copied colors: %+v %v", list, err)
	}
}

// The fill of a moment is saved, and a duplicate of the event keeps it.
func TestMomentFill(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 3)
	m := model.Moment{EventID: ev.ID, Title: "Spectacle", Color: "#e8c93a", Fill: true, StartsAt: at("2026-06-20 11:00"), EndsAt: at("2026-06-20 12:00")}
	if err := s.CreateMoment(ctx, &m); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMoment(ctx, ev.ID, m.ID)
	if err != nil || !got.Fill {
		t.Fatalf("fill not saved: %+v %v", got, err)
	}
	dup, err := s.DuplicateEvent(ctx, ev, "Copie", "")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListMoments(ctx, dup.ID)
	if len(list) != 1 || !list[0].Fill {
		t.Errorf("duplicate did not keep the fill: %+v", list)
	}
	got.Fill = false
	if err := s.UpdateMoment(ctx, got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetMoment(ctx, ev.ID, m.ID); got.Fill {
		t.Error("fill still on after the update")
	}
}
