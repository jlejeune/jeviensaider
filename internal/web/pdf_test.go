package web

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"jeviensaider/internal/model"
)

// planningWith builds a planning with one stand per entry of slotsPerStand,
// each slot from 8:00 for an hour, needing perSlot people.
func planningWith(slotsPerStand []int, perSlot int) *model.Planning {
	var stands []model.Stand
	var slots []model.Slot
	start := time.Date(2026, 6, 20, 8, 0, 0, 0, time.Local)
	id := int64(1)
	for i, n := range slotsPerStand {
		st := model.Stand{ID: int64(i + 1), Name: fmt.Sprintf("Stand %d", i+1), Color: "#2aa3b5",
			Description: "Une description assez longue pour tenir sur plusieurs lignes, avec des accents : fête, crêpes, œufs, à côté."}
		stands = append(stands, st)
		for j := range n {
			s := start.Add(time.Duration(j) * time.Hour)
			slots = append(slots, model.Slot{ID: id, StandID: st.ID, StartsAt: s, EndsAt: s.Add(time.Hour), MinPeople: perSlot})
			id++
		}
	}
	signups := []model.SignupRow{{Signup: model.Signup{ID: 1, SlotID: 1, VolunteerID: 1, Comment: "avec ma fille"}, Name: "Zoé Lefèvre", Phone: "06 12 34 56 78"}}
	return model.BuildPlanning(stands, slots, nil, signups, 0, true)
}

func TestRowHeightAndPagination(t *testing.T) {
	const first, next = 600.0, 680.0

	// Few lines: comfortable rows, one page.
	small := standBlocks(planningWith([]int{5}, 2).Stands[0]) // 10 lines
	if h := rowHeight(small, first, next); h != rowMax {
		t.Errorf("10 lines: row %v, want %v", h, rowMax)
	}
	// More lines: rows shrink to stay on one page.
	medium := standBlocks(planningWith([]int{15}, 2).Stands[0]) // 30 lines
	if h := rowHeight(medium, first, next); h != 20 || len(paginate(medium, h, first, next)) != 1 {
		t.Errorf("30 lines: row %v", h)
	}

	p := planningWith([]int{12}, 2) // 24 lines
	blocks := standBlocks(p.Stands[0])
	if n := countLines(blocks); n != 24 {
		t.Fatalf("%d lines, want 24", n)
	}
	h := rowHeight(blocks, first, next)
	if pages := paginate(blocks, h, first, next); len(pages) != 1 {
		t.Errorf("24 lines on %d pages, want 1", len(pages))
	}

	// 20 slots of 4: two pages, and no slot cut between them.
	p = planningWith([]int{20}, 4)
	blocks = standBlocks(p.Stands[0])
	h = rowHeight(blocks, first, next)
	pages := paginate(blocks, h, first, next)
	if len(pages) != len(paginate(blocks, rowMin, first, next)) || len(pages) != 2 || !pages[0].First || pages[1].First {
		t.Fatalf("%d pages", len(pages))
	}
	lines, seen := 0, map[int64]bool{}
	for _, pg := range pages {
		used := 0.0
		for _, b := range pg.Blocks {
			if seen[b.Slot.ID] {
				t.Errorf("slot %d cut across pages", b.Slot.ID)
			}
			seen[b.Slot.ID] = true
			lines += len(b.Lines)
			used += float64(len(b.Lines)) * h
		}
		if room := map[bool]float64{true: first, false: next}[pg.First]; used > room+1e-6 {
			t.Errorf("page overflows: %v > %v", used, room)
		}
	}
	if lines != 80 {
		t.Errorf("%d lines placed, want 80", lines)
	}

	// A slot longer than a page is split.
	p = planningWith([]int{1}, 60)
	blocks = standBlocks(p.Stands[0])
	if pages := paginate(blocks, rowMin, first, next); len(pages) != 2 {
		t.Errorf("a 60-person slot on %d pages, want 2", len(pages))
	}
}

func countPages(pdf []byte) int {
	return len(regexp.MustCompile(`/Type /Page\b[^s]`).FindAll(pdf, -1))
}

func TestPlanningPDF(t *testing.T) {
	ev := &model.Event{Title: "Fête de l'école", Date: "2026-06-20", Location: "Cour"}
	for _, c := range []struct {
		name  string
		p     *model.Planning
		pages int
	}{
		{"no stand", planningWith(nil, 2), 1},
		{"three small stands", planningWith([]int{3, 6, 9}, 2), 3},
		{"a stand without slots", planningWith([]int{0}, 2), 1},
		{"one long stand", planningWith([]int{30}, 4), 3},
	} {
		var buf bytes.Buffer
		if err := writePlanningPDF(&buf, ev, c.p, time.Now()); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
			t.Fatalf("%s: not a PDF", c.name)
		}
		if n := countPages(buf.Bytes()); n != c.pages {
			t.Errorf("%s: %d pages, want %d", c.name, n, c.pages)
		}
	}
}

// planningWithPledges builds a planning with no stand and the given number
// of pledge lists. Each list has perList pledges with quantities 2, 1, 3,
// 2, 1, 3 and so on.
func planningWithPledges(lists, perList int) *model.Planning {
	p := model.BuildPlanning(nil, nil, nil, nil, 0, true)
	var ls []model.PledgeList
	var rows []model.PledgeRow
	id := int64(1)
	for i := range lists {
		l := model.PledgeList{ID: int64(i + 1), Name: fmt.Sprintf("Gâteaux %d", i+1), Unit: "gâteaux",
			Description: "Sucrés ou salés, faits maison.", DropOff: "08:30", MinItems: 10}
		ls = append(ls, l)
		for j := range perList {
			rows = append(rows, model.PledgeRow{
				Pledge: model.Pledge{ID: id, ListID: l.ID, VolunteerID: id, Quantity: []int{2, 1, 3}[j%3]},
				Name:   fmt.Sprintf("Zoé Lefèvre %d", j+1), Phone: "06 12 34 56 78"})
			id++
		}
	}
	p.AddPledges(ls, rows)
	return p
}

