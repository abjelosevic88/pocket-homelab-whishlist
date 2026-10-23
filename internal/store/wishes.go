package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

const wishCols = `w.id, w.user_id, w.list_id, w.title, w.url, w.store, w.notes, w.tags, w.image_url, w.photo_path, w.price, w.currency, w.target_price,
	w.priority, w.quantity, w.status, w.track_price, w.availability, w.checked_at, w.check_error, w.lowest_price, w.alerted_price,
	w.bought_at, w.bought_price, w.bought_currency, w.bought_store, w.inventory_item_id, w.money_tx_id, w.split_expense_id, w.split_group_id,
	w.handoff_note, w.source, w.sort_order, w.created_at, w.updated_at,
	l.name, l.kind, l.recipient,
	COALESCE((SELECT p.price FROM price_history p WHERE p.wish_id = w.id AND p.currency = w.currency ORDER BY p.at, p.id LIMIT 1), 0),
	(SELECT COALESCE(SUM(r.quantity), 0) FROM reservations r WHERE r.wish_id = w.id)`

const wishFrom = ` FROM wishes w JOIN lists l ON l.id = w.list_id `

func scanWish(row interface{ Scan(...any) error }) (*Wish, error) {
	var w Wish
	var inv, tx, exp, grp sql.NullInt64
	var first int64
	if err := row.Scan(&w.ID, &w.UserID, &w.ListID, &w.Title, &w.URL, &w.Store, &w.Notes, &w.Tags, &w.ImageURL, &w.PhotoPath, &w.Price, &w.Currency, &w.TargetPrice,
		&w.Priority, &w.Quantity, &w.Status, &w.TrackPrice, &w.Availability, &w.CheckedAt, &w.CheckError, &w.LowestPrice, &w.AlertedPrice,
		&w.BoughtAt, &w.BoughtPrice, &w.BoughtCurrency, &w.BoughtStore, &inv, &tx, &exp, &grp,
		&w.HandoffNote, &w.Source, &w.SortOrder, &w.CreatedAt, &w.UpdatedAt,
		&w.ListName, &w.ListKind, &w.Recipient, &first, &w.Reserved); err != nil {
		return nil, notFound(err)
	}
	for _, p := range []struct {
		v   sql.NullInt64
		dst **int64
	}{{inv, &w.InventoryItemID}, {tx, &w.MoneyTxID}, {exp, &w.SplitExpenseID}, {grp, &w.SplitGroupID}} {
		if p.v.Valid {
			n := p.v.Int64
			*p.dst = &n
		}
	}
	w.AtTarget = w.TargetPrice > 0 && w.Price > 0 && w.Price <= w.TargetPrice
	if first > 0 && w.Price > 0 {
		w.Change = w.Price - first
	}
	return &w, nil
}

// WishFilter narrows ListWishes.
type WishFilter struct {
	ListID   int64
	Status   string // wanted (default) | bought | archived | all
	Query    string
	Tag      string
	Kind     string // mine | gifts: only wishes on lists of that kind
	AtTarget bool
	Tracked  bool
	Sort     string // manual | priority | price | price_desc | added | title | drop
	Limit    int
}

func (f WishFilter) where(args *[]any) string {
	var cl []string
	switch f.Status {
	case "all":
	case StatusBought, StatusArchived:
		cl = append(cl, "w.status = ?")
		*args = append(*args, f.Status)
	default:
		cl = append(cl, "w.status = 'wanted'")
	}
	if f.ListID > 0 {
		cl = append(cl, "w.list_id = ?")
		*args = append(*args, f.ListID)
	} else {
		cl = append(cl, "l.archived = 0")
	}
	if f.Kind == KindMine || f.Kind == KindGifts {
		cl = append(cl, "l.kind = ?")
		*args = append(*args, f.Kind)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		cl = append(cl, "(w.title LIKE ? OR w.notes LIKE ? OR w.store LIKE ? OR w.tags LIKE ? OR l.recipient LIKE ?)")
		like := "%" + q + "%"
		*args = append(*args, like, like, like, like, like)
	}
	if t := strings.ToLower(strings.TrimSpace(f.Tag)); t != "" {
		cl = append(cl, "(', ' || w.tags || ',') LIKE ?")
		*args = append(*args, "%, "+t+",%")
	}
	if f.AtTarget {
		cl = append(cl, "w.target_price > 0 AND w.price > 0 AND w.price <= w.target_price")
	}
	if f.Tracked {
		cl = append(cl, "w.track_price = 1")
	}
	if len(cl) == 0 {
		return ""
	}
	return " AND " + strings.Join(cl, " AND ")
}

