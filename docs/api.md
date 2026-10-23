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
| POST | `/wishes/{id}/status` | `{"status":"wanted|bought|archived"}` — no hand-off |
| POST | `/wishes/{id}/bought` | `{"date","price","currency","store","notes","inventory":bool,"inventory_location","inventory_category","money":bool,"money_account_id","money_category_id","split":bool,"split_group_id","split_member_id"}` → `{"wish","pending"}`. Marks it bought and files it in your Pocket apps (durable, retried, never twice); `409` when it is bought already |
| GET/POST/DELETE | `/wishes/{id}/photo` | The picture (multipart `file` to upload) |
| POST | `/preview` | `{"url"}` → what the page says (`product`, `price` in minor units, `currency`) |

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
- `wishes:write` — `POST /wishes` (and `/preview`), `PUT`/`DELETE /wishes/{id}` **only for wishes that app added** (their `source` is its slug).

Every other route answers `403` to a link token. Wishlist itself uses `items:read`/`items:write` in Inventory, `accounts:read`/`transactions:write` in Money, `groups:read`/`expenses:write` in Split and `contacts:read` in Contacts.

## Events (Pocket Core bus)

`wishlist.wish_added`, `wishlist.price_dropped` (`was`, `target`, `why`: `target|drop`), `wishlist.back_in_stock`, `wishlist.bought` (`price`, `currency`, `date`, `store`, `gift_for`), `wishlist.reserved` — carrying the **giver's** `user_email`, never the list owner's, so a journal does not spoil the surprise.
