package model

import (
	"sort"
	"time"
)

// SlotView is a slot with everything needed to render it.
type SlotView struct {
	Slot
	Stand   *Stand
	Signups []SignupRow
	Count   int
	Status  string // one of the Fill* constants
	Missing int
	Full    bool
	// For the current volunteer (Planning.MyID):
	Mine       bool  // signed up on this slot
	MySignupID int64 // their sign-up on it, 0 if none (to cancel it)
	Clash      bool  // Mine, and overlaps another of their slots
	// CanSignUp: the event is open, the slot is not full and the current
	// visitor is not on it yet.
	CanSignUp bool
}

// StandView is a stand with its slots, in chronological order.
type StandView struct {
	Stand
	Slots   []*SlotView
	Missing int // people still needed over all slots
	Needed  int // sum of minimums
	Count   int // sum of sign-ups
}

// Column is a distinct time interval of the planning grid.
type Column struct {
	StartsAt time.Time
	EndsAt   time.Time
	// Title is the title of the moment that has this exact time range. It is
	// empty when no moment has it.
	Title string
	// Color is the color of that moment: empty, or "#rrggbb".
	Color string
	// Fill is set when the moment of that color tints the whole column.
	Fill bool
	// MomentOnly means that the column exists for a moment and has no slot.
	MomentOnly bool
}

// span is the time range that identifies a column.
type span struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// Row is one stand in the grid; Cells is aligned with Planning.Columns.
type Row struct {
	Stand *StandView
	Cells [][]*SlotView // usually zero or one slot per cell
}

// TimeGroup lists, for one column, the slots of every stand (mobile view).
type TimeGroup struct {
	Column
	Slots []*SlotView
}

// Planning is the full, ready-to-render view of an event.
type Planning struct {
	Stands   []*StandView
	Columns  []Column
	Rows     []Row
	Groups   []TimeGroup
	MultiDay bool
	Missing  int
	Needed   int
	Count    int
	Open     bool        // the event accepts sign-ups
	MyID     int64       // the current volunteer, 0 if unknown
	MySlots  []*SlotView // their slots, chronologically
	HasClash bool        // two of their slots overlap

	// Pledges are the pledge lists, in display order. They are empty until
	// AddPledges is called. MyPledges holds the lists where the current
	// volunteer has a pledge, in the same order.
	Pledges   []*PledgeListView
	MyPledges []*PledgeListView

	columnIndex map[span]int // position of each column in Columns
}

// BuildPlanning assembles stands, slots, moments and sign-ups into a
// Planning. stands must be in display order. myVolunteerID may be 0; open
// says whether the event accepts sign-ups.
func BuildPlanning(stands []Stand, slots []Slot, moments []Moment, signups []SignupRow, myVolunteerID int64, open bool) *Planning {
	p := &Planning{MyID: myVolunteerID, Open: open, columnIndex: map[span]int{}}
	byStand := p.addStands(stands)
	views := p.addSlots(byStand, chronological(slots), signupsBySlot(signups))
	p.addMoments(moments)
	p.buildRows()
	p.buildGroups(views)
	p.markClashes()
	p.MultiDay = spansSeveralDays(p.Columns)
	return p
}

// addStands creates the stand views and returns them by ID.
func (p *Planning) addStands(stands []Stand) map[int64]*StandView {
	byStand := make(map[int64]*StandView, len(stands))
	for i := range stands {
		sv := &StandView{Stand: stands[i]}
		p.Stands = append(p.Stands, sv)
		byStand[sv.ID] = sv
	}
	return byStand
}

