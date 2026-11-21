package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/slotgen"
	"jeviensaider/internal/store"
)

func (s *Server) withAdmin(h func(http.ResponseWriter, *http.Request, *model.Event)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ev, err := s.store.EventByAdminToken(r.Context(), r.PathValue("token"))
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Ce lien d'administration n'est pas valide.")
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		h(w, r, ev)
	}
}

func adminURL(ev *model.Event) string { return "/a/" + ev.AdminToken }

// backToStand redirects to the admin page, scrolled to a stand, with that
// stand open.
func backToStand(w http.ResponseWriter, r *http.Request, ev *model.Event, standID int64) {
	u := adminURL(ev)
	if standID != 0 {
		id := strconv.FormatInt(standID, 10)
		u += "?stand=" + id + "#stand-" + id
	}
	redirect(w, r, u)
}

func adminFail(w http.ResponseWriter, r *http.Request, ev *model.Event, standID int64, msg string) {
	setFlash(w, "error", msg)
	backToStand(w, r, ev, standID)
}

// adminError reports err after an admin action: a message the organiser can
// act on (flash, back to the stand), or an internal error. It returns false
// when err is nil.
func (s *Server) adminError(w http.ResponseWriter, r *http.Request, ev *model.Event, standID int64, err error) bool {
	if err == nil {
		return false
	}
	if msg, ok := userMessage(err); ok {
		adminFail(w, r, ev, standID, msg)
		return true
	}
	return s.storeError(w, r, err)
}

type adminPage struct {
	Common
	Event      *model.Event
	P          *model.Planning
	PublicURL  string
	AdminURL   string
	AdminPath  string
	EventURL   string
	Volunteers int
	Gen        map[int64]genDefaults // slot generator, per stand
	NextColor  string
	// Moments are the titled parts of the day, in time order. MomentDate is
	// the day that the "add a moment" form starts with.
	Moments     []model.Moment
	MomentDate  string
	MomentColor string // the color that the "add a moment" form starts with
	Statuses    []string
	ExpiresOn   string // "YYYY-MM-DD", empty if events are kept forever
	// Retention is the days that apply to this event, which can differ from
	// the days stored with it (the default, or cut at the maximum).
	Retention      int
	RetentionField retentionField
	// NeedPassword: duplicating creates an event, so it asks for the
	// creation password too.
	NeedPassword bool
	MailEnabled  bool // e-mails really leave the server (SMTP is set up)
	// OpenStand is the stand whose card is open: the one that the organiser
	// just changed (?stand=<id>), else none.
	OpenStand int64
	// OpenList is the same for a pledge list (?list=<id>).
	OpenList int64
}

