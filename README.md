# NoemX21 Bot

![Go Version](https://img.shields.io/github/go-mod/go-version/vgy789/noemx21-bot?logo=go)
[![CI](https://github.com/vgy789/noemx21-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/vgy789/noemx21-bot/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](./LICENSE)

Telegram-бот для School 21: регистрация с OTP, просмотр статистики, peer review, синхронизация контента кампусов и временный internal HTTP endpoint для интеграций.

## Возможности

- FSM-диалоги на YAML
- OTP через Rocket.Chat или Email, плюс авторизация по Rocket.Chat токену
- Профиль и статистика School 21
- Review requests
- Git-синхронизация контента кампусов и каталога проектов

## Быстрый старт

1. Установите Go `1.26.2`.
2. Создайте `env/.env` по [`docs/specs/system/config.md`](./docs/specs/system/config.md).
3. Соберите проект:

```bash
make deps
make build
```

4. Запустите приложение:

```bash
./noemx21-bot
```

Миграции сейчас выполняются при обычном старте приложения. Явные флаги остаются для служебных сценариев:

```bash
./noemx21-bot -migrate
./noemx21-bot -migrate-status
./noemx21-bot -migrate-rollback
```

## Разработка

```bash
make test
make lint
make vet
make ci-check
```

Генерация PlantUML-диаграмм:

```bash
make docs-diagrams
```

## Документация

- Системные спецификации: [`docs/specs/system/`](./docs/specs/system/)
- FSM flow-спеки: [`docs/specs/flows/`](./docs/specs/flows/)
- Исходники C4: [`docs/c4/`](./docs/c4/)
- ER-диаграмма БД: [`docs/schema.svg`](./docs/schema.svg)

С чего начать:

- [`docs/specs/system/architecture.md`](./docs/specs/system/architecture.md)
- [`docs/specs/system/config.md`](./docs/specs/system/config.md)
- [`docs/specs/system/deployment.md`](./docs/specs/system/deployment.md)
- [`docs/specs/system/runtime.md`](./docs/specs/system/runtime.md)
- [`docs/specs/system/api.md`](./docs/specs/system/api.md)

## Лицензия

[MIT](./LICENSE)
