-- 000009: Переименование students → registered_users.
-- Убираем дублированные поля статистики (они живут в participant_stats_cache).

-- 1. Переименовать таблицу
ALTER TABLE students RENAME TO registered_users;

-- 2. Убрать дублирующиеся поля статистики из registered_users
--    (теперь они хранятся только в participant_stats_cache)
ALTER TABLE registered_users DROP COLUMN IF EXISTS campus_id;
ALTER TABLE registered_users DROP COLUMN IF EXISTS coalition_id;
ALTER TABLE registered_users DROP COLUMN IF EXISTS status;
ALTER TABLE registered_users DROP COLUMN IF EXISTS level;
ALTER TABLE registered_users DROP COLUMN IF EXISTS exp_value;
ALTER TABLE registered_users DROP COLUMN IF EXISTS prp;
ALTER TABLE registered_users DROP COLUMN IF EXISTS crp;
ALTER TABLE registered_users DROP COLUMN IF EXISTS coins;
ALTER TABLE registered_users DROP COLUMN IF EXISTS parallel_name;
ALTER TABLE registered_users DROP COLUMN IF EXISTS class_name;
ALTER TABLE registered_users DROP COLUMN IF EXISTS integrity;
ALTER TABLE registered_users DROP COLUMN IF EXISTS friendliness;
ALTER TABLE registered_users DROP COLUMN IF EXISTS punctuality;
ALTER TABLE registered_users DROP COLUMN IF EXISTS thoroughness;
