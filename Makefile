# noemx21-bot
BINARY		:= noemx21-bot
BUILD_PKG	:= ./cmd/noemx21-bot
LDFLAGS := -s -w

LOCAL_BIN := $(CURDIR)/bin

GOLANGCI_VERSION 	:= v2.11.4		## https://github.com/golangci/golangci-lint/releases
GOVULN_VERSION		:= v1.2.0		## https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck?tab=versions
SQLC_VERSION     	:= v1.30.0		## https://github.com/sqlc-dev/sqlc/releases
MIGRATE_VERSION  	:= v4.19.1		## https://github.com/golang-migrate/migrate/releases
MOCKGEN_VERSION  	:= v0.6.0		## https://github.com/uber-go/mock/releases/tag/v0.6.0

GOLANGCI 	:= $(LOCAL_BIN)/golangci-lint
GOVULN 		:= $(LOCAL_BIN)/govulncheck
SQLC     	:= $(LOCAL_BIN)/sqlc
MIGRATE  	:= $(LOCAL_BIN)/migrate
MOCKGEN  	:= $(LOCAL_BIN)/mockgen

ifneq (,$(wildcard env/.env))
    include env/.env
	export $(shell sed 's/=.*//' env/.env)
endif

DATABASE_URL	?= $(shell cat env/database_url 2>/dev/null)
MIGRATIONS_DIR	:= internal/database/migrations

.PHONY: \
	help \
	build run release \
	test cover \
	lint fmt vet security \
	docs-c4 docs-schema docs-diagrams \
	generate sqlc mockgen \
	migrate-up migrate-down migrate-create \
	docker-build docker-save \
	deps tidy verify \
	clean clean-tools \
	ci-check

# =========================
# Build & Run
# =========================
build: deps		## Build binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(BUILD_PKG)

run: build		## Run binary
	./$(BINARY)

# =========================
# Tests
# =========================

test:		## Run tests
	go test -race -cover ./...

test-integration: ## Run integration tests (requires Docker)
	go test -tags=integration ./...

cover:		## Coverage HTML report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# =========================
# Quality
# =========================

lint: $(GOLANGCI)		## Run linter
	$(GOLANGCI) run ./...

fmt: tidy		## Run go fmt and go fix
	go fix ./...
	gofmt -s -w .

vet:		## go vet
	go vet ./...

security: $(GOVULN)		## Vulnerability scan
	$(GOVULN) ./...

yaml:		## Lint YAML files
	yamllint . 2> /dev/null || docker run --rm -v "$(shell pwd):/data" cytopia/yamllint:latest . || echo "yamllint not found"

ci: .github/workflows/ci.yml		## Run CI pipeline locally (act)
	act push -P ubuntu-22.04=catthehacker/ubuntu:act-22.04

ci-check: fmt vet lint test security yaml		## Full CI check locally

# =========================
# Docs
# =========================

docs-c4:		## Generate C4 SVG diagrams into docs/specs/system/c4
	mkdir -p docs/specs/system/c4
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v "$(CURDIR):/workspace" -w /workspace plantuml/plantuml:latest -tsvg -o ../specs/system/c4 docs/c4/*.puml; \
	elif command -v plantuml >/dev/null 2>&1; then \
		plantuml -tsvg -o ../specs/system/c4 docs/c4/*.puml; \
	else \
		echo "docs-c4 requires either docker or plantuml in PATH"; \
		exit 1; \
	fi

docs-schema:		## Generate docs/schema.svg from docs/schema.puml
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v "$(CURDIR):/workspace" -w /workspace plantuml/plantuml:latest -tsvg docs/schema.puml; \
	elif command -v plantuml >/dev/null 2>&1; then \
		plantuml -tsvg docs/schema.puml; \
	else \
		echo "docs-schema requires either docker or plantuml in PATH"; \
		exit 1; \
	fi

docs-diagrams: docs-c4 docs-schema		## Generate all PlantUML SVG diagrams

# =========================
# Code generation
# =========================

generate: sqlc mockgen		## Generate all code

sqlc: $(SQLC)		## Generate SQL code
	$(SQLC) generate

mockgen: $(MOCKGEN)		## Generate mocks
	$(MOCKGEN) \
		-source=internal/database/db/querier.go \
		-destination=internal/database/db/mock/querier_mock.go \
		-package=mock

# =========================
# Migrations (BINARY or CLI)
# =========================

migrate-up:		## Apply migrations (embedded binary)
	./$(BINARY) -migrate

migrate-down:		## Rollback last migration (embedded binary)
	./$(BINARY) -migrate-rollback

migrate-status:		## Show migration status (embedded binary)
	./$(BINARY) -migrate-status

migrate-cli-up: $(MIGRATE)		## Apply migrations (golang-migrate CLI)
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-cli-down: $(MIGRATE)		## Rollback migrations (golang-migrate CLI)
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1                   

migrate-create: $(MIGRATE)		## Create migration (name=init)
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)

# =========================
# Help
# =========================

help:		## Show this help
	@grep -h -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

# =========================
# Docker
# =========================

docker-build:		## Build docker image
	docker build -t $(BINARY):local .

docker-save:		## Save docker image tar
	docker save $(BINARY):local -o $(BINARY).tar

# =========================
# Go modules
# =========================

deps:		## Download modules
	go mod download

tidy:		## Tidy modules
	go mod tidy

verify:		## Verify modules
	go mod verify

# =========================
# Tool installation
# =========================

$(LOCAL_BIN):
	mkdir -p $(LOCAL_BIN)

$(GOLANGCI): | $(LOCAL_BIN)
	@echo "Installing golangci-lint $(GOLANGCI_VERSION)..."
	@curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(LOCAL_BIN) $(GOLANGCI_VERSION)

$(GOVULN): | $(LOCAL_BIN)
	@echo "Installing govulncheck $(GOVULN_VERSION)..."
	GOBIN=$(LOCAL_BIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULN_VERSION)

$(SQLC): | $(LOCAL_BIN)
	@echo "Installing sqlc $(SQLC_VERSION)..."
	GOBIN=$(LOCAL_BIN) go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

$(MIGRATE): | $(LOCAL_BIN)
	@echo "Installing migrate $(MIGRATE_VERSION)..."
	GOBIN=$(LOCAL_BIN) go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

$(MOCKGEN): | $(LOCAL_BIN)
	@echo "Installing mockgen $(MOCKGEN_VERSION)..."
	GOBIN=$(LOCAL_BIN) go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)

# =========================
# Cleanup
# =========================

clean:		## Remove build artifacts
	rm -rf $(BINARY) $(BINARY).tar coverage.out coverage.html

clean-tools:		## Remove local tools
	rm -rf $(LOCAL_BIN)
