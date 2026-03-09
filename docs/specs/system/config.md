# Конфигурация

Сводка по env-конфигурации приложения.

## Загрузка конфигурации

- Источник: `env/.env` и переменные окружения процесса
- Парсинг: `caarlos0/env`
- Ошибка разбора обязательных параметров останавливает запуск

## Обязательные переменные

| Variable | Тип | Подсистема |
|---|---|---|
| `DATABASE_URL` | string | PostgreSQL |
| `TELEGRAM_BOT_TOKEN` | secret | Telegram |
| `ROCKETCHAT_API_URL` | secret | Rocket.Chat |
| `ROCKETCHAT_USER_ID` | secret | Rocket.Chat |
| `ROCKETCHAT_AUTH_TOKEN` | secret | Rocket.Chat |
| `SCHOOL21_API_URL` | string | School 21 |
| `SCHOOL21_USER_LOGIN` | string | School 21 |
| `SCHOOL21_USER_PASSWORD` | secret | School 21 |
| `AEAD_KEY` | secret | crypto |

## Optional с defaults

| Variable | Default |
|---|---|
| `LOG_LEVEL` | `debug` |
| `API_SERVER_PORT` | `8081` |
| `PRODUCTION` | `false` |
| `TEST_MODE_NO_OTP` | `false` |
| `TELEGRAM_WEBHOOK_ENABLED` | `false` |
| `TELEGRAM_WEBHOOK_PATH` | `/telegram/webhook` |
| `TELEGRAM_WEBHOOK_PORT` | `8080` |
| `REQUEST_TIMEOUT` | `25s` |
| `POLLING_TIMEOUT` | `9` |
| `MAX_ROUTINES` | `0` |
| `DROP_PENDING_UPDATES` | `true` |
| `GIT_BRANCH` | `main` |
| `GIT_SYNC_INTERVAL` | `5m` |
| `GIT_LOCAL_PATH` | `data` |
| `GIT_CAMPUSES_PATH` | `campuses` |
| `GIT_PROJECTS_PATH` | `bot_content/various/projects.yaml` |
| `CHART_TEMP_DIR` | `tmp/skills_radar` |
| `SCHEDULE_IMAGES_ENABLED` | `true` |
| `SCHEDULE_IMAGES_INTERVAL` | `5m` |
| `SCHEDULE_IMAGES_TEMP_DIR` | `tmp/schedules` |

## Режимы Telegram

### Polling

- `TELEGRAM_WEBHOOK_ENABLED=false`
- используются `POLLING_TIMEOUT`, `REQUEST_TIMEOUT`, `MAX_ROUTINES`, `DROP_PENDING_UPDATES`

### Webhook

- `TELEGRAM_WEBHOOK_ENABLED=true`
- обязательно задать `TELEGRAM_WEBHOOK_URL`
- опционально `TELEGRAM_WEBHOOK_SECRET`
- listener gotgbot стартует на `TELEGRAM_WEBHOOK_PORT`

## Git Sync

Если `GIT_REPO_URL` не задан:

- сервис не делает `git pull`
- пытается работать в local-only режиме
- читает локальные файлы из `GIT_LOCAL_PATH`

Если `GIT_REPO_URL` задан:

- нужен `SSH_KEY_BASE64`
- репозиторий pull/clone выполняется через `go-git`

## Guardrails

- `TEST_MODE_NO_OTP=true` и `PRODUCTION=true` вместе запрещены
- `AEAD_KEY` должен быть hex-строкой длиной 32 байта после decode
