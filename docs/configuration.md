# Configuration

Everything is read from environment variables; every value has a default, so a bare `docker run -v ./data:/data` works. Integration values seed the owner's settings on first start and can be changed later under **Settings → Connected apps** (the UI values win afterwards).

| Variable | Default | What it does |
|---|---|---|
| `PORT` / `HOST` | `8080` / `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL: share links, notification links, the OIDC callback |
| `DATA_DIR` | `./data` (`/data` in the image) | SQLite database, wish pictures and backups |
| `DB_PATH` / `FILES_DIR` | `$DATA_DIR/pocket-wishlist.db` / `$DATA_DIR/files` | Override locations |
| `TZ` | – | Container timezone (mount `/usr/share/zoneinfo` on Alpine-based hosts if dates look off) |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug`, `warn`, `error`; `json` |
| `TRUST_PROXY` | `true` | Honour `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` (row counts and price checks only, never content) |
| `MAX_UPLOAD_BYTES` | `20971520` | Picture upload limit |
| `AUTH_MODE` | `local` | `local` (built-in accounts), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (single user, LAN only) |
| `AUTH_PROXY_HEADER` / `AUTH_PROXY_EMAIL_HEADER` | `Remote-User` / `Remote-Email` | Headers for proxy mode |
| `SESSION_TTL` | `720h` | Cookie lifetime |
| `SECURE_COOKIES` | `false` | Set `true` behind HTTPS (on plain http the browser drops the cookie and login bounces) |
| `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | – | Sign in with an OpenID Connect provider (PocketID, Authelia, Authentik, Keycloak…). Needs `BASE_URL`; the callback is `BASE_URL/api/v1/auth/oidc/callback`. On a box with Pocket Core none of these are needed — the app receives its client when it registers. |
| `OIDC_NAME` | `single sign-on` | Button label: “Sign in with …” (“Pocket Core” when the client came from Core) |
| `OIDC_AUTO_CREATE` | `true` | Give an unknown (verified) email a member account instead of refusing it |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` / `ADMIN_NAME` | – | Create the first admin on an empty database (otherwise the UI asks) |
| `DEFAULT_CURRENCY` | `EUR` | The currency a new person starts with |
| `INVENTORY_URL` / `INVENTORY_TOKEN` | – | Pocket Inventory and an API token. These and the `MONEY_*` / `SPLIT_*` / `CONTACTS_*` / `NOTIFY_*` values are the owner's (first admin's) own direct connections, each copied once (clearing one in Settings keeps it cleared); everyone else sets their own, or uses Pocket Core app links with nothing to paste |
| `MONEY_URL` / `MONEY_TOKEN` | – | Pocket Money: “Bought it” books the expense |
| `SPLIT_URL` / `SPLIT_TOKEN` / `SPLIT_PUBLIC_URL` / `SPLIT_GROUP_ID` | – | Pocket Split: shared purchases and group gifts (public URL = what the browser opens) |
| `CONTACTS_URL` / `CONTACTS_TOKEN` | – | Pocket Contacts: gift recipients and their birthdays |
| `NOTIFY_URL` / `NOTIFY_FORMAT` | – / guessed | Apprise `…/notify/<key>`, ntfy topic URL or JSON webhook; `apprise`, `ntfy`, `json` |
| `SEARXNG_URL` | – | The SearXNG that deal hunts search with (JSON format on: `formats: [html, json]`). Each person can set their own under Settings → Deal hunting |
| `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` | – / `gpt-4o-mini` / OpenAI | The owner's own OpenAI-compatible AI for deal hunts (copied once). With Pocket Core, Pocket AI is used instead when the `inference:write` link is on |
| `DELIVER_TO` | – | The country people start out with under Settings → Deal hunting → Deliver to (ISO code, e.g. `BA`); hunts check every shop delivers there |
| `DEAL_MARKETS` | – | Where people start out shopping (“Bosnia and Herzegovina, Germany”): the AI searches in those languages |
| `HUNTS_PER_RUN` | `3` | At most this many “keep hunting” hunts per 15-minute sweep |
| `FX_PROVIDER` | `auto` | Exchange rates for ranking offers in the home currency: Frankfurter, then open.er-api.com (`none` = off) |
| `PRICE_CHECKS_PER_RUN` | `40` | At most this many shop pages are read per 15-minute sweep (one at a time, 3 s apart per shop) |
| `FETCH_PRIVATE` | `false` | Let link previews and price checks reach private / loopback / tailnet addresses. Off, a member cannot make the server probe your LAN |
| `USER_AGENT` | a desktop Chrome string + `PocketWishlist/1.0` | Sent to shops (many refuse obvious bots) |
| `CORE_URL` / `CORE_REGISTRATION_KEY` | – | [Pocket Core](https://github.com/abjelosevic88/pocket-homelab-core): register on start (the key is needed once; the app token lives in `$DATA_DIR/core.token`), sign in with Pocket Core, accept the tokens Core hands to paired phones, act in each person's own Inventory / Money / Split / Contacts through app links, offer `wishes:read` / `wishes:write`, publish events, report connection modes for Core's Links page |
| `CORE_APP_URL` | `BASE_URL` | How Core reaches this app for health checks (e.g. `http://host.docker.internal:8198`) |
| `DEMO_DATA` | `false` | Seed four lists and nine wishes into an empty owner account |

## Auth modes

- **local** — accounts with email + password. The first account becomes admin (the owner). Accounts created on first contact (Pocket Core / OIDC sign-in, Core device tokens) are members.
- **proxy** — trust a forward-auth proxy (Authelia, Authentik, Tinyauth…). Users are created on first visit. Keep `/s/*`, `/api/v1/s/*` and any bearer-token routes outside the forward-auth rule.
- **none** — no login at all; one implicit admin. Only for a trusted LAN (shared links then treat every visitor as the owner).

## CLI

```
pocket-wishlist                     start the server (default)
pocket-wishlist backup [-o FILE]    write a consistent SQLite backup
pocket-wishlist reset-password [-email USER] [-password NEW]
pocket-wishlist version
```

Inside the container: `docker exec pocket-wishlist pocket-wishlist backup`.

## Price checks

Each person picks how often their watched wishes are read again (Settings → General → Price watching, 12 h by default, 0 = never) and how big a drop is worth a push (5 % by default, besides the target price). The janitor runs every 15 minutes and reads the pages that are due, least recently checked first. A page that no longer shows a price keeps the last known one and says why on the wish. An alert is sent once per drop; when the price goes back up, the next drop alerts again. "Back in stock" alerts once too.

Prices come from what shops publish for link previews: schema.org `Product` / `Offer` data (JSON-LD or microdata) and the Open Graph `product:price:*` tags. Shops that build their pages in the browser only, or block servers, show no price — type it in and the wish works the same, just without watching.

## Deal hunts

1. **Plan** — the AI turns the request into the exact product name, what an offer must match, what means a different item, and 3–6 searches in the languages of your countries. Without AI: the request itself, plus “price” / “kaufen”.
2. **Search** — the searches run in parallel on SearXNG; up to 24 pages are kept, at most 3 per shop, never video / social / review sites.
3. **Read** — every page goes through the same reader as “Add from a link” (6 at a time). Prices written in the search results (“ab 194,99 €”) are kept too.
4. **Judge** — the AI labels each page exact / variant / used / comparison (accessories and unrelated pages are left out). A price it reports must match the page or the search result within 1 %, or the offer is dropped.
5. **Delivery** — with *Deliver to* set, each shop is checked, strongest evidence first: a shop on that country's domain delivers; the shop's schema.org `shippingDetails` name the countries (and the cost); the product page's shipping sentences; the shop's own shipping / delivery page (cached 30 days per shop) naming the country — in about twenty languages, with "no shipping to …" never read as a yes — or listing eight or more other countries without it (= no). What is still open goes to the AI as evidence; without evidence it stays "not confirmed". Shops that don't deliver are left out (or shown last, Settings). Known shipping costs are added before ranking.
6. **Rank** — converted to your currency at today's rate; exact matches, in stock, within budget, cheapest first. Dismissed offers stay hidden in later hunts.

The request and the pages found are sent to the AI you chose (Pocket AI = your own Claude account through Pocket Core).
