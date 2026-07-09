SQLC ?= sqlc
GO ?= go

.PHONY: help generate-db test verify check-sqlc docker-up docker-down

help:
	@echo "Available commands:"
	@echo "  make generate-db  		 Generate sqlc database code"
	@echo "  make test         		 Run Go tests"
	@echo "  make verify       		 Generate database code, then run tests"
	@echo "  make docker-up    		 Build and run the Docker container"
	@echo "  make docker-up-rebuild  Rebuild and run the Docker container"
	@echo "  make docker-down  		 Stop and remove the Docker container"

check-sqlc:
	@command -v $(SQLC) >/dev/null 2>&1 || { echo "sqlc not found. Install sqlc or run with SQLC=/path/to/sqlc."; exit 1; }

generate-db: check-sqlc
	$(SQLC) generate

test:
	$(GO) test -count=1 -v ./client/... ./server/...

docker-up:
	docker-compose up

docker-up-rebuild:
	docker-compose up --build --force-recreate

docker-down:
	docker-compose down

verify: generate-db test
