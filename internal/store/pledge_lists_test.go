package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"jeviensaider/internal/model"
)

// rawVolunteer inserts a volunteer with SQL, because the sign-up code is not
// the subject of these tests.
func rawVolunteer(t *testing.T, s *Store, eventID int64, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO volunteers (event_id, name, edit_token) VALUES (?, ?, ?)`, eventID, name, Token())
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// rawPledge inserts a pledge with SQL, for the same reason.
func rawPledge(t *testing.T, s *Store, listID, volunteerID int64, quantity int) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO pledges (list_id, volunteer_id, quantity) VALUES (?, ?, ?)`, listID, volunteerID, quantity); err != nil {
		t.Fatal(err)
	}
}

func newEvent(t *testing.T, s *Store, title string) *model.Event {
	t.Helper()
	ev := &model.Event{Title: title, Date: "2026-06-20", Status: model.StatusOpen}
	if err := s.CreateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func newPledgeList(t *testing.T, s *Store, l model.PledgeList) model.PledgeList {
	t.Helper()
	if err := s.CreatePledgeList(ctx, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPledgeListCRUD(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	other := newEvent(t, s, "Autre fête")

	a := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", Description: "Sans noix", Unit: "gâteaux", DropOff: "08:30", MinItems: 5, MaxItems: 20})
	b := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Boissons"})
	newPledgeList(t, s, model.PledgeList{EventID: other.ID, Name: "Chaises"})
	if a.ID == 0 || b.ID <= a.ID {
		t.Fatalf("IDs not set in order: %d, %d", a.ID, b.ID)
	}

	got, err := s.ListPledgeLists(ctx, ev.ID)
	if err != nil || len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("ListPledgeLists = %+v, %v; want %+v and %+v", got, err, a, b)
	}

	one, err := s.GetPledgeList(ctx, ev.ID, a.ID)
	if err != nil || one != a {
		t.Fatalf("GetPledgeList = %+v, %v; want %+v", one, err, a)
	}
	if _, err := s.GetPledgeList(ctx, other.ID, a.ID); err != ErrNotFound {
		t.Errorf("GetPledgeList with another event: want ErrNotFound, got %v", err)
	}

	// Update every field. The store writes a minimum and a maximum of 0 as unset.
	upd := model.PledgeList{ID: b.ID, EventID: ev.ID, Name: "Jus", Description: "Sans sucre", Unit: "bouteilles", DropOff: "09:15", MinItems: 2, MaxItems: 6}
	if err := s.UpdatePledgeList(ctx, upd); err != nil {
		t.Fatal(err)
	}
	if one, err := s.GetPledgeList(ctx, ev.ID, b.ID); err != nil || one != upd {
		t.Fatalf("after update: %+v, %v; want %+v", one, err, upd)
	}
	upd.MinItems, upd.MaxItems, upd.DropOff, upd.Unit = 0, 0, "", ""
	if err := s.UpdatePledgeList(ctx, upd); err != nil {
		t.Fatal(err)
	}
	if one, err := s.GetPledgeList(ctx, ev.ID, b.ID); err != nil || one != upd {
		t.Fatalf("after clearing: %+v, %v; want %+v", one, err, upd)
	}

	// Another event cannot change or delete the list.
	wrong := upd
	wrong.EventID = other.ID
	wrong.Name = "Pirate"
	if err := s.UpdatePledgeList(ctx, wrong); err != ErrNotFound {
		t.Errorf("UpdatePledgeList with another event: want ErrNotFound, got %v", err)
	}
	if err := s.DeletePledgeList(ctx, other.ID, b.ID); err != ErrNotFound {
		t.Errorf("DeletePledgeList with another event: want ErrNotFound, got %v", err)
	}
	if one, err := s.GetPledgeList(ctx, ev.ID, b.ID); err != nil || one.Name != "Jus" {
		t.Fatalf("list changed by another event: %+v, %v", one, err)
	}

	if err := s.DeletePledgeList(ctx, ev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPledgeList(ctx, ev.ID, a.ID); err != ErrNotFound {
		t.Errorf("after delete: want ErrNotFound, got %v", err)
	}
	if err := s.DeletePledgeList(ctx, ev.ID, a.ID); err != ErrNotFound {
		t.Errorf("second delete: want ErrNotFound, got %v", err)
	}
	if got, err := s.ListPledgeLists(ctx, ev.ID); err != nil || len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("ListPledgeLists after delete = %+v, %v", got, err)
	}
}

func TestPledgeListValidation(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)

	for _, tc := range []struct {
		name string
		list model.PledgeList
		want error
	}{
		{"max below min", model.PledgeList{EventID: ev.ID, Name: "Gâteaux", MinItems: 5, MaxItems: 4}, model.ErrPledgeRange},
		{"bad drop-off", model.PledgeList{EventID: ev.ID, Name: "Gâteaux", DropOff: "8h"}, model.ErrPledgeDropOff},
	} {
		if err := s.CreatePledgeList(ctx, &tc.list); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	if got, _ := s.ListPledgeLists(ctx, ev.ID); len(got) != 0 {
		t.Fatalf("refused lists were stored: %+v", got)
	}

	for i := 0; i < MaxPledgeLists; i++ {
		l := model.PledgeList{EventID: ev.ID, Name: fmt.Sprintf("Liste %d", i+1)}
		if err := s.CreatePledgeList(ctx, &l); err != nil {
			t.Fatalf("list %d: %v", i+1, err)
		}
	}
	extra := model.PledgeList{EventID: ev.ID, Name: "Une de trop"}
	if err := s.CreatePledgeList(ctx, &extra); !errors.Is(err, ErrTooManyPledgeLists) {
		t.Fatalf("11th list: want ErrTooManyPledgeLists, got %v", err)
	}
	if got, _ := s.ListPledgeLists(ctx, ev.ID); len(got) != MaxPledgeLists {
		t.Errorf("event has %d lists, want %d", len(got), MaxPledgeLists)
	}

	// The limit is per event.
	other := newEvent(t, s, "Autre fête")
	if err := s.CreatePledgeList(ctx, &model.PledgeList{EventID: other.ID, Name: "Gâteaux"}); err != nil {
		t.Errorf("list on another event: %v", err)
	}
}

func TestPledgeListSchemaChecks(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	res, err := s.db.Exec(`INSERT INTO pledge_lists (event_id, name) VALUES (?, 'Gâteaux')`, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	listID, _ := res.LastInsertId()

	// refused runs a statement and expects a CHECK constraint to refuse it.
	refused := func(what, query string, args ...any) {
		t.Helper()
		_, err := s.db.Exec(query, args...)
		if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("%s: want a CHECK constraint error, got %v", what, err)
		}
	}
	accepted := func(what, query string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(query, args...); err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}

	const insertPledge = `INSERT INTO pledges (list_id, volunteer_id, quantity) VALUES (?, ?, ?)`
	marie, paul := rawVolunteer(t, s, ev.ID, "Marie"), rawVolunteer(t, s, ev.ID, "Paul")
	accepted("quantity 20", insertPledge, listID, marie, 20)
	refused("quantity 0", insertPledge, listID, paul, 0)
	refused("quantity 21", insertPledge, listID, paul, 21)
	accepted("quantity 1", insertPledge, listID, paul, 1)
	if _, err := s.db.Exec(insertPledge, listID, paul, 2); err == nil {
		t.Error("a second pledge of the same volunteer on the same list is accepted")
	}

	const insertList = `INSERT INTO pledge_lists (event_id, name, drop_off, min_items, max_items) VALUES (?, 'Liste', ?, ?, ?)`
	accepted("drop-off 08:30", insertList, ev.ID, "08:30", 5, 5)
	refused("drop-off 8h", insertList, ev.ID, "8h", nil, nil)
	refused("max below min", insertList, ev.ID, "", 5, 4)
	refused("min 0", insertList, ev.ID, "", 0, nil)
	refused("max 0", insertList, ev.ID, "", nil, 0)
}

func TestUpdatePledgeListMaxBelowTotal(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux", MaxItems: 10})
	rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, "Marie"), 3)
	rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, "Paul"), 2)

	l.MaxItems = 4
	err := s.UpdatePledgeList(ctx, l)
	var maxErr *PledgeMaxError
	if !errors.As(err, &maxErr) || maxErr.Total != 5 {
		t.Fatalf("max 4: want *PledgeMaxError with Total 5, got %v", err)
	}
	if got, err := s.GetPledgeList(ctx, ev.ID, l.ID); err != nil || got.MaxItems != 10 {
		t.Fatalf("refused update changed the list: %+v, %v", got, err)
	}

	l.MaxItems = 5
	if err := s.UpdatePledgeList(ctx, l); err != nil {
		t.Fatalf("max 5: %v", err)
	}
	// Removing the maximum always works.
	l.MaxItems = 0
	if err := s.UpdatePledgeList(ctx, l); err != nil {
		t.Fatalf("no maximum: %v", err)
	}

	// The total of another list does not count.
	other := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Boissons"})
	other.MaxItems = 1
	if err := s.UpdatePledgeList(ctx, other); err != nil {
		t.Errorf("max 1 on an empty list: %v", err)
	}
}

