package web

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"jeviensaider/internal/model"
)

// The planning PDF: one A4 page per stand, with a table of its slots. Each
// slot has a line per volunteer, then an empty line per missing person, to
// fill in by hand on the day. After the stands, each pledge list gets its
// own pages, with one line per pledge: the quantity, the name and the phone.
//
// To keep a stand on one page, lines shrink from rowMax down to rowMin. A
// stand with more lines than that continues on more pages; a slot is then
// kept whole on one page whenever it fits on one.

// Page geometry, in points (1/72 inch). A4 is 595 × 842.
const (
	pageW, pageH = 595.0, 842.0
	margin       = 40.0
	contentW     = pageW - 2*margin
	bandH        = 8.0  // the stand's colour, across the top of the page
	tableHeadH   = 22.0 // the header row, as "Créneau / Bénévoles / Téléphone"
	footerH      = 18.0
	rowMax       = 26.0 // comfortable line height
	rowMin       = 15.0 // smallest line height still easy to write in
	timeColW     = 110.0
	phoneColW    = 115.0
	nameColW     = contentW - timeColW - phoneColW
	maxDescLines = 8
)

// pdfBlock is a slot and its lines on one page: its volunteers, then nil for
// each empty line. A pledge has no slot. Its time cell shows Label.
type pdfBlock struct {
	Slot  *model.SlotView
	Label string
	Lines []*model.SignupRow
}

// pdfTableHead holds the texts of the table header row.
type pdfTableHead struct{ Time, Name, Phone string }

// The header rows of the stand pages and of the pledge list pages.
var (
	standHead  = pdfTableHead{"Créneau", "Bénévoles", "Téléphone"}
	pledgeHead = pdfTableHead{"Quantité", "Nom", "Téléphone"}
)

// pledgeColor is the colour of the band on the pledge list pages.
const pledgeColor = "#8a5cc7"

// pdfPage is one page of a stand or a pledge list.
type pdfPage struct {
	First  bool // the first page, with the description
	Blocks []pdfBlock
}

// standBlocks lists the lines of each slot of a stand: its volunteers, then
// one empty line per missing person, and at least one line.
func standBlocks(st *model.StandView) []pdfBlock {
	var blocks []pdfBlock
	for _, sl := range st.Slots {
		b := pdfBlock{Slot: sl}
		for i := range sl.Signups {
			b.Lines = append(b.Lines, &sl.Signups[i])
		}
		for len(b.Lines) < max(sl.Count+sl.Missing, 1) {
			b.Lines = append(b.Lines, nil)
		}
		blocks = append(blocks, b)
	}
	return blocks
}

// pledgeBlocks makes one block per pledge of a list. The label is the
// quantity, and the only line holds the name and the phone.
func pledgeBlocks(l *model.PledgeListView) []pdfBlock {
	var blocks []pdfBlock
	for _, r := range l.Pledges {
		line := &model.SignupRow{Name: r.Name, Phone: r.Phone}
		blocks = append(blocks, pdfBlock{Label: itoa(r.Quantity), Lines: []*model.SignupRow{line}})
	}
	return blocks
}

// rowHeight chooses the line height of a stand: the largest one (from
// rowMax down to rowMin) that keeps it on as few pages as possible, that is
// on as few pages as with rowMin.
func rowHeight(blocks []pdfBlock, firstRoom, nextRoom float64) float64 {
	fewest := len(paginate(blocks, rowMin, firstRoom, nextRoom))
	h := rowMax
	for h > rowMin && len(paginate(blocks, h, firstRoom, nextRoom)) > fewest {
		h -= 0.5
	}
	return max(h, rowMin)
}

// paginate places the blocks on pages of firstRoom then nextRoom points,
// with lines of rowH. A slot that does not fit on the rest of a page moves
// to the next one, unless it is longer than a whole page: then it is split.
func paginate(blocks []pdfBlock, rowH, firstRoom, nextRoom float64) []pdfPage {
	pages := []pdfPage{{First: true}}
	room, used := firstRoom, 0.0
	newPage := func() {
		pages = append(pages, pdfPage{})
		room, used = nextRoom, 0
	}
	for _, b := range blocks {
		lines := b.Lines
		for len(lines) > 0 {
			free := int((room-used)/rowH + 1e-9)
			fitsOnAPage := float64(len(lines))*rowH <= nextRoom
			if len(lines) > free && used > 0 && (fitsOnAPage || free == 0) {
				newPage()
				continue
			}
			n := min(len(lines), free)
			page := &pages[len(pages)-1]
			page.Blocks = append(page.Blocks, pdfBlock{Slot: b.Slot, Label: b.Label, Lines: lines[:n]})
			used += float64(n) * rowH
			lines = lines[n:]
		}
	}
	return pages
}

