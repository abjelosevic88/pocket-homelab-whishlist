package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// ---- "Bought it" ----
//
// Marking a wish bought is local and immediate. In the same transaction a
// durable action is queued that files the purchase in the person's other
// Pocket apps, as they chose in the dialog: an Inventory item (with price,
// date, shop and link), a Pocket Money expense, a Pocket Split expense. Each
// step stores its result on the wish as soon as it succeeds, so a retry after
// a failure (or a restart) never repeats a finished step; Inventory and Split
// also get a stable Idempotency-Key. An app that is not connected is skipped
// with a note; a real failure retries with backoff, and gives up after a few
// tries with the reason on the wish.

const actionBought = "wishlist.bought"

type boughtWork struct {
	WishID    int64      `json:"wish_id"`
	Date      string     `json:"date"`
	Price     int64      `json:"price"`
	Currency  string     `json:"currency"`
	Store     string     `json:"store"`
	Notes     string     `json:"notes"`
	Inventory *invStep   `json:"inventory,omitempty"`
	Money     *moneyStep `json:"money,omitempty"`
	Split     *splitStep `json:"split,omitempty"`
}

type invStep struct {
	Location string `json:"location"`
	Category string `json:"category"`
	Tag      string `json:"tag"`
}

type moneyStep struct {
	AccountID  int64 `json:"account_id"`
	CategoryID int64 `json:"category_id"`
}

type splitStep struct {
	GroupID  int64  `json:"group_id"`
	MemberID int64  `json:"member_id"`
	Category string `json:"category"`
}

