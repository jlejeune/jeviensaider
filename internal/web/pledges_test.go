package web

import (
	"encoding/csv"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	listIDRe   = regexp.MustCompile(`id="list-(\d+)"`)
	pledgeIDRe = regexp.MustCompile(`/pledges/(\d+)/delete`)
	publicRe   = regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`)
	listItemRe = regexp.MustCompile(`(?s)<li>.*?</li>`)
	inputRe    = regexp.MustCompile(`<input\b[^>]*>`)
	textareaRe = regexp.MustCompile(`(?s)<textarea\b([^>]*)>(.*?)</textarea>`)
)

// newDraftEvent creates a draft event dated 2026-06-20 and returns its admin
// path.
func newDraftEvent(t *testing.T, e *env, admin *http.Client) string {
	t.Helper()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}, "date": {"2026-06-20"}})
	expect(t, r, http.StatusSeeOther)
	return r.location
}

// addList posts a list to the admin page and returns its ID, read from the
// redirect to the list anchor.
func addList(t *testing.T, e *env, admin *http.Client, adminPath string, list url.Values) string {
	t.Helper()
	r := e.post(admin, adminPath+"/lists", list)
	expect(t, r, http.StatusSeeOther)
	rest, ok := strings.CutPrefix(r.location, adminPath+"?list=")
	id, anchor, found := strings.Cut(rest, "#list-")
	if !ok || !found || id != anchor {
		t.Fatalf("the redirect %q does not point to the new list", r.location)
	}
	return id
}

// listLocation is where the admin page sends the organizer after a change to
// a list: the page, scrolled to the list, with that list open.
func listLocation(adminPath, id string) string {
	return adminPath + "?list=" + id + "#list-" + id
}

// openEventWithList creates an open event dated 2026-06-20 with one list,
// and returns the admin path, the public path and the list ID.
func openEventWithList(t *testing.T, e *env, admin *http.Client, list url.Values) (adminPath, pub, listID string) {
	t.Helper()
	adminPath = newDraftEvent(t, e, admin)
	expect(t, e.post(admin, adminPath+"/lists", list), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	page := e.get(admin, adminPath).body
	m := listIDRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no list on the admin page:\n%s", page)
	}
	listID = m[1]
	pm := publicRe.FindStringSubmatch(page)
	if pm == nil {
		t.Fatal("public link not found on the admin page")
	}
	return adminPath, pm[1], listID
}

// addPledge adds a pledge by hand through the admin page.
func addPledge(e *env, admin *http.Client, adminPath, listID, name string, quantity int) resp {
	return e.post(admin, adminPath+"/pledges", url.Values{
		"list": {listID}, "name": {name}, "phone": {"07 11 22 33 44"}, "quantity": {strconv.Itoa(quantity)},
	})
}

// pledge posts the public pledge form of a list as the visitor c.
func pledge(e *env, c *http.Client, pub, listID, name string, quantity int) resp {
	return e.post(c, pub+"/lists/"+listID+"/signup", url.Values{
		"name": {name}, "phone": {"07 11 22 33 44"}, "quantity": {strconv.Itoa(quantity)},
	})
}

// listItem returns the first <li> of the page that contains text.
func listItem(t *testing.T, page, text string) string {
	t.Helper()
	for _, li := range listItemRe.FindAllString(page, -1) {
		if strings.Contains(li, text) {
			return li
		}
	}
	t.Fatalf("no list item contains %q:\n%s", text, page)
	return ""
}

// formValues returns what a browser sends when it submits the form of the
// page that has this action, with the values that the page filled in.
func formValues(t *testing.T, page, action string) url.Values {
	t.Helper()
	m := regexp.MustCompile(`(?s)<form\b[^>]*action="` + regexp.QuoteMeta(action) + `"[^>]*>(.*?)</form>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no form with the action %q:\n%s", action, page)
	}
	attr := func(tag, name string) (string, bool) {
		am := regexp.MustCompile(`\b` + name + `="([^"]*)"`).FindStringSubmatch(tag)
		if am == nil {
			return "", false
		}
		return html.UnescapeString(am[1]), true
	}
	v := url.Values{}
	for _, tag := range inputRe.FindAllString(m[1], -1) {
		if name, ok := attr(tag, "name"); ok {
			value, _ := attr(tag, "value")
			v.Set(name, value)
		}
	}
	for _, ta := range textareaRe.FindAllStringSubmatch(m[1], -1) {
		if name, ok := attr(ta[1], "name"); ok {
			v.Set(name, html.UnescapeString(ta[2]))
		}
	}
	return v
}

func TestAdminPledgeLists(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	expect(t, e.get(admin, adminPath), 200, "À apporter", "Nouvelle liste", "Aucune liste")

	id := addList(t, e, admin, adminPath, url.Values{
		"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}, "drop_off": {"08:30"}, "min": {"18"},
	})
	r := e.get(admin, adminPath)
	expect(t, r, 200,
		"Liste « Gâteaux sucrés » ajoutée.",
		"À apporter", "Gâteaux sucrés", "Dépôt à 8h30",
		html.EscapeString("Rien de promis pour l'instant : il en manque 18"),
		`id="list-`+id+`"`,
		// The labels and the hint of the edit form.
		"Heure de dépôt", `name="drop_off"`, `type="time"`, "Minimum", "Maximum", "Unité",
		"Au pluriel, par exemple « gâteaux ».",
		`data-confirm="Supprimer la liste « Gâteaux sucrés » ?"`,
	)
	if strings.Contains(r.body, "Au total") {
		t.Error("one list must not show an overall total")
	}

	// Two pledges change the status and the delete confirmation.
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 1), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, id, "Paul Martin", 1), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200,
		"2 gâteaux promis sur 18 : il en manque 16",
		`data-confirm="Supprimer la liste « Gâteaux sucrés » avec ses 2 inscriptions ?"`)

	// Update: the new name shows, with the same pledges.
	r = e.post(admin, adminPath+"/lists/"+id, url.Values{"name": {"Gâteaux salés"}, "unit": {"gâteaux"}, "drop_off": {"09:15"}, "min": {"18"}})
	expect(t, r, http.StatusSeeOther)
	if r.location != listLocation(adminPath, id) {
		t.Errorf("update redirects to %q", r.location)
	}
	r = e.get(admin, adminPath)
	expect(t, r, 200, "Liste mise à jour.", "Gâteaux salés", "Dépôt à 9h15", "Marie Dupont")
	if strings.Contains(r.body, "Gâteaux sucrés") || strings.Contains(r.body, "Dépôt à 8h30") {
		t.Error("the old values are still on the page")
	}

	// Delete: the list goes with its pledges.
	expect(t, e.post(admin, adminPath+"/lists/"+id+"/delete", nil), http.StatusSeeOther)
	r = e.get(admin, adminPath)
	expect(t, r, 200, "Liste supprimée (avec ses inscriptions).", "Aucune liste")
	for _, gone := range []string{`id="list-`, "Gâteaux salés", "Marie Dupont"} {
		if strings.Contains(r.body, gone) {
			t.Errorf("after the delete, the page still has %q", gone)
		}
	}
	expect(t, e.post(admin, adminPath+"/lists/"+id+"/delete", nil), http.StatusNotFound)
}

