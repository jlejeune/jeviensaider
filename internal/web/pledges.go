package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"jeviensaider/internal/ical"
	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

// pledgeInput is what the visitor typed in the pledge form.
type pledgeInput struct {
	Name, Phone string
	Quantity    int
}

// pledgePage is the pledge form of one list, as a dialog or as a page.
type pledgePage struct {
	Common
	Event    *model.Event
	EventURL string
	List     *model.PledgeListView
	Me       *model.Volunteer
	Form     pledgeInput
	Error    string
}

// pledgeURL is the address of the pledge form of a list.
func pledgeURL(eventURL string, l *model.PledgeListView) templ.SafeURL {
	return templ.SafeURL(eventURL + "/lists/" + i64(l.ID) + "/signup")
}

// loadPledgeList returns a list of the event, as the volunteer myID sees it. It
// returns store.ErrNotFound if the event has no such list.
func (s *Server) loadPledgeList(ctx context.Context, ev *model.Event, listID, myID int64) (*model.PledgeListView, error) {
	p, err := s.planning(ctx, ev, myID)
	if err != nil {
		return nil, err
	}
	l := p.PledgeListByID(listID)
	if l == nil {
		return nil, store.ErrNotFound
	}
	return l, nil
}

func (s *Server) pledgeForm(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.publicID(w, r, ev)
	if !ok {
		return
	}
	me := s.cookieVolunteer(r, ev)
	l, err := s.loadPledgeList(r.Context(), ev, id, volunteerID(me))
	if s.storeError(w, r, err) {
		return
	}
	data := pledgePage{
		Common: s.common(w, r, l.Name), Event: ev, EventURL: eventURL(ev), List: l, Me: me,
		Form: pledgeInput{Quantity: 1},
	}
	if me != nil {
		data.Form.Name, data.Form.Phone = me.Name, me.Phone
	}
	if l.Mine {
		data.Form.Quantity = l.MyQuantity
	}
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, pledgeSignupDialog(data))
		return
	}
	s.render(w, r, http.StatusOK, pledgeSignupView(data))
}

func (s *Server) pledgeSubmit(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.publicID(w, r, ev)
	if !ok {
		return
	}
	// Honeypot: bots fill every field, humans never see this one.
	if r.FormValue("website") != "" {
		redirect(w, r, eventURL(ev))
		return
	}
	me := s.cookieVolunteer(r, ev)
	vf := readVolunteerForm(r)
	quantity, qerr := readPledgeQuantity(r)
	form := pledgeInput{Name: vf.Name, Phone: vf.Phone, Quantity: quantity}
	if err := vf.validate(); err != nil {
		s.pledgeFailed(w, r, ev, id, me, form, http.StatusBadRequest, err.Error())
		return
	}
	if qerr != nil {
		s.pledgeFailed(w, r, ev, id, me, form, http.StatusBadRequest, qerr.Error())
		return
	}
	req := store.PledgeRequest{EventID: ev.ID, ListID: id, Name: vf.Name, Phone: vf.Phone, Quantity: quantity}
	if me != nil {
		req.EditToken = me.EditToken
	}
	res, err := s.store.Pledge(r.Context(), req)
	if msg, ok := userMessage(err); ok { // full list, event closed
		s.pledgeFailed(w, r, ev, id, me, form, http.StatusConflict, msg)
		return
	}
	if s.storeError(w, r, err) {
		return
	}

	setVolunteerCookie(w, ev, res.Volunteer)
	if res.Previous != res.Quantity { // saving the same quantity changes nothing
		s.notifyPledge(r.Context(), r, ev, res.Volunteer, id, res.Quantity, false)
	}
	if res.NewVolunteer {
		redirect(w, r, meURL(ev, res.Volunteer)+"?welcome=1")
		return
	}
	setFlash(w, "success", fmt.Sprintf("C'est noté ! Vous apportez %d pour « %s ». Merci !", res.Quantity, res.List.Name))
	redirect(w, r, eventURL(ev))
}

// pledgeFailed shows the pledge form again with the input of the visitor and
// an error. It is always a full page, because the browser posts the form
// without htmx.
func (s *Server) pledgeFailed(w http.ResponseWriter, r *http.Request, ev *model.Event, listID int64, me *model.Volunteer, form pledgeInput, status int, msg string) {
	l, err := s.loadPledgeList(r.Context(), ev, listID, volunteerID(me))
	if s.storeError(w, r, err) {
		return
	}
	s.render(w, r, status, pledgeSignupView(pledgePage{
		Common: Common{Title: l.Name}, Event: ev, EventURL: eventURL(ev),
		List: l, Me: me, Form: form, Error: msg,
	}))
}

// mePledgeCancel removes a pledge of the volunteer. A pledge of another
// volunteer is not found, so it gives the 404 page.
func (s *Server) mePledgeCancel(w http.ResponseWriter, r *http.Request, ev *model.Event, vol *model.Volunteer) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if !ev.IsOpen() {
		setFlash(w, "error", msgClosed)
		redirect(w, r, meURL(ev, vol))
		return
	}
	listID, err := s.store.CancelPledge(r.Context(), vol.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	s.notifyPledge(r.Context(), r, ev, vol, listID, 0, true)
	setFlash(w, "info", "Votre inscription a été annulée.")
	redirect(w, r, meURL(ev, vol))
}

// pledgeEntryLength is how long a calendar entry for a drop-off lasts. A
// drop-off has no end time, so the entry has a short fixed length.
const pledgeEntryLength = 30 * time.Minute

// pledgeEntries returns one calendar entry for each list that the volunteer
// brings items to. A list has an entry only if it has a drop-off time and the
// event has a date. p must be the full planning, because Planning.Mine drops
// the pledges.
func pledgeEntries(ev *model.Event, p *model.Planning, volID int64, eventLink string) []ical.Event {
	var entries []ical.Event
	for _, l := range p.MyPledges {
		start, ok := dropOffStart(ev, l.DropOff)
		if !ok {
			continue
		}
		entries = append(entries, ical.Event{
			// The same list and volunteer always give the same UID, so that a
			// second import updates the entry instead of adding a copy.
			UID:         fmt.Sprintf("%s-p%d-%d@jeviensaider", ev.Slug, l.ID, volID),
			Summary:     fmt.Sprintf("À apporter : %s (%s)", l.Name, ev.Title),
			Description: l.Description,
			Location:    ev.Location,
			URL:         eventLink,
			Start:       start,
			End:         start.Add(pledgeEntryLength),
			Alarms:      reminders,
		})
	}
	return entries
}

// hasPledgeEntries says whether the calendar file of the volunteer has an
// entry for a pledge. The personal page uses it to show the calendar button.
func hasPledgeEntries(ev *model.Event, p *model.Planning) bool {
	return len(pledgeEntries(ev, p, 0, "")) > 0
}
