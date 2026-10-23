package server

import (
	"context"
	"time"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// seedDemo fills an empty box owner's account with a few lists (DEMO_DATA=true),
// so a first look at the app is not an empty page. Nothing is fetched.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	if lists, err := s.store.ListLists(ctx, owner, false); err != nil || len(lists) > 0 {
		return err
	}
	st, _ := s.store.GetSettings(ctx, owner)
	cur := st.HomeCurrency
	soon := time.Now().AddDate(0, 0, 12).Format("2006-01-02")
	xmas := time.Date(time.Now().Year(), 12, 24, 0, 0, 0, 0, time.Local)
	if xmas.Before(time.Now()) {
		xmas = xmas.AddDate(1, 0, 0)
	}
	mine := &store.List{Name: "My wishlist", Kind: store.KindMine, Emoji: "🎁", Currency: cur, Description: "Things I would love. Share the link with family before birthdays."}
	home := &store.List{Name: "For the flat", Kind: store.KindMine, Emoji: "🛋️", Currency: cur}
	mum := &store.List{Name: "Mum", Kind: store.KindGifts, Emoji: "💐", Recipient: "Mum", Occasion: "Birthday", OccasionDate: soon, Budget: 15000, Currency: cur}
	xm := &store.List{Name: "Christmas", Kind: store.KindGifts, Emoji: "🎄", Recipient: "Family", Occasion: "Christmas", OccasionDate: xmas.Format("2006-01-02"), Budget: 40000, Currency: cur}
	for _, l := range []*store.List{mine, home, mum, xm} {
		if err := s.store.CreateList(ctx, owner, l); err != nil {
			return err
		}
	}
	wishes := []store.Wish{
		{ListID: mine.ID, Title: "Noise-cancelling headphones", Store: "example-shop.com", Price: 34900, TargetPrice: 29900, Priority: 3, Tags: "audio, travel", Notes: "Black, over-ear."},
		{ListID: mine.ID, Title: "Cast iron skillet 28 cm", Store: "kitchenstore.example", Price: 5490, Priority: 2, Tags: "kitchen"},
		{ListID: mine.ID, Title: "Hiking boots", Store: "outdoor.example", Price: 18999, TargetPrice: 15000, Priority: 2, Tags: "outdoor", Notes: "Size 44, waterproof."},
		{ListID: mine.ID, Title: "A good fountain pen", Priority: 1, Tags: "desk"},
		{ListID: home.ID, Title: "Floor lamp", Store: "furniture.example", Price: 12900, Priority: 2},
		{ListID: home.ID, Title: "Linen bedding set", Store: "textiles.example", Price: 8900, TargetPrice: 7000, Priority: 1},
		{ListID: mum.ID, Title: "Silk scarf", Price: 4500, Priority: 2, Notes: "She likes blue."},
		{ListID: mum.ID, Title: "Spa day voucher", Price: 9000, Priority: 3},
		{ListID: xm.ID, Title: "Board game for the evening", Price: 3990, Priority: 2},
	}
	for i := range wishes {
		w := &wishes[i]
		w.Quantity, w.Currency = 1, cur
		if err := s.store.CreateWish(ctx, owner, w); err != nil {
			return err
		}
	}
	s.log.Info("demo data seeded", "user", owner)
	return nil
}
