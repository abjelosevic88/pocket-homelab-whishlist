package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// huntWorld is a fake web: a SearXNG, four shops and an OpenAI-compatible AI.
type huntWorld struct {
	search, shops, ai, push *httptest.Server
	mu                      sync.Mutex
	priceA                  string
	pushes                  []string
	prompts                 []string
}

func newHuntWorld(t *testing.T) *huntWorld {
	hw := &huntWorld{priceA: "120.00"}
	product := func(name, price, extra, body string) string {
		return fmt.Sprintf(`<html><head><title>%s</title><script type="application/ld+json">{"@type":"Product","name":%q,"offers":{"@type":"Offer","price":%q,"priceCurrency":"EUR","availability":"https://schema.org/InStock"%s}}</script></head><body>%s</body></html>`, name, name, price, extra, body)
	}
	hw.shops = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hw.mu.Lock()
		pa := hw.priceA
		hw.mu.Unlock()
		switch r.URL.Path {
		case "/a/x100":
			io.WriteString(w, product("Acme X100 Headphones Black", pa, "", `<a href="/a/versand">Versand und Lieferung</a>`))
		case "/a/versand":
			io.WriteString(w, `<html><body><h1>Versand</h1><p>Wir liefern nach Deutschland, Österreich und Bosnien und Herzegowina. Versandkosten 9,90 €.</p></body></html>`)
		case "/b/case":
			io.WriteString(w, product("Case for Acme X100", "19.00", "", ""))
		case "/d/x100":
			io.WriteString(w, product("Acme X100 Headphones", "110.00", `,"shippingDetails":{"shippingRate":{"value":"4.95","currency":"EUR"},"shippingDestination":{"addressCountry":"DE"}}`, ""))
		case "/e/x100":
			io.WriteString(w, product("Acme X100 Headphones", "100.00", "", `<p>Leider ist kein Versand nach Bosnien und Herzegowina möglich.</p>`))
		default:
			http.NotFound(w, r)
		}
	}))
	hw.search = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			w.WriteHeader(403)
			return
		}
		res := []map[string]string{
			{"url": hw.shops.URL + "/a/x100?utm_source=x", "title": "Acme X100 at Shop A", "content": "Buy now"},
			{"url": hw.shops.URL + "/b/case", "title": "Case for Acme X100", "content": "Protective case 19,00 €"},
			{"url": "http://compare.idealo.test/x100", "title": "Acme X100 ab 105,00 € | 30 Angebote", "content": "Preisvergleich"},
			{"url": hw.shops.URL + "/d/x100", "title": "Acme X100 Shop D", "content": ""},
			{"url": strings.Replace(hw.shops.URL, "127.0.0.1", "localhost", 1) + "/e/x100", "title": "Acme X100 Shop E", "content": ""},
			{"url": "https://www.youtube.com/watch?v=1", "title": "Acme X100 review", "content": ""},
		}
		json.NewEncoder(w).Encode(map[string]any{"results": res})
	}))
	hw.ai = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		hw.mu.Lock()
		hw.prompts = append(hw.prompts, in.Messages[1].Content)
		hw.mu.Unlock()
		var answer string
		if strings.Contains(in.Messages[0].Content, "plan web searches") {
			answer = `Sure! {"product":"Acme X100 Headphones","must_match":["X100"],"exclude":["case"],"queries":[{"q":"Acme X100 price","language":"en"},{"q":"Acme X100 kaufen","language":"de"}]}`
		} else {
			// find the candidate numbers by their shop paths
			p := in.Messages[1].Content
			idx := func(needle string) int {
				for _, line := range strings.Split(p, "\n") {
					if strings.Contains(line, needle) {
						var i int
						fmt.Sscanf(line, "[%d]", &i)
						return i
					}
				}
				return -1
			}
			a, b, c, d := idx("/a/x100"), idx("/b/case"), idx("idealo"), idx("/d/x100")
			hw.mu.Lock()
			pa := hw.priceA
			hw.mu.Unlock()
			answer = fmt.Sprintf("```json\n"+`{"offers":[
{"i":%d,"match":"exact","price":"%s","currency":"EUR","shop":"Shop A","in_stock":true,"note":"ships today"},
{"i":%d,"match":"accessory","price":"19.00","currency":"EUR","shop":"Shop B"},
{"i":%d,"match":"comparison","price":"105.00","currency":"EUR","shop":"idealo","note":"lowest listed price"},
{"i":%d,"match":"exact","price":"50.00","currency":"EUR","shop":"Shop D"}],
"summary":"Shop A has it in stock."}`+"\n```", a, pa, b, c, d)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": answer}}}})
	}))
	hw.push = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hw.mu.Lock()
		hw.pushes = append(hw.pushes, string(b))
		hw.mu.Unlock()
	}))
	t.Cleanup(func() { hw.search.Close(); hw.shops.Close(); hw.ai.Close(); hw.push.Close() })
	return hw
}

