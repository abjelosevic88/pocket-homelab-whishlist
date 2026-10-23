-- Deal hunting: describe what you want, and Wishlist searches the web, reads
-- the shop pages and (with Pocket AI or OpenAI) picks the matching offers.
ALTER TABLE wishes ADD COLUMN hunt_query     TEXT NOT NULL DEFAULT '';  -- what the person asked for
ALTER TABLE wishes ADD COLUMN hunt_max       INTEGER NOT NULL DEFAULT 0; -- highest price worth showing (wish currency)
ALTER TABLE wishes ADD COLUMN hunt_condition TEXT NOT NULL DEFAULT 'new'; -- new | any
ALTER TABLE wishes ADD COLUMN hunt_auto      INTEGER NOT NULL DEFAULT 0; -- hunt again every week
ALTER TABLE wishes ADD COLUMN hunt_status    TEXT NOT NULL DEFAULT '';  -- '' | running | done | error
ALTER TABLE wishes ADD COLUMN hunt_step      TEXT NOT NULL DEFAULT '';  -- what a running hunt is doing
ALTER TABLE wishes ADD COLUMN hunt_at        TEXT NOT NULL DEFAULT '';  -- when the last hunt finished
ALTER TABLE wishes ADD COLUMN hunt_summary   TEXT NOT NULL DEFAULT '';  -- the AI's two lines on the result
ALTER TABLE wishes ADD COLUMN hunt_error     TEXT NOT NULL DEFAULT '';
ALTER TABLE wishes ADD COLUMN hunt_ai        TEXT NOT NULL DEFAULT '';  -- which AI judged (pocket | openai | '' = keywords only)
ALTER TABLE wishes ADD COLUMN hunt_best      INTEGER NOT NULL DEFAULT 0; -- best offer's price in the home currency at the last hunt

CREATE TABLE offers (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  wish_id      INTEGER NOT NULL REFERENCES wishes(id) ON DELETE CASCADE,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url          TEXT NOT NULL,
  store        TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  image        TEXT NOT NULL DEFAULT '',
  price        INTEGER NOT NULL DEFAULT 0,       -- minor units of currency
  currency     TEXT NOT NULL DEFAULT '',
  price_home   INTEGER NOT NULL DEFAULT 0,       -- converted to the person's home currency (0 = no rate)
  availability TEXT NOT NULL DEFAULT '',
  match        TEXT NOT NULL DEFAULT 'unknown',  -- exact | variant | used | comparison | unknown
  source       TEXT NOT NULL DEFAULT 'page',     -- page (read from the shop) | snippet (search result text)
  note         TEXT NOT NULL DEFAULT '',
  over_budget  INTEGER NOT NULL DEFAULT 0,
  rank         INTEGER NOT NULL DEFAULT 0,
  dismissed    INTEGER NOT NULL DEFAULT 0,
  found_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (wish_id, url)
);
CREATE INDEX offers_wish ON offers(wish_id, dismissed, rank);
