package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// The four routes behind `capabilities` (links.go): /api/v1/pocket/search,
// /day/{date}, /widget and /capture. Each runs as the request's person and
// sees only their own wishes, as GET /wishes does: never a list somebody
// shared with them, never who reserved what. The SDK's handlers check and cap
// what goes in and out.

// wishURL opens a wish in the app.
func wishURL(id int64) string { return "/wishes/" + strconv.FormatInt(id, 10) }

// dots joins the parts that say something: "129.00 EUR · Test Shop".
func dots(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// priceText is a price as people read it, "" when there is none.
func priceText(minor int64, cur string) string {
	if minor <= 0 {
		return ""
	}
	return money(minor, cur)
}

// ---------- search ----------

// searchWords are the words of a query worth looking for: the ones with a
// letter or a digit in them ("??" and "*" find nothing), at most eight.
func searchWords(q string) []string {
	var out []string
	for _, w := range strings.Fields(q) {
		if strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
			continue
		}
		if out = append(out, w); len(out) == 8 {
			break
		}
	}
	return out
}

// wishLine says where a wish stands in one line: price · shop · list.
func wishLine(w *store.Wish) string {
	switch w.Status {
	case store.StatusBought:
		price := priceText(w.BoughtPrice, firstNonEmpty(w.BoughtCurrency, w.Currency))
		return dots("Bought", firstNonEmpty(price, priceText(w.Price, w.Currency)), firstNonEmpty(w.BoughtStore, w.Store), w.ListName)
	case store.StatusArchived:
		return dots("Archived", priceText(w.Price, w.Currency), w.Store, w.ListName)
	}
	return dots(priceText(w.Price, w.Currency), w.Store, w.ListName)
}

// pocketSearch finds the person's wishes by the words in their name, shop,
// notes, tags or who a gift is for: wanted ones first, then bought and
// archived ones.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	words := searchWords(q)
	ws, err := s.store.SearchWishes(ctx, actorID(ctx), words, limit)
	if err != nil {
		return nil, err
	}
	has := func(v string) bool {
		v = strings.ToLower(v)
		for _, w := range words {
			if !strings.Contains(v, strings.ToLower(w)) {
				return false
			}
		}
		return true
	}
	out := make([]core.SearchItem, 0, len(ws))
	for i := range ws {
		w := &ws[i]
		it := core.SearchItem{ID: strconv.FormatInt(w.ID, 10), Title: w.Title, Snippet: wishLine(w), Kind: "wish", Date: w.CreatedAt, URL: wishURL(w.ID), Link: w.URL}
		// say where the words were found when the line above does not show it
		if shown := w.Title + " " + it.Snippet; !has(shown) {
			switch {
			case w.Notes != "" && has(shown+" "+w.Notes):
				it.Snippet = dots(it.Snippet, w.Notes)
			case w.Tags != "" && has(shown+" "+w.Tags):
				it.Snippet = dots(it.Snippet, "Tags: "+w.Tags)
			case w.Recipient != "" && has(shown+" "+w.Recipient):
				it.Snippet = dots(it.Snippet, "For "+w.Recipient)
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// ---------- day ----------

// pocketDay says what happened with the person's wishes on one day: what
// they bought, which prices moved and what they added. A wish shows once, as
// the most telling of the three. The day is the caller's (?tz=), else the
// server's own, which is also what "bought on" dates are written in.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, nil
	}
	// stored moments are UTC and end in "Z" or ".123Z": bounds without the
	// zone letter sort before both
	const layout = "2006-01-02T15:04:05"
	added, bought, moved, err := s.store.WishesOfDay(ctx, actorID(ctx), date, d.UTC().Format(layout), d.AddDate(0, 0, 1).UTC().Format(layout))
	if err != nil {
		return nil, err
	}
	if len(added)+len(bought)+len(moved) == 0 {
		return nil, nil
	}
	clock := func(stamp string) string {
		if at, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
			return at.In(loc).Format("15:04")
		}
		return ""
	}
	day := &core.Day{Date: date}
	seen := map[int64]bool{}
	add := func(w *store.Wish, it core.DayItem) {
		if seen[w.ID] {
			return
		}
		seen[w.ID] = true
		it.ID, it.Title, it.URL = strconv.FormatInt(w.ID, 10), w.Title, wishURL(w.ID)
		if it.Amount <= 0 {
			it.Amount, it.Currency = 0, ""
		}
		day.Items = append(day.Items, it)
	}
	var spent int64
	spentIn, oneCurrency := "", true
	for i := range bought {
		w := &bought[i]
		cur := firstNonEmpty(w.BoughtCurrency, w.Currency)
		add(w, core.DayItem{Sub: firstNonEmpty(w.BoughtStore, w.Store, w.ListName), Kind: "bought", Done: true, Amount: w.BoughtPrice, Currency: cur})
		if w.BoughtPrice > 0 {
			oneCurrency = oneCurrency && (spentIn == "" || spentIn == cur)
			spent, spentIn = spent+w.BoughtPrice, cur
		}
	}
	down := 0
	for i := range moved {
		m := &moved[i]
		way := "up from "
		if m.To < m.From {
			way = "down from "
			down++
		}
		add(&m.Wish, core.DayItem{Time: clock(m.At), Sub: dots(m.Wish.Store, way+money(m.From, m.Currency)), Kind: "price", Amount: m.To, Currency: m.Currency})
	}
	for i := range added {
		w := &added[i]
		add(w, core.DayItem{Time: clock(w.CreatedAt), Sub: firstNonEmpty(w.Store, w.ListName), Kind: "added", Amount: w.Price, Currency: w.Currency})
	}

	var parts []string
	if n := len(added); n > 0 {
		parts = append(parts, plural(n, "wish added", "wishes added"))
		day.Metrics = append(day.Metrics, core.Metric{Label: "Added", Value: strconv.Itoa(n)})
	}
	if n := len(bought); n > 0 {
		if len(added) > 0 {
			parts = append(parts, strconv.Itoa(n)+" bought")
		} else {
			parts = append(parts, plural(n, "wish bought", "wishes bought"))
		}
		day.Metrics = append(day.Metrics, core.Metric{Label: "Bought", Value: strconv.Itoa(n)})
		if spent > 0 && oneCurrency {
			day.Metrics = append(day.Metrics, core.Metric{Label: "Spent", Value: money(spent, spentIn)})
		}
	}
	if n := len(moved); n > 0 {
		if down == n {
			parts = append(parts, plural(n, "price drop", "price drops"))
		} else {
			parts = append(parts, plural(n, "price change", "price changes"))
		}
		day.Metrics = append(day.Metrics, core.Metric{Label: "Price changes", Value: strconv.Itoa(n)})
	}
	day.Summary = strings.Join(parts, ", ")
	return day, nil
}

