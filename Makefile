# Servify Makefile

.PHONY: help build build-cli build-weknora build-knowledge-provider run run-cli run-weknora run-knowledge-provider migrate migrate-seed test clean clean-runtime docker-build docker-run docker-up-weknora docker-up-knowledge-provider docker-down docker-logs-weknora docker-logs-knowledge-provider docker-up-observ docker-down-observ dev-setup fmt lint update-deps docs changelog release-changelog sdk-sync-versions sdk-check-versions repo-hygiene generated-assets local-check security-check observability-check release-check dify-acceptance weknora-acceptance knowledge-provider-acceptance auth-session-acceptance validate-acceptance-manifest check-acceptance-evidence

# Default target
help:
	@echo "Available commands:"
	@echo "  build         - Build the application"
	@echo "  build-cli     - Build CLI (standard)"
	@echo "  build-weknora - Build CLI with WeKnora compatibility tag"
	@echo "  build-knowledge-provider - Alias of build-weknora for generic provider compatibility runs"
	@echo "  run           - Run the application"
	@echo "  run-cli       - Run CLI (standard)"
	@echo "  run-weknora   - Run CLI with WeKnora compatibility config"
	@echo "  run-knowledge-provider - Alias of run-weknora for external provider compatibility runs"
	@echo "  migrate       - Run database migrations"
	@echo "  migrate-seed  - Run database migrations with seed data"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  clean-runtime - Remove local runtime output directories"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run with Docker Compose"
	@echo "  docker-up-weknora - Up WeKnora compatibility/mock compose (server+weknora+db)"
	@echo "  docker-up-knowledge-provider - Alias of docker-up-weknora for compatibility/mock provider stack"
	@echo "  docker-down      - Down compose services"
	@echo "  docker-logs-weknora - Tail servify logs"
	@echo "  docker-logs-knowledge-provider - Alias of docker-logs-weknora"
	@echo "  docker-up-observ    - Up OTel Collector + Jaeger"
	@echo "  docker-down-observ  - Down observability stack"
	@echo "  docker-stop   - Stop Docker Compose services"
	@echo "  changelog     - Generate a release changelog draft"
	@echo "  release-changelog - Write changelog draft to .runtime/release/RELEASE_CHANGELOG.md"
	@echo "  sdk-sync-versions - Sync SDK package versions from sdk/package.json"
	@echo "  sdk-check-versions - Check SDK package versions without modifying files"
	@echo "  repo-hygiene  - Validate runtime/build artifacts are not tracked"
	@echo "  generated-assets - Regenerate and verify committed generated assets"
	@echo "  local-check   - Run the minimal local environment verification"
	@echo "  security-check - Validate the config security baseline in strict mode"
	@echo "  observability-check - Validate the observability baseline in strict mode"
	@echo "  release-check - Run the minimal release-readiness verification"
	@echo "  dify-acceptance - Run the Dify primary-path acceptance script"
	@echo "  weknora-acceptance - Run the WeKnora compatibility acceptance script"
	@echo "  knowledge-provider-acceptance - Alias of weknora-acceptance for provider compatibility runs"
	@echo "  auth-session-acceptance - Run the auth self-service session acceptance script"
	@echo "  validate-acceptance-manifest - Validate a generated acceptance manifest"
	@echo "  check-acceptance-evidence - Validate all checked-in acceptance manifests under scripts/test-results"

# Build the application
build:
	@echo "Building Servify..."
	$(MAKE) _build-with-ldflags

# Build CLI targets
build-cli:
	@echo "Building CLI (standard)..."
	$(MAKE) _build-cli-with-ldflags

build-weknora:
	@echo "Building CLI (weknora compatibility)..."
	$(MAKE) _build-cli-weknora-with-ldflags

build-knowledge-provider: build-weknora

# Run the application
run:
	@echo "Starting Servify server..."
	go -C apps/server run ./cmd/server

run-cli:
	@echo "Running CLI (standard)..."
	go -C apps/server run ./cmd -c $(or $(CONFIG),../../config.yml) run

run-weknora:
	@echo "Running CLI (weknora compatibility)..."
	go -C apps/server run -tags weknora ./cmd -c $(or $(CONFIG),../../config.weknora.yml) run

run-knowledge-provider: run-weknora

