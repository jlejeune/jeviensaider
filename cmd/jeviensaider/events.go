package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

const eventsUsage = `Usage: jeviensaider events <command> [flags] [args]

Super-admin tool. It works directly on the SQLite database, so it can see and
change every event; it is safe to run while the server is running.

Commands:
  list                  list all events, most recent first
  show <event>          details and the public and admin links of an event
  delete <event>...     delete events with all their data (asks first)

<event> is an event's slug (as in /e/<slug>) or its ID from "list".

Flags:
  -db FILE              SQLite database (env JEVIENSAIDER_DB, default jeviensaider.db)
  -base-url URL         public URL used to print links (env JEVIENSAIDER_BASE_URL)
  -retention-days N     days before an event is deleted when its organiser chose none, 0 = kept forever
                        (env JEVIENSAIDER_RETENTION_DAYS, default 60)
  -retention-max-days N longest period an organiser can choose; 0 = 180 days, or
                        -retention-days if longer (env JEVIENSAIDER_RETENTION_MAX_DAYS, default 0)
  -y                    delete: do not ask for confirmation

Use the same retention values as the server: they decide the expiry dates.
`

type eventsCmd struct {
	stdin   *bufio.Reader
	out     io.Writer
	db      string
	baseURL string
	policy  store.Retention
	yes     bool
}

// runEvents implements "jeviensaider events ..." and returns the exit code.
func runEvents(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	c := &eventsCmd{stdin: bufio.NewReader(stdin), out: stdout}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
		fmt.Fprint(stdout, eventsUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub, rest := args[0], args[1:]

	fs := flag.NewFlagSet("jeviensaider events "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, eventsUsage) }
	fs.StringVar(&c.db, "db", env("JEVIENSAIDER_DB", "jeviensaider.db"), "")
	fs.StringVar(&c.baseURL, "base-url", env("JEVIENSAIDER_BASE_URL", ""), "")
	retention, err := retentionFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	retentionMax, err := retentionMaxFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	var retentionDays, retentionMaxDays int
	fs.IntVar(&retentionDays, "retention-days", retention, "")
	fs.IntVar(&retentionMaxDays, "retention-max-days", retentionMax, "")
	fs.BoolVar(&c.yes, "y", false, "")
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		return 2
	}
	if c.policy, err = store.NewRetention(retentionDays, retentionMaxDays); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")

	var run func(context.Context, *store.Store, []string) error
	switch sub {
	case "list", "ls":
		run = c.list
	case "show":
		run = c.show
	case "delete", "rm":
		run = c.delete
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", sub, eventsUsage)
		return 2
	}

	if err := checkLegacyDB(c.db); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	st, err := store.Open(c.db)
	if err != nil {
		fmt.Fprintf(stderr, "open %s: %v\n", c.db, err)
		return 1
	}
	defer st.Close()
	if err := run(context.Background(), st, pos); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseInterspersed parses flags placed anywhere among the positional
// arguments ("delete my-event -y" as well as "delete -y my-event").
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func (c *eventsCmd) expires(e store.EventSummary) string {
	days := c.policy.Days(e.RetentionDays)
	if days <= 0 {
		return "never"
	}
	return store.ExpiresOn(e.LastDay, days).Format(model.DateLayout)
}

// retention describes how long an event is kept, for "show".
func (c *eventsCmd) retention(e store.EventSummary) string {
	days := c.policy.Days(e.RetentionDays)
	switch {
	case days <= 0:
		return "never"
	case e.RetentionDays == 0:
		return fmt.Sprintf("%d days (server default)", days)
	case days < e.RetentionDays:
		return fmt.Sprintf("%d days (cut from %d by the maximum)", days, e.RetentionDays)
	}
	return fmt.Sprintf("%d days", days)
}

