package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/slotgen"
)

var ctx = context.Background()

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func at(s string) time.Time {
	t, err := time.Parse(model.TimeLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

// fixture creates an open event with one stand and three 1-hour slots
// (min 2, max given).
func fixture(t *testing.T, s *Store, max int) (*model.Event, *model.Stand, []model.Slot) {
	t.Helper()
	ev := &model.Event{Title: "Fête de l'école", Date: "2026-06-20", Status: model.StatusOpen}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	st := &model.Stand{EventID: ev.ID, Name: "Pêche aux canards"}
	if err := s.CreateStand(ctx, st); err != nil {
		t.Fatal(err)
	}
	gen, err := slotgen.Generate(st.ID, at("2026-06-20 14:00"), at("2026-06-20 17:00"), time.Hour, 2, max)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSlots(ctx, ev.ID, st.ID, gen); err != nil {
		t.Fatal(err)
	}
	slots, err := s.ListSlots(ctx, ev.ID)
	if err != nil || len(slots) != 3 {
		t.Fatalf("slots: %v %v", slots, err)
	}
	return ev, st, slots
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Fête de l'école":  "fete-de-l-ecole",
		"Marché de Noël !": "marche-de-noel",
		"???":              "evenement",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}

func TestEventLookup(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	got, err := s.EventByAdminToken(ctx, ev.AdminToken)
	if err != nil || got.ID != ev.ID {
		t.Fatalf("by admin token: %v %v", got, err)
	}
	if _, err := s.EventByAdminToken(ctx, "nope"); err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if _, err := s.EventBySlug(ctx, ev.Slug); err != nil {
		t.Error(err)
	}
}

func TestSignupFlow(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 2)

	r, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie Dupont"})
	if err != nil || !r.NewVolunteer {
		t.Fatalf("first signup: %+v %v", r, err)
	}
	tok := r.Volunteer.EditToken

	// Same volunteer, same slot: refused.
	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, EditToken: tok, Name: "Marie Dupont"}); err != ErrAlreadySigned {
		t.Errorf("duplicate: want ErrAlreadySigned, got %v", err)
	}
	// Same volunteer, next slot: ok, reuses volunteer, no overlap.
	r2, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[1].ID, EditToken: tok, Name: "Marie Dupont"})
	if err != nil || r2.NewVolunteer || r2.Volunteer.ID != r.Volunteer.ID || len(r2.Overlapping) != 0 {
		t.Fatalf("second slot: %+v %v", r2, err)
	}

	// Fill slot 0 (max 2), third person refused.
	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Paul"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Léa"}); err != ErrSlotFull {
		t.Errorf("want ErrSlotFull, got %v", err)
	}

	rows, err := s.ListSignups(ctx, ev.ID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("list: %d %v", len(rows), err)
	}

	// Cancel: only own sign-ups.
	other := rows[2] // Paul's
	if _, err := s.CancelSignup(ctx, r.Volunteer.ID, other.ID); err != ErrNotFound {
		t.Errorf("cancel someone else's: want ErrNotFound, got %v", err)
	}
	if slot, err := s.CancelSignup(ctx, r.Volunteer.ID, rows[0].ID); err != nil || slot != rows[0].SlotID {
		t.Error(err)
	}

	// Closed event refuses sign-ups.
	ev.Status = model.StatusClosed
	if err := s.UpdateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[2].ID, Name: "X"}); err != ErrEventNotOpen {
		t.Errorf("closed: want ErrEventNotOpen, got %v", err)
	}
}

func TestOverlapWarning(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)
	buvette := &model.Stand{EventID: ev.ID, Name: "Buvette"}
	if err := s.CreateStand(ctx, buvette); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSlots(ctx, ev.ID, buvette.ID, []model.Slot{{StartsAt: at("2026-06-20 14:30"), EndsAt: at("2026-06-20 15:30"), MinPeople: 1}}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListSlots(ctx, ev.ID)
	var buvSlot model.Slot
	for _, sl := range all {
		if sl.StandID == buvette.ID {
			buvSlot = sl
		}
	}
	r, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: buvSlot.ID, EditToken: r.Volunteer.EditToken, Name: "Marie"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Overlapping) != 1 || r2.Overlapping[0].ID != slots[0].ID {
		t.Errorf("expected overlap with first slot, got %+v", r2.Overlapping)
	}
}

func TestConcurrentLastPlace(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 2)
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Parent"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrSlotFull):
				full++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 2 || full != n-2 {
		t.Errorf("want 2 ok / %d full, got %d / %d", n-2, ok, full)
	}
}

