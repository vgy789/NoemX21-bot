ALTER TABLE clubs
ALTER COLUMN category_id TYPE INTEGER;

ALTER TABLE club_categories
ALTER COLUMN id TYPE INTEGER;

ALTER SEQUENCE IF EXISTS club_categories_id_seq
AS INTEGER
NO MINVALUE
NO MAXVALUE;

SELECT setval(
    'club_categories_id_seq',
    COALESCE((SELECT MAX(id) FROM club_categories), 0) + 1,
    false
);
