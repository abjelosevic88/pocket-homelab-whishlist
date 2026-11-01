package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/product"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// ---- Pocket Lookout reads the prices Wishlist cannot ----
//
// Some shops refuse plain requests (403, 429, "checking your browser") or
// only show the price after JavaScript runs. For those, Wishlist hands the
// page to Pocket Lookout: a watch on the shop's published price and stock
// ("@price"), read with Lookout's headless browser, telling nobody (Wishlist
// sends its own price alerts). Every price check then reads that watch, and
// the price history, target and drop alerts work as for any other wish. The
// watch goes when the wish is bought, archived or deleted, or its link changes.

// freshKey marks a "check now": Lookout reads the page again before Wishlist looks.
const freshKey ctxKey = 7

// handOver reports whether a failed plain check is worth giving to Lookout:
// the shop turned robots away, or the page shows no price without JavaScript.
func handOver(res store.CheckResult) bool {
	e := strings.ToLower(res.Err)
	if e == "" {
		return false
	}
	for _, s := range []string{"no price", "http 401", "http 403", "http 429", "http 503", "timeout", "deadline"} {
		if strings.Contains(e, s) {
			return true
		}
	}
	return false
}

func (s *Server) lookoutConn(ctx context.Context, uid int64) (*siblingConn, error) {
	return s.clients(ctx, uid).conn(ctx, appLookout, scopeWatchesRead, scopeWatchesWrite)
}

// lookoutRead reads a wish's price from its Lookout watch. ok is false when
// the wish has no (longer a) watch there: Wishlist then reads the page itself.
func (s *Server) lookoutRead(ctx context.Context, wi *store.Wish) (store.CheckResult, bool) {
	if wi.LookoutWatchID == nil {
		return store.CheckResult{}, false
	}
	id := *wi.LookoutWatchID
	sib, err := s.lookoutConn(ctx, wi.UserID)
	if err != nil {
		return store.CheckResult{Err: "Pocket Lookout reads this price but cannot be reached: " + err.Error()}, true
	}
	gone := func() (store.CheckResult, bool) {
		_ = s.store.SetLookoutWatch(ctx, wi.UserID, wi.ID, nil)
		wi.LookoutWatchID = nil
		return store.CheckResult{}, false
	}
	if fresh, _ := ctx.Value(freshKey).(bool); fresh {
		if err := sib.client.CheckWatch(ctx, id); errors.Is(err, pocket.ErrNotFound) {
			return gone()
		}
	}
	wa, err := s.waitLookout(ctx, sib, id)
	if errors.Is(err, pocket.ErrNotFound) {
		return gone()
	}
	if err != nil {
		return store.CheckResult{Err: "Pocket Lookout: " + err.Error()}, true
	}
	if wa.Status == "error" {
		return store.CheckResult{Err: "Pocket Lookout: " + firstNonEmpty(wa.LastError, "the last check failed")}, true
	}
	body, _, err := sib.client.WatchText(ctx, id)
	if err != nil {
		return store.CheckResult{Err: "Pocket Lookout: " + err.Error()}, true
	}
	price, cur, avail := pocket.ParsePriceText(body)
	res := store.CheckResult{Availability: avail}
	if price == "" {
		res.Err = "Pocket Lookout has not found a price on the page yet"
		return res, true
	}
	if cur == "" || !currency.Valid(cur) {
		cur = wi.Currency
	}
	res.Currency = cur
	res.Price = parseMinor(price, cur)
	return res, true
}

// waitLookout reads a watch, waiting (up to two minutes) while Lookout is
// still taking its first look or checking right now.
func (s *Server) waitLookout(ctx context.Context, sib *siblingConn, id int64) (*pocket.LookoutWatch, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		wa, err := sib.client.Watch(ctx, id)
		if err != nil {
			return nil, err
		}
		if (wa.Status != "new" && wa.Status != "checking") || time.Now().After(deadline) {
			return wa, nil
		}
		select {
		case <-ctx.Done():
			return wa, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// lookoutTakeOver starts a Lookout watch for a wish's page. A nil error with a
// nil id means Lookout is not connected for this person.
func (s *Server) lookoutTakeOver(ctx context.Context, wi *store.Wish, st store.Settings) (*int64, error) {
	sib, err := s.lookoutConn(ctx, wi.UserID)
	if isNotConnected(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hours := max(st.PriceCheckHours, 1)
	wa, err := sib.client.CreateWatch(ctx, pocket.LookoutInput{
		URL: wi.URL, Title: "Price: " + wi.Title, Tags: "wishlist", Selector: "@price", FetchMode: "browser", NotifyMode: "off",
		IntervalMinutes: hours * 60, BrowserWait: 3,
		Notes: fmt.Sprintf("Pocket Wishlist reads the price of “%s” from here and sends its own price alerts. The watch goes when the wish is bought or deleted.", wi.Title),
	})
	if err != nil {
		return nil, err
	}
	id := wa.ID
	if err := s.store.SetLookoutWatch(ctx, wi.UserID, wi.ID, &id); err != nil {
		return nil, err
	}
	wi.LookoutWatchID = &id
	s.log.Info("price handed to Pocket Lookout", "wish", wi.ID, "watch", id)
	return &id, nil
}

// lookoutDrop stops a wish's Lookout watch (best effort) and forgets it.
func (s *Server) lookoutDrop(ctx context.Context, uid int64, wi *store.Wish) {
	if wi == nil || wi.LookoutWatchID == nil {
		return
	}
	if sib, err := s.lookoutConn(ctx, uid); err == nil {
		if err := sib.client.DeleteWatch(ctx, *wi.LookoutWatchID); err != nil && !errors.Is(err, pocket.ErrNotFound) {
			s.log.Warn("pocket lookout: delete watch", "wish", wi.ID, "err", err)
		}
	}
	_ = s.store.SetLookoutWatch(ctx, uid, wi.ID, nil)
	wi.LookoutWatchID = nil
}

// handleLookoutStart hands a wish's price to Pocket Lookout now (POST) or
// takes it back (DELETE).
func (s *Server) handleLookout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wi, err := s.store.GetWish(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "wish")
		return
	}
	if r.Method == http.MethodDelete {
		s.lookoutDrop(ctx, uid, wi)
	} else {
		if wi.URL == "" {
			writeErr(w, http.StatusBadRequest, "this wish has no link")
			return
		}
		if wi.LookoutWatchID == nil {
			st, _ := s.store.GetSettings(ctx, uid)
			id, err := s.lookoutTakeOver(ctx, wi, st)
			if err != nil {
				writeErr(w, http.StatusBadGateway, "Pocket Lookout: "+err.Error())
				return
			}
			if id == nil {
				_, _, detail := s.connStatus(ctx, userFrom(ctx), st, appLookout)
				writeErr(w, http.StatusBadRequest, "Pocket Lookout is not connected: "+firstNonEmpty(detail, "Settings → Connected apps"))
				return
			}
			if !wi.TrackPrice {
				wi.TrackPrice = true
				_ = s.store.UpdateWish(ctx, uid, wi)
			}
		}
		st, _ := s.store.GetSettings(ctx, uid)
		if _, err := s.checkOne(ctx, wi, st); err != nil {
			s.fail(w, err, "check")
			return
		}
	}
	got, err := s.ownerWish(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "wish")
		return
	}
	prices, _ := s.store.PriceHistory(ctx, wi.ID)
	writeJSON(w, http.StatusOK, map[string]any{"wish": got, "prices": prices, "error": got.CheckError})
}

func parseMinor(price, cur string) int64 {
	return product.ToMinor(price, currency.Decimals(cur))
}
