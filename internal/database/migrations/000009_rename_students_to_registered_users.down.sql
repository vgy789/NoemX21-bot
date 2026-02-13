-- Rollback: восстановить поля статистики и вернуть имя students
ALTER TABLE registered_users ADD COLUMN campus_id UUID REFERENCES campuses(id);
ALTER TABLE registered_users ADD COLUMN coalition_id SMALLINT REFERENCES coalitions(id);
ALTER TABLE registered_users ADD COLUMN status ENUM_STUDENT_STATUS DEFAULT 'ACTIVE';
ALTER TABLE registered_users ADD COLUMN level INT DEFAULT 0;
ALTER TABLE registered_users ADD COLUMN exp_value INT DEFAULT 0;
ALTER TABLE registered_users ADD COLUMN prp INT DEFAULT 0;
ALTER TABLE registered_users ADD COLUMN crp INT DEFAULT 0;
ALTER TABLE registered_users ADD COLUMN coins INT DEFAULT 0;
ALTER TABLE registered_users ADD COLUMN parallel_name VARCHAR(100);
ALTER TABLE registered_users ADD COLUMN class_name VARCHAR(100);
ALTER TABLE registered_users ADD COLUMN integrity REAL;
ALTER TABLE registered_users ADD COLUMN friendliness REAL;
ALTER TABLE registered_users ADD COLUMN punctuality REAL;
ALTER TABLE registered_users ADD COLUMN thoroughness REAL;

ALTER TABLE registered_users RENAME TO students;
