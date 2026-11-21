# <img src="internal/web/static/logo.svg" alt="" width="40" height="40" align="top"> Je viens aider

[![build](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml/badge.svg?branch=master)](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml)

A small web app that helps associations, schools, clubs and other organisers
find volunteers for their events: *fête de l'école*, *fest-noz*,
*vide-grenier*, *marché de Noël*…

It works like a simpler Doodle or Framadate, organised by **stand** and
**time slot**. For example, *Pêche aux canards* from 14:00 to 17:00 with
2 people per hour gives 3 slots and 6 places to fill.

The interface is in French. The whole app is one Go binary, and its data lives
in a single SQLite file.

## Features

**For organisers**
- Create an event and get a **secret admin link**, with no account to create.
  The organiser's e-mail is required: the link is also sent there, and can be
  **sent again** from the admin page (a lost e-mail, or a new address).
- Get an **e-mail for each sign-up or cancellation** (who, which slot, what
  is still missing), which can be turned off per event.
- Add stands, with a description of what volunteers do there, then generate
  their slots in one step: start, end, slot length and number of people.
- Add the **moments of the day**, such as "Spectacle des enfants": a title, a
  start, an end and an optional color. Nobody signs up for a moment. It only tells everyone how
  the day runs.
- Add **lists to bring**, such as cakes, next to the stands. Each list can
  have a minimum, a maximum, a unit and a drop-off time on the day of the
  event. The minimum and the maximum count items, not people. The admin page,
  the CSV file, the PDF and the e-mails show who brings what.
- Follow progress on a dashboard that shows who is signed up, with their
  contact details, and how many places are still missing.
- Open and close sign-ups.
- Add a volunteer by hand.
- Export to CSV; the file opens directly in French Excel.
- Download the planning as a PDF to print and post on the day: one A4 page
  per stand, with empty lines to write in the missing names. Lines shrink to
  keep a stand on one page; a very long stand continues on the next pages.
- Duplicate an event to reuse it next year.
- Erase personal data after the event (GDPR). Whole events are also deleted
  automatically, 60 days after they end by default. The organiser can choose
  another number of days, up to a maximum that the server sets. The admin
  page shows the deletion date. A private instance can turn this off and keep
  its events forever.

**For volunteers**
- **"Tous les créneaux"**: the whole event as a table of stands × hours,
  shown as a list grouped by hour on phones. Each slot says in words where it
  stands ("Il reste 2 places", "Équipe suffisante · renfort possible",
  "Complet") and who is there; an orange tint means people are still
  needed. At the top: "Il reste N places", a progress bar, and the slots that
  need help most. A switch shows only the places still to take.
- The **moments of the day** as titles above the hours of the table, and in
  the headings of the phone list. A moment with no slot, such as a show, still
  has its own column.
- Each stand's description under its name, cut after two lines; "Voir plus"
  opens the whole text. The sign-up form shows it too.
- Sign up in a few seconds with just a name; a phone number is optional, and
  no e-mail is asked or stored. There is no account: the browser remembers
  them, and a personal link lets them manage their slots from another device.
  A link shared by mistake can be replaced with a new one.
- **"Mes créneaux"**: their own slots as cards (day, time, stand, who else
  is there), "Je ne peux plus venir" on each, and a warning when two of them
  are at the same time.
- In **"À apporter"**, promise to bring some items, such as 2 cakes, with a
  name and no account. This promise is a pledge. The number is from 1 to 20.
  Change it later, or cancel it, in **"Mes créneaux"**, where "Ce que
  j'apporte" shows each pledge.
- An **"Ajouter à mon agenda"** button that downloads their slots as an
  `.ics` file for any calendar app. The file also has the drop-off time of
  each pledge, when the list has a drop-off time and the event has a date.
  Each entry has a reminder one day and one hour before. The file does not
  contain the personal link, and it is not available while the event is a
  draft. Importing it again updates the entries that still exist and does
  not add copies. A slot or a pledge that the volunteer cancels, or that an
  organiser deletes, stays in the calendar app until the volunteer removes it
  there. The times have no time zone, so a calendar shows the same clock time
  as the event page.