// writePlanningPDF writes the planning of ev as a PDF.
func writePlanningPDF(w io.Writer, ev *model.Event, p *model.Planning, now time.Time) error {
	d, err := newPDFDoc()
	if err != nil {
		return err
	}
	d.pdf.SetInfo(gopdf.PdfInfo{Title: "Planning · " + ev.Title, Creator: "Je viens aider", CreationDate: now})
	if len(p.Stands) == 0 && len(p.Pledges) == 0 {
		d.pdf.AddPage()
		d.eventLine(ev)
		d.text(margin, margin+40, contentW, 20, "Aucun stand pour l'instant.", false, 12, colText)
	}
	for _, st := range p.Stands {
		d.stand(ev, st, p.MultiDay)
	}
	for _, l := range p.Pledges {
		d.pledgeList(ev, l)
	}
	if d.err != nil {
		return d.err
	}
	_, err = d.pdf.WriteTo(w)
	return err
}

// pdfDoc draws with gopdf and keeps the first error, so that drawing code
// reads as a list of steps.
type pdfDoc struct {
	pdf gopdf.GoPdf
	err error
}

type rgb struct{ r, g, b uint8 }

var (
	colText   = rgb{32, 33, 36}
	colMuted  = rgb{95, 99, 104}
	colBorder = rgb{170, 170, 170}
	colHead   = rgb{238, 238, 238}
)

func newPDFDoc() (*pdfDoc, error) {
	d := &pdfDoc{}
	d.pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := d.pdf.AddTTFFontData("regular", goregular.TTF); err != nil {
		return nil, err
	}
	if err := d.pdf.AddTTFFontData("bold", gobold.TTF); err != nil {
		return nil, err
	}
	return d, nil
}

// stand draws the pages of one stand.
func (d *pdfDoc) stand(ev *model.Event, st *model.StandView, multiDay bool) {
	d.setFont(false, 10)
	desc := d.wrap(st.Description, contentW, maxDescLines)
	d.listing(ev, st.Name, st.Color, desc, standBlocks(st), "Aucun créneau.", standHead, multiDay)
}

// pledgeList draws the pages of one pledge list: one line per pledge, with
// no empty line. The description gives the drop-off time and the status.
func (d *pdfDoc) pledgeList(ev *model.Event, l *model.PledgeListView) {
	// A pledge has no slot, so multiDay has no effect on its time cell.
	d.listing(ev, l.Name, pledgeColor, d.pledgeDescLines(l), pledgeBlocks(l),
		"Aucune inscription pour l'instant.", pledgeHead, false)
}

// pledgeDescLines wraps the description of a list, then its drop-off time
// and its status, in the 10 pt font. The result has at most maxDescLines
// lines. The drop-off time takes one line and the status gets the rest, so
// both always show. The description gets what is left, if anything.
func (d *pdfDoc) pledgeDescLines(l *model.PledgeListView) []string {
	d.setFont(false, 10)
	short := d.wrap(dropOffLabel(l.DropOff), contentW, 1)
	short = append(short, d.wrap(pledgeStatus(l), contentW, maxDescLines-len(short))...)
	var lines []string
	if room := maxDescLines - len(short); room > 0 {
		lines = d.wrap(l.Description, contentW, room)
	}
	return append(lines, short...)
}

// listing draws the pages of a stand or a pledge list: a colour band, the
// name, the description lines on the first page, then the table of the
// blocks. If there are no blocks, it writes the empty text instead of the
// table. The description lines must be wrapped in the 10 pt font.
func (d *pdfDoc) listing(ev *model.Event, name, color string, desc []string, blocks []pdfBlock, empty string, head pdfTableHead, multiDay bool) {
	headH := func(first bool) float64 {
		h := 16 + 30 + 10.0 // event line, stand name, gap
		if first && len(desc) > 0 {
			h += float64(len(desc))*13 + 6
		}
		return h
	}
	room := func(first bool) float64 {
		return pageH - 2*margin - headH(first) - tableHeadH - footerH
	}
	rowH := rowHeight(blocks, room(true), room(false))
	pages := paginate(blocks, rowH, room(true), room(false))

	for i, pg := range pages {
		d.pdf.AddPage()
		d.fillRect(0, 0, pageW, bandH, standColor(color))
		d.eventLine(ev)
		title := name
		if !pg.First {
			title += " (suite)"
		}
		d.text(margin, margin+16, contentW, 30, title, true, 20, colText)
		y := margin + 46.0
		if pg.First && len(desc) > 0 {
			for _, line := range desc {
				d.text(margin, y, contentW, 13, line, false, 10, colMuted)
				y += 13
			}
			y += 6
		}
		y += 10
		if len(blocks) == 0 {
			d.text(margin, y, contentW, 20, empty, false, 11, colMuted)
		} else {
			d.table(pg.Blocks, y, rowH, head, multiDay)
		}
		if len(pages) > 1 {
			d.text(margin, pageH-margin, contentW, 12, strconv.Itoa(i+1)+" / "+strconv.Itoa(len(pages)), false, 9, colMuted)
		}
	}
}

func (d *pdfDoc) eventLine(ev *model.Event) {
	parts := []string{ev.Title}
	if ev.Date != "" {
		parts = append(parts, dateFR(ev.Date))
	}
	if ev.Location != "" {
		parts = append(parts, ev.Location)
	}
	d.text(margin, margin, contentW, 14, strings.Join(parts, " · "), false, 10, colMuted)
}

