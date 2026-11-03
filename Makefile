COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: help up up-all down reset logs ps demo test test-unit vet fmt tidy build

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

up: ## Build and start the platform (Postgres, Redis, ClickHouse, services)
	$(COMPOSE) up -d --build
	@echo ""
	@echo "Gateway:       http://localhost:8080   (try: make demo)"
	@echo "Control plane: http://localhost:8081/v1/catalog/apis"
	@echo "ClickHouse:    http://localhost:8123/play  (user default / dev)"

up-all: ## Also start MinIO, Mailpit and the OpenTelemetry Collector
	$(COMPOSE) --profile extras --profile observability up -d --build

down: ## Stop everything (keeps data)
	$(COMPOSE) --profile extras --profile observability down

reset: ## Stop and DELETE all local data, then start fresh with seed data
	$(COMPOSE) --profile extras --profile observability down -v
	$(MAKE) up

logs: ## Follow service logs
	$(COMPOSE) logs -f gateway controlplane metering

ps: ## Show running containers
	$(COMPOSE) ps

demo: ## Run example calls through the gateway
	./scripts/demo.sh

TEST_DATABASE_URL ?= postgres://apimarket:apimarket@localhost:5432/apimarket?sslmode=disable

test: ## Unit + integration tests (uses Postgres/Redis from `make up`; integration tests skip if absent)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" REDIS_ADDR=localhost:6379 go test -race -count=1 ./...

test-unit: ## Unit tests only
	go test -count=1 -short ./...

vet: ## Static checks
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

fmt: ## Format code
	gofmt -w .

tidy: ## Update go.mod / go.sum
	go mod tidy

build: ## Build all service binaries into ./bin
	CGO_ENABLED=0 go build -o bin/ ./cmd/...
