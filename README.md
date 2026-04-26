<p align="center">
  <img src="./apps/web/public/brand-mark.svg" width="140" alt="MTProxy Control logo" />
</p>

<h1 align="center">MTProxy Control</h1>

<p align="center">
  Локальная панель для управления Telegram MTProto proxy-серверами.
</p>

<p align="center">
  <img src="./docs/screenshots/dashboard.png" width="100%" alt="Серверная доска MTProxy Control" />
</p>

<p align="center">
  <img src="./docs/screenshots/workspace-overview.png" width="100%" alt="Рабочая зона сервера в MTProxy Control" />
</p>

<p align="center">
  <img src="./docs/screenshots/workspace-config.png" width="100%" alt="Конфиг MTProto в MTProxy Control" />
</p>

## О проекте

MTProxy Control - локальная панель для операторов Telegram MTProto proxy-серверов.

Панель подключается к серверам по SSH, помогает проверить состояние хоста, подготовить конфигурацию Telemt, выполнить deploy и просматривать состояние сервера в одном интерфейсе.

Этот README описывает пользовательскую установку панели на сервер и работу с ней через браузер.

## Что потребуется

Перед установкой панели подготовьте:

- Docker Engine
- Docker Compose plugin
- доступ по SSH к серверам, которыми будет управлять панель

## Установка на сервер

Скачайте `docker-compose.yml` и шаблон `.env`.

### Шаг 1. Скачать файлы

```bash
mkdir -p /opt/mtproxy-control
cd /opt/mtproxy-control
curl -fsSL "https://raw.githubusercontent.com/Informativus/MTProtoControl/main/deploy/docker-compose.release.yml" -o docker-compose.yml
curl -fsSL "https://raw.githubusercontent.com/Informativus/MTProtoControl/main/deploy/release.env.example" -o .env
mkdir -p ssh
```

### Шаг 2. Настроить `.env`

Откройте `.env` и замените значения под свой сервер.

### Шаг 3. Запустить контейнеры

```bash
docker compose --env-file .env up -d --pull always
```

Для остановки:

```bash
docker compose --env-file .env down
```

## Как это работает

- контейнер `api` запускает миграции SQLite и поднимает Go API на `:8080`
- контейнер `api` остаётся во внутренней docker-сети и по умолчанию не публикуется наружу
- контейнер `web` отдаёт React UI через nginx на `:80`, проксирует `/api/*` в контейнер `api` и отдаёт `/health` наружу
- named volume `mtproxy-control-data` хранит SQLite базу между перезапусками
- если ты хочешь использовать `private_key_path`, положи SSH-ключи и `known_hosts` в папку `./ssh` рядом с release `docker-compose.yml`; эта папка монтируется в контейнер API только на чтение как `/root/.ssh`

## Что менять в `.env`

- `IMAGE_REGISTRY`: Docker Hub namespace, по умолчанию `docker.io/informativus`
- `IMAGE_TAG`: тег образов, обычно менять не нужно
- `WEB_PORT`: внешний порт web UI на сервере, по умолчанию `8081`
- `HEALTHCHECK_INTERVAL`: как часто панель делает автоматические health checks
- `APP_ENV`: оставляйте `production` для серверной установки
- `DATABASE_PATH`: обычно не меняйте, если хотите хранить SQLite в стандартном docker volume

Проверка health после установки будет доступна через `http://<host>:<WEB_PORT>/health`.

Если в панели будете указывать путь к приватному ключу, сначала положите его в `./ssh`, а потом используйте путь внутри контейнера, например `/root/.ssh/id_ed25519`.

## Панель и прокси на одном сервере

Если панель установлена на том же сервере, которым она управляет, отдельный режим не нужен: добавьте этот хост в инвентарь как обычный сервер по SSH.

- В поле `host` можно указать `localhost` или `127.0.0.1`.
- В docker-установке API-контейнер сам переведет loopback SSH на хост сервера через `host.docker.internal`.
- Для `private_key_path` используйте путь внутри контейнера, например `/root/.ssh/id_ed25519`.
- Для docker-установки сначала положите SSH-ключ в `./ssh` рядом с release `docker-compose.yml`, а потом укажите его в панели как `/root/.ssh/<имя_ключа>`.
- Панель проверяет SSH host key, поэтому заранее положите запись в `./ssh/known_hosts`, например `ssh-keyscan <server-ip> localhost 127.0.0.1 > ./ssh/known_hosts`.
- Если удобнее, вместо `localhost` можно по-прежнему указывать обычный IP или DNS-имя сервера.

Короткий same-host рецепт:

```bash
mkdir -p ./ssh
cp ~/.ssh/id_ed25519 ./ssh/id_ed25519
ssh-keyscan <server-ip> localhost 127.0.0.1 > ./ssh/known_hosts
chmod 600 ./ssh/id_ed25519 ./ssh/known_hosts
```

Потом в панели укажите:

- `host = localhost`
- `ssh_user = <ваш SSH user>`
- `private_key_path = /root/.ssh/id_ed25519`
- `remote_base_path = /opt/mtproto-panel/telemt`

## Доступ через SSH туннель

Если панель запущена на удалённом сервере и вы не хотите открывать доступ к интерфейсу извне, используйте SSH-туннель.

На своём компьютере выполните:

```bash
ssh -N -L 8081:127.0.0.1:8081 -L 8080:127.0.0.1:8080 user@your-server
```

После этого откройте локально:

- Web UI: `http://127.0.0.1:8081`
- API health: `http://127.0.0.1:8080/health`

Если локальные порты заняты, используйте другие:

```bash
ssh -N -L 18081:127.0.0.1:8081 -L 18080:127.0.0.1:8080 user@your-server
```

Тогда будут доступны адреса:

- Web UI: `http://127.0.0.1:18081`
- API health: `http://127.0.0.1:18080/health`