// The edit form must show an unset minimum or maximum as an empty field. The
// server refuses an explicit 0, so a "0" would break an unchanged save.
func TestAdminPledgeListSavesUnchanged(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	for name, list := range map[string]url.Values{
		"none": {"name": {"Sans limite"}},
		"min":  {"name": {"Avec minimum"}, "min": {"5"}},
		"max":  {"name": {"Avec maximum"}, "max": {"7"}},
		"both": {"name": {"Avec les deux"}, "min": {"5"}, "max": {"7"}, "unit": {"cakes"}, "drop_off": {"08:30"}, "description": {"Pour la buvette"}},
	} {
		id := addList(t, e, admin, adminPath, list)
		form := formValues(t, e.get(admin, adminPath).body, adminPath+"/lists/"+id)
		for _, field := range []string{"min", "max"} {
			if _, ok := list[field]; !ok && form.Get(field) != "" {
				t.Errorf("%s: unset %s shows as %q, want an empty field", name, field, form.Get(field))
			}
		}
		if form.Get("name") != list.Get("name") || form.Get("min") != list.Get("min") || form.Get("max") != list.Get("max") ||
			form.Get("unit") != list.Get("unit") || form.Get("drop_off") != list.Get("drop_off") || form.Get("description") != list.Get("description") {
			t.Errorf("%s: the edit form shows %v, want %v", name, form, list)
		}
		expect(t, e.post(admin, adminPath+"/lists/"+id, form), http.StatusSeeOther)
		expect(t, e.get(admin, adminPath), 200, "Liste mise à jour.")
	}
}

func TestAdminPledgeListErrors(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)

	for _, c := range []struct {
		name string
		form url.Values
		want string
	}{
		{"empty name", url.Values{"name": {""}}, "Le nom de la liste est obligatoire."},
		{"drop-off 8h", url.Values{"name": {"Cakes"}, "drop_off": {"8h"}}, "Heure de dépôt invalide (exemple : 08:30)."},
		{"minimum above maximum", url.Values{"name": {"Cakes"}, "min": {"5"}, "max": {"4"}}, "Le maximum doit être vide ou supérieur ou égal au minimum."},
		{"minimum 0", url.Values{"name": {"Cakes"}, "min": {"0"}}, "Minimum : nombre invalide : 0"},
	} {
		expect(t, e.post(admin, adminPath+"/lists", c.form), http.StatusSeeOther)
		expect(t, e.get(admin, adminPath), 200, c.want)
	}
	if strings.Contains(e.get(admin, adminPath).body, `id="list-`) {
		t.Error("a refused list was created")
	}

	// A maximum below the total is refused, and the list stays as it was.
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Cakes"}})
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 3), http.StatusSeeOther)
	r := e.post(admin, adminPath+"/lists/"+id, url.Values{"name": {"Cakes"}, "max": {"2"}})
	expect(t, r, http.StatusSeeOther)
	if r.location != listLocation(adminPath, id) {
		t.Errorf("the error redirects to %q, want the list anchor", r.location)
	}
	page := e.get(admin, adminPath)
	expect(t, page, 200, "Il y a déjà 3 promis : le maximum doit être au moins 3.", "3 promis")
	if strings.Contains(page.body, "Complet") {
		t.Error("the refused maximum was saved")
	}
}

func TestAdminPledgeListLimit(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	for i := 1; i <= 10; i++ {
		addList(t, e, admin, adminPath, url.Values{"name": {"Liste " + strconv.Itoa(i)}})
	}
	expect(t, e.post(admin, adminPath+"/lists", url.Values{"name": {"Liste 11"}}), http.StatusSeeOther)
	r := e.get(admin, adminPath)
	expect(t, r, 200, "Un événement ne peut pas avoir plus de 10 listes à apporter.")
	if n := strings.Count(r.body, `id="list-`); n != 10 {
		t.Errorf("the page has %d lists, want 10", n)
	}
}

func TestAdminAddAndRemovePledge(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	// The event stays a draft: an organizer can add by hand at any time.
	adminPath := newDraftEvent(t, e, admin)
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux"}, "unit": {"gâteaux"}, "max": {"2"}})

	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 2), http.StatusSeeOther)
	page := e.get(admin, adminPath)
	expect(t, page, 200, "Marie Dupont a été inscrit·e.", "Complet : 2 gâteaux promis")
	li := listItem(t, page.body, "Marie Dupont")
	for _, want := range []string{"07 11 22 33 44", `>2 gâteaux</strong>`, "/pledges/"} {
		if !strings.Contains(li, want) {
			t.Errorf("the row of Marie lacks %q:\n%s", want, li)
		}
	}

	// The hand add obeys the maximum.
	expect(t, addPledge(e, admin, adminPath, id, "Paul Martin", 1), http.StatusSeeOther)
	page = e.get(admin, adminPath)
	expect(t, page, 200, "Cette liste est complète.")
	if strings.Contains(page.body, "Paul Martin") {
		t.Error("a pledge over the maximum was saved")
	}

	// Remove Marie, then Paul can take her place.
	pledgeID := pledgeIDRe.FindStringSubmatch(li)[1]
	r := e.post(admin, adminPath+"/pledges/"+pledgeID+"/delete", nil)
	expect(t, r, http.StatusSeeOther)
	if r.location != listLocation(adminPath, id) {
		t.Errorf("the remove redirects to %q, want the list anchor", r.location)
	}
	page = e.get(admin, adminPath)
	expect(t, page, 200, "Inscription retirée.", html.EscapeString("Rien de promis pour l'instant"))
	if strings.Contains(page.body, "Marie Dupont") {
		t.Error("the pledge is still listed")
	}
	expect(t, e.post(admin, adminPath+"/pledges/"+pledgeID+"/delete", nil), http.StatusNotFound)
	expect(t, addPledge(e, admin, adminPath, id, "Paul Martin", 2), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Paul Martin a été inscrit·e.", "Complet : 2 gâteaux promis")

	// The form errors come back on the list.
	for _, c := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"list": {id}, "name": {""}, "quantity": {"1"}}, html.EscapeString("Merci d'indiquer votre nom.")},
		{url.Values{"list": {id}, "name": {"Léa"}, "quantity": {"21"}}, "Indiquez un nombre entre 1 et 20."},
		{url.Values{"list": {id}, "name": {"Léa"}, "phone": {"abc"}, "quantity": {"1"}}, "Le numéro de téléphone ne semble pas valide."},
	} {
		r := e.post(admin, adminPath+"/pledges", c.form)
		expect(t, r, http.StatusSeeOther)
		if r.location != listLocation(adminPath, id) {
			t.Errorf("the error redirects to %q, want the list anchor", r.location)
		}
		expect(t, e.get(admin, adminPath), 200, c.want)
	}
	expect(t, e.post(admin, adminPath+"/pledges", url.Values{"list": {"abc"}, "name": {"Léa"}}), http.StatusNotFound)
}

