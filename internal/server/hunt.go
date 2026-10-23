package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/countries"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/currency"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/product"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/search"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// ---- deal hunting ----
//
// "Tell me what you want, find the best deal": a hunt plans web searches for
// the request (with AI: the exact product name, model number, the languages of
// the countries the person buys from), runs them through SearXNG, reads every
// shop page it found with the same reader as "Add from a link", and lets the
// AI judge which pages sell exactly that item (not a case, not the older
// model, not used when new was asked for). Every price the AI reports must
// appear in what was read; anything else is dropped. Offers are ranked in the
// home currency; the best one becomes the wish's link when it had none.
// Without AI the hunt still runs, matching by words only. With "keep hunting"
// it runs again every week and pushes when a clearly better deal turns up.

// huntSlots limits concurrent hunts on the box (each reads up to ~24 pages).
var huntSlots = make(chan struct{}, 2)

type huntPlan struct {
	Product   string   `json:"product"`
	MustMatch []string `json:"must_match"`
	Exclude   []string `json:"exclude"`
	Queries   []struct {
		Q        string `json:"q"`
		Language string `json:"language"`
	} `json:"queries"`
}

// candidate is one page the searches found, with what reading it gave.
type candidate struct {
	URL       string
	Domain    string
	Title     string // search result title
	Snippet   string
	Image     string
	Page      *product.Product
	PagePrice int64 // minor units of PageCur
	PageCur   string
	Snippets  []amount // prices written in the search result
	Ship      delivery // does it deliver to the person's country
}

// delivery is what we know about a shop delivering to the person's country.
// hard = from the shop's own data or its shipping page (the AI cannot
// overrule it); evidence is what the AI gets to read.
type delivery struct {
	verdict  string // yes | no | unknown
	note     string
	hard     bool
	cost     int64 // minor units of cur; -1 unknown
	cur      string
	evidence []string
}

type amount struct {
	Minor int64
	Cur   string
}

// skipDomains never sell anything.
var skipDomains = []string{"youtube.com", "youtu.be", "reddit.com", "wikipedia.org", "facebook.com", "instagram.com", "tiktok.com", "x.com", "twitter.com",
	"pinterest.", "quora.com", "rtings.com", "techradar.com", "theverge.com", "cnet.com", "chip.de", "computerbild.de", "trustpilot.", "github.com"}

// comparisonDomains list other shops' prices.
var comparisonDomains = []string{"idealo.", "geizhals.", "pricespy.", "prisjakt.", "pricerunner.", "billiger.de", "ceneo.pl", "kelkoo.", "heureka.", "arukereso.", "nabava.net", "jeftinije.hr", "pazaruvaj.", "skroutz.", "versus.com", "google.com/shopping", "shopping.google."}

