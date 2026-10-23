package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/config"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string // admin, owner of the box
	bob   string
	carol string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.FetchPrivate = true // the fake shop runs on localhost
	cfg.BaseURL = "http://wishlist.test"
	cfg.PriceChecksPerRun = 40
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}, {Email: "carol@example.com", Name: "Carol", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		switch u.Name {
		case "Alice":
			a.alice = tok
		case "Bob":
			a.bob = tok
		case "Carol":
			a.carol = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

// call sends a request; auth is a bearer token, or "giver:<secret>" for a link visitor.
func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	switch {
	case strings.HasPrefix(auth, "giver:"):
		req.Header.Set("X-Giver", strings.TrimPrefix(auth, "giver:"))
	case auth != "":
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) int64 { v, _ := m[k].(float64); return int64(v) }

func wishesOf(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, x := range m["wishes"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// Every list and wish is its owner's: anyone else gets 404, even an admin.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	l := a.ok(a.bob, "POST", "/api/v1/lists", map[string]any{"name": "Bob's things"})
	lid := num(l, "id")
	wi := a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"list_id": lid, "title": "Bike lights", "price": 2500})
	wid := num(wi, "id")
	for _, p := range []string{fmt.Sprintf("/api/v1/lists/%d", lid), fmt.Sprintf("/api/v1/wishes/%d", wid), fmt.Sprintf("/api/v1/wishes/%d/prices", wid), fmt.Sprintf("/api/v1/wishes/%d/photo", wid), fmt.Sprintf("/api/v1/shared/%d", lid)} {
		a.expect(404, a.alice, "GET", p, nil)
		a.expect(404, a.carol, "GET", p, nil)
	}
	a.expect(404, a.alice, "PUT", fmt.Sprintf("/api/v1/wishes/%d", wid), map[string]any{"title": "mine now"})
	a.expect(404, a.alice, "DELETE", fmt.Sprintf("/api/v1/lists/%d", lid), nil)
	a.expect(404, a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/bought", wid), map[string]any{})
	a.expect(404, a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": lid, "title": "sneaked in"})
	_, _, all := a.call(a.alice, "GET", "/api/v1/wishes?status=all", nil)
	if len(all) != 0 {
		t.Fatalf("alice sees %d of bob's wishes", len(all))
	}
	// shared with Carol: she can see it, Alice still cannot
	a.ok(a.bob, "POST", fmt.Sprintf("/api/v1/lists/%d/shares", lid), map[string]any{"kind": "user", "email": "carol@example.com"})
	got := a.ok(a.carol, "GET", fmt.Sprintf("/api/v1/shared/%d", lid), nil)
	if len(wishesOf(got)) != 1 {
		t.Fatalf("carol sees %v", got)
	}
	a.expect(404, a.alice, "GET", fmt.Sprintf("/api/v1/shared/%d", lid), nil)
	// a viewer cannot edit through the owner's routes
	a.expect(404, a.carol, "PUT", fmt.Sprintf("/api/v1/wishes/%d", wid), map[string]any{"title": "x"})
	// the photo route is per door, too
	a.expect(404, a.alice, "GET", fmt.Sprintf("/api/v1/shared/%d/wishes/%d/photo", lid, wid), nil)
}

// Reservations are a surprise: the owner never sees who, and only sees that
// something is taken when the list says so; nobody can take more than is wanted.
func TestReservationsStaySecret(t *testing.T) {
	a := newApp(t, nil)
	l := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Birthday"})
	lid := num(l, "id")
	wid := num(a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": lid, "title": "Headphones", "price": 9900, "quantity": 1}), "id")
	link := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/lists/%d/shares", lid), map[string]any{"kind": "link"})
	tok := link["token"].(string)
	if !strings.HasSuffix(link["url"].(string), "/s/"+tok) {
		t.Fatalf("link %v", link)
	}
	visitor, other := "giver:aaaaaaaaaaaaaaaaaaaaaaaa", "giver:bbbbbbbbbbbbbbbbbbbbbbbb"
	page := a.ok(visitor, "GET", "/api/v1/s/"+tok, nil)
	if page["owner"] == true || len(wishesOf(page)) != 1 {
		t.Fatalf("visitor page %v", page)
	}
	a.expect(400, visitor, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), map[string]any{})
	a.ok(visitor, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), map[string]any{"name": "Grandma", "note": "the blue one"})
	a.expect(409, other, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), map[string]any{"name": "Uncle"})
	// the visitor sees their own reservation, the other only that it is taken
	if w := wishesOf(a.ok(visitor, "GET", "/api/v1/s/"+tok, nil))[0]; num(w, "mine") != 1 || num(w, "reserved") != 1 || w["mine_note"] != "the blue one" {
		t.Fatalf("visitor wish %v", w)
	}
	if w := wishesOf(a.ok(other, "GET", "/api/v1/s/"+tok, nil))[0]; num(w, "mine") != 0 || num(w, "reserved") != 1 {
		t.Fatalf("other wish %v", w)
	}
	// the owner: nothing, not even through their own link
	ownerSees := func(path string) map[string]any {
		got := a.ok(a.alice, "GET", path, nil)
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "Grandma") || strings.Contains(string(raw), "the blue one") {
			t.Fatalf("owner learns who reserved via %s: %s", path, raw)
		}
		return got
	}
	if w := wishesOf(ownerSees(fmt.Sprintf("/api/v1/lists/%d", lid)))[0]; num(w, "reserved") != 0 {
		t.Fatalf("owner sees reserved %v", w)
	}
	if got := ownerSees("/api/v1/s/" + tok); got["owner"] != true || num(wishesOf(got)[0], "reserved") != 0 {
		t.Fatalf("owner via link %v", got)
	}
	ownerSees("/api/v1/home")
	ownerSees("/api/v1/wishes")
	ownerSees("/api/v1/export.json")
	a.expect(403, a.alice, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), map[string]any{"name": "me"})
	// "show that something is taken" — a count, still no names
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/lists/%d", lid), map[string]any{"show_reserved": true})
	if w := wishesOf(ownerSees(fmt.Sprintf("/api/v1/lists/%d", lid)))[0]; num(w, "reserved") != 1 {
		t.Fatalf("owner with show_reserved %v", w)
	}
	// only the visitor who reserved can drop it
	a.expect(404, other, "DELETE", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), nil)
	a.expect(204, visitor, "DELETE", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), nil)
	a.ok(other, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, wid), map[string]any{"name": "Uncle"})
	// a person on the box reserves as themselves and finds it under My reservations
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/lists/%d/shares", lid), map[string]any{"kind": "user", "email": "bob@example.com"})
	w2 := num(a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": lid, "title": "Socks", "quantity": 3}), "id")
	a.ok(a.bob, "POST", fmt.Sprintf("/api/v1/shared/%d/wishes/%d/reserve", lid, w2), map[string]any{"quantity": 2})
	a.expect(409, other, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, w2), map[string]any{"name": "Uncle", "quantity": 2})
	a.ok(other, "POST", fmt.Sprintf("/api/v1/s/%s/wishes/%d/reserve", tok, w2), map[string]any{"name": "Uncle", "quantity": 1})
	_, _, mine := a.call(a.bob, "GET", "/api/v1/reservations", nil)
	if len(mine) != 1 || mine[0].(map[string]any)["wish_title"] != "Socks" {
		t.Fatalf("bob's reservations %v", mine)
	}
	// a removed link stops working
	shares := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/lists/%d", lid), nil)
	_ = shares
	_, _, sl := a.call(a.alice, "GET", fmt.Sprintf("/api/v1/lists/%d/shares", lid), nil)
	for _, x := range sl {
		sh := x.(map[string]any)
		if sh["kind"] == "link" {
			a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/lists/%d/shares/%d", lid, num(sh, "id")), nil)
		}
	}
	a.expect(404, visitor, "GET", "/api/v1/s/"+tok, nil)
	// gift ideas for someone else cannot be shared
	g := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Mum", "kind": "gifts", "recipient": "Mum"})
	a.expect(400, a.alice, "POST", fmt.Sprintf("/api/v1/lists/%d/shares", num(g, "id")), map[string]any{"kind": "link"})
}

