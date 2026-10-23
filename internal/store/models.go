package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// List kinds.
const (
	KindMine  = "mine"  // my own wishlist; can be shared for gifting
	KindGifts = "gifts" // ideas for someone else
)

// Wish statuses.
const (
	StatusWanted   = "wanted"
	StatusBought   = "bought"
	StatusArchived = "archived"
)

// List is one wishlist or gift-idea list.
type List struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Kind         string `json:"kind"`
	Emoji        string `json:"emoji"`
	Color        string `json:"color"`
	Recipient    string `json:"recipient"`
	ContactID    *int64 `json:"contact_id"`
	Occasion     string `json:"occasion"`
	OccasionDate string `json:"occasion_date"`
	Budget       int64  `json:"budget"`
	Currency     string `json:"currency"`
	ShowReserved bool   `json:"show_reserved"`
	Archived     bool   `json:"archived"`
	SortOrder    int    `json:"sort_order"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`

	// computed
	Wanted   int    `json:"wanted"`    // wishes still wanted
	Bought   int    `json:"bought"`    // wishes bought
	Total    int64  `json:"total"`     // sum of wanted prices in Currency (others skipped)
	Spent    int64  `json:"spent"`     // sum of bought prices in Currency
	Shared   bool   `json:"shared"`    // has a link or a person share
	Owner    string `json:"owner"`     // the owner's name, on lists shared with me
	Cover    string `json:"cover"`     // photo URL of the first wish with a picture
	CoverID  int64  `json:"-"`         // that wish
	Drops    int    `json:"drops"`     // wishes at or below their target price
	Reserved int    `json:"reserved"`  // only for viewers (and owners with show_reserved)
	Role     string `json:"role"`      // owner | viewer
	ShareURL string `json:"share_url"` // the link, for the owner
}

// Wish is one thing someone would like.
type Wish struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	ListID          int64  `json:"list_id"`
	Title           string `json:"title"`
	URL             string `json:"url"`
	Store           string `json:"store"`
	Notes           string `json:"notes"`
	Tags            string `json:"tags"`
	ImageURL        string `json:"image_url"`
	PhotoPath       string `json:"-"`
	Price           int64  `json:"price"`
	Currency        string `json:"currency"`
	TargetPrice     int64  `json:"target_price"`
	Priority        int    `json:"priority"`
	Quantity        int    `json:"quantity"`
	Status          string `json:"status"`
	TrackPrice      bool   `json:"track_price"`
	Availability    string `json:"availability"`
	CheckedAt       string `json:"checked_at"`
	CheckError      string `json:"check_error"`
	LowestPrice     int64  `json:"lowest_price"`
	AlertedPrice    int64  `json:"-"`
	BoughtAt        string `json:"bought_at"`
	BoughtPrice     int64  `json:"bought_price"`
	BoughtCurrency  string `json:"bought_currency"`
	BoughtStore     string `json:"bought_store"`
	InventoryItemID *int64 `json:"inventory_item_id"`
	MoneyTxID       *int64 `json:"money_tx_id"`
	SplitExpenseID  *int64 `json:"split_expense_id"`
	SplitGroupID    *int64 `json:"split_group_id"`
	HandoffNote     string `json:"handoff_note"`
	Source          string `json:"source"`
	SortOrder       int    `json:"sort_order"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`

	// computed
	Photo     string `json:"photo"`     // URL of the picture ("" = none)
	ListName  string `json:"list_name"` // for cross-list views
	ListKind  string `json:"list_kind"` // for cross-list views
	Recipient string `json:"recipient"` // gifts lists: who it is for
	AtTarget  bool   `json:"at_target"` // price at or below target
	Change    int64  `json:"change"`    // price now minus the first price seen (negative = cheaper)
	Reserved  int    `json:"reserved"`  // quantity reserved (viewers; owners only with show_reserved)
	Mine      int    `json:"mine"`      // quantity the viewer reserved
	MineNote  string `json:"mine_note"` // the viewer's reservation note
	MineID    int64  `json:"mine_id"`   // the viewer's reservation id
	MineBuy   bool   `json:"mine_bought"`
}

// Remaining is how many are still free to reserve.
func (w *Wish) Remaining() int { return max(0, w.Quantity-w.Reserved) }

// PricePoint is one observed price.
type PricePoint struct {
	At       string `json:"at"`
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
	Source   string `json:"source"`
}

// Share is a link or a person a list is shared with.
type Share struct {
	ID        int64  `json:"id"`
	ListID    int64  `json:"list_id"`
	Kind      string `json:"kind"` // link | user
	Token     string `json:"token,omitempty"`
	UserID    *int64 `json:"user_id"`
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Reservation is a gift-giver's "I'll get this".
type Reservation struct {
	ID        int64  `json:"id"`
	WishID    int64  `json:"wish_id"`
	ShareID   *int64 `json:"share_id"`
	UserID    *int64 `json:"user_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	Note      string `json:"note"`
	Bought    bool   `json:"bought"`
	CreatedAt string `json:"created_at"`

	// computed for "My reservations"
	ListID    int64  `json:"list_id"`
	WishTitle string `json:"wish_title"`
	ListName  string `json:"list_name"`
	Owner     string `json:"owner"`
	Photo     string `json:"photo"`
	URL       string `json:"url"`
	Price     int64  `json:"price"`
	Currency  string `json:"currency"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections to the other Pocket apps. Connection modes: "core" (an app
// link through Pocket Core), "direct" (URL + token) or "off"; "" = not chosen
// yet (direct when a token is saved, else Pocket Core).
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	HomeCurrency    string `json:"home_currency"`
	DateFormat      string `json:"date_format"`
	TimeFormat      string `json:"time_format"`
	PriceCheckHours int    `json:"price_check_hours"` // 0 = never check prices
	DropPercent     int    `json:"drop_percent"`      // alert on a drop of at least this much
	OccasionDays    int    `json:"occasion_days"`     // how far ahead Home and reminders look

	NotifyURL       string `json:"notify_url"`
	NotifyFormat    string `json:"notify_format"` // apprise | ntfy | json
	NotifyDrops     bool   `json:"notify_drops"`
	NotifyOccasions bool   `json:"notify_occasions"`
	NotifyHour      int    `json:"notify_hour"`

	InventoryMode     string `json:"inventory_mode"`
	InventoryURL      string `json:"inventory_url"`
	InventoryToken    string `json:"inventory_token"`
	InventoryLocation string `json:"inventory_location"` // where bought things land
	InventoryCategory string `json:"inventory_category"`
	InventoryTag      string `json:"inventory_tag"`

	MoneyMode       string `json:"money_mode"`
	MoneyURL        string `json:"money_url"`
	MoneyToken      string `json:"money_token"`
	MoneyAccountID  int64  `json:"money_account_id"`
	MoneyCategoryID int64  `json:"money_category_id"`

	SplitMode      string `json:"split_mode"`
	SplitURL       string `json:"split_url"`
	SplitToken     string `json:"split_token"`
	SplitPublicURL string `json:"split_public_url"`
	SplitGroupID   int64  `json:"split_group_id"`
	SplitMemberID  int64  `json:"split_member_id"` // default payer
	SplitCategory  string `json:"split_category"`

	ContactsMode      string `json:"contacts_mode"`
	ContactsURL       string `json:"contacts_url"`
	ContactsToken     string `json:"contacts_token"`
	ContactsPublicURL string `json:"contacts_public_url"`

	// "Bought it" starts with these switched on.
	BoughtInventory bool `json:"bought_inventory"`
	BoughtMoney     bool `json:"bought_money"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
