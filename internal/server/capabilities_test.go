package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/config"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Wishlist registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "wishes:read", "write": "wishes:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "wishlist", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" }), fc
}

// fakeShop sells Trail Shoes (with a picture) at /p/1 and a Camp Stove at
// /p/2; /blank says nothing about a product and anything else is not there.
func fakeShop(t *testing.T) string {
	t.Helper()
	product := `<html><head><title>Shop</title><meta property="og:site_name" content="Test Shop">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"%s"%s,
"offers":{"@type":"Offer","price":"%s","priceCurrency":"EUR","availability":"https://schema.org/InStock"}}</script></head></html>`
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/img.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n0000"))
		case "/p/1":
			fmt.Fprintf(w, product, "Trail Shoes", `,"image":"/img.png"`, "129.00")
		case "/p/2":
			fmt.Fprintf(w, product, "Camp Stove", "", "49.90")
		case "/blank":
			io.WriteString(w, `<html><head></head><body></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(shop.Close)
	return shop.URL
}

// items are the rows of a capability answer.
func items(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// item finds the row with that title.
func item(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range items(t, m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

func titles(rows []map[string]any) string {
	var out []string
	for _, r := range rows {
		out = append(out, r["title"].(string))
	}
	return strings.Join(out, ", ")
}

func sid(m map[string]any) string { return strconv.FormatInt(num(m, "id"), 10) }

// wish reads one of the person's wishes back.
func (a *app) wish(tok, id string) map[string]any {
	a.t.Helper()
	return a.ok(tok, "GET", "/api/v1/wishes/"+id, nil)["wish"].(map[string]any)
}

// wishesOfAll is everything a person has, in every state.
func (a *app) wishesOfAll(tok string) []any {
	a.t.Helper()
	_, _, all := a.call(tok, "GET", "/api/v1/wishes?status=all", nil)
	return all
}

// backdate moves a wish and its first price to an earlier moment.
func (a *app) backdate(id, at string) {
	a.t.Helper()
	for _, q := range []string{`UPDATE wishes SET created_at = ? WHERE id = ?`, `UPDATE price_history SET at = ? WHERE wish_id = ?`} {
		if _, err := a.st.DB.Exec(q, at, id); err != nil {
			a.t.Fatal(err)
		}
	}
}

func TestPocketSearch(t *testing.T) {
	a := newApp(t, nil)
	shoes := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Trail Shoes", "price": 12900, "currency": "EUR", "url": "https://shop.example/p/1", "store": "Test Shop", "no_lookup": true})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Lego castle", "notes": "for the shelf in the hall", "tags": "toys, bricks"})
	gifts := a.ok(a.alice, "POST", "/api/v1/lists", map[string]any{"name": "Birthday ideas", "kind": "gifts", "recipient": "Ana"})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"list_id": num(gifts, "id"), "title": "Teapot", "price": 3500, "currency": "EUR"})
	a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"title": "Trail mix"})

	got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=trail", nil))
	if len(got) != 1 {
		t.Fatalf("trail: %v", got)
	}
	if it := got[0]; it["id"] != sid(shoes) || it["title"] != "Trail Shoes" || it["kind"] != "wish" || it["snippet"] != "129.00 EUR · Test Shop · My wishlist" ||
		it["url"] != "/wishes/"+sid(shoes) || it["link"] != "https://shop.example/p/1" || it["date"] != shoes["created_at"] {
		t.Fatalf("the wish: %v", it)
	}
	// every word counts, wherever it stands: name, shop, notes, tags, who a gift is for
	for q, want := range map[string]string{
		"shoes+test": "Trail Shoes", "TEST+trail": "Trail Shoes", "hall": "Lego castle", "bricks+lego": "Lego castle", "ana": "Teapot", "tea+ana": "Teapot",
		"trail+lego": "", "shoes+hall": "",
	} {
		if got := titles(items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil))); got != want {
			t.Errorf("%q finds %q, want %q", q, got, want)
		}
	}
	// the line says where the words were found when it would not show them
	for q, want := range map[string]string{"hall": "My wishlist · for the shelf in the hall", "bricks": "My wishlist · Tags: toys, bricks", "ana": "35.00 EUR · Birthday ideas · For Ana"} {
		if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 1 || got[0]["snippet"] != want {
			t.Errorf("%q: %v, want the line %q", q, got, want)
		}
	}
	// nothing to look for finds nothing, and % and _ are not wildcards
	for _, q := range []string{"", "%3F%3F", "*", "%25", "_", "-+%E2%80%93", "tr%25l", "tra_l"} {
		if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Errorf("%q finds %v", q, titles(got))
		}
	}
	// what is still wanted comes first, then what was bought, then what was put away
	old := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Trail map", "price": 900, "currency": "EUR"})
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(old)+"/status", map[string]any{"status": "archived"})
	done := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Trail socks", "price": 1500, "currency": "EUR", "store": "Sock Shop"})
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(done)+"/bought", map[string]any{"price": 1200, "inventory": false, "money": false})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Poles", "notes": "for the trail"})
	got = items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=trail", nil))
	if titles(got) != "Trail Shoes, Poles, Trail socks, Trail map" || got[2]["snippet"] != "Bought · 12.00 EUR · Sock Shop · My wishlist" || got[3]["snippet"] != "Archived · 9.00 EUR · My wishlist" {
		t.Fatalf("order and lines: %v", got)
	}
	if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=trail&limit=2", nil)); titles(got) != "Trail Shoes, Poles" {
		t.Fatalf("limit 2: %v", got)
	}
	// another person's wishes never appear, not even on a list shared with them
	a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/lists/%d/shares", num(shoes, "list_id")), map[string]any{"kind": "user", "email": "carol@example.com"})
	if got := a.ok(a.carol, "GET", fmt.Sprintf("/api/v1/shared/%d", num(shoes, "list_id")), nil); len(wishesOf(got)) == 0 {
		t.Fatalf("carol does not see the shared list: %v", got)
	}
	for _, q := range []string{"trail", "lego", "hall", "ana"} {
		if got := items(t, a.ok(a.carol, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("carol finds alice's %q: %v", q, got)
		}
	}
	if got := items(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q=trail", nil)); titles(got) != "Trail mix" {
		t.Fatalf("bob's trail: %v", got)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=trail", nil)
	// the old route still answers a bare list of what is wanted
	if _, _, ws := a.call(a.alice, "GET", "/api/v1/wishes?q=trail", nil); len(ws) != 2 {
		t.Fatalf("/wishes?q= changed: %v", ws)
	}
}

func TestPocketDay(t *testing.T) {
	a := newApp(t, nil)
	today := store.Today()
	const before = "2026-01-01T10:00:00Z"
	lights := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Bike lights", "price": 2500, "currency": "EUR"})
	tent := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Tent", "price": 20000, "currency": "EUR", "store": "Outdoor Shop"})
	kettle := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Kettle", "price": 4000, "currency": "EUR"})
	phones := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Headphones", "price": 9900, "currency": "EUR"})
	for _, w := range []map[string]any{tent, kettle, phones} {
		a.backdate(sid(w), before)
	}
	a.ok(a.alice, "PUT", "/api/v1/wishes/"+sid(tent), map[string]any{"price": 15000})
	// a watched price is written down once a day even when it did not move
	if _, err := a.st.DB.Exec(`INSERT INTO price_history (wish_id, at, price, currency, source) VALUES (?, ?, 4000, 'EUR', 'check')`, sid(kettle), store.Now()); err != nil {
		t.Fatal(err)
	}
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(phones)+"/bought", map[string]any{"price": 8900, "store": "Hi-Fi", "inventory": false, "money": false})

	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil)
	if day["date"] != today || day["summary"] != "1 wish added, 1 bought, 1 price drop" || len(items(t, day)) != 3 {
		t.Fatalf("day: %v", day)
	}
	if it := item(t, day, "Headphones"); it["id"] != sid(phones) || it["kind"] != "bought" || it["done"] != true || num(it, "amount") != 8900 || it["currency"] != "EUR" ||
		it["sub"] != "Hi-Fi" || it["url"] != "/wishes/"+sid(phones) || it["time"] != nil {
		t.Fatalf("bought: %v", it)
	}
	if it := item(t, day, "Tent"); it["kind"] != "price" || num(it, "amount") != 15000 || it["currency"] != "EUR" || it["sub"] != "Outdoor Shop · down from 200.00 EUR" ||
		it["url"] != "/wishes/"+sid(tent) || len(it["time"].(string)) != 5 {
		t.Fatalf("price: %v", it)
	}
	if it := item(t, day, "Bike lights"); it["kind"] != "added" || num(it, "amount") != 2500 || it["currency"] != "EUR" || it["sub"] != "My wishlist" || it["done"] != nil ||
		len(it["time"].(string)) != 5 {
		t.Fatalf("added: %v", it)
	}
	ms := day["metrics"].([]any)
	for i, want := range [][2]string{{"Added", "1"}, {"Bought", "1"}, {"Spent", "89.00 EUR"}, {"Price changes", "1"}} {
		if m := ms[i].(map[string]any); len(ms) != 4 || m["label"] != want[0] || m["value"] != want[1] {
			t.Fatalf("metrics: %v", ms)
		}
	}
	// up again, past where the day started: one change, said as it ended
	a.ok(a.alice, "PUT", "/api/v1/wishes/"+sid(tent), map[string]any{"price": 22000})
	day = a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil)
	if it := item(t, day, "Tent"); day["summary"] != "1 wish added, 1 bought, 1 price change" || num(it, "amount") != 22000 || it["sub"] != "Outdoor Shop · up from 200.00 EUR" {
		t.Fatalf("a rise: %v", day)
	}
	// back where it started: nothing changed that day
	a.ok(a.alice, "PUT", "/api/v1/wishes/"+sid(tent), map[string]any{"price": 20000})
	if day = a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil); day["summary"] != "1 wish added, 1 bought" || len(items(t, day)) != 2 {
		t.Fatalf("back to the start: %v", day)
	}
	// a wish added and bought the same day shows once
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(lights)+"/status", map[string]any{"status": "bought"})
	if day = a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil); day["summary"] != "1 wish added, 2 bought" || len(items(t, day)) != 2 || item(t, day, "Bike lights")["kind"] != "bought" {
		t.Fatalf("added and bought: %v", day)
	}

	// the day is the caller's: half past eleven at night in UTC is the next day in Sarajevo
	owl := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Night lamp"})
	a.backdate(sid(owl), "2026-03-10T23:30:00Z")
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-11?tz=Europe/Sarajevo", nil); d["summary"] != "1 wish added" || item(t, d, "Night lamp")["time"] != "00:30" ||
		item(t, d, "Night lamp")["amount"] != nil {
		t.Fatalf("in Sarajevo: %v", d)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-10?tz=UTC", nil); item(t, d, "Night lamp")["time"] != "23:30" {
		t.Fatalf("in UTC: %v", d)
	}
	// a day with nothing, and somebody else's day
	for _, p := range []string{"2026-03-10?tz=Europe/Sarajevo", "2026-03-11?tz=UTC", "2019-03-04"} {
		if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+p, nil); d["summary"] != nil || d["metrics"] != nil || len(items(t, d)) != 0 {
			t.Fatalf("an empty day %s: %v", p, d)
		}
	}
	if d := a.ok(a.bob, "GET", "/api/v1/pocket/day/"+today, nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("bob sees alice's day: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/yesterday", nil)
	a.expect(401, "", "GET", "/api/v1/pocket/day/"+today, nil)
	// the old route still answers as before
	if old := a.ok(a.alice, "GET", "/api/v1/day/"+today, nil); old["date"] != today || len(old["bought"].([]any)) != 2 || old["added"] == nil {
		t.Fatalf("/day changed: %v", old)
	}
}

func TestPocketWidget(t *testing.T) {
	a := newApp(t, nil)
	// nothing wanted: nothing to show
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || w["metrics"] != nil || w["url"] != nil || len(items(t, w)) != 0 {
		t.Fatalf("without wishes: %v", w)
	}
	lights := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Bike lights", "price": 2500, "currency": "EUR", "quantity": 2})
	tent := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Tent", "price": 20000, "currency": "EUR", "store": "Outdoor Shop"})
	a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Kayak", "price": 90000, "currency": "USD"})
	a.backdate(sid(lights), "2026-01-01T10:00:00Z")
	a.backdate(sid(tent), "2026-01-02T10:00:00Z")
	metrics := func(w map[string]any, want ...[2]string) {
		t.Helper()
		ms, _ := w["metrics"].([]any)
		if len(ms) != len(want) {
			t.Fatalf("metrics: %v, want %v", ms, want)
		}
		for i, m := range ms {
			if m := m.(map[string]any); m["label"] != want[i][0] || m["value"] != want[i][1] {
				t.Fatalf("metrics: %v, want %v", ms, want)
			}
		}
	}
	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	rows := items(t, w)
	if w["line"] != "3 wishes" || w["url"] != "/wishes" || titles(rows) != "Kayak, Tent, Bike lights" || rows[1]["detail"] != "200.00 EUR · Outdoor Shop" ||
		rows[1]["id"] != sid(tent) || rows[1]["url"] != "/wishes/"+sid(tent) || rows[2]["detail"] != "25.00 EUR" {
		t.Fatalf("widget: %v", w)
	}
	// the total is what the dashboard says: the home currency only, two lights counted twice
	metrics(w, [2]string{"Wishes", "3"}, [2]string{"Price drops", "0"}, [2]string{"Total", "250.00 EUR"})
	if d := a.ok(a.alice, "GET", "/api/v1/dashboard", nil); d["value_text"] != "250.00 EUR" || num(d, "at_target") != 0 {
		t.Fatalf("dashboard: %v", d)
	}

	// a wish at its target price leads, and the line is about it
	a.ok(a.alice, "PUT", "/api/v1/wishes/"+sid(lights), map[string]any{"price": 1900, "target_price": 2000})
	w = a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	rows = items(t, w)
	if w["line"] != "1 price drop" || w["url"] != "/wishes?view=drops" || titles(rows) != "Bike lights, Kayak, Tent" || rows[0]["detail"] != "19.00 EUR, was 25.00 EUR" {
		t.Fatalf("with a drop: %v", w)
	}
	metrics(w, [2]string{"Wishes", "3"}, [2]string{"Price drops", "1"}, [2]string{"Total", "238.00 EUR"})
	a.ok(a.alice, "PUT", "/api/v1/wishes/"+sid(tent), map[string]any{"price": 15000, "target_price": 16000, "priority": 3})
	if w = a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != "2 price drops" || titles(items(t, w)) != "Tent, Bike lights, Kayak" {
		t.Fatalf("two drops, the one wanted most first: %v", w)
	}
	// what was bought or put away is no longer wanted
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(tent)+"/status", map[string]any{"status": "bought"})
	a.ok(a.alice, "POST", "/api/v1/wishes/"+sid(lights)+"/status", map[string]any{"status": "archived"})
	w = a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	if w["line"] != "1 wish" || w["url"] != "/wishes" || titles(items(t, w)) != "Kayak" || items(t, w)[0]["detail"] != "900.00 USD" {
		t.Fatalf("one wish left: %v", w)
	}
	metrics(w, [2]string{"Wishes", "1"}, [2]string{"Price drops", "0"})

	// at most eight rows, and only the person's own
	for i := range 10 {
		a.ok(a.bob, "POST", "/api/v1/wishes", map[string]any{"title": fmt.Sprintf("Book %d", i)})
	}
	w = a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil)
	if rows := items(t, w); w["line"] != "10 wishes" || len(rows) != 8 || rows[0]["title"] != "Book 9" || rows[0]["detail"] != nil {
		t.Fatalf("bob's widget: %v", w)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/widget", nil)
}

// capture posts a quick line and returns the answer.
func capture(a *app, tok, requestID, text string, more map[string]any) (int, map[string]any) {
	body := map[string]any{"request_id": requestID, "kind": "wish", "text": text}
	for k, v := range more {
		body[k] = v
	}
	code, m, _ := a.call(tok, "POST", "/api/v1/pocket/capture", body)
	return code, m
}

// waitPhoto waits for the picture a quick line's wish gets once the answer is out.
func (a *app) waitPhoto(tok, id string) map[string]any {
	a.t.Helper()
	for range 200 {
		if w := a.wish(tok, id); w["photo"] != "" {
			return w
		}
		time.Sleep(25 * time.Millisecond)
	}
	a.t.Fatalf("wish %s never got its picture", id)
	return nil
}

func TestPocketCapture(t *testing.T) {
	a := newApp(t, nil)
	shop := fakeShop(t)
	keep := func(tok, requestID, text string, more map[string]any) map[string]any {
		t.Helper()
		code, m := capture(a, tok, requestID, text, more)
		if code != 201 || m["url"] != "/wishes/"+m["id"].(string) {
			t.Fatalf("capture %q → %d %v", text, code, m)
		}
		w := a.wish(tok, m["id"].(string))
		if w["title"] != m["title"] {
			t.Fatalf("capture %q answered %v for the wish %v", text, m, w)
		}
		return w
	}
	refuse := func(tok, requestID, text, want string) {
		t.Helper()
		if code, m := capture(a, tok, requestID, text, nil); code != 400 || m["error"] != want {
			t.Fatalf("capture %q → %d %v, want 400 %q", text, code, m, want)
		}
	}

	// a name: a wish on the first wishlist, made on the spot
	if ls, _ := a.st.ListLists(t.Context(), a.ids["Alice"], false); len(ls) != 0 {
		t.Fatalf("alice starts with lists: %v", ls)
	}
	code, first := capture(a, a.alice, "req-0000000000000001", "  Hiking   boots ", nil)
	if code != 201 || first["title"] != "Hiking boots" || first["url"] != "/wishes/"+first["id"].(string) {
		t.Fatalf("capture: %d %v", code, first)
	}
	boots := a.wish(a.alice, first["id"].(string))
	if boots["title"] != "Hiking boots" || boots["list_name"] != "My wishlist" || boots["status"] != "wanted" || boots["source"] != "" || boots["url"] != "" ||
		num(boots, "priority") != 2 || num(boots, "quantity") != 1 || boots["track_price"] != false {
		t.Fatalf("the wish: %v", boots)
	}
	// the same request again: the same answer, nothing added
	if code, again := capture(a, a.alice, "req-0000000000000001", "  Hiking   boots ", nil); code != 201 || again["id"] != first["id"] || again["title"] != first["title"] ||
		again["url"] != first["url"] || len(a.wishesOfAll(a.alice)) != 1 {
		t.Fatalf("a retry: %d %v, %d wishes", code, again, len(a.wishesOfAll(a.alice)))
	}
	refuse(a.alice, "req-0000000000000001", "Other boots", "this was already kept with other words")

	// a link alone: the page says what it is, as POST /wishes reads it
	shoes := keep(a.alice, "req-0000000000000002", shop+"/p/1", nil)
	if shoes["title"] != "Trail Shoes" || shoes["url"] != shop+"/p/1" || num(shoes, "price") != 12900 || shoes["currency"] != "EUR" || shoes["store"] != "Test Shop" ||
		shoes["track_price"] != true || shoes["availability"] != "in_stock" || shoes["notes"] != "" || num(shoes, "list_id") != num(boots, "list_id") {
		t.Fatalf("wish from a link: %v", shoes)
	}
	a.waitPhoto(a.alice, sid(shoes))
	a.expect(200, a.alice, "GET", "/api/v1/wishes/"+sid(shoes)+"/photo", nil)
	// the person's own name for it wins; the link is taken out of the line with what stood around it
	for i, c := range []struct {
		text, title, link, notes string
		more                     map[string]any
	}{
		{"Stove for the hut - " + shop + "/p/2.", "Stove for the hut", "/p/2", "", nil},
		{"(" + shop + "/p/2) the small one", "the small one", "/p/2", "", nil},
		{shop + "/p/2\nfor the trip in May", "Camp Stove", "/p/2", "for the trip in May", nil},
		{"Burner\nsee " + shop + "/p/2?ref=a&b=1, ask Ana", "Burner", "/p/2?ref=a&b=1", "see ask Ana", nil},
		{"ask Ana first", "Gas stove", "/p/2", "ask Ana first", map[string]any{"title": "Gas stove: " + shop + "/p/2"}},
		{"in green, " + shop + "/p/2", "Gas stove", "/p/2", "in green", map[string]any{"title": "Gas stove"}},
	} {
		w := keep(a.alice, "req-10000000000000"+strconv.Itoa(10+i), c.text, c.more)
		if w["title"] != c.title || w["url"] != shop+c.link || w["notes"] != c.notes || num(w, "price") != 4990 || w["store"] != "Test Shop" {
			t.Fatalf("%q %v → %q, link %q, notes %q, price %v", c.text, c.more, w["title"], w["url"], w["notes"], w["price"])
		}
	}
	// a page that cannot be read: the wish is kept when the person said what it is
	gone := keep(a.alice, "req-0000000000000003", "Old radio "+shop+"/gone", nil)
	if gone["title"] != "Old radio" || gone["url"] != shop+"/gone" || num(gone, "price") != 0 || gone["track_price"] != false || gone["store"] == "" {
		t.Fatalf("a link that is gone: %v", gone)
	}
	before := len(a.wishesOfAll(a.alice))
	const unreadable = "that page could not be read, write what it is in front of the link"
	refuse(a.alice, "req-0000000000000004", shop+"/gone", unreadable)
	refuse(a.alice, "req-0000000000000005", shop+"/blank", unreadable)
	if len(a.wishesOfAll(a.alice)) != before {
		t.Fatal("a refused line was kept")
	}
	// a refused line may be sent again with the same id once it is put right
	if w := keep(a.alice, "req-0000000000000004", shop+"/p/2", nil); w["title"] != "Camp Stove" {
		t.Fatalf("the same id after a refusal: %v", w)
	}

	// more lines become the notes; a long line is cut for the name and stays whole in the notes
	if w := keep(a.alice, "req-0000000000000007", "Rain jacket\nsize M,\nthe blue one", nil); w["title"] != "Rain jacket" || w["notes"] != "size M,\nthe blue one" {
		t.Fatalf("more lines: %v", w)
	}
	long := strings.Repeat("ž", 200)
	if w := keep(a.alice, "req-0000000000000008", long, nil); len(w["title"].(string)) > 300 || !strings.HasSuffix(w["title"].(string), "…") || w["notes"] != long {
		t.Fatalf("a long line: %v", w)
	}

	// what the route checks itself
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "short", "text": "x"})
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000009", "kind": "list", "text": "x"})
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000009", "text": ""})
	a.expect(401, "", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000009", "text": "x"})

	// per person: a request id is one's own, and the wish lands on one's own list
	mine := len(a.wishesOfAll(a.alice))
	bobs := keep(a.bob, "req-0000000000000001", "Chess set", nil)
	if bobs["title"] != "Chess set" || num(bobs, "user_id") != a.ids["Bob"] || num(bobs, "list_id") == num(boots, "list_id") || len(a.wishesOfAll(a.bob)) != 1 || len(a.wishesOfAll(a.alice)) != mine {
		t.Fatalf("bob's line: %v", bobs)
	}
	// a second line goes to the list the first one made
	if w := keep(a.bob, "req-0000000000000002", "Chess clock", nil); num(w, "list_id") != num(bobs, "list_id") {
		t.Fatalf("bob's second line: %v", w)
	}
	if ls, _ := a.st.ListLists(t.Context(), a.ids["Bob"], false); len(ls) != 1 {
		t.Fatalf("bob has %d lists", len(ls))
	}
}

// The same line sent twice at once is kept once.
func TestPocketCaptureAtOnce(t *testing.T) {
	a := newApp(t, nil)
	shop := fakeShop(t)
	var wg sync.WaitGroup
	ids := make([]string, 6)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, m := capture(a, a.alice, "req-0000000000000001", shop+"/p/2", nil); code == 201 {
				ids[i] = m["id"].(string)
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("answers: %v", ids)
		}
	}
	if n := len(a.wishesOfAll(a.alice)); n != 1 {
		t.Fatalf("%d wishes kept", n)
	}
	if ls, _ := a.st.ListLists(t.Context(), a.ids["Alice"], false); len(ls) != 1 {
		t.Fatalf("%d first wishlists made", len(ls))
	}
}

// A kept line tells the Core bus once, a retry not again.
func TestPocketCaptureEvent(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.CoreURL = "http://core.invalid"
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ctx := context.Background()
	if err := s.core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	u := &store.User{Email: "ana@example.com", Name: "Ana", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	a := &app{t: t, url: ts.URL, s: s, st: st}
	for range 2 {
		if code, m := capture(a, tok, "req-0000000000000001", "Hiking boots", nil); code != 201 {
			t.Fatalf("capture → %d %v", code, m)
		}
	}
	rows, err := st.DB.QueryContext(ctx, `SELECT body FROM pocket_outbox ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	added := 0
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		var ev struct {
			Topic   string
			Payload map[string]any
		}
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Topic != "wishlist.wish_added" {
			continue
		}
		added++
		if ev.Payload["wish"] != "Hiking boots" {
			t.Errorf("payload = %v", ev.Payload)
		}
	}
	if added != 1 {
		t.Fatalf("wish_added events = %d, want 1", added)
	}
}