// A product page fills in a wish from its link; a price check records the
// new price and alerts once when the target is reached.
func TestLinkLookupAndPriceAlert(t *testing.T) {
	var mu sync.Mutex
	price := "129.00"
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		p := price
		mu.Unlock()
		if r.URL.Path == "/img.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n0000"))
			return
		}
		fmt.Fprintf(w, `<html><head><title>Shop</title><meta property="og:site_name" content="Test Shop">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"Trail Shoes","image":"/img.png",
"offers":{"@type":"Offer","price":"%s","priceCurrency":"EUR","availability":"https://schema.org/InStock"}}</script></head></html>`, p)
	}))
	t.Cleanup(shop.Close)
	var alerts []string
	push := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		alerts = append(alerts, string(b))
		mu.Unlock()
	}))
	t.Cleanup(push.Close)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"notify_url": push.URL + "/notify/wishlist", "notify_format": "apprise"})
	wi := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"url": shop.URL + "/p/1", "target_text": "100"})
	if wi["title"] != "Trail Shoes" || num(wi, "price") != 12900 || wi["currency"] != "EUR" || wi["store"] != "Test Shop" || wi["track_price"] != true || wi["photo"] == "" || num(wi, "target_price") != 10000 {
		t.Fatalf("wish from link %v", wi)
	}
	wid := num(wi, "id")
	a.expect(200, a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/photo", wid), nil)
	preq, _ := http.NewRequest("GET", a.url+fmt.Sprintf("/api/v1/wishes/%d/photo", wid), nil)
	preq.Header.Set("Authorization", "Bearer "+a.alice)
	if pres, err := http.DefaultClient.Do(preq); err != nil || !strings.Contains(pres.Header.Get("Content-Security-Policy"), "sandbox") || pres.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("photo served without a sandbox: %v %v", err, pres.Header)
	}
	// no change: no alert
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", wid), nil)
	mu.Lock()
	price = "95,50"
	mu.Unlock()
	got := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", wid), nil)
	w := got["wish"].(map[string]any)
	if num(w, "price") != 9550 || w["at_target"] != true || num(w, "change") != -3350 || len(got["prices"].([]any)) < 2 {
		t.Fatalf("after drop %v", got)
	}
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/check", wid), nil) // same price: no second alert
	mu.Lock()
	n := len(alerts)
	first := ""
	if n > 0 {
		first = alerts[0]
	}
	mu.Unlock()
	if n != 1 || !strings.Contains(first, "Price target reached") {
		t.Fatalf("alerts %d %v", n, alerts)
	}
	// Home lists it under "at your target"
	home := a.ok(a.alice, "GET", "/api/v1/home", nil)
	if len(home["at_target"].([]any)) != 1 {
		t.Fatalf("home %v", home["at_target"])
	}
}

