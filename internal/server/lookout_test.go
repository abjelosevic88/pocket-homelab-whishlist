package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/pocket"
)

// A shop that turns robots away is handed to Pocket Lookout: Wishlist starts
// a "@price" watch there, reads the price back into the wish, and stops the
// watch when the wish is deleted.
func TestPriceHandedToLookout(t *testing.T) {
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "checking your browser", http.StatusForbidden)
	}))
	t.Cleanup(shop.Close)

	var mu sync.Mutex
	var created []map[string]any
	var deleted []string
	price := "149.00"
	lookout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer plk_test" {
			http.Error(w, `{"error":"no"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/watches":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			created = append(created, in)
			fmt.Fprint(w, `{"id":41,"status":"new"}`)
		case r.Method == "GET" && r.URL.Path == "/api/v1/watches/41":
			if len(deleted) > 0 {
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			fmt.Fprint(w, `{"id":41,"status":"ok","source":"wishlist"}`)
		case r.Method == "GET" && r.URL.Path == "/api/v1/watches/41/snapshot":
			fmt.Fprintf(w, `{"snapshot":{"taken_at":"2026-11-01T10:00:00Z","body":"Price: %s EUR\nAvailability: sold out"}}`, price)
		case r.Method == "POST" && r.URL.Path == "/api/v1/watches/41/check":
			fmt.Fprint(w, `{"outcome":{"ok":true}}`)
		case r.Method == "DELETE" && r.URL.Path == "/api/v1/watches/41":
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(lookout.Close)

	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"lookout_mode": "direct", "lookout_url": lookout.URL, "lookout_token": "plk_test"})
	wi := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Tent", "url": shop.URL + "/tent", "price_text": "199", "currency": "EUR", "track_price": true})
	wid := num(wi, "id")
	got := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", wid), nil)
	w := got["wish"].(map[string]any)
	if num(w, "lookout_watch_id") != 41 || num(w, "price") != 14900 || w["availability"] != "out_of_stock" || w["check_error"] != "" {
		t.Fatalf("after hand-off: %v (error %v)", w, got["error"])
	}
	mu.Lock()
	if len(created) != 1 || created[0]["selector"] != "@price" || created[0]["fetch_mode"] != "browser" || created[0]["notify_mode"] != "off" || !strings.Contains(created[0]["url"].(string), "/tent") {
		t.Fatalf("watch created with %v", created)
	}
	price = "129.00"
	mu.Unlock()
	// the next check reads Lookout, it does not start a second watch
	got = a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", wid), nil)
	if num(got["wish"].(map[string]any), "price") != 12900 {
		t.Fatalf("second check: %v", got["wish"])
	}
	mu.Lock()
	if len(created) != 1 {
		t.Fatalf("a second watch was created")
	}
	mu.Unlock()
	// Bob's settings have no Lookout: his blocked shop stays a plain error
	bw := a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"title": "Lamp", "url": shop.URL + "/lamp", "track_price": true})
	bg := a.ok(a.bob, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", num(bw, "id")), nil)
	if num(bg["wish"].(map[string]any), "lookout_watch_id") != 0 || !strings.Contains(bg["error"].(string), "403") {
		t.Fatalf("Bob's wish went to Alice's Lookout: %v", bg)
	}
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/wishes/%d", wid), nil)
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != 1 {
		t.Fatalf("watch not deleted with the wish: %v", deleted)
	}
}

func TestParsePriceText(t *testing.T) {
	p, c, av := pocket.ParsePriceText("Price: 1299.50 BAM\nAvailability: in stock")
	if p != "1299.50" || c != "BAM" || av != "in_stock" {
		t.Fatal(p, c, av)
	}
	if p, _, _ := pocket.ParsePriceText("nothing here"); p != "" {
		t.Fatal(p)
	}
}
