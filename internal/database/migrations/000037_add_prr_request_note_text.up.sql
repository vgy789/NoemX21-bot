ALTER TABLE review_requests
ADD COLUMN IF NOT EXISTS request_note_text TEXT NOT NULL DEFAULT '';
