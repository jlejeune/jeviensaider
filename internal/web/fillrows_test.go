package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The organiser chooses, for each stand, whether its whole row is coloured
// or only its dot. The choice shows on the public planning and on the admin grid.
func TestStandFillRowSetting(t *testing.T) {
	e := newEnv(t, Config{})
	admin := e.client()
	adminPath, pub, _ := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}})
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Tombola"}, "fill_row": {"1"}}), http.StatusSeeOther)
	ids := regexp.MustCompile(`id="stand-(\d+)"`).FindAllStringSubmatch(e.get(admin, adminPath).body, -1)
	buvette, tombola := ids[0][1], ids[1][1]
	filled := func(body string) int { return strings.Count(body, `<tr class="fill"`) }

	// A new stand is off unless the box was ticked; the edit form shows the state.
	adminBody := e.get(admin, adminPath).body
	if n := filled(adminBody); n != 1 {
		t.Errorf("admin grid: %d filled rows, want 1 (Tombola)", n)
	}
	if n := strings.Count(adminBody, `name="fill_row" value="1" checked`); n != 1 {
		t.Errorf("%d ticked boxes, want 1", n)
	}
	expect(t, e.get(e.client(), pub), 200, `<tr class="fill"`)

	// Tick the other stand: both rows fill. A form without the box turns it off.
	expect(t, e.post(admin, adminPath+"/stands/"+buvette, url.Values{"name": {"Buvette"}, "fill_row": {"1"}}), http.StatusSeeOther)
	if n := filled(e.get(e.client(), pub).body); n != 2 {
		t.Errorf("public page: %d filled rows, want 2", n)
	}
	expect(t, e.post(admin, adminPath+"/stands/"+tombola, url.Values{"name": {"Tombola"}}), http.StatusSeeOther)
	body := e.get(e.client(), pub).body
	if n := filled(body); n != 1 {
		t.Errorf("public page: %d filled rows after unticking, want 1", n)
	}
	// The phone list marks the slots of a filled stand.
	if !regexp.MustCompile(`class="slot [^"]*\bfill\b`).MatchString(body) {
		t.Error("phone list does not mark the slots of a filled stand")
	}
}