func TestAdminOverallTotal(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)

	sweet := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}, "min": {"18"}})
	if strings.Contains(e.get(admin, adminPath).body, "Au total") {
		t.Error("one list must not show an overall total")
	}

	salty := addList(t, e, admin, adminPath, url.Values{"name": {"Cakes salés"}, "min": {"3"}})
	expect(t, addPledge(e, admin, adminPath, sweet, "Marie Dupont", 14), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, salty, "Paul Martin", 3), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Au total : 17 promis sur 21")

	// A list without a minimum removes the "sur" part.
	expect(t, e.post(admin, adminPath+"/lists/"+salty, url.Values{"name": {"Cakes salés"}}), http.StatusSeeOther)
	r := e.get(admin, adminPath)
	expect(t, r, 200, "Au total : 17 promis")
	if strings.Contains(r.body, "Au total : 17 promis sur") {
		t.Error("the total shows a minimum although one list has none")
	}

	// Deleting a list leaves one, and the total goes.
	expect(t, e.post(admin, adminPath+"/lists/"+salty+"/delete", nil), http.StatusSeeOther)
	if strings.Contains(e.get(admin, adminPath).body, "Au total") {
		t.Error("one list must not show an overall total")
	}
}

func TestAdminPledgeScoping(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminA := newDraftEvent(t, e, admin)
	adminB := newDraftEvent(t, e, admin)
	listB := addList(t, e, admin, adminB, url.Values{"name": {"Liste B"}, "max": {"5"}})
	expect(t, addPledge(e, admin, adminB, listB, "Marie Dupont", 2), http.StatusSeeOther)
	pledgeB := pledgeIDRe.FindStringSubmatch(e.get(admin, adminB).body)[1]

	// Every attempt uses a valid body, so only the event can stop it.
	expect(t, e.post(admin, adminA+"/lists/"+listB, url.Values{"name": {"Piratée"}, "max": {"9"}}), http.StatusNotFound)
	expect(t, e.post(admin, adminA+"/lists/"+listB+"/delete", nil), http.StatusNotFound)
	expect(t, e.post(admin, adminA+"/pledges/"+pledgeB+"/delete", nil), http.StatusNotFound)
	expect(t, addPledge(e, admin, adminA, listB, "Paul Martin", 1), http.StatusNotFound)

	page := e.get(admin, adminB).body
	for _, want := range []string{"Liste B", "Marie Dupont", `id="list-` + listB + `"`, "2 promis, encore 3 places"} {
		if !strings.Contains(page, want) {
			t.Errorf("the list of event B lost %q:\n%s", want, page)
		}
	}
	for _, bad := range []string{"Piratée", "Paul Martin"} {
		if strings.Contains(page, bad) {
			t.Errorf("the list of event B has %q", bad)
		}
	}
	pageA := e.get(admin, adminA).body
	if strings.Contains(pageA, `id="list-`) || strings.Contains(pageA, "Marie Dupont") {
		t.Error("event A shows data of event B")
	}
}

func TestAdminPledgeRoutesNeedToken(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	form := url.Values{"name": {"Cakes"}, "list": {"1"}}
	// A real token reaches the route: the 404s below come from the token.
	expect(t, e.post(admin, adminPath+"/lists", form), http.StatusSeeOther)
	for _, p := range []string{"/lists", "/lists/1", "/lists/1/delete", "/pledges", "/pledges/1/delete"} {
		expect(t, e.post(admin, "/a/nope"+p, form), http.StatusNotFound)
	}
}

// Names and units come from visitors and organizers: the page must escape them.
func TestAdminPledgeEscaping(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	name, unit, person := `"><script>alert(1)</script>`, `<b>x</b>`, `<img src=x onerror=alert(1)>`
	id := addList(t, e, admin, adminPath, url.Values{"name": {name}, "unit": {unit}, "description": {"<i>d</i>"}})
	expect(t, addPledge(e, admin, adminPath, id, person, 2), http.StatusSeeOther)
	r := e.get(admin, adminPath)
	expect(t, r, 200, html.EscapeString(name), html.EscapeString(person))
	for _, raw := range []string{"<script>alert(1)</script>", "<b>x</b>", "<img src=x", "<i>d</i>"} {
		if strings.Contains(r.body, raw) {
			t.Errorf("the page has the raw text %q", raw)
		}
	}
}

func TestPledgeSignupFlow(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}})
	pub := publicRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]
	form := pub + "/lists/" + id + "/signup"

	// A draft hides its lists.
	expect(t, e.get(e.client(), form), http.StatusNotFound)
	expect(t, pledge(e, e.client(), pub, id, "Marie Dupont", 2), http.StatusNotFound)
	if strings.Contains(e.get(e.client(), pub).body, "À apporter") {
		t.Error("the page of a draft shows its lists")
	}
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)

	// The card and the form, as an htmx dialog and as a full page.
	marie := e.client()
	r := e.get(marie, pub)
	expect(t, r, 200, "À apporter", `id="pledge-`+id+`"`, "Gâteaux sucrés",
		html.EscapeString("Rien de promis pour l'instant"), `href="`+form+`"`, `hx-get="`+form+`"`, "Je m'en charge")
	expect(t, e.do(marie, "GET", form, nil, map[string]string{"HX-Request": "true"}), 200,
		"<dialog", "Combien en apportez-vous ?", `name="quantity"`, `min="1"`, `max="20"`, "Je m'en charge")
	expect(t, e.get(marie, form), 200, "<!doctype html>", "Prénom et nom", `name="website"`,
		// The same fields and promises as the slot sign-up form.
		`id="pledge-name"`, `id="pledge-phone"`, "Seuls les organisateurs le voient, pour vous joindre le jour J.",
		"Pas d'e-mail, pas de compte. Vous pourrez annuler à tout moment.")
	expect(t, e.get(marie, pub+"/lists/9999/signup"), http.StatusNotFound)

	// A first pledge: the personal page, with a welcome message.
	r = pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	if !strings.Contains(r.location, "/me/") || !strings.HasSuffix(r.location, "?welcome=1") {
		t.Fatalf("unexpected redirect %q", r.location)
	}
	r = e.get(e.client(), pub)
	expect(t, r, 200, "Marie D. (2)", "2 gâteaux promis")
	if strings.Contains(r.body, "Dupont") || strings.Contains(r.body, "07 11 22 33 44") {
		t.Error("the public page leaks personal data")
	}

	// The same browser changes the quantity: back to the event page.
	r = e.get(marie, form)
	expect(t, r, 200, "Vous en apportez 2", `value="2"`, "Enregistrer")
	r = pledge(e, marie, pub, id, "Marie Dupont", 3)
	expect(t, r, http.StatusSeeOther)
	if r.location != pub {
		t.Fatalf("a change redirects to %q, want %q", r.location, pub)
	}
	r = e.get(marie, pub)
	expect(t, r, 200, html.EscapeString("C'est noté ! Vous apportez 3 pour « Gâteaux sucrés ». Merci !"),
		"Marie D. (3)", "Vous en apportez 3", "Changer")
	if strings.Contains(r.body, "Marie D. (2)") {
		t.Error("the change made a second pledge")
	}

	// A list with a maximum refuses more than what is left.
	_, pub2, id2 := openEventWithList(t, e, admin, url.Values{"name": {"Cakes"}, "max": {"5"}})
	expect(t, pledge(e, e.client(), pub2, id2, "Marie Dupont", 4), http.StatusSeeOther)
	r = pledge(e, e.client(), pub2, id2, "Paul Martin", 3)
	expect(t, r, http.StatusConflict, "Vous ne pouvez en apporter que 1 sur cette liste.", `name="quantity"`, `value="Paul Martin"`)
	// The list of another event is not found.
	expect(t, e.get(e.client(), pub+"/lists/"+id2+"/signup"), http.StatusNotFound)
	expect(t, pledge(e, e.client(), pub, id2, "Paul Martin", 1), http.StatusNotFound)

	// A closed event shows the list with no link and no form.
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	r = e.get(e.client(), pub)
	expect(t, r, 200, "Marie D. (3)")
	if strings.Contains(r.body, form) {
		t.Error("a closed event links to the pledge form")
	}
	r = e.get(e.client(), form)
	expect(t, r, 200, "Les inscriptions ne sont pas ouvertes.")
	if strings.Contains(r.body, `name="quantity"`) {
		t.Error("a closed event shows the pledge form")
	}
	expect(t, pledge(e, e.client(), pub, id, "Léa Petit", 1), http.StatusConflict, "pas ouvertes")
}