func TestScopingAndCascade(t *testing.T) {
	s := openTest(t)
	ev1, st1, slots1 := fixture(t, s, 0)
	ev2, _, _ := fixture(t, s, 0)

	// A slot of event 1 is invisible through event 2.
	if _, err := s.GetSlot(ctx, ev2.ID, slots1[0].ID); err != ErrNotFound {
		t.Errorf("cross-event slot: want ErrNotFound, got %v", err)
	}
	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev2.ID, SlotID: slots1[0].ID, Name: "X"}); err != ErrNotFound {
		t.Errorf("cross-event signup: want ErrNotFound, got %v", err)
	}
	if err := s.DeleteStand(ctx, ev2.ID, st1.ID); err != ErrNotFound {
		t.Errorf("cross-event stand delete: want ErrNotFound, got %v", err)
	}

	if _, err := s.SignUp(ctx, SignupRequest{EventID: ev1.ID, SlotID: slots1[0].ID, Name: "Marie"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStand(ctx, ev1.ID, st1.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListSignups(ctx, ev1.ID); len(rows) != 0 {
		t.Errorf("signups should cascade with stand, got %d", len(rows))
	}
	if sl, _ := s.ListSlots(ctx, ev1.ID); len(sl) != 0 {
		t.Errorf("slots should cascade, got %d", len(sl))
	}

	if err := s.DeleteEvent(ctx, ev2.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.ListStands(ctx, ev2.ID); len(st) != 0 {
		t.Error("stands should cascade with event")
	}
}

func TestPurge(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)
	r, _ := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie"})
	n, err := s.PurgeVolunteers(ctx, ev.ID)
	if err != nil || n != 1 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if _, err := s.VolunteerByToken(ctx, ev.ID, r.Volunteer.EditToken); err != ErrNotFound {
		t.Error("volunteer should be gone")
	}
	if sl, _ := s.ListSlots(ctx, ev.ID); len(sl) != 3 {
		t.Error("slots must survive purge")
	}
}

func TestDuplicate(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 3)
	s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie"})

	dup, err := s.DuplicateEvent(ctx, ev, "Fête de l'école 2027", "2027-06-19")
	if err != nil {
		t.Fatal(err)
	}
	if dup.Status != model.StatusDraft || dup.AdminToken == ev.AdminToken {
		t.Errorf("unexpected dup: %+v", dup)
	}
	stands, _ := s.ListStands(ctx, dup.ID)
	newSlots, _ := s.ListSlots(ctx, dup.ID)
	if len(stands) != 1 || len(newSlots) != 3 {
		t.Fatalf("got %d stands, %d slots", len(stands), len(newSlots))
	}
	if !newSlots[0].StartsAt.Equal(at("2027-06-19 14:00")) || newSlots[0].MaxPeople != 3 {
		t.Errorf("slot not shifted/copied: %+v", newSlots[0])
	}
	if rows, _ := s.ListSignups(ctx, dup.ID); len(rows) != 0 {
		t.Error("volunteers must not be copied")
	}
}

func TestMoveStand(t *testing.T) {
	s := openTest(t)
	ev, a, _ := fixture(t, s, 0)
	b := &model.Stand{EventID: ev.ID, Name: "Buvette"}
	s.CreateStand(ctx, b)
	if err := s.MoveStand(ctx, ev.ID, b.ID, -1); err != nil {
		t.Fatal(err)
	}
	st, _ := s.ListStands(ctx, ev.ID)
	if st[0].ID != b.ID || st[1].ID != a.ID {
		t.Errorf("order after move: %v", st)
	}
	if err := s.MoveStand(ctx, ev.ID, b.ID, -1); err != nil {
		t.Error("moving the first stand up is a no-op, not an error")
	}
}

// The row fill is a setting of each stand: it is saved, and a duplicate of
// the event keeps it.
func TestStandFillRow(t *testing.T) {
	s := openTest(t)
	ev, st, _ := fixture(t, s, 3)
	if st.FillRow {
		t.Fatal("a new stand must not fill its row")
	}
	other := &model.Stand{EventID: ev.ID, Name: "Tombola", Color: "#d9534f", FillRow: true}
	if err := s.CreateStand(ctx, other); err != nil {
		t.Fatal(err)
	}
	st.FillRow = true
	if err := s.UpdateStand(ctx, st); err != nil {
		t.Fatal(err)
	}
	stands, _ := s.ListStands(ctx, ev.ID)
	if len(stands) != 2 || !stands[0].FillRow || !stands[1].FillRow {
		t.Fatalf("fill_row not saved: %+v", stands)
	}
	st.FillRow = false
	s.UpdateStand(ctx, st)
	dup, err := s.DuplicateEvent(ctx, ev, "Copie", "")
	if err != nil {
		t.Fatal(err)
	}
	stands, _ = s.ListStands(ctx, dup.ID)
	if len(stands) != 2 || stands[0].FillRow || !stands[1].FillRow {
		t.Errorf("duplicate did not keep the row fill: %+v", stands)
	}
}
