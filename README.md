[![Typing SVG](https://readme-typing-svg.demolab.com?font=Fira+Code&size=24&duration=4500&pause=1000&color=1F2328&vCenter=true&random=true&height=25&lines=%E2%A4%87%D0%92%D1%8B%D0%B1%D0%B5%D1%80%D0%B8+%D0%BA%D0%BB%D1%83%D0%B1+%D0%BF%D0%BE+%D0%B8%D0%BD%D1%82%D0%B5%D1%80%D0%B5%D1%81%D0%B0%D0%BC;%E2%A4%87%D0%A1%D1%80%D0%B0%D0%B2%D0%BD%D0%B8%D0%B2%D0%B0%D0%B9+%D1%81%D0%BA%D0%B8%D0%BB%D1%8B;%E2%A4%87%D0%9D%D0%B0%D0%B9%D0%B4%D0%B8+%D0%BF%D0%B8%D1%80%D0%B0+%D0%B4%D0%BB%D1%8F+peer+review;%E2%A4%87%D0%91%D0%B5%D1%80%D0%B8+%D0%BA%D0%BD%D0%B8%D0%B3%D0%B8+%D0%B8%D0%B7+%D0%B1%D0%B8%D0%B1%D0%BB%D0%B8%D0%BE%D1%82%D0%B5%D0%BA%D0%B8;%E2%A4%87%D0%91%D1%80%D0%BE%D0%BD%D0%B8%D1%80%D1%83%D0%B9+%D0%BF%D0%B5%D1%80%D0%B5%D0%B3%D0%BE%D0%B2%D0%BE%D1%80%D0%BA%D0%B8)](https://git.io/typing-svg)

# NoemX21 Bot

![Go Version](https://img.shields.io/github/go-mod/go-version/vgy789/noemx21-bot?logo=go)
[![CI](https://github.com/vgy789/noemx21-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/vgy789/noemx21-bot/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](./LICENSE)

Telegram-бот для School 21: регистрация с OTP, просмотр статистики, peer review, бронирование переговорок и книг, синхронизация контента кампусов и внешний read-only API через `PostgREST`.

## Возможности

- FSM-диалоги на YAML
- OTP через Rocket.Chat
- Профиль и статистика School 21
- Бронирование переговорок и расписания
- Сценарии библиотеки и review requests
- Git-синхронизация контента кампусов и каталога проектов
- Внешний read-only API через `PostgREST`

## Быстрый старт

1. Установите Go `1.26.1`.
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