func hasAny(s string, list []string) bool {
	for _, x := range list {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// aiFor is the person's AI for judging offers: Pocket AI through Core, their
// own OpenAI-compatible key, or nil (keywords only) with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) searchFor(st store.Settings) *search.Client {
	return search.New(firstNonEmpty(st.SearchURL, s.cfg.SearchURL))
}

// aiJSON asks for one JSON object and tolerates code fences and chatter around it.
func aiJSON(ctx context.Context, c *ai.Client, system, prompt string, v any) error {
	txt, err := c.Complete(ctx, system, prompt, true)
	if err != nil {
		return err
	}
	if i, j := strings.Index(txt, "{"), strings.LastIndex(txt, "}"); i >= 0 && j > i {
		txt = txt[i : j+1]
	}
	if err := json.Unmarshal([]byte(txt), v); err != nil {
		return fmt.Errorf("the AI's answer was not valid JSON: %w", err)
	}
	return nil
}

// huntBrief is what every prompt starts with: the request and the person's terms.
func huntBrief(w *store.Wish, st store.Settings) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The person wants: %s\n", firstNonEmpty(w.HuntQuery, w.Title))
	if w.HuntQuery != "" && w.Title != "" && !strings.EqualFold(w.Title, w.HuntQuery) {
		fmt.Fprintf(&b, "Saved as: %s\n", w.Title)
	}
	if w.Notes != "" {
		fmt.Fprintf(&b, "Their notes: %s\n", oneLine(w.Notes, 300))
	}
	if w.URL != "" {
		fmt.Fprintf(&b, "A page they already found: %s (%s, %s)\n", w.URL, w.Store, money(w.Price, w.Currency))
	}
	if w.HuntCondition == "any" {
		b.WriteString("Condition: new, or used / refurbished in good condition is fine.\n")
	} else {
		b.WriteString("Condition: new only.\n")
	}
	if w.HuntMax > 0 {
		fmt.Fprintf(&b, "Budget: up to %s.\n", money(w.HuntMax, w.Currency))
	}
	if st.DealMarkets != "" {
		fmt.Fprintf(&b, "They live in and buy from: %s.", st.DealMarkets)
		if st.DealAbroad {
			b.WriteString(" Online shops in other countries that deliver there also count.")
		} else {
			b.WriteString(" Only shops in those countries count.")
		}
		b.WriteString("\n")
	}
	if c := st.DeliverTo; c != "" {
		fmt.Fprintf(&b, "Deliver to: %s (%s). An offer only helps when the shop delivers there.\n", countries.Name(c), c)
	}
	fmt.Fprintf(&b, "Their currency: %s. Today: %s.\n", st.HomeCurrency, time.Now().Format("2006-01-02"))
	return b.String()
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

const planSystem = `You plan web searches that find where to buy a product at the best price. You answer with one JSON object and nothing else.`

const planPrompt = `Return JSON:
{"product": "the exact product name as shops list it: brand, model, model number, size/colour if given",
 "must_match": ["details an offer must have to be this product, e.g. the model number, capacity, size"],
 "exclude": ["words that mean a different item: accessories, cases, older or newer generations, refurbished when new is wanted"],
 "queries": [{"q": "search text", "language": "de"}]}
Write 3 to 6 queries: the product name with its model number, combined with words for buying it ("price", "buy", or the same words in the language of each country they buy from), one query per relevant language. One query may name a big online shop of those countries. language is a code like "en", "de", "bs", "hr", "sr", "fr", or "all".`

const judgeSystem = `You compare shop offers for one product for a careful shopper. Use only the data given: never invent shops, prices or facts. You answer with one JSON object and nothing else.`

const judgePrompt = `Return JSON:
{"offers": [{"i": 3, "match": "exact", "price": "299.00", "currency": "EUR", "shop": "Shop name", "in_stock": true, "ships": "yes", "ship_note": "at most 10 words", "note": "at most 12 words"}],
 "summary": "two or three short sentences for the person: the best deal and why, and what to watch out for"}
Rules:
- One entry per candidate that sells the wanted product or a close variant; leave out the rest.
- match: "exact" = this product as asked (the must-match details fit); "variant" = same product, other colour/size/bundle; "used" = used or refurbished; "comparison" = a price comparison site listing other shops; "accessory" or "unrelated" = not the product (leave those out).
- price and currency must be one of the prices shown for that candidate (page price first, else a price from its search result). If none is shown, leave the candidate out.
- in_stock: true, false, or null when unknown.
- ships: whether the shop delivers to the "Deliver to" country: "yes", "no", or "unknown". Decide only from the delivery and "about shipping" information shown for that candidate (a shop on that country's domain delivers there); without such information say "unknown". ship_note: the reason in a few words, e.g. "ships within the EU only", "page lists Bosnia and Herzegovina".
- note: what matters for buying there, e.g. "marketplace seller", "price from the search result, check the page", "ships from Germany", "older model".
- Leave out shops that cannot sell to the person under the rules above.`

// runHunt does one hunt for a wish (the caller already marked it running).
func (s *Server) runHunt(ctx context.Context, uid, wishID int64) {
	huntSlots <- struct{}{}
	defer func() { <-huntSlots }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	summary, aiUsed, best, err := s.hunt(ctx, uid, wishID)
	msg := ""
	if err != nil {
		msg = err.Error()
		s.log.Warn("deal hunt", "wish", wishID, "err", err)
	}
	_ = s.store.FinishHunt(context.Background(), wishID, summary, msg, aiUsed, best)
}

func (s *Server) hunt(ctx context.Context, uid, wishID int64) (summary, aiUsed string, bestHome int64, err error) {
	w, err := s.store.GetWish(ctx, uid, wishID)
	if err != nil {
		return "", "", 0, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	sc := s.searchFor(st)
	if !sc.Configured() {
		return "", "", 0, errors.New("no search engine: set the SearXNG address under Settings → Deal hunting")
	}
	cl, aiUsed, aiWhy := s.aiFor(ctx, uid, st)
	request := firstNonEmpty(w.HuntQuery, w.Title)

	// 1. plan
	s.store.HuntStep(ctx, w.ID, "Planning the search")
	plan := huntPlan{Product: request}
	if cl != nil {
		var p huntPlan
		if e := aiJSON(ctx, cl, planSystem, huntBrief(w, st)+"\n"+planPrompt, &p); e != nil {
			s.log.Warn("deal hunt: plan", "wish", w.ID, "err", e)
			aiWhy = "the AI did not answer (" + oneLine(e.Error(), 120) + ")"
			cl, aiUsed = nil, ""
		} else {
			plan = p
			if strings.TrimSpace(plan.Product) == "" {
				plan.Product = request
			}
		}
	}
	if len(plan.Queries) == 0 {
		for _, q := range []string{plan.Product, plan.Product + " price", plan.Product + " kaufen"} {
			plan.Queries = append(plan.Queries, struct {
				Q        string `json:"q"`
				Language string `json:"language"`
			}{q, "all"})
		}
	}
	if len(plan.Queries) > 6 {
		plan.Queries = plan.Queries[:6]
	}

	// 2. search
	s.store.HuntStep(ctx, w.ID, fmt.Sprintf("Searching the web (%d searches)", len(plan.Queries)))
	cands := s.huntSearch(ctx, sc, plan, w)
	if len(cands) == 0 {
		return "No shop pages came back from the search engine. Try different words.", aiUsed, 0, nil
	}

	// 3. read the pages
	s.store.HuntStep(ctx, w.ID, fmt.Sprintf("Reading %d shop pages", len(cands)))
	s.readCandidates(ctx, cands)
	if st.DeliverTo != "" {
		s.store.HuntStep(ctx, w.ID, "Checking delivery to "+countries.Name(st.DeliverTo))
		s.checkDelivery(ctx, cands, st.DeliverTo)
	}

	// 4. judge
	s.store.HuntStep(ctx, w.ID, "Comparing the offers")
	var offers []store.Offer
	if cl != nil {
		offers, summary, err = s.judge(ctx, cl, w, st, plan, cands)
		if err != nil {
			s.log.Warn("deal hunt: judge", "wish", w.ID, "err", err)
			aiWhy = "the AI did not answer (" + oneLine(err.Error(), 120) + ")"
			aiUsed, err = "", nil
		}
	}
	if aiUsed == "" {
		offers = keywordOffers(plan, request, cands)
	}

	// 5. rank in the home currency
	s.rankOffers(ctx, offers, w, st)
	hidden := 0
	if st.HideNoShip && st.DeliverTo != "" {
		kept := offers[:0]
		for _, o := range offers {
			if o.Ships == "no" {
				hidden++
				continue
			}
			kept = append(kept, o)
		}
		offers = kept
	}
	if len(offers) > 15 {
		offers = offers[:15]
	}
	if err := s.store.ReplaceOffers(ctx, uid, w.ID, offers); err != nil {
		return "", aiUsed, 0, err
	}
	saved, _ := s.store.ListOffers(ctx, uid, w.ID, false)
	// the best deal: cheapest eligible offer that surely delivers, else one not confirmed
	var bestOffer *store.Offer
	for _, want := range []string{"yes", "unknown"} {
		for i := range saved {
			o := &saved[i]
			if (o.Match == "exact" || o.Match == "unknown" || o.Match == "variant") && o.Availability != "out_of_stock" && !o.OverBudget && o.Ships == want {
				bestOffer = o
				break
			}
		}
		if bestOffer != nil {
			break
		}
	}
	if summary == "" {
		summary = keywordSummary(saved, bestOffer)
	}
	if st.DeliverTo != "" {
		summary += deliverySummary(saved, hidden, st.DeliverTo)
	}
	if aiUsed == "" && aiWhy != "" {
		summary += " Matched by keywords only — " + strings.TrimSuffix(aiWhy, ".") + "."
	}
	if bestOffer != nil {
		bestHome = bestOffer.PriceHome
		s.adoptBest(ctx, uid, w, plan, bestOffer)
		s.maybeBetterDeal(ctx, w, st, bestOffer)
	}
	return summary, aiUsed, bestHome, nil
}

var trackingParams = regexp.MustCompile(`^(utm_|gclid|fbclid|ref|tag|srsltid|_ga)`)

func cleanURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if trackingParams.MatchString(strings.ToLower(k)) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// huntSearch runs the planned queries and keeps up to 24 distinct pages, at
// most 3 per shop, the wish's own link first.
func (s *Server) huntSearch(ctx context.Context, sc *search.Client, plan huntPlan, w *store.Wish) []*candidate {
	var mu sync.Mutex
	results := make([][]search.Result, len(plan.Queries))
	var wg sync.WaitGroup
	for i, q := range plan.Queries {
		wg.Add(1)
		go func(i int, q, lang string) {
			defer wg.Done()
			res, err := sc.Search(ctx, q, lang)
			if err != nil {
				s.log.Debug("deal hunt: search", "q", q, "err", err)
				return
			}
			mu.Lock()
			results[i] = res
			mu.Unlock()
		}(i, q.Q, q.Language)
	}
	wg.Wait()
	seen := map[string]bool{}
	perDomain := map[string]int{}
	var out []*candidate
	add := func(raw, title, snippet, image string) {
		u := cleanURL(raw)
		if u == "" || seen[u] || len(out) >= 24 {
			return
		}
		d := product.StoreName(u)
		if d == "" || hasAny(d+"/", skipDomains) || perDomain[d] >= 3 {
			return
		}
		seen[u] = true
		perDomain[d]++
		c := &candidate{URL: u, Domain: d, Title: oneLine(title, 160), Snippet: oneLine(snippet, 260), Image: image}
		c.Snippets = snippetPrices(title + " " + snippet)
		out = append(out, c)
	}
	if w.URL != "" {
		add(w.URL, w.Title, "", "")
	}
	// round-robin over the queries so every search contributes
	for rank := 0; rank < 10; rank++ {
		for _, res := range results {
			if rank < len(res) {
				add(res[rank].URL, res[rank].Title, res[rank].Content, res[rank].Image)
			}
		}
	}
	return out
}

var (
	rePriceBefore = regexp.MustCompile(`(?i)(€|eur|km|bam|\$|usd|£|gbp|chf|rsd|din\.?)\s?(\d{1,3}(?:[.,\s]\d{3})*(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?)`)
	rePriceAfter  = regexp.MustCompile(`(?i)(\d{1,3}(?:[.,\s]\d{3})*(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?)\s?(€|eur|km|bam|\$|usd|£|gbp|chf|rsd|din\.?)(?:[^a-z]|$)`)
)

// snippetPrices finds the prices written in a search result ("ab 194,99 €").
func snippetPrices(text string) []amount {
	var out []amount
	seen := map[string]bool{}
	addOne := func(num, sym string) {
		cur := product.CurrencyFromSymbol(sym)
		if cur == "" {
			return
		}
		minor := product.ToMinor(strings.ReplaceAll(num, " ", ""), currency.Decimals(cur))
		if minor <= 0 {
			return
		}
		k := fmt.Sprint(minor, cur)
		if !seen[k] && len(out) < 4 {
			seen[k] = true
			out = append(out, amount{minor, cur})
		}
	}
	for _, m := range rePriceBefore.FindAllStringSubmatch(text, -1) {
		addOne(m[2], m[1])
	}
	for _, m := range rePriceAfter.FindAllStringSubmatch(text, -1) {
		addOne(m[1], m[2])
	}
	return out
}

// readCandidates reads every page (6 at a time) with the link reader.
func (s *Server) readCandidates(ctx context.Context, cands []*candidate) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, c := range cands {
		wg.Add(1)
		go func(c *candidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, 18*time.Second)
			defer cancel()
			p, err := s.fetch.Fetch(pctx, c.URL)
			if p == nil {
				s.log.Debug("deal hunt: page", "url", c.URL, "err", err)
				return
			}
			c.Page = p
			if p.Price != "" {
				cur := p.Currency
				if !currency.Valid(cur) {
					cur = ""
				}
				if cur != "" {
					c.PageCur = cur
					c.PagePrice = product.ToMinor(p.Price, currency.Decimals(cur))
				}
			}
		}(c)
	}
	wg.Wait()
}

