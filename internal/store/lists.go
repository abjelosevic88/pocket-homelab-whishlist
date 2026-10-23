package store

import (
	"context"
	"database/sql"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

const listCols = `l.id, l.user_id, l.name, l.description, l.kind, l.emoji, l.color, l.recipient, l.contact_id, l.occasion, l.occasion_date,
	l.budget, l.currency, l.show_reserved, l.archived, l.sort_order, l.created_at, l.updated_at`

// listStats adds the computed columns: counts, totals in the list currency,
// whether it is shared, how many wishes sit at their target price.
const listStats = `,
	(SELECT COUNT(1) FROM wishes w WHERE w.list_id = l.id AND w.status = 'wanted'),
	(SELECT COUNT(1) FROM wishes w WHERE w.list_id = l.id AND w.status = 'bought'),
	(SELECT COALESCE(SUM(w.price * w.quantity), 0) FROM wishes w WHERE w.list_id = l.id AND w.status = 'wanted' AND (w.currency = l.currency OR w.currency = '')),
	(SELECT COALESCE(SUM(w.bought_price), 0) FROM wishes w WHERE w.list_id = l.id AND w.status = 'bought' AND (w.bought_currency = l.currency OR w.bought_currency = '')),
	EXISTS(SELECT 1 FROM shares s WHERE s.list_id = l.id),
	(SELECT COUNT(1) FROM wishes w WHERE w.list_id = l.id AND w.status = 'wanted' AND w.target_price > 0 AND w.price > 0 AND w.price <= w.target_price),
	COALESCE((SELECT w.id FROM wishes w WHERE w.list_id = l.id AND w.photo_path <> '' AND w.status <> 'archived' ORDER BY w.status = 'wanted' DESC, w.priority DESC, w.sort_order, w.id LIMIT 1), 0),
	(SELECT COALESCE(SUM(r.quantity), 0) FROM reservations r JOIN wishes w ON w.id = r.wish_id WHERE w.list_id = l.id AND w.status = 'wanted'),
	COALESCE((SELECT u.name FROM users u WHERE u.id = l.user_id), '')`

func scanList(row interface{ Scan(...any) error }) (*List, error) {
	var l List
	var cid sql.NullInt64
	var cover int64
	if err := row.Scan(&l.ID, &l.UserID, &l.Name, &l.Description, &l.Kind, &l.Emoji, &l.Color, &l.Recipient, &cid, &l.Occasion, &l.OccasionDate,
		&l.Budget, &l.Currency, &l.ShowReserved, &l.Archived, &l.SortOrder, &l.CreatedAt, &l.UpdatedAt,
		&l.Wanted, &l.Bought, &l.Total, &l.Spent, &l.Shared, &l.Drops, &cover, &l.Reserved, &l.Owner); err != nil {
		return nil, notFound(err)
	}
	if cid.Valid {
		l.ContactID = &cid.Int64
	}
	l.CoverID = cover
	return &l, nil
}

func (s *Store) queryLists(ctx context.Context, q string, args ...any) ([]List, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+listCols+listStats+` FROM lists l `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []List{}
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// ListLists returns a person's own lists.
func (s *Store) ListLists(ctx context.Context, uid int64, archived bool) ([]List, error) {
	return s.queryLists(ctx, `WHERE l.user_id = ? AND l.archived = ? ORDER BY l.sort_order, l.id`, uid, boolInt(archived))
}

// GetList returns one of the person's own lists.
func (s *Store) GetList(ctx context.Context, uid, id int64) (*List, error) {
	return scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+listStats+` FROM lists l WHERE l.id = ? AND l.user_id = ?`, id, uid))
}

// SharedWithMe lists the lists other people on the box shared with this person.
func (s *Store) SharedWithMe(ctx context.Context, uid int64) ([]List, error) {
	return s.queryLists(ctx, `JOIN shares sh ON sh.list_id = l.id AND sh.kind = 'user' AND sh.user_id = ?
		WHERE l.archived = 0 AND l.user_id <> ? AND l.kind = 'mine' ORDER BY l.name, l.id`, uid, uid)
}

// GetSharedList returns a list shared with this person (never their own).
func (s *Store) GetSharedList(ctx context.Context, uid, id int64) (*List, error) {
	return scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+listStats+` FROM lists l
		JOIN shares sh ON sh.list_id = l.id AND sh.kind = 'user' AND sh.user_id = ?
		WHERE l.id = ? AND l.user_id <> ? AND l.archived = 0 AND l.kind = 'mine'`, uid, id, uid))
}

