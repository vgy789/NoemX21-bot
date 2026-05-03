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
- Для режима Telegram join requests объект `ChatJoinRequest` содержит `user_chat_id`. Если автоматический фейс-контроль отклоняет заявку из-за отсутствия регистрации, статуса или фильтров кампуса/трайба, бот сначала отправляет пользователю личное сообщение на `user_chat_id`, затем вызывает `declineChatJoinRequest`.
- `user_chat_id` доступен ограниченно: Telegram разрешает писать по нему примерно 5 минут и только до обработки заявки, если другой администратор уже не связался с пользователем. Для пользователей, которые уже вошли в группу без заявки и потом были удалены/забанены, бот не сможет первым написать в личку, если пользователь ранее не запускал бота.

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