## Quick start

Requires Go ≥ 1.26; with an older Go, the `go` command downloads the right
toolchain by itself. No C toolchain is needed: the SQLite driver is pure Go.

```sh
make run      # http://localhost:8080, database ./jeviensaider.db
make test     # unit + HTTP tests (make race: with the race detector)
make vet      # go vet
make fmt      # gofmt and templ fmt
make build    # ./jeviensaider, a static binary
make generate # generate the pages' Go code (the other targets do it first)
make backup   # hot copy of the database to backup-<date>.db
```

Pages are written with [templ](https://templ.guide). Their generated Go code
(`*_templ.go`) is not committed: run `make generate` (or
`go tool templ generate`) once after cloning and after editing a `.templ`
file, before a plain `go build` or `go test`. The `make` targets, CI and the
Dockerfile do it for you.

Open http://localhost:8080, create an event, and keep the admin link it gives
you.

Prebuilt binaries for `linux-amd64`, `linux-arm64` (Raspberry Pi 4/5) and
`linux-armv7` are attached to every
[build on `master`](https://github.com/arnaudmorin/jeviensaider/actions/workflows/build.yml)
as downloadable artifacts. They can also be built locally with
`make build-arm64` or `make build-armv7`.

## Configuration

| Flag | Environment variable | Default | Purpose |
|---|---|---|---|
| `-addr` | `JEVIENSAIDER_ADDR` | `:8080` | Listen address |
| `-db` | `JEVIENSAIDER_DB` | `jeviensaider.db` | SQLite file, created if missing |
| `-base-url` | `JEVIENSAIDER_BASE_URL` | *(from request)* | Public URL used in shareable links, e.g. `https://jeviensaider.fr` |
| `-trust-proxy` | `JEVIENSAIDER_TRUST_PROXY` | `false` | Trust `X-Forwarded-*` headers when behind Caddy or nginx |
| `-retention-days` | `JEVIENSAIDER_RETENTION_DAYS` | `60` | Delete events this many days after they end, unless the organiser chose another number; `0` or `off` keeps them forever |
| `-retention-max-days` | `JEVIENSAIDER_RETENTION_MAX_DAYS` | `0` (180) | Longest number of days that an organiser can choose for one event; `0` or empty means 180, or `JEVIENSAIDER_RETENTION_DAYS` if longer (see below) |
| — | `JEVIENSAIDER_CREATE_PASSWORD` | *(none)* | Password required to create an event |

On a public server, set `JEVIENSAIDER_CREATE_PASSWORD` so that only the
organisers can create events. It is read from the environment only, so it
never shows up in `ps` output. It is also asked for when an organiser
duplicates an event.

**E-mails.** The app e-mails organisers: the admin link when an event is
created or duplicated, or when they ask for it again, then, unless they turn
it off, one message per sign-up or cancellation. Volunteers never receive
e-mails. Configure an SMTP server
with these variables (environment only):

| Environment variable | Default | Purpose |
|---|---|---|
| `JEVIENSAIDER_SMTP_HOST` | *(none)* | SMTP server; empty disables sending (messages are only logged, without their content) |
| `JEVIENSAIDER_SMTP_PORT` | `587` | `587` (STARTTLS) or `465` (TLS) |
| `JEVIENSAIDER_SMTP_TLS` | `starttls`, or `tls` on 465 | `starttls`, `tls`, or `none` for a relay on this machine or a private network, without authentication |
| `JEVIENSAIDER_SMTP_USERNAME` / `JEVIENSAIDER_SMTP_PASSWORD` | *(none)* | Credentials, if the server requires them |
| `JEVIENSAIDER_SMTP_FROM` | `Je viens aider <noreply@…>` | Sender, by default on the domain of `JEVIENSAIDER_BASE_URL` |
| `JEVIENSAIDER_OPEN_CREATION` | `false` | Allow e-mails without `JEVIENSAIDER_CREATE_PASSWORD` (see below) |

With SMTP enabled, `JEVIENSAIDER_BASE_URL` is required: links in e-mails are
built from it, never from the request. The connection is always encrypted
(STARTTLS is required, not optional) unless `none` is chosen, and a password
is never sent over an unencrypted connection. `none` is refused unless the
SMTP server's address is a loopback or private one. Messages are sent in the
background. If the server is unavailable they are retried for about
15 minutes without holding up the others, and a message the server rejects
(an unknown address, for example) is not retried. Settings are checked at
startup.

Creating an event sends a mail to the address the visitor types. So with
e-mails enabled, the server refuses to start without
`JEVIENSAIDER_CREATE_PASSWORD`; otherwise anybody could use it to e-mail
anyone. On a private network, `JEVIENSAIDER_OPEN_CREATION=true` lifts this.
Mails are limited in any case. Each limit is a number, or `0` / `unlimited`
for no limit; an invalid value stops the server:

| Environment variable | Default | Limit |
|---|---|---|
| `JEVIENSAIDER_MAIL_CREATED_PER_ADDRESS_PER_DAY` | `3` | Mails with an event's links (creation, duplication, resend) to one address, per day |
| `JEVIENSAIDER_MAIL_CREATED_PER_HOUR` | `30` | Mails with an event's links to all addresses, per hour |
| `JEVIENSAIDER_MAIL_SIGNUPS_PER_EVENT_PER_HOUR` | `20` | Sign-up notifications for one event, per hour; past it the organiser gets one "notifications en pause" message |
| `JEVIENSAIDER_MAIL_PER_HOUR` | `300` | All mails, per hour |

The counters are kept in memory and start again from zero when the server
restarts.

To check the settings, send a test e-mail. It reads the same variables and
prints the SMTP server's error if sending fails:

```sh
jeviensaider mail-test you@example.org
docker compose exec app jeviensaider mail-test you@example.org   # with Docker
```

**Keeping events forever.** By default, events and their volunteers'
contact details are deleted 60 days after the event, which suits a public
instance. For a private instance that wants to keep its history, set
`JEVIENSAIDER_RETENTION_DAYS=0` (or `off`) in `.env`: nothing is deleted
automatically, the admin page no longer announces a deletion date, and old
events stay editable. Organisers can still delete an event or erase its
personal data from the admin page. An invalid value stops the server with an
error rather than falling back to 60 days.

**Choosing how long an event is kept.** The creation form and the "Modifier
l'événement" form of the admin page have a field for the number of days
before deletion. Left empty (it shows `JEVIENSAIDER_RETENTION_DAYS` as a hint),
the event follows the server default, even if you change it later. A number
from 1 to `JEVIENSAIDER_RETENTION_MAX_DAYS` fixes the event's own period. The
server refuses a higher number, and the database refuses 0 and anything above
3650 days (ten years). So an organiser cannot keep an event forever. A
duplicate of an event keeps the number of its source. Events that existed
before this feature store no number, so they follow
`JEVIENSAIDER_RETENTION_DAYS`.

If you do not set `JEVIENSAIDER_RETENTION_MAX_DAYS`, the maximum is 180 days.
If `JEVIENSAIDER_RETENTION_DAYS` is longer than 180 days, the maximum is equal
to it. So a server that already runs with a long retention still starts after an
upgrade. If you set both values and the default is above the maximum, the
server stops at startup with an error. The cleanup applies the maximum to every
event. If you lower it, the events that chose more days are deleted after the
new maximum. When `JEVIENSAIDER_RETENTION_DAYS` is `0` or `off`, the field is
hidden and the maximum is not used.

## Super-admin command line

The same binary includes a tool to manage every event directly in the
database, for example to delete one before it expires. Anyone who can run it
on the server can see and delete all events, so there is no password.

```sh
jeviensaider events list                  # all events, with counts and expiry date
jeviensaider events show <event>          # details, public and admin links
jeviensaider events delete <event>...     # delete events and all their data (asks first; -y to skip)
```

`<event>` is a slug (as in `/e/<slug>`) or an ID from `list`. The tool reads
`JEVIENSAIDER_DB`, `JEVIENSAIDER_BASE_URL`, `JEVIENSAIDER_RETENTION_DAYS` and
`JEVIENSAIDER_RETENTION_MAX_DAYS` like the server, or the `-db`, `-base-url`,
`-retention-days` and `-retention-max-days` flags. Use the same retention
values as the server, because they decide the expiry dates that `list` and
`show` print. The tool is safe to use while the server is running.

With Docker:

```sh
docker compose exec app jeviensaider events list
docker compose exec app jeviensaider events delete fete-de-l-ecole-ab12cd34
```

`show` prints the admin link, which is handy when an organiser has lost it.

## Docker

```sh
cp .env.example .env          # set JEVIENSAIDER_BASE_URL and JEVIENSAIDER_CREATE_PASSWORD
docker compose pull && docker compose up -d   # prebuilt image
# or: docker compose up -d --build            # build it locally
```

Every push to `master` publishes a multi-arch image (amd64, arm64, armv7) to
`ghcr.io/arnaudmorin/jeviensaider`. It is tagged `latest` and with the
commit's short SHA (`sha-xxxxxxx`) for pinning a version.

- The app listens on `127.0.0.1:8080` (change `PORT` in `.env`). Put a reverse
  proxy in front of it for HTTPS.
- The database is stored in the `data` volume, at `/data/jeviensaider.db`.
- The image is small (about 15 MB) and based on distroless. It runs as a
  non-root user with a read-only root filesystem, and has a built-in health
  check (`jeviensaider -healthcheck`).

To back up the database from the volume:

```sh
docker compose cp app:/data/jeviensaider.db ./backup-$(date +%F).db
```

Stop the app first (`docker compose stop`) to be sure the copy is consistent,
or use the `sqlite3 .backup` command below on the host.

## Deployment without Docker

1. Copy the binary to the server as `/usr/local/bin/jeviensaider`.
2. Install the [systemd unit](deploy/jeviensaider.service) as its header
   explains (system user, data directory in `/var/lib/jeviensaider`), set
   `JEVIENSAIDER_BASE_URL` in it, and put secrets such as
   `JEVIENSAIDER_CREATE_PASSWORD` in `/etc/jeviensaider.env` (mode 600).
3. Put a reverse proxy in front of the app for HTTPS: [Caddy](deploy/Caddyfile)
   (automatic certificates) or [nginx](deploy/nginx.conf) (with certbot).
   Either way, set `JEVIENSAIDER_TRUST_PROXY=true` and keep the app listening
   on `127.0.0.1` only.

The nginx template forwards the client's address and protocol, redirects
HTTP to HTTPS, and keeps the secret admin and personal links out of its
access log (their tokens are replaced with `***`, and the Referer, which can
contain them, is not logged).

**Backups.** The database is a single file in WAL mode. This command takes a
consistent copy while the server is running, for example from a daily cron job:

```sh
sqlite3 /var/lib/jeviensaider/jeviensaider.db ".backup '/var/backups/jeviensaider-$(date +%F).db'"
```

## Upgrading from the `benevole` names

The binary, its variables and its database file were renamed after the
project. An instance installed before the rename needs, once:

1. Stop the app.
2. Rename every `BENEVOLE_*` variable to `JEVIENSAIDER_*`: in `.env`, in the
   systemd unit and in `/etc/benevole.env` (now `/etc/jeviensaider.env`).
3. Rename the database, with its `-wal` and `-shm` files if present:
   `benevole.db` → `jeviensaider.db`. With Docker, the image has no shell, so
   use a throwaway container on the `data` volume (`docker volume ls` shows
   its full name, usually `<directory>_data`):

   ```sh
   docker run --rm -v jeviensaider_data:/data busybox sh -c 'cd /data && for f in benevole.db*; do mv "$f" "jeviensaider${f#benevole}"; done'
   ```

4. Without Docker: `systemctl disable benevole`, install the binary as
   `/usr/local/bin/jeviensaider` and the new
   [unit](deploy/jeviensaider.service), and move `/var/lib/benevole` to
   `/var/lib/jeviensaider`, owned by the new `jeviensaider` user.
5. Start the app.

The app refuses to start while a non-empty `BENEVOLE_*` variable is set, or
while it finds a `benevole.db` but no `jeviensaider.db`, so a forgotten step
cannot silently drop a setting such as the creation password or open an
empty database.

## Under the hood

```
cmd/jeviensaider/  entry point: configuration, server, CLI subcommands
internal/model/    domain types and rules, fill status, planning view
internal/slotgen/  splits a time range into slots
internal/store/    SQLite access, embedded migrations, sign-up transaction
internal/mail/     SMTP sender and background queue
internal/ical/     iCalendar writer (escaping, line folding) for the .ics file
internal/web/      handlers, middleware, CSV, PDF and calendar exports,
                   pages as templ components (*.templ), static files (htmx)
deploy/            systemd unit, Caddy and nginx templates
docs/SPEC.md       what the app does and where in the code
```

[`docs/SPEC.md`](docs/SPEC.md) describes in detail what the app does and how
the code is organised.

- **No overbooking.** Each sign-up checks capacity and inserts the row in a
  single `BEGIN IMMEDIATE` transaction. A test runs 20 concurrent sign-ups for
  the last places.
- **Secret links.** Admin and personal links carry 256-bit random tokens.
  Tokens are redacted from logs, `Referrer-Policy: same-origin` stops them
  leaking to other sites, and admin pages are never cached.
- **Protection.**
  - Cross-site POSTs are rejected using the `Sec-Fetch-Site` and `Origin`
    headers.
  - POSTs are rate-limited per IP (per /64 for IPv6). Behind a proxy, only
    the last `X-Forwarded-For` entry, the one the proxy adds, is trusted.
  - Request bodies are capped at 64 KB. An event has at most 50 stands, 500
    slots and 10 pledge lists, and a pledge list has at most 200 pledges. So
    no single request or event can exhaust the server.
  - Over HTTPS, cookies are `Secure` and HSTS is sent. Pages that can show
    personal data are never cached, including by htmx's history cache.
  - Signing up under a different name from the one the browser remembers
    creates a new volunteer, so a shared personal link or a family computer
    never exposes or renames someone else's sign-ups.
  - The sign-up form has a honeypot field to catch bots.
  - A strict CSP is sent.
  - The CSV export guards against formula injection.
- **Automatic cleanup.** At startup and then every hour, the app deletes
  every event whose last day is more than its number of days ago, together
  with its stands, slots, pledge lists and volunteers. This number is the one that the
  organiser chose, or `JEVIENSAIDER_RETENTION_DAYS` when they chose none. It
  is never more than `JEVIENSAIDER_RETENTION_MAX_DAYS`. An event's last day is
  the latest of its date, its last slot and its creation date, so an event
  that is still being prepared is never deleted.
- **E-mails.** Links come from `JEVIENSAIDER_BASE_URL`, so a forged `Host`
  header cannot turn them into links to another site. Headers are encoded
  and line breaks refused, so names cannot inject headers. E-mails require a
  creation password (or an explicit opt-out), and every kind of mail is
  rate-limited, so the site cannot be used to flood an inbox. A rejected
  address or a down SMTP server cannot block or drop the other messages.
- **Privacy.** Volunteers give only a name and, optionally, a phone number;
  no e-mail is stored. Public pages show only "Prénom N.", phone numbers are
  visible to the organisers only, and pages are marked `noindex`.

## Continuous integration

The [GitHub Actions workflow](.github/workflows/build.yml) runs on every push
or pull request to `master`, and once a week:

- `gofmt`, `go vet`, [`staticcheck`](https://staticcheck.dev), the tests with
  the race detector, and [`govulncheck`](https://go.dev/doc/security/vuln/) to catch known
  vulnerabilities in Go and the dependencies (the weekly run catches newly
  published ones);
- the three Linux binaries;
- the Docker image: built only for pull requests, built and pushed to
  `ghcr.io` for pushes to `master`. Only that job may write packages.

Actions are pinned to commit SHAs and the Docker base images to digests, so
a moved tag cannot change what runs. [Dependabot](.github/dependabot.yml)
proposes updates for them and for the Go modules every week.
