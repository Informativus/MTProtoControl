# API Contract Draft

## Health

```http
GET /health
```

## Servers

```http
GET /api/servers
POST /api/servers
GET /api/servers/{id}
PATCH /api/servers/{id}
DELETE /api/servers/{id}
```

## SSH Test

```http
POST /api/ssh/test
```

Response:

```json
{
  "ok": true,
  "hostname": "proxy-node-1",
  "os": "Ubuntu 24.04",
  "arch": "x86_64",
  "user": "operator",
  "sudo_available": false
}
```

## Diagnostics

```http
POST /api/servers/{id}/diagnose
```

Checks:

- SSH reachable.
- Docker installed.
- Docker Compose installed.
- Public IP.
- DNS `public_host`.
- Port `443` listener.
- Existing containers.
- Telemt API if present.

## Config

```http
POST /api/servers/{id}/configs/generate
GET /api/servers/{id}/configs/current
PUT /api/servers/{id}/configs/current
```

## Deploy

```http
POST /api/servers/{id}/deploy/preview
POST /api/servers/{id}/deploy/apply
```

## Operations

```http
POST /api/servers/{id}/restart
GET /api/servers/{id}/logs
GET /api/servers/{id}/logs/stream
GET /api/servers/{id}/status
GET /api/servers/{id}/link
```

Use SSE for `logs/stream`.
