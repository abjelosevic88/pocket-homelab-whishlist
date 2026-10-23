package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/product"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

var priceChecks atomic.Int64

// ---- price watching ----
//
// Every 15 minutes the janitor looks at the tracked wishes that were checked
// longest ago and reads each page again whose owner's interval has passed
// (Settings → Price checks, 12 h by default, 0 = never). At most
// PRICE_CHECKS_PER_RUN pages per sweep, one at a time, a short pause between
// pages of the same shop. A lower price is recorded in the history; reaching
// the target price or dropping by the owner's percentage sends one alert
// (until the price goes up again); "back in stock" sends one too.

func (s *Server) checkDue(ctx context.Context) int {
	if !s.checkMu.TryLock() {
		return 0
	}
	defer s.checkMu.Unlock()
	due, err := s.store.DueChecks(ctx, s.cfg.PriceChecksPerRun*4)
	if err != nil {
		s.log.Warn("price checks", "err", err)
		return 0
	}
	settings := map[int64]store.Settings{}
	lastHost := map[string]time.Time{}
	n := 0
	for _, wi := range due {
		if ctx.Err() != nil || n >= s.cfg.PriceChecksPerRun {
			break
		}
		st, ok := settings[wi.UserID]
		if !ok {
			st, _ = s.store.GetSettings(ctx, wi.UserID)
			settings[wi.UserID] = st
		}
		if st.PriceCheckHours <= 0 {
			continue
		}
		if wi.CheckedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, wi.CheckedAt); err == nil && time.Since(t) < time.Duration(st.PriceCheckHours)*time.Hour {
				continue
			}
		}
		host := product.StoreName(wi.URL)
		if t, ok := lastHost[host]; ok && time.Since(t) < 3*time.Second {
			time.Sleep(3*time.Second - time.Since(t))
		}
		lastHost[host] = time.Now()
		s.checkOne(ctx, &wi, st)
		n++
	}
	if n > 0 {
		s.log.Info("price checks", "pages", n)
	}
	return n
}

// checkOne reads a wish's page again and records what it found.
func (s *Server) checkOne(ctx context.Context, wi *store.Wish, st store.Settings) (store.CheckResult, error) {
	priceChecks.Add(1)
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	p, err := s.fetch.Fetch(pctx, wi.URL)
	cancel()
	res := store.CheckResult{}
	if err != nil && (p == nil || p.Price == "") {
		res.Err = err.Error()
	}
	if p != nil {
		cur := p.Currency
		if cur == "" || !currency.Valid(cur) {
			cur = wi.Currency
		}
		res.Price, res.Currency, res.Availability = product.ToMinor(p.Price, currency.Decimals(cur)), cur, p.Availability
	}
	if res.Err == "" && res.Price <= 0 {
		res.Err = "no price on the page any more"
	}
	alert := s.alertFor(wi, res, st)
	topic := ""
	switch alert.kind {
	case "target", "drop":
		topic = "wishlist.price_dropped"
	case "stock":
		topic = "wishlist.back_in_stock"
	}
	after := wi
	if topic != "" {
		cp := *wi
		cp.Price, cp.Currency = res.Price, res.Currency
		after = &cp
	}
	var err2 error
	if topic != "" {
		err2 = s.store.RecordCheck(ctx, wi.ID, res, s.wishEvent(ctx, topic, after, map[string]any{"was": wi.Price, "target": wi.TargetPrice, "why": alert.kind}))
	} else {
		err2 = s.store.RecordCheck(ctx, wi.ID, res)
	}
	if err2 != nil {
		s.log.Warn("record price check", "wish", wi.ID, "err", err2)
		return res, err2
	}
	switch {
	case alert.kind == "target" || alert.kind == "drop":
		_ = s.store.SetAlerted(ctx, wi.ID, res.Price)
	case res.Err == "" && wi.AlertedPrice > 0 && res.Price > wi.AlertedPrice:
		_ = s.store.SetAlerted(ctx, wi.ID, 0) // went up again: the next drop alerts
	}
	if alert.kind != "" && st.NotifyDrops && st.NotifyURL != "" {
		link := ""
		if s.cfg.BaseURL != "" {
			link = fmt.Sprintf("%s/wishes/%d", s.cfg.BaseURL, wi.ID)
		}
		n := notify.New(st.NotifyURL, st.NotifyFormat)
		if err := n.Send(ctx, notify.Message{Title: alert.title, Body: alert.body, Priority: "success", Tags: []string{"gift", "chart_with_downwards_trend"}, Link: link}); err != nil {
			s.log.Warn("price alert", "wish", wi.ID, "err", err)
		}
	}
	return res, nil
}

type alert struct{ kind, title, body string }

func money(minor int64, cur string) string {
	d := currency.Decimals(cur)
	if d == 0 {
		return fmt.Sprintf("%d %s", minor, cur)
	}
	p := 1.0
	for i := 0; i < d; i++ {
		p *= 10
	}
	return strings.TrimSpace(fmt.Sprintf("%.*f %s", d, float64(minor)/p, cur))
}