func (c *candidate) line(i int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] shop: %s | url: %s", i, c.Domain, oneLine(c.URL, 140))
	if hasAny(c.Domain, comparisonDomains) {
		b.WriteString(" | (price comparison site)")
	}
	if c.Page != nil {
		if c.Page.Store != "" && c.Page.Store != c.Domain {
			fmt.Fprintf(&b, " | shop name: %s", c.Page.Store)
		}
		fmt.Fprintf(&b, " | page title: %s", oneLine(c.Page.Title, 140))
		if c.PagePrice > 0 {
			fmt.Fprintf(&b, " | page price: %s %s", majorText(c.PagePrice, c.PageCur), c.PageCur)
		}
		if c.Page.Availability != "" {
			fmt.Fprintf(&b, " | stock: %s", c.Page.Availability)
		}
	} else {
		b.WriteString(" | page could not be read")
	}
	if c.Ship.verdict != "" && c.Ship.verdict != "unknown" {
		fmt.Fprintf(&b, " | delivery: %s (%s)", c.Ship.verdict, c.Ship.note)
	}
	for _, e := range c.Ship.evidence {
		fmt.Fprintf(&b, " | about shipping: %q", oneLine(e, 300))
	}
	fmt.Fprintf(&b, " | search result: %q", oneLine(c.Title+" — "+c.Snippet, 300))
	if len(c.Snippets) > 0 {
		var ps []string
		for _, m := range c.Snippets {
			ps = append(ps, majorText(m.Minor, m.Cur)+" "+m.Cur)
		}
		fmt.Fprintf(&b, " | prices in the search result: %s", strings.Join(ps, ", "))
	}
	return b.String()
}

