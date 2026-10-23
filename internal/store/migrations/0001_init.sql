CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- A list belongs to one person. kind "mine" is their own wishlist (others can
-- be given a link to it and reserve gifts without the owner seeing who);
-- kind "gifts" is ideas for someone else (recipient, optionally a Pocket
-- Contacts contact) with an occasion. Ids are never reused (AUTOINCREMENT):
-- links from other apps and durable actions point at them.
CREATE TABLE lists (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  kind           TEXT NOT NULL DEFAULT 'mine',      -- mine | gifts
  emoji          TEXT NOT NULL DEFAULT '',
  color          TEXT NOT NULL DEFAULT '',
  recipient      TEXT NOT NULL DEFAULT '',          -- gifts: who they are for
  contact_id     INTEGER,                           -- gifts: Pocket Contacts id
  occasion       TEXT NOT NULL DEFAULT '',          -- "Birthday", "Christmas"…
  occasion_date  TEXT NOT NULL DEFAULT '',          -- YYYY-MM-DD (next one)
  budget         INTEGER NOT NULL DEFAULT 0,        -- minor units of currency, 0 = none
  currency       TEXT NOT NULL DEFAULT '',
  show_reserved  INTEGER NOT NULL DEFAULT 0,        -- mine: owner sees that something is reserved (never by whom)
  archived       INTEGER NOT NULL DEFAULT 0,
  sort_order     INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX lists_user ON lists(user_id, archived, sort_order);

-- One wish. Money is in minor units of its currency.
CREATE TABLE wishes (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  list_id            INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  title              TEXT NOT NULL,
  url                TEXT NOT NULL DEFAULT '',
  store              TEXT NOT NULL DEFAULT '',
  notes              TEXT NOT NULL DEFAULT '',
  tags               TEXT NOT NULL DEFAULT '',
  image_url          TEXT NOT NULL DEFAULT '',     -- where the picture came from
  photo_path         TEXT NOT NULL DEFAULT '',     -- local copy (FILES_DIR relative)
  price              INTEGER NOT NULL DEFAULT 0,
  currency           TEXT NOT NULL DEFAULT '',
  target_price       INTEGER NOT NULL DEFAULT 0,   -- tell me when it is at or below this
  priority           INTEGER NOT NULL DEFAULT 2,   -- 1 nice to have · 2 want · 3 must have
  quantity           INTEGER NOT NULL DEFAULT 1,
  status             TEXT NOT NULL DEFAULT 'wanted', -- wanted | bought | archived
  track_price        INTEGER NOT NULL DEFAULT 0,
  availability       TEXT NOT NULL DEFAULT '',     -- in_stock | out_of_stock | preorder | ''
  checked_at         TEXT NOT NULL DEFAULT '',
  check_error        TEXT NOT NULL DEFAULT '',
  lowest_price       INTEGER NOT NULL DEFAULT 0,
  alerted_price      INTEGER NOT NULL DEFAULT 0,   -- the price the last drop alert was about
  bought_at          TEXT NOT NULL DEFAULT '',
  bought_price       INTEGER NOT NULL DEFAULT 0,
  bought_currency    TEXT NOT NULL DEFAULT '',
  bought_store       TEXT NOT NULL DEFAULT '',
  inventory_item_id  INTEGER,
  money_tx_id        INTEGER,
  split_expense_id   INTEGER,
  split_group_id     INTEGER,
  handoff_note       TEXT NOT NULL DEFAULT '',     -- what happened in the other apps
  source             TEXT NOT NULL DEFAULT '',     -- '' = the person; else the Pocket app (slug) that added it
  sort_order         INTEGER NOT NULL DEFAULT 0,
  created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX wishes_list ON wishes(list_id, status, sort_order);
CREATE INDEX wishes_user ON wishes(user_id, status);
CREATE INDEX wishes_track ON wishes(track_price, status, checked_at);

CREATE TABLE price_history (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  wish_id   INTEGER NOT NULL REFERENCES wishes(id) ON DELETE CASCADE,
  at        TEXT NOT NULL,
  price     INTEGER NOT NULL,
  currency  TEXT NOT NULL DEFAULT '',
  source    TEXT NOT NULL DEFAULT 'check'          -- check | manual | added
);
CREATE INDEX price_history_wish ON price_history(wish_id, at);

-- Sharing a "mine" list: a secret link anyone on the network can open, or a
-- person on this box (who then sees it under "Shared with me").
CREATE TABLE shares (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  list_id     INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,                       -- link | user
  token       TEXT NOT NULL DEFAULT '',            -- link: the secret in /s/<token>
  user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX shares_token ON shares(token) WHERE token <> '';
CREATE UNIQUE INDEX shares_user ON shares(list_id, user_id) WHERE user_id IS NOT NULL;

-- "I'll get this": a gift-giver reserves a wish. The list owner never sees
-- these rows (only, when show_reserved is on, that a wish is taken).
CREATE TABLE reservations (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  wish_id     INTEGER NOT NULL REFERENCES wishes(id) ON DELETE CASCADE,
  share_id    INTEGER REFERENCES shares(id) ON DELETE SET NULL,
  user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,  -- a person on this box
  name        TEXT NOT NULL DEFAULT '',            -- a link visitor's name
  secret_hash TEXT NOT NULL DEFAULT '',            -- a link visitor releases with this
  quantity    INTEGER NOT NULL DEFAULT 1,
  note        TEXT NOT NULL DEFAULT '',
  bought      INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX reservations_wish ON reservations(wish_id);