// A link token with one pocket:* scope reaches its one route and nothing else.
func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	today := store.Today()
	tent := a.ok(a.alice, "POST", "/api/v1/wishes", map[string]any{"title": "Tent", "price": 20000, "currency": "EUR"})
	line := func(requestID string) map[string]any {
		return map[string]any{"request_id": requestID, "kind": "wish", "text": "Camp stove"}
	}
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=tent", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + today, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", line("req-0000000000000001"), 201},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_search", "POST", "/api/v1/pocket/capture", line("req-0000000000000002"), 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=tent", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=tent", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/pocket/capture", line("req-0000000000000003"), 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/search?q=tent", nil, 403},
		// the rest of the app
		{"pcx_alice_search", "GET", "/api/v1/wishes?q=tent", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/wishes/" + sid(tent), nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/lists", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/day/" + today, nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/dashboard", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/home", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/wishes/" + sid(tent) + "/check", nil, 403},
		{"pcx_alice_capture", "POST", "/api/v1/wishes", map[string]any{"title": "x"}, 403},
		{"pcx_alice_capture", "POST", "/api/v1/preview", map[string]any{"url": "https://shop.example"}, 403},
		{"pcx_alice_capture", "PUT", "/api/v1/wishes/" + sid(tent), map[string]any{"title": "x"}, 403},
		{"pcx_alice_capture", "DELETE", "/api/v1/wishes/" + sid(tent), nil, 403},
		{"pcx_alice_capture", "POST", "/api/v1/wishes/" + sid(tent) + "/bought", map[string]any{}, 403},
		{"pcx_alice_capture", "GET", "/api/v1/shared", nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/settings", nil, 403},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=tent", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", line("req-0000000000000004"), 403},
		{"pcx_alice_read", "GET", "/api/v1/wishes?q=tent", nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only
	if got := items(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=tent", nil)); titles(got) != "Tent" {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := items(t, a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=tent", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's wish: %v", got)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); len(items(t, w)) != 0 {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if d := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+today, nil); len(items(t, d)) != 0 {
		t.Fatalf("bob's day through the link: %v", d)
	}
	// only the one allowed capture landed, as a wish the sending app added; a retry adds nothing
	if code, m := capture(a, "pcx_alice_capture", "req-0000000000000001", "Camp stove", nil); code != 201 {
		t.Fatalf("retry through the link: %d %v", code, m)
	}
	_, _, all := a.call(a.alice, "GET", "/api/v1/wishes?sort=added", nil)
	if len(all) != 2 || all[0].(map[string]any)["title"] != "Camp stove" || all[0].(map[string]any)["source"] != "hub" {
		t.Fatalf("alice's wishes: %v", all)
	}
	if code, m := capture(a, "pcx_bob_capture", "req-0000000000000001", "Chess set", nil); code != 201 || len(a.wishesOfAll(a.bob)) != 1 || len(a.wishesOfAll(a.alice)) != 2 {
		t.Fatalf("bob's line through the link: %d %v", code, m)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/day/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/dashboard"), false},
		{core.ScopeCapture, req("GET", "/api/v1/pocket/capture"), false},
		{core.ScopeCapture, req("POST", "/api/v1/wishes"), false},
		{"wishes:read", req("GET", "/api/v1/pocket/widget"), false},
		{"wishes:write", req("POST", "/api/v1/pocket/capture"), false},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Wishlist tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "wishlist" || m["category"] != "Home" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	cap, _ := m["capture"].(map[string]any)
	if search == nil || fmt.Sprint(search["bangs"]) != "[wish wishlist]" || fmt.Sprint(search["kinds"]) != "[wish]" || day == nil || day["label"] != "Wishlist" || day["one"] != "wish" ||
		day["many"] != "wishes" || widget == nil || widget["title"] != "Wishlist" || cap == nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, cap)
	}
	if k := cap["kinds"].([]any)[0].(map[string]any); k["kind"] != "wish" || fmt.Sprint(k["fields"]) != "[title]" {
		t.Fatalf("capture kinds: %v", cap)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"wishes:read", "wishes:write", core.ScopeMoveExport, core.ScopeMoveImport, core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
}

func TestCaptureWish(t *testing.T) {
	for _, c := range []struct {
		title, text, wantTitle, wantLink, wantNotes string
	}{
		{"", "Hiking boots", "Hiking boots", "", ""},
		{"", "https://shop.example/p/1", "", "https://shop.example/p/1", ""},
		{"", "HTTPS://Shop.example/p/1?x=1#top!", "", "HTTPS://Shop.example/p/1?x=1#top", ""},
		{"", "Boots: https://shop.example/p/1, size 44", "Boots size 44", "https://shop.example/p/1", ""},
		{"", "-40° sleeping bag https://shop.example/bag", "-40° sleeping bag", "https://shop.example/bag", ""},
		{"", "https://shop.example/a and https://shop.example/b", "and https://shop.example/b", "https://shop.example/a", ""},
		{"", "\n\nBoots\n\nbrown\n", "Boots", "", "brown"},
		{"Boots", "", "Boots", "", ""},
		{"Boots", "brown, https://shop.example/p/1", "Boots", "https://shop.example/p/1", "brown"},
		{"Boots https://shop.example/p/1", "see https://other.example", "Boots", "https://shop.example/p/1", "see https://other.example"},
		{"", "ftp://shop.example/p/1", "ftp://shop.example/p/1", "", ""},
		{"", "Mug (https://shop.example/mug_(blue)).", "Mug", "https://shop.example/mug_(blue)", ""},
	} {
		title, link, notes := captureWish(core.CaptureInput{Title: c.title, Text: c.text})
		if title != c.wantTitle || link != c.wantLink || notes != c.wantNotes {
			t.Errorf("%q / %q → %q, %q, %q; want %q, %q, %q", c.title, c.text, title, link, notes, c.wantTitle, c.wantLink, c.wantNotes)
		}
	}
}