func (f WishFilter) order() string {
	switch f.Sort {
	case "priority":
		return " ORDER BY w.priority DESC, w.sort_order, w.id"
	case "price":
		return " ORDER BY w.price = 0, w.price, w.id"
	case "price_desc":
		return " ORDER BY w.price DESC, w.id"
	case "added":
		return " ORDER BY w.created_at DESC, w.id DESC"
	case "title":
		return " ORDER BY w.title COLLATE NOCASE, w.id"
	case "bought":
		return " ORDER BY w.bought_at DESC, w.id DESC"
	case "checked":
		return " ORDER BY w.checked_at DESC, w.id DESC"
	}
	return " ORDER BY w.sort_order, w.id"
}

func (s *Store) queryWishes(ctx context.Context, q string, args ...any) ([]Wish, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+wishCols+wishFrom+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Wish{}
	for rows.Next() {
		w, err := scanWish(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// ListWishes returns a person's own wishes.
func (s *Store) ListWishes(ctx context.Context, uid int64, f WishFilter) ([]Wish, error) {
	args := []any{uid}
	q := `WHERE w.user_id = ?` + f.where(&args) + f.order()
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	return s.queryWishes(ctx, q, args...)
}

// ListWishesOn returns the wanted and bought wishes of one list, for someone
// who may see it (the caller checked access): no user filter.
func (s *Store) ListWishesOn(ctx context.Context, listID int64, sort string) ([]Wish, error) {
	f := WishFilter{ListID: listID, Sort: sort, Status: "all"}
	args := []any{}
	q := `WHERE w.status <> 'archived'` + f.where(&args) + f.order()
	return s.queryWishes(ctx, q, args...)
}

// GetWish returns one of the person's own wishes.
func (s *Store) GetWish(ctx context.Context, uid, id int64) (*Wish, error) {
	return scanWish(s.DB.QueryRowContext(ctx, `SELECT `+wishCols+wishFrom+`WHERE w.id = ? AND w.user_id = ?`, id, uid))
}

// GetWishOn returns a wish of a given list (viewer paths).
func (s *Store) GetWishOn(ctx context.Context, listID, id int64) (*Wish, error) {
	return scanWish(s.DB.QueryRowContext(ctx, `SELECT `+wishCols+wishFrom+`WHERE w.id = ? AND w.list_id = ? AND w.status <> 'archived'`, id, listID))
}

// CreateWish inserts a wish (its first price goes into the history).
func (s *Store) CreateWish(ctx context.Context, uid int64, w *Wish, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM lists WHERE id = ?`, w.ListID).Scan(&owner); err != nil || owner != uid {
		return ErrNotFound
	}
	if w.Status == "" {
		w.Status = StatusWanted
	}
	var next int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM wishes WHERE list_id = ?`, w.ListID).Scan(&next)
	now := Now()
	w.LowestPrice = w.Price
	res, err := tx.ExecContext(ctx, `INSERT INTO wishes (user_id, list_id, title, url, store, notes, tags, image_url, photo_path, price, currency, target_price, priority, quantity,
		status, track_price, availability, lowest_price, source, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, w.ListID, w.Title, w.URL, w.Store, w.Notes, w.Tags, w.ImageURL, w.PhotoPath, w.Price, w.Currency, w.TargetPrice, w.Priority, w.Quantity,
		w.Status, boolInt(w.TrackPrice), w.Availability, w.LowestPrice, w.Source, next, now, now)
	if err != nil {
		return err
	}
	w.ID, _ = res.LastInsertId()
	w.UserID, w.SortOrder, w.CreatedAt, w.UpdatedAt = uid, next, now, now
	if w.Price > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_history (wish_id, at, price, currency, source) VALUES (?, ?, ?, ?, 'added')`, w.ID, now, w.Price, w.Currency); err != nil {
			return err
		}
	}
	return core.Commit(tx, after...)
}

// UpdateWish saves a wish's details. A changed price is recorded as a manual
// observation; moving it to another of the owner's lists is allowed.
func (s *Store) UpdateWish(ctx context.Context, uid int64, w *Wish, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM lists WHERE id = ?`, w.ListID).Scan(&owner); err != nil || owner != uid {
		return ErrNotFound
	}
	var oldPrice int64
	var oldCur string
	if err := tx.QueryRowContext(ctx, `SELECT price, currency FROM wishes WHERE id = ? AND user_id = ?`, w.ID, uid).Scan(&oldPrice, &oldCur); err != nil {
		return notFound(err)
	}
	now := Now()
	if w.Price > 0 && (w.Price != oldPrice || w.Currency != oldCur) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_history (wish_id, at, price, currency, source) VALUES (?, ?, ?, ?, 'manual')`, w.ID, now, w.Price, w.Currency); err != nil {
			return err
		}
		if w.LowestPrice == 0 || w.Price < w.LowestPrice || w.Currency != oldCur {
			w.LowestPrice = w.Price
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE wishes SET list_id = ?, title = ?, url = ?, store = ?, notes = ?, tags = ?, image_url = ?, photo_path = ?, price = ?, currency = ?,
		target_price = ?, priority = ?, quantity = ?, status = ?, track_price = ?, lowest_price = ?, bought_at = ?, bought_price = ?, bought_currency = ?, bought_store = ?,
		updated_at = ? WHERE id = ? AND user_id = ?`,
		w.ListID, w.Title, w.URL, w.Store, w.Notes, w.Tags, w.ImageURL, w.PhotoPath, w.Price, w.Currency,
		w.TargetPrice, w.Priority, w.Quantity, w.Status, boolInt(w.TrackPrice), w.LowestPrice, w.BoughtAt, w.BoughtPrice, w.BoughtCurrency, w.BoughtStore,
		now, w.ID, uid)
	if err != nil {
		return err
	}
	w.UpdatedAt = now
	return core.Commit(tx, after...)
}

// DeleteWish removes a wish and returns its photo file.
func (s *Store) DeleteWish(ctx context.Context, uid, id int64, after ...core.CommitHook) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var photo string
	if err := tx.QueryRowContext(ctx, `SELECT photo_path FROM wishes WHERE id = ? AND user_id = ?`, id, uid).Scan(&photo); err != nil {
		return "", notFound(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wishes WHERE id = ? AND user_id = ?`, id, uid); err != nil {
		return "", err
	}
	return photo, core.Commit(tx, after...)
}

