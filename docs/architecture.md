# Architecture

## High-Level Shape

```text
React UI
  |
  | HTTP/SSE
  v
Go API + workers
  |
  | SQLite
  v
local panel database
  |
  | SSH
  v
managed MTProto servers
```

The panel starts locally. Later it can run on a control server with the same components.

## Backend Responsibilities

- Server inventory CRUD.
- SSH credential handling.
- SSH connection tests.
- Remote command execution with timeouts.
- Server diagnostics.
- Telemt config generation and validation.
- Deploy preview and deploy execution.
- Restart actions.
- Log retrieval and streaming.
- Health-check scheduler.
- Telegram alert sender.

## Frontend Responsibilities

- Server list and status badges.
- Add server wizard.
- Diagnostics view.
- Config editor.
- Deploy preview and execution progress.
- Logs view.
- Health history.
- Settings for Telegram alerts and security.

## Remote Server Layout

Default panel-managed path:

```text
/opt/mtproto-panel/telemt/
  docker-compose.yml
  config.toml
  backups/
```

For existing manually configured hosts, the panel must support custom paths.

## Data Flow: Add Server

```text
User enters SSH details
  -> API tests SSH
  -> API gathers facts
  -> API stores server and credential
  -> UI shows diagnostics
  -> User generates config
  -> User deploys after preview
```

## Data Flow: Health Check

```text
scheduler tick
  -> TCP check public_host:port
  -> SSH check docker state
  -> SSH curl Telemt API
  -> persist result
  -> if state changed, send Telegram alert
```

## Security Boundaries

- The browser never receives private SSH key material after save.
- Backend decrypts credentials only in memory for the current operation.
- Remote commands are templates with parameters, not arbitrary text from UI.
- Raw command execution can exist later as an admin-only diagnostic feature, but it is not MVP.

