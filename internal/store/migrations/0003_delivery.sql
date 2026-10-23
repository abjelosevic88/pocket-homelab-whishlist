-- Delivery: whether a shop delivers to the person's country, and what it costs.
ALTER TABLE offers ADD COLUMN ships         TEXT NOT NULL DEFAULT 'unknown'; -- yes | no | unknown
ALTER TABLE offers ADD COLUMN ship_note     TEXT NOT NULL DEFAULT '';        -- the evidence, in a few words
ALTER TABLE offers ADD COLUMN ship_cost     INTEGER NOT NULL DEFAULT -1;     -- minor units of ship_currency; -1 = not known, 0 = free
ALTER TABLE offers ADD COLUMN ship_currency TEXT NOT NULL DEFAULT '';