// addSlots creates the slot views (in the given order), adds them to their
// stand, sums the totals and collects the distinct time columns.
func (p *Planning) addSlots(byStand map[int64]*StandView, slots []Slot, bySlot map[int64][]SignupRow) []*SlotView {
	var views []*SlotView
	for _, sl := range slots {
		st := byStand[sl.StandID]
		if st == nil {
			continue
		}
		v := p.newSlotView(sl, &st.Stand, bySlot[sl.ID])
		st.Slots = append(st.Slots, v)
		st.Missing += v.Missing
		st.Needed += sl.MinPeople
		st.Count += v.Count
		p.Missing += v.Missing
		p.Needed += sl.MinPeople
		p.Count += v.Count
		if v.Mine {
			p.MySlots = append(p.MySlots, v)
		}
		k := span{sl.StartsAt, sl.EndsAt}
		if _, ok := p.columnIndex[k]; !ok {
			p.columnIndex[k] = len(p.Columns)
			p.Columns = append(p.Columns, Column{StartsAt: sl.StartsAt, EndsAt: sl.EndsAt})
		}
		views = append(views, v)
	}
	return views
}

// addMoments gives the title of each moment to the column that has its time
// range. The match is exact: a moment from 10:00 to 12:00 does not cover the
// slots 10:00 to 11:00 and 11:00 to 12:00. A moment with no such column adds
// a column of its own, with no slots. The columns are then sorted again by start, then end.
func (p *Planning) addMoments(moments []Moment) {
	if len(moments) == 0 {
		return
	}
	for _, m := range moments {
		k := span{m.StartsAt, m.EndsAt}
		if i, ok := p.columnIndex[k]; ok {
			if p.Columns[i].Title == "" {
				p.Columns[i].Title, p.Columns[i].Color, p.Columns[i].Fill = m.Title, m.Color, m.Tints()
			}
			continue
		}
		p.columnIndex[k] = len(p.Columns)
		p.Columns = append(p.Columns, Column{StartsAt: m.StartsAt, EndsAt: m.EndsAt, Title: m.Title, Color: m.Color, Fill: m.Tints(), MomentOnly: true})
	}
	sort.SliceStable(p.Columns, func(i, j int) bool {
		a, b := p.Columns[i], p.Columns[j]
		if !a.StartsAt.Equal(b.StartsAt) {
			return a.StartsAt.Before(b.StartsAt)
		}
		return a.EndsAt.Before(b.EndsAt)
	})
	for i, c := range p.Columns {
		p.columnIndex[span{c.StartsAt, c.EndsAt}] = i
	}
}

func (p *Planning) newSlotView(sl Slot, stand *Stand, signups []SignupRow) *SlotView {
	v := &SlotView{Slot: sl, Stand: stand, Signups: signups, Count: len(signups)}
	v.Status = sl.FillStatus(v.Count)
	v.Missing = sl.Missing(v.Count)
	v.Full = sl.IsFull(v.Count)
	for _, su := range signups {
		if p.MyID != 0 && su.VolunteerID == p.MyID {
			v.Mine, v.MySignupID = true, su.ID
		}
	}
	v.CanSignUp = p.Open && !v.Full && !v.Mine
	return v
}

// buildRows lays out each stand's slots in the grid columns.
func (p *Planning) buildRows() {
	for _, st := range p.Stands {
		row := Row{Stand: st, Cells: make([][]*SlotView, len(p.Columns))}
		for _, v := range st.Slots {
			i := p.column(v)
			row.Cells[i] = append(row.Cells[i], v)
		}
		p.Rows = append(p.Rows, row)
	}
}

// buildGroups lists, for each column, its slots of every stand.
func (p *Planning) buildGroups(views []*SlotView) {
	p.Groups = make([]TimeGroup, len(p.Columns))
	for i, c := range p.Columns {
		p.Groups[i].Column = c
	}
	for _, v := range views {
		i := p.column(v)
		p.Groups[i].Slots = append(p.Groups[i].Slots, v)
	}
}

func (p *Planning) column(v *SlotView) int {
	return p.columnIndex[span{v.StartsAt, v.EndsAt}]
}

// markClashes flags the current volunteer's slots that overlap each other.
func (p *Planning) markClashes() {
	for i, a := range p.MySlots {
		for _, b := range p.MySlots[i+1:] {
			if a.Overlaps(b.Slot) {
				a.Clash, b.Clash = true, true
				p.HasClash = true
			}
		}
	}
}

