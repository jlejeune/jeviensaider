-- Pledge lists: things people promise to bring. drop_off is HH:MM on the
-- event date, or ''. min_items and max_items count items, not people. unit is
-- free text in the plural, such as 'gâteaux' (its length is checked in Go).
CREATE TABLE pledge_lists (
    id          INTEGER PRIMARY KEY,
    event_id    INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    unit        TEXT NOT NULL DEFAULT '',
    drop_off    TEXT NOT NULL DEFAULT '' CHECK (drop_off = '' OR drop_off GLOB '[0-2][0-9]:[0-5][0-9]'),
    min_items   INTEGER CHECK (min_items IS NULL OR min_items >= 1),
    max_items   INTEGER CHECK (max_items IS NULL OR max_items >= 1),
    CHECK (min_items IS NULL OR max_items IS NULL OR max_items >= min_items)
);
CREATE INDEX pledge_lists_event ON pledge_lists(event_id, id);

CREATE TABLE pledges (
    id           INTEGER PRIMARY KEY,
    list_id      INTEGER NOT NULL REFERENCES pledge_lists(id) ON DELETE CASCADE,
    volunteer_id INTEGER NOT NULL REFERENCES volunteers(id) ON DELETE CASCADE,
    quantity     INTEGER NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 20),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    UNIQUE (list_id, volunteer_id)
);
CREATE INDEX pledges_volunteer ON pledges(volunteer_id);
