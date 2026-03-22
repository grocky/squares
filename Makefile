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
build: ## Build server Lambda binary (linux/arm64) → dist/
	@mkdir -p $(DIST_DIR)
	@echo "$(GREEN)Building server Lambda binary...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -ldflags "$(VERSION_LDFLAG)" -o $(DIST_DIR)/bootstrap ./cmd/server
	rm -f $(DIST_DIR)/bootstrap.zip
	cd $(DIST_DIR) && zip bootstrap.zip bootstrap && rm bootstrap
	@echo "$(GREEN)Built $(DIST_DIR)/bootstrap.zip$(RESET)"

.PHONY: build-cron
build-cron: ## Build cron Lambda binary (linux/arm64) → dist/
	@mkdir -p $(DIST_DIR)
	@echo "$(GREEN)Building cron Lambda binary...$(RESET)"
	GOOS=linux GOARCH=arm64 go build -ldflags "$(VERSION_LDFLAG)" -o $(DIST_DIR)/bootstrap ./cmd/cron
	rm -f $(DIST_DIR)/bootstrap-cron.zip
	cd $(DIST_DIR) && zip bootstrap-cron.zip bootstrap && rm bootstrap
	@echo "$(GREEN)Built $(DIST_DIR)/bootstrap-cron.zip$(RESET)"

.PHONY: build-all
build-all: build build-cron ## Build all Lambda binaries → dist/

# =============================================================================
# Docker / ECS
# =============================================================================

AWS_REGION ?= us-east-1
AWS_ACCOUNT_ID ?= $(shell aws sts get-caller-identity --query Account --output text 2>/dev/null)
ECR_REGISTRY := $(AWS_ACCOUNT_ID).dkr.ecr.$(AWS_REGION).amazonaws.com
ECR_REPO ?= $(ECR_REGISTRY)/squares-server

.PHONY: docker-build
docker-build: ## Build Docker image for the server
	@echo "$(GREEN)Building Docker image...$(RESET)"
	docker build -t squares-server .

.PHONY: docker-push
docker-push: docker-build ## Build and push Docker image to ECR
	@echo "$(GREEN)Pushing to ECR: $(ECR_REPO)...$(RESET)"
	aws ecr get-login-password --region $(AWS_REGION) | docker login --username AWS --password-stdin $(ECR_REGISTRY)
	docker tag squares-server:latest $(ECR_REPO):latest
	docker push $(ECR_REPO):latest
	@echo "$(GREEN)Pushed $(ECR_REPO):latest$(RESET)"

.PHONY: ecs-deploy
ecs-deploy: docker-push ## Push image and force ECS service redeploy
	@echo "$(GREEN)Forcing ECS service redeploy...$(RESET)"
	aws ecs update-service --cluster squares --service squares-server --force-new-deployment --region $(AWS_REGION) > /dev/null
	@echo "$(GREEN)ECS redeploy triggered$(RESET)"

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
tf-plan: build-all ## Plan infrastructure changes
	terraform -chdir=infrastructure plan

.PHONY: tf-apply
tf-apply: build-all ## Apply infrastructure changes
	terraform -chdir=infrastructure apply

.PHONY: tf-destroy
tf-destroy: ## Destroy infrastructure (careful!)
	terraform -chdir=infrastructure destroy

.PHONY: deploy
deploy: build-cron tf-apply ecs-deploy ## Full deploy: cron Lambda + Docker image + ECS redeploy

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
