package web

import (
	"strings"
	"time"

	"jeviensaider/internal/model"
)

// unitCount writes a quantity with its unit, as in "14 gâteaux". It leaves
// out the unit for 0 or 1 item, because the unit is in the plural. It also
// leaves out the unit if the list has none.
func unitCount(n int, unit string) string {
	if unit == "" || n <= 1 {
		return itoa(n)
	}
	return itoa(n) + " " + unit
}

// pledgeStatus says in words how a list is filled, for example "14 gâteaux
// promis sur 21 : il en manque 7, encore 16 places". It has three parts: what
// people promised, the minimum, then the maximum. A full list says so and
// gives only what people promised.
func pledgeStatus(l *model.PledgeListView) string {
	base := "Rien de promis pour l'instant"
	if l.Total > 0 {
		base = unitCount(l.Total, l.Unit) + " promis"
	}
	if l.Full {
		return "Complet : " + base
	}
	status := base
	if l.MinItems > 0 {
		if l.Missing > 0 {
			if l.Total > 0 {
				status += " sur " + itoa(l.MinItems)
			}
			status += " : il en manque " + itoa(l.Missing)
		} else {
			status += " : c'est bon"
		}
	}
	if l.Left > 0 {
		status += ", encore " + itoa(l.Left) + " " + plural(l.Left, "place", "places")
	}
	return status
}

// pledgeWanted says whether a list still asks for items: it is not full and
// it has no minimum or it is under its minimum.
func pledgeWanted(l *model.PledgeListView) bool {
	return !l.Full && (l.MinItems == 0 || l.Missing > 0)
}

// listsOnly says whether an event has pledge lists and no slots. The public
// page then shows only the lists.
func listsOnly(p *model.Planning) bool {
	return len(p.Columns) == 0 && len(p.Pledges) > 0
}

// dropOffLabel says when to bring the items: "Dépôt à 8h30". It is empty
// when the list has no valid drop-off time.
func dropOffLabel(dropOff string) string {
	t, err := time.Parse("15:04", dropOff)
	if err != nil {
		return ""
	}
	return "Dépôt à " + hm(t)
}

// dropOffStart is the date and time of the drop-off, like the times of the
// slots. It returns false if the list has no drop-off time or the event has
// no date.
func dropOffStart(ev *model.Event, dropOff string) (time.Time, bool) {
	if dropOff == "" || ev.Date == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(model.TimeLayout, ev.Date+" "+dropOff)
	return t, err == nil
}

// pledgeNames lists who brings items as the public sees them: "Marie D. (2),
// Paul L.". The quantity is shown when it is above 1.
func pledgeNames(rows []model.PledgeRow) string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = model.DisplayName(r.Name)
		if r.Quantity > 1 {
			names[i] += " (" + itoa(r.Quantity) + ")"
		}
	}
	return strings.Join(names, ", ")
}
