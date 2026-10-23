package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/countries"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the tokens
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "home_currency": st.HomeCurrency, "date_format": st.DateFormat, "time_format": st.TimeFormat,
		"price_check_hours": st.PriceCheckHours, "drop_percent": st.DropPercent, "occasion_days": st.OccasionDays,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_drops": st.NotifyDrops, "notify_occasions": st.NotifyOccasions,
		"notify_hour": st.NotifyHour, "notify_configured": st.NotifyURL != "",
		"inventory_url": st.InventoryURL, "inventory_token": mask(st.InventoryToken), "inventory_location": st.InventoryLocation,
		"inventory_category": st.InventoryCategory, "inventory_tag": st.InventoryTag,
		"money_url": st.MoneyURL, "money_token": mask(st.MoneyToken), "money_account_id": st.MoneyAccountID, "money_category_id": st.MoneyCategoryID,
		"split_url": st.SplitURL, "split_token": mask(st.SplitToken), "split_public_url": st.SplitPublicURL, "split_group_id": st.SplitGroupID,
		"split_member_id": st.SplitMemberID, "split_category": st.SplitCategory,
		"contacts_url": st.ContactsURL, "contacts_token": mask(st.ContactsToken), "contacts_public_url": st.ContactsPublicURL,
		"bought_inventory": st.BoughtInventory, "bought_money": st.BoughtMoney,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel,
		"openai_base_url": st.OpenAIBaseURL, "search_url": st.SearchURL, "search_default": s.cfg.SearchURL, "deal_markets": st.DealMarkets,
		"deal_abroad": st.DealAbroad, "hunt_every_days": st.HuntEveryDays,
		"deliver_to": st.DeliverTo, "deliver_to_name": countries.Name(st.DeliverTo), "hide_no_ship": st.HideNoShip,
		"max_upload_bytes": s.cfg.MaxUploadBytes, "setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings (preferences,
