package model

import (
	"errors"
	"testing"
)

func TestPledgeListCheck(t *testing.T) {
	tests := []struct {
		name string
		list PledgeList
		want error
	}{
		{"drop-off with a time", PledgeList{DropOff: "08:30"}, nil},
		{"drop-off empty", PledgeList{DropOff: ""}, nil},
		{"drop-off hour too big", PledgeList{DropOff: "25:00"}, ErrPledgeDropOff},
		{"drop-off with a letter", PledgeList{DropOff: "8h"}, ErrPledgeDropOff},
		{"drop-off without a leading zero", PledgeList{DropOff: "8:30"}, ErrPledgeDropOff},
		{"minimum too big", PledgeList{MinItems: 1001}, ErrPledgeNumber},
		{"maximum negative", PledgeList{MaxItems: -1}, ErrPledgeNumber},
		{"maximum below minimum", PledgeList{MinItems: 5, MaxItems: 4}, ErrPledgeRange},
		{"both numbers unset", PledgeList{MinItems: 0, MaxItems: 0}, nil},
		{"minimum without maximum", PledgeList{MinItems: 5}, nil},
		{"both numbers at the limit", PledgeList{MinItems: MaxPledgeItems, MaxItems: MaxPledgeItems}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.list.Check(); !errors.Is(got, tc.want) {
				t.Errorf("Check() = %v, want %v", got, tc.want)
			}
		})
	}
}

// pledgeRow builds a row for the volunteer vol with the given quantity.
func pledgeRow(id, listID, vol int64, qty int) PledgeRow {
	return PledgeRow{Pledge: Pledge{ID: id, ListID: listID, VolunteerID: vol, Quantity: qty}}
}

func TestPledgeListView(t *testing.T) {
	const me, other = 7, 8
	tests := []struct {
		name        string
		list        PledgeList
		rows        []PledgeRow
		visitor     int64
		closed      bool
		total       int
		missing     int
		left        int
		full        bool
		mine        bool
		myQuantity  int
		maxQuantity int
		canSignUp   bool
	}{
		{
			name: "full list, visitor holds part of it",
			list: PledgeList{ID: 1, MaxItems: 5},
			rows: []PledgeRow{pledgeRow(1, 1, me, 3), pledgeRow(2, 1, other, 2)}, visitor: me,
			total: 5, left: 0, full: true, mine: true, myQuantity: 3, maxQuantity: 3, canSignUp: true,
		},
		{
			name: "full list, another visitor",
			list: PledgeList{ID: 1, MaxItems: 5},
			rows: []PledgeRow{pledgeRow(1, 1, me, 3), pledgeRow(2, 1, other, 2)}, visitor: 99,
			total: 5, left: 0, full: true, maxQuantity: 0, canSignUp: false,
		},
		{
			name: "one item left, visitor holds part of it",
			list: PledgeList{ID: 1, MaxItems: 5},
			rows: []PledgeRow{pledgeRow(1, 1, me, 3), pledgeRow(2, 1, other, 1)}, visitor: me,
			total: 4, left: 1, mine: true, myQuantity: 3, maxQuantity: 4, canSignUp: true,
		},
		{
			name: "no limits and no rows",
			list: PledgeList{ID: 1}, visitor: me,
			maxQuantity: 20, canSignUp: true,
		},
		{
			name: "minimum not reached",
			list: PledgeList{ID: 1, MinItems: 21},
			rows: []PledgeRow{pledgeRow(1, 1, other, 10), pledgeRow(2, 1, 9, 4)}, visitor: me,
			total: 14, missing: 7, maxQuantity: 20, canSignUp: true,
		},
		{
			name: "minimum exceeded",
			list: PledgeList{ID: 1, MinItems: 21},
			rows: []PledgeRow{pledgeRow(1, 1, other, 20), pledgeRow(2, 1, 9, 5)}, visitor: me,
			total: 25, missing: 0, maxQuantity: 20, canSignUp: true,
		},
		{
			name: "maximum above the quantity limit",
			list: PledgeList{ID: 1, MaxItems: 100}, visitor: me,
			left: 100, maxQuantity: 20, canSignUp: true,
		},
		{
			name: "event not open",
			list: PledgeList{ID: 1, MaxItems: 5},
			rows: []PledgeRow{pledgeRow(1, 1, me, 2)}, visitor: me, closed: true,
			total: 2, left: 3, mine: true, myQuantity: 2, maxQuantity: 0, canSignUp: false,
		},
		{
			name: "rows of another list do not count",
			list: PledgeList{ID: 1},
			rows: []PledgeRow{pledgeRow(1, 2, me, 4)}, visitor: me,
			maxQuantity: 20, canSignUp: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := BuildPlanning(nil, nil, nil, nil, tc.visitor, !tc.closed)
			p.AddPledges([]PledgeList{tc.list}, tc.rows)
			v := p.PledgeListByID(tc.list.ID)
			if v == nil {
				t.Fatal("PledgeListByID returned nil")
			}
			if v.Total != tc.total || v.Missing != tc.missing || v.Left != tc.left || v.Full != tc.full {
				t.Errorf("numbers = total %d, missing %d, left %d, full %v; want %d, %d, %d, %v",
					v.Total, v.Missing, v.Left, v.Full, tc.total, tc.missing, tc.left, tc.full)
			}
			if v.Mine != tc.mine || v.MyQuantity != tc.myQuantity {
				t.Errorf("mine = %v with quantity %d; want %v with %d", v.Mine, v.MyQuantity, tc.mine, tc.myQuantity)
			}
			if v.MaxQuantity != tc.maxQuantity || v.CanSignUp != tc.canSignUp {
				t.Errorf("MaxQuantity = %d, CanSignUp = %v; want %d, %v",
					v.MaxQuantity, v.CanSignUp, tc.maxQuantity, tc.canSignUp)
			}
		})
	}
}

