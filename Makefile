ifneq (,$(wildcard .env))
include .env
export
endif

MIGRATE_DATABASE_URL=postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=$(POSTGRES_SSLMODE)
MIGRATE_PATH=services/api/migrations

.PHONY: migrate-up migrate-down install-hooks test ci-local smoke fmt

# On Windows Git Bash without GNU make, use:
#   bash scripts/install-hooks.sh
#   bash scripts/ci-local.sh
#   bash scripts/smoke-test.sh
# Or: mingw32-make install-hooks  (if MSYS2 make is installed)

migrate-up:
	migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DATABASE_URL)" down 1

install-hooks:
	bash scripts/install-hooks.sh

test:
	cd services/api && go test ./... -count=1
	cd apps/web && npm run lint

fmt:
	cd services/api && gofmt -w .

ci-local:
	bash scripts/ci-local.sh

smoke:
	bash scripts/smoke-test.sh