// notifications, their own connections). The box-wide title is only taken
// from an admin and silently ignored for everyone else.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in store.Settings
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["home_currency"] {
		if !currency.Valid(in.HomeCurrency) {
			writeErr(w, http.StatusBadRequest, "unknown currency")
			return
		}
		cur.HomeCurrency = strings.ToUpper(in.HomeCurrency)
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	intIn := func(k string, dst *int, v, lo, hi int) {
		if keys[k] && v >= lo && v <= hi {
			*dst = v
		}
	}
	intIn("price_check_hours", &cur.PriceCheckHours, in.PriceCheckHours, 0, 24*14)
	intIn("drop_percent", &cur.DropPercent, in.DropPercent, 0, 90)
	intIn("occasion_days", &cur.OccasionDays, in.OccasionDays, 1, 366)
	intIn("notify_hour", &cur.NotifyHour, in.NotifyHour, 0, 23)
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	idIn := func(k string, dst *int64, v int64) {
		if keys[k] {
			*dst = max(0, v)
		}
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	boolIn("notify_drops", &cur.NotifyDrops, in.NotifyDrops)
	boolIn("notify_occasions", &cur.NotifyOccasions, in.NotifyOccasions)
	url("inventory_url", &cur.InventoryURL, in.InventoryURL)
	secret("inventory_token", &cur.InventoryToken, in.InventoryToken)
	text("inventory_location", &cur.InventoryLocation, in.InventoryLocation)
	text("inventory_category", &cur.InventoryCategory, in.InventoryCategory)
	if keys["inventory_tag"] {
		cur.InventoryTag = store.JoinTags(in.InventoryTag)
	}
	url("money_url", &cur.MoneyURL, in.MoneyURL)
	secret("money_token", &cur.MoneyToken, in.MoneyToken)
	idIn("money_account_id", &cur.MoneyAccountID, in.MoneyAccountID)
	idIn("money_category_id", &cur.MoneyCategoryID, in.MoneyCategoryID)
	url("split_url", &cur.SplitURL, in.SplitURL)
	secret("split_token", &cur.SplitToken, in.SplitToken)
	url("split_public_url", &cur.SplitPublicURL, in.SplitPublicURL)
	idIn("split_group_id", &cur.SplitGroupID, in.SplitGroupID)
	idIn("split_member_id", &cur.SplitMemberID, in.SplitMemberID)
	if keys["split_category"] && strings.TrimSpace(in.SplitCategory) != "" {
		cur.SplitCategory = strings.TrimSpace(in.SplitCategory)
	}
	url("contacts_url", &cur.ContactsURL, in.ContactsURL)
	secret("contacts_token", &cur.ContactsToken, in.ContactsToken)
	url("contacts_public_url", &cur.ContactsPublicURL, in.ContactsPublicURL)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			writeErr(w, http.StatusBadRequest, "AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			writeErr(w, http.StatusBadRequest, "unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	url("search_url", &cur.SearchURL, in.SearchURL)
	text("deal_markets", &cur.DealMarkets, in.DealMarkets)
	boolIn("deal_abroad", &cur.DealAbroad, in.DealAbroad)
	intIn("hunt_every_days", &cur.HuntEveryDays, in.HuntEveryDays, 1, 60)
	if keys["deliver_to"] {
		c := strings.ToUpper(strings.TrimSpace(in.DeliverTo))
		if c != "" && !countries.Valid(c) {
			writeErr(w, http.StatusBadRequest, "unknown country")
			return
		}
		cur.DeliverTo = c
	}
	boolIn("hide_no_ship", &cur.HideNoShip, in.HideNoShip)
	boolIn("bought_inventory", &cur.BoughtInventory, in.BoughtInventory)
	boolIn("bought_money", &cur.BoughtMoney, in.BoughtMoney)
	mode := func(k string, dst *string, v string) {
		if keys[k] && (v == modeCore || v == modeDirect || v == modeOff) {
			*dst = v
		}
	}
	mode("inventory_mode", &cur.InventoryMode, in.InventoryMode)
	mode("money_mode", &cur.MoneyMode, in.MoneyMode)
	mode("split_mode", &cur.SplitMode, in.SplitMode)
	mode("contacts_mode", &cur.ContactsMode, in.ContactsMode)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, k := range modeKeys {
		if keys[k] {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return in.URL
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		// a token typed into the form is tried as is; otherwise the saved mode decides
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, actorID(ctx)).conn(ctx, a, a.scopes[0])
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				switch a.slug {
				case "inventory":
					var ls []pocket.Named
					ls, err = conn.client.Locations(ctx)
					msg = fmt.Sprintf("Pocket Inventory %s, %s, %d places", v, how, len(ls))
				case "money":
					var as []pocket.Account
					as, err = conn.client.Accounts(ctx)
					msg = fmt.Sprintf("Pocket Money %s, %s, %d accounts to book on", v, how, len(as))
				case "split":
					var gs []pocket.Group
					gs, err = conn.client.Groups(ctx)
					msg = fmt.Sprintf("Pocket Split %s, %s, %d groups", v, how, len(gs))
				case "contacts":
					var bs []pocket.Occasion
					bs, err = conn.client.Birthdays(ctx, 60)
					msg = fmt.Sprintf("Pocket Contacts %s, %s, %d birthdays in the next 60 days", v, how, len(bs))
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Wishlist", Body: "Notifications work. Price drops and upcoming gift occasions will arrive here.", Priority: "info", Tags: []string{"gift"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n, err := s.sendReminder(ctx, actorID(ctx), st, true)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": n})
}

// ---- dashboard (Homepage widget), home page, Pocket Day ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	wanted, _ := s.store.ListWishes(ctx, uid, store.WishFilter{})
	var value int64
	drops, tracked, gifts := 0, 0, 0
	for _, wi := range wanted {
		if wi.Currency == st.HomeCurrency || wi.Currency == "" {
			value += wi.Price * int64(max(wi.Quantity, 1))
		}
		if wi.AtTarget {
			drops++
		}
		if wi.TrackPrice {
			tracked++
		}
		if wi.ListKind == store.KindGifts {
			gifts++
		}
	}
	occ, _ := s.occasions(ctx, uid, st.OccasionDays)
	next := "—"
	if len(occ) > 0 {
		o := occ[0]
		next = fmt.Sprintf("%s · %s", firstNonEmpty(o.Person, o.ListName), o.Title)
		switch o.Days {
		case 0:
			next += " today"
		case 1:
			next += " tomorrow"
		default:
			next += fmt.Sprintf(" in %d days", o.Days)
		}
	}
	reserved, _ := s.store.MyReservations(ctx, uid)
	open := 0
	for _, rv := range reserved {
		if !rv.Bought {
			open++
		}
	}
	lists, _ := s.store.ListLists(ctx, uid, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"wishes": len(wanted) - gifts, "gift_ideas": gifts, "lists": len(lists), "at_target": drops, "tracked": tracked,
		"value": major(value, st.HomeCurrency), "value_text": money(value, st.HomeCurrency), "currency": st.HomeCurrency,
		"occasions": len(occ), "next_occasion": next, "to_buy_for_others": open,
	})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	lists, _ := s.store.ListLists(ctx, uid, false)
	show := showReservedMap(lists)
	for i := range lists {
		s.ownerList(ctx, &lists[i])
	}
	drops, _ := s.store.ListWishes(ctx, uid, store.WishFilter{AtTarget: true, Sort: "priority", Limit: 12})
	recent, _ := s.store.ListWishes(ctx, uid, store.WishFilter{Sort: "added", Limit: 8})
	tracked, _ := s.store.ListWishes(ctx, uid, store.WishFilter{Tracked: true, Sort: "checked"})
	// cheaper than when added, but not (yet) at a target
	var cheaper []store.Wish
	for _, wi := range tracked {
		if wi.Change < 0 && !wi.AtTarget {
			cheaper = append(cheaper, wi)
		}
	}
	sort.SliceStable(cheaper, func(i, j int) bool {
		return cheaper[i].Change*100/max(1, cheaper[i].Price-cheaper[i].Change) < cheaper[j].Change*100/max(1, cheaper[j].Price-cheaper[j].Change)
	})
	if len(cheaper) > 8 {
		cheaper = cheaper[:8]
	}
	if cheaper == nil {
		cheaper = []store.Wish{}
	}
	bought, _ := s.store.ListWishes(ctx, uid, store.WishFilter{Status: store.StatusBought, Sort: "bought", Limit: 6})
	for _, ws := range [][]store.Wish{drops, recent, cheaper, bought} {
		ownerView(ws, show)
	}
	occ, why := s.occasions(ctx, uid, st.OccasionDays)
	reserved, _ := s.store.MyReservations(ctx, uid)
	shared, _ := s.store.SharedWithMe(ctx, uid)
	for i := range shared {
		if shared[i].CoverID > 0 {
			shared[i].Cover = photoURL(sharedBase(shared[i].ID), shared[i].CoverID, shared[i].UpdatedAt)
		}
		shared[i].Role, shared[i].Spent, shared[i].Budget = "viewer", 0, 0
	}
	for i := range reserved {
		reserved[i].Photo = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lists": lists, "at_target": drops, "cheaper": cheaper, "recent": recent, "bought": bought,
		"occasions": occ, "contacts": why == "", "contacts_detail": why, "reservations": reserved, "shared": shared,
	})
}