func TestPledgeListViewKeepsMyPledge(t *testing.T) {
	const me = 7
	p := BuildPlanning(nil, nil, nil, nil, me, true)
	p.AddPledges([]PledgeList{{ID: 1}}, []PledgeRow{pledgeRow(40, 1, 8, 1), pledgeRow(41, 1, me, 2), pledgeRow(42, 1, 9, 3)})
	v := p.PledgeListByID(1)
	if v.MyPledgeID != 41 {
		t.Errorf("MyPledgeID = %d, want 41", v.MyPledgeID)
	}
	if len(v.Pledges) != 3 || v.Pledges[0].ID != 40 || v.Pledges[2].ID != 42 {
		t.Errorf("Pledges = %+v, want the rows in their order", v.Pledges)
	}
}

func TestPlanningPledgeHelpers(t *testing.T) {
	const me, other = 7, 8
	lists := []PledgeList{
		{ID: 1, MinItems: 21},
		{ID: 2, MinItems: 3},
		{ID: 3},
	}
	rows := []PledgeRow{
		pledgeRow(1, 1, other, 14),
		pledgeRow(2, 2, me, 3),
	}

	p := BuildPlanning(nil, nil, nil, nil, me, true)
	p.AddPledges(lists[:2], rows)
	if total, minSum, allMin := p.PledgeTotals(); total != 17 || minSum != 24 || !allMin {
		t.Errorf("PledgeTotals() = %d, %d, %v; want 17, 24, true", total, minSum, allMin)
	}

	p = BuildPlanning(nil, nil, nil, nil, me, true)
	p.AddPledges(lists, rows)
	if total, minSum, allMin := p.PledgeTotals(); total != 17 || minSum != 24 || allMin {
		t.Errorf("PledgeTotals() with a list without minimum = %d, %d, %v; want 17, 24, false", total, minSum, allMin)
	}
	if len(p.Pledges) != 3 || p.Pledges[0].ID != 1 || p.Pledges[1].ID != 2 || p.Pledges[2].ID != 3 {
		t.Errorf("Pledges are not in list order: %+v", p.Pledges)
	}
	if len(p.MyPledges) != 1 || p.MyPledges[0].ID != 2 {
		t.Errorf("MyPledges = %+v, want only list 2", p.MyPledges)
	}
	if p.PledgeListByID(99) != nil {
		t.Error("PledgeListByID(99) found a list")
	}

	empty := BuildPlanning(nil, nil, nil, nil, me, true)
	if total, minSum, allMin := empty.PledgeTotals(); total != 0 || minSum != 0 || allMin {
		t.Errorf("PledgeTotals() without lists = %d, %d, %v; want 0, 0, false", total, minSum, allMin)
	}
}
