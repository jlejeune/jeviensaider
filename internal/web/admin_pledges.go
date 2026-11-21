package web

import (
	"fmt"
	"net/http"
	"strconv"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

// backToList redirects to the admin page, scrolled to a pledge list, with
// that list open. It works like backToStand.
func backToList(w http.ResponseWriter, r *http.Request, ev *model.Event, listID int64) {
	u := adminURL(ev)
	if listID != 0 {
		u += "?list=" + i64(listID) + "#list-" + i64(listID)
	}
	redirect(w, r, u)
}

// adminListError works like adminError, with a list anchor.
func (s *Server) adminListError(w http.ResponseWriter, r *http.Request, ev *model.Event, listID int64, err error) bool {
	if err == nil {
		return false
	}
	if msg, ok := userMessage(err); ok {
		setFlash(w, "error", msg)
		backToList(w, r, ev, listID)
		return true
	}
	return s.storeError(w, r, err)
}

func (s *Server) adminCreateList(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	l, err := readPledgeList(r)
	if err == nil {
		l.EventID = ev.ID
		err = s.store.CreatePledgeList(r.Context(), &l)
	}
	if s.adminListError(w, r, ev, 0, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("Liste « %s » ajoutée.", l.Name))
	backToList(w, r, ev, l.ID)
}

func (s *Server) adminUpdateList(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	l, err := readPledgeList(r)
	if err == nil {
		l.ID, l.EventID = id, ev.ID
		err = s.store.UpdatePledgeList(r.Context(), l)
	}
	if s.adminListError(w, r, ev, id, err) {
		return
	}
	setFlash(w, "success", "Liste mise à jour.")
	backToList(w, r, ev, id)
}

func (s *Server) adminDeleteList(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok || s.storeError(w, r, s.store.DeletePledgeList(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Liste supprimée (avec ses inscriptions).")
	redirect(w, r, adminURL(ev))
}

// adminAddPledge adds a pledge by hand (for someone who phoned in). It works
// whatever the status of the event. The maximum of the list still applies.
// It sends no e-mail.
func (s *Server) adminAddPledge(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	listID, err := strconv.ParseInt(r.FormValue("list"), 10, 64)
	if err != nil {
		s.storeError(w, r, store.ErrNotFound)
		return
	}
	form := readVolunteerForm(r)
	err = form.validate()
	var quantity int
	if err == nil {
		quantity, err = readPledgeQuantity(r)
	}
	if err == nil {
		_, err = s.store.Pledge(r.Context(), store.PledgeRequest{
			EventID: ev.ID, ListID: listID, Name: form.Name, Phone: form.Phone,
			Quantity: quantity, Admin: true,
		})
	}
	if s.adminListError(w, r, ev, listID, err) {
		return
	}
	setFlash(w, "success", form.Name+" a été inscrit·e.")
	backToList(w, r, ev, listID)
}

func (s *Server) adminDeletePledge(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	listID, err := s.store.AdminDeletePledge(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	setFlash(w, "info", "Inscription retirée.")
	backToList(w, r, ev, listID)
}

// pledgeTotalLine sums up all the lists for the organizer: "Au total : 17
// promis sur 21". It has no unit, because the lists have different ones. The
// "sur" part shows only when every list has a minimum. The line is empty when
// there are fewer than two lists.
func pledgeTotalLine(p *model.Planning) string {
	if len(p.Pledges) < 2 {
		return ""
	}
	total, minSum, allMin := p.PledgeTotals()
	line := "Au total : " + itoa(total) + " promis"
	if allMin {
		line += " sur " + itoa(minSum)
	}
	return line
}

// deleteListConfirm asks before the delete of a list, and says how many
// pledges go with it.
func deleteListConfirm(l *model.PledgeListView) string {
	msg := "Supprimer la liste « " + l.Name + " »"
	if n := len(l.Pledges); n > 0 {
		msg += " avec ses " + itoa(n) + " " + plural(n, "inscription", "inscriptions")
	}
	return msg + " ?"
}
