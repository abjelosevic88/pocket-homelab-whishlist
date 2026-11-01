package pocket

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// LookoutWatch is a page Pocket Lookout watches.
type LookoutWatch struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Name          string `json:"name"`
	Status        string `json:"status"` // new | ok | error | checking
	LastError     string `json:"last_error"`
	LastCheckedAt string `json:"last_checked_at"`
	Paused        bool   `json:"paused"`
	Source        string `json:"source"`
}

// LookoutInput starts a watch.
type LookoutInput struct {
	URL             string `json:"url"`
	Title           string `json:"title"`
	Tags            string `json:"tags"`
	Notes           string `json:"notes"`
	Selector        string `json:"selector"`
	FetchMode       string `json:"fetch_mode"`
	NotifyMode      string `json:"notify_mode"`
	IntervalMinutes int    `json:"interval_minutes"`
	BrowserWait     int    `json:"browser_wait"`
}

// Watches lists the person's watches (the connection test).
func (c *Client) Watches(ctx context.Context) ([]LookoutWatch, error) {
	var out []LookoutWatch
	return out, c.get(ctx, "/api/v1/watches", nil, &out)
}

// CreateWatch starts watching a page.
func (c *Client) CreateWatch(ctx context.Context, in LookoutInput) (*LookoutWatch, error) {
	var out LookoutWatch
	return &out, c.do(ctx, http.MethodPost, "/api/v1/watches", nil, in, &out)
}

// Watch reads one watch.
func (c *Client) Watch(ctx context.Context, id int64) (*LookoutWatch, error) {
	var out LookoutWatch
	return &out, c.get(ctx, fmt.Sprintf("/api/v1/watches/%d", id), nil, &out)
}

// CheckWatch has Lookout read the page now (it answers when done).
func (c *Client) CheckWatch(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/watches/%d/check", id), nil, nil, nil)
}

// DeleteWatch stops a watch (a linked app may only delete the ones it added).
func (c *Client) DeleteWatch(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/watches/%d", id), nil, nil, nil)
}

// WatchText is what the page says now ("" before the first look).
func (c *Client) WatchText(ctx context.Context, id int64) (string, string, error) {
	var out struct {
		Snapshot *struct {
			TakenAt string `json:"taken_at"`
			Body    string `json:"body"`
		} `json:"snapshot"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/v1/watches/%d/snapshot", id), nil, &out); err != nil {
		return "", "", err
	}
	if out.Snapshot == nil {
		return "", "", nil
	}
	return out.Snapshot.Body, out.Snapshot.TakenAt, nil
}

var (
	rePriceLine = regexp.MustCompile(`(?m)^Price:\s*([0-9]+(?:\.[0-9]+)?)\s*([A-Z]{3})?\s*$`)
	reAvailLine = regexp.MustCompile(`(?m)^Availability:\s*(.+?)\s*$`)
)

// ParsePriceText reads Lookout's "@price" text: "Price: 149.00 BAM" and
// "Availability: in stock | sold out | pre-order". Price is as written ("149.00").
func ParsePriceText(body string) (price, currency, availability string) {
	if m := rePriceLine.FindStringSubmatch(body); m != nil {
		price, currency = m[1], m[2]
	}
	if m := reAvailLine.FindStringSubmatch(body); m != nil {
		switch strings.ToLower(m[1]) {
		case "in stock":
			availability = "in_stock"
		case "sold out":
			availability = "out_of_stock"
		case "pre-order":
			availability = "preorder"
		}
	}
	return
}