// Another event cannot read the total of a list through the maximum check. The
// call finds no list, so it does not give the total in a *PledgeMaxError.
func TestUpdatePledgeListMaxFromAnotherEvent(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	other := newEvent(t, s, "Autre fête")
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, "Marie"), 3)

	wrong := l
	wrong.EventID = other.ID
	wrong.MaxItems = 1
	if err := s.UpdatePledgeList(ctx, wrong); err != ErrNotFound {
		t.Fatalf("max 1 from another event: want ErrNotFound, got %v", err)
	}
	if got, err := s.GetPledgeList(ctx, ev.ID, l.ID); err != nil || got != l {
		t.Fatalf("the list changed: %+v, %v; want %+v", got, err, l)
	}
}

// A database created before migration 005 keeps its data and gets the new
// tables.
func TestMigrationKeepsOlderData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_admin_email.sql", "003_drop_volunteer_email.sql", "004_retention_days.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 4;
		INSERT INTO events (id, slug, title, admin_token) VALUES (1, 'fete', 'Fête', 'tok');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev, err := s.EventBySlug(ctx, "fete")
	if err != nil || ev.Title != "Fête" {
		t.Fatalf("event after migration: %+v, %v", ev, err)
	}
	for _, table := range []string{"pledge_lists", "pledges"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s: count %d, %v", table, n, err)
		}
	}
	// The new tables work on the migrated database.
	if err := s.CreatePledgeList(ctx, &model.PledgeList{EventID: ev.ID, Name: "Gâteaux"}); err != nil {
		t.Error(err)
	}
}

