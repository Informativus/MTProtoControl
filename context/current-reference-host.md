# Current Reference Host Context

This context is for future agents implementing diagnostics and deploy logic. It is not a universal template; always re-check the live host before changing it.

## Known Host

- SSH alias: `reference_host`
- Public IP: `<public-ip>`
- MTProto domain: `mt.example.com`
- Jitsi domain: `meet.example.com`

## Working MTProto State

Telemt is running as:

```text
container: telemt-mtproto
image: ghcr.io/telemt/telemt:latest
status: healthy
remote path: /srv/telemt
local backend bind: 127.0.0.1:17443 -> container 443
local API: 127.0.0.1:9091
```

Current Telemt config shape:

```toml
[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
show = "*"
public_host = "mt.example.com"
public_port = 443

[server]
port = 443

[server.api]
enabled = true
listen = "0.0.0.0:9091"
whitelist = ["127.0.0.1/32", "172.16.0.0/12"]

[censorship]
tls_domain = "mt.example.com"
mask = true
mask_host = "www.yandex.ru"
mask_port = 443
tls_emulation = false
tls_front_dir = "tlsfront"
```

Current working Telegram link format:

```text
https://t.me/proxy?server=mt.example.com&port=443&secret=ee<secret><hex(tls_domain)>
```

## Existing Jitsi Conflict

Jitsi already existed on the host and used public `443`. To preserve it, public `443` was moved to HAProxy:

```text
0.0.0.0:443 -> HAProxy SNI router
127.0.0.1:8443 -> Jitsi HTTPS
127.0.0.1:17443 -> Telemt HTTPS/FakeTLS
```

HAProxy rule:

```haproxy
use_backend jitsi_https if { req.ssl_sni -i meet.example.com }
default_backend mtproto_tls
```

This means:

- `meet.example.com` stays on Jitsi.
- Any other SNI defaults to MTProto.
- Telegram traffic does not use Jitsi.

## Validation Commands

Useful checks:

```bash
ssh reference_host 'docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"'
ssh reference_host 'curl -sS http://127.0.0.1:9091/v1/users'
ssh reference_host 'ss -ltnp | grep -E ":(443|8443|17443|9091)\b"'
```

External check from another server:

```bash
openssl s_client -connect <public-ip>:443 -servername mt.example.com -brief </dev/null
```

## Lessons To Encode In Panel

- Show port ownership before deploy.
- Show SNI/DNS alignment.
- Warn if `tls_domain` does not resolve to the proxy IP.
- Prefer generated link from Telemt API over hand-built links.
- Keep existing services safe by default.
