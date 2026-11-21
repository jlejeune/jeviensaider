package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"jeviensaider/internal/model"
)

// mustPledge makes a pledge and stops the test if it fails.
func mustPledge(t *testing.T, s *Store, req PledgeRequest) *PledgeResult {
	t.Helper()
	res, err := s.Pledge(ctx, req)
	if err != nil {
		t.Fatalf("pledge of %q for %d: %v", req.Name, req.Quantity, err)
	}
	return res
}

// wantFull checks that err is a *PledgeFullError with this number of free items.
func wantFull(t *testing.T, err error, free int) {
	t.Helper()
	var full *PledgeFullError
	if !errors.As(err, &full) || full.Free != free {
		t.Fatalf("want *PledgeFullError with Free %d, got %v", free, err)
	}
}

// listTotal returns the sum of the quantities of a list.
func listTotal(t *testing.T, s *Store, listID int64) int {
	t.Helper()
	var total int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(quantity), 0) FROM pledges WHERE list_id = ?`, listID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

// countPledges returns the number of pledge rows of a list.
func countPledges(t *testing.T, s *Store, listID int64) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pledges WHERE list_id = ?`, listID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func wantVolunteers(t *testing.T, s *Store, eventID int64, want int) {
	t.Helper()
	if n, err := s.CountVolunteers(ctx, eventID); err != nil || n != want {
		t.Fatalf("volunteers = %d, %v; want %d", n, err, want)
	}
}

func TestPledgeFlow(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", Unit: "gâteaux"})

	first := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie Dupont", Phone: "0601020304", Quantity: 2})
	if !first.NewVolunteer || first.Previous != 0 || first.Quantity != 2 || first.List != l {
		t.Fatalf("first pledge = %+v", first)
	}
	if first.Volunteer == nil || first.Volunteer.EditToken == "" || first.Volunteer.Name != "Marie Dupont" {
		t.Fatalf("volunteer of the first pledge = %+v", first.Volunteer)
	}

	// The same person changes the quantity: the row is reused.
	second := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, EditToken: first.Volunteer.EditToken, Name: "Marie Dupont", Phone: "0601020304", Quantity: 3})
	if second.NewVolunteer || second.Previous != 2 || second.Quantity != 3 || second.Volunteer.ID != first.Volunteer.ID {
		t.Fatalf("second pledge = %+v", second)
	}

	rows, err := s.ListPledges(ctx, ev.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListPledges = %+v, %v; want one row", rows, err)
	}
	r := rows[0]
	if r.ListID != l.ID || r.VolunteerID != first.Volunteer.ID || r.Quantity != 3 || r.Name != "Marie Dupont" || r.Phone != "0601020304" || r.ID == 0 || r.CreatedAt.IsZero() {
		t.Errorf("row = %+v", r)
	}
}

