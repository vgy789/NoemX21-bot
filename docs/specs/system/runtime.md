# Runtime

Документ описывает boot sequence и фоновые сервисы.

## Boot Sequence

1. Загружается env-конфиг.
2. Инициализируется logger.
3. Создаётся подключение к PostgreSQL.
4. Создаются Rocket.Chat и School 21 клиенты.
5. Если задан `AEAD_KEY`, инициализируется crypter и `CredentialService`.
6. Выполняется seed/verify initial School 21 credentials.
7. Собирается `App` со всеми transport/service компонентами.
8. Запускаются background services и Telegram mode.

## Background Services

### GitSync

- стартует сразу при запуске
- interval: `GIT_SYNC_INTERVAL` (`5m` по умолчанию)
- initial sync выполняется сразу в goroutine
- задачи:
  - clone/fetch git repo
  - sync projects catalog
  - sync campus content from filesystem

### CampusService

- initial campus update выполняется сразу
- `@weekly`: обновление списка кампусов из School 21 API
- `@every 60m`: cleanup устаревших review requests

### Schedule Generator

- включается через `SCHEDULE_IMAGES_ENABLED`
- interval: `SCHEDULE_IMAGES_INTERVAL` (`5m` по умолчанию)
- initial generation выполняется сразу
- по событиям бронирования может запускаться `ForceRegenerate()`

## Telegram Runtime

### Polling mode

- `telegram.Run(ctx)`
- получает updates через long polling
- в production у app нет публичного домена и нет port mapping на `80/443`

### Webhook mode

- `telegram.RunWebhook(ctx)`
- приложение само вызывает `SetWebhook` на `TELEGRAM_WEBHOOK_URL`
- поднимает listener gotgbot на `TELEGRAM_WEBHOOK_PORT`
- production ingress должен идти на `TELEGRAM_WEBHOOK_PORT=8080`
- polling env могут оставаться в конфиге, но в этом режиме не используются

## Deployment Coupling

### Polling deployment

- `dokku domains:disable bot`
- `dokku ports:clear bot`
- `TELEGRAM_WEBHOOK_ENABLED=false`

### Webhook deployment

- у домена достаточно корректной `A`-записи на IPv4 сервера; `AAAA` опционален
- если для polling отключались checks, их нужно вернуть через `dokku checks:enable bot web`
- `dokku ps:scale bot web=1`
- `dokku domains:enable bot`
- `dokku domains:set bot <bot-domain>`
- `dokku ports:set bot http:80:8080 https:443:8080`
- `TELEGRAM_WEBHOOK_ENABLED=true`
- `TELEGRAM_WEBHOOK_URL=https://<bot-domain>/telegram/webhook`
- `TELEGRAM_WEBHOOK_SECRET=<random hex>`
- верификация через `getWebhookInfo` должна показать `url=https://<bot-domain>/telegram/webhook`

### External API deployment

- `PostgREST` поднимается отдельным app `bot-api`
- публикуется отдельный домен вида `https://api.<domain>`
- использует ту же PostgreSQL базу, но только схемы `api_v1` и `api_private`
- использует `db-pre-request=api_private.pre_request`

## Shutdown

- `app.Run` использует `errgroup`
- при отмене контекста вызывается `Stop()` у background services, которые его реализуют
