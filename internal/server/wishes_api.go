package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/product"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

func filterFrom(r *http.Request) store.WishFilter {
	q := r.URL.Query()
	return store.WishFilter{
		ListID:   int64(queryInt(r, "list")),
		Status:   q.Get("status"),
		Query:    q.Get("q"),
		Tag:      q.Get("tag"),
		Kind:     q.Get("kind"),
		AtTarget: queryBool(r, "at_target"),
		Tracked:  queryBool(r, "tracked"),
		Sort:     q.Get("sort"),
		Limit:    queryInt(r, "limit"),
	}
}

func (s *Server) handleListWishes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ws, err := s.store.ListWishes(ctx, actorID(ctx), filterFrom(r))
	if err != nil {
		s.fail(w, err, "wishes")
		return
	}
	lists, _ := s.store.ListLists(ctx, actorID(ctx), false)
	ownerView(ws, showReservedMap(lists))
	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) ownerWish(ctx context.Context, id int64) (*Wish, error) {
	wi, err := s.store.GetWish(ctx, actorID(ctx), id)
	if err != nil {
		return nil, err
	}
	show := false
	if l, err := s.store.GetList(ctx, actorID(ctx), wi.ListID); err == nil {
		show = l.ShowReserved
	}
	one := []Wish{*wi}
	ownerView(one, map[int64]bool{wi.ListID: show})
	return &one[0], nil
}

func (s *Server) handleGetWish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.ownerWish(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "wish")
		return
	}
	prices, _ := s.store.PriceHistory(ctx, wi.ID)
	writeJSON(w, http.StatusOK, map[string]any{"wish": wi, "prices": prices})
}

func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "prices")
		return
	}
	prices, err := s.store.PriceHistory(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "prices")
		return
	}
	writeJSON(w, http.StatusOK, prices)
}

// wishInput is a create or a partial update: fields left out stay as they are.
type wishInput struct {
	ListID      *int64  `json:"list_id"`
	Title       *string `json:"title"`
	URL         *string `json:"url"`
	Store       *string `json:"store"`
	Notes       *string `json:"notes"`
	Tags        *string `json:"tags"`
	ImageURL    *string `json:"image_url"`
	Price       *int64  `json:"price"`
	Currency    *string `json:"currency"`
	TargetPrice *int64  `json:"target_price"`
	Priority    *int    `json:"priority"`
	Quantity    *int    `json:"quantity"`
	TrackPrice  *bool   `json:"track_price"`
	Status      *string `json:"status"`
	// a price as written ("12,99") instead of minor units — handy for Shortcuts
	PriceText  *string `json:"price_text"`
	TargetText *string `json:"target_text"`
	// leave the link as given (no page lookup on create)
	NoLookup bool `json:"no_lookup"`
}

func (in wishInput) apply(wi *store.Wish) string {
	trim := func(p *string) string { return strings.TrimSpace(*p) }
	if in.ListID != nil {
		wi.ListID = *in.ListID
	}
	if in.Title != nil {
		wi.Title = trim(in.Title)
	}
	if in.URL != nil {
		u := trim(in.URL)
		if u != "" {
			if _, err := product.CheckURL(u); err != nil {
				return err.Error()
			}
		}
		wi.URL = u
	}
	if in.Store != nil {
		wi.Store = trim(in.Store)
	}
	if in.Notes != nil {
		wi.Notes = trim(in.Notes)
	}
	if in.Tags != nil {
		wi.Tags = store.JoinTags(*in.Tags)
	}
	if in.ImageURL != nil {
		wi.ImageURL = trim(in.ImageURL)
	}
	if in.Currency != nil {
		c := strings.ToUpper(trim(in.Currency))
		if c != "" && !currency.Valid(c) {
			return "unknown currency " + c
		}
		wi.Currency = c
	}
	dec := currency.Decimals(wi.Currency)
	if in.Price != nil {
		wi.Price = *in.Price
	} else if in.PriceText != nil {
		wi.Price = product.ToMinor(*in.PriceText, dec)
	}
	if in.TargetPrice != nil {
		wi.TargetPrice = *in.TargetPrice
	} else if in.TargetText != nil {
		wi.TargetPrice = product.ToMinor(*in.TargetText, dec)
	}
	if in.Priority != nil {
		wi.Priority = *in.Priority
	}
	if in.Quantity != nil {
		wi.Quantity = *in.Quantity
	}
	if in.TrackPrice != nil {
		wi.TrackPrice = *in.TrackPrice
	}
	if in.Status != nil {
		switch *in.Status {
		case store.StatusWanted, store.StatusBought, store.StatusArchived:
			wi.Status = *in.Status
		default:
			return "status is wanted, bought or archived"
		}
	}
	switch {
	case wi.Title == "":
		return "a wish needs a name (or a link to read it from)"
	case len(wi.Title) > 300:
		return "the name is too long"
	case wi.Price < 0 || wi.TargetPrice < 0:
		return "prices cannot be negative"
	case wi.Priority < 1 || wi.Priority > 3:
		return "priority is 1 (nice to have), 2 (want) or 3 (must have)"
	case wi.Quantity < 1 || wi.Quantity > 999:
		return "quantity is 1 to 999"
	}
	if wi.TrackPrice && wi.URL == "" {
		wi.TrackPrice = false
	}
	if wi.Store == "" && wi.URL != "" {
		wi.Store = product.StoreName(wi.URL)
	}
	return ""
}

