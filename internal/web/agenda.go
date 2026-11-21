package web

import (
	"fmt"
	"net/http"
	"time"

	"jeviensaider/internal/ical"
	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

// reminders are the alarms added to each calendar entry: the day before, to
// plan the day, and one hour before, to leave on time.
var reminders = []time.Duration{24 * time.Hour, time.Hour}

// meCalendar sends the slots of one volunteer as an iCalendar file. It also
// holds the drop-off time of each pledge that has one. It gives a volunteer a
// reminder without storing an e-mail address.
//
// The file holds only what the volunteer already sees on their page, and not
// their personal link: people can share a calendar file, and the link lets
// anyone edit the sign-ups.
func (s *Server) meCalendar(w http.ResponseWriter, r *http.Request, ev *model.Event, vol *model.Volunteer) {
	// A draft is not public: the page and the sign-up form show nothing of
	// it, so this file must not either. A closed event keeps its sign-ups.
	if ev.IsDraft() {
		s.renderError(w, r, http.StatusNotFound, msgNotOpenYet)
		return
	}
	p, err := s.planning(r.Context(), ev, vol.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(p.MySlots) == 0 && !hasPledgeEntries(ev, p) {
		s.renderError(w, r, http.StatusNotFound, "Vous n'avez aucun créneau à ajouter à l'agenda.")
		return
	}
	eventLink := s.absURL(r, eventURL(ev))
	events := make([]ical.Event, 0, len(p.MySlots))
	for _, sl := range p.MySlots {
		events = append(events, ical.Event{
			// The same sign-up always gets the same UID, so that a second
			// import updates the entry instead of adding a copy.
			UID:         fmt.Sprintf("%s-%d-%d@jeviensaider", ev.Slug, sl.ID, vol.ID),
			Summary:     fmt.Sprintf("%s (%s)", sl.Stand.Name, ev.Title),
			Description: sl.Stand.Description,
			Location:    ev.Location,
			URL:         eventLink,
			Start:       sl.StartsAt,
			End:         sl.EndsAt,
			Alarms:      reminders,
		})
	}
	events = append(events, pledgeEntries(ev, p, vol.ID, eventLink)...)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="mes-creneaux-%s.ics"`, store.Slugify(ev.Title)))
	w.Write(ical.Build("-//jeviensaider//jeviensaider//FR", s.now(), events))
}
