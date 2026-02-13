-- Rollback: change student_skills FK back to registered_users
ALTER TABLE student_skills
DROP CONSTRAINT student_skills_s21_login_fkey;

ALTER TABLE student_skills
ADD CONSTRAINT student_skills_student_id_fkey
FOREIGN KEY (student_id) REFERENCES registered_users(s21_login) ON DELETE CASCADE;

-- Also rollback renames performed in the combined up migration
-- 1) Drop the FK on participant_skills if present
ALTER TABLE IF EXISTS participant_skills
	DROP CONSTRAINT IF EXISTS participant_skills_s21_login_fkey;

-- 2) Rename s21_login back to student_id in related tables
ALTER TABLE IF EXISTS participant_skills RENAME COLUMN IF EXISTS s21_login TO student_id;
ALTER TABLE IF EXISTS auth_verification_codes RENAME COLUMN IF EXISTS s21_login TO student_id;
ALTER TABLE IF EXISTS rocketchat_credentials RENAME COLUMN IF EXISTS s21_login TO student_id;
ALTER TABLE IF EXISTS platform_credentials RENAME COLUMN IF EXISTS s21_login TO student_id;
ALTER TABLE IF EXISTS user_accounts RENAME COLUMN IF EXISTS s21_login TO student_id;

-- 3) Rename participant_skills back to student_skills
ALTER TABLE IF EXISTS participant_skills RENAME TO student_skills;

-- 4) Recreate original FK pointing to registered_users(s21_login)
ALTER TABLE student_skills
	DROP CONSTRAINT IF EXISTS student_skills_student_id_fkey;

ALTER TABLE student_skills
	ADD CONSTRAINT student_skills_student_id_fkey
	FOREIGN KEY (student_id) REFERENCES registered_users(s21_login) ON DELETE CASCADE;
