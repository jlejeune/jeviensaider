package model

import (
	"errors"
	"time"
)

// Limits of the pledge lists.
const (
	MaxPledgeItems    = 1000 // limit of MinItems and MaxItems
	MaxPledgeQuantity = 20   // limit of one person's quantity
)

// PledgeList is a list of things that people promise to bring, such as cakes.
type PledgeList struct {
	ID, EventID                      int64
	Name, Description, Unit, DropOff string // DropOff is "HH:MM" or ""
	MinItems, MaxItems               int    // 0 = unset
}

// Pledge is the quantity of items that one volunteer promises to bring.
type Pledge struct {
	ID, ListID, VolunteerID int64
	Quantity                int
	CreatedAt               time.Time
}

// PledgeRow is a pledge joined with its volunteer, as listed on pages.
type PledgeRow struct {
	Pledge
	Name, Phone string
}

// Errors returned by PledgeList.Check.
var (
	ErrPledgeNumber  = errors.New("pledge minimum or maximum is out of range")
	ErrPledgeRange   = errors.New("pledge maximum is below its minimum")
	ErrPledgeDropOff = errors.New("pledge drop-off time is not HH:MM")
)

// dropOffLayout is the layout of PledgeList.DropOff.
const dropOffLayout = "15:04"

// ValidDropOff reports whether s is empty or a time from 00:00 to 23:59.
// The time must have exactly two digits for the hour, as in "08:30".
func ValidDropOff(s string) bool {
	if s == "" {
		return true
	}
	// time.Parse also accepts "8:30", so the shape needs its own check.
	if len(s) != len(dropOffLayout) || s[2] != ':' {
		return false
	}
	_, err := time.Parse(dropOffLayout, s)
	return err == nil
}

// Check verifies the numbers and the drop-off time of a list. The minimum
// and the maximum (0 = unset) are from 0 to MaxPledgeItems. The maximum
// is not below the minimum.
func (l PledgeList) Check() error {
	switch {
	case l.MinItems < 0 || l.MinItems > MaxPledgeItems,
		l.MaxItems < 0 || l.MaxItems > MaxPledgeItems:
		return ErrPledgeNumber
	case l.MaxItems != 0 && l.MaxItems < l.MinItems:
		return ErrPledgeRange
	case !ValidDropOff(l.DropOff):
		return ErrPledgeDropOff
	}
	return nil
}

// PledgeListView is a list with everything needed to render it.
type PledgeListView struct {
	PledgeList
	Pledges []PledgeRow // oldest first
	Total   int         // sum of quantities
	Missing int         // MinItems - Total, 0 if no minimum or reached
	Left    int         // MaxItems - Total, 0 if no maximum or full
	Full    bool        // MaxItems > 0 and Total >= MaxItems
	// For the current volunteer (Planning.MyID):
	Mine       bool  // has a pledge on this list
	MyPledgeID int64 // that pledge, 0 if none (to cancel it)
	MyQuantity int   // its quantity, 0 if none
	// MaxQuantity is the most that this visitor can hold on the list. It is
	// 0 if they cannot pledge.
	MaxQuantity int
	// CanSignUp: the event is open and MaxQuantity is above 0.
	CanSignUp bool
}

// newPledgeListView sums the rows of one list. The rows must be oldest first.
func (p *Planning) newPledgeListView(l PledgeList, rows []PledgeRow) *PledgeListView {
	v := &PledgeListView{PledgeList: l, Pledges: rows}
	for _, r := range rows {
		v.Total += r.Quantity
		if p.MyID != 0 && r.VolunteerID == p.MyID {
			v.Mine, v.MyPledgeID, v.MyQuantity = true, r.ID, r.Quantity
		}
	}
	if l.MinItems > v.Total {
		v.Missing = l.MinItems - v.Total
	}
	if l.MaxItems > 0 {
		v.Full = v.Total >= l.MaxItems
		if !v.Full {
			v.Left = l.MaxItems - v.Total
		}
	}
	v.MaxQuantity = v.maxQuantity(p.Open)
	v.CanSignUp = p.Open && v.MaxQuantity > 0
	return v
}

// maxQuantity is the most that the current volunteer can hold on the list:
// the room left plus what they already hold, up to MaxPledgeQuantity.
func (v *PledgeListView) maxQuantity(open bool) int {
	switch {
	case !open:
		return 0
	case v.MaxItems == 0:
		return MaxPledgeQuantity
	}
	return min(MaxPledgeQuantity, v.Left+v.MyQuantity)
}
