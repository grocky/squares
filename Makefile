GREEN  := $(shell tput -Txterm setaf 2)
RESET  := $(shell tput -Txterm sgr0)

PROJECT_NAME := squares
DIST_DIR     := dist
.DEFAULT_GOAL := help

# =============================================================================
# Local Dev Environment
# =============================================================================

LOCAL_ENV := AWS_REGION=us-east-1 \
             AWS_ACCESS_KEY_ID=local \
             AWS_SECRET_ACCESS_KEY=local \
             AWS_ENDPOINT_URL=http://localhost:8000 \
             DYNAMODB_TABLE=squares \
             ADMIN_TOKEN=da76b49de385e373ed1b687d8fc8bcda114753fbf20d92f6

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
	$(LOCAL_ENV) SYNC_INTERVAL=60s POOL_ID=main SERVER_URL=http://localhost:8080 wgo run -xdir '.git,scripts' -file '\.go$$' ./cmd/cron

.PHONY: seed
seed: ## Seed local DynamoDB with sample data
	@echo "$(GREEN)Seeding local DynamoDB...$(RESET)"
	$(LOCAL_ENV) go run ./cmd/seed

.PHONY: seed-prod
seed-prod: ## Seed production DynamoDB from config/seed.json (uses AWS credentials from environment)
	@echo "$(GREEN)Seeding production DynamoDB...$(RESET)"
	AWS_REGION=us-east-1 DYNAMODB_TABLE=squares go run ./cmd/seed

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

GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION_LDFLAG := -X github.com/grocky/squares/internal/version.commit=$(GIT_COMMIT)

.PHONY: build
build: build-server ## Alias for build-server (EC2 binary)

.PHONY: build-cron
build-cron: ## Build cron Lambda binary (linux/arm64) → dist/
	@mkdir -p $(DIST_DIR)
	@echo "$(GREEN)Building cron Lambda binary...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -ldflags "$(VERSION_LDFLAG)" -o $(DIST_DIR)/bootstrap ./cmd/cron
	rm -f $(DIST_DIR)/bootstrap-cron.zip
	cd $(DIST_DIR) && zip bootstrap-cron.zip bootstrap && rm bootstrap
	@echo "$(GREEN)Built $(DIST_DIR)/bootstrap-cron.zip$(RESET)"

.PHONY: build-all
build-all: build-server build-cron ## Build server binary + cron Lambda binary

# =============================================================================
# EC2 Deploy
# =============================================================================

AWS_REGION ?= us-east-1
EC2_HOST ?= $(shell cd infrastructure && terraform output -raw ec2_public_ip 2>/dev/null)
EC2_USER ?= ec2-user
EC2_KEY  ?= ~/.ssh/squares
EC2_BINARY := $(DIST_DIR)/squares-server

.PHONY: build-server
build-server: ## Build server binary for Linux arm64 (EC2 t4g.micro)
	@mkdir -p $(DIST_DIR)
	@echo "$(GREEN)Building server binary (linux/arm64)...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -ldflags "$(VERSION_LDFLAG)" -o $(EC2_BINARY) ./cmd/server
	@echo "$(GREEN)Built $(EC2_BINARY)$(RESET)"

.PHONY: ec2-deploy
ec2-deploy: build-server ## Build + deploy server binary to EC2, then restart the service
	@if [ -z "$(EC2_HOST)" ]; then echo "EC2_HOST not set — run: make ec2-deploy EC2_HOST=<ip>"; exit 1; fi
	@echo "$(GREEN)Deploying to $(EC2_USER)@$(EC2_HOST)...$(RESET)"
	scp -i $(EC2_KEY) -o StrictHostKeyChecking=no $(EC2_BINARY) $(EC2_USER)@$(EC2_HOST):/tmp/squares-server
	ssh -i $(EC2_KEY) -o StrictHostKeyChecking=no $(EC2_USER)@$(EC2_HOST) \
		"sudo mv /tmp/squares-server /opt/squares/squares-server && \
		 sudo chown squares:squares /opt/squares/squares-server && \
		 sudo chmod +x /opt/squares/squares-server && \
		 sudo systemctl restart squares && \
		 sudo systemctl status squares --no-pager"
	@echo "$(GREEN)Deploy complete$(RESET)"

.PHONY: ec2-ssh
ec2-ssh: ## SSH into the EC2 instance
	@if [ -z "$(EC2_HOST)" ]; then echo "EC2_HOST not set"; exit 1; fi
	ssh -i $(EC2_KEY) $(EC2_USER)@$(EC2_HOST)

.PHONY: ec2-logs
ec2-logs: ## Tail the squares service logs on EC2
	@if [ -z "$(EC2_HOST)" ]; then echo "EC2_HOST not set"; exit 1; fi
	ssh -i $(EC2_KEY) -o StrictHostKeyChecking=no $(EC2_USER)@$(EC2_HOST) \
		"sudo journalctl -u squares -f --no-pager"

.PHONY: invoke-cron
invoke-cron: ## Manually invoke the score sync cron Lambda
	aws lambda invoke --function-name squares-cron --region $(AWS_REGION) /dev/stdout

# =============================================================================
# Infrastructure
# =============================================================================

.PHONY: tf-init
tf-init: ## Initialize Terraform
	terraform -chdir=infrastructure init

.PHONY: tf-plan
tf-plan: build-cron ## Plan infrastructure changes
	terraform -chdir=infrastructure plan

.PHONY: tf-apply
tf-apply: build-cron ## Apply infrastructure changes
	terraform -chdir=infrastructure apply

.PHONY: tf-destroy
tf-destroy: ## Destroy infrastructure (careful!)
	terraform -chdir=infrastructure destroy

.PHONY: deploy
deploy: build-cron tf-apply ec2-deploy ## Full deploy: cron Lambda + server binary to EC2

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
	rm -rf $(DIST_DIR) coverage.out
	@echo "$(GREEN)Cleaned build artifacts$(RESET)"

# =============================================================================
# Help
# =============================================================================

.PHONY: help
help: ## Print this help message
	@awk -F ':|##' '/^[^\t].+?:.*?##/ { printf "${GREEN}%-20s${RESET}%s\n", $$1, $$NF }' $(MAKEFILE_LIST) | sort
