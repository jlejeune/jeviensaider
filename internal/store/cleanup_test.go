package store

import (
	"testing"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/slotgen"
)

func day(s string) time.Time {
	t, err := time.ParseInLocation(model.DateLayout, s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

// Pledge lists have no date. A pledge has a creation date, but it must not
// keep an old event alive.
func TestPledgeListsDoNotMoveLastDay(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	if _, err := s.db.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00' WHERE id = ?`, ev.ID); err != nil {
		t.Fatal(err)
	}
	before, err := s.EventLastDay(ctx, ev.ID)
	if err != nil || !before.Equal(day("2026-06-20")) {
		t.Fatalf("EventLastDay before = %v, %v; want 2026-06-20", before, err)
	}

	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", DropOff: "23:59"})
	rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, "Marie"), 2)
	if _, err := s.db.Exec(`UPDATE pledges SET created_at = '2099-01-01 10:00:00'`); err != nil {
		t.Fatal(err)
	}

	after, err := s.EventLastDay(ctx, ev.ID)
	if err != nil || !after.Equal(before) {
		t.Errorf("EventLastDay after = %v, %v; want %v", after, err, before)
	}
}

func TestCleanup(t *testing.T) {
	s := openTest(t)
	backdate := func(id int64) {
		t.Helper()
		if _, err := s.db.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00' WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}

	// Dated 2026-06-20, slots on the same day, one volunteer.
	fete, _, slots := fixture(t, s, 0)
	backdate(fete.ID)
	if _, err := s.SignUp(ctx, SignupRequest{EventID: fete.ID, SlotID: slots[0].ID, Name: "Marie Dupont"}); err != nil {
		t.Fatal(err)
	}

	// No date, no slots: only its creation date counts.
	undated := &model.Event{Title: "Sans date"}
	if err := s.CreateEvent(ctx, undated); err != nil {
		t.Fatal(err)
	}
	backdate(undated.ID)

	// Dated in March, but with a slot in September: the slot counts.
	late := &model.Event{Title: "Créneau tardif", Date: "2026-03-01"}
	if err := s.CreateEvent(ctx, late); err != nil {
		t.Fatal(err)
	}
	backdate(late.ID)
	st := &model.Stand{EventID: late.ID, Name: "Buvette"}
	if err := s.CreateStand(ctx, st); err != nil {
		t.Fatal(err)
	}
	gen, err := slotgen.Generate(st.ID, at("2026-09-01 10:00"), at("2026-09-01 11:00"), time.Hour, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSlots(ctx, late.ID, st.ID, gen); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[int64]string{fete.ID: "2026-06-20", undated.ID: "2026-01-01", late.ID: "2026-09-01"} {
		got, err := s.EventLastDay(ctx, id)
		if err != nil || !got.Equal(day(want)) {
			t.Errorf("EventLastDay(%d) = %v, %v; want %s", id, got, err, want)
		}
	}
	if got := ExpiresOn(day("2026-06-20"), 60); !got.Equal(day("2026-08-20")) {
		t.Errorf("ExpiresOn = %v, want 2026-08-20", got)
	}

	exists := func(ev *model.Event) bool {
		_, err := s.EventBySlug(ctx, ev.Slug)
		return err == nil
	}

	// Disabled.
	if n, err := s.Cleanup(ctx, Retention{}, day("2030-01-01")); n != 0 || err != nil {
		t.Fatalf("disabled cleanup: %d, %v", n, err)
	}

	// Day 60 after the fête: only the undated event goes.
	if n, err := s.Cleanup(ctx, Retention{DefaultDays: 60, MaxDays: 60}, day("2026-08-19")); n != 1 || err != nil {
		t.Fatalf("cleanup 08-19: %d, %v", n, err)
	}
	if exists(undated) || !exists(fete) || !exists(late) {
		t.Fatalf("after 08-19: undated=%v fete=%v late=%v", exists(undated), exists(fete), exists(late))
	}

	// Day 61 (= ExpiresOn): the fête goes, with its volunteers.
	if n, err := s.Cleanup(ctx, Retention{DefaultDays: 60, MaxDays: 60}, day("2026-08-20")); n != 1 || err != nil {
		t.Fatalf("cleanup 08-20: %d, %v", n, err)
	}
	if exists(fete) || !exists(late) {
		t.Fatalf("after 08-20: fete=%v late=%v", exists(fete), exists(late))
	}
	var left int
	s.db.QueryRow(`SELECT (SELECT count(*) FROM volunteers) + (SELECT count(*) FROM signups) + (SELECT count(*) FROM slots st JOIN stands s ON s.id = st.stand_id WHERE s.event_id = ?)`, fete.ID).Scan(&left)
	if left != 0 {
		t.Errorf("rows left after cascade: %d", left)
	}
}

// A moment counts as activity: an event with only a late moment must not be
// deleted before that moment happens.
func TestCleanupKeepsEventsUntilTheirLastMoment(t *testing.T) {
	s := openTest(t)
	ev := &model.Event{Title: "Spectacle"}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00' WHERE id = ?`, ev.ID); err != nil {
		t.Fatal(err)
	}
	newMoment(t, s, ev.ID, "Spectacle", "2026-04-10 15:00", "2026-04-10 16:00")

	got, err := s.EventLastDay(ctx, ev.ID)
	if err != nil || !got.Equal(day("2026-04-10")) {
		t.Fatalf("EventLastDay = %v, %v; want 2026-04-10", got, err)
	}
	r := Retention{DefaultDays: 30, MaxDays: 30}
	if n, err := s.Cleanup(ctx, r, day("2026-05-10")); n != 0 || err != nil {
		t.Fatalf("cleanup on the last day of retention: %d, %v", n, err)
	}
	if n, err := s.Cleanup(ctx, r, day("2026-05-11")); n != 1 || err != nil {
		t.Fatalf("cleanup the day after: %d, %v", n, err)
	}
}