// siblingApps fakes Pocket Inventory, Money and Split behind one server.
type siblingApps struct {
	*httptest.Server
	mu        sync.Mutex
	calls     []string
	failMoney bool
}

func newSiblingApps(t *testing.T) *siblingApps {
	sb := &siblingApps{}
	sb.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sb.mu.Lock()
		defer sb.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "GET" {
			sb.calls = append(sb.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Idempotency-Key"))
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/items":
			io.WriteString(w, `{"id":41,"name":"x"}`)
		case r.Method == "POST" && r.URL.Path == "/api/v1/transactions":
			if sb.failMoney {
				w.WriteHeader(502)
				io.WriteString(w, `{"error":"down"}`)
				return
			}
			io.WriteString(w, `{"id":77,"account_id":3,"account_name":"Checking"}`)
		case r.URL.Path == "/api/v1/accounts":
			io.WriteString(w, `[{"id":3,"name":"Checking","currency":"EUR","everyday":true,"role":"owner"}]`)
		case r.URL.Path == "/api/v1/groups/5":
			io.WriteString(w, `{"id":5,"name":"Home","currency":"EUR","members":[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]}`)
		case r.Method == "POST" && r.URL.Path == "/api/v1/groups/5/expenses":
			io.WriteString(w, `{"id":9,"group_id":5}`)
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(sb.Close)
	return sb
}

func (sb *siblingApps) count(prefix string) int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	n := 0
	for _, c := range sb.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// "Bought it" files the purchase in Inventory, Money and Split; a failing app
// is retried without repeating the steps that already worked.
func TestBoughtHandOff(t *testing.T) {
	sb := newSiblingApps(t)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{
		"inventory_mode": "direct", "inventory_url": sb.URL, "inventory_token": "inv-token",
		"money_mode": "direct", "money_url": sb.URL, "money_token": "money-token",
		"split_mode": "direct", "split_url": sb.URL, "split_token": "split-token"})
	wid := num(a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Desk lamp", "price": 4999, "currency": "EUR"}), "id")
	sb.mu.Lock()
	sb.failMoney = true
	sb.mu.Unlock()
	res := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/bought", wid), map[string]any{"split": true, "split_group_id": 5, "store": "Lamp Shop"})
	w := res["wish"].(map[string]any)
	if w["status"] != "bought" || res["pending"] != true || num(w, "inventory_item_id") != 41 || w["money_tx_id"] != nil {
		t.Fatalf("first attempt %v", res)
	}
	sb.mu.Lock()
	sb.failMoney = false
	sb.mu.Unlock()
	// let the retry come due and run it
	if _, err := a.st.DB.Exec(`UPDATE pocket_actions SET next_at = 0`); err != nil {
		t.Fatal(err)
	}
	a.s.drainActions(context.Background(), 0)
	got := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d", wid), nil)["wish"].(map[string]any)
	if num(got, "inventory_item_id") != 41 || num(got, "money_tx_id") != 77 || num(got, "split_expense_id") != 9 || !strings.Contains(got["handoff_note"].(string), "Pocket Money") {
		t.Fatalf("after retry %v", got)
	}
	if n := sb.count("POST /api/v1/items"); n != 1 {
		t.Fatalf("inventory created %d times", n)
	}
	if n := sb.count("POST /api/v1/groups/5/expenses"); n != 1 {
		t.Fatalf("split created %d times", n)
	}
	if n := sb.count("POST /api/v1/transactions"); n != 2 { // the failed one and the one that worked
		t.Fatalf("money calls %d", n)
	}
	// gift ideas don't go into my inventory unless asked
	g := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Mum", "kind": "gifts", "recipient": "Mum"})
	gid := num(a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": num(g, "id"), "title": "Scarf", "price": 3000}), "id")
	res = a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/bought", gid), map[string]any{})
	if w := res["wish"].(map[string]any); w["inventory_item_id"] != nil || num(w, "money_tx_id") != 77 {
		t.Fatalf("gift bought %v", res)
	}
	// a person without connections just marks it bought
	bw := num(a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"title": "Book", "price": 1500}), "id")
	res = a.ok(a.bob, "POST", fmt.Sprintf("/api/v1/wishes/%d/bought", bw), map[string]any{})
	if w := res["wish"].(map[string]any); w["status"] != "bought" || w["inventory_item_id"] != nil {
		t.Fatalf("bob bought %v", res)
	}
	a.s.drainActions(context.Background(), 0)
	if n := sb.count("POST /api/v1/items"); n != 1 {
		t.Fatalf("bob's purchase used alice's inventory: %d", n)
	}
}

// Another Pocket app acting through a Core link: wishes:read reads, only
// wishes:write adds, and it changes only the wishes it added.
func TestCoreLinkScopes(t *testing.T) {
	links := map[string]string{"pcx_read": "wishes:read", "pcx_write": "wishes:write"}
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		scope, ok := links[in.Token]
		if !ok {
			io.WriteString(w, `{"active":false}`)
			return
		}
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"wishlist","user":{"id":2,"sub":"bob@example.com","email":"bob@example.com","name":"Bob","role":"member"},"app":{"id":5,"slug":"clips","name":"Pocket Clips"}}`, scope)
	}))
	t.Cleanup(coreSrv.Close)
	a := newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
	mineID := num(a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"title": "Bob's own"}), "id")
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Alice's"})
	_, _, ws := a.call("pcx_read", "GET", "/api/v1/wishes", nil)
	if len(ws) != 1 || ws[0].(map[string]any)["title"] != "Bob's own" {
		t.Fatalf("read link sees %v", ws)
	}
	a.expect(403, "pcx_read", "POST", "/api/v1/wishes", map[string]any{"title": "x"})
	a.expect(403, "pcx_read", "GET", "/api/v1/shared", nil)
	a.expect(403, "pcx_read", "GET", "/api/v1/reservations", nil)
	a.expect(403, "pcx_write", "GET", "/api/v1/settings", nil)
	added := a.expect(201, "pcx_write", "POST", "/api/v1/wishes", map[string]any{"title": "From Clips", "no_lookup": true})
	if added["source"] != "clips" {
		t.Fatalf("source %v", added["source"])
	}
	a.expect(200, "pcx_write", "PUT", fmt.Sprintf("/api/v1/wishes/%d", num(added, "id")), map[string]any{"notes": "edited by clips"})
	a.expect(403, "pcx_write", "PUT", fmt.Sprintf("/api/v1/wishes/%d", mineID), map[string]any{"title": "hijacked"})
	a.expect(403, "pcx_write", "DELETE", fmt.Sprintf("/api/v1/wishes/%d", mineID), nil)
	a.expect(204, "pcx_write", "DELETE", fmt.Sprintf("/api/v1/wishes/%d", num(added, "id")), nil)
}

// Export and import stay with the person doing them.
func TestExportImport(t *testing.T) {
	a := newApp(t, nil)
	lid := num(a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Gadgets"}), "id")
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": lid, "title": "Watch", "price": 20000})
	code, doc, _ := a.call(a.alice, "GET", "/api/v1/export.json", nil)
	if code != 200 || len(doc["wishes"].([]any)) != 1 {
		t.Fatalf("export %d %v", code, doc)
	}
	counts := a.ok(a.bob, "POST", "/api/v1/import", doc)
	if num(counts, "lists") != 1 || num(counts, "wishes") != 1 {
		t.Fatalf("import %v", counts)
	}
	_, _, bobs := a.call(a.bob, "GET", "/api/v1/wishes", nil)
	_, _, alices := a.call(a.alice, "GET", "/api/v1/wishes", nil)
	if len(bobs) != 1 || len(alices) != 1 {
		t.Fatalf("bob %d alice %d", len(bobs), len(alices))
	}
	// importing again adds nothing
	if again := a.ok(a.bob, "POST", "/api/v1/import", doc); num(again, "wishes") != 0 {
		t.Fatalf("second import %v", again)
	}
}
