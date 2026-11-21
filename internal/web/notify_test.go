package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"jeviensaider/internal/mail"
)

// fakeMailer records the messages instead of sending them.
type fakeMailer struct {
	mu   sync.Mutex
	msgs []mail.Message
}

func (f *fakeMailer) Enqueue(m mail.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
}

func (f *fakeMailer) take() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.msgs
	f.msgs = nil
	return m
}

func TestAdminEmailRequired(t *testing.T) {
	e := newEnv(t, Config{})
	c := e.client()
	expect(t, e.get(c, "/"), 200, `name="admin_email"`, "required")
	expect(t, e.post(c, "/events", url.Values{"title": {"Fête"}}), http.StatusBadRequest, "Indiquez votre e-mail")
	expect(t, e.post(c, "/events", url.Values{"title": {"Fête"}, "admin_email": {"x@y.fr?bcc=z@evil.fr"}}), http.StatusBadRequest, "ne semble pas valide")
}

func TestOrganiserEmails(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	admin := e.client()

	// Creation: the organiser receives the admin and public links.
	r := e.post(admin, "/events", url.Values{"title": {"Fête de l'école"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}, "notify": {"1"}})
	expect(t, r, http.StatusSeeOther)
	adminPath := r.location
	expect(t, e.get(admin, adminPath), 200, "il vient de vous être envoyé à orga@example.org", "E-mails de l'organisateur envoyés à orga@example.org")
	msgs := fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages after creation", len(msgs))
	}
	m := msgs[0]
	if m.To != "orga@example.org" || !strings.Contains(m.Subject, "Fête de l'école") ||
		!strings.Contains(m.Body, "https://jeviensaider.example"+adminPath) || !strings.Contains(m.Body, "https://jeviensaider.example/e/") {
		t.Fatalf("creation mail: %+v", m)
	}

	// Build a stand with a slot and open the event.
	expect(t, e.post(admin, adminPath+"/stands", url.Values{"name": {"Buvette"}}), http.StatusSeeOther)
	standID := regexp.MustCompile(`id="stand-(\d+)"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	expect(t, e.post(admin, adminPath+"/stands/"+standID+"/slots", url.Values{"date": {"2026-06-20"}, "from": {"14:00"}, "to": {"15:00"}, "step": {"60"}, "min": {"2"}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	pub := regexp.MustCompile(`href="(/e/[^"]+)" target="_blank"`).FindStringSubmatch(e.get(admin, adminPath).body)[1]
	slot := regexp.MustCompile(`/slots/(\d+)/signup`).FindStringSubmatch(e.get(e.client(), pub).body)[1]
	fm.take()

	// Sign-up: the organiser is told who, when, and what is still missing.
	marie := e.client()
	r = e.post(marie, pub+"/slots/"+slot+"/signup", url.Values{"name": {"Marie Dupont"}, "phone": {"06 12 34 56 78"}, "comment": {"Je viens avec ma fille"}})
	expect(t, r, http.StatusSeeOther)
	msgs = fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages after sign-up", len(msgs))
	}
	m = msgs[0]
	for _, want := range []string{"Marie Dupont s'est inscrit·e sur « Buvette »", "06 12 34 56 78", "Je viens avec ma fille", "Ce créneau : 1/2, il reste 1 place", "https://jeviensaider.example" + adminPath} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("sign-up mail lacks %q:\n%s", want, m.Body)
		}
	}
	if m.Subject != "Inscription : Marie Dupont – Buvette, 14h – 15h" {
		t.Errorf("subject %q", m.Subject)
	}

	// Cancellation.
	meLink := strings.TrimSuffix(r.location, "?welcome=1")
	signupID := regexp.MustCompile(`/signups/(\d+)/delete`).FindStringSubmatch(e.get(marie, meLink).body)[1]
	expect(t, e.post(marie, meLink+"/signups/"+signupID+"/delete", url.Values{}), http.StatusSeeOther)
	msgs = fm.take()
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Subject, "Désinscription : Marie Dupont") || !strings.Contains(msgs[0].Body, "0/2, il reste 2 places") {
		t.Fatalf("cancellation mail: %+v", msgs)
	}

	// The organiser turns notifications off: no more messages.
	expect(t, e.post(admin, adminPath+"/event", url.Values{"title": {"Fête de l'école"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}}), http.StatusSeeOther)
	expect(t, e.post(e.client(), pub+"/slots/"+slot+"/signup", url.Values{"name": {"Paul"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("messages sent although notifications are off: %+v", msgs)
	}

	// The e-mail stays mandatory when editing the event.
	expect(t, e.post(admin, adminPath+"/event", url.Values{"title": {"Fête de l'école"}, "admin_email": {""}}), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Indiquez votre e-mail")

	// Duplicating sends the new admin link.
	expect(t, e.post(admin, adminPath+"/duplicate", url.Values{"title": {"Fête 2027"}}), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "Fête 2027") {
		t.Fatalf("duplicate mail: %+v", msgs)
	}
}

// The organiser can have the links e-mailed again, for instance to a new
// address; the per-address limit still applies.
func TestResendLinks(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	admin := e.client()
	r := e.post(admin, "/events", url.Values{"title": {"Fête"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}})
	adminPath := r.location
	expect(t, e.get(admin, adminPath), 200, "Renvoyer les liens par e-mail")
	fm.take() // the creation mail

	// The organiser's address changed: the links go to the new one.
	expect(t, e.post(admin, adminPath+"/event", url.Values{"title": {"Fête"}, "date": {"2026-06-20"}, "admin_email": {"new@example.org"}}), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/resend", nil), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Les liens de l&#39;événement ont été envoyés à new@example.org")
	msgs := fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages", len(msgs))
	}
	m := msgs[0]
	for _, want := range []string{"envoyés depuis sa page d'administration", "https://jeviensaider.example" + adminPath, "Lien public, à partager avec les bénévoles :\nhttps://jeviensaider.example/e/", "Si vous n'organisez pas cet événement"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("resent mail lacks %q:\n%s", want, m.Body)
		}
	}
	if m.To != "new@example.org" || m.Subject != "Les liens de votre événement « Fête »" || strings.Contains(m.Body, "Prochaines étapes") {
		t.Errorf("resent mail: %+v", m)
	}

	// The per-address limit (3 a day) counts resends.
	expect(t, e.post(admin, adminPath+"/resend", nil), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/resend", nil), http.StatusSeeOther)
	expect(t, e.post(admin, adminPath+"/resend", nil), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Trop d&#39;envois récents vers new@example.org")
	if n := len(fm.take()); n != 2 {
		t.Errorf("%d more mails, want 2", n)
	}
}

func TestResendLinksWithoutSMTP(t *testing.T) {
	e := newEnv(t, Config{Mailer: &fakeMailer{}}) // mails are only logged
	admin := e.client()
	adminPath := e.post(admin, "/events", url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}}).location
	if strings.Contains(e.get(admin, adminPath).body, "Renvoyer les liens") {
		t.Error("resend button shown without SMTP")
	}
	expect(t, e.post(admin, adminPath+"/resend", nil), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "pas configuré sur ce serveur")
}

// openNotifiedEvent creates an open event dated 2026-06-20 that e-mails the
// organizer about each sign-up. It returns the admin path and the public path.
// It drops the creation mail from fm.
func openNotifiedEvent(t *testing.T, e *env, fm *fakeMailer, admin *http.Client) (adminPath, pub string) {
	t.Helper()
	r := e.post(admin, "/events", url.Values{"admin_email": {"orga@example.org"}, "title": {"Kermesse"}, "date": {"2026-06-20"}, "notify": {"1"}})
	expect(t, r, http.StatusSeeOther)
	adminPath = r.location
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"open"}}), http.StatusSeeOther)
	pub = publicRe.FindStringSubmatch(e.get(admin, adminPath).body)[1]
	fm.take()
	return adminPath, pub
}

