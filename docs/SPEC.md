# Je viens aider — specification

This document describes what the application does and how the code is
organised. It is the reference for reviews: the code should do what is
written here, in the place written here, and nothing much more.

## 1. Purpose

An association, a school, a club or a group of neighbours organises an event
(*fête de l'école*, *fest-noz*, *vide-grenier*, *marché de Noël*). It needs
volunteers on **stands** (bar, crêpes, games…),
each stand split into **time slots** that need a minimum number of people.

The app lets organisers publish the planning, and volunteers sign up for slots
in a few seconds, without creating an account. It is a small, self-hosted
alternative to Doodle or Framadate, specialised for this job.

Everything visible to users is in French. Code, comments and docs are in
English.

## 2. Users and how they are identified

There are no accounts and no passwords for users. Identity is carried by
secret links (random 256-bit tokens, base64url).

| Role | Identified by | Can |
|---|---|---|
| **Visitor** | nothing | see an open event's planning, sign up |
| **Volunteer** | personal link `/e/<slug>/me/<token>`, also kept in a cookie (`v<eventID>`, path `/e/<slug>`, HttpOnly, SameSite=Lax, 1 year) | see and cancel their slots and pledges, change their name and phone, replace their link |
| **Organiser** | admin link `/a/<token>` | everything about one event |
| **Super-admin** | shell access to the server | list, show and delete any event (`jeviensaider events`) |
| **Creator** | the creation password, if `JEVIENSAIDER_CREATE_PASSWORD` is set | create (or duplicate) events |

## 3. Domain

- **Event**: title, description, location, optional date, status, organiser
  e-mail (mandatory), "notify me of sign-ups" flag, public slug, admin token.
  - Status: `draft` (planning not public, no sign-ups), `open` (sign-ups
    allowed), `closed` (read-only for volunteers; the organiser can still add
    people).
- **Stand**: belongs to an event; name, description, optional colour
  (`#rrggbb`; empty means no colour), display position, and `fill_row` (see
  the planning table: the colour tints the whole row, not only its stripe).
  `fill_row` has no effect on a stand that has no valid colour, and the forms
  clear it then. At most **50** stands per event.
- **Slot**: belongs to a stand; start and end (local wall-clock time, no
  timezone), minimum people (≥ 1), optional maximum. At most **500** slots
  per event.
- **Moment**: belongs to an event. It has a title of 1 to 80 characters, and a
  start and an end (local wall-clock time, no timezone). It names a part of
  the day, such as "Spectacle des enfants". Nobody signs up for a moment and
  it blocks nothing. An event has at most **30** moments, and one for each
  exact start and end. A moment has an optional color (`#rrggbb`). An empty
  color means no color. A moment also has `fill`: when it is set, the color
  tints the whole column of the moment, not only its stripe. `fill` has no effect
  on a moment that has no valid color.
- **Volunteer**: belongs to an event; name (required), phone (optional), edit
  token. No e-mail is ever asked or stored.
- **Sign-up**: a volunteer on a slot, with an optional comment. One per
  (slot, volunteer).
- **Pledge list**: belongs to an event. It is a list of things that people
  promise to bring, such as cakes. It has no time slot. It has a name
  (required) and five optional values: a description, a unit, a drop-off
  time, a minimum and a maximum. At most **10** pledge lists per event.
  - The unit is free text in the plural, up to 30 characters, such as
    "gâteaux". The app writes it only when the count is above 1, because the
    unit is in the plural: "14 gâteaux", but "1".
  - The drop-off time is the time of day when people bring the items. It is
    `HH:MM` on the event date. It has no end time and no "days before". The
    app stores only the time of day, so the time follows the event date.
  - The minimum and the maximum are whole numbers from 1 to 1000. They count
    items, not people.
- **Pledge**: the promise of one volunteer to bring items to one pledge list.
  It holds a quantity from 1 to 20. One per (list, volunteer). At most
  **200** pledges per list.

Derived notions (computed, not stored):

- **Fill status** of a slot: `empty` (0 people), `partial` (below minimum),
  `ok` (minimum reached), `full` (maximum reached).
- **Missing**: people still needed to reach the minimum, per slot, stand and
  event.
- **Overlap**: two slots whose time ranges intersect.
- **Public name**: public pages show the first word of the name, then the
  initial of each other word. "Marie Dupont" becomes "Marie D." and "Julien Le
  Jeune" becomes "Julien LJ.".
- **Total** of a pledge list: the sum of its quantities.
- **Items missing**: the minimum minus the total, or 0 when there is no
  minimum or it is reached.
- **Items left**: the maximum minus the total.
- **Full**: a list is full when the total reaches the maximum.
- **Pledge status**: one sentence that says where a list stands, with the
  unit when there is one. Examples: "Rien de promis pour l'instant", "14
  gâteaux promis sur 21 : il en manque 7, encore 16 places", "Complet : 30
  gâteaux promis".

Pledge rules:

- One row per volunteer and list. A second pledge of the same volunteer on the
  same list changes the quantity.
- The minimum never refuses anyone. A list with no maximum refuses nobody,
  until it has 200 pledges.
- With a maximum, the app refuses a pledge or an increase that passes it. The
  message gives the items left. Lowering a quantity is always allowed.
- The organizer cannot set the maximum below the total or below the minimum.
- Event status works as for slots. Only the organizer can add a pledge to a
  draft or closed event.

## 4. Features

### 4.1 Creating an event (home page)

- Form: title (required), date, location, description, organiser e-mail
  (required, strict format), "e-mail me at each sign-up" (checked by
  default), and the creation password when configured.
- On success the organiser lands on the admin page. The admin link is also
  e-mailed to them (when e-mail is enabled and within the limits, §6).

### 4.2 Public event page `/e/<slug>`

- Header: title, date, location, description.
- Draft: only a "not open yet" notice. Closed: a "closed" notice.
- **"Tous les créneaux"** (the overview):
  - Progress: "Il reste N places" (or "Tous les créneaux sont couverts,
    merci !"), a bar and "N places prises sur M".
  - "Là où on a le plus besoin de vous": up to three slots the visitor can
    take that are under their minimum, the emptiest first, then the earliest.
    Hidden under the filter.
  - Wide screens: a table, one row per stand, one column per distinct time
    range. Each row header shows a stripe of the stand's colour on its left
    side (none for a stand with no colour), the name and the description
    cut after two lines; a "Voir plus" link (only when the text is cut) opens
    the full description.
  - Phones: a list, one group per time range, one card per slot.
  - A slot says in words where it stands, never as a bare fraction: "Il
    reste N places" (under the minimum, orange tint), "Équipe
    suffisante · encore N places" or "Équipe suffisante · renfort possible"
    (minimum reached,
    flat grey), "Complet" (maximum reached, flat grey). The visitor's own
    slots have an orange border. It lists the public
    names and, on the visitor's own slots, "Vous y êtes" ("· ⚠ en même temps
    qu'un autre créneau" if two of their slots overlap). A slot one can take
    is a link to the sign-up form ("Je m'inscris"), on the right of the card
    on phones. A legend explains the colours: "Places à prendre" and "Équipe
    suffisante ou complet".
  - Switch "Seulement les places à prendre" (`?todo=1`) hides slots that
    reached their minimum.
  - Moments (section 3): a column whose time range is the exact range of a
    moment shows the title on its own line above the time range. A moment
    with no slot at its exact time range gets a column of its own, tinted
    from top to bottom, with empty cells that say "Pas de créneau" to screen
    readers. The legend does not name these columns: their title says what
    they are. On phones, a group shows the title in its heading, and a group
    with no slot says "Pas d'inscription à ce moment". The filter above hides
    these groups on phones and keeps every column in the table. An event
    with moments and no slot (no stand, or stands without slots) shows the
    list on every screen size, not "Aucun créneau n'a encore été proposé". A
    draft event shows no moment. A
    moment never appears in "Mes créneaux", the PDF, the CSV export or the
    calendar file.
  - The color of a stand or of a moment shows as a stripe, and never as a
    dot. A stand has a vertical stripe on the left side of the row header of
    the table, of each slot card in the phone list, of the cards of "Mes
    créneaux", of the titles of the sign-up window and of the stand page, and
    of the list of stands and the head of each stand in the admin page. A
    moment is a column, so it has a horizontal stripe on top of its column
    header, of its group in the phone list and of its entry in the admin
    list. Without a color, there is no stripe.
  - Without `fill`, a column with no slot has the neutral tint, and a column
    that has slots is not tinted. With `fill`, the column is tinted with the
    color of the moment, mixed with the page background so that it stays
    readable in dark mode. This is true for a column with slots too, and for
    its group in the phone list. A color that is not `#rrggbb` gives no tint
    and no stripe, even with `fill`. Two columns of moments that sit side by
    side keep a gap between them.
  - A stand with `fill_row` set has its whole row tinted with its colour,
    mixed with the page background, in the table. The setting has no effect
    in the phone list: a card keeps its own background, because that
    background says how the slot stands (open, enough people, full), and the
    stripe shows the colour. A moment that tints a column wins over the stand
    in the cells that they share.
- **"À apporter"**, after "Tous les créneaux", when the event has at least
  one pledge list. It has one card per list:
  - A card shows the name, the description, the drop-off time ("Dépôt à
    8h30"), the pledge status and the public names. A quantity above 1 shows
    after the name ("Marie D. (2)").
  - There is no progress bar, because the bar counts places. The page shows no
    total across lists. Lists add nothing to the number of places taken or
    needed.
  - "Je m'en charge" opens the pledge form (see 4.3). A volunteer who has
    a pledge sees "Vous en apportez N" and "Changer" instead. A full list shows
    "Complet" to everyone else.
  - A closed event shows the cards with no button. A draft event shows no
    list.
  - Under the filter (`?todo=1`), a list shows only while it still wants
    items. It is not full, and it has no minimum or is under it.
  - An event with lists and no slots shows only this section. The page hides
    "Tous les créneaux" and "Aucun créneau n'a encore été proposé.", and the
    filter switch moves to "À apporter".
- **"Mes créneaux"** (`/e/<slug>/me/<token>`), a tab next to "Tous les
  créneaux" once the browser knows the volunteer, and a link in the header:
  - After a first sign-up (`?welcome=1`): "Merci <nom> !".
  - The volunteer's slots as cards (day, time, stand, who else is there,
    overlap warning), with a summary (number of slots, total time, stands)
    and "Je ne peux plus venir" on each (while the event is open).
  - "Ce que j'apporte": one card per pledge of the volunteer, with the list
    name, "Vous en apportez N" and the drop-off time. While the event is open,
    "Changer" opens the pledge form and "Je ne peux plus en apporter" cancels
    the whole pledge (`POST /e/<slug>/me/<token>/pledges/<id>/delete`). The
    route looks only at the pledges of this volunteer, so the pledge of
    another volunteer gives 404. The count on the tab adds slots and pledges. A
    volunteer with pledges and no slots sees no slot section.
  - "Ajouter à mon agenda" (`/e/<slug>/me/<token>/agenda.ics`) shows when
    the volunteer has at least one slot or one calendar entry for a pledge
    (see below). It gives an iCalendar file with one entry per slot (stand and
    event names, the stand description, the location and the public event
    link) and two reminders, one day and one hour before. Times are written
    without a time zone, like the stored times. Each slot entry has a stable
    UID (event, slot, volunteer), so a second import updates the entries that
    still exist. A cancelled or deleted slot stays in the calendar app. Not
    available for a draft event (404), like the sign-up form; a closed event
    still gives its file.
    - A pledge gives an entry only when its list has a drop-off time and the
      event has a date. The entry starts at the drop-off time and lasts 30
      minutes. It has the same location, public link and two reminders.
    - The summary is "À apporter : <list> (<event>)". The description is the
      list description.
    - The UID is `<slug>-p<listID>-<volunteerID>@jeviensaider`.
    - Like the slot entries, it holds no name, phone or token.
  - "Encore un peu de temps ?": the slot that needs people most, to take too.
  - "Mes coordonnées": change name and phone (while the event is open).
  - Folded "Retrouver mes créneaux sur un autre appareil": the personal link
    with "Copier", "J'ai partagé mon lien par erreur : en créer un nouveau"
    (the old link stops working) and "Vous n'êtes pas <nom> ? Oublier sur
    cet appareil" (forgets this browser's cookie).
- **Stand view** `/e/<slug>/stands/<id>`: name and full description, as a
  dialog (htmx) or a page; a "Fermer" button.
- Pages follow the device's light or dark setting.

### 4.3 Signing up `/e/<slug>/slots/<id>/signup`

- Dialog (htmx) or page with: stand, day, time, where the slot stands ("Il
  reste N places"), who is already there, the stand's full description, then
  name, optional phone, optional comment ("Un mot pour l'équipe").
  Pre-filled for a known volunteer.
- Warns (does not block) when the slot overlaps another of the volunteer's
  slots; the button then reads "M'inscrire quand même".
- Refused: event not open, slot full, already signed up on that slot.
- The capacity check and the insert happen in one transaction, so two people
  cannot take the last place.
- First sign-up: creates the volunteer, sets the cookie, shows the personal
  link ("welcome"). Later sign-ups reuse the volunteer.
- If the browser remembers someone with a **different name**, a new volunteer
  is created instead (shared computer, or a personal link shared by mistake).
  Names are compared ignoring case and spacing.
- Honeypot field against bots.

Pledging to a pledge list uses `GET` and `POST /e/<slug>/lists/<id>/signup`.
It works like a slot sign-up, with these differences:

- The dialog (htmx) or page shows the list name, the description, the
  drop-off time, the pledge status and who already pledged. It asks for a
  quantity ("Combien en apportez-vous ?"), a name and an optional phone. It
  has no comment field.
- The form pre-fills the name and phone of a known volunteer. If that
  volunteer already has a pledge on this list, it also pre-fills the
  quantity, and the button reads "Enregistrer". Otherwise the quantity is 1
  and the button reads "Je m'en charge".
- The quantity is a whole number from 1 to 20. It stops at the items left
  plus the quantity of the volunteer.
- A second pledge on the same list changes the quantity. It does not add a
  row.
- A full list shows "Complet. Merci !" and no form. A volunteer who has a
  pledge on it can still lower the quantity.
- Refused: event not open, quantity out of range, total above the maximum,
  and 200 pledges on the list. For a maximum, the message gives the most that
  the person can hold: the items left plus what they hold now ("Vous ne
  pouvez en apporter que 3 sur cette liste."). A refused pledge creates no
  volunteer.
- The app compares the total with the maximum and saves the pledge in one
  transaction, so two people cannot take the last items.
- A draft event gives 404, as for slots. A list has no end time, so it never
  clashes with a slot, and the form shows no overlap warning.
- A pledge from a new volunteer creates the volunteer, sets the cookie and
  shows the personal link ("welcome"). Otherwise, the event page shows the
  message "C'est noté ! Vous apportez 3 pour « Gâteaux sucrés ». Merci !".
  The rule about a different name and the honeypot field are the same as for
  slots.

### 4.4 Organiser page `/a/<token>`

- One column that works on a phone (the badge "Organisation" in the header).
- "Inscriptions": Brouillon / Ouvertes / Closes, with what each means.
- Progress: places still needed, places taken, number of volunteers. An
  event with pledge lists and no slots does not show it.
- "Les deux liens de l'événement": the secret organiser link and the public
  link (both with "Copier"), and a message ready to share ("Partager le
  message" opens the phone's share sheet, or copies it).
- Event settings ("Modifier l'événement"), including the organiser e-mail,
  the notification flag and the number of days before the event is deleted
  (see 4.6).
- The page is cut into sections ("Les deux liens de l'événement", "Déroulé
  de la journée", "Stands", "Outils"), each with a rule above its title. The
  title of "Déroulé de la journée" and of "Stands" shows how many there are.
  Each form that adds or changes something opens from a button, in a framed
  panel with its own title, its save button and "Annuler" (which puts the
  form back as it was and closes the panel; without the script, the button
  that opened the panel closes it). A delete button sits at the bottom of the
  panel, below a rule.
- "Déroulé de la journée" (above "Stands"): the moments of the event (see
  section 3), in time order. A "Nouveau moment" button above the list opens
  the form that adds one. Each moment shows its time range and title, and has
  a "Modifier" panel with day, start, end, title and color (Jour, De, À,
  Titre, Couleur), the box "Colorer toute la colonne du planning" (`fill`), a save button and a
  delete button that asks for confirmation. The add form has the same fields. In the add form, the color picker
  starts with the next color of the stand palette. In the "Modifier" panel,
  it shows the color of the moment, or a neutral color if the moment has none.
  The box "Sans couleur" gives a moment no color, and so does a value that is
  not `#rrggbb`; a moment with no color never has `fill`. If the organizer changes the picker, the new color wins, even
  when the box is ticked. A hidden field `color_was` holds the color that the
  picker showed, so the server can see the change. A small script keeps the boxes in step
  on the screen, in the forms of a stand too: picking a color unticks "Sans
  couleur", ticking "Sans couleur" unticks the fill box, and ticking the fill
  box unticks "Sans couleur". The server does not rely on it. The day
  starts as the event's date, else the day of the first slot, else today. Errors use the
  usual red message: end not after start, empty title, a moment already at
  that time, more than 30 moments. The routes are `POST /a/<token>/moments`,
  `POST /a/<token>/moments/<id>` and `POST /a/<token>/moments/<id>/delete`.
  Each route acts only on the moments of that event.
- Stands: a "Nouveau stand" button (its panel is open while the event has
  no stand), then each stand as a card that folds. Its head shows the name,
  the stripe of its colour, the number of slots, the places taken out of the
  places needed, and its status ("Il reste N places", "Complet", "Aucun
  créneau"). A click on the head opens the card, and another click closes
  it. The cards start closed, except the stand that the organiser just
  changed: every action on a stand or on one of its slots comes back to
  `/a/<token>?stand=<id>#stand-<id>`, with that card open. An open card shows
  the description, two buttons that open the panel that edits or deletes the
  stand ("Modifier") and the slot generator ("Ajouter des créneaux", or
  "Créer les créneaux" and open while the stand has no slot), only one of the
  two open at a time, the buttons that move the stand up and down, and then
  the slots of the stand. The colour of a stand is optional and works
  like the colour of a moment: the add form starts with the next colour of
  the stand palette, the "Modifier" panel shows the stand's colour (or the
  default blue if it has none), and the box "Sans couleur", with the hidden
  field `color_was`, follows the same rules. A stand with no colour has no
  stripe, and its pages in the PDF have a light grey band.
- Slots: a generator ("from 14:00 to 17:00, every 1 h, 2 people" → three
  slots) that previews, as you type, the slots it will create; per-slot edit
  (times, min, max, its own "Enregistrer") and delete.
- Volunteers, on each slot: full names and phones (with a call button),
  "Inscrire quelqu'un" by hand (allowed whatever the status, capacity still
  applies), "Retirer".
- "À apporter", after the stands: the pledge lists. It looks like the stands:
  the title shows the number of lists. The section has these parts:
  - With two or more lists, it starts with an overall total, such as "Au
    total : 17 promis sur 21". The total has no unit, because lists have
    different units. The "sur" part shows only when every list has a minimum.
    No other page shows a total across lists.
  - A "Nouvelle liste" button, whose panel is open while the event has no
    list (`POST /a/<token>/lists`). The form asks for a name (required) and an
    optional description, drop-off time, unit, minimum and maximum.
  - Each list is a card that folds. Its head shows the name, the drop-off time
    and the pledge status. The cards start closed, except the list that the
    organizer just changed: every action on a list comes back to
    `/a/<token>?list=<id>#list-<id>`, with that card open. A value that is not
    a list opens nothing.
  - An open card shows the description, the buttons "Modifier" and "Inscrire
    quelqu'un" (only one of the two panels is open at a time), and one line per
    pledge. A line shows the full name, the phone (with a call button), the
    quantity with the unit and "Retirer" (`POST /a/<token>/pledges/<id>/delete`).
  - The panel "Modifier" has the same fields as the new list form
    (`POST /a/<token>/lists/<id>`) and a button "Supprimer cette liste"
    (`POST /a/<token>/lists/<id>/delete`). The deletion removes the list with
    its pledges. It asks first and gives the number of pledges.
  - "Inscrire quelqu'un" adds a pledge by hand (`POST /a/<token>/pledges`).
    It works whatever the status of the event, and it sends no e-mail. The
    maximum still applies. The button is hidden when the list is full. A name
    that the event already has (same name, ignoring case and spacing) is the
    same person, so a second pledge changes the quantity of the first.
  - The organizer cannot set the maximum below the total or the minimum. For a
    maximum below the total, the message names the total: "Il y a déjà 14
    promis : le maximum doit être au moins 14." For a maximum below the
    minimum, the message is "Le maximum doit être vide ou supérieur ou égal
    au minimum."
  - Lists show oldest first. They have no reorder buttons.
- "Qui est où, et quand" (wide screens): the whole planning with full names,
  and the titles of the moments in its header. It shows only when the event
  has at least one slot.
- CSV export (`;`, UTF-8 BOM for Excel, one line per sign-up and one per
  missing person, formula injection neutralised). After the stands, it has
  one line per pledge of each list. "Stand" is "À apporter · <list>". "Date"
  is the event date, empty when the event has no date. "Début" is the
  drop-off time, empty when the list has none. "Fin" is empty. The last
  column, "Quantité", holds the quantity as a number with no unit. It is
  empty on the slot lines. A list under its minimum gets one more line, "(il
  en manque N)".
- Planning as a PDF to print (`/a/<token>/planning.pdf`): one A4 page per
  stand, with a line per volunteer and an empty line per missing person.
  Lines and text shrink (down to a readable minimum) to keep a stand on one
  page; a longer stand continues on more pages, without cutting a slot when
  it fits on one page. After the stands, each pledge list gets its own page,
  and more pages when it is long. A page shows the name, the description, the
  drop-off time and the pledge status. Then it has one line per pledge, with
  the quantity, the name and the phone, and no empty line.
- Erase personal data: the volunteers, with their sign-ups and pledges (keeps
  stands, slots, moments and pledge lists).
- Duplicate the event (stands, slots, moments and pledge lists, without
  volunteers or pledges). It asks for the creation password when one is
  configured. With a new date, slots and moments move by the same number of
  days. A list keeps its drop-off time of day, minimum, maximum and unit.
- Delete the event, with its pledge lists and pledges.
- Shows when the event will be deleted automatically.
- Sends the event's links to the organiser again, when e-mail is set up.

### 4.5 E-mails (to organisers only)

- On creation and duplication: the admin and public links. The organiser can
  have them sent again from the admin page ("Renvoyer les liens par e-mail"),
  to the current organiser address; this counts in the same limits.
- On each sign-up and cancellation, if the organiser asked: who, which slot,
  phone and comment, and what is still missing.
- On each pledge, change of quantity and cancellation of a pledge, if the
  organizer asked. The e-mail gives the name, the quantity, the list, the
  phone and the pledge status. The subject looks like `Inscription : Marie
  Dupont – Gâteaux sucrés (à apporter)`. A cancellation e-mail has no
  quantity and no phone, and its subject starts with "Désinscription".
  - The quantity uses the unit of the list: "apporte 3 gâteaux pour « Gâteaux
    sucrés »".
  - A pledge e-mail and a slot sign-up e-mail count in the same hourly limit
    for each event (§6). So slots and lists count together. The limit for the
    whole site applies to both.
  - A pledge that the organizer adds by hand sends no e-mail. A volunteer who
    saves the same quantity again sends none either.
- Sent in the background through SMTP (STARTTLS required by default, or
  implicit TLS; `none` only towards a loopback or private address). Temporary
  failures are retried (30 s, 2 min, 10 min) without blocking other mails;
  permanent (5xx) failures are not retried.
- Without SMTP configured, messages are only logged (recipient and subject,
  never the body, which holds secret links).
- Links in e-mails always come from `JEVIENSAIDER_BASE_URL` (required when SMTP
  is set), never from the request. Links shown on pages fall back to the
  request's host when it is not set.

### 4.6 Automatic deletion

- Every hour, events whose last day (latest of: date, last slot, creation)
  is more than their number of days ago are deleted with everything in them.
  The number is the one the organiser chose, or `JEVIENSAIDER_RETENTION_DAYS`
  (default 60) when they chose none. `0`/`off` for `JEVIENSAIDER_RETENTION_DAYS`
  keeps events forever, whatever number an event stores. Then the field is
  hidden.
- Pledge lists do not move the last day, because they have no end time. The
  deletion removes the pledge lists and pledges with the rest.
- The organiser sets the number when they create or edit the event. The field
  is optional: empty (the default shown as a placeholder) stores nothing, so
  the event follows `JEVIENSAIDER_RETENTION_DAYS`, even when it changes. Otherwise
  it is a whole number from 1 to `JEVIENSAIDER_RETENTION_MAX_DAYS` (0 or unset:
  180, or `JEVIENSAIDER_RETENTION_DAYS` if that is longer). The server refuses a
  higher number, and the database refuses 0 and anything above 3650 days (ten
  years, `store.CeilingRetentionDays`), so nobody can keep an event forever.
- The cleanup cuts a stored number at the maximum. If the operator lowers the
  maximum, events that chose more days are deleted after the new maximum.
- Events created before this feature store no number and follow
  `JEVIENSAIDER_RETENTION_DAYS`. A duplicate keeps the number of its source.
  The edit form shows the event's own number (the maximum if it stores more),
  or an empty field; saving it stores what it shows, and emptying it goes back
  to the default.
- If both values are set and `JEVIENSAIDER_RETENTION_DAYS` is above
  `JEVIENSAIDER_RETENTION_MAX_DAYS`, the program does not start.
- Dates are accepted up to two years ahead and, when deletion is on, back to
  max(365, maximum retention) days.

### 4.7 Command line

- `jeviensaider` — run the web server.
- `jeviensaider events list|show|delete` — super-admin tool on the database.
  - `show` prints a "Pledges" line: "N lists, N pledges, N items". Items is the
    sum of the quantities.
  - The `delete` preview adds the number of pledge lists and the number of
    pledges to the line of each event.
- `jeviensaider mail-test <address>` — send one test e-mail with the SMTP
  settings and print the server's error.
- `jeviensaider -healthcheck` — for container health checks.

## 5. Configuration

Flags and environment variables, documented in the README and
`.env.example`: listen address, database file, public URL, trust proxy
headers, retention (default and maximum), creation password, SMTP settings, "open creation"
opt-out, and the four e-mail limits. An empty variable counts as unset.
Invalid values stop the program at startup with a clear message; nothing
silently falls back to a default.

## 6. Security and privacy rules

- **Secrets**: admin and personal tokens are never logged (paths are
  redacted), never sent in Referer (`Referrer-Policy: same-origin`), and
  pages that show them are `Cache-Control: no-store`.
- **Calendar file**: the `.ics` file of a volunteer holds no token and no
  data about volunteers (no name, phone or comment). Text fields are escaped
  and control characters and line breaks are removed, so a text typed by an
  organiser cannot add a calendar property. The response is `no-store`.
- **CSRF**: cross-site POSTs are refused (`Sec-Fetch-Site`, else `Origin`).
- **XSS**: all output goes through templ, which escapes it; user text is never marked
  as trusted HTML. Strict CSP (`script-src 'self'`). The JavaScript never
  builds HTML from text. htmx `eval` is off.
- **Abuse limits**: 64 KB request bodies; POST rate limit per IP (per /64 for
  IPv6; behind a trusted proxy, the last `X-Forwarded-For` entry); 50 stands
  and 500 slots per event; bounded memory for every in-memory counter. An
  event has at most 10 pledge lists, and a pledge list has at most 200
  pledges.
- **E-mail abuse**: with SMTP on, the server refuses to start without a
  creation password (unless `JEVIENSAIDER_OPEN_CREATION=true`). Limits, all
  configurable: 3 mails with an event's links (creation, duplication or
  resend) per address per day, 30 per hour in total,
  20 sign-up or pledge notifications per event per hour (then one "paused"
  notice),
  300 mails per hour overall. No CR/LF in headers.
- **Privacy**: public pages show "Prénom N." only; phones are visible to the
  organiser and the volunteer only; `noindex`; personal data is erased on
  request and deleted with the event. On a pledge list, public pages also
  show the quantity next to the name.
- **HTTPS**: HSTS and `Secure` cookies when served over HTTPS.

## 7. Architecture

One Go binary, no CGO, all assets embedded. SQLite through
`modernc.org/sqlite`. Server-rendered HTML with templ components, plus htmx
for dialogs and partial updates; every feature also works without
JavaScript.

```
cmd/jeviensaider/    main.go: wiring, server lifecycle, background loops
                     config.go, smtp.go, retention.go, maillimits.go: reading
                     flags and environment
                     events.go, mailtest.go: the CLI subcommands
internal/model/      domain types and pure rules (Slot.Check, fill status,
                     statuses, public names); BuildPlanning turns stands +
                     slots + moments + sign-ups into a ready-to-render view
                     pledge.go: pledge lists and pledges, PledgeList.Check,
                     PledgeListView (total, items missing, items left)
internal/slotgen/    pure slot generator (start, end, step, min, max)
internal/store/      SQLite access: one file per table group (events,
                     stands, slots, moments, signups, pledge_lists, pledges),
                     cleanup.go for automatic deletion, retention.go for the
                     deletion policy (default and maximum days), embedded
                     migrations tracked with PRAGMA user_version
internal/mail/       SMTP sender, message formatting, background queue
internal/ical/       iCalendar writer: text escaping, line folding, reminders
internal/web/        HTTP handlers, middleware, templates, static files
  server.go          routes and middleware chain
  public.go          home, event page, sign-up, volunteer self-service
  admin.go           organiser pages
  forms.go           reading and validating forms; userMessage turns
                     errors into French sentences for users
  render.go          template loading and helpers, flash messages
  middleware.go      security headers, CSRF, body limit, rate limit, logs
  notify.go          organiser e-mails
  mailbudget.go      e-mail limits
  admin_pledges.go   organizer handlers for pledge lists and pledges, and the
                     overall total line. The section itself is in
                     admin_pledges.templ.
  pledges.go         pledge form and sign-up, cancel from the personal page,
                     the drop-off entries of the calendar file
  pledges.templ      the "À apporter" section of the event page, "Ce que
                     j'apporte", the pledge form
  pledge_view.go     wording of pledges: status text, unit, drop-off time,
                     names
  export.go          CSV export
  agenda.go          the personal calendar file (.ics) of a volunteer
  pdf.go             planning PDF (layout, then drawing with gopdf), with
                     the pledge list pages
  *.templ            pages (templ); *_templ.go is generated by `make generate` (not committed)
  view.go            helpers shared by the pages (slot wording, urgent slots)
  static/            CSS, JS, htmx, Atkinson Hyperlegible font (OFL)
deploy/              systemd unit, Caddy and nginx examples
```

Dependency direction: `cmd` → `web`, `store`, `mail` → `model`. `model`
imports nothing from the project, `slotgen` only `model`. `store` knows
nothing about HTTP; `web` holds no SQL. Errors from `store`, `model` and
`slotgen` are plain Go errors; only `web` writes French.

### Data

Tables `events`, `stands`, `slots`, `moments`, `volunteers`, `signups`,
`pledge_lists`, `pledges`, all with `ON DELETE CASCADE` from the event down. A
pledge also goes away with its list or its volunteer. Times are stored as
`YYYY-MM-DD HH:MM` local wall-clock text. A drop-off time is stored as `HH:MM`
text, or as an empty string. Migrations live in
`internal/store/migrations/NNN_name.sql` and run in order at startup.

### Request flow

`middleware chain → route → withEvent / withVolunteer / withAdmin (loads the
event, checks the token, sets no-store) → handler → store → model.BuildPlanning
→ template`. State changes answer with a 303 redirect and a flash message
(POST/redirect/GET). Validation errors on the sign-up form re-render the form.

## 8. Quality expectations

The code should stay small and readable by one person in an afternoon.

- Plain Go and the standard library first; a dependency needs a real reason.
- Short functions with one job; names that say what they do; a comment
  explaining *why* where the reason is not obvious.
- No layer that only forwards calls, no interface with a single
  implementation unless tests need it, no framework.
- Rules live in one place, not repeated across handlers: form reading and
  validation in `forms.go`, SQL in `store`, domain rules and everything a
  view needs to decide (can I sign up here? is this mine?) in `model`.
  Templates only display; they do not compute rules.
- Every feature and every security rule above has a test; tests read like
  the scenario they check.
- `gofmt`, `go vet`, `staticcheck` and `govulncheck` are clean in CI.

## 9. Non-goals

- User accounts, logins, roles beyond the secret links.
- E-mails or SMS to volunteers.
- Several organisations or tenants; each organisation runs its own instance.
- Translations: the interface is French only.
- A JavaScript front-end framework or a build step.
- A description of the food that a volunteer brings. A pledge holds a quantity
  only.
- An end time for a drop-off.
- Several structured drop-off times for one pledge list.
- A grid of empty boxes, as in a spreadsheet planning, where each volunteer
  writes a name to promise one item.
- Reordering of pledge lists.
- A link between a pledge list and a stand.