// fullList creates a list with a maximum of 5, where Marie holds 3 and Paul
// holds 2. It returns the public path, the list ID and the client of Marie.
func fullList(t *testing.T, e *env) (pub, id string, marie *http.Client) {
	t.Helper()
	_, pub, id = openEventWithList(t, e, e.client(), url.Values{"name": {"Cakes"}, "max": {"5"}})
	marie = e.client()
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 3), http.StatusSeeOther)
	expect(t, pledge(e, e.client(), pub, id, "Paul Martin", 2), http.StatusSeeOther)
	expect(t, e.get(e.client(), pub), 200, "Complet : 5 promis", "Marie D. (3), Paul M. (2)")
	return pub, id, marie
}

func TestPledgeOwnQuantityNearMaximum(t *testing.T) {
	e := newEnv(t, Config{})
	pub, id, marie := fullList(t, e)

	// Marie cannot go above the maximum, but she can lower her number. The
	// message tells her the most that she can hold, which is her 3.
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 4), http.StatusConflict, "Vous ne pouvez en apporter que 3 sur cette liste.")
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	if r.location != pub {
		t.Errorf("the change redirects to %q, want %q", r.location, pub)
	}
	expect(t, e.get(e.client(), pub), 200, "4 promis, encore 1 place", "Marie D. (2), Paul M. (2)")
}

func TestPledgeFullListStillShowsFormToPledger(t *testing.T) {
	e := newEnv(t, Config{})
	pub, id, marie := fullList(t, e)
	form := pub + "/lists/" + id + "/signup"

	// Marie can still lower her number.
	expect(t, e.get(marie, form), 200, `name="quantity"`, `max="3"`, `value="3"`, "Enregistrer", "Vous en apportez 3")
	expect(t, e.get(marie, pub), 200, "Vous en apportez 3", "Changer", `href="`+form+`"`)

	// Somebody else gets no form.
	r := e.get(e.client(), form)
	expect(t, r, 200, `callout info">Complet`)
	if strings.Contains(r.body, `name="quantity"`) {
		t.Error("a full list shows the form to a visitor who is not on it")
	}
	r = e.get(e.client(), pub)
	expect(t, r, 200, "Complet : 5 promis")
	if strings.Contains(r.body, form) {
		t.Error("a full list links to the form for a visitor who is not on it")
	}
}

func TestPledgeTodoFilter(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	free := addList(t, e, admin, adminPath, url.Values{"name": {"Sans limite"}})
	reached := addList(t, e, admin, adminPath, url.Values{"name": {"Minimum atteint"}, "min": {"1"}})
	full := addList(t, e, admin, adminPath, url.Values{"name": {"Pleine"}, "max": {"1"}})
	missing := addList(t, e, admin, adminPath, url.Values{"name": {"Il en manque"}, "min": {"3"}})
	expect(t, addPledge(e, admin, adminPath, reached, "Marie Dupont", 1), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, full, "Paul Martin", 1), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, missing, "Léa Petit", 1), http.StatusSeeOther)

	all := `id="pledge-`
	r := e.get(e.client(), pub)
	expect(t, r, 200, "Tous les créneaux", all+free+`"`, all+reached+`"`, all+full+`"`, all+missing+`"`)
	// The event has slots, so only the slot section has the switch.
	if n := strings.Count(r.body, "Seulement les places à prendre"); n != 1 {
		t.Errorf("the page has %d switches, want 1", n)
	}
	// The public page has no total over the lists.
	if strings.Contains(r.body, "Au total") {
		t.Error("the public page shows an overall total")
	}

	r = e.get(e.client(), pub+"?todo=1")
	expect(t, r, 200, `aria-checked="true"`, all+free+`"`, all+missing+`"`)
	for name, id := range map[string]string{"a list with its minimum": reached, "a full list": full} {
		if strings.Contains(r.body, all+id+`"`) {
			t.Errorf("the filter shows %s", name)
		}
	}
}

func TestPledgeOnlyEventPage(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {"Gâteaux"}, "min": {"1"}})

	// An anonymous client: a known visitor also has the tab "Tous les créneaux".
	r := e.get(e.client(), pub)
	expect(t, r, 200, "À apporter", `id="pledge-`+id+`"`, "Seulement les places à prendre", `href="`+pub+`?todo=1"`)
	for _, bad := range []string{"Aucun créneau", "Tous les créneaux"} {
		if strings.Contains(r.body, bad) {
			t.Errorf("an event with lists only shows %q", bad)
		}
	}

	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 1), http.StatusSeeOther)
	expect(t, e.get(e.client(), pub), 200, `id="pledge-`+id+`"`)
	r = e.get(e.client(), pub+"?todo=1")
	expect(t, r, 200, "À apporter", `aria-checked="true"`)
	if strings.Contains(r.body, `id="pledge-`+id+`"`) {
		t.Error("the filter shows a list with its minimum")
	}
}

func TestPledgeInjection(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	// The name has a quote, so it cannot close the data-confirm attribute.
	name, unit, person := `<script>alert("1")</script>`, `<b>`, `<i>Léa</i> Martin`
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {name}, "unit": {unit}, "description": {"<u>desc</u>"}})
	visitor := e.client()
	r := pledge(e, visitor, pub, id, person, 2)
	expect(t, r, http.StatusSeeOther)
	meLink := strings.TrimSuffix(r.location, "?welcome=1")

	for page, want := range map[string][]string{
		pub:                              {html.EscapeString(name), html.EscapeString("2 <b> promis"), html.EscapeString("<i>Léa</i> M. (2)"), html.EscapeString("<u>desc</u>")},
		adminPath:                        {html.EscapeString(name), html.EscapeString("2 <b>"), html.EscapeString(person), html.EscapeString("<u>desc</u>")},
		pub + "/lists/" + id + "/signup": {html.EscapeString(name), html.EscapeString("<u>desc</u>")},
		// The personal page shows the name in the card title and in the
		// cancel confirmation. It shows no unit.
		meLink: {"<h3>" + html.EscapeString(name) + "</h3>", `data-confirm="Annuler votre inscription : ` + html.EscapeString(name) + ` ?"`},
	} {
		c := e.client()
		switch page {
		case adminPath:
			c = admin
		case meLink:
			c = visitor
		}
		r := e.get(c, page)
		expect(t, r, 200, want...)
		// The layout has its own <b> tag, so look for the unit after a number.
		for _, raw := range []string{name, "2 <b>", "<i>Léa</i>", "<u>desc</u>"} {
			if strings.Contains(r.body, raw) {
				t.Errorf("%s has the raw text %q", page, raw)
			}
		}
	}
}