// defaultList is where a wish lands when no list was named: the person's
// first own wishlist, made on the spot when they have none.
func (s *Server) defaultList(ctx context.Context, uid int64) (int64, error) {
	lists, err := s.store.ListLists(ctx, uid, false)
	if err != nil {
		return 0, err
	}
	for _, l := range lists {
		if l.Kind == store.KindMine {
			return l.ID, nil
		}
	}
	st, _ := s.store.GetSettings(ctx, uid)
	l := &store.List{Name: "My wishlist", Kind: store.KindMine, Emoji: "🎁", Currency: st.HomeCurrency}
	if err := s.store.CreateList(ctx, uid, l); err != nil {
		return 0, err
	}
	return l.ID, nil
}

func (s *Server) wishEvent(ctx context.Context, topic string, wi *store.Wish, extra map[string]any) core.CommitHook {
	if !s.core.Configured() {
		return nil
	}
	u := userFrom(ctx)
	email := ""
	if u != nil && u.ID == wi.UserID {
		email = u.Email
	} else if owner, err := s.store.GetUser(ctx, wi.UserID); err == nil {
		email = owner.Email
	}
	payload := map[string]any{"user_email": email, "wish_id": wi.ID, "wish": wi.Title, "list_id": wi.ListID, "url": wi.URL,
		"price": wi.Price, "currency": wi.Currency, "store": wi.Store}
	for k, v := range extra {
		payload[k] = v
	}
	return func(tx *sql.Tx) error {
		return core.QueueEvent(ctx, tx, topic, fmt.Sprintf("wish:%d", wi.ID), payload)
	}
}

func (s *Server) handleCreateWish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in wishInput
	if _, err := decodeKeys(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	wi := &store.Wish{Priority: 2, Quantity: 1, Currency: st.HomeCurrency, Source: linkApp(ctx)}
	// a link alone is enough: read the page for name, picture and price
	var page *product.Product
	if in.URL != nil && strings.TrimSpace(*in.URL) != "" && !in.NoLookup && (in.Title == nil || in.Price == nil || in.ImageURL == nil) {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		page, _ = s.fetch.Fetch(pctx, strings.TrimSpace(*in.URL))
		cancel()
	}
	if page != nil {
		wi.Title, wi.ImageURL, wi.Store, wi.Availability = page.Title, page.Image, page.Store, page.Availability
		if page.Currency != "" && currency.Valid(page.Currency) {
			wi.Currency = page.Currency
		}
		wi.Price = product.ToMinor(page.Price, currency.Decimals(wi.Currency))
		if wi.Price > 0 && in.TrackPrice == nil {
			wi.TrackPrice = true
		}
	}
	if msg := in.apply(wi); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if wi.ListID == 0 {
		id, err := s.defaultList(ctx, uid)
		if err != nil {
			s.fail(w, err, "create wish")
			return
		}
		wi.ListID = id
	}
	if err := s.store.CreateWish(ctx, uid, wi, s.wishEvent(ctx, "wishlist.wish_added", wi, nil)); err != nil {
		s.fail(w, err, "create wish")
		return
	}
	if wi.ImageURL != "" {
		s.fetchPhoto(ctx, uid, wi.ID, wi.ImageURL)
	}
	got, err := s.ownerWish(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "create wish")
		return
	}
	writeJSON(w, http.StatusCreated, got)
}

// mayChange: a linked app changes only the wishes it added itself.
func mayChange(ctx context.Context, wi *store.Wish) bool {
	app := linkApp(ctx)
	return app == "" || wi.Source == app
}

