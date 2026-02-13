-- Fix student_skills to reference participant_stats_cache instead of students/registered_users
-- student_skills now links skills to any participant in the cache, not just registered users

-- Combined migration: fix FK and rename columns/table
-- 1) Rename student_skills -> participant_skills
ALTER TABLE student_skills RENAME TO participant_skills;

-- 2) Rename student_id -> s21_login in related tables
ALTER TABLE user_accounts RENAME COLUMN student_id TO s21_login;
ALTER TABLE platform_credentials RENAME COLUMN student_id TO s21_login;
ALTER TABLE rocketchat_credentials RENAME COLUMN student_id TO s21_login;
ALTER TABLE auth_verification_codes RENAME COLUMN student_id TO s21_login;
ALTER TABLE participant_skills RENAME COLUMN student_id TO s21_login;

-- 3) Replace FK on participant_skills to reference participant_stats_cache(s21_login)
ALTER TABLE participant_skills
	DROP CONSTRAINT IF EXISTS student_skills_student_id_fkey;

ALTER TABLE participant_skills
	ADD CONSTRAINT participant_skills_s21_login_fkey
	FOREIGN KEY (s21_login) REFERENCES participant_stats_cache(s21_login) ON DELETE CASCADE;
