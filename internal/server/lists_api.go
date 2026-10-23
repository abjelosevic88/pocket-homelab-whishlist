package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// ---- photo addresses: each viewer reaches a picture through their own door ----

// photoBase is the path prefix under which a viewer fetches wish photos:
// the owner "/api/v1/wishes", a person it is shared with
// "/api/v1/shared/<list>/wishes", a link visitor "/api/v1/s/<token>/wishes".
func ownerBase() string              { return "/api/v1/wishes" }
func sharedBase(listID int64) string { return fmt.Sprintf("/api/v1/shared/%d/wishes", listID) }
func linkBase(token string) string   { return "/api/v1/s/" + token + "/wishes" }
func photoURL(base string, id int64, v string) string {
	return fmt.Sprintf("%s/%d/photo?v=%s", base, id, strings.NewReplacer(":", "", ".", "", "-", "").Replace(v))
}

// ownerView prepares wishes for their owner: reservations are a surprise,
// so the owner sees at most that something is taken (when the list says so).
func ownerView(ws []Wish, showReserved map[int64]bool) {
	for i := range ws {
		w := &ws[i]
		if w.PhotoPath != "" {
			w.Photo = photoURL(ownerBase(), w.ID, w.UpdatedAt)
		}
		if !showReserved[w.ListID] || w.ListKind != store.KindMine {
			w.Reserved = 0
		}
		w.Mine, w.MineNote, w.MineID, w.MineBuy = 0, "", 0, false
	}
}

// Wish is the store's wish (alias keeps handler signatures short).
type Wish = store.Wish

// viewerView prepares a shared list's wishes for a gift-giver: no purchase
// details from the owner's other apps, no private notes about money flows.
func viewerView(ws []Wish, base string) {
	for i := range ws {
		w := &ws[i]
		if w.PhotoPath != "" {
			w.Photo = photoURL(base, w.ID, w.UpdatedAt)
		}
		w.InventoryItemID, w.MoneyTxID, w.SplitExpenseID, w.SplitGroupID = nil, nil, nil, nil
		w.HandoffNote, w.BoughtPrice, w.BoughtCurrency, w.BoughtStore = "", 0, "", ""
		w.TrackPrice, w.CheckError, w.Source, w.TargetPrice = false, "", "", 0
	}
}

func (s *Server) ownerList(ctx context.Context, l *store.List) {
	l.Role = "owner"
	if !l.ShowReserved || l.Kind != store.KindMine {
		l.Reserved = 0
	}
	if l.CoverID > 0 {
		l.Cover = photoURL(ownerBase(), l.CoverID, l.UpdatedAt)
	}
	if t := s.store.LinkToken(ctx, l.ID); t != "" {
		l.ShareURL = s.shareURL(t)
	}
}

func (s *Server) shareURL(token string) string {
	return s.cfg.BaseURL + "/s/" + token
}

func showReservedMap(lists []store.List) map[int64]bool {
	m := map[int64]bool{}
	for _, l := range lists {
		m[l.ID] = l.ShowReserved
	}
	return m
}

// ---- lists ----

func (s *Server) handleListLists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lists, err := s.store.ListLists(ctx, actorID(ctx), queryBool(r, "archived"))
	if err != nil {
		s.fail(w, err, "lists")
		return
	}
	for i := range lists {
		s.ownerList(ctx, &lists[i])
	}
	writeJSON(w, http.StatusOK, lists)
}

type listInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	Kind         string  `json:"kind"`
	Emoji        *string `json:"emoji"`
	Color        *string `json:"color"`
	Recipient    *string `json:"recipient"`
	ContactID    *int64  `json:"contact_id"`
	Occasion     *string `json:"occasion"`
	OccasionDate *string `json:"occasion_date"`
	Budget       *int64  `json:"budget"`
	Currency     *string `json:"currency"`
	ShowReserved *bool   `json:"show_reserved"`
	Archived     *bool   `json:"archived"`
}

// apply merges the fields that were sent onto l; "" = fine.
func (in listInput) apply(l *store.List, keys map[string]bool) string {
	str := func(p *string) string { return strings.TrimSpace(*p) }
	if in.Name != nil {
		l.Name = str(in.Name)
	}
	if in.Description != nil {
		l.Description = str(in.Description)
	}
	if in.Emoji != nil {
		l.Emoji = str(in.Emoji)
	}
	if in.Color != nil {
		l.Color = str(in.Color)
	}
	if in.Recipient != nil {
		l.Recipient = str(in.Recipient)
	}
	if keys["contact_id"] {
		l.ContactID = in.ContactID
		if l.ContactID != nil && *l.ContactID <= 0 {
			l.ContactID = nil
		}
	}
	if in.Occasion != nil {
		l.Occasion = str(in.Occasion)
	}
	if in.OccasionDate != nil {
		if d := str(in.OccasionDate); d != "" && !validDate(d) {
			return "occasion_date must be YYYY-MM-DD"
		}
		l.OccasionDate = str(in.OccasionDate)
	}
	if in.Budget != nil {
		if *in.Budget < 0 {
			return "budget cannot be negative"
		}
		l.Budget = *in.Budget
	}
	if in.Currency != nil {
		c := strings.ToUpper(str(in.Currency))
		if c != "" && !currency.Valid(c) {
			return "unknown currency"
		}
		l.Currency = c
	}
	if in.ShowReserved != nil {
		l.ShowReserved = *in.ShowReserved
	}
	if in.Archived != nil {
		l.Archived = *in.Archived
	}
	if l.Name == "" {
		return "a list needs a name"
	}
	if len(l.Name) > 200 {
		return "the name is too long"
	}
	return ""
}