func (s *Server) handleUpdateWish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wi, err := s.store.GetWish(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "wish")
		return
	}
	if !mayChange(ctx, wi) {
		writeErr(w, http.StatusForbidden, "a linked app may only change the wishes it added")
		return
	}
	var in wishInput
	if _, err := decodeKeys(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	oldImage := wi.ImageURL
	if msg := in.apply(wi); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateWish(ctx, uid, wi); err != nil {
		s.fail(w, err, "update wish")
		return
	}
	if wi.ImageURL != "" && wi.ImageURL != oldImage {
		s.fetchPhoto(ctx, uid, wi.ID, wi.ImageURL)
	}
	got, err := s.ownerWish(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "update wish")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteWish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "wish")
		return
	}
	if !mayChange(ctx, wi) {
		writeErr(w, http.StatusForbidden, "a linked app may only delete the wishes it added")
		return
	}
	photo, err := s.store.DeleteWish(ctx, actorID(ctx), wi.ID)
	if err != nil {
		s.fail(w, err, "delete wish")
		return
	}
	s.removeFile(photo)
	w.WriteHeader(http.StatusNoContent)
}

// handleSetStatus moves a wish back to wanted, to bought without the
// hand-off (already filed elsewhere), or into the archive.
func (s *Server) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Status {
	case store.StatusWanted, store.StatusArchived:
		if err := s.store.SetStatus(ctx, actorID(ctx), idParam(r, "id"), in.Status); err != nil {
			s.fail(w, err, "status")
			return
		}
	case store.StatusBought:
		wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
		if err != nil {
			s.fail(w, err, "status")
			return
		}
		if err := s.store.MarkBought(ctx, actorID(ctx), wi.ID, store.Today(), wi.Price, wi.Currency, wi.Store); err != nil {
			s.fail(w, err, "status")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "status is wanted, bought or archived")
		return
	}
	got, err := s.ownerWish(ctx, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "status")
		return
	}
	writeJSON(w, http.StatusOK, got)
}

// handlePreview reads a product page for the "Add from a link" form.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	p, err := s.fetch.Fetch(ctx, in.URL)
	if p == nil {
		msg := "could not read that page"
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": msg, "store": product.StoreName(in.URL)})
		return
	}
	st, _ := s.store.GetSettings(r.Context(), actorID(r.Context()))
	cur := p.Currency
	if cur == "" || !currency.Valid(cur) {
		cur = st.HomeCurrency
	}
	out := map[string]any{"ok": true, "product": p, "currency": cur, "price": product.ToMinor(p.Price, currency.Decimals(cur))}
	if err != nil {
		out["warning"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- photos ----

// fetchPhoto keeps a local copy of a wish's picture, so lists still show it
// when the shop changes its page (and no third party sees who looks at it).
func (s *Server) fetchPhoto(ctx context.Context, uid, id int64, imageURL string) {
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, ext, err := s.fetch.Image(pctx, imageURL, 8<<20)
	if err != nil {
		s.log.Debug("wish picture", "wish", id, "url", imageURL, "err", err)
		return
	}
	rel, err := s.saveFile("photos", ext, b)
	if err != nil {
		s.log.Warn("save picture", "err", err)
		return
	}
	old, err := s.store.SetPhoto(ctx, uid, id, rel, imageURL)
	if err != nil {
		s.removeFile(rel)
		return
	}
	if old != rel {
		s.removeFile(old)
	}
}

func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	wi, err := s.store.GetWish(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	s.serveFile(w, r, wi.PhotoPath)
}

func (s *Server) handleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	b, name, ct, err := s.readUpload(r, "file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "upload: "+err.Error())
		return
	}
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(b)
	}
	if !strings.HasPrefix(ct, "image/") {
		writeErr(w, http.StatusBadRequest, "that is not a picture")
		return
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif", "image/heic": ".heic", "image/avif": ".avif"}[strings.Split(ct, ";")[0]]
	if ext == "" {
		ext = ".img"
		if i := strings.LastIndex(name, "."); i >= 0 {
			ext = name[i:]
		}
	}
	rel, err := s.saveFile("photos", ext, b)
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	old, err := s.store.SetPhoto(ctx, actorID(ctx), wi.ID, rel, "")
	if err != nil {
		s.removeFile(rel)
		s.fail(w, err, "photo")
		return
	}
	s.removeFile(old)
	got, _ := s.ownerWish(ctx, wi.ID)
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	old, err := s.store.SetPhoto(ctx, actorID(ctx), idParam(r, "id"), "", "")
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	s.removeFile(old)
	w.WriteHeader(http.StatusNoContent)
}