func TestPledgeBlocks(t *testing.T) {
	l := planningWithPledges(1, 3).Pledges[0]
	blocks := pledgeBlocks(l)
	if len(blocks) != 3 {
		t.Fatalf("%d blocks, want 3", len(blocks))
	}
	for i, want := range []string{"2", "1", "3"} {
		b := blocks[i]
		if b.Slot != nil || b.Label != want {
			t.Errorf("block %d: slot %v, label %q, want no slot and %q", i, b.Slot, b.Label, want)
		}
		if len(b.Lines) != 1 || b.Lines[0] == nil {
			t.Fatalf("block %d: %d lines, want one filled line", i, len(b.Lines))
		}
		if su := b.Lines[0]; su.Name != l.Pledges[i].Name || su.Phone != l.Pledges[i].Phone {
			t.Errorf("block %d: line %q %q, want %q %q", i, su.Name, su.Phone, l.Pledges[i].Name, l.Pledges[i].Phone)
		}
	}
}

func TestPaginateKeepsLabel(t *testing.T) {
	// A block of 10 lines on pages of 4 lines is split across 3 pages.
	blocks := []pdfBlock{
		{Label: "2", Lines: make([]*model.SignupRow, 1)},
		{Label: "long", Lines: make([]*model.SignupRow, 10)},
	}
	pages := paginate(blocks, rowMin, 4*rowMin, 4*rowMin)
	if len(pages) < 2 {
		t.Fatalf("%d pages, want the long block split", len(pages))
	}
	n := 0
	for _, pg := range pages {
		for _, b := range pg.Blocks {
			n++
			if b.Label != "2" && b.Label != "long" {
				t.Errorf("block %d: label %q", n, b.Label)
			}
		}
	}
	if first := pages[0].Blocks[0]; first.Label != "2" {
		t.Errorf("first block: label %q, want %q", first.Label, "2")
	}
	if n < 3 {
		t.Errorf("%d blocks placed, want the long block in several parts", n)
	}
}

// A long description is cut, but the drop-off time and the status stay.
func TestPledgeDescLinesKeepDropOffAndStatus(t *testing.T) {
	l := planningWithPledges(1, 1).Pledges[0] // 2 items on a minimum of 10
	var desc []string
	for i := range 10 {
		desc = append(desc, fmt.Sprintf("- article autorisé %d", i+1))
	}
	l.Description = strings.Join(desc, "\n")
	d, err := newPDFDoc()
	if err != nil {
		t.Fatal(err)
	}
	lines := d.pledgeDescLines(l)
	if len(lines) > maxDescLines {
		t.Errorf("%d lines, want at most %d", len(lines), maxDescLines)
	}
	n := len(lines)
	if n < 3 || lines[0] != desc[0] || lines[n-2] != "Dépôt à 8h30" || lines[n-1] != pledgeStatus(l) {
		t.Errorf("lines = %q, want the description, then %q, then %q", lines, "Dépôt à 8h30", pledgeStatus(l))
	}

	// A long unit makes a long status. The block still has at most
	// maxDescLines lines and keeps the drop-off time and the status.
	long := *l
	long.Unit = strings.Repeat("gâteaux-au-chocolat-et-aux-noisettes ", 12)
	long.Total, long.Full = 20, true
	lines = d.pledgeDescLines(&long)
	if len(lines) > maxDescLines {
		t.Errorf("long status: %d lines, want at most %d", len(lines), maxDescLines)
	}
	if len(lines) < 2 || !slices.Contains(lines, "Dépôt à 8h30") || !strings.HasPrefix(lines[len(lines)-1], "gâteaux") {
		t.Errorf("long status: lines = %q, want the drop-off time and the end of the status", lines)
	}

	// With no description and no drop-off time, only the status is left.
	l.Description, l.DropOff = "", ""
	if lines := d.pledgeDescLines(l); len(lines) != 1 || lines[0] != pledgeStatus(l) {
		t.Errorf("lines = %q, want only the status", lines)
	}
}

func TestPlanningPDFWithPledges(t *testing.T) {
	ev := &model.Event{Title: "Fête de l'école", Date: "2026-06-20", Location: "Cour"}
	standAndList := planningWith([]int{3}, 2)
	standAndList.Pledges = planningWithPledges(1, 3).Pledges
	for _, c := range []struct {
		name  string
		p     *model.Planning
		pages int
	}{
		{"two lists and no stand", planningWithPledges(2, 3), 2},
		{"one stand and one list", standAndList, 2},
		{"a list of 60 pledges", planningWithPledges(1, 60), 2},
		{"a list with no pledge", planningWithPledges(1, 0), 1},
	} {
		var buf bytes.Buffer
		if err := writePlanningPDF(&buf, ev, c.p, time.Now()); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
			t.Fatalf("%s: not a PDF", c.name)
		}
		if n := countPages(buf.Bytes()); n != c.pages {
			t.Errorf("%s: %d pages, want %d", c.name, n, c.pages)
		}
	}
}

func countLines(blocks []pdfBlock) int {
	n := 0
	for _, b := range blocks {
		n += len(b.Lines)
	}
	return n
}
