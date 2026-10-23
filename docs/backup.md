# Backup & restore

Everything lives in `DATA_DIR` (`/data` in the container):

- `pocket-wishlist.db` (+ `-wal` / `-shm` while running) — lists, wishes, price history, shares, reservations, settings, users
- `files/photos/…` — wish pictures (downloaded from shops or uploaded)
- `backups/` — where the CLI writes snapshots

## Consistent copies

Never copy a live WAL database file. Use one of:

- **UI**: Settings → Backup & data → *Download database*
- **API**: `GET /api/v1/backup` with a bearer token
- **CLI**: `docker exec pocket-wishlist pocket-wishlist backup` → prints the path under `/data/backups/`
- **sqlite3**: `sqlite3 data/pocket-wishlist.db "VACUUM INTO 'snap.db'"`

Then back up `files/` with your usual tool (restic, Backrest, rsync). Exclude `pocket-wishlist.db*` from raw file backups and include the snapshot instead.

## Restore

1. Stop the container.
2. Replace `data/pocket-wishlist.db` with the snapshot (remove any stale `-wal` / `-shm`).
3. Restore `files/`.
4. Start the container. Migrations run forward automatically.

## JSON export

Settings → Backup & data exports everything but users and tokens as one JSON file; importing it merges (lists by name and kind, wishes by list and title).
