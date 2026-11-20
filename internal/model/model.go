// Package model holds the domain types shared by the store and the web layer,
// plus small pure helpers (display names, fill status, overlaps).
package model

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TimeLayout is how slot times are stored in SQLite: local wall-clock time,
// without timezone (an event happens in one place, one timezone).
const TimeLayout = "2006-01-02 15:04"

// DateLayout is the layout of Event.Date.
const DateLayout = "2006-01-02"

// Event statuses.
const (
	StatusDraft  = "draft"
	StatusOpen   = "open"
	StatusClosed = "closed"
)

// Statuses lists the event statuses in their natural order.
var Statuses = []string{StatusDraft, StatusOpen, StatusClosed}

// ValidStatus reports whether s is one of Statuses.
func ValidStatus(s string) bool {
	return s == StatusDraft || s == StatusOpen || s == StatusClosed
}

type Event struct {
	ID          int64
	Slug        string
	Title       string
	Description string
	Location    string
	Date        string // YYYY-MM-DD, may be empty
	AdminToken  string
	Status      string
	CreatedAt   time.Time
	// AdminEmail receives the admin link at creation and, when
	// NotifySignups is set, a message for each sign-up or cancellation.
	AdminEmail    string
	NotifySignups bool
	// RetentionDays is how many days after its last day of activity the event
	// is deleted. 0 means that the organiser chose nothing, so the server
	// default applies. The server also caps it (see store.Retention).
	RetentionDays int
}

// ParsedDate returns the event date, or zero time if unset/invalid.
func (e *Event) ParsedDate() time.Time {
	t, err := time.Parse(DateLayout, e.Date)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (e *Event) IsOpen() bool   { return e.Status == StatusOpen }
func (e *Event) IsDraft() bool  { return e.Status == StatusDraft }
func (e *Event) IsClosed() bool { return e.Status == StatusClosed }

type Stand struct {
	ID          int64
	EventID     int64
	Name        string
	Description string
	Position    int
	Color       string
	// FillRow colours the whole row of the stand in the planning, not only
	// the dot next to its name.
	FillRow bool
}

type Slot struct {
	ID        int64
	StandID   int64
	StartsAt  time.Time
	EndsAt    time.Time
	MinPeople int
	MaxPeople int // 0 = no maximum
}

// Errors returned by Slot.Check.
var (
	ErrSlotRange = errors.New("slot ends before it starts")
	ErrSlotMin   = errors.New("slot minimum is below 1")
	ErrSlotMax   = errors.New("slot maximum is below its minimum")
)

// Check verifies the times and capacities of a slot: it ends after it
// starts, needs at least one person, and its maximum (0 = none) is not
// below its minimum.
func (s Slot) Check() error {
	switch {
	case !s.EndsAt.After(s.StartsAt):
		return ErrSlotRange
	case s.MinPeople < 1:
		return ErrSlotMin
	case s.MaxPeople != 0 && s.MaxPeople < s.MinPeople:
		return ErrSlotMax
	}
	return nil
}

// MaxMomentTitle is the longest title of a moment, in characters.
const MaxMomentTitle = 80

// Errors returned by Moment.Check.
var (
	ErrMomentRange = errors.New("moment ends before it starts")
	ErrMomentTitle = errors.New("moment title is empty or too long")
)

// Moment is a titled part of the day, for example "Spectacle des enfants".
// It has a start and an end in local wall-clock time. It only informs the
// public: nobody signs up for a moment, and it blocks nothing.
type Moment struct {
	ID      int64
	EventID int64
	Title   string
	// Color is empty (no color) or "#rrggbb".
	Color string
	// Fill tints the column of the moment with its color, not only the dot
	// next to its title. It has no effect without a color.
	Fill     bool
	StartsAt time.Time
	EndsAt   time.Time
}

// Check makes sure that the moment ends after it starts and that its title
// has 1 to MaxMomentTitle characters after trimming spaces.
func (m Moment) Check() error {
	if !m.EndsAt.After(m.StartsAt) {
		return ErrMomentRange
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(m.Title)); n < 1 || n > MaxMomentTitle {
		return ErrMomentTitle
	}
	return nil
}

// Overlaps reports whether two slots share some time.
func (s Slot) Overlaps(o Slot) bool {
	return s.StartsAt.Before(o.EndsAt) && o.StartsAt.Before(s.EndsAt)
}

// IsFull reports whether no more volunteers can sign up for the slot.
func (s Slot) IsFull(count int) bool {
	return s.MaxPeople > 0 && count >= s.MaxPeople
}

// Fill statuses, used as CSS classes.
const (
	FillEmpty   = "empty"   // nobody yet (and somebody is needed)
	FillPartial = "partial" // some people, but fewer than needed
	FillOK      = "ok"      // minimum reached, more people still welcome
	FillFull    = "full"    // maximum reached, sign-ups closed
)

// FillStatus classifies a slot given its current number of sign-ups.
func (s Slot) FillStatus(count int) string {
	switch {
	case s.IsFull(count):
		return FillFull
	case count >= s.MinPeople:
		return FillOK
	case count == 0:
		return FillEmpty
	default:
		return FillPartial
	}
}

// Missing is the number of people still needed to reach the minimum.
func (s Slot) Missing(count int) int {
	if count >= s.MinPeople {
		return 0
	}
	return s.MinPeople - count
}

type Volunteer struct {
	ID        int64
	EventID   int64
	Name      string
	Phone     string
	EditToken string
	CreatedAt time.Time
}

type Signup struct {
	ID          int64
	SlotID      int64
	VolunteerID int64
	Comment     string
	CreatedAt   time.Time
}

// SignupRow is a sign-up joined with its volunteer, as listed on pages.
type SignupRow struct {
	Signup
	Name  string
	Phone string
}

// DisplayName returns the public form of a name: the first word, then the
// initial of every other word ("Marie Dupont" -> "Marie D.", "Julien Le
// Jeune" -> "Julien LJ."). Single words are kept.
func DisplayName(name string) string {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	var initials strings.Builder
	for _, p := range parts[1:] {
		r, _ := utf8.DecodeRuneInString(p)
		initials.WriteRune(unicode.ToUpper(r))
	}
	return parts[0] + " " + initials.String() + "."
}