# Run database migrations
migrate:
	@echo "Running database migrations..."
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) DB_NAME=$(DB_NAME) DB_SSLMODE=$(or $(DB_SSLMODE),disable) DB_TIMEZONE=$(or $(DB_TIMEZONE),UTC) \
	go -C apps/server run ./cmd/migrate $(MIGRATE_ARGS)

# Run database migrations with seed data
migrate-seed:
	@echo "Running database migrations with seed data..."
	DB_HOST=$(DB_HOST) DB_PORT=$(DB_PORT) DB_USER=$(DB_USER) DB_PASSWORD=$(DB_PASSWORD) DB_NAME=$(DB_NAME) DB_SSLMODE=$(or $(DB_SSLMODE),disable) DB_TIMEZONE=$(or $(DB_TIMEZONE),UTC) \
	go -C apps/server run ./cmd/migrate --seed $(MIGRATE_ARGS)

# Run tests
test:
	@echo "Running tests via scripts/run-tests.sh..."
	./scripts/run-tests.sh

# Clean build artifacts
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	go clean

clean-runtime:
	@echo "Cleaning runtime output..."
	sh ./scripts/clean-runtime.sh

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t servify:latest .

# Run with Docker Compose
docker-run:
	@echo "Starting services with Docker Compose..."
	docker-compose up -d

docker-up-weknora:
	@echo "Starting WeKnora compatibility/mock stack..."
	docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml up -d

docker-up-knowledge-provider: docker-up-weknora

docker-down:
	@echo "Stopping services..."
	docker-compose -f infra/compose/docker-compose.yml down

docker-logs-weknora:
	@echo "Tailing servify logs (weknora compatibility stack)..."
	docker-compose -f infra/compose/docker-compose.yml -f infra/compose/docker-compose.weknora.yml logs -f servify

docker-logs-knowledge-provider: docker-logs-weknora

docker-up-observ:
	@echo "Starting observability stack (OTel Collector + Jaeger)..."
	docker-compose -f infra/compose/docker-compose.observability.yml up -d

docker-down-observ:
	@echo "Stopping observability stack..."
	docker-compose -f infra/compose/docker-compose.observability.yml down -v

# Stop Docker Compose services
docker-stop:
	@echo "Stopping Docker Compose services..."
	docker-compose down

# Development helpers
dev-setup:
	@echo "Setting up development environment..."
	@echo "Installing dependencies..."
	go -C apps/server mod tidy
	@echo "Creating .env file if it doesn't exist..."
	@test -f .env || cp .env.example .env
	@echo "Setup complete! Edit .env file with your configuration."

# Format code
fmt:
	@echo "Formatting code..."
	go -C apps/server fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	cd apps/server && golangci-lint run ./...

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	go -C apps/server mod tidy
	go -C apps/server mod download

# Generate API documentation (if using swag)
docs:
	@echo "Generating API documentation..."
	@command -v swag >/dev/null 2>&1 || { echo "swag is not installed. Install with: go install github.com/swaggo/swag/cmd/swag@latest"; exit 1; }
	swag init -g apps/server/cmd/server/main.go -o docs/

changelog:
	@echo "Generating changelog draft..."
	./scripts/generate-changelog.sh $(FROM) $(TO)

release-changelog:
	@echo "Generating release changelog draft file..."
	@mkdir -p ./.runtime/release
	./scripts/generate-changelog.sh $(FROM) $(TO) > ./.runtime/release/RELEASE_CHANGELOG.md
	@echo "Wrote ./.runtime/release/RELEASE_CHANGELOG.md"

sdk-sync-versions:
	@echo "Syncing SDK workspace versions..."
	npm -C sdk run version:sync

sdk-check-versions:
	@echo "Checking SDK workspace versions..."
	npm -C sdk run version:check

repo-hygiene:
	@echo "Running repository hygiene checks..."
	bash ./scripts/check-repo-hygiene.sh

generated-assets:
	@echo "Regenerating committed generated assets..."
	sh ./scripts/regenerate-generated-assets.sh

local-check:
	@echo "Running local environment verification..."
	sh ./scripts/check-local-environment.sh

security-check:
	@echo "Running security baseline validation..."
	sh ./scripts/check-security-baseline.sh $(or $(CONFIG),config.yml)

