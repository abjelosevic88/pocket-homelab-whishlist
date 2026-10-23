package store

import (
	"context"
	"time"
)

const offerCols = `id, wish_id, url, store, title, image, price, currency, price_home, availability, match, source, note, over_budget, rank, dismissed, found_at`

func scanOffer(row interface{ Scan(...any) error }) (*Offer, error) {
	var o Offer
	if err := row.Scan(&o.ID, &o.WishID, &o.URL, &o.Store, &o.Title, &o.Image, &o.Price, &o.Currency, &o.PriceHome, &o.Availability,
		&o.Match, &o.Source, &o.Note, &o.OverBudget, &o.Rank, &o.Dismissed, &o.FoundAt); err != nil {
		return nil, notFound(err)
	}
	return &o, nil
}

// ListOffers returns the offers of one of the person's wishes, best first
// (dismissed ones last, only when asked for).
func (s *Store) ListOffers(ctx context.Context, uid, wishID int64, dismissed bool) ([]Offer, error) {
	q := `SELECT ` + offerCols + ` FROM offers WHERE wish_id = ? AND user_id = ?`
	if !dismissed {
		q += ` AND dismissed = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY dismissed, rank, id`, wishID, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Offer{}
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// GetOffer returns one offer of the person's wish.
func (s *Store) GetOffer(ctx context.Context, uid, wishID, id int64) (*Offer, error) {
	return scanOffer(s.DB.QueryRowContext(ctx, `SELECT `+offerCols+` FROM offers WHERE id = ? AND wish_id = ? AND user_id = ?`, id, wishID, uid))
}

// DismissOffer hides an offer; it stays hidden in later hunts (same address).
func (s *Store) DismissOffer(ctx context.Context, uid, wishID, id int64, dismissed bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE offers SET dismissed = ? WHERE id = ? AND wish_id = ? AND user_id = ?`, boolInt(dismissed), id, wishID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceOffers stores a hunt's result: the offers found now replace the
// earlier ones, except that an address the person dismissed stays dismissed.
func (s *Store) ReplaceOffers(ctx context.Context, uid, wishID int64, offers []Offer) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	dismissed := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT url FROM offers WHERE wish_id = ? AND user_id = ? AND dismissed = 1`, wishID, uid)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		dismissed[u] = true
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM offers WHERE wish_id = ? AND user_id = ? AND dismissed = 0`, wishID, uid); err != nil {
		return err
	}
	for i, o := range offers {
		if dismissed[o.URL] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO offers (wish_id, user_id, url, store, title, image, price, currency, price_home, availability, match, source, note, over_budget, rank)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(wish_id, url) DO NOTHING`,
			wishID, uid, o.URL, o.Store, o.Title, o.Image, o.Price, o.Currency, o.PriceHome, o.Availability, o.Match, o.Source, o.Note, boolInt(o.OverBudget), i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetHuntRequest saves what the person asked a hunt to find.
func (s *Store) SetHuntRequest(ctx context.Context, uid, id int64, query string, maxPrice int64, condition string, auto bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE wishes SET hunt_query = ?, hunt_max = ?, hunt_condition = ?, hunt_auto = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		query, maxPrice, condition, boolInt(auto), Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// StartHunt marks a hunt running unless one already is (false = it was).
func (s *Store) StartHunt(ctx context.Context, id int64) (bool, error) {
	// a hunt "running" for over 15 minutes died with the process: take it over
	stale := time.Now().UTC().Add(-15 * time.Minute).Format(time.RFC3339Nano)
	res, err := s.DB.ExecContext(ctx, `UPDATE wishes SET hunt_status = 'running', hunt_step = 'Starting', hunt_error = '', updated_at = ?
		WHERE id = ? AND (hunt_status <> 'running' OR updated_at < ?)`, Now(), id, stale)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// HuntStep records what a running hunt is doing (shown while it works).
func (s *Store) HuntStep(ctx context.Context, id int64, step string) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE wishes SET hunt_step = ?, updated_at = ? WHERE id = ? AND hunt_status = 'running'`, step, Now(), id)
}

// FinishHunt records a hunt's outcome.
func (s *Store) FinishHunt(ctx context.Context, id int64, summary, errText, ai string, best int64) error {
	status := "done"
	if errText != "" {
		status = "error"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE wishes SET hunt_status = ?, hunt_step = '', hunt_summary = CASE WHEN ? <> '' THEN ? ELSE hunt_summary END,
		hunt_error = ?, hunt_ai = ?, hunt_best = CASE WHEN ? > 0 THEN ? ELSE hunt_best END, hunt_at = ?, updated_at = ? WHERE id = ?`,
		status, summary, summary, errText, ai, best, best, Now(), Now(), id)
	return err
}

// DueHunts lists wanted wishes set to keep hunting, longest ago first.
func (s *Store) DueHunts(ctx context.Context, limit int) ([]Wish, error) {
	return s.queryWishes(ctx, `WHERE w.hunt_auto = 1 AND w.status = 'wanted' AND w.hunt_status <> 'running' AND l.archived = 0 ORDER BY w.hunt_at, w.id LIMIT ?`, limit)
}