func (a *app) waitHunt(id int64) map[string]any {
	a.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		w := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d", id), nil)["wish"].(map[string]any)
		if w["hunt_status"] != "running" {
			return w
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("hunt still running: %v", w["hunt_step"])
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDealHuntWithAI(t *testing.T) {
	hw := newHuntWorld(t)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"ai_provider": "openai", "openai_api_key": "sk-test", "openai_base_url": hw.ai.URL,
		"search_url": hw.search.URL, "deal_markets": "Bosnia and Herzegovina, Germany", "home_currency": "EUR",
		"notify_url": hw.push.URL + "/notify/wishlist", "notify_format": "apprise"})
	st := a.ok(a.alice, "GET", "/api/v1/ai/status", nil)
	if st["ready"] != true || st["provider"] != "openai" || st["search"] != true {
		t.Fatalf("ai status %v", st)
	}
	w := a.expect(202, a.alice, "POST", "/api/v1/hunt", map[string]any{"query": "acme x100 black headphones", "max_text": "130", "auto": true})
	id := num(w, "id")
	w = a.waitHunt(id)
	if w["hunt_status"] != "done" || w["hunt_ai"] != "openai" || w["hunt_summary"] != "Shop A has it in stock." {
		t.Fatalf("hunt %v / %v / %v / %v", w["hunt_status"], w["hunt_error"], w["hunt_ai"], w["hunt_summary"])
	}
	// the best exact offer became the wish: link (tracking stripped), price, the AI's product name, watched
	if !strings.HasSuffix(w["url"].(string), "/a/x100") || num(w, "price") != 12000 || w["title"] != "Acme X100 Headphones" || w["track_price"] != true {
		t.Fatalf("adopted %v %v %v %v", w["url"], w["price"], w["title"], w["track_price"])
	}
	offers := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)
	var got []string
	for _, o := range offers {
		m := o.(map[string]any)
		got = append(got, fmt.Sprintf("%s:%v:%s", m["match"], m["price"], m["source"]))
	}
	// the case (accessory) and Shop D's invented 50.00 are gone; YouTube never got read
	if strings.Join(got, " ") != "exact:12000:page comparison:10500:snippet" {
		t.Fatalf("offers %v", got)
	}
	hw.mu.Lock()
	judge := hw.prompts[len(hw.prompts)-1]
	hw.mu.Unlock()
	if !strings.Contains(judge, "Bosnia and Herzegovina") || strings.Contains(judge, "youtube") || strings.Contains(judge, "utm_source") {
		t.Fatalf("judge prompt:\n%s", judge)
	}
	// another person cannot see or start it
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/wishes/%d/hunt", id), nil)
	// a repeat hunt that finds it 20 % cheaper pushes "better deal"
	hw.mu.Lock()
	hw.priceA = "96.00"
	hw.mu.Unlock()
	a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/hunt", id), map[string]any{})
	w = a.waitHunt(id)
	hw.mu.Lock()
	pushes := strings.Join(hw.pushes, "\n")
	hw.mu.Unlock()
	if num(w, "hunt_best") != 9600 || !strings.Contains(pushes, "Better deal found") {
		t.Fatalf("second hunt best %v pushes %q", w["hunt_best"], pushes)
	}
	// dismiss sticks across hunts; "use" sets the link
	offers = a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)
	cmp := offers[len(offers)-1].(map[string]any)
	a.expect(204, a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/offers/%d/dismiss", id, num(cmp, "id")), nil)
	a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/hunt", id), map[string]any{})
	a.waitHunt(id)
	if n := len(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)); n != 1 {
		t.Fatalf("dismissed offer came back: %d", n)
	}
}