func (s *Server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in listInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	l := &store.List{Kind: in.Kind, Currency: st.HomeCurrency}
	if msg := in.apply(l, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateList(ctx, actorID(ctx), l); err != nil {
		s.fail(w, err, "create list")
		return
	}
	got, err := s.store.GetList(ctx, actorID(ctx), l.ID)
	if err != nil {
		s.fail(w, err, "create list")
		return
	}
	s.ownerList(ctx, got)
	writeJSON(w, http.StatusCreated, got)
}

func (s *Server) handleGetList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	s.ownerList(ctx, l)
	sort := r.URL.Query().Get("sort")
	ws, err := s.store.ListWishesOn(ctx, l.ID, sort)
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	ownerView(ws, map[int64]bool{l.ID: l.ShowReserved})
	writeJSON(w, http.StatusOK, map[string]any{"list": l, "wishes": ws})
}

func (s *Server) handleUpdateList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	var in listInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := in.apply(l, keys); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateList(ctx, actorID(ctx), l); err != nil {
		s.fail(w, err, "update list")
		return
	}
	got, _ := s.store.GetList(ctx, actorID(ctx), l.ID)
	s.ownerList(ctx, got)
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	photos, err := s.store.DeleteList(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete list")
		return
	}
	for _, p := range photos {
		s.removeFile(p)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderLists(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderLists(r.Context(), actorID(r.Context()), in.IDs); err != nil {
		s.fail(w, err, "reorder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReorderWishes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.store.GetList(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "list")
		return
	}
	if err := s.store.ReorderWishes(r.Context(), actorID(r.Context()), idParam(r, "id"), in.IDs); err != nil {
		s.fail(w, err, "reorder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- sharing (owner side) ----

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	shares, err := s.store.ListShares(ctx, l.ID)
	if err != nil {
		s.fail(w, err, "shares")
		return
	}
	for i := range shares {
		if shares[i].Kind == "link" {
			shares[i].URL = s.shareURL(shares[i].Token)
		}
	}
	writeJSON(w, http.StatusOK, shares)
}

// handleCreateShare makes the secret link ({"kind":"link"}) or shares with a
// person on the box ({"kind":"user","user_id":N}). Only "mine" lists can be
// shared: gift ideas for someone else stay private.
func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	l, err := s.store.GetList(ctx, me.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	if l.Kind != store.KindMine {
		writeErr(w, http.StatusBadRequest, "gift ideas stay private; only your own wishlists can be shared")
		return
	}
	var in struct {
		Kind   string `json:"kind"`
		UserID int64  `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Kind {
	case "link":
		t, err := s.store.CreateLink(ctx, l.ID)
		if err != nil {
			s.fail(w, err, "share")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"url": s.shareURL(t), "token": t})
	case "user":
		var u *store.User
		if in.UserID > 0 {
			u, err = s.store.GetUser(ctx, in.UserID)
		} else {
			u, err = s.store.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(in.Email)))
		}
		if err != nil {
			writeErr(w, http.StatusNotFound, "nobody with that account on this box")
			return
		}
		if u.ID == me.ID {
			writeErr(w, http.StatusBadRequest, "this is your own list")
			return
		}
		if err := s.store.ShareWithUser(ctx, l.ID, u.ID); err != nil {
			s.fail(w, err, "share")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusBadRequest, "kind is link or user")
	}
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	if err := s.store.DeleteShare(ctx, l.ID, idParam(r, "sid")); err != nil {
		s.fail(w, err, "unshare")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- lists shared with me (people on the box) ----

func (s *Server) handleSharedLists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lists, err := s.store.SharedWithMe(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "shared lists")
		return
	}
	for i := range lists {
		lists[i].Role = "viewer"
		if lists[i].CoverID > 0 {
			lists[i].Cover = photoURL(sharedBase(lists[i].ID), lists[i].CoverID, lists[i].UpdatedAt)
		}
		lists[i].Spent, lists[i].Budget = 0, 0
	}
	writeJSON(w, http.StatusOK, lists)
}

func (s *Server) handleSharedList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetSharedList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "shared list")
		return
	}
	s.writeViewerList(w, r, l, sharedBase(l.ID), store.Giver{UserID: actorID(ctx)})
}

// writeViewerList answers a gift-giver: the list, its wishes with how many
// are taken, and which of them this giver reserved.
func (s *Server) writeViewerList(w http.ResponseWriter, r *http.Request, l *store.List, base string, g store.Giver) {
	ctx := r.Context()
	ws, err := s.store.ListWishesOn(ctx, l.ID, r.URL.Query().Get("sort"))
	if err != nil {
		s.fail(w, err, "list")
		return
	}
	if err := s.store.MarkMine(ctx, g, ws); err != nil {
		s.fail(w, err, "list")
		return
	}
	viewerView(ws, base)
	l.Role, l.Spent, l.Budget, l.ShareURL = "viewer", 0, 0, ""
	if l.CoverID > 0 {
		l.Cover = photoURL(base, l.CoverID, l.UpdatedAt)
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l, "wishes": ws})
}

func (s *Server) handleSharedPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetSharedList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	s.servePhotoOn(w, r, l.ID, idParam(r, "wid"))
}

func (s *Server) servePhotoOn(w http.ResponseWriter, r *http.Request, listID, wishID int64) {
	wi, err := s.store.GetWishOn(r.Context(), listID, wishID)
	if err != nil {
		s.fail(w, err, "photo")
		return
	}
	s.serveFile(w, r, wi.PhotoPath)
}

type reserveInput struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Note     string `json:"note"`
	Bought   bool   `json:"bought"`
}

func (s *Server) reserve(w http.ResponseWriter, r *http.Request, l *store.List, wishID int64, g store.Giver, who string) {
	ctx := r.Context()
	var in reserveInput
	_ = decode(r, &in)
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) > 500 {
		in.Note = in.Note[:500]
	}
	wi, err := s.store.GetWishOn(ctx, l.ID, wishID)
	if err != nil {
		s.fail(w, err, "reserve")
		return
	}
	var hook core.CommitHook
	if s.core.Configured() && who != "" {
		// the giver's event: the owner must not learn about it from their journal
		hook = func(tx *sql.Tx) error {
			return core.QueueEvent(ctx, tx, "wishlist.reserved", fmt.Sprintf("wish:%d", wi.ID), map[string]any{
				"user_email": who, "wish_id": wi.ID, "wish": wi.Title, "list": l.Name, "for": l.Owner})
		}
	}
	res, err := s.store.Reserve(ctx, wi.ID, g, in.Quantity, in.Note, hook)
	if err != nil {
		s.fail(w, err, "reserve")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSharedReserve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := userFrom(ctx)
	l, err := s.store.GetSharedList(ctx, me.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "reserve")
		return
	}
	s.reserve(w, r, l, idParam(r, "wid"), store.Giver{UserID: me.ID, Name: firstNonEmpty(me.Name, me.Email)}, me.Email)
}

func (s *Server) handleSharedRelease(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetSharedList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "release")
		return
	}
	if _, err := s.store.GetWishOn(ctx, l.ID, idParam(r, "wid")); err != nil {
		s.fail(w, err, "release")
		return
	}
	if err := s.store.Release(ctx, idParam(r, "wid"), store.Giver{UserID: actorID(ctx)}); err != nil {
		s.fail(w, err, "release")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSharedBought(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l, err := s.store.GetSharedList(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "bought")
		return
	}
	if _, err := s.store.GetWishOn(ctx, l.ID, idParam(r, "wid")); err != nil {
		s.fail(w, err, "bought")
		return
	}
	var in reserveInput
	_ = decode(r, &in)
	if err := s.store.SetReservationBought(ctx, idParam(r, "wid"), store.Giver{UserID: actorID(ctx)}, in.Bought); err != nil {
		s.fail(w, err, "bought")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMyReservations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := actorID(ctx)
	rs, err := s.store.MyReservations(ctx, me)
	if err != nil {
		s.fail(w, err, "reservations")
		return
	}
	shared, _ := s.store.SharedWithMe(ctx, me)
	visible := map[int64]bool{}
	for _, l := range shared {
		visible[l.ID] = true
	}
	for i := range rs {
		if rs[i].Photo != "" && visible[rs[i].ListID] {
			rs[i].Photo = photoURL(sharedBase(rs[i].ListID), rs[i].WishID, rs[i].CreatedAt)
		} else {
			rs[i].Photo = ""
		}
	}
	writeJSON(w, http.StatusOK, rs)
}

// ---- a share link (no login) ----

// linkGiver is who is reserving through a link: a person signed in to this
// box (not the owner) reserves as themselves, anyone else by the secret
// their browser keeps (X-Giver) and the name they type.
func (s *Server) linkGiver(r *http.Request, shareID int64, name string) (store.Giver, *store.User) {
	if u, err := s.resolveUser(r); err == nil && u != nil {
		return store.Giver{UserID: u.ID, ShareID: shareID, Name: firstNonEmpty(name, u.Name, u.Email)}, u
	}
	secret := strings.TrimSpace(r.Header.Get("X-Giver"))
	if len(secret) < 16 {
		return store.Giver{ShareID: shareID, Name: name}, nil
	}
	return store.Giver{ShareID: shareID, Name: name, SecretHash: store.HashToken("giver:" + secret)}, nil
}

func (s *Server) linkList(w http.ResponseWriter, r *http.Request) (*store.List, int64, bool) {
	l, shareID, err := s.store.ListByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "this link does not work any more")
		return nil, 0, false
	}
	return l, shareID, true
}

func (s *Server) handleLinkList(w http.ResponseWriter, r *http.Request) {
	l, shareID, ok := s.linkList(w, r)
	if !ok {
		return
	}
	g, u := s.linkGiver(r, shareID, "")
	if u != nil && u.ID == l.UserID {
		// the owner opened their own link: keep the surprise
		ws, _ := s.store.ListWishesOn(r.Context(), l.ID, r.URL.Query().Get("sort"))
		ownerView(ws, map[int64]bool{l.ID: l.ShowReserved})
		s.ownerList(r.Context(), l)
		writeJSON(w, http.StatusOK, map[string]any{"list": l, "wishes": ws, "owner": true})
		return
	}
	s.writeViewerList(w, r, l, linkBase(chi.URLParam(r, "token")), g)
}

func (s *Server) handleLinkPhoto(w http.ResponseWriter, r *http.Request) {
	l, _, ok := s.linkList(w, r)
	if !ok {
		return
	}
	s.servePhotoOn(w, r, l.ID, idParam(r, "id"))
}

func (s *Server) handleLinkReserve(w http.ResponseWriter, r *http.Request) {
	l, shareID, ok := s.linkList(w, r)
	if !ok {
		return
	}
	var peek reserveInput
	keys, err := decodeKeys(r, &peek)
	if err != nil && len(keys) > 0 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	g, u := s.linkGiver(r, shareID, strings.TrimSpace(peek.Name))
	if u != nil && u.ID == l.UserID {
		writeErr(w, http.StatusForbidden, "this is your own list")
		return
	}
	if g.UserID == 0 && g.SecretHash == "" {
		writeErr(w, http.StatusBadRequest, "your browser did not send its giver key; reload the page")
		return
	}
	if g.UserID == 0 && g.Name == "" {
		writeErr(w, http.StatusBadRequest, "tell the list owner's family who you are: enter your name")
		return
	}
	if len(g.Name) > 80 {
		g.Name = g.Name[:80]
	}
	who := ""
	if u != nil {
		who = u.Email
	}
	// decodeKeys consumed the body; reserve reads the values from peek
	ctx := r.Context()
	wi, err := s.store.GetWishOn(ctx, l.ID, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "reserve")
		return
	}
	var hook core.CommitHook
	if s.core.Configured() && who != "" {
		hook = func(tx *sql.Tx) error {
			return core.QueueEvent(ctx, tx, "wishlist.reserved", fmt.Sprintf("wish:%d", wi.ID), map[string]any{
				"user_email": who, "wish_id": wi.ID, "wish": wi.Title, "list": l.Name, "for": l.Owner})
		}
	}
	note := strings.TrimSpace(peek.Note)
	if len(note) > 500 {
		note = note[:500]
	}
	res, err := s.store.Reserve(ctx, wi.ID, g, peek.Quantity, note, hook)
	if err != nil {
		s.fail(w, err, "reserve")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLinkRelease(w http.ResponseWriter, r *http.Request) {
	l, shareID, ok := s.linkList(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetWishOn(r.Context(), l.ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "release")
		return
	}
	g, _ := s.linkGiver(r, shareID, "")
	if g.UserID == 0 && g.SecretHash == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.store.Release(r.Context(), idParam(r, "id"), g); err != nil {
		s.fail(w, err, "release")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLinkBought(w http.ResponseWriter, r *http.Request) {
	l, shareID, ok := s.linkList(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetWishOn(r.Context(), l.ID, idParam(r, "id")); err != nil {
		s.fail(w, err, "bought")
		return
	}
	var in reserveInput
	_ = decode(r, &in)
	g, _ := s.linkGiver(r, shareID, "")
	if g.UserID == 0 && g.SecretHash == "" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.store.SetReservationBought(r.Context(), idParam(r, "id"), g, in.Bought); err != nil {
		s.fail(w, err, "bought")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