func TestDuplicateCopiesPledgeLists(t *testing.T) {
	s := openTest(t)
	src, _, _ := fixture(t, s, 0)
	a := newPledgeList(t, s, model.PledgeList{EventID: src.ID, Name: "Gâteaux", Description: "Sans noix", Unit: "gâteaux", DropOff: "08:30", MinItems: 5, MaxItems: 20})
	b := newPledgeList(t, s, model.PledgeList{EventID: src.ID, Name: "Boissons"})
	rawPledge(t, s, a.ID, rawVolunteer(t, s, src.ID, "Marie"), 3)

	dst, err := s.DuplicateEvent(ctx, src, "Fête 2027", "2027-06-19")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListPledgeLists(ctx, dst.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("copied lists = %+v, %v", got, err)
	}
	for i, want := range []model.PledgeList{a, b} {
		if got[i].EventID != dst.ID || got[i].ID == want.ID {
			t.Errorf("copy %d: event %d, ID %d; want event %d and a new ID", i, got[i].EventID, got[i].ID, dst.ID)
		}
		got[i].ID, got[i].EventID = want.ID, want.EventID
		if got[i] != want {
			t.Errorf("copy %d = %+v, want %+v", i, got[i], want)
		}
	}

	var pledges int
	if err := s.db.QueryRow(`SELECT count(*) FROM pledges p JOIN pledge_lists l ON l.id = p.list_id WHERE l.event_id = ?`, dst.ID).Scan(&pledges); err != nil || pledges != 0 {
		t.Errorf("copied pledges: %d, %v", pledges, err)
	}
	if kept, err := s.ListPledgeLists(ctx, src.ID); err != nil || len(kept) != 2 {
		t.Errorf("source lists after duplicate: %+v, %v", kept, err)
	}
}