// table draws the header row then the blocks' lines, from y down. The time
// cell of a block without a slot shows its Label.
func (d *pdfDoc) table(blocks []pdfBlock, y, rowH float64, head pdfTableHead, multiDay bool) {
	xName, xPhone := margin+timeColW, margin+timeColW+nameColW
	d.fillRect(margin, y, contentW, tableHeadH, colHead)
	d.text(margin+6, y, timeColW-12, tableHeadH, head.Time, true, 9, colText)
	d.text(xName+6, y, nameColW-12, tableHeadH, head.Name, true, 9, colText)
	d.text(xPhone+6, y, phoneColW-12, tableHeadH, head.Phone, true, 9, colText)
	d.strokeRect(margin, y, contentW, tableHeadH)
	y += tableHeadH

	size := fontSize(rowH)
	for _, b := range blocks {
		h := float64(len(b.Lines)) * rowH
		// The slot's time (or the label), in a cell as tall as its lines.
		cell := b.Label
		if b.Slot != nil {
			cell = when(multiDay, b.Slot.StartsAt, b.Slot.EndsAt)
		}
		d.text(margin+6, y, timeColW-12, min(rowH, h), cell, true, size, colText)
		d.strokeRect(margin, y, timeColW, h)
		for _, su := range b.Lines {
			d.strokeRect(xName, y, nameColW, rowH)
			d.strokeRect(xPhone, y, phoneColW, rowH)
			if su != nil {
				d.volunteer(su, xName+6, y, nameColW-12, rowH, size)
				d.text(xPhone+6, y, phoneColW-12, rowH, su.Phone, false, size, colText)
			}
			y += rowH
		}
	}
}

// volunteer draws a name, then their comment in grey if there is room.
func (d *pdfDoc) volunteer(su *model.SignupRow, x, y, w, h, size float64) {
	d.text(x, y, w, h, su.Name, false, size, colText)
	if su.Comment == "" {
		return
	}
	d.setFont(false, size)
	used := d.width(su.Name + "  ")
	if used < w*0.6 {
		d.text(x+used, y, w-used, h, "("+su.Comment+")", false, size*0.9, colMuted)
	}
}

// fontSize goes with the line height: 11 pt on comfortable lines, 9 pt on
// the smallest.
func fontSize(rowH float64) float64 {
	return 9 + 2*(rowH-rowMin)/(rowMax-rowMin)
}

// text draws s in a box, vertically centred, cut with "…" if too long.
func (d *pdfDoc) text(x, y, w, h float64, s string, bold bool, size float64, c rgb) {
	if d.err != nil || s == "" {
		return
	}
	d.setFont(bold, size)
	d.pdf.SetTextColor(c.r, c.g, c.b)
	d.pdf.SetXY(x, y)
	d.err = d.pdf.CellWithOption(&gopdf.Rect{W: w, H: h}, d.fit(s, w), gopdf.CellOption{Align: gopdf.Left | gopdf.Middle})
}

func (d *pdfDoc) setFont(bold bool, size float64) {
	if d.err != nil {
		return
	}
	family := "regular"
	if bold {
		family = "bold"
	}
	d.err = d.pdf.SetFont(family, "", size)
}

func (d *pdfDoc) width(s string) float64 {
	w, err := d.pdf.MeasureTextWidth(s)
	if err != nil && d.err == nil {
		d.err = err
	}
	return w
}

// fit cuts s to the width w (in the current font), ending with "…".
func (d *pdfDoc) fit(s string, w float64) string {
	if d.width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && d.width(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "…"
}

// wrap splits text into lines of at most width w (in the current font),
// keeping its line breaks, and cuts it after maxLines lines with "…".
func (d *pdfDoc) wrap(text string, w float64, maxLines int) []string {
	var lines []string
	for _, para := range strings.Split(strings.TrimSpace(text), "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && d.width(line+" "+word) > w {
				lines = append(lines, line)
				line = word
			} else if line == "" {
				line = word
			} else {
				line += " " + word
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = d.fit(lines[maxLines-1]+" …", w)
	}
	return lines
}

func (d *pdfDoc) fillRect(x, y, w, h float64, c rgb) {
	d.pdf.SetFillColor(c.r, c.g, c.b)
	d.pdf.RectFromUpperLeftWithStyle(x, y, w, h, "F")
}

func (d *pdfDoc) strokeRect(x, y, w, h float64) {
	d.pdf.SetLineWidth(0.6)
	d.pdf.SetStrokeColor(colBorder.r, colBorder.g, colBorder.b)
	d.pdf.RectFromUpperLeftWithStyle(x, y, w, h, "D")
}

// standColor turns a validated "#rrggbb" into a colour. A stand with no
// color, or a color that is not valid, gets the light grey of the headers.
func standColor(hex string) rgb {
	if !validColor(hex) {
		return colHead
	}
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}
