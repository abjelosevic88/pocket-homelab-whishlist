# API

JSON over HTTP at `/api/v1`. Authenticate with the session cookie (the web app), `Authorization: Bearer <token>` (a personal token from Settings → API & shortcuts, prefix `pw_`), a Pocket Core device/person token, or a Pocket Core link token (`pcx_`, another Pocket app acting for a person — see below). Money is always in **minor units** of its currency (`12990` EUR = €129.90). Everything is per person: another person's lists and wishes answer `404`, admins included.

## Lists

| Method | Path | |
|---|---|---|
| GET | `/lists?archived=1` | Your lists with counts (`wanted`, `bought`, `total`, `drops`, `shared`, `cover`) |
| POST | `/lists` | `{"name","kind":"mine|gifts","emoji","description","recipient","contact_id","occasion","occasion_date","budget","currency","show_reserved"}` |
| GET | `/lists/{id}?sort=` | The list and its wishes (`sort`: `priority`, `price`, `price_desc`, `added`, `title`, default = your order) |
| PUT | `/lists/{id}` | Partial update (fields left out stay) |
| DELETE | `/lists/{id}` | With its wishes, shares and reservations |
| PUT | `/lists/reorder` · `/lists/{id}/order` | `{"ids":[…]}` |
| GET | `/lists/{id}/shares` | Links and people |
| POST | `/lists/{id}/shares` | `{"kind":"link"}` → `{"url","token"}` · `{"kind":"user","user_id"|"email"}`. Only `mine` lists |
| DELETE | `/lists/{id}/shares/{sid}` | A removed link stops working |

## Wishes

