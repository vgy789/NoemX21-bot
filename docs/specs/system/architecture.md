# Архитектура NoemX21-bot

Системная сводка по устройству приложения и связям между подсистемами.

## Стек

```yaml
language: "Go 1.26.2"
database: "PostgreSQL (pgx/v5, sqlc)"
telegram_sdk: "gotgbot/v2"
http_server: "net/http"
config: "caarlos0/env"
crypto: "tink-go/v2 aead/subtle (AES-256-GCM)"
scheduler: "robfig/cron/v3"
sync: "golang.org/x/sync"
git: "go-git/v5"
```

## Runtime Model

Приложение запускается как один Go-процесс, внутри которого живут:

- Telegram transport (`polling` или `webhook`)
- HTTP server для временного internal endpoint
- FSM engine и action modules
- фоновые сервисы `gitsync`, `campus`, `schedule_generator`
- PostgreSQL repository layer

Отдельных runtime-контейнеров для API и worker сейчас нет: это единый процесс. В deployment-схеме `Dokku + Caddy` наружу публикуется только Telegram webhook.

## Project Layout

```
cmd/noemx21-bot/        # Entry point, CLI flags for migrations
internal/
  app/                  # Сборка и запуск runtime-компонентов
  transport/
    telegram/           # Long polling / webhook integration
    http/               # Internal HTTP endpoint(s)
  fsm/                  # Flow parser, engine, state storage
  service/              # Domain services
  sync/gitsync/         # Git-based content sync
  database/             # Migrations, sqlc repository
  clients/              # School 21 / Rocket.Chat / Telegram clients
  config/               # Env-parsed configuration
  crypto/               # AES-256-GCM crypter
  initialization/       # Bootstrap and dependency wiring
  pkg/                  # Shared packages (imgcache, charts, schedule, retry)
docs/
  c4/                   # Source PlantUML C4 diagrams
  specs/flows/          # YAML flows for FSM
  specs/system/         # System specifications
```

## Core Flows

### Telegram update

```text
Telegram Bot API
  -> transport/telegram
  -> fsm/engine
  -> fsm/actions/*
  -> service / database
  -> Telegram response
  -> fsm/storage
```

### Internal HTTP request

```text
Internal client
  -> transport/http
  -> ApiKeyService
  -> repository lookup
  -> JSON response
```

### Background execution

```text
app.Run
  -> gitsync.Start()
  -> campus.Start()
  -> schedule_generator.Start()
  -> httpServer.Start()
  -> telegram.Run() or telegram.RunWebhook()
```

## Key Subsystems

| Subsystem | Responsibility |
|---|---|
| `transport/telegram` | Приём updates, callback handling, отправка сообщений и фото |
| `fsm` | Загрузка YAML flows, переходы состояний, state persistence |
| `service/otp` | Генерация и проверка OTP, доставка через Rocket.Chat или Email (SMTP) |
| `service/credentials` | Шифрование School 21 credentials и получение access token |
| `service/campus` | Синхронизация кампусов и cleanup review requests |
| `sync/gitsync` | Pull/clone git-контента и синхронизация YAML в БД |
| `service/schedule_generator` | Генерация PNG расписаний и cache invalidation |
| `transport/http` | `POST /api/v1/webhook/register` (internal-only) |

## Related Specs

- [`config.md`](./config.md) — конфигурация и режимы запуска
- [`runtime.md`](./runtime.md) — boot sequence и фоновые сервисы
- [`api.md`](./api.md) — internal HTTP API
- [`security.md`](./security.md) — безопасность и реальные ограничения
- [`deployment.md`](./deployment.md) — развёртывание
- [`fsm-syntax.md`](./fsm-syntax.md) — синтаксис YAML flows

## C4 Диаграммы

Исходники лежат в `docs/c4/*.puml`, генерация:

```bash
make docs-diagrams
```

Артефакты:

- [System Context](./c4/c4_context.svg)
- [Container](./c4/c4_container.svg)
- [Bot Components](./c4/c4_component_bot.svg)
- [API Components](./c4/c4_component_api.svg)
- [Scheduler Components](./c4/c4_component-scheduler.svg)

## Диаграмма БД

- [ER Diagram](../../schema.svg)
