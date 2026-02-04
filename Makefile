# noemx21-bot
BINARY		:= noemx21-bot
BUILD_PKG	:= ./cmd/noemx21-bot
GOFLAGS		?=
SQLC		:= "$(shell go env GOPATH)/bin/sqlc"
MIGRATE		:= "$(shell go env GOPATH)/bin/migrate"
MIGRATIONS_DIR	:= internal/database/migrations
DATABASE_URL	?= "$(shell cat env/database_url 2>/dev/null)"
GOLANGCI	:= "$(shell go env GOPATH)/bin/golangci-lint"
GO_SRCS		:= $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: help build test lint vet security yaml lint-deps deps tidy ci clean clean_gcov generate sqlc migrate-up migrate-down migrate-create migrate-deps

generate: sqlc		## Generate code (sqlc, mocks, etc)
	@go generate ./... 2>/dev/null || true

sqlc:			## Generate SQL code with sqlc
	$(SQLC) generate

migrate-up: migrate-deps ## Run database migrations up
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database $(DATABASE_URL) up

migrate-down: migrate-deps ## Run database migrations down
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database $(DATABASE_URL) down 1

migrate-create: migrate-deps ## Create a new migration (usage: make migrate-create name=migration_name)
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)

migrate-deps:		## Install golang-migrate if missing
	@command -v $(MIGRATE) >/dev/null 2>&1 || go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

help:		## Show this help
	@grep -h -E '^[a-zA-Z0-9_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

$(BINARY): go.mod go.sum $(GO_SRCS)
	go build $(GOFLAGS) -o $@ $(BUILD_PKG)

build: deps $(BINARY)		## Build binary

docker-build:		## Build docker image
	docker build -t $(BINARY):local .

docker-save:		## Save docker image
	docker save $(BINARY):local -o $(BINARY).tar

test: deps		## Run tests with race and coverage
	go test -race -cover ./...

vet: deps		## Run go vet
	go vet ./...

lint: lint-deps		## Run golangci-lint
	$(GOLANGCI) run ./...

lint-deps:		## Install golangci-lint if missing
	@command -v $(GOLANGCI) >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

security: deps		## Run govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

yaml:		## Lint YAML files
	yamllint . 2> /dev/null || docker run --rm -v "$(shell pwd):/data" cytopia/yamllint:latest . || echo "yamllint not found"

deps: go.mod go.sum		## Download Go modules
	go mod download

tidy:		## Tidy Go modules
	go mod tidy

ci: .github/workflows/ci.yml		## Run CI pipeline locally (act)
	act push -P ubuntu-22.04=catthehacker/ubuntu:act-22.04

clean: clean_gcov		## Remove build artifacts
	rm -rf $(BINARY) $(BINARY).tar

clean_gcov:		## Remove coverage artifacts
	rm -f *.out coverage.* *.coverprofile profile.cov