// alertFor decides whether a check is worth telling the owner about.
func (s *Server) alertFor(wi *store.Wish, r store.CheckResult, st store.Settings) alert {
	if r.Err != "" || r.Price <= 0 {
		return alert{}
	}
	sameCur := r.Currency == wi.Currency || wi.Currency == ""
	fresh := wi.AlertedPrice == 0 || r.Price < wi.AlertedPrice
	if sameCur && fresh && wi.TargetPrice > 0 && r.Price <= wi.TargetPrice && (wi.Price == 0 || wi.Price > wi.TargetPrice || wi.AlertedPrice > 0 || r.Price < wi.Price) {
		return alert{"target", "Price target reached: " + wi.Title,
			fmt.Sprintf("%s is now %s at %s (your target %s).", wi.Title, money(r.Price, r.Currency), wi.Store, money(wi.TargetPrice, r.Currency))}
	}
	if sameCur && fresh && wi.Price > 0 && r.Price < wi.Price && st.DropPercent > 0 && (wi.Price-r.Price)*100 >= int64(st.DropPercent)*wi.Price {
		pct := (wi.Price - r.Price) * 100 / wi.Price
		return alert{"drop", fmt.Sprintf("−%d%%: %s", pct, wi.Title),
			fmt.Sprintf("%s dropped from %s to %s at %s.", wi.Title, money(wi.Price, wi.Currency), money(r.Price, r.Currency), wi.Store)}
	}
	if wi.Availability == "out_of_stock" && r.Availability == "in_stock" {
		return alert{"stock", "Back in stock: " + wi.Title, fmt.Sprintf("%s is available again at %s for %s.", wi.Title, wi.Store, money(r.Price, r.Currency))}
	}
	return alert{}
}

// handleCheckNow reads one wish's page right away ("Check price now").
func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "check")
		return
	}
	if wi.URL == "" {
		writeErr(w, http.StatusBadRequest, "this wish has no link to check")
		return
	}
	st, _ := s.store.GetSettings(ctx, wi.UserID)
	res, err := s.checkOne(ctx, wi, st)
	if err != nil {
		s.fail(w, err, "check")
		return
	}
	got, _ := s.ownerWish(ctx, wi.ID)
	prices, _ := s.store.PriceHistory(ctx, wi.ID)
	writeJSON(w, http.StatusOK, map[string]any{"wish": got, "prices": prices, "error": res.Err})
}

// handleRunChecks runs a sweep now (Settings → Price checks → Check all now).
func (s *Server) handleRunChecks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	ws, err := s.store.ListWishes(ctx, uid, store.WishFilter{Tracked: true, Sort: "checked"})
	if err != nil {
		s.fail(w, err, "checks")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	checked, failed := 0, 0
	for i := range ws {
		if ws[i].URL == "" || i >= 25 {
			continue
		}
		res, err := s.checkOne(ctx, &ws[i], st)
		checked++
		if err != nil || res.Err != "" {
			failed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"checked": checked, "failed": failed})
}

// ---- occasions ----

// Occasion is something coming up that gifts are for: a gift list's date,
// or a birthday / anniversary from Pocket Contacts.
type Occasion struct {
	Kind      string `json:"kind"` // list | birthday | anniversary
	Title     string `json:"title"`
	Date      string `json:"date"`
	Days      int    `json:"days"`
	Age       *int   `json:"age,omitempty"`
	ListID    int64  `json:"list_id,omitempty"`
	ListName  string `json:"list_name,omitempty"`
	ContactID int64  `json:"contact_id,omitempty"`
	Person    string `json:"person"`
	Initials  string `json:"initials,omitempty"`
	Color     string `json:"color,omitempty"`
	Wanted    int    `json:"wanted"`
	Bought    int    `json:"bought"`
	Budget    int64  `json:"budget"`
	Spent     int64  `json:"spent"`
	Currency  string `json:"currency"`
}

var recurringWords = []string{"birthday", "rođendan", "anniversary", "godišnjica", "christmas", "božić", "name day", "imendan", "valentine", "mother", "father", "slava", "new year", "nova godina"}

// nextDate rolls a yearly occasion forward to its next date (today counts).
func nextDate(date, occasion string, today time.Time) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", date, today.Location())
	if err != nil {
		return t, false
	}
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	if !t.Before(day) {
		return t, true
	}
	o := strings.ToLower(occasion)
	for _, wrd := range recurringWords {
		if strings.Contains(o, wrd) {
			for t.Before(day) {
				t = t.AddDate(1, 0, 0)
			}
			return t, true
		}
	}
	return t, false
}

