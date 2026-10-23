package store

import (
	"context"
	"database/sql"
	"errors"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// ErrFullyReserved means every one of a wish is already taken.
var ErrFullyReserved = errors.New("already reserved")

// Giver identifies who reserves: a person on the box, or a link visitor
// (name + the hash of the secret their browser keeps).
type Giver struct {
	UserID     int64
	ShareID    int64
	Name       string
	SecretHash string
}

func (g Giver) clause(args *[]any) string {
	if g.UserID > 0 {
		*args = append(*args, g.UserID)
		return "r.user_id = ?"
	}
	*args = append(*args, g.SecretHash)
	return "r.user_id IS NULL AND r.secret_hash = ? AND r.secret_hash <> ''"
}

// MarkMine fills in the giver's own reservations on a set of wishes.
func (s *Store) MarkMine(ctx context.Context, g Giver, wishes []Wish) error {
	if len(wishes) == 0 || (g.UserID == 0 && g.SecretHash == "") {
		return nil
	}
	args := []any{}
	cl := g.clause(&args)
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id, r.wish_id, r.quantity, r.note, r.bought FROM reservations r WHERE `+cl, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	type mine struct {
		id     int64
		qty    int
		note   string
		bought bool
	}
	got := map[int64]mine{}
	for rows.Next() {
		var m mine
		var wid int64
		if err := rows.Scan(&m.id, &wid, &m.qty, &m.note, &m.bought); err != nil {
			return err
		}
		got[wid] = m
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range wishes {
		if m, ok := got[wishes[i].ID]; ok {
			wishes[i].Mine, wishes[i].MineNote, wishes[i].MineID, wishes[i].MineBuy = m.qty, m.note, m.id, m.bought
		}
	}
	return nil
}

// Reserve takes qty of a wanted wish for the giver (or changes their
// existing reservation). It fails with ErrFullyReserved when not enough are free.
func (s *Store) Reserve(ctx context.Context, wishID int64, g Giver, qty int, note string, after ...core.CommitHook) (*Reservation, error) {
	if qty < 1 {
		qty = 1
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var want int
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT quantity, status FROM wishes WHERE id = ?`, wishID).Scan(&want, &status); err != nil {
		return nil, notFound(err)
	}
	if status != StatusWanted {
		return nil, ErrFullyReserved
	}
	args := []any{wishID}
	cl := g.clause(&args)
	var mineID int64
	var mineQty int
	err = tx.QueryRowContext(ctx, `SELECT r.id, r.quantity FROM reservations r WHERE r.wish_id = ? AND `+cl, args...).Scan(&mineID, &mineQty)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var taken int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity), 0) FROM reservations WHERE wish_id = ?`, wishID).Scan(&taken)
	if taken-mineQty+qty > max(want, 1) {
		return nil, ErrFullyReserved
	}
	r := &Reservation{WishID: wishID, Name: g.Name, Quantity: qty, Note: note, CreatedAt: Now()}
	if mineID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE reservations SET quantity = ?, note = ?, name = CASE WHEN ? <> '' THEN ? ELSE name END WHERE id = ?`, qty, note, g.Name, g.Name, mineID); err != nil {
			return nil, err
		}
		r.ID = mineID
	} else {
		res, err := tx.ExecContext(ctx, `INSERT INTO reservations (wish_id, share_id, user_id, name, secret_hash, quantity, note) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			wishID, nullI(&g.ShareID), nullI(&g.UserID), g.Name, g.SecretHash, qty, note)
		if err != nil {
			return nil, err
		}
		r.ID, _ = res.LastInsertId()
	}
	if g.UserID > 0 {
		r.UserID = &g.UserID
	}
	return r, core.Commit(tx, after...)
}

// Release drops the giver's reservation of a wish.
func (s *Store) Release(ctx context.Context, wishID int64, g Giver) error {
	args := []any{wishID}
	cl := g.clause(&args)
	res, err := s.DB.ExecContext(ctx, `DELETE FROM reservations AS r WHERE r.wish_id = ? AND `+cl, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetReservationBought lets the giver tick "bought" on their reservation.
func (s *Store) SetReservationBought(ctx context.Context, wishID int64, g Giver, bought bool) error {
	args := []any{boolInt(bought), wishID}
	cl := g.clause(&args)
	res, err := s.DB.ExecContext(ctx, `UPDATE reservations AS r SET bought = ? WHERE r.wish_id = ? AND `+cl, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MyReservations lists what a person on the box promised to get for others.
func (s *Store) MyReservations(ctx context.Context, uid int64) ([]Reservation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id, r.wish_id, r.share_id, r.user_id, r.name, r.quantity, r.note, r.bought, r.created_at,
		l.id, w.title, l.name, COALESCE(u.name, u.email, ''), w.photo_path, w.url, w.price, w.currency
		FROM reservations r JOIN wishes w ON w.id = r.wish_id JOIN lists l ON l.id = w.list_id LEFT JOIN users u ON u.id = l.user_id
		WHERE r.user_id = ? AND w.status <> 'archived' ORDER BY r.bought, r.created_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reservation{}
	for rows.Next() {
		var r Reservation
		var share, user sql.NullInt64
		var photo string
		if err := rows.Scan(&r.ID, &r.WishID, &share, &user, &r.Name, &r.Quantity, &r.Note, &r.Bought, &r.CreatedAt,
			&r.ListID, &r.WishTitle, &r.ListName, &r.Owner, &photo, &r.URL, &r.Price, &r.Currency); err != nil {
			return nil, err
		}
		if share.Valid {
			r.ShareID = &share.Int64
		}
		if user.Valid {
			r.UserID = &user.Int64
		}
		if photo != "" {
			r.Photo = "has"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
