DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM club_categories WHERE id > 32767) THEN
        RAISE EXCEPTION 'cannot downgrade club_categories.id to SMALLINT: values exceed SMALLINT range';
    END IF;
END $$;

ALTER TABLE clubs
ALTER COLUMN category_id TYPE SMALLINT;

ALTER TABLE club_categories
ALTER COLUMN id TYPE SMALLINT;

ALTER SEQUENCE IF EXISTS club_categories_id_seq
AS SMALLINT
MINVALUE 1
MAXVALUE 32767;

SELECT setval(
    'club_categories_id_seq',
    COALESCE((SELECT MAX(id) FROM club_categories), 0) + 1,
    false
);
