-- Кеш статистики участников S21 (поиск пира). Не связан с user_accounts.
-- Таблица students остаётся только для зарегистрированных в боте пользователей.
CREATE TABLE participant_stats_cache (
    s21_login VARCHAR(21) PRIMARY KEY,
    campus_id UUID REFERENCES campuses(id),
    coalition_id SMALLINT REFERENCES coalitions(id),
    status ENUM_STUDENT_STATUS NOT NULL,
    level INT NOT NULL DEFAULT 0,
    exp_value INT NOT NULL DEFAULT 0,
    prp INT NOT NULL DEFAULT 0,
    crp INT NOT NULL DEFAULT 0,
    coins INT NOT NULL DEFAULT 0,
    parallel_name VARCHAR(100),
    class_name VARCHAR(100),
    integrity REAL,
    friendliness REAL,
    punctuality REAL,
    thoroughness REAL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_participant_stats_cache_updated_at ON participant_stats_cache(updated_at);
