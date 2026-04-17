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

## Optional без default

| Variable | Подсистема |
|---|---|
| `TELEGRAM_API_URL` | Telegram |
| `TELEGRAM_WEBHOOK_URL` | Telegram webhook |
| `TELEGRAM_WEBHOOK_SECRET` | Telegram webhook |
| `GIT_REPO_URL` | Git sync |
| `SSH_KEY_BASE64` | Git sync |

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
| `OTP_EMAIL_ENABLED` | `false` |
| `OTP_EMAIL_SMTP_PORT` | `587` |
| `OTP_EMAIL_SMTP_TIMEOUT` | `20s` |
| `OTP_EXPIRES_IN` | `5m` |
| `OTP_EMAIL_SUBJECT` | `NOEMX21-BOT \| Verification code` |
| `OTP_EMAIL_TEMPLATE_PATH` | `internal/service/templates/otp_email.html.tmpl` |

## Optional для Email OTP (без default)

Используются только если `OTP_EMAIL_ENABLED=true`.

| Variable | Подсистема |
|---|---|
| `OTP_EMAIL_SMTP_HOST` | SMTP |
| `OTP_EMAIL_SMTP_USERNAME` | SMTP |
| `OTP_EMAIL_SMTP_PASSWORD` | SMTP |
| `OTP_EMAIL_FROM` | SMTP |
| `OTP_EMAIL_TEST_TO` | SMTP (override recipient for testing) |

## Deployment note

- `API_SERVER_PORT` — внутренний HTTP runtime порт.
- В схеме `Dokku + Caddy` этот порт не должен проксироваться наружу.
- В webhook mode публичный ingress должен идти только на `TELEGRAM_WEBHOOK_PORT`.

## Режимы Telegram

### Polling

- `TELEGRAM_WEBHOOK_ENABLED=false`
- используются `POLLING_TIMEOUT`, `REQUEST_TIMEOUT`, `MAX_ROUTINES`, `DROP_PENDING_UPDATES`
- в Dokku deployment обычно сопровождается `domains:disable` и `ports:clear`

### Webhook

- `TELEGRAM_WEBHOOK_ENABLED=true`
- обязательно задать `TELEGRAM_WEBHOOK_URL`
- опционально `TELEGRAM_WEBHOOK_SECRET`
- `TELEGRAM_WEBHOOK_SECRET` рекомендуется генерировать локально, например `openssl rand -hex 32`
- listener gotgbot стартует на `TELEGRAM_WEBHOOK_PORT`
- в Dokku deployment публикуется только `TELEGRAM_WEBHOOK_PORT=8080` через `ports:set ... 80:8080 443:8080`
- достаточно `A`-записи домена на IPv4 адрес сервера; `AAAA` необязателен

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