func TestDeleteEventRemovesPledges(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	l := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	rawPledge(t, s, l.ID, rawVolunteer(t, s, ev.ID, "Marie"), 3)

	if err := s.DeleteEvent(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	var lists, pledges int
	if err := s.db.QueryRow(`SELECT (SELECT count(*) FROM pledge_lists WHERE event_id = ?), (SELECT count(*) FROM pledges WHERE list_id = ?)`, ev.ID, l.ID).Scan(&lists, &pledges); err != nil {
		t.Fatal(err)
	}
	if lists != 0 || pledges != 0 {
		t.Errorf("rows left after DeleteEvent: %d lists, %d pledges", lists, pledges)
	}
}

func TestEventSummaryCountsPledges(t *testing.T) {
	s := openTest(t)
	ev, _, _ := fixture(t, s, 0)
	a := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Gâteaux"})
	b := newPledgeList(t, s, model.PledgeList{EventID: ev.ID, Name: "Boissons"})
	rawPledge(t, s, a.ID, rawVolunteer(t, s, ev.ID, "Marie"), 1)
	rawPledge(t, s, a.ID, rawVolunteer(t, s, ev.ID, "Paul"), 2)
	rawPledge(t, s, b.ID, rawVolunteer(t, s, ev.ID, "Anne"), 3)

	// Another event must not change the counts.
	other := newEvent(t, s, "Autre fête")
	c := newPledgeList(t, s, model.PledgeList{EventID: other.ID, Name: "Chaises"})
	rawPledge(t, s, c.ID, rawVolunteer(t, s, other.ID, "Luc"), 7)

	sums, err := s.ListEventSummaries(ctx)
	if err != nil || len(sums) != 2 {
		t.Fatalf("summaries: %+v, %v", sums, err)
	}
	for _, sum := range sums {
		want := [3]int{2, 3, 6}
		if sum.ID == other.ID {
			want = [3]int{1, 1, 7}
		}
		if got := [3]int{sum.PledgeLists, sum.Pledges, sum.Items}; got != want {
			t.Errorf("event %q: lists, pledges, items = %v, want %v", sum.Title, got, want)
		}
	}
}

// A database at the schema of the moments and fills (migrations 001 to 006)
// gets the pledge tables and keeps its events, stands and moments.
func TestUpgradeToPledgesKeepsMomentsAndStands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_admin_email.sql", "003_drop_volunteer_email.sql", "004_retention_days.sql", "005_moments.sql", "006_fill.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(string(body)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := old.Exec(`PRAGMA user_version = 6;
		INSERT INTO events (id, slug, title, admin_token, date) VALUES (1, 'fete', 'Fête', 'tok', '2026-06-20');
		INSERT INTO stands (id, event_id, name, position, color, fill_row) VALUES (1, 1, 'Buvette', 0, '#3d7cc9', 1);
		INSERT INTO moments (id, event_id, title, starts_at, ends_at, color, fill) VALUES (1, 1, 'Spectacle', '2026-06-20 11:00', '2026-06-20 12:00', '#e8c93a', 1);`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st, err := s.ListStands(ctx, 1); err != nil || len(st) != 1 || !st[0].FillRow {
		t.Errorf("stands after the upgrade: %+v, %v", st, err)
	}
	if ms, err := s.ListMoments(ctx, 1); err != nil || len(ms) != 1 || ms[0].Color != "#e8c93a" || !ms[0].Fill {
		t.Errorf("moments after the upgrade: %+v, %v", ms, err)
	}
	if err := s.CreatePledgeList(ctx, &model.PledgeList{EventID: 1, Name: "Gâteaux"}); err != nil {
		t.Error(err)
	}
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var version int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != len(files) {
		t.Errorf("user_version = %d, want %d (the number of migrations)", version, len(files))
	}
}
