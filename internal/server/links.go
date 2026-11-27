package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Wishlist as
// the target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed in
// linkRoutes, portable or capabilities stays closed to it (403). A link sees
// exactly the person's own lists (never lists shared with them, never who
// reserved what); a wish it adds carries the app's slug as source, and it may
// change or delete only those.

var linkOffers = []core.Offer{
	{Scope: "wishes:read", Name: "See your wishlists", Description: "Read your lists and wishes (titles, prices, price drops, what you bought) and upcoming gift occasions, for your journal, calendar and dashboards. Never who reserved a gift for you."},
	{Scope: "wishes:write", Name: "Add wishes", Description: "Add things to your wishlists (e.g. from a shop page or a chat), mark a wish bought when you order it (Pocket Parcels), and change or delete only the wishes that app added itself."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "wishlist.json", Kind: core.KindPocket, Title: "Your wishlists and gift lists with every wish, what you bought and the price history (never who reserved what)"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: 32 << 20, Title: "Adds the lists and wishes from another box; lists with the same name are merged and wishes you already have are skipped"},
	},
}

// capabilities are what Pocket Search, Pocket Day and Pocket Hub may ask
// Wishlist (docs/capabilities.md in pocket-homelab-core); Register adds the
// pocket:* offers. The routes are in capabilities.go.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your wishes and gift ideas, by name, shop and notes", Kinds: []string{"wish"}, Bangs: []string{"wish", "wishlist"}},
	Day: &core.DayFeed{Label: "Wishlist", One: "wish", Many: "wishes", Unit: "count",
		What: "The wishes you added or bought that day and the prices that changed"},
	Widget: &core.Widget{Title: "Wishlist", What: "Wishes that reached your target price, then the ones you added last"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "wish", Label: "Wish",
		What: "A new wish on your wishlist; a shop link is read for the name, picture and price", Fields: []string{"title"}}}},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(lists|wishes|dashboard|occasions|tags)$`), "wishes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/(lists|wishes)/\d+$`), "wishes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/wishes/\d+/(photo|prices)$`), "wishes:read"},
	{"GET", regexp.MustCompile(`^/api/v1/day/\d{4}-\d{2}-\d{2}$`), "wishes:read"},
	{"POST", regexp.MustCompile(`^/api/v1/wishes$`), "wishes:write"},
	{"POST", regexp.MustCompile(`^/api/v1/preview$`), "wishes:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/wishes/\d+$`), "wishes:write"},
	// Pocket Parcels marks the wish a parcel is as bought when it is ordered;
	// any of the person's own wishes (bought once: a second time is a 409)
	{"POST", regexp.MustCompile(`^/api/v1/wishes/\d+/bought$`), "wishes:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token,
// "" otherwise.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