func majorText(minor int64, cur string) string {
	d := currency.Decimals(cur)
	return fmt.Sprintf("%.*f", d, major(minor, cur))
}

// judge asks the AI which candidates sell the product, and keeps only what
// checks out against the data (a price the AI made up is dropped).
func (s *Server) judge(ctx context.Context, cl *ai.Client, w *store.Wish, st store.Settings, plan huntPlan, cands []*candidate) ([]store.Offer, string, error) {
	var b strings.Builder
	b.WriteString(huntBrief(w, st))
	fmt.Fprintf(&b, "Product: %s\n", plan.Product)
	if len(plan.MustMatch) > 0 {
		fmt.Fprintf(&b, "Must match: %s\n", strings.Join(plan.MustMatch, "; "))
	}
	if len(plan.Exclude) > 0 {
		fmt.Fprintf(&b, "Not the product: %s\n", strings.Join(plan.Exclude, "; "))
	}
	b.WriteString("\nCandidates:\n")
	for i, c := range cands {
		b.WriteString(c.line(i))
		b.WriteString("\n")
	}
	b.WriteString("\n" + judgePrompt)
	var out struct {
		Offers []struct {
			I        int    `json:"i"`
			Match    string `json:"match"`
			Price    any    `json:"price"`
			Currency string `json:"currency"`
			Shop     string `json:"shop"`
			InStock  *bool  `json:"in_stock"`
			Ships    string `json:"ships"`
			ShipNote string `json:"ship_note"`
			Note     string `json:"note"`
		} `json:"offers"`
		Summary string `json:"summary"`
	}
	if err := aiJSON(ctx, cl, judgeSystem, b.String(), &out); err != nil {
		return nil, "", err
	}
	var offers []store.Offer
	used := map[int]bool{}
	for _, o := range out.Offers {
		if o.I < 0 || o.I >= len(cands) || used[o.I] {
			continue
		}
		match := strings.ToLower(strings.TrimSpace(o.Match))
		switch match {
		case "exact", "variant", "used", "comparison":
		default:
			continue
		}
		if match == "used" && w.HuntCondition != "any" {
			continue
		}
		c := cands[o.I]
		cur := strings.ToUpper(strings.TrimSpace(o.Currency))
		minor := product.ToMinor(fmt.Sprint(o.Price), currency.Decimals(cur))
		src, ok := verifyPrice(c, minor, cur)
		if !ok {
			continue // not a price that was on the page or in the search result
		}
		used[o.I] = true
		of := offerFrom(c, minor, cur, src)
		of.Match = match
		if o.Shop != "" {
			of.Store = oneLine(o.Shop, 60)
		}
		if o.InStock != nil && of.Availability == "" {
			of.Availability = map[bool]string{true: "in_stock", false: "out_of_stock"}[*o.InStock]
		}
		of.Note = oneLine(o.Note, 140)
		// the AI may settle delivery only from shipping information it was shown
		if !c.Ship.hard && len(c.Ship.evidence) > 0 {
			switch v := strings.ToLower(strings.TrimSpace(o.Ships)); v {
			case "yes", "no":
				of.Ships, of.ShipNote = v, oneLine(firstNonEmpty(o.ShipNote, "from the shop's shipping information"), 120)
			}
		}
		offers = append(offers, of)
	}
	return offers, oneLine(out.Summary, 600), nil
}