// openList is the pledge list whose card is open, from ?list=<id>.
func openList(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.URL.Query().Get("list"), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func openStand(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.URL.Query().Get("stand"), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, moments, err := s.planningWithMoments(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	nvol, err := s.store.CountVolunteers(r.Context(), ev.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var expires string
	days := s.cfg.Retention.Days(ev.RetentionDays)
	if days > 0 {
		last, err := s.store.EventLastDay(r.Context(), ev.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		expires = store.ExpiresOn(last, days).Format(model.DateLayout)
	}
	s.render(w, r, http.StatusOK, adminView(adminPage{
		Common:         s.common(w, r, "Admin · "+ev.Title),
		Event:          ev,
		P:              p,
		PublicURL:      s.absURL(r, eventURL(ev)),
		AdminURL:       s.absURL(r, adminURL(ev)),
		AdminPath:      adminURL(ev),
		EventURL:       eventURL(ev),
		Volunteers:     nvol,
		Gen:            generatorDefaults(p.Stands, ev.Date, s.now()),
		NextColor:      palette[len(p.Stands)%len(palette)],
		Moments:        moments,
		MomentDate:     momentDefaultDate(ev.Date, p, s.now()),
		MomentColor:    palette[len(moments)%len(palette)],
		Statuses:       model.Statuses,
		ExpiresOn:      expires,
		Retention:      days,
		RetentionField: s.retentionField(ev.RetentionDays),
		NeedPassword:   s.cfg.CreatePassword != "",
		OpenStand:      openStand(r),
		OpenList:       openList(r),
		MailEnabled:    s.cfg.MailEnabled,
	}))
}

// genDefaults pre-fills a stand's slot generator.
type genDefaults struct {
	Date string
	From string
	To   string
}

// generatorDefaults suggests, for each stand, the next slots to generate.
// A stand without slots starts at 14:00–17:00 on the event's date (or today).
// Otherwise the suggestion continues where the stand's last slot ends, for
// the same duration: organisers usually add slots one block after another.
func generatorDefaults(stands []*model.StandView, eventDate string, today time.Time) map[int64]genDefaults {
	date := eventDate
	if date == "" {
		date = today.Format(model.DateLayout)
	}
	gen := map[int64]genDefaults{}
	for _, st := range stands {
		d := genDefaults{Date: date, From: "14:00", To: "17:00"}
		if n := len(st.Slots); n > 0 {
			last := st.Slots[n-1]
			d.Date = last.EndsAt.Format(model.DateLayout)
			d.From = last.EndsAt.Format("15:04")
			d.To = last.EndsAt.Add(last.EndsAt.Sub(last.StartsAt)).Format("15:04")
			if d.To <= d.From { // would cross midnight
				d.To = "23:59"
			}
		}
		gen[st.ID] = d
	}
	return gen
}

func (s *Server) adminUpdateEvent(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	form, err := s.readEventForm(r)
	if s.adminError(w, r, ev, 0, err) {
		return
	}
	ev.Title, ev.Date, ev.Location, ev.Description = form.Title, form.Date, form.Location, form.Description
	ev.AdminEmail, ev.NotifySignups = form.AdminEmail, form.NotifySignups
	// The field holds the event's own days, empty for the server default:
	// save what it says. A form without the field (deletion off, or a page
	// from before the field existed) keeps the stored days.
	if s.cfg.Retention.Enabled() && r.Form.Has("retention_days") {
		ev.RetentionDays = form.RetentionDays
	}
	if s.storeError(w, r, s.store.UpdateEvent(r.Context(), ev)) {
		return
	}
	setFlash(w, "success", "Événement mis à jour.")
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminSetStatus(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	st := r.FormValue("status")
	if !model.ValidStatus(st) {
		adminFail(w, r, ev, 0, "Statut inconnu.")
		return
	}
	ev.Status = st
	if s.storeError(w, r, s.store.UpdateEvent(r.Context(), ev)) {
		return
	}
	setFlash(w, "success", "Inscriptions : "+strings.ToLower(eventStatusLabel(st))+".")
	redirect(w, r, adminURL(ev))
}

func readStand(r *http.Request, ev *model.Event) (*model.Stand, error) {
	st := &model.Stand{
		EventID:     ev.ID,
		Name:        clean(r.FormValue("name"), maxStandName, false),
		Description: clean(r.FormValue("description"), maxStandDescription, true),
		Color:       readColor(r),
	}
	// Like the fill of a moment, the fill of a row needs a color.
	st.FillRow = r.FormValue("fill_row") == "1" && st.Color != ""
	if st.Name == "" {
		return nil, userError("Le nom du stand est obligatoire.")
	}
	return st, nil
}

func (s *Server) adminCreateStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	st, err := readStand(r, ev)
	if err == nil {
		err = s.store.CreateStand(r.Context(), st)
	}
	if s.adminError(w, r, ev, 0, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("Stand « %s » ajouté. Ajoutez maintenant ses créneaux.", st.Name))
	backToStand(w, r, ev, st.ID)
}

func (s *Server) adminUpdateStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	st, err := readStand(r, ev)
	if err == nil {
		st.ID = id
		err = s.store.UpdateStand(r.Context(), st)
	}
	if s.adminError(w, r, ev, id, err) {
		return
	}
	setFlash(w, "success", "Stand mis à jour.")
	backToStand(w, r, ev, id)
}

func (s *Server) adminDeleteStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok || s.storeError(w, r, s.store.DeleteStand(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Stand supprimé (avec ses créneaux et inscriptions).")
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminMoveStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	if s.storeError(w, r, s.store.MoveStand(r.Context(), ev.ID, id, delta)) {
		return
	}
	backToStand(w, r, ev, id)
}

func (s *Server) adminGenerateSlots(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	standID, ok := s.pathID(w, r)
	if !ok {
		return
	}
	var slots []model.Slot
	form, err := s.readSlotForm(r, r.FormValue("date"))
	if err == nil {
		var step int
		if step, err = formInt(r, "step", 0); err != nil {
			err = userError("Durée : " + err.Error())
		}
		if err == nil {
			slots, err = slotgen.Generate(standID, form.StartsAt, form.EndsAt, time.Duration(step)*time.Minute, form.MinPeople, form.MaxPeople)
		}
		if err == nil {
			err = s.store.CreateSlots(r.Context(), ev.ID, standID, slots)
		}
	}
	if s.adminError(w, r, ev, standID, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("%d créneau(x) ajouté(s).", len(slots)))
	backToStand(w, r, ev, standID)
}

func (s *Server) adminUpdateSlot(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	sl, err := s.store.GetSlot(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	date := r.FormValue("date")
	if date == "" {
		date = sl.StartsAt.Format(model.DateLayout)
	}
	form, err := s.readSlotForm(r, date)
	if err == nil {
		sl.StartsAt, sl.EndsAt, sl.MinPeople, sl.MaxPeople = form.StartsAt, form.EndsAt, form.MinPeople, form.MaxPeople
		err = sl.Check()
	}
	if err == nil {
		err = s.store.UpdateSlot(r.Context(), ev.ID, sl)
	}
	if s.adminError(w, r, ev, sl.StandID, err) {
		return
	}
	setFlash(w, "success", "Créneau mis à jour.")
	backToStand(w, r, ev, sl.StandID)
}

// momentDefaultDate is the day that the form to add a moment starts with: the
// event's date, else the day of the first slot, else today. A column that
// exists only for a moment does not count, because the organiser has not
// chosen that day for the slots.
func momentDefaultDate(eventDate string, p *model.Planning, today time.Time) string {
	if eventDate != "" {
		return eventDate
	}
	for _, c := range p.Columns {
		if !c.MomentOnly {
			return c.StartsAt.Format(model.DateLayout)
		}
	}
	return today.Format(model.DateLayout)
}

// backToMoments redirects to the admin page, scrolled to the moments.
func backToMoments(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	redirect(w, r, adminURL(ev)+"#moments")
}

// momentError reports err after an action on a moment: a message that the
// organiser can act on (flash, back to the moments), or an internal error. It
// returns false when err is nil.
func (s *Server) momentError(w http.ResponseWriter, r *http.Request, ev *model.Event, err error) bool {
	if err == nil {
		return false
	}
	if msg, ok := userMessage(err); ok {
		setFlash(w, "error", msg)
		backToMoments(w, r, ev)
		return true
	}
	return s.storeError(w, r, err)
}

func (s *Server) adminCreateMoment(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	m, err := s.readMomentForm(r, r.FormValue("date"))
	if err == nil {
		m.EventID = ev.ID
		if err = m.Check(); err == nil {
			err = s.store.CreateMoment(r.Context(), &m)
		}
	}
	if s.momentError(w, r, ev, err) {
		return
	}
	setFlash(w, "success", "Moment ajouté.")
	backToMoments(w, r, ev)
}

func (s *Server) adminUpdateMoment(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMoment(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	date := r.FormValue("date")
	if date == "" {
		date = m.StartsAt.Format(model.DateLayout)
	}
	form, err := s.readMomentForm(r, date)
	if err == nil {
		m.Title, m.Color, m.Fill, m.StartsAt, m.EndsAt = form.Title, form.Color, form.Fill, form.StartsAt, form.EndsAt
		if err = m.Check(); err == nil {
			err = s.store.UpdateMoment(r.Context(), m)
		}
	}
	if s.momentError(w, r, ev, err) {
		return
	}
	setFlash(w, "success", "Moment mis à jour.")
	backToMoments(w, r, ev)
}

func (s *Server) adminDeleteMoment(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if s.storeError(w, r, s.store.DeleteMoment(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Moment supprimé.")
	backToMoments(w, r, ev)
}

func (s *Server) adminDeleteSlot(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	sl, err := s.store.GetSlot(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) || s.storeError(w, r, s.store.DeleteSlot(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Créneau supprimé.")
	backToStand(w, r, ev, sl.StandID)
}

// adminAddSignup registers someone by hand (who phoned in). It is
// allowed whatever the event's status; capacity still applies.
func (s *Server) adminAddSignup(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	slotID, err := strconv.ParseInt(r.FormValue("slot"), 10, 64)
	if err != nil {
		adminFail(w, r, ev, 0, "Choisissez un créneau.")
		return
	}
	form := readVolunteerForm(r)
	err = form.validate()
	var res *store.SignupResult
	if err == nil {
		res, err = s.store.SignUp(r.Context(), store.SignupRequest{
			EventID: ev.ID, SlotID: slotID, Name: form.Name, Phone: form.Phone,
			Comment: form.Comment, Admin: true,
		})
	}
	if errors.Is(err, store.ErrNotFound) {
		err = userError("Choisissez un créneau.")
	}
	if s.adminError(w, r, ev, 0, err) {
		return
	}
	setFlash(w, "success", form.Name+" a été inscrit·e.")
	backToStand(w, r, ev, res.Slot.StandID)
}

func (s *Server) adminDeleteSignup(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := s.pathID(w, r)
	if !ok || s.storeError(w, r, s.store.AdminDeleteSignup(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Inscription retirée.")
	backToStand(w, r, ev, 0)
}

func (s *Server) adminPurge(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	if r.FormValue("confirm") != "yes" {
		adminFail(w, r, ev, 0, "Cochez la case de confirmation pour effacer les données.")
		return
	}
	n, err := s.store.PurgeVolunteers(r.Context(), ev.ID)
	if s.storeError(w, r, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("Données personnelles effacées (%d bénévole(s)).", n))
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminDuplicate(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	// An admin link is shared among organisers: on its own, it must not
	// allow creating events (and sending creation e-mails).
	if !s.createPasswordOK(r) {
		adminFail(w, r, ev, 0, "Mot de passe de création incorrect.")
		return
	}
	title, date, err := s.readTitleAndDate(r)
	var dup *model.Event
	if err == nil {
		dup, err = s.store.DuplicateEvent(r.Context(), ev, title, date)
	}
	if s.adminError(w, r, ev, 0, err) {
		return
	}
	msg := "Nouvel événement créé à partir de « " + ev.Title + " ». Voici son lien d'administration : conservez-le !"
	if s.sendLinks(r, dup, false) && s.cfg.MailEnabled {
		msg += " Il a aussi été envoyé à " + dup.AdminEmail + "."
	}
	setFlash(w, "success", msg)
	redirect(w, r, adminURL(dup))
}

// adminResendLinks e-mails the admin and public links to the organiser
// again: the first mail got lost, or the organiser's address changed.
func (s *Server) adminResendLinks(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	switch {
	case !s.cfg.MailEnabled:
		adminFail(w, r, ev, 0, "L'envoi d'e-mails n'est pas configuré sur ce serveur.")
	case ev.AdminEmail == "":
		adminFail(w, r, ev, 0, "Indiquez d'abord l'e-mail de l'organisateur, dans « Modifier l'événement ».")
	case !s.sendLinks(r, ev, true):
		adminFail(w, r, ev, 0, "Trop d'envois récents vers "+ev.AdminEmail+" : réessayez plus tard.")
	default:
		setFlash(w, "success", "Les liens de l'événement ont été envoyés à "+ev.AdminEmail+".")
		redirect(w, r, adminURL(ev))
	}
}

func (s *Server) adminDeleteEvent(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	if r.FormValue("confirm") != "yes" {
		adminFail(w, r, ev, 0, "Cochez la case de confirmation pour supprimer l'événement.")
		return
	}
	if s.storeError(w, r, s.store.DeleteEvent(r.Context(), ev.ID)) {
		return
	}
	setFlash(w, "info", "L'événement « "+ev.Title+" » a été supprimé.")
	redirect(w, r, "/")
}

// ---- planning PDF ----

// adminPDF sends the planning as a PDF to print: one page per stand, then one
// page per pledge list.
func (s *Server) adminPDF(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, err := s.planning(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := writePlanningPDF(&buf, ev, p, s.now()); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="planning-%s.pdf"`, store.Slugify(ev.Title)))
	buf.WriteTo(w)
}
