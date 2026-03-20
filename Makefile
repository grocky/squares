GREEN  := $(shell tput -Txterm setaf 2)
RESET  := $(shell tput -Txterm sgr0)

PROJECT_NAME := squares
.DEFAULT_GOAL := help

# =============================================================================
# Local Dev Environment
# =============================================================================

LOCAL_ENV := AWS_REGION=us-east-1 \
             AWS_ACCESS_KEY_ID=local \
             AWS_SECRET_ACCESS_KEY=local \
             AWS_ENDPOINT_URL=http://localhost:8000 \
             DYNAMODB_TABLE=squares

.PHONY: dev
dev: ## Start DynamoDB Local via Docker
	docker compose up -d
	@echo "Waiting for DynamoDB Local to be ready..."
	@until curl -s http://localhost:8000 > /dev/null 2>&1; do sleep 1; done
	@echo "$(GREEN)DynamoDB Local is up at http://localhost:8000$(RESET)"

.PHONY: dev-down
dev-down: ## Stop DynamoDB Local
	docker compose down

.PHONY: dev-setup
dev-setup: dev-down dev create-table seed ## Fresh restart of DynamoDB Local, create table, and seed data

.PHONY: create-table
create-table: ## Create the DynamoDB table in local environment
	@echo "$(GREEN)Creating DynamoDB table...$(RESET)"
	$(LOCAL_ENV) ./scripts/create-table.sh

# =============================================================================
# Application
# =============================================================================

.PHONY: run
run: ## Run the server locally against DynamoDB Local
	$(LOCAL_ENV) wgo run -xdir '.git,scripts' -file '\.go$$' -file '\.html$$' -file '\.css$$' ./cmd/server

# Run cron locally — notifies the local server via SERVER_URL after each sync
.PHONY: cron-local
cron-local: ## Run the score sync cron locally (notifies server for SSE broadcast) 
	$(LOCAL_ENV) SYNC_INTERVAL=60s POOL_ID=main SERVER_URL=http://localhost:8080 wgo run -xdir '.git,scripts' -file '\.go$$' -file '\.html$$' -file '\.css$$' ./cmd/cron

.PHONY: seed
seed: ## Seed local DynamoDB with sample data
	@echo "$(GREEN)Seeding local DynamoDB...$(RESET)"
	$(LOCAL_ENV) go run ./cmd/seed

.PHONY: dump-seed
dump-seed: ## Snapshot current DynamoDB state into cmd/seed/main.go
	@echo "$(GREEN)Dumping current state to seed file...$(RESET)"
	$(LOCAL_ENV) go run ./cmd/dump

.PHONY: sync
sync: ## Sync live ESPN scores against local server
	curl -X POST http://localhost:8080/pools/main/sync

# =============================================================================
# Build
# =============================================================================

.PHONY: build
build: ## Build Lambda binary (linux/arm64)
	@echo "$(GREEN)Building Lambda binary...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -o bootstrap ./cmd/server
	@echo "$(GREEN)Built bootstrap binary for Lambda$(RESET)"

.PHONY: build-cron
build-cron: ## Build Lambda binary for cron (linux/arm64)
	@echo "$(GREEN)Building cron Lambda binary...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -o bootstrap-cron ./cmd/cron
	@echo "$(GREEN)Built bootstrap-cron binary for Lambda$(RESET)"

# =============================================================================
# Quality
# =============================================================================

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: vet
vet: fmt ## Vet Go code
	go vet ./...

.PHONY: test
test: ## Run all tests
	@echo "$(GREEN)Running tests...$(RESET)"
	go test ./...

.PHONY: test-coverage
test-coverage: ## Run tests with coverage report
	@echo "$(GREEN)Running tests with coverage...$(RESET)"
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

.PHONY: clean
clean: ## Remove build artifacts
	rm -f bootstrap bootstrap-cron coverage.out
	@echo "$(GREEN)Cleaned build artifacts$(RESET)"

# =============================================================================
# Help
# =============================================================================

.PHONY: help
help: ## Print this help message
	@awk -F ':|##' '/^[^\t].+?:.*?##/ { printf "${GREEN}%-20s${RESET}%s\n", $$1, $$NF }' $(MAKEFILE_LIST) | sort