// verifyPrice accepts a price only when it is the page's price or one written
// in the search result (1 % tolerance for rounding).
func verifyPrice(c *candidate, minor int64, cur string) (string, bool) {
	if minor <= 0 || !currency.Valid(cur) {
		return "", false
	}
	close := func(a, b int64) bool { return math.Abs(float64(a-b)) <= math.Max(1, float64(b)*0.01) }
	if c.PagePrice > 0 && c.PageCur == cur && close(minor, c.PagePrice) {
		return "page", true
	}
	for _, m := range c.Snippets {
		if m.Cur == cur && close(minor, m.Minor) {
			return "snippet", true
		}
	}
	return "", false
}

func offerFrom(c *candidate, minor int64, cur, src string) store.Offer {
	o := store.Offer{URL: c.URL, Store: c.Domain, Title: c.Title, Image: c.Image, Price: minor, Currency: cur, Source: src, Match: "unknown"}
	if c.Page != nil {
		o.Title = firstNonEmpty(c.Page.Title, c.Title)
		o.Image = firstNonEmpty(c.Page.Image, c.Image)
		o.Availability = c.Page.Availability
		if c.Page.Store != "" {
			o.Store = c.Page.Store
		}
	}
	o.Title = oneLine(o.Title, 200)
	o.Ships, o.ShipNote, o.ShipCost, o.ShipCurrency = firstNonEmpty(c.Ship.verdict, "unknown"), c.Ship.note, -1, ""
	if c.Ship.cost >= 0 && c.Ship.cur != "" {
		o.ShipCost, o.ShipCurrency = c.Ship.cost, c.Ship.cur
	}
	if hasAny(c.Domain, comparisonDomains) {
		o.Match = "comparison"
		o.Ships, o.ShipNote = "unknown", "a price comparison site: check the shop you pick"
	}
	return o
}

func words(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127) }) {
		if len(f) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

// keywordOffers is the hunt without AI: pages whose title holds most of the
// request's words, with the price read from the page (or the search result).
func keywordOffers(plan huntPlan, request string, cands []*candidate) []store.Offer {
	want := words(firstNonEmpty(plan.Product, request))
	var out []store.Offer
	for _, c := range cands {
		title := c.Title
		if c.Page != nil {
			title += " " + c.Page.Title
		}
		have := map[string]bool{}
		for _, x := range words(title) {
			have[x] = true
		}
		hit := 0
		for _, x := range want {
			if have[x] {
				hit++
			}
		}
		if len(want) == 0 || float64(hit) < math.Ceil(float64(len(want))*0.7) {
			continue
		}
		switch {
		case c.PagePrice > 0:
			out = append(out, offerFrom(c, c.PagePrice, c.PageCur, "page"))
		case len(c.Snippets) > 0:
			o := offerFrom(c, c.Snippets[0].Minor, c.Snippets[0].Cur, "snippet")
			o.Note = "price from the search result, check the page"
			out = append(out, o)
		}
	}
	return out
}

func keywordSummary(offers []store.Offer, best *store.Offer) string {
	if len(offers) == 0 {
		return "No shop page with a price matched. Shops that build their pages in the browser or block servers cannot be read; try other words, or add the shop's link yourself."
	}
	if best == nil {
		return fmt.Sprintf("%d offers found, none in stock within the budget.", len(offers))
	}
	return fmt.Sprintf("Best: %s at %s (%d offers found).", money(best.Price, best.Currency), best.Store, len(offers))
}

// rankOffers converts to the home currency and sorts: exact matches first,
// in stock before out of stock, then cheapest.
func (s *Server) rankOffers(ctx context.Context, offers []store.Offer, w *store.Wish, st store.Settings) {
	home := st.HomeCurrency
	maxHome := int64(0)
	if w.HuntMax > 0 {
		maxHome = s.convert(ctx, w.HuntMax, firstNonEmpty(w.Currency, home), home)
	}
	for i := range offers {
		o := &offers[i]
		o.PriceHome = s.convert(ctx, o.Price, o.Currency, home)
		if o.PriceHome > 0 && o.ShipCost > 0 {
			// compare what it costs delivered
			if c := s.convert(ctx, o.ShipCost, o.ShipCurrency, home); c > 0 {
				o.PriceHome += c
			}
		}
		o.OverBudget = maxHome > 0 && o.PriceHome > maxHome
	}
	order := map[string]int{"exact": 0, "unknown": 1, "variant": 2, "comparison": 3, "used": 4}
	sort.SliceStable(offers, func(i, j int) bool {
		a, b := offers[i], offers[j]
		if (a.Ships == "no") != (b.Ships == "no") {
			return b.Ships == "no"
		}
		if order[a.Match] != order[b.Match] {
			return order[a.Match] < order[b.Match]
		}
		if (a.Availability == "out_of_stock") != (b.Availability == "out_of_stock") {
			return b.Availability == "out_of_stock"
		}
		if a.OverBudget != b.OverBudget {
			return b.OverBudget
		}
		if (a.PriceHome == 0) != (b.PriceHome == 0) {
			return b.PriceHome == 0
		}
		return a.PriceHome < b.PriceHome
	})
}

// convert turns an amount into another currency at today's rate (cached a
// day); 0 when no rate is known.
func (s *Server) convert(ctx context.Context, minor int64, from, to string) int64 {
	if from == to || from == "" {
		return minor
	}
	day := time.Now().Format("2006-01-02")
	key := "fx:" + from + ":" + to + ":" + day
	var rate float64
	if v, _, ok := s.store.CacheGet(ctx, key); ok {
		fmt.Sscan(v, &rate)
	}
	if rate == 0 {
		r, _, err := s.fx.Rate(ctx, from, to, day)
		if err != nil || r <= 0 {
			return 0
		}
		rate = r
		_ = s.store.CachePut(ctx, key, fmt.Sprint(rate))
	}
	f := major(minor, from) * rate
	return int64(math.Round(f * math.Pow10(currency.Decimals(to))))
}

// adoptBest gives a wish made by "Find the best deal" (no link yet) the best
// offer's link, price and picture, and starts watching its price.
func (s *Server) adoptBest(ctx context.Context, uid int64, w *store.Wish, plan huntPlan, o *store.Offer) {
	if w.URL != "" || o.Source != "page" || o.Match == "comparison" {
		return
	}
	cur, err := s.store.GetWish(ctx, uid, w.ID)
	if err != nil || cur.URL != "" {
		return
	}
	cur.URL, cur.Price, cur.Currency, cur.Store, cur.TrackPrice = o.URL, o.Price, o.Currency, o.Store, true
	if strings.EqualFold(strings.TrimSpace(cur.Title), strings.TrimSpace(cur.HuntQuery)) && plan.Product != "" {
		cur.Title = oneLine(plan.Product, 200)
	}
	if cur.ImageURL == "" {
		cur.ImageURL = o.Image
	}
	if err := s.store.UpdateWish(ctx, uid, cur); err != nil {
		return
	}
	if cur.PhotoPath == "" && o.Image != "" {
		s.fetchPhoto(ctx, uid, cur.ID, o.Image)
	}
}

// maybeBetterDeal pushes when a repeat hunt finds a clearly cheaper offer
// than the last one (or than what the wish costs now).
func (s *Server) maybeBetterDeal(ctx context.Context, w *store.Wish, st store.Settings, best *store.Offer) {
	if best.PriceHome <= 0 || w.HuntAt == "" {
		return // the first hunt is not news
	}
	ref := w.HuntBest
	if wishHome := s.convert(ctx, w.Price, w.Currency, st.HomeCurrency); wishHome > 0 && (ref == 0 || wishHome < ref) {
		ref = wishHome
	}
	pct := int64(max(st.DropPercent, 1))
	if ref <= 0 || (ref-best.PriceHome)*100 < pct*ref {
		return
	}
	if s.core.Configured() {
		hook := s.wishEvent(ctx, "wishlist.better_deal", w, map[string]any{"offer_url": best.URL, "offer_store": best.Store, "offer_price": best.Price, "offer_currency": best.Currency, "was_home": ref, "now_home": best.PriceHome})
		if hook != nil {
			if tx, err := s.store.DB.BeginTx(ctx, nil); err == nil {
				if hook(tx) == nil {
					_ = tx.Commit()
				} else {
					_ = tx.Rollback()
				}
			}
		}
	}
	if !st.NotifyDrops || st.NotifyURL == "" {
		return
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = fmt.Sprintf("%s/wishes/%d", s.cfg.BaseURL, w.ID)
	}
	body := fmt.Sprintf("%s: %s at %s (was %s).", w.Title, money(best.Price, best.Currency), best.Store, money(ref, st.HomeCurrency))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Better deal found: " + w.Title, Body: body, Priority: "success", Tags: []string{"gift", "mag"}, Link: link}); err != nil {
		s.log.Warn("better deal push", "wish", w.ID, "err", err)
	}
}