// chronological returns a copy of slots sorted by start, then end. The sort
// is stable, so slots at the same time keep their order (the stands').
func chronological(slots []Slot) []Slot {
	sorted := append([]Slot(nil), slots...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].StartsAt.Equal(sorted[j].StartsAt) {
			return sorted[i].StartsAt.Before(sorted[j].StartsAt)
		}
		return sorted[i].EndsAt.Before(sorted[j].EndsAt)
	})
	return sorted
}

func signupsBySlot(signups []SignupRow) map[int64][]SignupRow {
	bySlot := make(map[int64][]SignupRow)
	for _, su := range signups {
		bySlot[su.SlotID] = append(bySlot[su.SlotID], su)
	}
	return bySlot
}

// spansSeveralDays reports whether the columns are not all on the same day
// (the views then show the day next to the times).
func spansSeveralDays(cols []Column) bool {
	for _, c := range cols {
		if c.StartsAt.YearDay() != cols[0].StartsAt.YearDay() || c.StartsAt.Year() != cols[0].StartsAt.Year() {
			return true
		}
	}
	return false
}

// HasSlots reports whether at least one column has slots, so whether the
// event has something to sign up for.
func (p *Planning) HasSlots() bool {
	for _, c := range p.Columns {
		if !c.MomentOnly {
			return true
		}
	}
	return false
}

// HasMomentColumns reports whether at least one column exists only for a
// moment.
func (p *Planning) HasMomentColumns() bool {
	for _, c := range p.Columns {
		if c.MomentOnly {
			return true
		}
	}
	return false
}

// Slot returns the slot view with the given id, or nil.
func (p *Planning) Slot(id int64) *SlotView {
	for _, st := range p.Stands {
		for _, v := range st.Slots {
			if v.ID == id {
				return v
			}
		}
	}
	return nil
}

// Mine is the planning restricted to the current volunteer's slots (and
// their stands): their personal overview.
func (p *Planning) Mine() *Planning {
	var stands []Stand
	var slots []Slot
	var signups []SignupRow
	for _, st := range p.Stands {
		added := false
		for _, v := range st.Slots {
			if !v.Mine {
				continue
			}
			if !added {
				stands = append(stands, st.Stand)
				added = true
			}
			slots = append(slots, v.Slot)
			signups = append(signups, v.Signups...)
		}
	}
	return BuildPlanning(stands, slots, nil, signups, p.MyID, p.Open)
}

// MyOverlaps returns the current volunteer's other slots that overlap v.
func (p *Planning) MyOverlaps(v *SlotView) []*SlotView {
	var out []*SlotView
	for _, m := range p.MySlots {
		if m.ID != v.ID && m.Overlaps(v.Slot) {
			out = append(out, m)
		}
	}
	return out
}

// MyDuration is the total time of the current volunteer's slots.
func (p *Planning) MyDuration() time.Duration {
	var d time.Duration
	for _, v := range p.MySlots {
		d += v.EndsAt.Sub(v.StartsAt)
	}
	return d
}

// AddPledges builds the views of the pledge lists and adds them to the
// planning. lists must be in display order. rows must be oldest first. A row
// whose list is not in lists is ignored.
func (p *Planning) AddPledges(lists []PledgeList, rows []PledgeRow) {
	byList := make(map[int64][]PledgeRow)
	for _, r := range rows {
		byList[r.ListID] = append(byList[r.ListID], r)
	}
	for _, l := range lists {
		v := p.newPledgeListView(l, byList[l.ID])
		p.Pledges = append(p.Pledges, v)
		if v.Mine {
			p.MyPledges = append(p.MyPledges, v)
		}
	}
}

// PledgeListByID returns the pledge list view with the given id, or nil.
func (p *Planning) PledgeListByID(id int64) *PledgeListView {
	for _, v := range p.Pledges {
		if v.ID == id {
			return v
		}
	}
	return nil
}

// PledgeTotals sums the quantities and the minimums of all the lists. allMin
// is true when there is at least one list and every list has a minimum.
func (p *Planning) PledgeTotals() (total, minSum int, allMin bool) {
	allMin = len(p.Pledges) > 0
	for _, v := range p.Pledges {
		total += v.Total
		minSum += v.MinItems
		if v.MinItems == 0 {
			allMin = false
		}
	}
	return total, minSum, allMin
}