func TestPledgeEmail(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	admin := e.client()
	adminPath, pub := openNotifiedEvent(t, e, fm, admin)
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}, "min": {"21"}})

	// A pledge: the organizer reads who brings what and how the list stands.
	marie := e.client()
	r := pledge(e, marie, pub, id, "Marie Dupont", 2)
	expect(t, r, http.StatusSeeOther)
	msgs := fm.take()
	if len(msgs) != 1 {
		t.Fatalf("%d messages after a pledge, want 1", len(msgs))
	}
	m := msgs[0]
	if m.To != "orga@example.org" || m.Subject != "Inscription : Marie Dupont – Gâteaux sucrés (à apporter)" {
		t.Errorf("pledge mail: %+v", m)
	}
	for _, want := range []string{
		"Marie Dupont apporte 2 gâteaux pour « Gâteaux sucrés »", "Téléphone : 07 11 22 33 44",
		"2 gâteaux promis sur 21 : il en manque 19", "https://jeviensaider.example" + adminPath,
		"Pour ne plus recevoir ces e-mails",
	} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("pledge mail lacks %q:\n%s", want, m.Body)
		}
	}

	// A change of quantity sends the same mail with the new number.
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 3), http.StatusSeeOther)
	msgs = fm.take()
	if len(msgs) != 1 || msgs[0].Subject != m.Subject ||
		!strings.Contains(msgs[0].Body, "Marie Dupont apporte 3 gâteaux") || !strings.Contains(msgs[0].Body, "3 gâteaux promis sur 21 : il en manque 18") {
		t.Fatalf("change mail: %+v", msgs)
	}

	// The same quantity again changes nothing, so it sends no mail.
	expect(t, pledge(e, marie, pub, id, "Marie Dupont", 3), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("mail after saving the same quantity: %+v", msgs)
	}

	// A cancel: the subject starts with "Désinscription" and the body has no quantity.
	meLink := strings.TrimSuffix(r.location, "?welcome=1")
	pledgeID := pledgeIDRe.FindStringSubmatch(e.get(marie, meLink).body)[1]
	expect(t, e.post(marie, meLink+"/pledges/"+pledgeID+"/delete", nil), http.StatusSeeOther)
	msgs = fm.take()
	if len(msgs) != 1 || msgs[0].Subject != "Désinscription : Marie Dupont – Gâteaux sucrés (à apporter)" {
		t.Fatalf("cancel mail: %+v", msgs)
	}
	for _, want := range []string{"Marie Dupont s'est désinscrit·e de « Gâteaux sucrés »", "Rien de promis pour l'instant : il en manque 21", "https://jeviensaider.example" + adminPath} {
		if !strings.Contains(msgs[0].Body, want) {
			t.Errorf("cancel mail lacks %q:\n%s", want, msgs[0].Body)
		}
	}
	for _, bad := range []string{"apporte", "gâteaux pour", "Téléphone"} {
		if strings.Contains(msgs[0].Body, bad) {
			t.Errorf("cancel mail has %q:\n%s", bad, msgs[0].Body)
		}
	}

	// The organizer turns notifications off: no more messages.
	expect(t, e.post(admin, adminPath+"/event", url.Values{"title": {"Kermesse"}, "date": {"2026-06-20"}, "admin_email": {"orga@example.org"}}), http.StatusSeeOther)
	expect(t, pledge(e, e.client(), pub, id, "Paul Martin", 1), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("messages sent although notifications are off: %+v", msgs)
	}
}

