package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"jeviensaider/internal/model"
	"jeviensaider/internal/slotgen"
	"jeviensaider/internal/store"
)

// defaultColor is what the color picker shows for a stand that has no color.
const defaultColor = "#3d7cc9"

// neutralMomentColor is what the color picker shows for a moment that has no
// color: the light tint of a column with no slot.
const neutralMomentColor = "#fbf0c4"

// palette gives each new stand a distinct default colour.
var palette = []string{"#3d7cc9", "#d9534f", "#2e9e6a", "#e8932c", "#8a5cc7", "#c94f8f", "#2aa3b5", "#7a8b2e"}

func validColor(s string) bool { return model.ValidColor(s) }

// Maximum lengths of text fields, in characters. The templates repeat them
// as maxlength attributes, so that browsers stop typing at the same point.
const (
	maxTitle            = 120 // event title and location
	maxDescription      = 2000
	maxEmail            = 120
	maxStandName        = 80
	maxStandDescription = 1000
	maxPledgeUnit       = 30 // the unit of a pledge list, such as "gâteaux"
	maxName             = 80 // volunteer
	maxPhone            = 30
	maxComment          = 300
)

// clean trims, strips control characters (newlines kept if multiline) and
// caps the length in runes.
func clean(s string, max int, multiline bool) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == '\r' {
			continue
		}
		if unicode.IsControl(r) && !(multiline && r == '\n') {
			r = ' '
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// pathID reads the {id} of the URL. An invalid one renders "not found",
// like an ID that does not exist, and returns false.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.storeError(w, r, store.ErrNotFound)
		return 0, false
	}
	return id, true
}

// formInt parses an optional integer field; empty means def.
func formInt(r *http.Request, key string, def int) (int, error) {
	return formIntIn(r, key, def, 0, 1000)
}

// formIntIn reads an integer field from lo to hi; def when it is empty.
func formIntIn(r *http.Request, key string, def, lo, hi int) (int, error) {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, userError("nombre invalide : " + v)
	}
	return n, nil
}

// retentionField is the "days before deletion" input of the event forms.
// An empty field means the server default.
type retentionField struct {
	Show    bool // false when events are kept forever: there is nothing to choose
	Value   int  // the event's own days, 0: none (the field is empty)
	Default int  // the server default, shown as the placeholder
	Max     int  // the longest period that the server allows
}

// retentionField fills the input for an event that stores eventDays (0:
// none). Days above the maximum show as the maximum, the days that apply.
func (s *Server) retentionField(eventDays int) retentionField {
	r := s.cfg.Retention
	return retentionField{Show: r.Enabled(), Value: min(eventDays, r.Max()), Default: r.DefaultDays, Max: r.Max()}
}

// readRetention reads how many days after the event the server deletes it:
// 0 when the field is empty (the server default) or when events are kept
// forever, else a number from 1 to the maximum.
func (s *Server) readRetention(r *http.Request) (int, error) {
	if !s.cfg.Retention.Enabled() {
		return 0, nil
	}
	max := s.cfg.Retention.Max()
	n, err := formIntIn(r, "retention_days", 0, 1, max)
	if err != nil {
		return 0, userError(fmt.Sprintf("Durée de conservation invalide : laissez le champ vide pour la durée par défaut, ou indiquez un nombre de jours entre 1 et %d.", max))
	}
	return n, nil
}

// checkDateRange accepts dates up to two years ahead: a far-future date (a
// typo, or a way around the automatic cleanup) would keep an event and its
// personal data forever.
//
// Past dates are accepted back to the longest retention period that an
// organiser can choose (at least one year), so organisers can still edit
// their events. When events are kept forever, any past date is fine.
func (s *Server) checkDateRange(t time.Time) error {
	today := s.now()
	max := time.Date(today.Year()+2, today.Month(), today.Day(), 23, 59, 59, 0, time.UTC)
	if !s.cfg.Retention.Enabled() {
		if t.After(max) {
			return userError(fmt.Sprintf("date hors limites : choisissez une date avant le %s", dateFR(max.Format(model.DateLayout))))
		}
		return nil
	}
	back := s.cfg.Retention.Max()
	if back < 365 {
		back = 365
	}
	min := time.Date(today.Year(), today.Month(), today.Day()-back, 0, 0, 0, 0, time.UTC)
	if t.Before(min) || t.After(max) {
		return userError(fmt.Sprintf("date hors limites : choisissez une date entre le %s et le %s", dateFR(min.Format(model.DateLayout)), dateFR(max.Format(model.DateLayout))))
	}
	return nil
}