func (s *Server) handleBought(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wi, err := s.store.GetWish(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "bought")
		return
	}
	if wi.Status == store.StatusBought {
		// a second press (or a retried request) must not file the purchase twice
		writeErr(w, http.StatusConflict, "this wish is already marked bought")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	in := struct {
		Date              string  `json:"date"`
		Price             *int64  `json:"price"`
		Currency          string  `json:"currency"`
		Store             string  `json:"store"`
		Notes             string  `json:"notes"`
		Inventory         *bool   `json:"inventory"`
		InventoryLocation *string `json:"inventory_location"`
		InventoryCategory *string `json:"inventory_category"`
		Money             *bool   `json:"money"`
		MoneyAccountID    *int64  `json:"money_account_id"`
		MoneyCategoryID   *int64  `json:"money_category_id"`
		Split             bool    `json:"split"`
		SplitGroupID      *int64  `json:"split_group_id"`
		SplitMemberID     *int64  `json:"split_member_id"`
	}{}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	work := boughtWork{WishID: wi.ID, Date: strings.TrimSpace(in.Date), Price: wi.Price, Currency: strings.ToUpper(strings.TrimSpace(in.Currency)),
		Store: strings.TrimSpace(firstNonEmpty(in.Store, wi.Store)), Notes: strings.TrimSpace(in.Notes)}
	if work.Date == "" {
		work.Date = store.Today()
	}
	if !validDate(work.Date) {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	if in.Price != nil {
		if *in.Price < 0 {
			writeErr(w, http.StatusBadRequest, "price cannot be negative")
			return
		}
		work.Price = *in.Price
	}
	if work.Currency == "" {
		work.Currency = firstNonEmpty(wi.Currency, st.HomeCurrency)
	}
	if !currency.Valid(work.Currency) {
		writeErr(w, http.StatusBadRequest, "unknown currency")
		return
	}
	// gift ideas are for someone else: they do not go into my inventory by default
	inv := st.BoughtInventory && wi.ListKind == store.KindMine
	if in.Inventory != nil {
		inv = *in.Inventory
	}
	if inv {
		step := &invStep{Location: st.InventoryLocation, Category: st.InventoryCategory, Tag: st.InventoryTag}
		if in.InventoryLocation != nil {
			step.Location = strings.TrimSpace(*in.InventoryLocation)
		}
		if in.InventoryCategory != nil {
			step.Category = strings.TrimSpace(*in.InventoryCategory)
		}
		work.Inventory = step
	}
	mon := st.BoughtMoney && work.Price > 0
	if in.Money != nil {
		mon = *in.Money && work.Price > 0
	}
	if mon {
		step := &moneyStep{AccountID: st.MoneyAccountID, CategoryID: st.MoneyCategoryID}
		if in.MoneyAccountID != nil {
			step.AccountID = *in.MoneyAccountID
		}
		if in.MoneyCategoryID != nil {
			step.CategoryID = *in.MoneyCategoryID
		}
		work.Money = step
	}
	if in.Split && work.Price > 0 {
		step := &splitStep{GroupID: st.SplitGroupID, MemberID: st.SplitMemberID, Category: st.SplitCategory}
		if in.SplitGroupID != nil {
			step.GroupID = *in.SplitGroupID
		}
		if in.SplitMemberID != nil {
			step.MemberID = *in.SplitMemberID
		}
		if step.GroupID <= 0 {
			writeErr(w, http.StatusBadRequest, "pick the Pocket Split group to share this in")
			return
		}
		work.Split = step
	}
	var actionID string
	hooks := []core.CommitHook{s.wishEvent(ctx, "wishlist.bought", wi, map[string]any{"price": work.Price, "currency": work.Currency, "date": work.Date, "store": work.Store, "gift_for": wi.Recipient})}
	if work.Inventory != nil || work.Money != nil || work.Split != nil {
		hooks = append(hooks, func(tx *sql.Tx) error {
			id, err := core.QueueAction(ctx, tx, uid, actionBought, work)
			actionID = id
			return err
		})
	}
	if err := s.store.MarkBought(ctx, uid, wi.ID, work.Date, work.Price, work.Currency, work.Store, hooks...); err != nil {
		s.fail(w, err, "bought")
		return
	}
	pending := false
	if actionID != "" {
		dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		s.drainActions(dctx, uid)
		cancel()
		var done int64
		_ = s.store.DB.QueryRowContext(ctx, `SELECT done_at FROM pocket_actions WHERE id = ?`, actionID).Scan(&done)
		pending = done == 0
	}
	got, err := s.ownerWish(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "bought")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wish": got, "pending": pending})
}

func (s *Server) actionsLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil || s.store.DB.PingContext(ctx) != nil {
			return
		}
		s.drainActions(ctx, 0)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) drainActions(ctx context.Context, uid int64) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	for n := 0; n < 50; n++ {
		actions, err := core.PendingActions(ctx, s.store.DB, uid)
		if err != nil || len(actions) == 0 {
			return
		}
		progressed := false
		for _, a := range actions {
			if err := s.runAction(ctx, a); err != nil {
				_ = core.RetryAction(ctx, s.store.DB, a, err)
				s.log.Warn("hand-off waiting to retry", "kind", a.Kind, "attempt", a.Attempts+1, "err", err)
				continue
			}
			progressed = true
		}
		if !progressed {
			return
		}
	}
}

// maxAttempts is when a hand-off gives up (backoff 1 s … 32 s, about a minute in all).
const maxAttempts = 6

func (s *Server) runAction(ctx context.Context, a core.Action) error {
	if a.Kind != actionBought {
		return core.FinishAction(ctx, s.store.DB, a)
	}
	var work boughtWork
	if err := json.Unmarshal(a.Payload, &work); err != nil {
		return core.FinishAction(ctx, s.store.DB, a)
	}
	wi, err := s.store.GetWish(ctx, a.UserID, work.WishID)
	if err != nil {
		return core.FinishAction(ctx, s.store.DB, a) // the wish is gone
	}
	cl := s.clients(ctx, a.UserID)
	var notes []string
	step := func(name string, err error) error {
		if err == nil {
			return nil
		}
		if isNotConnected(err) {
			notes = append(notes, err.Error())
			return nil
		}
		if a.Attempts+1 >= maxAttempts {
			notes = append(notes, name+" failed: "+err.Error())
			return nil
		}
		return err
	}
	if work.Inventory != nil && wi.InventoryItemID == nil {
		if err := step("Pocket Inventory", s.boughtInventory(ctx, cl, a, wi, work)); err != nil {
			return err
		}
	}
	if work.Money != nil && wi.MoneyTxID == nil {
		if err := step("Pocket Money", s.boughtMoney(ctx, cl, wi, work)); err != nil {
			return err
		}
	}
	if work.Split != nil && wi.SplitExpenseID == nil {
		if err := step("Pocket Split", s.boughtSplit(ctx, cl, a, wi, work)); err != nil {
			return err
		}
	}
	final, _ := s.store.GetWish(ctx, a.UserID, wi.ID)
	if final == nil {
		final = wi
	}
	var done []string
	if final.InventoryItemID != nil {
		done = append(done, "added to Pocket Inventory")
	}
	if final.MoneyTxID != nil {
		done = append(done, "booked in Pocket Money")
	}
	if final.SplitExpenseID != nil {
		done = append(done, "shared in Pocket Split")
	}
	note := strings.Join(done, ", ")
	if len(notes) > 0 {
		if note != "" {
			note += ". "
		}
		note += strings.Join(notes, ". ")
	}
	if note != "" {
		note = strings.ToUpper(note[:1]) + note[1:]
	}
	return core.FinishAction(ctx, s.store.DB, a, func(tx *sql.Tx) error {
		return store.SetHandoff(ctx, tx, wi.ID, nil, nil, nil, nil, note)
	})
}

