##
# Console Colors
##
GREEN  := $(shell echo -e "\033[0;32m")
YELLOW := $(shell echo -e "\033[0;33m")
WHITE  := $(shell echo -e "\033[0;37m")
CYAN   := $(shell echo -e "\033[0;36m")
RESET  := $(shell echo -e "\033[0m")

# renovate: github=golangci/golangci-lint
GO_LINT_CI_VERSION := v1.59.1

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


.PHONY: fmt  ## Format code
fmt:
	@go fmt ./...
	@-go run github.com/daixiang0/gci@latest write .
	@-go run mvdan.cc/gofumpt@latest -l -w .
	@-go run golang.org/x/tools/cmd/goimports@latest -l -w .
	@-go run github.com/bombsimon/wsl/v4/cmd...@latest -strict-append -test=true -fix ./...
	@-go run github.com/catenacyber/perfsprint@latest -fix ./...
	@-go run github.com/tetafro/godot/cmd/godot@latest -w .
	# @-go run go run github.com/ssgreg/nlreturn/v2/cmd/nlreturn@latest -fix ./...
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@${GO_LINT_CI_VERSION} run ./... --fix

.PHONY: golangci
golangci:
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@${GO_LINT_CI_VERSION} run ./...

.PHONY: 3rdpartylicenses
3rdpartylicenses:
	@go run github.com/google/go-licenses@latest save . --save_path=3rdpartylicenses