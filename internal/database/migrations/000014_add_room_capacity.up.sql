-- Add capacity column to rooms table
ALTER TABLE rooms
ADD COLUMN capacity INT NOT NULL DEFAULT 2;
