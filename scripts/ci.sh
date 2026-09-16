#!/usr/bin/env bash
# novel2all-go 本地 CI 脚本（与 .github/workflows/ci.yml 镜像）
# 用法：./scripts/ci.sh

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "${SCRIPT_DIR}/.." && pwd )"

cd "${ROOT_DIR}"

echo "==== novel2all-go CI ===="
echo "ROOT: ${ROOT_DIR}"
echo "GO:   $(go version 2>&1 || echo 'NOT INSTALLED')"
echo ""

echo "[1/5] go mod verify"
go mod verify

echo "[2/5] go build ./..."
go build -v ./...

echo "[3/5] go vet ./..."
go vet ./...

echo "[4/5] go test -race ./..."
go test -race -count=1 ./...

echo "[5/5] smoke test"
go build -o bin/novel2all ./cmd/server
./bin/novel2all &
SERVER_PID=$!
trap "kill ${SERVER_PID} 2>/dev/null || true" EXIT
sleep 2

curl -sf http://localhost:8000/health | python -m json.tool
curl -sf http://localhost:8000/version | python -m json.tool

echo ""
echo "==== CI PASS ===="