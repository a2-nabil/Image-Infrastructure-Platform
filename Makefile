ifneq (,$(wildcard .env))
include .env
export
endif

MIGRATE_DATABASE_URL=postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=$(POSTGRES_SSLMODE)
MIGRATE_PATH=services/image-api/migrations

.PHONY: migrate-up migrate-down

migrate-up:
	migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DATABASE_URL)" up

migrate-down:
	migrate -path $(MIGRATE_PATH) -database "$(MIGRATE_DATABASE_URL)" down 1
