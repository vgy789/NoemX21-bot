-- 000022_remove_reviews_progress_text.down.sql
ALTER TABLE review_requests ADD COLUMN reviews_progress_text TEXT;
