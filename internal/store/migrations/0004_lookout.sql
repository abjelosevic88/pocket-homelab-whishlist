-- Pocket Lookout reads the price of shops Wishlist cannot (they block robots
-- or only show the price with JavaScript): the watch there, NULL = Wishlist reads the page itself.
ALTER TABLE wishes ADD COLUMN lookout_watch_id INTEGER;
