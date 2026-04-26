# MTProxy Control

This project is a local-first MTProto proxy management panel.

The panel should start as a local development tool and later move to a control server.

## Local Startup

```bash
make setup
make db:migrate
make dev
```

- API: `http://localhost:8080/health`
- Web: `http://localhost:5173`
- Local database: `./data/panel.db`

## Goal

Build a UI that can:

- Add a server by SSH credentials.
- Check prerequisites such as Docker, Compose, DNS, port `443`, and existing containers.
- Generate a default Telemt config with field explanations.
- Let the operator edit config before applying it.
- Deploy Telemt over SSH.
- Restart MTProto service.
- Show Docker logs.
- Run health checks.
- Send Telegram alerts when a server or MTProto endpoint goes down.

<<<<<<< Updated upstream
## Recommended Stack
=======
Это самый прямой путь для локальной разработки, потому что в репозитории уже есть готовый `Makefile` для API и web UI. Если нужен более изолированный запуск, ниже есть вариант через Docker Compose.

## Docker Compose

Для более простого запуска без ручного старта двух процессов теперь есть контейнерная сборка:

```bash
docker compose build
docker compose up -d
```

После запуска:

- Web UI: `http://localhost:8081`
- API: `http://localhost:8080/health`

Что важно для SSH внутри контейнера:

- `docker-compose.yml` по умолчанию монтирует `${HOME}/.ssh` в `/root/.ssh` только для чтения.
- Если будешь использовать `private_key_path` в панели, внутри контейнера путь должен быть вида `/root/.ssh/id_ed25519`.
- Если не хочешь монтировать ключи с хоста, можно использовать вставку приватного ключа через интерфейс.
>>>>>>> Stashed changes

- Go backend.
- React frontend.
- SQLite database.
- SSH command execution from backend.
- Telemt on managed servers.

## Milestones

### Milestone 1: Local Skeleton And Diagnostics

Deliver:

- `make dev`
- Go API skeleton
- React UI skeleton
- SQLite migrations
- Add server form
- SSH connection test
- Docker/DNS/port diagnostics

### Milestone 2: Telemt Config And Deploy

Deliver:

- Telemt config generator
- Config editor with explanations
- Deploy preview
- Remote deploy executor
- Restart action
- Link retrieval from Telemt API

### Milestone 3: Operations UI

Deliver:

- Logs viewer
- Status view
- Event history
- Server detail page
- Safe remote command output handling

### Milestone 4: Health And Alerts

Deliver:

- Background health worker
- State transitions
- Telegram alerts
- Recovery alerts
- Anti-spam logic

### Milestone 5: Control Server Readiness

Deliver:

- UI auth
- Encrypted credential storage
- Docker Compose for panel itself
- Backup/export process

## Implementation Flow

Build the project milestone by milestone. Keep each change small enough to run and verify locally before moving on.

## Local Task Files

Detailed task files live in `tasks/`. They are intentionally ignored by git as local agent handoff notes, but should be kept in the working tree while building the project.

Execute tasks in order:

1. `tasks/00-scaffold.md`
2. `tasks/01-backend-foundation.md`
3. `tasks/02-ssh-layer.md`
4. `tasks/03-server-diagnostics.md`
5. `tasks/04-telemt-config.md`
6. `tasks/05-deploy-flow.md`
7. `tasks/06-server-operations.md`
8. `tasks/07-health-checks.md`
9. `tasks/08-telegram-alerts.md`
10. `tasks/09-hardening-and-server-deploy.md`
