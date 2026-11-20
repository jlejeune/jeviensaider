-- When set, the planning colours the whole row of the stand, not only the dot
-- next to its name. The organiser chooses it for each stand.
ALTER TABLE stands ADD COLUMN fill_row INTEGER NOT NULL DEFAULT 0;