func TestPledgeClosedLeavesNoVolunteer(t *testing.T) {
	s := openTest(t)
	ev := &model.Event{Title: "Brouillon", Date: "2026-06-20", Status: model.StatusDraft}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	req := PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie", Quantity: 1}

	if _, err := s.Pledge(ctx, req); !errors.Is(err, ErrEventNotOpen) {
		t.Fatalf("draft event: want ErrEventNotOpen, got %v", err)
	}
	wantVolunteers(t, s, ev.ID, 0)
	if n := countPledges(t, s, l.ID); n != 0 {
		t.Fatalf("refused pledge left %d rows", n)
	}

	// A closed event refuses a pledge too.
	ev.Status = model.StatusClosed
	if err := s.UpdateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pledge(ctx, req); !errors.Is(err, ErrEventNotOpen) {
		t.Fatalf("closed event: want ErrEventNotOpen, got %v", err)
	}
	wantVolunteers(t, s, ev.ID, 0)

	// The organiser can pledge whatever the status.
	req.Admin = true
	res := mustPledge(t, s, req)
	if !res.NewVolunteer || res.Previous != 0 {
		t.Errorf("admin pledge = %+v", res)
	}
	wantVolunteers(t, s, ev.ID, 1)
	if total := listTotal(t, s, l.ID); total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestPledgeMaximum(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", MaxItems: 5})
	pledge := func(name string, quantity int) (*PledgeResult, error) {
		return s.Pledge(ctx, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: name, Quantity: quantity})
	}

	if _, err := pledge("A", 3); err != nil {
		t.Fatal(err)
	}
	_, err := pledge("B", 3)
	wantFull(t, err, 2)
	// The refusal leaves no volunteer and no row.
	wantVolunteers(t, s, ev.ID, 1)
	if total := listTotal(t, s, l.ID); total != 3 {
		t.Fatalf("total after refusal = %d, want 3", total)
	}

	if _, err := pledge("B", 2); err != nil {
		t.Fatalf("B asks 2: %v", err)
	}
	_, err = pledge("C", 1)
	wantFull(t, err, 0)
	wantVolunteers(t, s, ev.ID, 2)
	if total := listTotal(t, s, l.ID); total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

// The maximum counts what the others hold, not what the person holds now.
func TestPledgeOwnQuantityNearMaximum(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", MaxItems: 5})
	req := func(name, token string, quantity int) PledgeRequest {
		return PledgeRequest{EventID: ev.ID, ListID: l.ID, EditToken: token, Name: name, Quantity: quantity}
	}

	a := mustPledge(t, s, req("A", "", 3)).Volunteer.EditToken
	b := mustPledge(t, s, req("B", "", 2)).Volunteer.EditToken
	// quantityOf returns the quantity of a person, 0 if they hold none.
	quantityOf := func(name string) int {
		t.Helper()
		rows, err := s.ListPledges(ctx, ev.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Name == name {
				return r.Quantity
			}
		}
		return 0
	}

	// The list is full, and A lowers to 2.
	if res := mustPledge(t, s, req("A", a, 2)); res.Previous != 3 || res.Quantity != 2 {
		t.Fatalf("A asks 2: %+v", res)
	}
	// A goes back to 3. The list is full again.
	if res := mustPledge(t, s, req("A", a, 3)); res.Previous != 2 {
		t.Fatalf("A asks 3 after 2: %+v", res)
	}
	// 4 would make 6 items. The refusal reports the most that A can hold:
	// the 0 items left plus the 3 that A holds.
	_, err := s.Pledge(ctx, req("A", a, 4))
	wantFull(t, err, 3)
	if got := quantityOf("A"); got != 3 {
		t.Fatalf("A holds %d after the refusal, want 3", got)
	}
	// The same quantity as now always works, even on a full list.
	if res := mustPledge(t, s, req("A", a, 3)); res.Previous != 3 {
		t.Fatalf("A asks 3 again: %+v", res)
	}

	// B lowers to 1, so the total is 4 and 1 item is left.
	if res := mustPledge(t, s, req("B", b, 1)); res.Previous != 2 {
		t.Fatalf("B asks 1: %+v", res)
	}
	if total := listTotal(t, s, l.ID); total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	// 5 would make 6 items. A can hold 4: the 1 item left plus the 3 that A holds.
	_, err = s.Pledge(ctx, req("A", a, 5))
	wantFull(t, err, 4)
	if got := quantityOf("A"); got != 3 {
		t.Fatalf("A holds %d after the second refusal, want 3", got)
	}
	// 4 makes exactly 5 items, which is allowed.
	if res := mustPledge(t, s, req("A", a, 4)); res.Previous != 3 {
		t.Fatalf("A asks 4: %+v", res)
	}
	if total := listTotal(t, s, l.ID); total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	wantVolunteers(t, s, ev.ID, 2)
}

func TestPledgeNoMaximumRefusesNobody(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})

	for i := 0; i < 11; i++ {
		name := fmt.Sprintf("Parent %d", i)
		mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: name, Quantity: model.MaxPledgeQuantity})
	}
	if total := listTotal(t, s, l.ID); total != 11*model.MaxPledgeQuantity {
		t.Errorf("total = %d, want %d", total, 11*model.MaxPledgeQuantity)
	}
}

func TestConcurrentLastItems(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", MaxItems: 5})
	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Pledge(ctx, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: fmt.Sprintf("Parent %d", i), Quantity: 1})
			var pf *PledgeFullError
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.As(err, &pf):
				full++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 5 || full != n-5 {
		t.Errorf("want 5 ok / %d full, got %d / %d", n-5, ok, full)
	}
	if total := listTotal(t, s, l.ID); total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	// The refused people left no volunteer behind.
	wantVolunteers(t, s, ev.ID, 5)
}