// saveStep stores one finished step on the wish at once.
func (s *Server) saveStep(ctx context.Context, wishID int64, inventory, money, expense, group *int64, note string) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := store.SetHandoff(ctx, tx, wishID, inventory, money, expense, group, note); err != nil {
		return err
	}
	return tx.Commit()
}

func major(minor int64, cur string) float64 {
	d := currency.Decimals(cur)
	p := 1.0
	for i := 0; i < d; i++ {
		p *= 10
	}
	return float64(minor) / p
}

func (s *Server) boughtInventory(ctx context.Context, cl *clients, a core.Action, wi *store.Wish, work boughtWork) error {
	c, err := cl.conn(ctx, appInventory, scopeItemsWrite)
	if err != nil {
		return err
	}
	qty := float64(max(wi.Quantity, 1))
	in := pocket.ItemInput{Name: wi.Title, Location: work.Inventory.Location, Category: work.Inventory.Category, Quantity: &qty, Tags: work.Inventory.Tag,
		PurchaseDate: work.Date, URL: wi.URL, Brand: "", Notes: strings.TrimSpace(strings.Join([]string{work.Notes, "Bought at " + firstNonEmpty(work.Store, "a shop") + " · from Pocket Wishlist"}, "\n"))}
	if work.Price > 0 {
		p := major(work.Price, work.Currency) / qty
		in.Price = &p
	}
	it, err := c.client.CreateItem(ctx, in, a.ID+":inventory")
	if err != nil {
		return fmt.Errorf("Pocket Inventory: %w", err)
	}
	id := it.ID
	return s.saveStep(ctx, wi.ID, &id, nil, nil, nil, "")
}

func (s *Server) boughtMoney(ctx context.Context, cl *clients, wi *store.Wish, work boughtWork) error {
	c, err := cl.conn(ctx, appMoney, scopeAccountsRead, scopeTxWrite)
	if err != nil {
		return err
	}
	acct := work.Money.AccountID
	if acct <= 0 {
		accts, err := c.client.Accounts(ctx)
		if err != nil {
			return fmt.Errorf("Pocket Money: %w", err)
		}
		for _, x := range accts {
			if x.Everyday {
				acct = x.ID
				break
			}
		}
		if acct <= 0 && len(accts) > 0 {
			acct = accts[0].ID
		}
		if acct <= 0 {
			return &notConnected{"Pocket Money: you have no account to book on"}
		}
	}
	tags := []string{"wishlist"}
	notes := "From Pocket Wishlist"
	if wi.ListKind == store.KindGifts {
		tags = append(tags, "gift")
		if wi.Recipient != "" {
			notes = "Gift for " + wi.Recipient + " · from Pocket Wishlist"
		}
	}
	if work.Notes != "" {
		notes = work.Notes + "\n" + notes
	}
	t := pocket.Tx{AccountID: acct, Date: work.Date, Amount: work.Price, Currency: work.Currency, Description: wi.Title, Payee: work.Store,
		Tags: tags, Notes: notes, ExternalID: fmt.Sprintf("wishlist:%d", wi.ID)}
	if work.Money.CategoryID > 0 {
		cat := work.Money.CategoryID
		t.CategoryID = &cat
	}
	b, err := c.client.BookExpense(ctx, t)
	if err != nil {
		return fmt.Errorf("Pocket Money: %w", err)
	}
	id := b.ID
	return s.saveStep(ctx, wi.ID, nil, &id, nil, nil, "")
}