// occasions lists what is coming up for a person in the next days; the
// second value says why Contacts' birthdays are missing ("" = included).
func (s *Server) occasions(ctx context.Context, uid int64, days int) ([]Occasion, string) {
	if days <= 0 {
		days = 30
	}
	today := time.Now()
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	end := day.AddDate(0, 0, days)
	lists, _ := s.store.ListLists(ctx, uid, false)
	out := []Occasion{}
	byContact := map[int64]store.List{}
	for _, l := range lists {
		if l.Kind == store.KindGifts && l.ContactID != nil {
			byContact[*l.ContactID] = l
		}
		if l.OccasionDate == "" {
			continue
		}
		t, ok := nextDate(l.OccasionDate, l.Occasion, today)
		if !ok || t.Before(day) || !t.Before(end) {
			continue
		}
		title := firstNonEmpty(l.Occasion, l.Name)
		person := l.Recipient
		if l.Kind == store.KindMine {
			person = ""
		}
		o := Occasion{Kind: "list", Title: title, Date: t.Format("2006-01-02"), Days: int(t.Sub(day).Hours() / 24), ListID: l.ID, ListName: l.Name,
			Person: person, Wanted: l.Wanted, Bought: l.Bought, Budget: l.Budget, Spent: l.Spent, Currency: l.Currency}
		if l.ContactID != nil {
			o.ContactID = *l.ContactID
		}
		out = append(out, o)
	}
	why := ""
	cl := s.clients(ctx, uid)
	if c, err := cl.conn(ctx, appContacts, scopeContactsRead); err != nil {
		why = err.Error()
	} else {
		occ, err := c.client.Birthdays(ctx, days)
		if err != nil {
			why = "Pocket Contacts: " + err.Error()
		}
		for _, b := range occ {
			l, has := byContact[b.Contact.ID]
			dup := false
			for _, o := range out {
				if has && o.ListID == l.ID && o.Date == b.Date {
					dup = true
				}
			}
			if dup {
				continue
			}
			title := "Birthday"
			if b.Kind == "anniversary" {
				title = "Anniversary"
			}
			o := Occasion{Kind: b.Kind, Title: title, Date: b.Date, Days: b.Days, Age: b.Age, ContactID: b.Contact.ID, Person: b.Contact.FN,
				Initials: b.Contact.Initials, Color: b.Contact.Color}
			if has {
				o.ListID, o.ListName, o.Wanted, o.Bought, o.Budget, o.Spent, o.Currency = l.ID, l.Name, l.Wanted, l.Bought, l.Budget, l.Spent, l.Currency
			}
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Person < out[j].Person
	})
	return out, why
}

func (s *Server) handleOccasions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	days := queryInt(r, "days")
	if days <= 0 {
		days = st.OccasionDays
	}
	occ, why := s.occasions(ctx, actorID(ctx), min(days, 366))
	writeJSON(w, http.StatusOK, map[string]any{"occasions": occ, "contacts": why == "", "contacts_detail": why})
}

// maybeRemind sends each person, once a day after their hour, a reminder of
// gift occasions 14, 7, 3 and 1 days ahead (and on the day).
func (s *Server) maybeRemind(ctx context.Context) {
	uids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	day := now.Format("2006-01-02")
	for _, uid := range uids {
		st, _ := s.store.GetSettings(ctx, uid)
		if st.NotifyURL == "" || !st.NotifyOccasions || now.Hour() < st.NotifyHour {
			continue
		}
		key := fmt.Sprintf("remind:last_day:%d", uid)
		s.notifyMu.Lock()
		last := s.notifyDay[uid]
		if last == "" {
			if v, _, ok := s.store.CacheGet(ctx, key); ok {
				last = v
			}
		}
		if last == day {
			s.notifyDay[uid] = last
			s.notifyMu.Unlock()
			continue
		}
		s.notifyDay[uid] = day
		s.notifyMu.Unlock()
		_ = s.store.CachePut(ctx, key, day)
		if _, err := s.sendReminder(ctx, uid, st, false); err != nil {
			s.log.Warn("occasion reminder", "user", uid, "err", err)
		}
	}
}

func (s *Server) sendReminder(ctx context.Context, uid int64, st store.Settings, force bool) (int, error) {
	occ, _ := s.occasions(ctx, uid, 15)
	var lines []string
	for _, o := range occ {
		if !force && o.Days != 14 && o.Days != 7 && o.Days != 3 && o.Days != 1 && o.Days != 0 {
			continue
		}
		when := fmt.Sprintf("in %d days", o.Days)
		switch o.Days {
		case 0:
			when = "today"
		case 1:
			when = "tomorrow"
		}
		who := firstNonEmpty(o.Person, o.ListName)
		l := fmt.Sprintf("%s · %s %s", who, o.Title, when)
		switch {
		case o.ListID == 0:
			l += " — no gift ideas yet"
		case o.Bought > 0:
			l += fmt.Sprintf(" — %d bought, %d ideas left", o.Bought, o.Wanted)
		case o.Wanted > 0:
			l += fmt.Sprintf(" — %d ideas, nothing bought yet", o.Wanted)
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return 0, nil
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = s.cfg.BaseURL + "/occasions"
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Gifts coming up", Body: strings.Join(lines, "\n"), Priority: "info", Tags: []string{"gift"}, Link: link}); err != nil {
		return 0, err
	}
	return len(lines), nil
}