// autoHunts runs the "keep hunting" wishes whose interval has passed.
func (s *Server) autoHunts(ctx context.Context) {
	due, err := s.store.DueHunts(ctx, 20)
	if err != nil {
		return
	}
	n := 0
	for _, w := range due {
		if n >= s.cfg.HuntsPerRun || ctx.Err() != nil {
			return
		}
		st, _ := s.store.GetSettings(ctx, w.UserID)
		every := time.Duration(max(st.HuntEveryDays, 1)) * 24 * time.Hour
		if w.HuntAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, w.HuntAt); err == nil && time.Since(t) < every {
				continue
			}
		}
		if ok, _ := s.store.StartHunt(ctx, w.ID); ok {
			s.runHunt(ctx, w.UserID, w.ID)
			n++
		}
	}
}

// ---- HTTP ----

type huntInput struct {
	Query     *string `json:"query"`
	MaxPrice  *int64  `json:"max_price"`
	MaxText   *string `json:"max_text"`
	Condition *string `json:"condition"`
	Auto      *bool   `json:"auto"`
	ListID    int64   `json:"list_id"`
	Currency  string  `json:"currency"`
}

// handleNewHunt makes a wish from a description and starts its hunt.
func (s *Server) handleNewHunt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in huntInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	q := ""
	if in.Query != nil {
		q = strings.TrimSpace(*in.Query)
	}
	if q == "" {
		writeErr(w, http.StatusBadRequest, "say what you are looking for")
		return
	}
	if len(q) > 500 {
		writeErr(w, http.StatusBadRequest, "keep it under 500 characters")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cur := strings.ToUpper(firstNonEmpty(in.Currency, st.HomeCurrency))
	if !currency.Valid(cur) {
		cur = st.HomeCurrency
	}
	listID := in.ListID
	if listID == 0 {
		id, err := s.defaultList(ctx, uid)
		if err != nil {
			s.fail(w, err, "hunt")
			return
		}
		listID = id
	}
	title := q
	if len(title) > 120 {
		title = strings.TrimSpace(title[:120]) + "…"
	}
	wi := &store.Wish{ListID: listID, Title: title, Currency: cur, Priority: 2, Quantity: 1, Source: linkApp(ctx)}
	if err := s.store.CreateWish(ctx, uid, wi, s.wishEvent(ctx, "wishlist.wish_added", wi, nil)); err != nil {
		s.fail(w, err, "hunt")
		return
	}
	s.startHunt(w, r, wi.ID, in, q)
}

