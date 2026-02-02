# noemx21-bot
BINARY		:= noemx21-bot
BUILD_PKG	:= ./cmd/noemx21-bot
GOFLAGS		?=
GOLANGCI	:= $(shell go env GOPATH)/bin/golangci-lint
GO_SRCS		:= $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: help build test lint vet security yaml lint-deps deps tidy ci clean clean_gcov generate

generate:		## Generate code (mocks, etc)
	go generate ./...

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
	yamllint -d "{extends: default, rules: {line-length: disable, truthy: disable}}" . 2> /dev/null || docker run --rm -v $(shell pwd):/data cytopia/yamllint:latest -d "{extends: default, rules: {line-length: disable, truthy: disable}}" . || echo "yamllint not found"

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
