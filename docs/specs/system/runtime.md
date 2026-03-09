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
8. Запускаются background services, HTTP server и Telegram mode.

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

## HTTP Runtime

- server стартует всегда
- default port: `API_SERVER_PORT=8081`
- текущий endpoint: `POST /api/v1/webhook/register`

## Telegram Runtime

### Polling mode

- `telegram.Run(ctx)`
- получает updates через long polling

### Webhook mode

- `telegram.RunWebhook(ctx)`
- вызывает `SetWebhook`
- поднимает listener gotgbot на `TELEGRAM_WEBHOOK_PORT`
- одновременно `App` также держит общий HTTP server

## Shutdown

- `app.Run` использует `errgroup`
- при отмене контекста вызывается `Stop()` у background services, которые его реализуют
- HTTP server останавливается через graceful shutdown