func TestDealHuntKeywordsOnly(t *testing.T) {
	hw := newHuntWorld(t)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"ai_provider": "openai", "search_url": hw.search.URL})
	if st := a.ok(a.alice, "GET", "/api/v1/ai/status", nil); st["ready"] != false {
		t.Fatalf("no key, but AI ready: %v", st)
	}
	id := num(a.expect(202, a.alice, "POST", "/api/v1/hunt", map[string]any{"query": "Acme X100 Headphones"}), "id")
	w := a.waitHunt(id)
	if w["hunt_status"] != "done" || w["hunt_ai"] != "" || !strings.Contains(w["hunt_summary"].(string), "keywords only") {
		t.Fatalf("keyword hunt %v %v", w["hunt_status"], w["hunt_summary"])
	}
	offers := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)
	if len(offers) != 3 { // Shop E (100), D (110), A (120); the case lacks "headphones"
		t.Fatalf("keyword offers %v", offers)
	}
	if first := offers[0].(map[string]any); num(first, "price") != 10000 {
		t.Fatalf("cheapest first: %v", first)
	}
	// no search engine: a clear error
	b := num(a.expect(202, a.bob, "POST", "/api/v1/hunt", map[string]any{"query": "anything"}), "id")
	deadline := time.Now().Add(5 * time.Second)
	for {
		w := a.ok(a.bob, "GET", fmt.Sprintf("/api/v1/wishes/%d", b), nil)["wish"].(map[string]any)
		if w["hunt_status"] == "error" && strings.Contains(w["hunt_error"].(string), "SearXNG") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bob's hunt %v %v", w["hunt_status"], w["hunt_error"])
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSnippetPrices(t *testing.T) {
	got := snippetPrices("Sony WH-1000XM5 ab 194,99 € | from $279.99 or 399 KM, € 1.299,00")
	var s []string
	for _, m := range got {
		s = append(s, fmt.Sprintf("%d %s", m.Minor, m.Cur))
	}
	if strings.Join(s, " ") != "27999 USD 129900 EUR 19499 EUR 39900 BAM" {
		t.Fatalf("%v", s)
	}
}

// Deliver to Bosnia and Herzegovina: the shipping page of A names it, D's
// data says Germany only (left out), E says it does not deliver there in a
// sentence (not confirmed, never "yes"). The best deal is the one that delivers.
func TestDeliveryCheck(t *testing.T) {
	hw := newHuntWorld(t)
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"ai_provider": "openai", "search_url": hw.search.URL, "deliver_to": "BA", "hide_no_ship": true, "home_currency": "EUR"})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"deliver_to": "ZZ"})
	id := num(a.expect(202, a.alice, "POST", "/api/v1/hunt", map[string]any{"query": "Acme X100 Headphones"}), "id")
	w := a.waitHunt(id)
	if w["hunt_status"] != "done" || !strings.Contains(w["hunt_summary"].(string), "1 deliver to Bosnia and Herzegovina") || !strings.Contains(w["hunt_summary"].(string), "1 left out") {
		t.Fatalf("summary %q", w["hunt_summary"])
	}
	offers := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)
	var got []string
	for _, o := range offers {
		m := o.(map[string]any)
		got = append(got, fmt.Sprintf("%v:%s", m["price"], m["ships"]))
	}
	if strings.Join(got, " ") != "10000:unknown 12000:yes" {
		t.Fatalf("offers %v", got)
	}
	if !strings.HasSuffix(w["url"].(string), "/a/x100") {
		t.Fatalf("best should be the shop that delivers: %v", w["url"])
	}
	// with "hide" off, D shows up last as "no"
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"hide_no_ship": false})
	a.expect(202, a.alice, "POST", fmt.Sprintf("/api/v1/wishes/%d/hunt", id), map[string]any{})
	a.waitHunt(id)
	offers = a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/wishes/%d/offers", id), nil)["offers"].([]any)
	last := offers[len(offers)-1].(map[string]any)
	if last["ships"] != "no" || !strings.Contains(last["ship_note"].(string), "DE") {
		t.Fatalf("last %v", last)
	}
	if c := a.ok(a.alice, "GET", "/api/v1/settings", nil); c["deliver_to_name"] != "Bosnia and Herzegovina" {
		t.Fatalf("settings %v", c["deliver_to_name"])
	}
}
