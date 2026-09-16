# novel2all-go Makefile
# 用法：
#   make help        查看所有命令
#   make build       构建二进制
#   make test        跑测试
#   make lint        跑 golangci-lint
#   make verify      build + vet + test + lint（CI 镜像）
#   make dev         构建 + 跑服务器（需 API keys in configs/.env）
#
# CI 与本地都用同一组 target，保证结果一致。

GO          ?= go
GOLANGCI    ?= golangci-lint
BIN_DIR     := bin
BINARY      := $(BIN_DIR)/novel2all.exe
MAIN_PKG    := ./cmd/server

# 颜色输出（Windows MSYS 支持）
GREEN  := \033[0;32m
YELLOW := \033[0;33m
RED    := \033[0;31m
NC     := \033[0m

.PHONY: help
help: ## 显示帮助
	@echo "$(GREEN)novel2all-go Makefile$(NC)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""

.PHONY: deps
deps: ## 检查 Go 版本
	@go version

.PHONY: build
build: ## 构建二进制到 bin/novel2all.exe
	@mkdir -p $(BIN_DIR)
	@echo "$(GREEN)Building $(BINARY)...$(NC)"
	@$(GO) build -o $(BINARY) $(MAIN_PKG)
	@echo "$(GREEN)✓ Build OK: $(BINARY) ($(shell du -h $(BINARY) | cut -f1))$(NC)"

.PHONY: vet
vet: ## go vet ./...
	@echo "$(GREEN)Running go vet...$(NC)"
	@$(GO) vet ./...

.PHONY: test
test: ## 跑单元测试
	@echo "$(GREEN)Running tests...$(NC)"
	@$(GO) test -v ./...

.PHONY: test-race
test-race: ## 跑带 -race 的测试（Linux/Mac）
	@echo "$(GREEN)Running tests with -race...$(NC)"
	@CGO_ENABLED=1 $(GO) test -race ./...

.PHONY: test-coverage
test-coverage: ## 跑测试 + 覆盖率
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	@$(GO) test -coverprofile=coverage.out -covermode=atomic ./...
	@$(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: ## 跑 golangci-lint（与 CI 一致）
	@echo "$(GREEN)Running golangci-lint...$(NC)"
	@$(GOLANGCI) run --timeout=5m

.PHONY: lint-fix
lint-fix: ## 跑 golangci-lint 自动修复
	@echo "$(YELLOW)Running golangci-lint --fix...$(NC)"
	@$(GOLANGCI) run --fix --timeout=5m

.PHONY: fmt
fmt: ## gofmt 格式化
	@echo "$(GREEN)Formatting Go files...$(NC)"
	@$(GO) fmt ./...

.PHONY: tidy
tidy: ## go mod tidy
	@echo "$(GREEN)Running go mod tidy...$(NC)"
	@$(GO) mod tidy

.PHONY: verify
verify: build vet test lint ## 全套验证（CI 镜像）
	@echo "$(GREEN)✓ All checks passed$(NC)"

.PHONY: dev
dev: build ## 构建 + 启动服务器（前台）
	@echo "$(GREEN)Starting server...$(NC)"
	@./$(BINARY)

.PHONY: dev-bg
dev-bg: build ## 构建 + 启动服务器（后台）
	@echo "$(GREEN)Starting server in background...$(NC)"
	@./$(BINARY) > /tmp/novel2all.log 2>&1 &
	@echo "PID: $$!"
	@sleep 2
	@curl -s http://localhost:8000/health && echo ""

.PHONY: clean
clean: ## 清理构建产物
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf $(BIN_DIR) coverage.out
	@$(GO) clean -cache -testcache

.PHONY: install-lint
install-lint: ## 安装 golangci-lint v1.61.0
	@echo "$(GREEN)Installing golangci-lint v1.61.0...$(NC)"
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0
	@echo "$(GREEN)✓ Installed to $$($(GO) env GOPATH)/bin/$(NC)"

.PHONY: ci-local
ci-local: ## 模拟 CI 完整流程（无 lint，因为 Windows cgo 默认关）
	@echo "$(GREEN)=== CI Local ===$(NC)"
	@$(GO) mod verify
	@$(GO) build ./...
	@$(GO) vet ./...
	@$(GO) test ./...
	@$(MAKE) lint