// MarkBought records the purchase on the wish (inside the caller's
// transaction hooks, the durable hand-off to the other apps is queued).
func (s *Store) MarkBought(ctx context.Context, uid, id int64, at string, price int64, cur, store string, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE wishes SET status = 'bought', bought_at = ?, bought_price = ?, bought_currency = ?, bought_store = ?, handoff_note = '', updated_at = ?
		WHERE id = ? AND user_id = ? AND status <> 'bought'`, at, price, cur, store, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM wishes WHERE id = ? AND user_id = ?`, id, uid).Scan(&exists)
		if exists > 0 {
			return ErrConflict // bought already: never file a purchase twice
		}
		return ErrNotFound
	}
	return core.Commit(tx, after...)
}

// SetStatus moves a wish between wanted / bought / archived. Going back to
// "wanted" forgets the purchase (the rows it made in other apps stay there).
func (s *Store) SetStatus(ctx context.Context, uid, id int64, status string, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := `UPDATE wishes SET status = ?, updated_at = ? WHERE id = ? AND user_id = ?`
	if status == StatusWanted {
		q = `UPDATE wishes SET status = ?, bought_at = '', bought_price = 0, bought_currency = '', bought_store = '', updated_at = ? WHERE id = ? AND user_id = ?`
	}
	res, err := tx.ExecContext(ctx, q, status, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return core.Commit(tx, after...)
}

// SetHandoff stores what the other apps made of a purchase (a durable action's result).
func SetHandoff(ctx context.Context, tx *sql.Tx, id int64, inventory, money, expense, group *int64, note string) error {
	_, err := tx.ExecContext(ctx, `UPDATE wishes SET inventory_item_id = COALESCE(?, inventory_item_id), money_tx_id = COALESCE(?, money_tx_id),
		split_expense_id = COALESCE(?, split_expense_id), split_group_id = COALESCE(?, split_group_id), handoff_note = ?, updated_at = ? WHERE id = ?`,
		nullI(inventory), nullI(money), nullI(expense), nullI(group), note, Now(), id)
	return err
}

// ReorderWishes stores the order of a list's wishes.
func (s *Store) ReorderWishes(ctx context.Context, uid, listID int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE wishes SET sort_order = ? WHERE id = ? AND list_id = ? AND user_id = ?`, i+1, id, listID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetPhoto stores (or clears) the local picture of a wish and returns the old one.
func (s *Store) SetPhoto(ctx context.Context, uid, id int64, path, imageURL string) (string, error) {
	var old string
	if err := s.DB.QueryRowContext(ctx, `SELECT photo_path FROM wishes WHERE id = ? AND user_id = ?`, id, uid).Scan(&old); err != nil {
		return "", notFound(err)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE wishes SET photo_path = ?, image_url = ?, updated_at = ? WHERE id = ? AND user_id = ?`, path, imageURL, Now(), id, uid)
	return old, err
}

