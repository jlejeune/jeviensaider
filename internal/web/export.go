package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

// adminExportCSV writes the full planning as CSV: one line per sign-up, plus
// one line per missing person so empty places are visible too. After the
// stands, it writes one line per pledge of each list. Semicolon separator
// and UTF-8 BOM so that French Excel opens it directly.
func (s *Server) adminExportCSV(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, err := s.planning(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="benevoles-%s.csv"`, store.Slugify(ev.Title)))
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"Stand", "Date", "Début", "Fin", "Nom", "Téléphone", "Commentaire", "Inscrit le", "Quantité"})
	for _, st := range p.Stands {
		name := csvSafe(st.Name)
		for _, sl := range st.Slots {
			// line is one CSV line of this slot: stand, date, times, then cols.
			// The last col is the quantity, which only pledges have.
			line := func(cols ...string) []string {
				return append([]string{name, sl.StartsAt.Format("02/01/2006"), sl.StartsAt.Format("15:04"), sl.EndsAt.Format("15:04")}, cols...)
			}
			for _, su := range sl.Signups {
				cw.Write(line(csvSafe(su.Name), csvSafe(su.Phone), csvSafe(su.Comment), su.CreatedAt.Local().Format("02/01/2006 15:04"), ""))
			}
			for range sl.Missing {
				cw.Write(line("(place libre)", "", "", "", ""))
			}
		}
	}
	// A pledge list has the date of the event, or no date if the event has
	// none. The drop-off time is its start. It has no end.
	var date string
	if d, err := time.Parse(model.DateLayout, ev.Date); err == nil {
		date = d.Format("02/01/2006")
	}
	for _, l := range p.Pledges {
		stand := "À apporter · " + csvSafe(l.Name)
		// line is one CSV line of this list: stand, date, drop-off, then cols.
		line := func(cols ...string) []string {
			return append([]string{stand, date, l.DropOff, ""}, cols...)
		}
		for _, pl := range l.Pledges {
			cw.Write(line(csvSafe(pl.Name), csvSafe(pl.Phone), "", pl.CreatedAt.Local().Format("02/01/2006 15:04"), itoa(pl.Quantity)))
		}
		if l.Missing > 0 {
			cw.Write(line("(il en manque "+itoa(l.Missing)+")", "", "", "", ""))
		}
	}
	cw.Flush()
}

// csvSafe neutralises spreadsheet formula injection (=, +, -, @ prefixes).
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}