func TestPledgeHoneypotAndValidation(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {"Gâteaux"}})
	form := pub + "/lists/" + id + "/signup"

	r := e.post(e.client(), form, url.Values{"name": {"Robot Bot"}, "quantity": {"2"}, "website": {"x"}})
	expect(t, r, http.StatusSeeOther)
	if r.location != pub {
		t.Errorf("the honeypot redirects to %q, want %q", r.location, pub)
	}
	expect(t, e.post(e.client(), form, url.Values{"name": {" "}, "quantity": {"1"}}), http.StatusBadRequest, "votre nom")
	for _, q := range []string{"0", "21", "abc"} {
		r = e.post(e.client(), form, url.Values{"name": {"Léa Petit"}, "quantity": {q}})
		expect(t, r, http.StatusBadRequest, "Indiquez un nombre entre 1 et 20.", `name="quantity"`, `value="Léa Petit"`)
	}
	for _, page := range []string{pub, adminPath} {
		body := e.get(admin, page).body
		for _, bad := range []string{"Robot", "Léa"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s shows %q after a refused pledge", page, bad)
			}
		}
	}
}

// tabCountRe finds the number on the "Mes créneaux" tab.
var tabCountRe = regexp.MustCompile(`Mes créneaux <span class="count">(\d+)</span>`)

// myTabCount returns the number on the "Mes créneaux" tab of a personal page.
func myTabCount(t *testing.T, page string) string {
	t.Helper()
	m := tabCountRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("the page has no \"Mes créneaux\" tab:\n%s", page)
	}
	return m[1]
}

// emptyMyText is what a volunteer without slots and pledges reads.
const emptyMyText = "Vous n'êtes inscrit·e sur aucun créneau."

// emptyListsOnlyText is what a volunteer without slots and pledges reads on an
// event that has lists and no slots.
const emptyListsOnlyText = "Vous n'êtes inscrit·e nulle part pour l'instant."

// A volunteer who only brings items still has a personal page with content.
func TestMyPledgesPageWithoutSlots(t *testing.T) {
	e := newEnv(t, Config{})
	// The list has no drop-off time, so the page offers no calendar entry.
	_, pub, id := openEventWithList(t, e, e.client(), url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}})
	marie := e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	meLink := strings.TrimSuffix(r.location, "?welcome=1")

	r = e.get(marie, meLink)
	form := pub + "/lists/" + id + "/signup"
	expect(t, r, 200, "Ce que j'apporte", "Gâteaux sucrés", "Vous en apportez 2",
		// The link to change opens the dialog of the list.
		"Changer", `href="`+form+`"`, `hx-get="`+form+`"`, `hx-target="#modal"`,
		// The volunteer can cancel while the event is open.
		"Je ne peux plus en apporter", `class="cancel-form"`,
		`data-confirm="Annuler votre inscription : Gâteaux sucrés ?"`)
	if n := myTabCount(t, r.body); n != "1" {
		t.Errorf("the tab count is %s, want 1", n)
	}
	// The page has no slot section, so it shows no empty heading.
	for _, bad := range []string{emptyMyText, "Ajouter à mon agenda", `id="my-slots"`} {
		if strings.Contains(r.body, bad) {
			t.Errorf("a volunteer with one pledge and no slot sees %q", bad)
		}
	}
}

func TestCancelPledgeFromPersonalPage(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}})
	marie, paul := e.client(), e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	marieLink := strings.TrimSuffix(r.location, "?welcome=1")
	r = pledge(e, paul, pub, id, "Paul Martin", 1)
	expect(t, r, http.StatusSeeOther)
	paulLink := strings.TrimSuffix(r.location, "?welcome=1")

	m := pledgeIDRe.FindStringSubmatch(e.get(marie, marieLink).body)
	if m == nil {
		t.Fatal("the personal page of Marie has no cancel form")
	}
	cancel := marieLink + "/pledges/" + m[1] + "/delete"
	// stillThere makes sure that the pledge of Marie is untouched.
	stillThere := func() {
		t.Helper()
		expect(t, e.get(e.client(), pub), 200, "Marie D. (2), Paul M.")
		expect(t, e.get(marie, marieLink), 200, "Vous en apportez 2")
	}

	// The link of another volunteer cannot cancel this pledge. The page is
	// the not-found page of the store, not the one for a bad link.
	r = e.post(paul, paulLink+"/pledges/"+m[1]+"/delete", nil)
	expect(t, r, http.StatusNotFound, "introuvable")
	stillThere()

	// A closed event refuses the cancel and says so. The page has no form.
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	r = e.post(marie, cancel, nil)
	expect(t, r, http.StatusSeeOther)
	if r.location != marieLink {
		t.Errorf("the refusal redirects to %q, want %q", r.location, marieLink)
	}
	r = e.get(marie, marieLink)
	expect(t, r, 200, msgClosed, "Vous en apportez 2")
	for _, bad := range []string{"Je ne peux plus en apporter", `class="cancel-form"`, "Changer"} {
		if strings.Contains(r.body, bad) {
			t.Errorf("a closed event shows %q on the personal page", bad)
		}
	}
	stillThere()
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)

	// The cancel removes the pledge, and the other pledge stays.
	r = e.post(marie, cancel, nil)
	expect(t, r, http.StatusSeeOther)
	if r.location != marieLink {
		t.Errorf("the cancel redirects to %q, want %q", r.location, marieLink)
	}
	r = e.get(marie, marieLink)
	expect(t, r, 200, "Votre inscription a été annulée.", emptyListsOnlyText)
	if strings.Contains(r.body, "Vous en apportez") {
		t.Error("the personal page still shows the cancelled pledge")
	}
	r = e.get(e.client(), pub)
	expect(t, r, 200, "Paul M.", "1 promis")
	if strings.Contains(r.body, "Marie D.") {
		t.Error("the public page still shows Marie after the cancel")
	}
	expect(t, e.get(paul, paulLink), 200, "Vous en apportez 1")

	// The pledge is gone, so a second cancel finds nothing.
	expect(t, e.post(marie, cancel, nil), http.StatusNotFound, "introuvable")
}

func TestMyPledgesAndSlotsTogether(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slot := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}, "drop_off": {"08:30"}})
	marie := e.client()
	meLink := strings.TrimSuffix(e.signUp(marie, pub, slot, "Marie Dupont"), "?welcome=1")
	// The same browser pledges, so both rows belong to one volunteer.
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 3), http.StatusSeeOther)

	r := e.get(marie, meLink)
	expect(t, r, 200,
		"1 créneau · 1 h · 1 stand", "Je ne peux plus venir",
		"Ce que j'apporte", "Gâteaux sucrés", "Vous en apportez 3", "Dépôt à 8h30", "Changer", "Je ne peux plus en apporter",
		"Ajouter à mon agenda", "Si vous annulez une inscription, supprimez-la aussi de votre agenda.")
	if n := myTabCount(t, r.body); n != "2" {
		t.Errorf("the tab count is %s, want 2", n)
	}
	if strings.Contains(r.body, emptyMyText) {
		t.Error("a volunteer with a slot and a pledge sees the empty text")
	}
	// The slot cards come first, then the pledges, then the calendar.
	slots, pledges, agenda := strings.Index(r.body, "Je ne peux plus venir"), strings.Index(r.body, "Ce que j'apporte"), strings.Index(r.body, "Ajouter à mon agenda")
	if !(slots < pledges && pledges < agenda) {
		t.Errorf("the sections are in the wrong order: slots %d, pledges %d, agenda %d", slots, pledges, agenda)
	}
	// The slot list stays free of pledges.
	if mine := mySlotsHTML(r.body); strings.Contains(mine, "Gâteaux sucrés") {
		t.Errorf("the pledge is in the slot list: %s", mine)
	}
}