// handleHunt (re)starts the hunt of an existing wish, optionally with new terms.
func (s *Server) handleHunt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "hunt")
		return
	}
	var in huntInput
	_ = decode(r, &in)
	s.startHunt(w, r, wi.ID, in, firstNonEmpty(wi.HuntQuery, wi.Title))
}

func (s *Server) startHunt(w http.ResponseWriter, r *http.Request, wishID int64, in huntInput, query string) {
	ctx := r.Context()
	uid := actorID(ctx)
	wi, err := s.store.GetWish(ctx, uid, wishID)
	if err != nil {
		s.fail(w, err, "hunt")
		return
	}
	if in.Query != nil && strings.TrimSpace(*in.Query) != "" {
		query = strings.TrimSpace(*in.Query)
	}
	maxPrice, cond, auto := wi.HuntMax, firstNonEmpty(wi.HuntCondition, "new"), wi.HuntAuto
	if in.MaxPrice != nil {
		maxPrice = max(0, *in.MaxPrice)
	} else if in.MaxText != nil {
		maxPrice = product.ToMinor(*in.MaxText, currency.Decimals(wi.Currency))
	}
	if in.Condition != nil && (*in.Condition == "new" || *in.Condition == "any") {
		cond = *in.Condition
	}
	if in.Auto != nil {
		auto = *in.Auto
	}
	if err := s.store.SetHuntRequest(ctx, uid, wi.ID, query, maxPrice, cond, auto); err != nil {
		s.fail(w, err, "hunt")
		return
	}
	ok, err := s.store.StartHunt(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "hunt")
		return
	}
	if ok {
		go s.runHunt(context.Background(), uid, wi.ID)
	}
	got, err := s.ownerWish(ctx, wi.ID)
	if err != nil {
		s.fail(w, err, "hunt")
		return
	}
	writeJSON(w, http.StatusAccepted, got)
}

func (s *Server) handleOffers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wi, err := s.store.GetWish(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "offers")
		return
	}
	offers, err := s.store.ListOffers(ctx, actorID(ctx), wi.ID, queryBool(r, "dismissed"))
	if err != nil {
		s.fail(w, err, "offers")
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	writeJSON(w, http.StatusOK, map[string]any{"offers": offers, "home_currency": st.HomeCurrency})
}

// handleUseOffer makes an offer the wish's link and price, and watches it.
func (s *Server) handleUseOffer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	wi, err := s.store.GetWish(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "offer")
		return
	}
	o, err := s.store.GetOffer(ctx, uid, wi.ID, idParam(r, "oid"))
	if err != nil {
		s.fail(w, err, "offer")
		return
	}
	wi.URL, wi.Price, wi.Currency, wi.Store, wi.TrackPrice = o.URL, o.Price, o.Currency, o.Store, o.Match != "comparison"
	newImage := wi.PhotoPath == "" && o.Image != ""
	if newImage {
		wi.ImageURL = o.Image
	}
	if err := s.store.UpdateWish(ctx, uid, wi); err != nil {
		s.fail(w, err, "offer")
		return
	}
	if newImage {
		s.fetchPhoto(ctx, uid, wi.ID, o.Image)
	}
	got, _ := s.ownerWish(ctx, wi.ID)
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDismissOffer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Dismissed *bool `json:"dismissed"`
	}
	_ = decode(r, &in)
	d := true
	if in.Dismissed != nil {
		d = *in.Dismissed
	}
	if err := s.store.DismissOffer(ctx, actorID(ctx), idParam(r, "id"), idParam(r, "oid"), d); err != nil {
		s.fail(w, err, "offer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAIStatus tells Settings and the hunt dialog which AI would judge.
func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why, "search": s.searchFor(st).Configured()})
}

