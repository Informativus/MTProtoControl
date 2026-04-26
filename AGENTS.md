# MTProxy Control Agent Guide

## Purpose

This repository is the project workspace for building a local-first web panel that manages many Telegram MTProto proxy servers over SSH.

The target product is:

- React UI for operators.
- Go backend/API/worker.
- SQLite local database.
- SSH orchestration, no required agent on managed servers.
- Telemt as the default MTProto engine.
- Later deployable to a dedicated control server without redesign.

## Current Operational Context

The first known working server should be treated as a generic reference host.

Observed working setup on the reference host:

- Public IP: `<public-ip>`
- Public domain: `mt.example.com`
- MTProto public port: `443`
- MTProto engine: `ghcr.io/telemt/telemt:latest`
- Telemt compose path: `/srv/telemt/docker-compose.yml`
- Telemt config path: `/srv/telemt/config.toml`
- Telemt local API: `127.0.0.1:9091`
- Telemt local backend bind: `127.0.0.1:17443 -> container 443`
- Current SNI/FakeTLS domain: `mt.example.com`
- Current mask host: `www.yandex.ru:443`
- Existing Jitsi also runs on the same host.
- Because Jitsi previously owned public `443`, a host-network HAProxy SNI router now owns public `443`.
- Router path: `/srv/sni-router/haproxy.cfg`
- Router rule: `meet.example.com` goes to Jitsi, all other TLS traffic goes to MTProto.

Important lesson:

- For FakeTLS, prefer an SNI domain that resolves to the proxy IP. A generic third-party SNI may pass TLS probing but still fail in Telegram; switching to a domain that resolves to the proxy IP made Telegram show ping.

## Product Rules

- Do not assume a server is empty.
- Always run diagnostics before deploy.
- Do not overwrite existing services without a deploy preview and explicit confirmation.
- Treat port `443` conflicts as first-class. Show what owns the port and offer choices.
- Store SSH private keys encrypted if they are stored in the app database.
- Prefer storing a key path for local development when possible.
- Every remote command must have timeout, stdout, stderr, exit code, and visible event log.
- Every config apply must save a previous version before replacing a remote file.
- Health checks must test real MTProto reachability, not only container liveness.
- Telegram alerts must be state-change based to avoid alert spam.

## Engineering Defaults

- Backend: Go.
- Router: `chi` or standard `net/http` with small middleware.
- Database: SQLite.
- Migrations: explicit SQL migration files.
- Frontend: React with Vite unless the implementing agent has a strong reason to use Next.js.
- UI should be operational and dense, not a marketing page.
- Logs streaming: SSE first; WebSocket only if needed.
- Config format: TOML for Telemt.
- Remote deploy base path default: `/opt/mtproto-panel/telemt`.
- Local dev database default: `./data/panel.db`.

## Makefile Contract

The repo implementation must support:

```bash
make setup
make db:migrate
make db:reset
make dev
make dev:api
make dev:web
make api:test
make web:test
make fmt
make lint
```

`make dev` must start both API and web UI with one command.

## Agent Workflow

1. Read `README.md`.
2. Read `context/current-reference-host.md`.
3. Read `docs/architecture.md`.
4. Pick exactly one task file from `tasks/`.
5. Do not skip acceptance criteria.
6. Update only files needed for the current task.
7. Leave a concise completion note with changed paths and verification commands.

## Stop Rules

Stop and report instead of improvising if:

- A task would require storing plaintext SSH keys.
- A deploy action might overwrite non-panel services on a managed server.
- Port `443` is already used and no routing strategy was selected.
- Docker install would require broad host changes not approved by the operator.
- Health checks cannot distinguish panel failure from remote server failure.