observability-check:
	@echo "Running observability baseline validation..."
	sh ./scripts/check-observability-baseline.sh $(or $(CONFIG),config.yml)

release-check:
	@echo "Running release-readiness validation..."
	sh ./scripts/check-release-readiness.sh $(or $(CONFIG),config.yml)

dify-acceptance:
	@echo "Running Dify primary-path acceptance..."
	chmod +x ./scripts/test-dify-integration.sh
	./scripts/test-dify-integration.sh

weknora-acceptance:
	@echo "Running WeKnora compatibility acceptance..."
	chmod +x ./scripts/test-weknora-integration.sh
	./scripts/test-weknora-integration.sh

knowledge-provider-acceptance: weknora-acceptance

auth-session-acceptance:
	@echo "Running auth self-service session acceptance..."
	chmod +x ./scripts/test-auth-session-acceptance.sh
	./scripts/test-auth-session-acceptance.sh

validate-acceptance-manifest:
	@echo "Validating acceptance manifest..."
	sh ./scripts/validate-acceptance-manifest.sh $(MANIFEST)

check-acceptance-evidence:
	@echo "Checking acceptance evidence manifests..."
	sh ./scripts/check-acceptance-evidence.sh

# Internal targets with ldflags (version info)
VERSION ?= dev
GIT_COMMIT = $(shell sh -lc 'git rev-parse --short HEAD 2>/dev/null || printf none')
BUILD_TIME = $(shell sh -lc 'date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || printf unknown')
LDFLAGS = -X 'servify/apps/server/internal/version.Version=$(VERSION)' -X 'servify/apps/server/internal/version.Commit=$(GIT_COMMIT)' -X 'servify/apps/server/internal/version.BuildTime=$(BUILD_TIME)'

_build-with-ldflags:
	go -C apps/server build -ldflags "$(LDFLAGS)" -o ../../bin/servify ./cmd/server
	go -C apps/server build -ldflags "$(LDFLAGS)" -o ../../bin/migrate ./cmd/migrate
	go -C apps/server build -ldflags "$(LDFLAGS)" -o ../../bin/servify-cli ./cmd

_build-cli-with-ldflags:
	go -C apps/server build -ldflags "$(LDFLAGS)" -o ../../bin/servify-cli ./cmd

_build-cli-weknora-with-ldflags:
	go -C apps/server build -ldflags "$(LDFLAGS)" -tags weknora -o ../../bin/servify-cli-weknora ./cmd

# Database operations
db-reset: migrate-seed
	@echo "Database reset complete with seed data"

# Show application status
status:
	@echo "Checking application status..."
	@curl -s http://localhost:8080/health | json_pp || echo "Application not running"

# View logs (for Docker Compose)
logs:
	@echo "Showing application logs..."
	docker-compose -f infra/compose/docker-compose.yml logs -f servify

# Sync SDK bundles into demo-sdk
demo-sync-sdk:
	@echo "Syncing SDK bundles into apps/demo-sdk ..."
	chmod +x ./scripts/sync-sdk-to-demo.sh
	./scripts/sync-sdk-to-demo.sh

# Admin panel commands
admin-install:
	@echo "Installing admin dependencies..."
	cd apps/admin && pnpm install

admin-dev:
	@echo "Starting admin dev server..."
	cd apps/admin && pnpm dev

admin-build:
	@echo "Building admin panel..."
	cd apps/admin && pnpm install --frozen-lockfile && pnpm build

# Website (Cloudflare static assets) commands
website-dev:
	@echo "Starting local static preview for website..."
	@(python3 -m http.server -d apps/website 8081 || python -m http.server -d apps/website 8081)

website-deploy:
	@echo "Deploying website static assets with Wrangler..."
	npx --yes wrangler deploy --config apps/website/wrangler.jsonc

website-pages-deploy:
	@echo "Deploying website to Cloudflare Pages..."
	@if ! command -v wrangler >/dev/null 2>&1; then echo 'wrangler not found. Install with: npm i -g wrangler'; exit 1; fi
	@CF_PAGES_PROJECT=$${CF_PAGES_PROJECT:-servify-website}; \
	echo "Using project name: $$CF_PAGES_PROJECT"; \
	wrangler pages deploy apps/website --project-name "$$CF_PAGES_PROJECT"
