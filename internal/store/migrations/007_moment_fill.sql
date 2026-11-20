-- When set, the planning tints the column of the moment with its color, not
-- only the dot next to its title. It needs a color to have an effect.
ALTER TABLE moments ADD COLUMN fill INTEGER NOT NULL DEFAULT 0;
