package store

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"jeviensaider/internal/model"
)

// A browser that remembers Mallory (her personal link was shared) must not
// turn Alice's sign-up into an update of Mallory's record.
func TestSignupWithSomeoneElsesToken(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)

	m, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Mallory", Phone: "0700000000"})
	if err != nil {
		t.Fatal(err)
	}
	tok := m.Volunteer.EditToken

	a, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[1].ID, EditToken: tok, Name: "Alice Victim", Phone: "0601020304"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.NewVolunteer || a.Volunteer.ID == m.Volunteer.ID || a.Volunteer.EditToken == tok {
		t.Fatalf("expected a new volunteer for Alice, got %+v", a)
	}
	mal, err := s.VolunteerByToken(ctx, ev.ID, tok)
	if err != nil || mal.Name != "Mallory" || mal.Phone != "0700000000" {
		t.Fatalf("Mallory's record changed: %+v %v", mal, err)
	}

	// The same person (same name, other case and spacing) only updates
	// their own details.
	b, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[2].ID, EditToken: a.Volunteer.EditToken, Name: "  alice   VICTIM ", Phone: "0611111111"})
	if err != nil {
		t.Fatal(err)
	}
	if b.NewVolunteer || b.Volunteer.ID != a.Volunteer.ID || b.Volunteer.Phone != "0611111111" {
		t.Fatalf("expected Alice's record to be reused and updated, got %+v", b)
	}
}

func TestEventLimits(t *testing.T) {
	s := openTest(t)
	ev, st, _ := fixture(t, s, 0) // 1 stand, 3 slots

	for i := 1; i < MaxStands; i++ {
		if err := s.CreateStand(ctx, &model.Stand{EventID: ev.ID, Name: "Stand"}); err != nil {
			t.Fatalf("stand %d: %v", i, err)
		}
	}
	if err := s.CreateStand(ctx, &model.Stand{EventID: ev.ID, Name: "One too many"}); !errors.Is(err, ErrTooManyStands) {
		t.Fatalf("want ErrTooManyStands, got %v", err)
	}

	slot := func(i int) model.Slot {
		start := at("2026-06-21 00:00").Add(time.Duration(i) * time.Minute)
		return model.Slot{StartsAt: start, EndsAt: start.Add(time.Minute), MinPeople: 1}
	}
	var batch []model.Slot
	for i := 0; i < MaxSlots-3; i++ {
		batch = append(batch, slot(i))
	}
	if err := s.CreateSlots(ctx, ev.ID, st.ID, batch); err != nil {
		t.Fatalf("filling up to the limit: %v", err)
	}
	if err := s.CreateSlots(ctx, ev.ID, st.ID, []model.Slot{slot(9999)}); !errors.Is(err, ErrTooManySlots) {
		t.Fatalf("want ErrTooManySlots, got %v", err)
	}
	// A refused batch adds nothing.
	all, _ := s.ListSlots(ctx, ev.ID)
	if len(all) != MaxSlots {
		t.Fatalf("slots: %d, want %d", len(all), MaxSlots)
	}
}

func TestRenewVolunteerToken(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)
	res, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.Volunteer
	old := v.EditToken
	if err := s.RenewVolunteerToken(ctx, v); err != nil || v.EditToken == old || len(v.EditToken) < 40 {
		t.Fatalf("renew: %v, token %q", err, v.EditToken)
	}
	if _, err := s.VolunteerByToken(ctx, ev.ID, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token still works: %v", err)
	}
	got, err := s.VolunteerByToken(ctx, ev.ID, v.EditToken)
	if err != nil || got.ID != v.ID {
		t.Errorf("new token: %+v %v", got, err)
	}
	v.ID = 999999
	if err := s.RenewVolunteerToken(ctx, v); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown volunteer: %v", err)
	}
}

