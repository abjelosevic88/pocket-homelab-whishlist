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
// linkRoutes stays closed to it (403). A link sees exactly the person's own
// lists (never lists shared with them, never who reserved what); a wish it
// adds carries the app's slug as source, and it may change or delete only
// those.

var linkOffers = []core.Offer{
	{Scope: "wishes:read", Name: "See your wishlists", Description: "Read your lists and wishes (titles, prices, price drops, what you bought) and upcoming gift occasions, for your journal, calendar and dashboards. Never who reserved a gift for you."},
	{Scope: "wishes:write", Name: "Add wishes", Description: "Add things to your wishlists (e.g. from a shop page or a chat), and change or delete only the wishes that app added itself."},
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
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
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
