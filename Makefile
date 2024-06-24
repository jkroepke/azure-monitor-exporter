##
# Console Colors
##
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
WHITE  := $(shell tput -Txterm setaf 7)
CYAN   := $(shell tput -Txterm setaf 6)
RESET  := $(shell tput -Txterm sgr0)

# renovate: github=golangci/golangci-lint
GO_LINT_CI_VERSION := v1.59.0

##
# Targets
##
.PHONY: help
help: ## show this help.
	@echo 'Usage:'
	@echo '  ${GREEN}make${RESET} ${YELLOW}<target>${RESET}'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} { \
		if (/^[a-zA-Z_-]+:.*?##.*$$/) {printf "  ${GREEN}%-21s${YELLOW}%s${RESET}\n", $$1, $$2} \
		else if (/^## .*$$/) {printf "  ${CYAN}%s${RESET}\n", substr($$1,4)} \
		}' $(MAKEFILE_LIST) | sort

.PHONY: clean
clean: ## clean builds dir
	@rm -rf azure-monitor-exporter azure-monitor-exporter.exe dist/

.PHONY: check
check: test lint golangci ## Run all checks locally

.PHONY: update
update: ## Run dependency updates
	@go get -u ./...
	@go mod tidy

.PHONY: build
ifeq ($(OS),Windows_NT)
build: clean azure-monitor-exporter.exe  ## Build azure-monitor-exporter
else
build: clean azure-monitor-exporter
endif


azure-monitor-exporter:
	@go build -o azure-monitor-exporter .

azure-monitor-exporter.exe:
	@go build -o azure-monitor-exporter.exe .

.Phony: build-debug
build-debug: ## Build azure-monitor-exporter with debug flags
	@go build -gcflags="-l=4 -m=2" -o azure-monitor-exporter .

.PHONY: test
test:  ## Test azure-monitor-exporter
	@go test -race ./...

.PHONY: lint
lint: golangci  ## Run linter

.PHONY: format
format: fmt goimports gogci gofumpt gowsl goperfsprint golangci-fix ## Format source code

.PHONY: fmt
fmt:
	@go fmt ./...

.PHONY: gogci
gogci:
	@-go run github.com/daixiang0/gci@latest write .

.PHONY: gofumpt
gofumpt:
	@-go run mvdan.cc/gofumpt@latest -l -w .

.PHONY: goimports
goimports:
	@-go run golang.org/x/tools/cmd/goimports@latest -l -w .

.PHONY: gowsl
gowsl:
	@-go run github.com/bombsimon/wsl/v4/cmd...@latest -strict-append -test=true -fix ./...

.PHONY: goperfsprint
goperfsprint:
	@-go run github.com/catenacyber/perfsprint@latest -fix ./...

.PHONY: golangci
golangci:
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@${GO_LINT_CI_VERSION} run ./...

.PHONY: golangci-fix
golangci-fix:
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@${GO_LINT_CI_VERSION} run ./... --fix

.PHONY: 3rdpartylicenses
3rdpartylicenses:
	@go run github.com/google/go-licenses@latest save . --save_path=3rdpartylicenses