<p align="center"><img src="web/public/favicon.svg" width="72" alt=""></p>
<h1 align="center">Pocket Wishlist</h1>
<p align="center">Self-hosted wishlists and gift ideas. Paste a link to anything in any shop: the name, picture and price are read from the page, the price is watched, and you get a push when it reaches your target. Share a list with family so they can say “I’ll get this” — nobody buys the same present twice and you never see who got what. Collect gift ideas for the people you buy for, with their birthdays from Pocket Contacts. When you buy something, it goes into your Pocket Inventory, Pocket Money and, for a shared purchase or a group gift, Pocket Split.<br>One Go binary, one SQLite file, one volume. Part of the Pocket apps; works on its own too.</p>
<p align="center">
  <a href="https://github.com/abjelosevic88/pocket-homelab-whishlist/actions"><img src="https://github.com/abjelosevic88/pocket-homelab-whishlist/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
  <a href="https://github.com/abjelosevic88/pocket-homelab-whishlist/pkgs/container/pocket-homelab-whishlist"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64%20%7C%20armv7-blue" alt="image"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT"></a>
</p>

---

<p align="center"><img src="docs/screenshots/desktop-home.png" width="900" alt="Home: wishes at their target price, the next birthday and what was bought recently"></p>

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/desktop-list.png" alt="A wishlist with pictures, prices, price drops and reserved wishes"></td>
    <td width="50%"><img src="docs/screenshots/desktop-price-chart.png" alt="A wish with its price history chart and target price"></td>
  </tr>
  <tr>
    <td align="center"><sub>A wishlist; the owner sees that something is reserved, never by whom.</sub></td>
    <td align="center"><sub>Every watched price keeps its history, with the target you set.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/desktop-price-drops.png" alt="Price drops: every wish at or below its target price"></td>
    <td width="50%"><img src="docs/screenshots/desktop-gift-list.png" alt="A gift list for a birthday with its budget bar"></td>
  </tr>
  <tr>
    <td align="center"><sub>Price drops: everything at or below the price you said you would pay.</sub></td>
    <td align="center"><sub>Gift ideas for someone, with the date and how much of the budget is spent.</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/desktop-share-link.png" alt="The shared list as a giver sees it, without an account"></td>
    <td width="50%"><img src="docs/screenshots/desktop-reserve.png" alt="A giver saying I'll get this for a wish"></td>
  </tr>
  <tr>
    <td align="center"><sub>The link you send: no account needed to see the list.</sub></td>
    <td align="center"><sub>“I’ll get this”, so nobody buys the same present twice.</sub></td>
  </tr>
</table>

<p align="center">
  <img src="docs/screenshots/phone-list.png" width="240" alt="A wishlist on a phone">&nbsp;&nbsp;
  <img src="docs/screenshots/phone-share-link.png" width="240" alt="The shared list on a giver's phone">
</p>

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