func (s *Server) boughtSplit(ctx context.Context, cl *clients, a core.Action, wi *store.Wish, work boughtWork) error {
	c, err := cl.conn(ctx, appSplit, scopeGroupsRead, scopeExpensesWrite)
	if err != nil {
		return err
	}
	g, err := c.client.Group(ctx, work.Split.GroupID)
	if err != nil {
		return fmt.Errorf("Pocket Split: %w", err)
	}
	payer := work.Split.MemberID
	if payer <= 0 && cl.u != nil {
		for _, m := range g.Members {
			if m.UserID != nil && strings.EqualFold(m.Name, cl.u.Name) {
				payer = m.ID
			}
		}
	}
	if payer <= 0 && len(g.Members) > 0 {
		payer = g.Members[0].ID
	}
	title := wi.Title
	if wi.ListKind == store.KindGifts && wi.Recipient != "" {
		title = "Gift for " + wi.Recipient + ": " + wi.Title
	}
	e, err := c.client.CreateExpense(ctx, g, pocket.ExpenseInput{Title: title, Amount: work.Price, Currency: work.Currency, PaidBy: payer, Date: work.Date,
		Category: work.Split.Category, Notes: strings.TrimSpace(work.Notes + "\nFrom Pocket Wishlist")}, a.ID+":split")
	if err != nil {
		return fmt.Errorf("Pocket Split: %w", err)
	}
	id, gid := e.ID, g.ID
	return s.saveStep(ctx, wi.ID, nil, nil, &id, &gid, "")
}

// ---- pickers in the dialogs and Settings (through the connection in effect) ----

func (s *Server) handleContactSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	cl := s.clients(ctx, actorID(ctx))
	c, err := cl.conn(ctx, appContacts, scopeContactsRead)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "detail": err.Error(), "contacts": []pocket.Contact{}})
		return
	}
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "contacts": []pocket.Contact{}})
		return
	}
	list, err := c.client.SearchContacts(ctx, q, 12)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Contacts: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "contacts": list})
}

func (s *Server) handleMoneyAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cl := s.clients(ctx, actorID(ctx))
	c, err := optional(cl.conn(ctx, appMoney, scopeAccountsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, []pocket.Account{})
		return
	}
	list, err := c.client.Accounts(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Money: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMoneyCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cl := s.clients(ctx, actorID(ctx))
	c, err := optional(cl.conn(ctx, appMoney, scopeAccountsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, []pocket.MoneyCategory{})
		return
	}
	list, err := c.client.MoneyCategories(ctx, int64(queryInt(r, "account")))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Money: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSplitGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cl := s.clients(ctx, actorID(ctx))
	sp, err := optional(cl.conn(ctx, appSplit, scopeGroupsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if sp == nil {
		writeJSON(w, http.StatusOK, []pocket.Group{})
		return
	}
	gs, err := sp.client.Groups(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Split: "+err.Error())
		return
	}
	// groups come without members in the list; fetch them one by one
	out := make([]pocket.Group, 0, len(gs))
	for _, g := range gs {
		if g.Archived {
			continue
		}
		if len(g.Members) == 0 {
			if full, err := sp.client.Group(ctx, g.ID); err == nil {
				g = *full
			}
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleInventoryLocations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cl := s.clients(ctx, actorID(ctx))
	inv, err := optional(cl.conn(ctx, appInventory, scopeItemsRead))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if inv == nil {
		writeJSON(w, http.StatusOK, []pocket.Named{})
		return
	}
	ls, err := inv.client.Locations(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Pocket Inventory: "+err.Error())
		return
	}
	if ls == nil {
		ls = []pocket.Named{}
	}
	writeJSON(w, http.StatusOK, ls)
}
