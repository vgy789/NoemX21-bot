# Internal HTTP API

Текущий HTTP API минимален и состоит из одного endpoint.

Этот API не входит в публичный deployment-контракт `Dokku + Caddy`:

- в production он должен оставаться internal-only;
- наружу публикуется только Telegram webhook;
- в будущем этот сценарий планируется вынести в `PostgREST`.

## Auth

- Заголовок: `X-Secret`
- Проверка: lookup по SHA-256 hash API key
- При отсутствии или невалидном ключе: `401 Unauthorized`

## Endpoint

### `POST /api/v1/webhook/register`

Проверяет, привязан ли `external_id` к указанному School 21 login.

#### Request

```json
{
  "external_id": "123456789",
  "login": "student_login"
}
```

#### Semantics

- платформа сейчас жёстко считается `telegram`
- поиск выполняется по `user_accounts`
- сравнение login выполняется case-insensitive

#### Response 200

```json
{
  "registered": true
}
```

или

```json
{
  "registered": false
}
```

#### Error responses

| Status | Причина |
|---|---|
| `400` | invalid JSON |
| `401` | missing or invalid `X-Secret` |
| `405` | method is not `POST` |
| `500` | internal DB/auth error |

## Transport

- server: `net/http`
- default port: `API_SERVER_PORT=8081`
- request/response format: JSON
- не публиковать напрямую через Dokku/Caddy
