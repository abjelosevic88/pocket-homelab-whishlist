// Package pocket talks to sibling Pocket apps: Pocket Inventory (a bought
// wish becomes something you own), Pocket Money (what it cost), Pocket Split
// (a shared purchase or a group gift) and Pocket Contacts (who a gift is
// for, their birthday). All of them share the same bearer-token JSON API.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any, operation ...string) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(operation) > 0 {
		req.Header.Set("Idempotency-Key", operation[0])
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	return c.do(ctx, http.MethodGet, path, q, nil, v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.get(ctx, "/api/v1/auth/me", nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// ---------------- Pocket Inventory ----------------

// Item is a Pocket Inventory item.
type Item struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	MinQuantity float64 `json:"min_quantity"`
	Barcode     string  `json:"barcode"`
	Brand       string  `json:"brand"`
	ExpiryDate  string  `json:"expiry_date"`
	Tags        string  `json:"tags"`
	Photo       string  `json:"photo"`
	Archived    bool    `json:"archived"`
	Location    string  `json:"location_name"`
	Category    string  `json:"category_name"`
	Low         bool    `json:"low"`
	UpdatedAt   string  `json:"updated_at"`
}

func decodeItems(raw json.RawMessage) ([]Item, error) {
	var list []Item
	if err := json.Unmarshal(raw, &list); err != nil {
		var wrap struct {
			Items []Item `json:"items"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return nil, err
		}
		list = wrap.Items
	}
	return list, nil
}

// ItemsByTag lists active items carrying a tag.
func (c *Client) ItemsByTag(ctx context.Context, tag string) ([]Item, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/items", url.Values{"tag": {tag}, "sort": {"name"}}, &raw); err != nil {
		return nil, err
	}
	return decodeItems(raw)
}

// SearchItems looks up items by name.
func (c *Client) SearchItems(ctx context.Context, q string, limit int) ([]Item, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/items", url.Values{"q": {q}, "limit": {fmt.Sprint(limit)}}, &raw); err != nil {
		return nil, err
	}
	return decodeItems(raw)
}

// LookupBarcode finds an inventory item by barcode (nil when none).
func (c *Client) LookupBarcode(ctx context.Context, code string) (*Item, error) {
	var it Item
	err := c.get(ctx, "/api/v1/lookup", url.Values{"barcode": {code}}, &it)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// GetItem fetches one item.
func (c *Client) GetItem(ctx context.Context, id int64) (*Item, error) {
	var out struct {
		Item Item `json:"item"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/v1/items/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out.Item, nil
}

// ItemInput creates or updates an inventory item.
type ItemInput struct {
	Name        string   `json:"name"`
	Notes       string   `json:"notes,omitempty"`
	Location    string   `json:"location,omitempty"`
	Category    string   `json:"category,omitempty"`
	Quantity    *float64 `json:"quantity,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	MinQuantity *float64 `json:"min_quantity,omitempty"`
	Barcode     string   `json:"barcode,omitempty"`
	Brand       string   `json:"brand,omitempty"`
	ExpiryDate  string   `json:"expiry_date,omitempty"`
	Tags        string   `json:"tags,omitempty"`
	Note        string   `json:"note,omitempty"`
	// a bought wish: what it cost (major units), when, where from
	Price        *float64 `json:"price,omitempty"`
	PurchaseDate string   `json:"purchase_date,omitempty"`
	URL          string   `json:"url,omitempty"`
}

// CreateItem creates an inventory item.
func (c *Client) CreateItem(ctx context.Context, in ItemInput, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, "/api/v1/items", nil, in, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// UpdateItem updates an inventory item (only the sent fields change).
func (c *Client) UpdateItem(ctx context.Context, id int64, in map[string]any) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/items/%d", id), nil, in, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// AdjustItem changes the quantity by delta with a movement note.
func (c *Client) AdjustItem(ctx context.Context, id int64, delta float64, note string, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/adjust", id), nil, map[string]any{"delta": delta, "note": note}, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// SetItemQuantity sets an absolute quantity.
func (c *Client) SetItemQuantity(ctx context.Context, id int64, qty float64, note string, operation ...string) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/adjust", id), nil, map[string]any{"quantity": qty, "note": note}, &it, operation...); err != nil {
		return nil, err
	}
	return &it, nil
}

// ArchiveItem archives an inventory item.
func (c *Client) ArchiveItem(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/items/%d/archive", id), nil, nil, nil)
}

// Named is a location or category.
type Named struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Locations lists inventory locations.
func (c *Client) Locations(ctx context.Context) ([]Named, error) {
	var out []Named
	err := c.get(ctx, "/api/v1/locations", nil, &out)
	return out, err
}

// ---------------- Pocket Split ----------------

// Member is a member of a Split group.
type Member struct {
	ID     int64  `json:"id"`
	UserID *int64 `json:"user_id"`
	Name   string `json:"name"`
}

// Group is a Split group.
type Group struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Currency string   `json:"currency"`
	Archived bool     `json:"archived"`
	Members  []Member `json:"members"`
}

// Groups lists the caller's Split groups.
func (c *Client) Groups(ctx context.Context) ([]Group, error) {
	var out []Group
	err := c.get(ctx, "/api/v1/groups", nil, &out)
	return out, err
}

// Group fetches one group with members.
func (c *Client) Group(ctx context.Context, id int64) (*Group, error) {
	var g Group
	if err := c.get(ctx, fmt.Sprintf("/api/v1/groups/%d", id), nil, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// Categories lists Split expense categories.
func (c *Client) Categories(ctx context.Context) ([]string, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/categories", nil, &raw); err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		return names, nil
	}
	var objs []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, err
	}
	for _, o := range objs {
		if o.Name != "" {
			names = append(names, o.Name)
		} else {
			names = append(names, o.ID)
		}
	}
	return names, nil
}

// ExpenseInput creates a Split expense. Amount is in minor units of Currency.
type ExpenseInput struct {
	Title     string `json:"title"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	PaidBy    int64  `json:"paid_by"`
	Date      string `json:"date"`
	Category  string `json:"category"`
	Notes     string `json:"notes"`
	SplitMode string `json:"split_mode"`
	Shares    []struct {
		MemberID int64 `json:"member_id"`
		Share    int64 `json:"share"`
	} `json:"shares"`
}

// Expense is the created expense (only what we keep).
type Expense struct {
	ID          int64  `json:"id"`
	GroupID     int64  `json:"group_id"`
	Title       string `json:"title"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	AmountGroup int64  `json:"amount_group"`
}

// CreateExpense posts an expense split evenly between every member.
func (c *Client) CreateExpense(ctx context.Context, g *Group, in ExpenseInput, operation ...string) (*Expense, error) {
	if in.SplitMode == "" {
		in.SplitMode = "even"
	}
	if len(in.Shares) == 0 {
		for _, m := range g.Members {
			in.Shares = append(in.Shares, struct {
				MemberID int64 `json:"member_id"`
				Share    int64 `json:"share"`
			}{m.ID, 1})
		}
	}
	var e Expense
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/groups/%d/expenses", g.ID), nil, in, &e, operation...); err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------- Pocket Money ----------------

// Account is a Money account the person can see.
type Account struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Currency string `json:"currency"`
	Everyday bool   `json:"everyday"`
	Archived bool   `json:"archived"`
	Role     string `json:"role"` // owner | editor | viewer
}

// Accounts lists the accounts the person can book on.
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var all []Account
	if err := c.get(ctx, "/api/v1/accounts", nil, &all); err != nil {
		return nil, err
	}
	out := []Account{}
	for _, a := range all {
		if !a.Archived && a.Role != "viewer" {
			out = append(out, a)
		}
	}
	return out, nil
}

// MoneyCategory is a Money expense category.
type MoneyCategory struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Kind     string `json:"kind"`
	Archived bool   `json:"archived"`
}

// MoneyCategories lists the expense categories usable on an account.
func (c *Client) MoneyCategories(ctx context.Context, accountID int64) ([]MoneyCategory, error) {
	var all []MoneyCategory
	path := "/api/v1/categories"
	if accountID > 0 {
		path = fmt.Sprintf("/api/v1/accounts/%d/categories", accountID)
	}
	if err := c.get(ctx, path, nil, &all); err != nil {
		return nil, err
	}
	out := []MoneyCategory{}
	for _, ct := range all {
		if !ct.Archived && ct.Kind != "income" {
			out = append(out, ct)
		}
	}
	return out, nil
}

// Tx is an expense to book in Money (Amount in minor units of Currency;
// Money converts to the account's currency).
type Tx struct {
	AccountID   int64    `json:"account_id"`
	Date        string   `json:"date"`
	Amount      int64    `json:"amount"`
	Currency    string   `json:"currency"`
	Description string   `json:"description"`
	Payee       string   `json:"payee"`
	CategoryID  *int64   `json:"category_id,omitempty"`
	Tags        []string `json:"tags"`
	Notes       string   `json:"notes"`
	ExternalID  string   `json:"external_id,omitempty"`
}

// BookedTx is the part of Money's answer Wishlist keeps.
type BookedTx struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"account_id"`
	AccountName string `json:"account_name"`
}

// BookExpense creates the expense in Money, marked as Wishlist's.
func (c *Client) BookExpense(ctx context.Context, t Tx) (*BookedTx, error) {
	if t.Tags == nil {
		t.Tags = []string{}
	}
	body := map[string]any{"kind": "expense", "account_id": t.AccountID, "date": t.Date, "amount": t.Amount, "currency": t.Currency,
		"description": t.Description, "payee": t.Payee, "tags": t.Tags, "notes": t.Notes, "source": "wishlist", "external_id": t.ExternalID}
	if t.CategoryID != nil {
		body["category_id"] = *t.CategoryID
	}
	var out BookedTx
	if err := c.do(ctx, http.MethodPost, "/api/v1/transactions", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------- Pocket Contacts ----------------

// Contact is a Pocket Contacts summary.
type Contact struct {
	ID       int64    `json:"id"`
	FN       string   `json:"fn"`
	Nickname string   `json:"nickname"`
	Emails   []string `json:"emails"`
	Birthday string   `json:"birthday"`
	Initials string   `json:"initials"`
	Color    string   `json:"color"`
	PhotoURL string   `json:"photo_url"`
	Starred  bool     `json:"starred"`
}

// SearchContacts finds contacts by name, email or phone.
func (c *Client) SearchContacts(ctx context.Context, q string, limit int) ([]Contact, error) {
	var out struct {
		Contacts []Contact `json:"contacts"`
	}
	if err := c.get(ctx, "/api/v1/search", url.Values{"q": {q}, "limit": {fmt.Sprint(limit)}}, &out); err != nil {
		return nil, err
	}
	if out.Contacts == nil {
		out.Contacts = []Contact{}
	}
	return out.Contacts, nil
}

// Occasion is an upcoming birthday or anniversary from Contacts.
type Occasion struct {
	Contact Contact `json:"contact"`
	Kind    string  `json:"kind"` // birthday | anniversary
	Date    string  `json:"date"`
	Age     *int    `json:"age"`
	Days    int     `json:"days"`
}

// Birthdays lists birthdays and anniversaries in the next days.
func (c *Client) Birthdays(ctx context.Context, days int) ([]Occasion, error) {
	var out []Occasion
	if err := c.get(ctx, "/api/v1/birthdays", url.Values{"days": {fmt.Sprint(days)}}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Occasion{}
	}
	return out, nil
}
