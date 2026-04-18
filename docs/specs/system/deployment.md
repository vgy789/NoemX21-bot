# Развертывание (VPS + Dokku + Caddy proxy plugin)

Целевая схема:

- `Dokku` остаётся платформой deploy/env/runtime.
- PostgreSQL поднимается через `dokku-postgres`.
- HTTPS и reverse proxy обслуживаются встроенной `Caddy`-интеграцией Dokku.
- Один app: `bot`.
- `Polling` и `webhook` переключаются конфигом одного и того же app.
- `/api/v1/webhook/register` не входит в публичный deployment-контракт и остаётся internal-only до переезда на `PostgREST`.

## 1. Подготовка сервера

> Предварительно включите 2FA в панели хостинга.

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install ufw fail2ban unattended-upgrades docker-compose-plugin -y
sudo systemctl enable --now fail2ban
sudo dpkg-reconfigure --priority=low unattended-upgrades
sudo apt clean && sudo apt autoremove -y

sudo ufw allow 2299/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

`docker-compose-plugin` нужен Caddy proxy plugin внутри Dokku. Отдельный `docker-compose.yml` для приложения не используется.

**`/etc/ssh/sshd_config`:**

```text
Port 2299
PasswordAuthentication no
PermitEmptyPasswords no
PermitRootLogin no
UsePAM no
AllowUsers user deploy
```

```bash
sudo systemctl restart ssh
```

## 2. Пользователи

```bash
# deploy — для CI/CD
sudo adduser deploy
echo 'deploy ALL=(ALL) NOPASSWD: /usr/bin/dokku *' | sudo tee /etc/sudoers.d/deploy
sudo chmod 440 /etc/sudoers.d/deploy

CURRENT_ADMIN_HOME="$(getent passwd "$USER" | cut -d: -f6)"
ssh-keygen -t ed25519 -f "$CURRENT_ADMIN_HOME/deploy_key" -N ""
sudo mkdir -p /home/deploy/.ssh
cat "$CURRENT_ADMIN_HOME/deploy_key.pub" | sudo tee -a /home/deploy/.ssh/authorized_keys
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys
sudo passwd -S deploy

# user — для ручного администрирования
sudo adduser user
sudo usermod -aG sudo user
sudo mkdir -p /home/user/.ssh
sudo cp /root/.ssh/authorized_keys /home/user/.ssh/
sudo chown -R user:user /home/user/.ssh
sudo chmod 700 /home/user/.ssh
sudo chmod 600 /home/user/.ssh/authorized_keys
```

Ожидаемый статус для `sudo passwd -S deploy` после создания: `P`, не `L`.

Если статус `L`, задайте пароль для разблокировки аккаунта:

```bash
sudo passwd deploy
```

Это не включает вход по паролю через SSH, если в `sshd_config` уже стоит `PasswordAuthentication no`.

## 3. Bootstrap Dokku, PostgreSQL и Caddy

```bash
wget -NP . https://dokku.com/bootstrap.sh
sudo DOKKU_TAG=v0.37.7 bash bootstrap.sh

cat ~/.ssh/authorized_keys | sudo dokku ssh-keys:add admin
sudo dokku plugin:install https://github.com/dokku/dokku-postgres.git --name postgres

sudo dokku apps:create bot
sudo dokku postgres:create bot-db
sudo dokku postgres:link bot-db bot

sudo timedatectl set-timezone Europe/Moscow
sudo dokku config:set bot TZ=Europe/Moscow
sudo dokku postgres:restart bot-db
```

Переключение proxy на Caddy:

```bash
sudo dokku proxy:set --global caddy
sudo dokku nginx:stop
sudo dokku caddy:start
sudo dokku caddy:set --global letsencrypt-email ops@example.com
```

Примечания:

- Эта схема рассчитана на single-host deployment, где `Dokku` и `Caddy` обслуживают один и тот же набор app.
- Не нужно поднимать внешний `Caddy` рядом с Dokku и не нужно использовать `Compose` внутри Dokku.

## 4. GitHub Container Registry

1. Создайте classic token с правом `read:packages`.
2. Авторизуйте Dokku-хост в `ghcr.io`:

```bash
sudo dokku registry:login --global ghcr.io <github-user> <classic-token>
```

## 5. CI/CD (GitHub Actions)

Workflow `.github/workflows/cd.yml` публикует image в GHCR и деплоит его через `dokku git:from-image`.

**Secrets**:

- `VPS_HOST`
- `VPS_USER`
- `VPS_SSH_KEY` = содержимое `$CURRENT_ADMIN_HOME/deploy_key`

**Variables**:

| Key | Value |
|---|---|
| `REGISTRY` | `ghcr.io` |
| `VPS_PORT` | `2299` |
| `DOKKU_APP_NAME` | `bot` |

Опционально, только после того как:

- приватный ключ уже сохранён в GitHub Secret `VPS_SSH_KEY`;
- вы проверили ручной вход `ssh -i "$CURRENT_ADMIN_HOME/deploy_key" -p 2299 deploy@<VPS_HOST>`.

Тогда локальные файлы ключа можно удалить:

```bash
rm -f "$CURRENT_ADMIN_HOME/deploy_key" "$CURRENT_ADMIN_HOME/deploy_key.pub"
```

Mode-specific domains, ports и webhook env настраиваются на Dokku-хосте и не зашиваются в workflow.

## 6. Переменные окружения

### Обязательные

| Variable | Назначение |
|---|---|
| `DATABASE_URL` | PostgreSQL DSN |
| `TELEGRAM_BOT_TOKEN` | токен Telegram-бота |
| `ROCKETCHAT_API_URL` | base URL Rocket.Chat API |
| `ROCKETCHAT_USER_ID` | service user id для API |
| `ROCKETCHAT_AUTH_TOKEN` | service auth token |
| `SCHOOL21_API_URL` | base URL School 21 API |
| `SCHOOL21_USER_LOGIN` | login технического аккаунта School 21 |
| `SCHOOL21_USER_PASSWORD` | пароль технического аккаунта |
| `AEAD_KEY` | 32-byte key encoded as hex |

### Общие режимные

| Variable | Default | Назначение |
|---|---|---|
| `PRODUCTION` | `false` | production mode |
| `LOG_LEVEL` | `debug` | уровень логирования |
| `API_SERVER_PORT` | `8081` | internal HTTP runtime port; не публиковать через Dokku/Caddy |
| `TEST_MODE_NO_OTP` | `false` | mock OTP provider; не включать в production |

### Telegram webhook mode

| Variable | Default | Назначение |
|---|---|---|
| `TELEGRAM_WEBHOOK_ENABLED` | `false` | включить webhook mode |
| `TELEGRAM_WEBHOOK_URL` | - | публичный webhook URL |
| `TELEGRAM_WEBHOOK_PATH` | `/telegram/webhook` | путь webhook |
| `TELEGRAM_WEBHOOK_PORT` | `8080` | локальный listener gotgbot |
| `TELEGRAM_WEBHOOK_SECRET` | - | secret token для webhook |

### Telegram polling mode

| Variable | Default | Назначение |
|---|---|---|
| `POLLING_TIMEOUT` | `9` | timeout long polling |
| `REQUEST_TIMEOUT` | `25s` | timeout HTTP запроса к Telegram |
| `MAX_ROUTINES` | `0` | max routines in dispatcher |
| `DROP_PENDING_UPDATES` | `true` | drop backlog on start |

### Git sync

| Variable | Default | Назначение |
|---|---|---|
| `GIT_REPO_URL` | - | SSH URL git-репозитория |
| `SSH_KEY_BASE64` | - | private key для git |
| `GIT_BRANCH` | `main` | branch |
| `GIT_SYNC_INTERVAL` | `5m` | interval |
| `GIT_LOCAL_PATH` | `data` | local checkout path |
| `GIT_CAMPUSES_PATH` | `campuses` | путь к campus content |
| `GIT_VARIOUS_PATH` | `bot_content/various` | путь к каталогу `various` |

### Generated assets

| Variable | Default | Назначение |
|---|---|---|
| `SCHEDULE_IMAGES_ENABLED` | `true` | генерация расписаний |
| `SCHEDULE_IMAGES_INTERVAL` | `5m` | период генерации |
| `SCHEDULE_IMAGES_TEMP_DIR` | `tmp/schedules` | путь для PNG расписаний |
| `CHART_TEMP_DIR` | `tmp/skills_radar` | путь для chart artifacts |

Базовая настройка:

```bash
sudo dokku config:set bot \
  PRODUCTION=true \
  TELEGRAM_BOT_TOKEN=... \
  ROCKETCHAT_API_URL=... \
  ROCKETCHAT_USER_ID=... \
  ROCKETCHAT_AUTH_TOKEN=... \
  SCHOOL21_API_URL=... \
  SCHOOL21_USER_LOGIN=... \
  SCHOOL21_USER_PASSWORD=... \
  AEAD_KEY=... \
  API_SERVER_PORT=8081
```