// PriceHistory returns a wish's observed prices, oldest first.
func (s *Store) PriceHistory(ctx context.Context, wishID int64) ([]PricePoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT at, price, currency, source FROM price_history WHERE wish_id = ? ORDER BY at, id`, wishID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PricePoint{}
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.At, &p.Price, &p.Currency, &p.Source); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DueChecks lists tracked wanted wishes with a link, least recently checked
// first; the caller applies each owner's interval.
func (s *Store) DueChecks(ctx context.Context, limit int) ([]Wish, error) {
	return s.queryWishes(ctx, `WHERE w.track_price = 1 AND w.status = 'wanted' AND w.url <> '' AND l.archived = 0 ORDER BY w.checked_at, w.id LIMIT ?`, limit)
}

// CheckResult is what a price check found.
type CheckResult struct {
	Price        int64
	Currency     string
	Availability string
	Err          string
}

// RecordCheck saves a price check. A price is added to the history when it
// changed (or once a day, so the chart has points); the lowest price follows.
func (s *Store) RecordCheck(ctx context.Context, id int64, r CheckResult, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	if r.Err != "" || r.Price <= 0 {
		msg := r.Err
		if msg == "" {
			msg = "no price on the page"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wishes SET checked_at = ?, check_error = ?, availability = CASE WHEN ? <> '' THEN ? ELSE availability END WHERE id = ?`,
			now, msg, r.Availability, r.Availability, id); err != nil {
			return err
		}
		return core.Commit(tx, after...)
	}
	var price, lowest int64
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT price, currency, lowest_price FROM wishes WHERE id = ?`, id).Scan(&price, &cur, &lowest); err != nil {
		return notFound(err)
	}
	if r.Currency == "" {
		r.Currency = cur
	}
	var lastAt string
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(at), '') FROM price_history WHERE wish_id = ?`, id).Scan(&lastAt)
	stale := lastAt == "" || lastAt < time.Now().UTC().Add(-23*time.Hour).Format(time.RFC3339Nano)
	if r.Price != price || r.Currency != cur || stale {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_history (wish_id, at, price, currency, source) VALUES (?, ?, ?, ?, 'check')`, id, now, r.Price, r.Currency); err != nil {
			return err
		}
	}
	if lowest == 0 || r.Price < lowest || r.Currency != cur {
		lowest = r.Price
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wishes SET price = ?, currency = ?, lowest_price = ?, availability = ?, checked_at = ?, check_error = '' WHERE id = ?`,
		r.Price, r.Currency, lowest, r.Availability, now, id); err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

// SetAlerted remembers the price the last drop alert was about.
func (s *Store) SetAlerted(ctx context.Context, id, price int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE wishes SET alerted_price = ? WHERE id = ?`, price, id)
	return err
}

// Tags lists a person's tags with counts.
func (s *Store) Tags(ctx context.Context, uid int64) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tags FROM wishes WHERE user_id = ? AND tags <> '' AND status <> 'archived'`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, x := range SplitTags(t) {
			out[x]++
		}
	}
	return out, rows.Err()
}

// WishesOnDay lists a person's wishes added or bought on a date (YYYY-MM-DD, UTC-ish prefix match).
func (s *Store) WishesOnDay(ctx context.Context, uid int64, day string) (added, bought []Wish, err error) {
	added, err = s.queryWishes(ctx, `WHERE w.user_id = ? AND substr(w.created_at, 1, 10) = ? ORDER BY w.created_at`, uid, day)
	if err != nil {
		return
	}
	bought, err = s.queryWishes(ctx, `WHERE w.user_id = ? AND w.status = 'bought' AND w.bought_at = ? ORDER BY w.updated_at`, uid, day)
	return
}
