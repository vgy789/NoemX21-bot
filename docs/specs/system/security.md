# Безопасность и Ограничения

Документ описывает фактические ограничения и защитные меры, реализованные в текущем коде.

## Внутренние ограничения

### OTP resend cooldown

- Источник: `auth_verification_codes`
- Ограничение: не чаще 1 нового OTP в 60 секунд на `s21_login`
- Поведение: при раннем повторе возвращается `RATE_LIMIT:{seconds_left}`

### Registration attempt limiter

- Источник: in-memory limiter
- Область: попытки OTP/регистрации на уровне Telegram user id
- Порог: 6 неуспешных попыток
- Наказание: блокировка на 24 часа
- Сброс: после успешной верификации OTP

### HTTP API auth

- Internal endpoint требует заголовок `X-Secret`
- Значение проверяется через `ApiKeyService`
- В БД хранится только SHA-256 hash ключа, не raw secret
- Endpoint не должен публиковаться напрямую через `Dokku`/`Caddy`

## Внешние интеграции

### School 21 API

- Используется для auth, профиля, skills, coalition, feedback, campuses, participant projects
- Сетевые ошибки и retryable HTTP-статусы обрабатываются через `netretry`
- `CredentialService` хранит access token и при необходимости переаутентифицируется

### Rocket.Chat API

- Используется для `users.info` и отправки OTP в direct message
- Для token-based auth используется `GET /me` с пользовательскими `X-User-Id` + `X-Auth-Token`
- Сетевые ошибки и retryable HTTP-статусы обрабатываются через `netretry`

### SMTP (Email OTP)

- Используется для отправки OTP на адрес вида `%login%@student.21-school.ru`
- Тело письма берется из внешнего HTML-шаблона (`OTP_EMAIL_TEMPLATE_PATH`)
- SMTP-доступ задается через env (`OTP_EMAIL_SMTP_*`)

### Telegram Bot API

- Используется через `gotgbot/v2`
- Режимы запуска: polling или webhook
- Group Manager retired. Новые группы получают только PRR-радар и радар групповых проектов.
- Defender, cleanup, JSON import UI, global member-tag scan и group member tag discovery отключены.
- Существующие legacy owners могут только выключить старые DB-флаги Defender; включение фейс-контроля и запуск проверок остаются no-op.
- `/reset` доступен только фактическому Telegram-владельцу текущей группы. Команда сбрасывает настройки бота к radar-only режиму и не удаляет участников, сообщения или `telegram_group_legacy_access`.
- На startup, join, join request, message, import и saved callback запрещены автоматические `banChatMember`, kick, decline и массовые member actions, даже если старые DB-флаги Defender включены.
- Миграция retirement-среза выставляет `defender_enabled=false`, `defender_remove_blocked=false` и `defender_recheck_known_members=false` для всех групп.
- Legacy-доступ хранится в `telegram_group_legacy_access` как snapshot текущих owners/moderators на момент миграции; новые owners/moderators не получают privileged Group Manager автоматически.
- Ручные `/ban` и `/kick` доступны только frozen legacy users с `can_ban/full_access` и ограничены 3 действиями на группу за 1 час на одного legacy admin.
- Автоприветствие выключено по умолчанию. При включении оно публикует Telegram-имя, username и учебный ник, но не уровень или кампус, и планируется к удалению через 30 дней.
- Очередь удаления хранит только `chat_id`, `message_id`, срок и retry state. Rendered text, Telegram-имя, username и учебный ник не сохраняются и не логируются этой очередью.

## Хранение и кэш

### In-memory

- cache распарсенных FSM flows
- image byte cache для расписаний (`imgcache`)
- cache Telegram `file_id` для уже отправленных schedule images
- in-memory limiter попыток регистрации

### PostgreSQL

- `platform_credentials`: зашифрованные School 21 credentials и access token
- `auth_verification_codes`: одноразовые OTP-коды с TTL 5 минут
- `participant_stats_cache`: кэш статистики участников
- остальные доменные данные: users, accounts, bookings, reviews, catalogs

Redis в текущей реализации не используется.

## Шифрование

- Алгоритм: AES-256-GCM
- Ключ: один `AEAD_KEY`, 32 байта в hex
- Реализация: `tink-go/v2/aead/subtle`
- Область применения: чувствительные поля в БД (`platform_credentials`)
- Additional Authenticated Data: используется login пользователя

## OTP

- Генерируется как случайный 6-значный код
- Срок жизни: 5 минут
- Перед созданием нового OTP старые коды для этого login удаляются
- После успешной проверки использованный код удаляется
- Канал доставки выбирается в меню регистрации (`Rocket.Chat` или `Email`)

## Защитные меры

- Секреты редактируются в логах через `config.RedactString`
- При `TEST_MODE_NO_OTP=true` и `PRODUCTION=true` приложение завершает запуск
- Raw API keys не сохраняются, только hash
- School 21 credentials не логируются
- В production webhook ingress должен идти только на `TELEGRAM_WEBHOOK_PORT=8080`

## Известные границы

- OTP-коды сейчас хранятся в PostgreSQL, а не только в RAM
- Регистрационный limiter сейчас не `Token Bucket`, а счётчик попыток с временной блокировкой
- Централизованного distributed rate limiter нет