## 7. Переключение режимов

### Polling mode

Polling mode не должен иметь публичный ingress.

```bash
sudo dokku checks:disable bot web || true
sudo dokku config:set bot TELEGRAM_WEBHOOK_ENABLED=false
sudo dokku config:unset bot TELEGRAM_WEBHOOK_URL TELEGRAM_WEBHOOK_SECRET || true
sudo dokku domains:disable bot
sudo dokku ports:clear bot
```

Правила:

- бот получает обновления через long polling;
- для polling deploy checks лучше держать выключенными, иначе Dokku может кратко держать старый и новый контейнеры одновременно;
- `API_SERVER_PORT=8081` остаётся внутренним портом процесса;
- приложение дополнительно удерживает PostgreSQL advisory lock, поэтому даже при overlap только один контейнер может выполнять `getUpdates`;
- не используйте `dokku proxy:disable bot`: это обходит proxy-слой Dokku и может открыть app на случайном host port.

### Webhook mode

Webhook mode публикует только listener gotgbot на `8080`.

```bash
BOT_DOMAIN=bot.example.com
WEBHOOK_SECRET="$(openssl rand -hex 32)"

sudo dokku checks:enable bot web
sudo dokku ps:scale bot web=1
sudo dokku domains:enable bot
sudo dokku domains:set bot "$BOT_DOMAIN"
sudo dokku ports:set bot http:80:8080 https:443:8080
sudo dokku config:set bot \
  TELEGRAM_WEBHOOK_ENABLED=true \
  TELEGRAM_WEBHOOK_URL=https://$BOT_DOMAIN/telegram/webhook \
  TELEGRAM_WEBHOOK_PATH=/telegram/webhook \
  TELEGRAM_WEBHOOK_PORT=8080 \
  TELEGRAM_WEBHOOK_SECRET="$WEBHOOK_SECRET"
sudo dokku ps:restart bot
```

Проверка:

```bash
dig +short A "$BOT_DOMAIN"
sudo dokku proxy:report bot
sudo dokku domains:report bot
sudo dokku ports:list bot
curl -I "https://$BOT_DOMAIN/telegram/webhook"
TOKEN="$(sudo dokku config:get bot TELEGRAM_BOT_TOKEN)"
curl -s "https://api.telegram.org/bot$TOKEN/getWebhookInfo"
sudo dokku logs bot -t -p web
```

Должно получиться:

- `A`-запись домена указывает на IPv4 Dokku/VPS;
- `AAAA` не обязателен;
- публичный маршрут: `https://<bot-domain>/telegram/webhook`;
- `80/443` проксируются на `8080`;
- `8081` не попадает в `Dokku` port mapping.
- `getWebhookInfo` возвращает `url=https://<bot-domain>/telegram/webhook`.
- в логах приложения появляются `webhook set successfully` и `webhook server started`.

Примечания:

- `TELEGRAM_WEBHOOK_SECRET` не выдаётся Telegram, его нужно сгенерировать самостоятельно.
- Бот сам вызывает `SetWebhook` при старте в webhook mode, отдельный ручной `setWebhook` не нужен.
- Старые polling env (`POLLING_TIMEOUT`, `REQUEST_TIMEOUT`, `MAX_ROUTINES`, `DROP_PENDING_UPDATES`) можно не удалять: в webhook mode они просто не используются.
- Если для polling mode ранее выполнялся `dokku checks:disable bot web`, перед переходом на webhook его нужно вернуть через `dokku checks:enable bot web`.

## 8. Operational Notes

- Приложение всегда поднимает внутренний HTTP server на `API_SERVER_PORT=8081`.
- В webhook mode дополнительно стартует отдельный listener gotgbot на `TELEGRAM_WEBHOOK_PORT=8080`.
- В текущем коде webhook handler также регистрируется на внутреннем mux, поэтому `8081` тем более нельзя публиковать наружу.
- `TEST_MODE_NO_OTP=true` несовместим с `PRODUCTION=true`.
- `/api/v1/webhook/register` считается временным internal endpoint до выноса в `PostgREST`.

## 9. Логи Docker

**`/etc/docker/daemon.json`:**

```json
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
```

```bash
sudo systemctl restart docker
```