func (s *Server) parseDate(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	t, err := time.Parse(model.DateLayout, v)
	if err != nil {
		return "", userError("date invalide (format attendu : AAAA-MM-JJ)")
	}
	if err := s.checkDateRange(t); err != nil {
		return "", err
	}
	return v, nil
}

// parseDateTime combines a YYYY-MM-DD date and an HH:MM time.
func (s *Server) parseDateTime(date, hhmm string) (time.Time, error) {
	t, err := time.Parse(model.TimeLayout, strings.TrimSpace(date)+" "+strings.TrimSpace(hhmm))
	if err != nil {
		return time.Time{}, userError("date ou heure invalide")
	}
	if err := s.checkDateRange(t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// userError is a message for the visitor, shown as is: a full French
// sentence, capitalised and punctuated (unlike ordinary Go errors).
type userError string

func (e userError) Error() string { return string(e) }

// userMessage returns the French sentence to show for err, if err is one a
// user can cause and fix (a full slot, a bad time range…). Other errors are
// internal: the caller reports them with serverError.
func userMessage(err error) (string, bool) {
	var ue userError
	var full *store.PledgeFullError
	var tooLow *store.PledgeMaxError
	switch {
	case errors.As(err, &ue):
		return string(ue), true
	case errors.As(err, &full):
		if full.Free == 0 {
			return "Cette liste est complète.", true
		}
		return fmt.Sprintf("Vous ne pouvez en apporter que %d sur cette liste.", full.Free), true
	case errors.As(err, &tooLow):
		return fmt.Sprintf("Il y a déjà %d promis : le maximum doit être au moins %d.", tooLow.Total, tooLow.Total), true
	case errors.Is(err, store.ErrSlotFull):
		return "Ce créneau est déjà complet.", true
	case errors.Is(err, store.ErrAlreadySigned):
		return "Vous êtes déjà inscrit·e sur ce créneau.", true
	case errors.Is(err, store.ErrEventNotOpen):
		return "Les inscriptions ne sont pas ouvertes pour cet événement.", true
	case errors.Is(err, store.ErrTooManyStands):
		return fmt.Sprintf("Un événement ne peut pas avoir plus de %d stands.", store.MaxStands), true
	case errors.Is(err, store.ErrTooManySlots):
		return fmt.Sprintf("Un événement ne peut pas avoir plus de %d créneaux.", store.MaxSlots), true
	case errors.Is(err, store.ErrTooManyMoments):
		return fmt.Sprintf("Un événement ne peut pas avoir plus de %d moments.", store.MaxMoments), true
	case errors.Is(err, store.ErrMomentExists):
		return "Un moment existe déjà à cette heure.", true
	case errors.Is(err, model.ErrMomentRange):
		return "L'heure de fin doit être après l'heure de début.", true
	case errors.Is(err, model.ErrMomentTitle):
		return fmt.Sprintf("Le titre du moment est obligatoire (%d caractères au maximum).", model.MaxMomentTitle), true
	case errors.Is(err, model.ErrSlotRange):
		return "L'heure de fin doit être après l'heure de début.", true
	case errors.Is(err, model.ErrSlotMin):
		return "Le minimum doit être au moins 1.", true
	case errors.Is(err, model.ErrSlotMax), errors.Is(err, model.ErrPledgeRange):
		return "Le maximum doit être vide ou supérieur ou égal au minimum.", true
	case errors.Is(err, model.ErrPledgeNumber):
		return fmt.Sprintf("Indiquez un nombre entre 1 et %d.", model.MaxPledgeItems), true
	case errors.Is(err, model.ErrPledgeDropOff):
		return msgPledgeDropOff, true
	case errors.Is(err, store.ErrPledgeQuantity):
		return msgPledgeQuantity, true
	case errors.Is(err, store.ErrTooManyPledgeLists):
		return fmt.Sprintf("Un événement ne peut pas avoir plus de %d listes à apporter.", store.MaxPledgeLists), true
	case errors.Is(err, store.ErrTooManyPledges):
		return fmt.Sprintf("Cette liste ne peut pas avoir plus de %d inscriptions.", store.MaxPledgesPerList), true
	case errors.Is(err, slotgen.ErrTooMany):
		return fmt.Sprintf("Trop de créneaux d'un coup (maximum %d) : vérifiez la durée.", slotgen.MaxSlots), true
	}
	return "", false
}

// readTitleAndDate reads an event's name (required) and date (optional).
func (s *Server) readTitleAndDate(r *http.Request) (title, date string, err error) {
	title = clean(r.FormValue("title"), maxTitle, false)
	date, err = s.parseDate(r.FormValue("date"))
	if err == nil && title == "" {
		err = userError("Le nom de l'événement est obligatoire.")
	}
	return title, date, err
}

// readEventForm reads the event settings form (creation and edition). The
// event is filled even when an error is returned, to show the form again.
func (s *Server) readEventForm(r *http.Request) (model.Event, error) {
	ev := model.Event{
		Location:      clean(r.FormValue("location"), maxTitle, false),
		Description:   clean(r.FormValue("description"), maxDescription, true),
		AdminEmail:    clean(r.FormValue("admin_email"), maxEmail, false),
		NotifySignups: r.FormValue("notify") == "1",
	}
	// Read first: a valid number is kept to show the form again, even when
	// another field is wrong.
	var retentionErr error
	ev.RetentionDays, retentionErr = s.readRetention(r)
	var err error
	ev.Title, ev.Date, err = s.readTitleAndDate(r)
	if err != nil {
		return ev, err
	}
	if err := checkAdminEmail(ev.AdminEmail); err != nil {
		return ev, err
	}
	return ev, retentionErr
}

// readSlotForm reads a slot's times on date and its capacities. The times
// and capacities themselves are checked by model.Slot.Check.
func (s *Server) readSlotForm(r *http.Request, date string) (model.Slot, error) {
	var sl model.Slot
	var err error
	if sl.StartsAt, err = s.parseDateTime(date, r.FormValue("from")); err != nil {
		return sl, userError("Début : " + err.Error())
	}
	if sl.EndsAt, err = s.parseDateTime(date, r.FormValue("to")); err != nil {
		return sl, userError("Fin : " + err.Error())
	}
	if sl.MinPeople, err = formInt(r, "min", 1); err != nil {
		return sl, userError("Minimum : " + err.Error())
	}
	if sl.MaxPeople, err = formInt(r, "max", 0); err != nil {
		return sl, userError("Maximum : " + err.Error())
	}
	return sl, nil
}

// Messages that both the pledge form readers and userMessage use.
const (
	msgPledgeDropOff  = "Heure de dépôt invalide (exemple : 08:30)."
	msgPledgeQuantity = "Indiquez un nombre entre 1 et 20."
)

// readPledgeList reads the form of a pledge list. The name is required. The
// description, the unit, the drop-off time, the minimum and the maximum are
// optional. An empty minimum or maximum means 0, which is unset. The list is
// filled even when an error is returned, to show the form again.
func readPledgeList(r *http.Request) (model.PledgeList, error) {
	l := model.PledgeList{
		Name:        clean(r.FormValue("name"), maxStandName, false),
		Description: clean(r.FormValue("description"), maxStandDescription, true),
		Unit:        clean(r.FormValue("unit"), maxPledgeUnit, false),
		// The time is not cut: a cut time such as "08:30xyz" would look valid.
		DropOff: strings.TrimSpace(r.FormValue("drop_off")),
	}
	if l.Name == "" {
		return l, userError("Le nom de la liste est obligatoire.")
	}
	if !model.ValidDropOff(l.DropOff) {
		return l, userError(msgPledgeDropOff)
	}
	var err error
	if l.MinItems, err = formIntIn(r, "min", 0, 1, model.MaxPledgeItems); err != nil {
		return l, userError("Minimum : " + err.Error())
	}
	if l.MaxItems, err = formIntIn(r, "max", 0, 1, model.MaxPledgeItems); err != nil {
		return l, userError("Maximum : " + err.Error())
	}
	return l, l.Check()
}

// readPledgeQuantity reads how many items a volunteer brings: 1 when the
// field is empty, else a number from 1 to model.MaxPledgeQuantity.
func readPledgeQuantity(r *http.Request) (int, error) {
	n, err := formIntIn(r, "quantity", 1, 1, model.MaxPledgeQuantity)
	if err != nil {
		return 0, userError(msgPledgeQuantity)
	}
	return n, nil
}

// volunteerForm is what a volunteer types in the sign-up form.
type volunteerForm struct {
	Name    string
	Phone   string
	Comment string
}

// readMomentForm reads the times, the title and the color of a moment. The times belong
// to the day date (YYYY-MM-DD). The title is read one character past
// model.MaxMomentTitle, so that Moment.Check can say that it is too long.
func (s *Server) readMomentForm(r *http.Request, date string) (model.Moment, error) {
	var m model.Moment
	var err error
	if m.StartsAt, err = s.parseDateTime(date, r.FormValue("from")); err != nil {
		return m, userError("Début : " + err.Error())
	}
	if m.EndsAt, err = s.parseDateTime(date, r.FormValue("to")); err != nil {
		return m, userError("Fin : " + err.Error())
	}
	m.Title = clean(r.FormValue("title"), model.MaxMomentTitle+1, false)
	m.Color = readColor(r)
	// The fill needs a color.
	m.Fill = r.FormValue("fill") == "1" && m.Color != ""
	return m, nil
}

// readColor reads the optional color of a stand or a moment. Anything that
// is not a "#rrggbb" color, or the "Sans couleur" box, gives an empty color.
// The box does not count if the organizer changed the picker (color_was is
// the color that the picker showed), so a color that was picked is kept. A
// form without color_was keeps the box in charge.
func readColor(r *http.Request) string {
	c := r.FormValue("color")
	was := r.FormValue("color_was")
	picked := was != "" && c != was
	if (picked || r.FormValue("nocolor") != "1") && validColor(c) {
		return c
	}
	return ""
}

// readVolunteerForm reads a sign-up: a name, an optional phone and comment.
func readVolunteerForm(r *http.Request) volunteerForm {
	return volunteerForm{
		Name:    clean(r.FormValue("name"), maxName, false),
		Phone:   clean(r.FormValue("phone"), maxPhone, false),
		Comment: clean(r.FormValue("comment"), maxComment, false),
	}
}

var phoneRe = regexp.MustCompile(`^[0-9+ .()-]{6,30}$`)

// emailRe is deliberately strict: a plain address, without the characters
// (?, &, =, #, %, /, quotes, spaces…) that could add headers such as a BCC
// to the organiser's mailto: link.
var emailRe = regexp.MustCompile(`^[A-Za-z0-9._+'-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$`)

func validEmail(s string) bool {
	if !emailRe.MatchString(s) {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// checkAdminEmail validates the organiser's e-mail, which is mandatory.
func checkAdminEmail(email string) error {
	if email == "" {
		return userError("Indiquez votre e-mail : il recevra le lien d'administration de l'événement.")
	}
	if !validEmail(email) {
		return userError("L'adresse e-mail de l'organisateur ne semble pas valide.")
	}
	return nil
}

// validate checks a volunteer form: only the name is required.
func (f volunteerForm) validate() error {
	if f.Name == "" {
		return userError("Merci d'indiquer votre nom.")
	}
	if f.Phone != "" && !phoneRe.MatchString(f.Phone) {
		return userError("Le numéro de téléphone ne semble pas valide.")
	}
	return nil
}