// handleDay is one person's wishlist day for Pocket Day: what they added and bought.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day := chi.URLParam(r, "date")
	if !validDate(day) {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	added, bought, err := s.store.WishesOnDay(ctx, actorID(ctx), day)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	lists, _ := s.store.ListLists(ctx, actorID(ctx), false)
	ownerView(added, showReservedMap(lists))
	ownerView(bought, showReservedMap(lists))
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "added": added, "bought": bought})
}

func (s *Server) handleCurrencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currency.All())
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.Tags(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tc{}
	for t, n := range tags {
		out = append(out, tc{t, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	writeJSON(w, http.StatusOK, out)
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size, files int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	_ = filepath.WalkDir(s.cfg.FilesDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				files += fi.Size()
			}
		}
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size, "files_bytes": files,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "price_checks": priceChecks.Load(),
	})
}

// handleBackup downloads the whole database, so it is a box operation for an
// admin only (everyone can export their own data as JSON).
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-wishlist-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's lists (no users, tokens,
// shares or reservations: those are about other people).
type exportDoc struct {
	App      string                        `json:"app"`
	Version  string                        `json:"version"`
	Exported string                        `json:"exported_at"`
	Lists    []store.List                  `json:"lists"`
	Wishes   []store.Wish                  `json:"wishes"`
	Prices   map[string][]store.PricePoint `json:"prices"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-wishlist", Version: Version, Exported: store.Now(), Prices: map[string][]store.PricePoint{}}
	doc.Lists, _ = s.store.ListLists(ctx, uid, false)
	arch, _ := s.store.ListLists(ctx, uid, true)
	doc.Lists = append(doc.Lists, arch...)
	doc.Wishes, _ = s.store.ListWishes(ctx, uid, store.WishFilter{Status: "all", ListID: 0})
	for _, l := range arch {
		ws, _ := s.store.ListWishes(ctx, uid, store.WishFilter{Status: "all", ListID: l.ID})
		doc.Wishes = append(doc.Wishes, ws...)
	}
	for i := range doc.Lists {
		doc.Lists[i].ShareURL, doc.Lists[i].Reserved = "", 0
	}
	for i := range doc.Wishes {
		doc.Wishes[i].Reserved = 0
		p, _ := s.store.PriceHistory(ctx, doc.Wishes[i].ID)
		doc.Prices[fmt.Sprint(doc.Wishes[i].ID)] = p
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-wishlist-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleImportJSON adds an export to the caller's own lists: lists by name
// and kind (merged when one exists), wishes by list and title.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{}
	existing, _ := s.store.ListLists(ctx, uid, false)
	key := func(l store.List) string { return l.Kind + "\x00" + strings.ToLower(l.Name) }
	byKey := map[string]int64{}
	for _, l := range existing {
		byKey[key(l)] = l.ID
	}
	listIDs := map[int64]int64{}
	for _, l := range doc.Lists {
		if id, ok := byKey[key(l)]; ok {
			listIDs[l.ID] = id
			continue
		}
		old := l.ID
		if err := s.store.CreateList(ctx, uid, &l); err == nil {
			if l.Archived {
				_ = s.store.UpdateList(ctx, uid, &l)
			}
			listIDs[old] = l.ID
			byKey[key(l)] = l.ID
			counts["lists"]++
		}
	}
	have := map[string]bool{}
	mine, _ := s.store.ListWishes(ctx, uid, store.WishFilter{Status: "all"})
	for _, wi := range mine {
		have[fmt.Sprintf("%d\x00%s", wi.ListID, strings.ToLower(wi.Title))] = true
	}
	for _, wi := range doc.Wishes {
		lid, ok := listIDs[wi.ListID]
		if !ok || have[fmt.Sprintf("%d\x00%s", lid, strings.ToLower(wi.Title))] {
			continue
		}
		status, bAt, bPrice, bCur, bStore := wi.Status, wi.BoughtAt, wi.BoughtPrice, wi.BoughtCurrency, wi.BoughtStore
		wi.ID, wi.ListID, wi.PhotoPath, wi.Status = 0, lid, "", store.StatusWanted
		wi.InventoryItemID, wi.MoneyTxID, wi.SplitExpenseID, wi.SplitGroupID = nil, nil, nil, nil
		if err := s.store.CreateWish(ctx, uid, &wi); err != nil {
			continue
		}
		counts["wishes"]++
		if status == store.StatusBought {
			_ = s.store.MarkBought(ctx, uid, wi.ID, bAt, bPrice, bCur, bStore)
		} else if status == store.StatusArchived {
			_ = s.store.SetStatus(ctx, uid, wi.ID, store.StatusArchived)
		}
		if wi.ImageURL != "" {
			s.fetchPhoto(ctx, uid, wi.ID, wi.ImageURL)
		}
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) handleCountries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, countries.All())
}
