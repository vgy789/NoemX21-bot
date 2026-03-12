# External API

Публичный read-only API теперь строится через `PostgREST` поверх схемы `api_v1`.

Контур разделён:

- `bot` публикует только `https://<bot-domain>/telegram/webhook`;
- `bot-api` публикует только `https://<api-domain>/...`.

## Auth

### 1. Long-lived API key

- Формат: `noemx_sk_<64 hex>`
- В БД хранится только SHA-256 hash
- Типы ключей:
  - `personal` — выдаётся пользователю через `Настройки -> API`
  - `service` — создаётся отдельно админом через `api_private.create_service_key(...)`

### 2. JWT exchange

`PostgREST` принимает короткий Bearer token.

Поэтому внешний клиент сначала вызывает:

### `POST /rpc/exchange_api_key`

#### Request

```json
{
  "api_key": "noemx_sk_..."
}
```

#### Response 200

```json
{
  "access_token": "<jwt>",
  "token_type": "Bearer",
  "expires_in": 3600,
  "principal_id": 42,
  "principal_kind": "personal",
  "scopes": ["self.read"],
  "campus_id": null
}
```

## Public Interfaces

### `POST /rpc/check_registration`

Требует scope `registration.check`.

#### Request

```json
{
  "external_id": "123456789",
  "login": "student_login",
  "platform": "telegram"
}
```

#### Response 200

```json
{
  "registered": true
}
```

### `GET /me_book_loans`

Персональный feed по книгам текущего пользователя.

Поля:

- `campus_id`
- `campus_short_name`
- `book_id`
- `book_title`
- `book_author`
- `borrowed_at`
- `due_at`
- `returned_at`

### `GET /me_room_bookings`

Персональный feed по переговоркам текущего пользователя.

Поля:

- `campus_id`
- `campus_short_name`
- `room_id`
- `room_name`
- `booking_date`
- `start_time`
- `duration_minutes`
- `created_at`

### `GET /campus_book_loans`

Campus-wide feed с логинами. Доступен только `service` principal с:

- `campus.logins.read`
- `allow_login_exposure=true`

### `GET /campus_room_bookings`

Campus-wide feed по бронированиям переговорок с `s21_login`.

### `GET /campus_book_loan_daily_stats`

Дневные агрегаты:

- `campus_id`
- `campus_short_name`
- `stat_date`
- `loans_started`
- `loans_returned`
- `unique_users`

### `GET /campus_room_booking_daily_stats`

Дневные агрегаты:

- `campus_id`
- `campus_short_name`
- `stat_date`
- `booking_count`
- `unique_users`
- `unique_rooms`
- `total_duration_minutes`

## Authorization Model

- `api_anon` имеет доступ только к `rpc/exchange_api_key`
- `api_user` работает через JWT и `db-pre-request=api_private.pre_request`
- login role из `db-uri` должна быть member ролей `api_anon` и `api_user`, чтобы `PostgREST` мог применять `SET ROLE`
- Источник истины по scope/campus — `api_principals`, а не клиентские параметры
- `personal` principal видит только свои записи
- `service` principal жёстко ограничен одним `campus_id`

## Transport

- `PostgREST` app: отдельный Dokku app `bot-api`
- exposed schema: `api_v1`
- private helpers: `api_private`
- пример конфига: [postgrest.conf.example](/home/school/qq/noemx21-bot/deploy/postgrest/postgrest.conf.example)
