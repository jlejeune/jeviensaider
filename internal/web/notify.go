package web

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"text/template"

	"jeviensaider/internal/mail"
	"jeviensaider/internal/model"
)

// Mailer queues e-mails for background delivery.
type Mailer interface {
	Enqueue(mail.Message)
}

// mailTemplates are plain text (text/template): the same helpers as the
// pages, no HTML escaping.
var mailTemplates = template.Must(template.New("mail").Funcs(mailFuncs()).Parse(`
{{define "links"}}Bonjour,

{{if .Resent -}}
Voici les liens de votre événement « {{.Event.Title}} »{{with .Event.Date}} du {{date .}}{{end}} sur Je viens aider, envoyés depuis sa page d'administration.
{{- else -}}
Votre événement « {{.Event.Title}} »{{with .Event.Date}} du {{date .}}{{end}} a bien été créé sur Je viens aider.
{{- end}}

Lien d'administration, à garder précieusement :
{{.AdminURL}}

C'est la seule clé pour gérer l'événement : conservez ce message et ne partagez ce lien qu'avec les autres organisateurs.
{{if .Resent}}
Lien public, à partager avec les bénévoles :
{{.PublicURL}}
{{else}}
Prochaines étapes :
  1. Ajoutez vos stands et leurs créneaux.
  2. Ouvrez les inscriptions : « Inscriptions », puis « Ouvertes ».
  3. Partagez le lien public avec les bénévoles :
     {{.PublicURL}}
{{end}}{{if .Event.NotifySignups}}
Vous recevrez un e-mail à chaque inscription ou désinscription. Vous pouvez les désactiver depuis la page d'administration, dans « Modifier l'événement ».
{{end}}
{{if .Resent}}Si vous n'organisez pas cet événement{{else}}Si vous n'avez pas créé cet événement{{end}}, ignorez simplement ce message.

-- 
Je viens aider
{{end}}

{{define "signup"}}Bonjour,

{{.Name}} {{if .Cancel}}s'est désinscrit·e de{{else}}s'est inscrit·e sur{{end}} « {{.Slot.Stand.Name}} », {{day .Slot.StartsAt}}, {{span .Slot.StartsAt .Slot.EndsAt}}.
{{if not .Cancel}}
Téléphone : {{or .Phone "non renseigné"}}{{with .Comment}}
Commentaire : {{.}}{{end}}
{{end}}
Ce créneau : {{.Slot.Count}}/{{.Slot.MinPeople}}{{if .Slot.Missing}}, il reste {{.Slot.Missing}} {{plural .Slot.Missing "place" "places"}}{{else}}, complet{{end}}.
L'événement : {{.P.Count}}/{{.P.Needed}} places couvertes{{if .P.Missing}}, il reste {{.P.Missing}} {{plural .P.Missing "place" "places"}}{{end}}.

Voir le planning et les coordonnées :
{{.AdminURL}}

Pour ne plus recevoir ces e-mails : page d'administration, « Modifier l'événement ».

-- 
Je viens aider
{{end}}

{{define "pledge"}}Bonjour,

{{.Name}} {{if .Cancel}}s'est désinscrit·e de « {{.List.Name}} »{{else}}apporte {{count .Quantity .List.Unit}} pour « {{.List.Name}} »{{end}}.
{{if not .Cancel}}
Téléphone : {{or .Phone "non renseigné"}}
{{end}}
{{.Status}}.

Voir le planning et les coordonnées :
{{.AdminURL}}

Pour ne plus recevoir ces e-mails : page d'administration, « Modifier l'événement ».

-- 
Je viens aider
{{end}}

{{define "paused"}}Bonjour,

Plus de {{.Limit}} inscriptions ou désinscriptions ont eu lieu en une heure sur votre événement. Pour ne pas encombrer votre boîte, les notifications sont en pause jusqu'à {{.Until}}.

Le planning à jour est toujours sur la page d'administration :
{{.AdminURL}}

-- 
Je viens aider
{{end}}
`))

func renderMail(name string, data any) string {
	var b bytes.Buffer
	if err := mailTemplates.ExecuteTemplate(&b, name, data); err != nil {
		return "" // templates are tested; never send a half-written message
	}
	return strings.TrimLeft(b.String(), "\n")
}

// sendMail queues m within the overall hourly limit and reports whether it
// was queued.
func (s *Server) sendMail(m mail.Message) bool {
	if s.cfg.Mailer == nil || m.To == "" || m.Body == "" {
		return false
	}
	if !s.mails.allowMail(s.now()) {
		s.log.Warn("mail not sent: hourly limit of the site reached", "limit", s.mails.limits.PerHour, "to", m.To, "subject", m.Subject)
		return false
	}
	s.cfg.Mailer.Enqueue(m)
	return true
}