| Method | Path | |
|---|---|---|
| GET | `/wishes` | `?list=&status=wanted|bought|archived|all&q=&tag=&kind=mine|gifts&at_target=1&tracked=1&sort=&limit=` |
| POST | `/wishes` | `{"url":"https://…"}` is enough: the page is read for title, picture, price, currency, shop and stock, and the price is watched. Optional: `list_id` (default: your first wishlist, made when missing), `title`, `price` / `price_text` (`"12,99"`), `target_price` / `target_text`, `currency`, `priority` 1–3, `quantity`, `tags`, `notes`, `track_price`, `no_lookup` |
| GET | `/wishes/{id}` | `{"wish","prices":[{"at","price","currency","source"}]}` |
| PUT | `/wishes/{id}` | Partial update; a changed price is recorded in the history |
| DELETE | `/wishes/{id}` | |
| POST | `/wishes/{id}/check` | Read the page now |
| POST / DELETE | `/wishes/{id}/lookout` | Let Pocket Lookout read this price (a browser watch on the shop's price and stock) / read it here again |
| POST | `/wishes/{id}/status` | `{"status":"wanted|bought|archived"}` — no hand-off |
| POST | `/wishes/{id}/bought` | `{"date","price","currency","store","notes","inventory":bool,"inventory_location","inventory_category","money":bool,"money_account_id","money_category_id","split":bool,"split_group_id","split_member_id"}` → `{"wish","pending"}`. Marks it bought and files it in your Pocket apps (durable, retried, never twice); `409` when it is bought already |
| GET/POST/DELETE | `/wishes/{id}/photo` | The picture (multipart `file` to upload) |
| POST | `/preview` | `{"url"}` → what the page says (`product`, `price` in minor units, `currency`) |

## Deal hunting

| Method | Path | |
|---|---|---|
| POST | `/hunt` | `{"query":"Sony WH-1000XM5 black","max_price"|"max_text","condition":"new|any","auto":bool,"list_id","currency"}` → `202` and the new wish; the hunt runs in the background (`hunt_status`: `running` → `done` / `error`, `hunt_step` while it works) |
| POST | `/wishes/{id}/hunt` | Hunt again, optionally with new terms (same fields) |
| GET | `/wishes/{id}/offers?dismissed=1` | `{"offers":[{"url","store","title","price","currency","price_home","availability","match","source","note","over_budget","ships":"yes|no|unknown","ship_note","ship_cost","ship_currency"}],"home_currency"}` best first; `price_home` includes known shipping |
| POST | `/wishes/{id}/offers/{oid}/use` | Make the offer the wish's link and price (and watch it) |
| POST | `/wishes/{id}/offers/{oid}/dismiss` | `{"dismissed":false}` to bring it back |
| GET | `/countries` | ISO codes and names for the Deliver to picker |
| GET | `/ai/status` · `/ai/models` | Which AI judges (`pocket`, `openai`, none) and whether search is set up; your Pocket AI models |

## Sharing and reservations

A list owner never sees who reserved what — only, when the list has `show_reserved`, a count.

| Method | Path | |
|---|---|---|
| GET | `/shared` · `/shared/{id}` | Lists people on this box shared with you; a list with `reserved`/`mine` per wish |
| POST/DELETE | `/shared/{id}/wishes/{wid}/reserve` | `{"quantity","note"}` / drop it |
| POST | `/shared/{id}/wishes/{wid}/bought` | `{"bought":true}` — tick your own reservation |
| GET | `/reservations` | What you promised to get |
| GET | `/s/{token}` | A share link, **no login**. Send `X-Giver: <random secret ≥16 chars>` so the visitor's own reservations come back as `mine` |
| POST/DELETE | `/s/{token}/wishes/{id}/reserve` | `{"name","quantity","note"}` with `X-Giver` |
| POST | `/s/{token}/wishes/{id}/bought` | |

## Everything else

| Method | Path | |
|---|---|---|
| GET | `/dashboard` | For Homepage: `wishes`, `gift_ideas`, `at_target`, `tracked`, `value_text`, `next_occasion`, `to_buy_for_others` |
| GET | `/home` | What the Home page shows |
| GET | `/occasions?days=` | Gift lists with a date plus Pocket Contacts birthdays/anniversaries |
| GET | `/day/{YYYY-MM-DD}` | Wishes added and bought that day (Pocket Day) |
| GET | `/tags` · `/currencies` | |
| GET | `/contacts/search?q=` · `/money/accounts` · `/money/categories?account=` · `/split/groups` · `/inventory/locations` | Pickers, through your connection to that app |
| POST | `/checks/run` | Check your watched prices now (up to 25) |
| GET | `/export.json` · POST `/import` | Your lists, wishes and price history |
| GET | `/backup` | The whole database (admin) |

## Pocket Core app links

Wishlist offers two scopes to other Pocket apps (Core → Links):

- `wishes:read` — `GET /lists`, `/lists/{id}`, `/wishes`, `/wishes/{id}`, `/wishes/{id}/prices|photo`, `/dashboard`, `/occasions`, `/tags`, `/day/{date}`. Only the person's own lists; never lists shared with them, never reservations.
- `wishes:write` — `POST /wishes` (and `/preview`), `PUT`/`DELETE /wishes/{id}` **only for wishes that app added** (their `source` is its slug), and `POST /wishes/{id}/bought` for any of the person's wishes (Pocket Parcels marks the wish a parcel is as bought when it is ordered; a wish already bought answers 409).
- `move:export` / `move:import` — Pocket Move can copy your wishlists out and bring them in: `GET /export.json` out, `POST /import` in (adds; lists with the same name are merged, wishes you have are skipped, so a second run adds nothing). `GET /pocket/portable` describes both.

Every other route answers `403` to a link token. Wishlist itself uses `inference:write` in Pocket AI (deal hunts), `items:read`/`items:write` in Inventory, `accounts:read`/`transactions:write` in Money, `groups:read`/`expenses:write` in Split and `contacts:read` in Contacts.

## Pocket Search, Pocket Day and Pocket Hub

Wishlist answers the four routes every Pocket app can offer (`docs/capabilities.md` in Pocket Core), so Pocket Search, Pocket Day and Pocket Hub find it on their own. Each has its own link scope that opens that one route and nothing else (`wishes:read` and `wishes:write` do not open them); a signed-in person or a personal token can call them too. They see what `GET /wishes` sees: only the person's own wishes, never a list shared with them, never reservations. Addresses (`url`) are paths in the app.

| Route | Scope | What |
|---|---|---|
| `GET /pocket/search?q&limit` | `pocket:search` | `{items}`: your wishes that have every word of `q` in the name, shop, notes, tags or who a gift is for. Wanted ones first, then bought, then archived; a name that has the words before the rest. Each has `id`, `title`, `snippet` (price · shop · list, starting with "Bought" or "Archived" when it is), `kind` `wish`, `date` (when it was added), `url` and `link` (the shop page). Words without a letter or digit find nothing. `limit` 1–50 (8) |
| `GET /pocket/day/{date}?tz=` | `pocket:day` | `{date, summary, items, metrics}`: the wishes bought that day (`kind` `bought`, `amount` what was paid), the prices that ended the day somewhere else than they started it (`kind` `price`, `amount` the new price, `sub` "Shop · down from 129.00 EUR") and the wishes added (`kind` `added`, `amount` the price). `amount` is minor units of `currency`; `sub` is the shop, else the list. A wish shows once. The day is the one of `tz`, else the server's. `summary` is like "2 wishes added, 1 bought, 1 price drop" |
| `GET /pocket/widget` | `pocket:widget` | `{line, metrics, items, url}`: the wishes at or below their target price first (most wanted first), then the ones added last, at most 8, with `detail` "95.50 EUR, was 129.00 EUR · Shop". `line` is "2 price drops", else "14 wishes"; `metrics` are Wishes, Price drops and Total (what `/dashboard` calls `value_text`). No shop page is read. Nothing wanted: empty |
| `POST /pocket/capture` | `pocket:capture` | `{request_id, kind: "wish", title, text}` adds a wish to your first wishlist (made when missing) and answers `201 {id, title, url}`. The name is `title`, else the first line of `text`; more lines become the notes. The first web address in the line becomes the wish's link and is taken out of the name: the page is read as `POST /wishes` reads it (name, price, shop, stock; the price is then watched), for at most 8 seconds, and the picture is copied after the answer. A link alone is enough; when its page cannot be read the answer is `400` asking for a name in front of the link. The same `request_id` again answers the same wish and adds nothing; with other words it answers `400` |

## Events (Pocket Core bus)

`wishlist.wish_added`, `wishlist.price_dropped` (`was`, `target`, `why`: `target|drop`), `wishlist.back_in_stock`, `wishlist.better_deal` (a repeat hunt found it clearly cheaper), `wishlist.bought` (`price`, `currency`, `date`, `store`, `gift_for`), `wishlist.reserved` — carrying the **giver's** `user_email`, never the list owner's, so a journal does not spoil the surprise.