func (c *eventsCmd) list(ctx context.Context, st *store.Store, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("list takes no arguments")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintln(c.out, "No events.")
		return nil
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSLUG\tTITLE\tDATE\tSTATUS\tSTANDS\tSLOTS\tVOLUNTEERS\tEXPIRES")
	for _, e := range events {
		date := e.Date
		if date == "" {
			date = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n",
			e.ID, e.Slug, truncate(e.Title, 40), date, e.Status, e.Stands, e.Slots, e.Volunteers, c.expires(e))
	}
	return w.Flush()
}

func (c *eventsCmd) show(ctx context.Context, st *store.Store, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: jeviensaider events show <event>")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	e, err := find(events, args[0])
	if err != nil {
		return err
	}
	base := c.baseURL
	if base == "" {
		base = "(base URL)"
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID\t%d\n", e.ID)
	fmt.Fprintf(w, "Title\t%s\n", e.Title)
	fmt.Fprintf(w, "Date\t%s\n", orDash(e.Date))
	fmt.Fprintf(w, "Location\t%s\n", orDash(e.Location))
	fmt.Fprintf(w, "Status\t%s\n", e.Status)
	fmt.Fprintf(w, "Organiser\t%s\n", orDash(e.AdminEmail))
	fmt.Fprintf(w, "Created\t%s\n", e.CreatedAt.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "Content\t%d stands, %d slots, %d volunteers, %d sign-ups\n", e.Stands, e.Slots, e.Volunteers, e.Signups)
	fmt.Fprintf(w, "Pledges\t%d lists, %d pledges, %d items\n", e.PledgeLists, e.Pledges, e.Items)
	fmt.Fprintf(w, "Last day\t%s\n", e.LastDay.Format(model.DateLayout))
	fmt.Fprintf(w, "Retention\t%s\n", c.retention(e))
	fmt.Fprintf(w, "Expires\t%s\n", c.expires(e))
	fmt.Fprintf(w, "Public link\t%s/e/%s\n", base, e.Slug)
	fmt.Fprintf(w, "Admin link\t%s/a/%s\n", base, e.AdminToken)
	return w.Flush()
}

func (c *eventsCmd) delete(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: jeviensaider events delete [-y] <event> [<event> ...]")
	}
	events, err := st.ListEventSummaries(ctx)
	if err != nil {
		return err
	}
	// Resolve every argument first, so a typo deletes nothing.
	var targets []store.EventSummary
	seen := map[int64]bool{}
	for _, ref := range args {
		e, err := find(events, ref)
		if err != nil {
			return err
		}
		if !seen[e.ID] {
			seen[e.ID] = true
			targets = append(targets, e)
		}
	}
	for _, e := range targets {
		fmt.Fprintf(c.out, "  %d  %s  %q  (%d stands, %d slots, %d volunteers, %d pledge lists, %d pledges)\n",
			e.ID, e.Slug, e.Title, e.Stands, e.Slots, e.Volunteers, e.PledgeLists, e.Pledges)
	}
	if !c.yes {
		fmt.Fprintf(c.out, "Delete %d event(s) and all their data? This cannot be undone. [y/N] ", len(targets))
		answer, _ := c.stdin.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" && a != "o" && a != "oui" {
			fmt.Fprintln(c.out, "Cancelled.")
			return nil
		}
	}
	for _, e := range targets {
		if err := st.DeleteEvent(ctx, e.ID); err != nil {
			return fmt.Errorf("delete %s: %w", e.Slug, err)
		}
		fmt.Fprintf(c.out, "Deleted %s\n", e.Slug)
	}
	return nil
}

// find resolves an event by slug, or by ID when ref is a number.
func find(events []store.EventSummary, ref string) (store.EventSummary, error) {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "/e/")
	id, idErr := strconv.ParseInt(ref, 10, 64)
	for _, e := range events {
		if e.Slug == ref || (idErr == nil && e.ID == id) {
			return e, nil
		}
	}
	return store.EventSummary{}, fmt.Errorf("no event %q (use a slug or an ID from \"jeviensaider events list\")", ref)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
