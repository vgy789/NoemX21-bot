CREATE TABLE club_categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE clubs (
    id SMALLINT,
    campus_id UUID NOT NULL REFERENCES campuses(id),
    leader_login VARCHAR(255),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    category_id INTEGER NOT NULL REFERENCES club_categories(id),
    external_link TEXT,
    is_local BOOLEAN DEFAULT true,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campus_id, id)
);

CREATE INDEX idx_clubs_campus_id ON clubs(campus_id);

-- Alter campuses table
-- Ensure short_name is unique (in case it was dropped)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'campuses_short_name_key') THEN
        ALTER TABLE campuses ADD CONSTRAINT campuses_short_name_key UNIQUE (short_name);
    END IF;
END $$;

ALTER TABLE campuses ALTER COLUMN is_active SET NOT NULL;
ALTER TABLE campuses ALTER COLUMN is_active SET DEFAULT true;
