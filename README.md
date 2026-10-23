<p align="center"><img src="web/public/favicon.svg" width="72" alt=""></p>
<h1 align="center">Pocket Wishlist</h1>
<p align="center">Self-hosted wishlists and gift ideas. Paste a link to anything in any shop: the name, picture and price are read from the page, the price is watched, and you get a push when it reaches your target. Share a list with family so they can say “I’ll get this” — nobody buys the same present twice and you never see who got what. Collect gift ideas for the people you buy for, with their birthdays from Pocket Contacts. When you buy something, it goes into your Pocket Inventory, Pocket Money and, for a shared purchase or a group gift, Pocket Split.<br>One Go binary, one SQLite file, one volume. Part of the Pocket apps; works on its own too.</p>
<p align="center">
  <a href="https://github.com/abjelosevic88/pocket-homelab-whishlist/actions"><img src="https://github.com/abjelosevic88/pocket-homelab-whishlist/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/abjelosevic88/pocket-homelab-whishlist/pkgs/container/pocket-homelab-whishlist"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-blue" alt="image"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT"></a>
</p>

---

## What it does

- **Find the best deal** — say what you want (“Sony WH-1000XM5, black, new, under 300”). Wishlist plans the searches (with Pocket AI or your own OpenAI key: the exact product name and model number, in the languages of the countries you buy from), searches through your SearXNG, reads every shop page it finds, and keeps only offers for exactly that item — not a case, not the older model. Every price shown was on the shop page or in the search result; anything else the AI says is dropped. Offers are ranked in your currency; the best becomes the wish's link and its price is watched. “Keep hunting” runs it again every week and pushes when it gets clearly cheaper. Without an AI it still works, matching by keywords.
- **Add from a link** — schema.org Product data and Open Graph tags give the title, picture, price, currency and stock. A bookmarklet, `/add?url=…` for iOS Shortcuts and the PWA share target add from anywhere. Pictures are stored locally.
- **Watch prices** — every watched page is read again on your schedule (12 h by default). The history is a chart on each wish; *Price drops* lists what is at your target; a push goes out once per drop (target reached, a drop of your percentage, back in stock).
- **Wishlists and gift ideas** — your own lists, and lists for someone else with an occasion, a date (birthdays roll on every year), a budget and what you already bought.
- **Share without spoiling** — a secret link (no account needed) or a person on the box. Givers reserve, see what is still free and tick what they bought; the owner sees at most that something is taken, never by whom.
- **Bought it** — files the purchase in Pocket Inventory (item with price, date, shop and link), Pocket Money (an expense tagged `wishlist`, `gift` for gifts) and Pocket Split (evenly in a group), as a durable action that retries and never books twice.
- **Occasions** — birthdays and anniversaries from Pocket Contacts next to your gift lists, with reminders 14, 7, 3 and 1 days before.
- Per person: everyone has their own lists and connections; admins manage accounts, not content. Keyboard shortcuts, ⌘K, light/dark, phone layout, JSON export/import, Homepage widget endpoint.

## Run it

```sh
docker run -d --name pocket-wishlist -p 8080:8080 -v ./data:/data \
  -e BASE_URL=https://wishlist.example.com -e SECURE_COOKIES=true \
  ghcr.io/abjelosevic88/pocket-homelab-whishlist:latest
```

or `docker compose up -d` with [docker-compose.yml](docker-compose.yml). Open it, create the first account, paste a link. All options: [docs/configuration.md](docs/configuration.md). API: [docs/api.md](docs/api.md).

## With Pocket Core

Set `CORE_URL` and `CORE_REGISTRATION_KEY` and Wishlist joins the box: people sign in with Pocket Core, phones paired with Core work right away, and Wishlist acts in each person's own Inventory, Money, Split and Contacts through app links — nothing to paste. Other Pocket apps can read wishlists (`wishes:read`, e.g. Pocket Day) or add wishes (`wishes:write`). Without Core every connection can be made directly with a URL and an API token; the Core-only choices stay visible, greyed out.

## Develop

```sh
make dev     # API on :8080 with demo data + Vite on :5173
make test    # go vet, go test, tsc
make         # web + binary
```

MIT licensed.