func TestPledgeSecurity(t *testing.T) {
	t.Run("quantity", func(t *testing.T) {
		s := openTest(t)
		ev, _, _ := fixture(t, s, 0)
		l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
		// A quantity outside 1 to 20 is refused before anything is stored.
		for _, q := range []int{-1, 0, model.MaxPledgeQuantity + 1} {
			_, err := s.Pledge(ctx, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie", Quantity: q})
			if !errors.Is(err, ErrPledgeQuantity) {
				t.Errorf("quantity %d: want ErrPledgeQuantity, got %v", q, err)
			}
		}
		wantVolunteers(t, s, ev.ID, 0)
		if n := countPledges(t, s, l.ID); n != 0 {
			t.Errorf("refused quantities left %d rows", n)
		}
	})

	t.Run("another event", func(t *testing.T) {
		s := openTest(t)
		evA, _, _ := fixture(t, s, 0)
		evB := newEvent(t, s, "Autre fête")
		listA := newPledgeList(t, s, model.PledgeList{EventID: evA.ID, Name: "Gâteaux"})
		listB := newPledgeList(t, s, model.PledgeList{EventID: evB.ID, Name: "Chaises"})

		// A token of event A does not attach to the volunteer of A through a list of event B.
		// The name is the same, so only the event scope of the token can explain a new volunteer.
		alice := mustPledge(t, s, PledgeRequest{EventID: evA.ID, ListID: listA.ID, Name: "Alice", Phone: "0601020304", Quantity: 1}).Volunteer
		res := mustPledge(t, s, PledgeRequest{EventID: evB.ID, ListID: listB.ID, EditToken: alice.EditToken, Name: "Alice", Phone: "0700000000", Quantity: 1})
		if !res.NewVolunteer || res.Volunteer.ID == alice.ID || res.Volunteer.EditToken == alice.EditToken || res.Volunteer.EventID != evB.ID {
			t.Fatalf("expected a new volunteer on event B, got %+v", res)
		}
		kept, err := s.VolunteerByToken(ctx, evA.ID, alice.EditToken)
		if err != nil || kept.Name != "Alice" || kept.Phone != "0601020304" {
			t.Fatalf("Alice changed: %+v, %v", kept, err)
		}
		if n := countPledges(t, s, listA.ID); n != 1 {
			t.Errorf("list of event A has %d pledges, want 1", n)
		}

		// A list of another event is not found, and the refusal leaves no volunteer.
		if _, err := s.Pledge(ctx, PledgeRequest{EventID: evB.ID, ListID: listA.ID, Name: "Paul", Quantity: 1}); !errors.Is(err, ErrNotFound) {
			t.Errorf("list of another event: want ErrNotFound, got %v", err)
		}
		if _, err := s.Pledge(ctx, PledgeRequest{EventID: evB.ID, ListID: 999999, Name: "Paul", Quantity: 1}); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown list: want ErrNotFound, got %v", err)
		}
		wantVolunteers(t, s, evB.ID, 1)
		if n := countPledges(t, s, listA.ID); n != 1 {
			t.Errorf("list of event A has %d pledges after the attack, want 1", n)
		}
	})

	t.Run("pledgers per list", func(t *testing.T) {
		s := openTest(t)
		ev, _, _ := fixture(t, s, 0)
		l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})

		// The list holds the limit: one real pledger and the others added with SQL.
		marie := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie", Quantity: 1})
		for i := 1; i < MaxPledgesPerList; i++ {
			rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, fmt.Sprintf("Parent %d", i)), 1)
		}

		// The 201st person is refused and leaves no volunteer.
		_, err := s.Pledge(ctx, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Une de trop", Quantity: 1})
		if !errors.Is(err, ErrTooManyPledges) {
			t.Fatalf("201st pledger: want ErrTooManyPledges, got %v", err)
		}
		wantVolunteers(t, s, ev.ID, MaxPledgesPerList)
		if n := countPledges(t, s, l.ID); n != MaxPledgesPerList {
			t.Fatalf("list has %d pledges, want %d", n, MaxPledgesPerList)
		}

		// A person who is on the list can still change the quantity.
		res := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, EditToken: marie.Volunteer.EditToken, Name: "Marie", Quantity: 5})
		if res.Previous != 1 || res.NewVolunteer {
			t.Errorf("change on a list at the limit = %+v", res)
		}
		// The limit is for each list.
		other := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Boissons"})
		mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: other.ID, Name: "Une de trop", Quantity: 1})
	})
}
