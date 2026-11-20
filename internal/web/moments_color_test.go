package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"jeviensaider/internal/model"
)

// storedMoments lists the moments of the event that adminPath belongs to.
func storedMoments(t *testing.T, e *env, adminPath string) []model.Moment {
	t.Helper()
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.store.ListMoments(testCtx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// setMomentColor gives a color to a moment that addMoment created.
func setMomentColor(t *testing.T, e *env, adminPath string, m model.Moment, color string) {
	t.Helper()
	ev, err := e.store.EventByAdminToken(testCtx, strings.TrimPrefix(adminPath, "/a/"))
	if err != nil {
		t.Fatal(err)
	}
	m.EventID, m.Color = ev.ID, color
	if err := e.store.UpdateMoment(testCtx, m); err != nil {
		t.Fatal(err)
	}
}

func momentForm(from, to, title, color string) url.Values {
	v := url.Values{"date": {"2026-06-20"}, "from": {from}, "to": {to}, "title": {title}}
	if color != "" {
		v.Set("color", color)
	}
	return v
}

func TestMomentColorFromTheForm(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	expect(t, e.post(admin, adminPath+"/moments", momentForm("12:00", "13:00", "Spectacle", "#e8c93a")), http.StatusSeeOther)
	if list := storedMoments(t, e, adminPath); len(list) != 1 || list[0].Color != "#e8c93a" {
		t.Fatalf("stored moments: %+v", list)
	}
	expect(t, e.get(admin, adminPath), 200, "--moment-color: #e8c93a", `type="color" name="color" value="#e8c93a"`)
	expect(t, e.get(e.client(), pub), 200, "--moment-color: #e8c93a")
}

func TestMomentInvalidColorIsEmpty(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	for i, color := range []string{"red", "#12", "#fff;background:url(x)", "#gggggg", ""} {
		from := time.Date(2026, 6, 20, 8+i, 0, 0, 0, time.UTC)
		f := momentForm(from.Format("15:04"), from.Add(30*time.Minute).Format("15:04"), "Moment", "")
		f.Set("color", color)
		expect(t, e.post(admin, adminPath+"/moments", f), http.StatusSeeOther)
	}
	list := storedMoments(t, e, adminPath)
	if len(list) != 5 {
		t.Fatalf("stored %d moments, want 5", len(list))
	}
	for _, m := range list {
		if m.Color != "" {
			t.Errorf("a moment kept the color %q, want none", m.Color)
		}
	}
	if body := e.get(e.client(), pub).body; strings.Contains(body, "--moment-color") {
		t.Error("an invalid color reached the public page")
	}
}

func TestMomentNoColorCheckbox(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	expect(t, e.post(admin, adminPath+"/moments", momentForm("12:00", "13:00", "Spectacle", "#e8c93a")), http.StatusSeeOther)
	id := i64(storedMoments(t, e, adminPath)[0].ID)

	f := momentForm("12:00", "13:00", "Spectacle", "#e8c93a")
	f.Set("nocolor", "1")
	expect(t, e.post(admin, adminPath+"/moments/"+id, f), http.StatusSeeOther)
	if got := storedMoments(t, e, adminPath)[0].Color; got != "" {
		t.Errorf("color after \"Sans couleur\" = %q, want none", got)
	}
	expect(t, e.get(admin, adminPath), 200, `value="#fbf0c4"`, `name="nocolor" value="1" checked`)
}

func TestUncoloredMomentKeepsTheOldMarkup(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	addMoment(t, e, adminPath, "Goûter", "2026-06-20 14:00", "2026-06-20 15:00")
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 11:00", "2026-06-20 12:00")

	body := e.get(e.client(), pub).body
	for _, want := range []string{`class="col-title">Goûter`, `class="moment"`, `class="moment-cell"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not contain %q", want)
		}
	}
	for _, not := range []string{"moment-dot", "tinted", "--moment-color"} {
		if strings.Contains(body, not) {
			t.Errorf("a moment with no color must not produce %q", not)
		}
	}
}

func TestColorOnAJoinedColumn(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	m := addMoment(t, e, adminPath, "Goûter", "2026-06-20 14:00", "2026-06-20 15:00")
	setMomentColor(t, e, adminPath, m, "#e8c93a")

	body := e.get(e.client(), pub).body
	if !strings.Contains(body, `class="dot moment-dot"`) {
		t.Error("the header must show the dot")
	}
	if strings.Contains(body, "tinted") {
		t.Error("a column that has slots must not be tinted")
	}
}

// A color alone gives the dot. The tint of the column is a choice, made with
// the "fill" box of the moment.
func TestColorWithoutFillOnlyGivesTheDot(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	m := addMoment(t, e, adminPath, "Spectacle", "2026-06-20 11:00", "2026-06-20 12:00")
	setMomentColor(t, e, adminPath, m, "#e8c93a")

	body := e.get(e.client(), pub).body
	for _, want := range []string{`class="moment"`, `class="moment-cell"`, `class="dot moment-dot"`, "--moment-color: #e8c93a"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not contain %q", want)
		}
	}
	if strings.Contains(body, "tinted") {
		t.Error("a moment without the fill must not tint its column")
	}
}

func TestFillTintsAMomentOnlyColumn(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	m := addMoment(t, e, adminPath, "Spectacle", "2026-06-20 11:00", "2026-06-20 12:00")
	setMomentColor(t, e, adminPath, m, "#e8c93a")
	m.Fill = true
	setMomentColor(t, e, adminPath, m, "#e8c93a")

	body := e.get(e.client(), pub).body
	expect(t, resp{code: 200, body: body}, 200, `class="moment tinted"`, `class="moment-cell tinted"`, `<section class="tgroup tinted"`, "--moment-color: #e8c93a")
}

// The fill also tints a column that has slots.
func TestFillTintsAColumnWithSlots(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	m := addMoment(t, e, adminPath, "Goûter", "2026-06-20 14:00", "2026-06-20 15:00")
	m.Fill = true
	setMomentColor(t, e, adminPath, m, "#e8c93a")

	// The header, the cell of the stand and the group of the phone list.
	body := e.get(e.client(), pub).body
	if n := strings.Count(body, "tinted"); n != 3 {
		t.Errorf("%d tinted parts, want 3 (header, cell, phone group)", n)
	}
	// The admin grid shows the same: the header and the cell.
	if body := e.get(admin, adminPath).body; strings.Count(body, `class="tinted"`) != 2 {
		t.Error("the admin grid does not tint the column")
	}
}

func TestMomentFillFromTheForm(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})

	f := momentForm("12:00", "13:00", "Spectacle", "#e8c93a")
	f.Set("fill", "1")
	expect(t, e.post(admin, adminPath+"/moments", f), http.StatusSeeOther)
	if m := storedMoments(t, e, adminPath)[0]; !m.Fill {
		t.Fatalf("fill not saved: %+v", m)
	}
	expect(t, e.get(admin, adminPath), 200, `name="fill" value="1" checked`)
	id := i64(storedMoments(t, e, adminPath)[0].ID)

	// A form without the box turns it off.
	expect(t, e.post(admin, adminPath+"/moments/"+id, momentForm("12:00", "13:00", "Spectacle", "#e8c93a")), http.StatusSeeOther)
	if m := storedMoments(t, e, adminPath)[0]; m.Fill {
		t.Error("fill still on after the box was cleared")
	}
	// The fill needs a color: "Sans couleur" turns it off too.
	f = momentForm("12:00", "13:00", "Spectacle", "#e8c93a")
	f.Set("fill", "1")
	f.Set("nocolor", "1")
	expect(t, e.post(admin, adminPath+"/moments/"+id, f), http.StatusSeeOther)
	if m := storedMoments(t, e, adminPath)[0]; m.Fill || m.Color != "" {
		t.Errorf("a moment with no color kept the fill: %+v", m)
	}
}

// A moment with no color opens its edit panel with "Sans couleur" ticked. A
// color that the organizer picks must still be saved, without unticking the box.
func TestMomentPickedColorWinsOverTheTickedBox(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, _, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	addMoment(t, e, adminPath, "Spectacle", "2026-06-20 11:00", "2026-06-20 12:00")
	id := i64(storedMoments(t, e, adminPath)[0].ID)

	// The panel sends back the color it showed, so the server can see a change.
	expect(t, e.get(admin, adminPath), 200, `name="color_was" value="#fbf0c4"`)

	f := momentForm("11:00", "12:00", "Spectacle", "#ff0000")
	f.Set("nocolor", "1")
	f.Set("color_was", "#fbf0c4")
	expect(t, e.post(admin, adminPath+"/moments/"+id, f), http.StatusSeeOther)
	if got := storedMoments(t, e, adminPath)[0].Color; got != "#ff0000" {
		t.Errorf("color after picking red with the box ticked = %q, want #ff0000", got)
	}

	// The picker was not touched and the box is ticked: no color.
	f.Set("color", "#ff0000")
	f.Set("color_was", "#ff0000")
	expect(t, e.post(admin, adminPath+"/moments/"+id, f), http.StatusSeeOther)
	if got := storedMoments(t, e, adminPath)[0].Color; got != "" {
		t.Errorf("color after ticking the box with an untouched picker = %q, want none", got)
	}
}