// ListByToken finds the list behind a share link, with the share's id.
func (s *Store) ListByToken(ctx context.Context, token string) (*List, int64, error) {
	token = strings.TrimSpace(token)
	if len(token) < 16 {
		return nil, 0, ErrNotFound
	}
	var shareID, listID int64
	if err := s.DB.QueryRowContext(ctx, `SELECT id, list_id FROM shares WHERE kind = 'link' AND token = ?`, token).Scan(&shareID, &listID); err != nil {
		return nil, 0, notFound(err)
	}
	l, err := scanList(s.DB.QueryRowContext(ctx, `SELECT `+listCols+listStats+` FROM lists l WHERE l.id = ? AND l.archived = 0 AND l.kind = 'mine'`, listID))
	if err != nil {
		return nil, 0, err
	}
	return l, shareID, nil
}

// CreateList inserts a list for the person.
func (s *Store) CreateList(ctx context.Context, uid int64, l *List) error {
	if l.Kind != KindGifts {
		l.Kind = KindMine
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var next int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM lists WHERE user_id = ?`, uid).Scan(&next)
	res, err := tx.ExecContext(ctx, `INSERT INTO lists (user_id, name, description, kind, emoji, color, recipient, contact_id, occasion, occasion_date, budget, currency, show_reserved, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, l.Name, l.Description, l.Kind, l.Emoji, l.Color, l.Recipient, nullI(l.ContactID), l.Occasion, l.OccasionDate,
		l.Budget, l.Currency, boolInt(l.ShowReserved), next)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	l.UserID, l.SortOrder = uid, next
	return tx.Commit()
}

// UpdateList saves a list's details (the owner's).
func (s *Store) UpdateList(ctx context.Context, uid int64, l *List) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE lists SET name = ?, description = ?, emoji = ?, color = ?, recipient = ?, contact_id = ?, occasion = ?, occasion_date = ?,
		budget = ?, currency = ?, show_reserved = ?, archived = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		l.Name, l.Description, l.Emoji, l.Color, l.Recipient, nullI(l.ContactID), l.Occasion, l.OccasionDate, l.Budget, l.Currency,
		boolInt(l.ShowReserved), boolInt(l.Archived), Now(), l.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteList removes a list with its wishes, shares and reservations, and
// returns the photo files that belonged to it.
func (s *Store) DeleteList(ctx context.Context, uid, id int64) ([]string, error) {
	photos, err := s.photoPaths(ctx, `SELECT photo_path FROM wishes WHERE list_id = ? AND user_id = ? AND photo_path <> ''`, id, uid)
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM lists WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return photos, nil
}

// ReorderLists stores the person's list order.
func (s *Store) ReorderLists(ctx context.Context, uid int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE lists SET sort_order = ? WHERE id = ? AND user_id = ?`, i+1, id, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) photoPaths(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OwnedPhotos lists every photo file of a person (for account deletion).
func (s *Store) OwnedPhotos(ctx context.Context, uid int64) ([]string, error) {
	return s.photoPaths(ctx, `SELECT photo_path FROM wishes WHERE user_id = ? AND photo_path <> ''`, uid)
}

// ---- shares ----

// ListShares lists a list's links and people (the owner's view).
func (s *Store) ListShares(ctx context.Context, listID int64) ([]Share, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sh.id, sh.list_id, sh.kind, sh.token, sh.user_id, COALESCE(u.name, ''), COALESCE(u.email, ''), sh.created_at
		FROM shares sh LEFT JOIN users u ON u.id = sh.user_id WHERE sh.list_id = ? ORDER BY sh.kind, sh.id`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Share{}
	for rows.Next() {
		var sh Share
		var uid sql.NullInt64
		if err := rows.Scan(&sh.ID, &sh.ListID, &sh.Kind, &sh.Token, &uid, &sh.UserName, &sh.UserEmail, &sh.CreatedAt); err != nil {
			return nil, err
		}
		if uid.Valid {
			sh.UserID = &uid.Int64
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// LinkToken returns the list's share link token ("" when it has none).
func (s *Store) LinkToken(ctx context.Context, listID int64) string {
	var t string
	_ = s.DB.QueryRowContext(ctx, `SELECT token FROM shares WHERE list_id = ? AND kind = 'link' ORDER BY id LIMIT 1`, listID).Scan(&t)
	return t
}

// CreateLink makes (or returns) the list's secret link.
func (s *Store) CreateLink(ctx context.Context, listID int64) (string, error) {
	if t := s.LinkToken(ctx, listID); t != "" {
		return t, nil
	}
	t := RandomToken(18)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO shares (list_id, kind, token) VALUES (?, 'link', ?)`, listID, t)
	return t, err
}

// ShareWithUser shares a list with a person on the box (idempotent).
func (s *Store) ShareWithUser(ctx context.Context, listID, userID int64, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO shares (list_id, kind, user_id) VALUES (?, 'user', ?) ON CONFLICT DO NOTHING`, listID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	return core.Commit(tx, after...)
}

// DeleteShare removes a link (a new one gets a new secret) or a person.
// Reservations made through it stay: the gift is still being bought.
func (s *Store) DeleteShare(ctx context.Context, listID, shareID int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM shares WHERE id = ? AND list_id = ?`, shareID, listID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