// A list in the event does not add a pledge to a volunteer. Only the pledges
// of the volunteer count.
func TestNoPledgesKeepsOldEmptyText(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slot := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}})
	marie := e.client()
	meLink := strings.TrimSuffix(e.signUp(marie, pub, slot, "Marie Dupont"), "?welcome=1")

	// A volunteer with one slot has no pledge section, although the event has a list.
	r := e.get(marie, meLink)
	if n := myTabCount(t, r.body); n != "1" {
		t.Errorf("the tab count is %s, want 1", n)
	}
	if strings.Contains(r.body, "Ce que j'apporte") {
		t.Error("a volunteer without a pledge sees the pledge section")
	}

	// After the cancel, the volunteer has nothing and sees the old text.
	m := regexp.MustCompile(`/signups/(\d+)/delete`).FindStringSubmatch(r.body)
	if m == nil {
		t.Fatal("the personal page has no cancel form for the slot")
	}
	expect(t, e.post(marie, meLink+"/signups/"+m[1]+"/delete", nil), http.StatusSeeOther)
	r = e.get(marie, meLink)
	expect(t, r, 200, emptyMyText, "Voir les créneaux")
	if n := myTabCount(t, r.body); n != "0" {
		t.Errorf("the tab count is %s, want 0", n)
	}
	for _, bad := range []string{"Ce que j'apporte", "Ajouter à mon agenda"} {
		if strings.Contains(r.body, bad) {
			t.Errorf("a volunteer with nothing sees %q", bad)
		}
	}
}

// On an event with lists and no slots, a volunteer with nothing reads a text
// that does not talk about slots.
func TestListsOnlyEmptyPersonalPage(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, id := openEventWithList(t, e, e.client(), url.Values{"name": {"Gâteaux sucrés"}})
	marie := e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	meLink := strings.TrimSuffix(r.location, "?welcome=1")
	m := pledgeIDRe.FindStringSubmatch(e.get(marie, meLink).body)
	if m == nil {
		t.Fatal("the personal page has no cancel form for the pledge")
	}
	expect(t, e.post(marie, meLink+"/pledges/"+m[1]+"/delete", nil), http.StatusSeeOther)

	r = e.get(marie, meLink)
	expect(t, r, 200, emptyListsOnlyText, `href="`+pub+`">Voir l'événement</a>`)
	for _, bad := range []string{emptyMyText, "Voir les créneaux", "Voir tous les créneaux"} {
		if strings.Contains(r.body, bad) {
			t.Errorf("a volunteer with nothing on an event with lists only sees %q", bad)
		}
	}
}

// An event with lists and no slots has nothing to show in the progress card,
// so the organizer page hides it and still shows the lists.
func TestAdminListsOnlyEvent(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	// Without a stand and a list, the progress card stays.
	expect(t, e.get(admin, adminPath), 200, "Pas encore de créneau",
		"Ajoutez vos stands ou vos listes à apporter, puis choisissez « Ouvertes ».")

	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "min": {"6"}})
	r := e.get(admin, adminPath)
	expect(t, r, 200, "À apporter", `id="list-`+id+`"`, "Gâteaux sucrés",
		html.EscapeString("Rien de promis pour l'instant : il en manque 6"),
		// The hints after the event name the lists too.
		"Les stands, les créneaux et les listes à apporter sont gardés.",
		"de ses stands, créneaux, listes à apporter et inscriptions.")
	for _, bad := range []string{"Pas encore de créneau", `aria-label="Avancement"`, "places prises"} {
		if strings.Contains(r.body, bad) {
			t.Errorf("the organizer page of an event with lists only shows %q", bad)
		}
	}
}

// csvRows reads a CSV export with its BOM and its semicolons. Every row has
// the nine columns of the header, as the reader enforces.
func csvRows(t *testing.T, body string) [][]string {
	t.Helper()
	cr := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\ufeff")))
	cr.Comma = ';'
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("the CSV export is not valid: %v\n%s", err, body)
	}
	if len(rows) == 0 || len(rows[0]) != 9 {
		t.Fatalf("the CSV export needs a header with 9 columns:\n%s", body)
	}
	return rows
}

// signedAtRe matches the sign-up date of a row, such as "02/01/2026 15:04".
var signedAtRe = regexp.MustCompile(`^\d{2}/\d{2}/\d{4} \d{2}:\d{2}$`)

// pledgeCSVLines returns the rows of the pledge lists as text. The sign-up
// date depends on the clock, so the helper checks its shape and blanks it.
func pledgeCSVLines(t *testing.T, rows [][]string) []string {
	t.Helper()
	var out []string
	for _, row := range rows {
		if !strings.HasPrefix(row[0], "À apporter · ") {
			continue
		}
		row = append([]string(nil), row...)
		if row[7] != "" {
			if !signedAtRe.MatchString(row[7]) {
				t.Errorf("the sign-up date %q of the row %q has the wrong shape", row[7], row)
			}
			row[7] = ""
		}
		out = append(out, strings.Join(row, ";"))
	}
	return out
}

// checkLines compares the lines that the test got with the lines that it
// wants.
func checkLines(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("got these rows:\n%s\nwant these rows:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCSVExportPledges(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slots := setupEvent(t, e, admin) // dated 2026-06-20, one stand
	e.signUp(e.client(), pub, slots[0], "Marie Dupont")
	sweet := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}, "drop_off": {"08:30"}, "min": {"21"}})
	drinks := addList(t, e, admin, adminPath, url.Values{"name": {"Boissons"}, "min": {"3"}})
	expect(t, addPledge(e, admin, adminPath, sweet, "Marie Dupont", 2), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, sweet, "Paul Martin", 12), http.StatusSeeOther)
	expect(t, addPledge(e, admin, adminPath, drinks, "Léa Petit", 3), http.StatusSeeOther)

	r := e.get(admin, adminPath+"/export.csv")
	expect(t, r, 200)
	rows := csvRows(t, r.body)
	wantHeader := []string{"Stand", "Date", "Début", "Fin", "Nom", "Téléphone", "Commentaire", "Inscrit le", "Quantité"}
	if !slices.Equal(rows[0], wantHeader) {
		t.Errorf("the header is %q, want %q", rows[0], wantHeader)
	}

	// The slot rows keep their columns and have an empty "Quantité". The
	// pledge rows come after all of them.
	slotRows, seenPledge := 0, false
	for _, row := range rows[1:] {
		if strings.HasPrefix(row[0], "À apporter · ") {
			seenPledge = true
			continue
		}
		slotRows++
		if seenPledge || row[0] != "Pêche aux canards" || row[1] != "20/06/2026" || row[8] != "" {
			t.Errorf("slot row %q (after a pledge row: %v)", row, seenPledge)
		}
	}
	if slotRows != 6 { // 3 slots for 2 people: 1 sign-up and 5 free places
		t.Errorf("%d slot rows, want 6", slotRows)
	}
	first := rows[1]
	if first[2] != "14:00" || first[3] != "15:00" || first[4] != "Marie Dupont" || first[5] != "07 11 22 33 44" || !signedAtRe.MatchString(first[7]) {
		t.Errorf("the first slot row is %q", first)
	}

	// The pledge rows come list by list. A list under its minimum ends with a
	// row for what is missing.
	checkLines(t, pledgeCSVLines(t, rows), []string{
		"À apporter · Gâteaux sucrés;20/06/2026;08:30;;Marie Dupont;07 11 22 33 44;;;2",
		"À apporter · Gâteaux sucrés;20/06/2026;08:30;;Paul Martin;07 11 22 33 44;;;12",
		"À apporter · Gâteaux sucrés;20/06/2026;08:30;;(il en manque 7);;;;",
		"À apporter · Boissons;20/06/2026;;;Léa Petit;07 11 22 33 44;;;3",
	})
}

