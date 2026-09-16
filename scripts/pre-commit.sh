#!/usr/bin/env bash
# novel2all-go pre-commit 检查脚本（手动版）
#
# 用法：
#   ./scripts/pre-commit.sh           # 跑所有检查
#   ./scripts/pre-commit.sh --fix      # 跑 + 自动修复
#
# 与 .githooks/pre-commit 逻辑一致，但不依赖 git hook 配置
# 适合：
#   - 临时手动验证（commit 前）
#   - CI pipeline 复用
#   - 不想定 git core.hooksPath 的开发者

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "${SCRIPT_DIR}/.." && pwd )"
cd "${ROOT_DIR}"

FIX_MODE=false
if [[ "${1:-}" == "--fix" ]]; then
    FIX_MODE=true
fi

echo -e "${GREEN}=== novel2all-go pre-commit ===${NC}"
echo "ROOT: ${ROOT_DIR}"
echo "FIX:  ${FIX_MODE}"
echo ""

# 1. secrets 检查
echo "[1/5] Checking for secrets..."
SECRET_PATTERN='(secret|password|api[_-]?key|token).*=.*["\x27][a-zA-Z0-9_-]{16,}'
LEAKED=$(git diff --diff-filter=ACM 2>/dev/null | grep -iE "${SECRET_PATTERN}" || true)
if [[ -n "${LEAKED}" ]]; then
    echo -e "${RED}✗ 检测到疑似 secret:${NC}"
    echo "${LEAKED}"
    echo "  移到 configs/.env 或 git commit --no-verify"
    exit 1
fi

# 2. go fmt
echo "[2/5] go fmt..."
UNFORMATTED=$(gofmt -l . 2>/dev/null || true)
if [[ -n "${UNFORMATTED}" ]]; then
    if [[ "${FIX_MODE}" == "true" ]]; then
        echo -e "${YELLOW}  自动 gofmt -w ...${NC}"
        gofmt -w "${UNFORMATTED}"
    else
        echo -e "${RED}✗ 以下文件未 gofmt:${NC}"
        echo "${UNFORMATTED}"
        echo "  修复: ./scripts/pre-commit.sh --fix"
        exit 1
    fi
fi

# 3. go vet
echo "[3/5] go vet ./..."
go vet ./...

# 4. go test
echo "[4/5] go test ./..."
go test ./...

# 5. golangci-lint
echo "[5/5] golangci-lint..."
if command -v golangci-lint >/dev/null 2>&1; then
    if [[ "${FIX_MODE}" == "true" ]]; then
        golangci-lint run --fix --timeout=5m
    else
        golangci-lint run --timeout=5m
    fi
else
    echo -e "${YELLOW}  golangci-lint 未安装,跳过（安装: make install-lint）${NC}"
fi

echo ""
echo -e "${GREEN}=== All checks passed ===${NC}"