func TestConcurrentSamePerson(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	first := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie", Quantity: 1})

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Pledge(ctx, PledgeRequest{EventID: ev.ID, ListID: l.ID, EditToken: first.Volunteer.EditToken, Name: "Marie", Quantity: i%model.MaxPledgeQuantity + 1})
			if err != nil {
				t.Errorf("pledge %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	rows, err := s.ListPledges(ctx, ev.ID)
	if err != nil || len(rows) != 1 || rows[0].VolunteerID != first.Volunteer.ID {
		t.Fatalf("rows = %+v, %v; want one row of Marie", rows, err)
	}
	wantVolunteers(t, s, ev.ID, 1)
}

func TestCancelPledge(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	other := newEvent(t, s, "Autre fête")
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	marie := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Marie", Quantity: 2}).Volunteer
	paul := mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: "Paul", Quantity: 3}).Volunteer
	rows, err := s.ListPledges(ctx, ev.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	mariePledge, paulPledge := rows[0].ID, rows[1].ID

	// Another volunteer cannot cancel the pledge of Marie.
	if _, err := s.CancelPledge(ctx, paul.ID, mariePledge); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel by another volunteer: want ErrNotFound, got %v", err)
	}
	// An organiser of another event cannot delete it either.
	if _, err := s.AdminDeletePledge(ctx, other.ID, mariePledge); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete through another event: want ErrNotFound, got %v", err)
	}
	if n := countPledges(t, s, l.ID); n != 2 {
		t.Fatalf("refused requests left %d rows, want 2", n)
	}

	// The owner cancels.
	listID, err := s.CancelPledge(ctx, marie.ID, mariePledge)
	if err != nil || listID != l.ID {
		t.Fatalf("cancel by the owner = %d, %v; want list %d", listID, err, l.ID)
	}
	if _, err := s.CancelPledge(ctx, marie.ID, mariePledge); !errors.Is(err, ErrNotFound) {
		t.Errorf("second cancel: want ErrNotFound, got %v", err)
	}
	if total := listTotal(t, s, l.ID); total != 3 {
		t.Errorf("total = %d, want 3", total)
	}

	// The organiser of the right event deletes the pledge of Paul.
	listID, err = s.AdminDeletePledge(ctx, ev.ID, paulPledge)
	if err != nil || listID != l.ID {
		t.Fatalf("admin delete = %d, %v; want list %d", listID, err, l.ID)
	}
	if n := countPledges(t, s, l.ID); n != 0 {
		t.Errorf("%d rows left, want 0", n)
	}
	// The volunteers stay: only the pledges went.
	wantVolunteers(t, s, ev.ID, 2)
}

func TestListPledgesOldestFirstAndScoped(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	other := newEvent(t, s, "Autre fête")
	lo := newPledgeList(t, s, model.PledgeList{EventID: other.ID, Name: "Chaises"})
	mustPledge(t, s, PledgeRequest{EventID: other.ID, ListID: lo.ID, Name: "Luc", Quantity: 1})

	names := []string{"Anne", "Zoé", "Marie", "Paul"}
	tokens := map[string]string{}
	for _, name := range names {
		tokens[name] = mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: name, Quantity: 1}).Volunteer.EditToken
	}
	// A change keeps the place of the person in the order.
	mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, EditToken: tokens["Anne"], Name: "Anne", Quantity: 4})

	rows, err := s.ListPledges(ctx, ev.ID)
	if err != nil || len(rows) != len(names) {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	for i, r := range rows {
		if r.Name != names[i] {
			t.Errorf("row %d is %q, want %q", i, r.Name, names[i])
		}
	}
	if rows[0].Quantity != 4 {
		t.Errorf("Anne holds %d, want 4", rows[0].Quantity)
	}
}

func TestPurgeAndCascadeRemovePledges(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	a := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	b := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Boissons"})
	pledgeBoth := func() {
		t.Helper()
		mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: a.ID, Name: "Marie", Quantity: 2})
		mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: b.ID, Name: "Paul", Quantity: 3})
	}

	pledgeBoth()
	if _, err := s.PurgeVolunteers(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	if na, nb := countPledges(t, s, a.ID), countPledges(t, s, b.ID); na != 0 || nb != 0 {
		t.Errorf("after PurgeVolunteers: %d and %d pledges, want none", na, nb)
	}
	if lists, err := s.ListPledgeLists(ctx, ev.ID); err != nil || len(lists) != 2 {
		t.Errorf("PurgeVolunteers changed the lists: %+v, %v", lists, err)
	}

	pledgeBoth()
	if err := s.DeletePledgeList(ctx, ev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if n := countPledges(t, s, a.ID); n != 0 {
		t.Errorf("after DeletePledgeList: %d pledges on the list, want 0", n)
	}
	if n := countPledges(t, s, b.ID); n != 1 {
		t.Errorf("the other list has %d pledges, want 1", n)
	}
}

// The organizer has no token. Adding the same name again changes the
// quantity of the first pledge and keeps the phone number.
func TestAdminPledgeSamePersonTwice(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	add := func(name, phone string, quantity int) *PledgeResult {
		return mustPledge(t, s, PledgeRequest{EventID: ev.ID, ListID: l.ID, Name: name, Phone: phone, Quantity: quantity, Admin: true})
	}

	first := add("Marie Dupont", "0611223344", 2)
	if !first.NewVolunteer {
		t.Fatal("the first pledge must create a volunteer")
	}
	second := add("  marie   dupont ", "", 3)
	if second.NewVolunteer || second.Previous != 2 || second.Volunteer.ID != first.Volunteer.ID {
		t.Fatalf("second pledge = %+v, want the same volunteer with Previous 2", second)
	}
	if second.Volunteer.Phone != "0611223344" {
		t.Errorf("phone = %q, want it kept", second.Volunteer.Phone)
	}
	wantVolunteers(t, s, ev.ID, 1)
	if total := listTotal(t, s, l.ID); total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	add("Paul Martin", "", 1)
	wantVolunteers(t, s, ev.ID, 2)
}