// handleAIModels lists the person's Pocket AI models (Settings).
func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := userFrom(ctx).Email
	c, status := s.core.SelectAI(ctx, email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

// ---- delivery to the person's country ----

// checkDelivery decides, without AI, what it can: a shop on the country's
// domain delivers there; the shop's structured shipping data names countries;
// the shop's own shipping page names the country (or lists many others).
// What stays open goes to the AI as evidence.
func (s *Server) checkDelivery(ctx context.Context, cands []*candidate, code string) {
	name := countries.Name(code)
	tld := countries.TLD(code)
	var open []*candidate
	for _, c := range cands {
		c.Ship = delivery{verdict: "unknown", cost: -1}
		host := strings.ToLower(c.Domain)
		switch {
		case strings.HasSuffix(host, tld):
			c.Ship.verdict, c.Ship.hard, c.Ship.note = "yes", true, "shop in "+name
		case c.Page != nil && len(c.Page.ShipsTo) > 0:
			c.Ship.hard = true
			if contains(c.Page.ShipsTo, code) {
				c.Ship.verdict, c.Ship.note = "yes", "the shop's data lists "+name
			} else {
				c.Ship.verdict, c.Ship.note = "no", "delivers only to "+strings.Join(c.Page.ShipsTo[:min(6, len(c.Page.ShipsTo))], ", ")
			}
		}
		if c.Page != nil {
			for _, r := range c.Page.ShipRates {
				if (r.Country == code || r.Country == "") && currency.Valid(r.Currency) {
					c.Ship.cost, c.Ship.cur = product.ToMinor(r.Price, currency.Decimals(r.Currency)), r.Currency
					if r.Price == "0" || r.Price == "0.00" {
						c.Ship.cost = 0
					}
					if r.Country == code {
						break
					}
				}
			}
			for _, t := range c.Page.ShipText {
				if found, around := countries.Find(t, code); found != "" && !c.Ship.hard {
					if negated(around) {
						c.Ship.evidence = append(c.Ship.evidence, around) // "no shipping to …": the AI decides
						continue
					}
					c.Ship.verdict, c.Ship.note = "yes", "the page mentions "+found
					c.Ship.evidence = append(c.Ship.evidence, around)
				}
			}
			if len(c.Ship.evidence) == 0 && len(c.Page.ShipText) > 0 {
				c.Ship.evidence = append(c.Ship.evidence, strings.Join(c.Page.ShipText[:min(3, len(c.Page.ShipText))], " / "))
			}
		}
		if !c.Ship.hard && c.Page != nil && len(c.Page.ShipLinks) > 0 && (c.PagePrice > 0 || len(c.Snippets) > 0) {
			open = append(open, c)
		}
	}
	// the shop's shipping page: one per shop, at most 10 shops, cached 30 days
	byShop := map[string][]*candidate{}
	var shops []string
	for _, c := range open {
		if _, ok := byShop[c.Domain]; !ok {
			shops = append(shops, c.Domain)
		}
		byShop[c.Domain] = append(byShop[c.Domain], c)
	}
	if len(shops) > 10 {
		shops = shops[:10]
	}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, shop := range shops {
		wg.Add(1)
		go func(shop string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d := s.shopDelivery(ctx, shop, byShop[shop][0].Page.ShipLinks, code)
			for _, c := range byShop[shop] {
				if d.verdict != "unknown" {
					c.Ship.verdict, c.Ship.note, c.Ship.hard = d.verdict, d.note, true
				}
				c.Ship.evidence = append(c.Ship.evidence, d.evidence...)
			}
		}(shop)
	}
	wg.Wait()
}

func contains(list []string, x string) bool {
	for _, v := range list {
		if v == x {
			return true
		}
	}
	return false
}

type shopShipping struct {
	Verdict  string   `json:"verdict"`
	Note     string   `json:"note"`
	Evidence []string `json:"evidence"`
}

// shopDelivery reads a shop's shipping page for the country (cached 30 days per shop and country).
func (s *Server) shopDelivery(ctx context.Context, shop string, links []string, code string) delivery {
	key := "ship:" + shop + ":" + code
	if v, at, ok := s.store.CacheGet(ctx, key); ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 30*24*time.Hour {
			var c shopShipping
			if json.Unmarshal([]byte(v), &c) == nil {
				return delivery{verdict: c.Verdict, note: c.Note, evidence: c.Evidence, cost: -1}
			}
		}
	}
	name := countries.Name(code)
	out := shopShipping{Verdict: "unknown"}
	for _, link := range links[:min(2, len(links))] {
		pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		text, err := s.fetch.FetchText(pctx, link)
		cancel()
		if err != nil || text == "" {
			continue
		}
		if found, around := countries.Find(text, code); found != "" {
			if negated(around) {
				out = shopShipping{Verdict: "unknown", Evidence: []string{"shipping page: " + around}}
				break
			}
			out = shopShipping{Verdict: "yes", Note: "the shop's shipping page names " + found, Evidence: []string{around}}
			break
		}
		if n := countries.CountIn(text, code); n >= 8 {
			out = shopShipping{Verdict: "no", Note: fmt.Sprintf("the shipping page lists %d countries, not %s", n, name)}
			break
		}
		// neither: keep what it says about shipping for the AI
		var said []string
		for _, sent := range strings.Split(text, ". ") {
			if mentionsShip(sent) {
				said = append(said, oneLine(sent, 200))
				if len(said) == 3 {
					break
				}
			}
		}
		if len(said) > 0 {
			out.Evidence = []string{"shipping page: " + strings.Join(said, " / ")}
		}
	}
	if b, err := json.Marshal(out); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return delivery{verdict: out.Verdict, note: out.Note, evidence: out.Evidence, cost: -1}
}

var shipHint = []string{"ship", "deliver", "versand", "liefer", "livraison", "spedizion", "envío", "envio", "dostav", "isporu", "wysyłk", "verzend", "countries", "länder", "pays", "zemlj"}

func mentionsShip(s string) bool {
	low := strings.ToLower(s)
	for _, w := range shipHint {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// deliverySummary adds one line on delivery to the hunt's summary.
func deliverySummary(offers []store.Offer, hidden int, code string) string {
	yes, unknown := 0, 0
	for _, o := range offers {
		switch o.Ships {
		case "yes":
			yes++
		case "unknown":
			unknown++
		}
	}
	name := countries.Name(code)
	parts := []string{}
	if yes > 0 {
		parts = append(parts, fmt.Sprintf("%d deliver to %s", yes, name))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d not confirmed", unknown))
	}
	if hidden > 0 {
		parts = append(parts, fmt.Sprintf("%d left out because they do not deliver there", hidden))
	}
	if len(parts) == 0 {
		return ""
	}
	return " Delivery: " + strings.Join(parts, ", ") + "."
}

// negation words that turn "we ship to X" into "we do not ship to X".
var reNegation = regexp.MustCompile(`(?i)\b(not|no|don't|doesn't|cannot|can't|unfortunately|except|excluding|nicht|kein|keine|keinen|leider|ausgenommen|außer|ne|nije|nisu|nažalost|osim|pas|sauf|non|tranne|eccetto|niestety|oprócz|nie|niet|geen|behalve)\b`)

func negated(around string) bool { return reNegation.MatchString(around) }