// ---------- widget ----------

// widgetDetail is a wish's row in the widget: price · shop, with the price
// it started at when it is cheaper now.
func widgetDetail(w *store.Wish) string {
	price := priceText(w.Price, w.Currency)
	if price != "" && w.Change < 0 {
		price += ", was " + money(w.Price-w.Change, w.Currency)
	}
	return dots(price, w.Store)
}

// pocketWidget is the wishlist in a few lines: the wishes that reached their
// target price (the app's "Price drops"), then the ones added last. It reads
// the counts and a few rows, never a shop page.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	wanted, drops, value, err := s.store.WishTotals(ctx, uid, st.HomeCurrency)
	if err != nil {
		return nil, err
	}
	if wanted == 0 {
		return nil, nil
	}
	out := &core.WidgetData{URL: "/wishes", Line: plural(wanted, "wish", "wishes"), Metrics: []core.Metric{
		{Label: "Wishes", Value: strconv.Itoa(wanted)},
		{Label: "Price drops", Value: strconv.Itoa(drops)},
	}}
	if value > 0 {
		out.Metrics = append(out.Metrics, core.Metric{Label: "Total", Value: money(value, st.HomeCurrency)})
	}
	const rows = 8
	seen := map[int64]bool{}
	fill := func(f store.WishFilter) error {
		f.Limit = rows
		ws, err := s.store.ListWishes(ctx, uid, f)
		if err != nil {
			return err
		}
		for i := range ws {
			if w := &ws[i]; !seen[w.ID] && len(out.Items) < rows {
				seen[w.ID] = true
				out.Items = append(out.Items, core.WidgetItem{ID: strconv.FormatInt(w.ID, 10), Title: w.Title, Detail: widgetDetail(w), URL: wishURL(w.ID)})
			}
		}
		return nil
	}
	if drops > 0 {
		out.Line, out.URL = plural(drops, "price drop", "price drops"), "/wishes?view=drops"
		if err := fill(store.WishFilter{AtTarget: true, Sort: "priority"}); err != nil {
			return nil, err
		}
	}
	if len(out.Items) < rows {
		if err := fill(store.WishFilter{Sort: "added"}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------- capture ----------

var reLink = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"']+`)

// What may stand around a web address in a sentence and says nothing once
// the address is taken out: "Trail shoes - (https://…)."
const (
	linkEnding = `.,;:!?)]}>'"`
	beforeLink = " \t-–—:|,([{<'\""
	afterLink  = " \t-–—:|,.;!?)]}>'\""
)

// captureBudget is how long a quick line waits for the shop page (POST
// /wishes waits up to 20 seconds; the apps that send a quick line give up
// sooner than that).
const captureBudget = 8 * time.Second

// cutLink takes the first web address out of a line.
func cutLink(s string) (rest, link string) {
	loc := reLink.FindStringIndex(s)
	if loc == nil {
		return s, ""
	}
	link = s[loc[0]:loc[1]]
	for link != "" && strings.IndexByte(linkEnding, link[len(link)-1]) >= 0 {
		// a bracket that closes one inside the address belongs to it
		if link[len(link)-1] == ')' && strings.Count(link, "(") >= strings.Count(link, ")") {
			break
		}
		link = link[:len(link)-1]
	}
	return strings.TrimRight(s[:loc[0]], beforeLink) + " " + strings.TrimLeft(s[loc[0]+len(link):], afterLink), link
}

// captureWish splits a quick line into a wish's name, link and notes: the
// title when one was given (the text is then the notes), else the text's
// first line; the first web address in either is the wish's link.
func captureWish(in core.CaptureInput) (title, link, notes string) {
	title, link = cutLink(in.Title)
	notes = in.Text
	if link == "" {
		notes, link = cutLink(strings.TrimSpace(in.Text))
	}
	if in.Title == "" {
		title, notes, _ = strings.Cut(notes, "\n")
	}
	title, notes = strings.Join(strings.Fields(title), " "), strings.TrimSpace(notes)
	// a wish's name holds 300 bytes: what is longer stays whole in the notes
	if len(title) > 300 {
		whole := in.Text
		if in.Title != "" {
			whole = strings.TrimSpace(title + "\n" + notes)
		}
		cut := []rune(title)
		for len(string(cut))+len("…") > 300 {
			cut = cut[:len(cut)-1]
		}
		title, notes = strings.TrimSpace(string(cut))+"…", strings.TrimSpace(strings.Replace(whole, link, "", 1))
	}
	return title, link, notes
}

// pocketCapture keeps a quick line as a new wish, where POST /wishes without
// a list puts one (the person's first wishlist, made when missing). A line
// that is or has a web address becomes a wish for that page: its name,
// picture and price are read as POST /wishes reads them, within
// captureBudget, and the picture is copied once the answer is out. The same
// request_id again answers the same wish and adds nothing: the receipt is
// written in the transaction that adds the wish, and lines are written down
// one at a time, so the same line sent twice at once is kept once (and makes
// one first wishlist, not two).
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	op, err := core.NewOperation(fmt.Sprintf("wishlist:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	title, link, notes := captureWish(in)
	wish := wishInput{}
	if title != "" {
		wish.Title = &title
	}
	if notes != "" {
		wish.Notes = &notes
	}
	if link != "" {
		wish.URL = &link
	}
	pctx, cancel := context.WithTimeout(ctx, captureBudget)
	page := s.readPage(pctx, wish)
	cancel()
	switch {
	case title != "" || (page != nil && page.Title != ""):
	case link != "":
		return nil, fmt.Errorf("%w: that page could not be read, write what it is in front of the link", core.ErrCapture)
	default:
		return nil, fmt.Errorf("%w: a wish needs a name or a link", core.ErrCapture)
	}

	s.keepMu.Lock()
	defer s.keepMu.Unlock()
	// the same line may have been kept while this one read the page
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}
	wi, msg, err := s.newWish(ctx, uid, wish, page)
	if msg != "" {
		return nil, fmt.Errorf("%w: %s", core.ErrCapture, msg)
	}
	if err != nil {
		return nil, err
	}
	err = s.saveWish(ctx, uid, wi, func(tx *sql.Tx) error {
		res = core.CaptureResult{ID: strconv.FormatInt(wi.ID, 10), Title: wi.Title, URL: wishURL(wi.ID)}
		return op.Save(ctx, tx, res)
	})
	if err != nil {
		return nil, err
	}
	if wi.ImageURL != "" {
		go s.fetchPhoto(context.WithoutCancel(ctx), uid, wi.ID, wi.ImageURL)
	}
	return &res, nil
}