// The organizer already knows about a pledge that they add by hand, and a
// refused pledge changes nothing. Neither sends a mail.
func TestPledgeEmailNotSent(t *testing.T) {
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example"})
	admin := e.client()
	adminPath, pub := openNotifiedEvent(t, e, fm, admin)
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "max": {"2"}})

	// The hand add fills the list.
	expect(t, addPledge(e, admin, adminPath, id, "Marie Dupont", 2), http.StatusSeeOther)
	expect(t, e.get(admin, adminPath), 200, "Marie Dupont a été inscrit·e.")
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("the hand add sent mail: %+v", msgs)
	}

	// A visitor meets a full list.
	expect(t, pledge(e, e.client(), pub, id, "Paul Martin", 1), http.StatusConflict, "Cette liste est complète.")
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("a refused pledge sent mail: %+v", msgs)
	}

	// A visitor meets a closed event.
	expect(t, e.post(admin, adminPath+"/status", url.Values{"status": {"closed"}}), http.StatusSeeOther)
	expect(t, pledge(e, e.client(), pub, id, "Léa Petit", 1), http.StatusConflict)
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("a pledge on a closed event sent mail: %+v", msgs)
	}
}

// Slot sign-ups and pledges use the same hourly limit. The organizer reads
// one "notifications en pause" mail, then nothing.
func TestPledgeEmailSharesHourlyLimit(t *testing.T) {
	const limit = 3
	fm := &fakeMailer{}
	e := newEnv(t, Config{Mailer: fm, MailEnabled: true, BaseURL: "https://jeviensaider.example", MailLimits: &MailLimits{SignupsPerEventPerHour: limit}})
	admin := e.client()
	adminPath, pub, slot := openEventWithSlot(t, e, admin, url.Values{"title": {"Fête"}, "admin_email": {"orga@example.org"}, "notify": {"1"}})
	id := addList(t, e, admin, adminPath, url.Values{"name": {"Gâteaux sucrés"}, "unit": {"gâteaux"}})
	fm.take()

	// Two sign-ups and one pledge use the whole limit.
	e.signUp(e.client(), pub, slot, "Anne")
	expect(t, pledge(e, e.client(), pub, id, "Bruno", 1), http.StatusSeeOther)
	e.signUp(e.client(), pub, slot, "Chloé")
	if msgs := fm.take(); len(msgs) != limit {
		t.Fatalf("%d messages within the limit, want %d: %+v", len(msgs), limit, msgs)
	}

	// The next pledge goes over the limit: one pause notice.
	expect(t, pledge(e, e.client(), pub, id, "David", 1), http.StatusSeeOther)
	msgs := fm.take()
	if len(msgs) != 1 || !strings.Contains(msgs[0].Subject, "notifications en pause") || !strings.Contains(msgs[0].Body, adminPath) {
		t.Fatalf("pause notice: %+v", msgs)
	}

	// After that, a sign-up and a pledge send nothing.
	e.signUp(e.client(), pub, slot, "Éva")
	expect(t, pledge(e, e.client(), pub, id, "Farid", 1), http.StatusSeeOther)
	if msgs := fm.take(); len(msgs) != 0 {
		t.Fatalf("messages over the limit: %+v", msgs)
	}
}
