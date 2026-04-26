SHELL := /bin/sh

API_ADDR ?= :8080
DATABASE_PATH ?= ./data/panel.db
WEB_HOST ?= 0.0.0.0
WEB_PORT ?= 5173
VITE_API_BASE_URL ?= http://localhost:8080
GOCACHE ?= /tmp/mtproxy-control-gocache

.PHONY: setup db\:migrate db\:reset dev dev\:api dev\:web api\:test web\:test fmt lint

setup:
	GOCACHE="$(GOCACHE)" go mod download
	cd apps/web && npm install

db\:migrate:
	GOCACHE="$(GOCACHE)" DATABASE_PATH="$(abspath $(DATABASE_PATH))" go run ./apps/api/cmd/migrate up

db\:reset:
	@if [ "$$APP_ENV" = "production" ]; then \
		echo "refusing to reset database when APP_ENV=production"; \
		exit 1; \
	fi
	rm -f "$(DATABASE_PATH)"
	$(MAKE) db:migrate

dev:
	$(MAKE) -j2 dev:api dev:web

dev\:api:
	@api_port="$(API_ADDR)"; \
	api_port="$${api_port##*:}"; \
	if curl -fsS "$(VITE_API_BASE_URL)/health" >/dev/null 2>&1; then \
		echo "reusing existing api at $(VITE_API_BASE_URL)"; \
	elif lsof -nP -iTCP:"$$api_port" -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "port $(API_ADDR) is already in use and no healthy mtproxy-control api responded at $(VITE_API_BASE_URL)/health"; \
		echo "stop the existing process or set API_ADDR and VITE_API_BASE_URL to a free port"; \
		exit 1; \
	else \
		GOCACHE="$(GOCACHE)" API_ADDR="$(API_ADDR)" DATABASE_PATH="$(abspath $(DATABASE_PATH))" go run ./apps/api/cmd/api; \
	fi

dev\:web:
	cd apps/web && VITE_API_BASE_URL="$(VITE_API_BASE_URL)" npm run dev -- --host "$(WEB_HOST)" --port "$(WEB_PORT)"

api\:test:
	GOCACHE="$(GOCACHE)" go test ./apps/api/...

web\:test:
	cd apps/web && npm run test

fmt:
	gofmt -w apps/api
	cd apps/web && npm run fmt

lint:
	GOCACHE="$(GOCACHE)" go test ./apps/api/...
	cd apps/web && npm run lint
