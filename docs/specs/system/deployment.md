# Развертывание (VPS + Dokku)

## 1. Подготовка сервера

> Предварительно включите 2FA в панели хостинга.

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install ufw fail2ban unattended-upgrades -y
sudo systemctl enable --now fail2ban
sudo dpkg-reconfigure --priority=low unattended-upgrades
sudo apt clean && sudo apt autoremove -y

ufw allow 2299/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable
```

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
systemctl restart ssh
```

## 2. Пользователи

```bash
# deploy — для CI/CD
sudo groupadd -f deploy
sudo useradd -r -g deploy -s /bin/bash -m deploy
echo 'deploy ALL=(ALL) NOPASSWD: /usr/bin/dokku *' | sudo tee /etc/sudoers.d/deploy
sudo chmod 440 /etc/sudoers.d/deploy

ssh-keygen -t ed25519 -f ~/deploy_key -N ""
sudo mkdir -p /home/deploy/.ssh
cat ~/deploy_key.pub | sudo tee -a /home/deploy/.ssh/authorized_keys
sudo chown -R deploy:deploy /home/deploy/.ssh
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys

# user — для ручного администрирования
sudo adduser user
usermod -aG sudo user
mkdir -p /home/user/.ssh
sudo cp /root/.ssh/authorized_keys /home/user/.ssh/
sudo chown -R user:user /home/user/.ssh
sudo chmod 700 /home/user/.ssh
sudo chmod 600 /home/user/.ssh/authorized_keys
```

## 3. Dokku + PostgreSQL

> Предварительно настройте DNS (`noemx.ru` и/или `bot.noemx.ru` -> IP VPS).

```bash
wget -NP . https://dokku.com/bootstrap.sh
sudo DOKKU_TAG=v0.37.6 bash bootstrap.sh
dokku domains:set-global noemx.ru
cat ~/.ssh/authorized_keys | sudo dokku ssh-keys:add admin
dokku apps:create bot

sudo dokku plugin:install https://github.com/dokku/dokku-postgres.git --name postgres
dokku postgres:create bot-db
dokku postgres:link bot-db bot

sudo timedatectl set-timezone Europe/Moscow
sudo dokku config:set bot TZ=Europe/Moscow
sudo dokku postgres:set bot-db env-set TZ Europe/Moscow
sudo dokku postgres:restart bot-db
```

## 4. CI/CD (GitHub Actions)

**Secrets**:

- `VPS_SSH_KEY` = содержимое `~/deploy_key`

**Variables**:

| Key | Value |
|---|---|
| `REGISTRY` | `ghcr.io` |
| `DOKKU_DOMAIN` | `bot.noemx.ru` |
| `VPS_HOST` | `185.76.242.15` |
| `VPS_USER` | `deploy` |
| `VPS_PORT` | `2299` |
| `DOKKU_APP_NAME` | `bot` |

```bash
rm -rf ~/deploy_key ~/deploy_key.pub
```

## 5. GitHub Container Registry

1. Создать classic token с правом `read:packages`.
2. Авторизовать Dokku:

```bash
dokku registry:login --global ghcr.io vgy789 <CLASSIC_TOKEN>
```

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

### Часто используемые режимные

| Variable | Default | Назначение |
|---|---|---|
| `PRODUCTION` | `false` | production mode |
| `LOG_LEVEL` | `debug` | уровень логирования |
| `API_SERVER_PORT` | `8081` | HTTP server port |
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
| `GIT_PROJECTS_PATH` | `bot_content/various/projects.yaml` | путь к каталогу проектов |

### Generated assets

| Variable | Default | Назначение |
|---|---|---|
| `SCHEDULE_IMAGES_ENABLED` | `true` | генерация расписаний |
| `SCHEDULE_IMAGES_INTERVAL` | `5m` | период генерации |
| `SCHEDULE_IMAGES_TEMP_DIR` | `tmp/schedules` | путь для PNG расписаний |
| `CHART_TEMP_DIR` | `tmp/skills_radar` | путь для chart artifacts |

Пример:

```bash
dokku config:set bot \
  DATABASE_URL=... \
  TELEGRAM_BOT_TOKEN=... \
  ROCKETCHAT_API_URL=... \
  ROCKETCHAT_USER_ID=... \
  ROCKETCHAT_AUTH_TOKEN=... \
  SCHOOL21_API_URL=... \
  SCHOOL21_USER_LOGIN=... \
  SCHOOL21_USER_PASSWORD=... \
  AEAD_KEY=...
```

## 7. Operational Notes

- В polling mode достаточно `API_SERVER_PORT` для HTTP API.
- В webhook mode текущая реализация поднимает:
  - `API_SERVER_PORT` для публичного HTTP API
  - `TELEGRAM_WEBHOOK_PORT` для webhook listener gotgbot
- `TEST_MODE_NO_OTP=true` несовместим с `PRODUCTION=true`

## 8. Логи Docker

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
systemctl restart docker
```
