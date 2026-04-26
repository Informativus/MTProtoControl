# Makefile Contract

The implementation must expose simple commands from the repository root.

## Required Commands

```bash
make setup
```

Install local dependencies for API and web.

```bash
make db:migrate
```

Apply SQLite migrations to `DATABASE_PATH`.

```bash
make db:reset
```

Delete local dev database and re-run migrations. Must refuse to run when `APP_ENV=production`.

```bash
make dev
```

Start API and web UI together.

```bash
make dev:api
make dev:web
```

Start one side only.

```bash
make api:test
make web:test
```

Run tests.

```bash
make fmt
make lint
```

Format and lint.

## Suggested Implementation

Use a root `Makefile` that delegates:

```makefile
dev:
	$(MAKE) -j2 dev:api dev:web

dev:api:
	cd apps/api && go run ./cmd/api

dev:web:
	cd apps/web && npm run dev

db:migrate:
	cd apps/api && go run ./cmd/migrate up
```

Do not require Docker for local development unless a future task explicitly adds it.