// sendLinks e-mails the organiser the admin and public links of an event:
// when it is created (or duplicated), or again on request (resent). It
// reports whether the mail was queued. The address was typed by a visitor,
// so mails to one address and in total are limited.
func (s *Server) sendLinks(r *http.Request, ev *model.Event, resent bool) bool {
	if s.cfg.Mailer == nil || ev.AdminEmail == "" {
		return false
	}
	if ok, why := s.mails.allowCreated(ev.AdminEmail, s.now()); !ok {
		s.log.Warn("links mail not sent: "+why, "to", ev.AdminEmail)
		return false
	}
	subject := "Votre événement « " + ev.Title + " » est créé"
	if resent {
		subject = "Les liens de votre événement « " + ev.Title + " »"
	}
	return s.sendMail(mail.Message{
		To:      ev.AdminEmail,
		Subject: subject,
		Body: renderMail("links", map[string]any{
			"Event":     ev,
			"Resent":    resent,
			"AdminURL":  s.absURL(r, adminURL(ev)),
			"PublicURL": s.absURL(r, eventURL(ev)),
		}),
	})
}

// notifiesSignups says whether the organiser wants an e-mail for each
// sign-up and the server can send it.
func (s *Server) notifiesSignups(ev *model.Event) bool {
	return ev.NotifySignups && ev.AdminEmail != "" && s.cfg.Mailer != nil
}

// allowSignupNotice applies the hourly limit of the event to a sign-up
// e-mail, whether it is for a slot or a pledge list. It reports whether the
// e-mail can go out. When the limit is first exceeded, it sends the "paused"
// e-mail instead and reports false.
func (s *Server) allowSignupNotice(r *http.Request, ev *model.Event) bool {
	switch notice, until := s.mails.signupNotice(ev.ID, s.now()); notice {
	case noticeSkip:
		return false
	case noticePause:
		s.sendMail(mail.Message{
			To:      ev.AdminEmail,
			Subject: "Beaucoup d'inscriptions sur « " + ev.Title + " » : notifications en pause",
			Body: renderMail("paused", map[string]any{
				"Limit": s.mails.limits.SignupsPerEventPerHour, "Until": hm(until.Local()), "AdminURL": s.absURL(r, adminURL(ev)),
			}),
		})
		return false
	}
	return true
}

// noticeVerb starts the subject of a sign-up e-mail.
func noticeVerb(cancel bool) string {
	if cancel {
		return "Désinscription"
	}
	return "Inscription"
}

// notifySignup tells the organiser that someone signed up for, or cancelled,
// a slot, if they asked for these messages.
func (s *Server) notifySignup(ctx context.Context, r *http.Request, ev *model.Event, vol *model.Volunteer, slotID int64, comment string, cancel bool) {
	if !s.notifiesSignups(ev) {
		return
	}
	p, err := s.planning(ctx, ev, 0)
	if err != nil {
		s.log.Error("notify: load planning", "err", err)
		return
	}
	sv := p.Slot(slotID)
	if sv == nil {
		return
	}
	if !s.allowSignupNotice(r, ev) {
		return
	}
	s.sendMail(mail.Message{
		To:      ev.AdminEmail,
		Subject: noticeVerb(cancel) + " : " + vol.Name + " – " + sv.Stand.Name + ", " + timeRange(sv.StartsAt, sv.EndsAt),
		Body: renderMail("signup", map[string]any{
			"Name":     vol.Name,
			"Phone":    vol.Phone,
			"Comment":  comment,
			"Cancel":   cancel,
			"Slot":     sv,
			"P":        p,
			"AdminURL": s.absURL(r, adminURL(ev)),
		}),
	})
}

// notifyPledge tells the organiser that someone pledged items to a list,
// changed their quantity or cancelled, if they asked for these messages. It
// uses the same guard and the same hourly limit as notifySignup, so slots and
// lists count together. A cancel has no quantity: pass 0.
func (s *Server) notifyPledge(ctx context.Context, r *http.Request, ev *model.Event, vol *model.Volunteer, listID int64, quantity int, cancel bool) {
	if !s.notifiesSignups(ev) {
		return
	}
	p, err := s.planning(ctx, ev, 0)
	if err != nil {
		s.log.Error("notify: load planning", "err", err)
		return
	}
	l := p.PledgeListByID(listID)
	if l == nil {
		return
	}
	if !s.allowSignupNotice(r, ev) {
		return
	}
	s.sendMail(mail.Message{
		To:      ev.AdminEmail,
		Subject: noticeVerb(cancel) + " : " + vol.Name + " – " + l.Name + " (à apporter)",
		Body: renderMail("pledge", map[string]any{
			"Name":     vol.Name,
			"Phone":    vol.Phone,
			"Cancel":   cancel,
			"Quantity": quantity,
			"List":     l,
			"Status":   pledgeStatus(l),
			"AdminURL": s.absURL(r, adminURL(ev)),
		}),
	})
}