// An event with no date has no date to show on the pledge rows.
func TestCSVExportWithoutDate(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux"}, "drop_off": {"08:30"}})
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 1), http.StatusSeeOther)

	rows := csvRows(t, e.get(admin, adminPath+"/export.csv").body)
	checkLines(t, pledgeCSVLines(t, rows), []string{
		"À apporter · Gâteaux;;08:30;;Marie Dupont;07 11 22 33 44;;;1",
	})
}

// An event with lists and no stand still exports the pledge rows.
func TestCSVExportListsOnly(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	sweet := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "drop_off": {"08:30"}})
	addList(t, e, admin, adminPath, url.Values{"name": {"Salades"}, "min": {"4"}})
	expect(t, addPledge(e, admin, adminPath, sweet, "Marie Dupont", 2), http.StatusSeeOther)

	rows := csvRows(t, e.get(admin, adminPath+"/export.csv").body)
	if len(rows) != 3 || rows[0][0] != "Stand" || rows[0][8] != "Quantité" {
		t.Errorf("want the header and two rows, got %q", rows)
	}
	checkLines(t, pledgeCSVLines(t, rows), []string{
		"À apporter · Gâteaux sucrés;20/06/2026;08:30;;Marie Dupont;07 11 22 33 44;;;2",
		"À apporter · Salades;20/06/2026;;;(il en manque 4);;;;",
	})
}

// A name that starts with =, +, - or @ could run as a formula in a
// spreadsheet. The export puts a quote before it.
func TestCSVNeutralisesNames(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"=1+1"}}), http.StatusSeeOther)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	expect(t, e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{"date": {"2026-06-20"}, "from": {"14:00"}, "to": {"15:00"}, "step": {"60"}, "min": {"1"}}), http.StatusSeeOther)
	id := addList(t, e, admin, adminPath, url.Values{"name": {"@x"}})
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 1), http.StatusSeeOther)

	rows := csvRows(t, e.get(admin, adminPath+"/export.csv").body)
	if len(rows) != 3 || rows[1][0] != "'=1+1" {
		t.Errorf("the stand name is not neutralised: %q", rows)
	}
	checkLines(t, pledgeCSVLines(t, rows), []string{
		"À apporter · '@x;20/06/2026;;;Marie Dupont;07 11 22 33 44;;;1",
	})
}

// openEvent opens a draft event and returns its public path.
func openEvent(t *testing.T, e *env, admin *http.Client, adminPath string) string {
	t.Helper()
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	m := publicRe.FindStringSubmatch(e.get(admin, adminPath).body)
	if m == nil {
		t.Fatal("public link not found on the admin page")
	}
	return m[1]
}

// agendaNone is what a volunteer reads when the calendar file has no entry.
// The error page escapes the apostrophes.
var agendaNone = html.EscapeString("Vous n'avez aucun créneau à ajouter à l'agenda.")

// A list with a drop-off time gives a 30-minute entry at that time. A list
// without a drop-off time gives none.
func TestCalendarPledgeEntry(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	salads := addList(t, e, admin, adminPath, url.Values{"name": {"Salades"}})
	cakes := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "description": {"Sans arachides"}, "drop_off": {"08:30"}})
	pub := openEvent(t, e, admin, adminPath)
	marie := e.client()
	r := pledge(e, marie, pub, salads, "Marie Dupont", 1)
	expect(t, r, http.StatusSeeOther)
	me := strings.TrimSuffix(r.location, "?welcome=1")
	expect(t, pledge(e, marie, pub, cakes, "Marie Dupont", 2), http.StatusSeeOther)

	r = e.get(marie, me+"/agenda.ics")
	expect(t, r, 200, "BEGIN:VCALENDAR\r\n", "END:VCALENDAR\r\n",
		"DTSTART:20260620T083000\r\n", "DTEND:20260620T090000\r\n",
		"SUMMARY:À apporter : Gâteaux sucrés (Kermesse)\r\n", "DESCRIPTION:Sans arachides\r\n",
		"URL:http", pub, "TRIGGER:-P1D\r\n", "TRIGGER:-PT1H\r\n")
	if n := strings.Count(r.body, "BEGIN:VEVENT"); n != 1 {
		t.Errorf("want 1 event, got %d:\n%s", n, r.body)
	}
	if n := strings.Count(r.body, "BEGIN:VALARM"); n != 2 {
		t.Errorf("want 2 alarms, got %d:\n%s", n, r.body)
	}
	if strings.Contains(r.body, "Salades") {
		t.Error("the list without a drop-off time is in the file")
	}
	// The UID names the list and the volunteer, and it stays the same, so a
	// second import updates the entry.
	uid := regexp.MustCompile(`UID:` + regexp.QuoteMeta(strings.TrimPrefix(pub, "/e/")) + `-p` + cakes + `-\d+@jeviensaider\r\n`)
	if !uid.MatchString(r.body) {
		t.Errorf("the UID does not match %s:\n%s", uid, r.body)
	}
	uidLine := regexp.MustCompile(`UID:[^\r\n]+`)
	if a, b := uidLine.FindString(r.body), uidLine.FindString(e.get(marie, me+"/agenda.ics").body); a != b {
		t.Errorf("the UID changes between downloads: %q / %q", a, b)
	}
	// The secret link must not leak into the file.
	if strings.Contains(r.body, me[strings.LastIndex(me, "/")+1:]) {
		t.Error("the calendar file contains the personal token")
	}
}

// The pledge entries come after the slot entries, and they have the location
// and the link of the event, like the slot entries.
func TestCalendarPledgeAfterSlots(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, slot := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}, "location": {"Cour"}})
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "drop_off": {"08:30"}})
	marie := e.client()
	me := strings.TrimSuffix(e.signUp(marie, pub, slot, "Marie Dupont"), "?welcome=1")
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 2), http.StatusSeeOther)

	r := e.get(marie, me+"/agenda.ics")
	expect(t, r, 200, "SUMMARY:Buvette (Kermesse)", "DTSTART:20260620T140000", "SUMMARY:À apporter : Gâteaux sucrés (Kermesse)", "DTSTART:20260620T083000")
	for token, want := range map[string]int{"BEGIN:VEVENT": 2, "BEGIN:VALARM": 4, "LOCATION:Cour": 2, "URL:http": 2} {
		if n := strings.Count(r.body, token); n != want {
			t.Errorf("want %d of %q, got %d:\n%s", want, token, n, r.body)
		}
	}
	if strings.Index(r.body, "SUMMARY:Buvette") > strings.Index(r.body, "SUMMARY:À apporter") {
		t.Error("the pledge entry comes before the slot entry")
	}
}

// A volunteer with only a pledge has a calendar file, and the page offers it.
func TestCalendarPledgeOnlyVolunteer(t *testing.T) {
	e := newEnv(t, Config{})
	_, pub, id := openEventWithList(t, e, e.client(), url.Values{"name": {"Gâteaux sucrés"}, "drop_off": {"08:30"}})
	marie := e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	me := strings.TrimSuffix(r.location, "?welcome=1")

	expect(t, e.get(marie, me), 200, "Ajouter à mon agenda", me+"/agenda.ics")
	r = e.get(marie, me+"/agenda.ics")
	expect(t, r, 200, "BEGIN:VEVENT", "SUMMARY:À apporter : Gâteaux sucrés (Kermesse)")
	if ct := r.header.Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// Without a date or a drop-off time, a pledge has no entry. A volunteer with
// nothing else gets the 404 text and no button. A volunteer with a slot gets
// the slot entry only.
func TestCalendarPledgeWithoutDateOrDropOff(t *testing.T) {
	e := newEnv(t, Config{})
	// noEntry checks that the volunteer has no file and no button.
	noEntry := func(c *http.Client, me string) {
		t.Helper()
		expect(t, e.get(c, me+"/agenda.ics"), 404, agendaNone)
		page := e.get(c, me)
		expect(t, page, 200, "Ce que j'apporte")
		if strings.Contains(page.body, "agenda.ics") || strings.Contains(page.body, "Ajouter à mon agenda") {
			t.Error("the page offers a calendar file for a pledge without an entry")
		}
	}

	// An event without a date: the drop-off time has no day.
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "drop_off": {"08:30"}})
	pub := openEvent(t, e, admin, adminPath)
	marie := e.client()
	r = pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	noEntry(marie, strings.TrimSuffix(r.location, "?welcome=1"))

	// A list without a drop-off time, on an event with a date.
	_, pub, id = openEventWithList(t, e, e.client(), url.Values{"name": {"Gâteaux sucrés"}})
	paul := e.client()
	r = pledge(e, paul, pub, id, "Paul Martin", 1)
	expect(t, r, http.StatusSeeOther)
	noEntry(paul, strings.TrimSuffix(r.location, "?welcome=1"))

	// A slot and such a pledge: the file has the slot entry only.
	admin = e.client()
	adminPath, pub, slot := openEventWithSlot(t, e, admin, url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}})
	id = addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}})
	anne := e.client()
	me := strings.TrimSuffix(e.signUp(anne, pub, slot, "Anne Durand"), "?welcome=1")
	expect(t, pledge(e, anne, pub, id, "Anne Durand", 1), http.StatusSeeOther)
	r = e.get(anne, me+"/agenda.ics")
	expect(t, r, 200, "SUMMARY:Buvette (Kermesse)")
	if n := strings.Count(r.body, "BEGIN:VEVENT"); n != 1 || strings.Contains(r.body, "À apporter") {
		t.Errorf("want the slot entry only, got %d events:\n%s", n, r.body)
	}
}

// A draft event has no calendar file, even for a volunteer with a pledge.
func TestCalendarDraftStillHidden(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {"Gâteaux sucrés"}, "drop_off": {"08:30"}})
	marie := e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	me := strings.TrimSuffix(r.location, "?welcome=1")
	expect(t, e.get(marie, me+"/agenda.ics"), 200, "BEGIN:VEVENT")

	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"draft"}}), http.StatusSeeOther)
	r = e.get(marie, me+"/agenda.ics")
	expect(t, r, 404, "pas encore ouvertes")
	if strings.Contains(r.body, "BEGIN:VEVENT") {
		t.Error("the file of a draft event has an entry")
	}
}

// An event with lists and moments, and no slot, shows the moments as a list
// and the lists under it. It must not say that no slot exists, and the
// exports must keep working.
func TestPledgeListsWithMomentsAndNoSlot(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, id := openEventWithList(t, e, admin, url.Values{"name": {"Gâteaux"}, "min": {"2"}})
	addMoment(t, e, adminPath, "Spectacle des enfants", "2026-06-20 11:00", "2026-06-20 12:00")
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 1), http.StatusSeeOther)

	r := e.get(e.client(), pub)
	expect(t, r, 200, "Spectacle des enfants", "Pas d'inscription à ce moment", "À apporter", `id="pledge-`+id+`"`)
	if strings.Contains(r.body, "Aucun créneau") {
		t.Error(`the page says "Aucun créneau" on an event with moments and lists`)
	}
	if i, j := strings.Index(r.body, "Spectacle des enfants"), strings.Index(r.body, `id="pledge-`+id+`"`); i > j {
		t.Error("the moments must come before the lists")
	}
	expect(t, e.get(e.client(), pub+"?todo=1"), 200, "À apporter")
	expect(t, e.get(admin, adminPath), 200, "Spectacle des enfants", "Gâteaux")
	expect(t, e.get(admin, adminPath+"/export.csv"), 200, "Marie Dupont")
	expect(t, e.get(admin, adminPath+"/planning.pdf"), 200)
}

// The cards of the lists work like the cards of the stands: they start
// closed, and after a change to a list the page comes back with that card
// open, and only that one.
func TestPledgeListCardOpensAfterAChange(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath := newDraftEvent(t, e, admin)
	first := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux"}})
	second := addList(t, e, admin, adminPath, url.Values{"name": {"Boissons"}})

	openCard := regexp.MustCompile(`<details class="stand" id="list-(\d+)"[^>]*\bopen\b`)
	plain := e.get(admin, adminPath)
	expect(t, plain, 200, `id="pledges-title">À apporter <span class="section-count">2</span>`, `class="panel-foot"`)
	if m := openCard.FindAllStringSubmatch(plain.body, -1); len(m) != 0 {
		t.Errorf("open cards on the plain page: %v, want none", m)
	}

	r := e.post(admin, adminPath+"/lists/"+first, url.Values{"name": {"Gâteaux sucrés"}})
	expect(t, r, http.StatusSeeOther)
	if want := listLocation(adminPath, first); r.location != want {
		t.Fatalf("redirect = %q, want %q", r.location, want)
	}
	m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?list="+first).body, -1)
	if len(m) != 1 || m[0][1] != first {
		t.Errorf("open cards after changing list %s: %v, want only that one", first, m)
	}
	if m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?list="+second).body, -1); len(m) != 1 || m[0][1] != second {
		t.Errorf("open cards with ?list=%s: %v", second, m)
	}
	// A value that is not a list opens nothing.
	if m := openCard.FindAllStringSubmatch(e.get(admin, adminPath+"?list=abc").body, -1); len(m) != 0 {
		t.Errorf("open cards with ?list=abc: %v", m)
	}
}
