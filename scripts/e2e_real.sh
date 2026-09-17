#!/usr/bin/env bash
# scripts/e2e_real.sh - 连真实 LLM 跑 E2E 测试 (Sprint 36).
#
# 前置:
#   - .env 或 configs/.env 配 DEEPSEEK_API_KEY (或其他 provider)
#   - go 1.22+ 已装
#
# 跑法:
#   ./scripts/e2e_real.sh         # 默认: 短篇 1 + 长篇 1
#   ./scripts/e2e_real.sh long    # 只跑长篇
#   ./scripts/e2e_real.sh short   # 只跑短篇

set -euo pipefail

cd "$(dirname "$0")/.."

# 加载 .env
if [ -f .env ]; then
    set -a
    # shellcheck disable=SC1091
    source .env
    set +a
fi
if [ -f configs/.env ]; then
    set -a
    # shellcheck disable=SC1091
    source configs/.env
    set +a
fi

# 检查 API key
if [ -z "${DEEPSEEK_API_KEY:-}" ] && [ -z "${DASHSCOPE_API_KEY:-}" ] && [ -z "${MINIMAX_API_KEY:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ]; then
    echo "ERROR: 需要至少一个 provider 的 API key (DEEPSEEK_API_KEY/DASHSCOPE_API_KEY/MINIMAX_API_KEY/ANTHROPIC_API_KEY)"
    echo "设置方法: 编辑 .env 或 configs/.env"
    exit 1
fi

MODE="${1:-all}"
TIMEOUT="${E2E_TIMEOUT:-120s}"
PROJECT_DIR=$(mktemp -d)
echo "Project dir: ${PROJECT_DIR}"

# Setup mock_project via testfixture
echo "==> 1. 初始化项目结构..."
mkdir -p "${PROJECT_DIR}/设定/世界观" "${PROJECT_DIR}/大纲" "${PROJECT_DIR}/正文"
cat > "${PROJECT_DIR}/设定/文风.md" <<'EOF'
# 文风

第一人称, 短句为主, 避免翻译腔, 重视场景描写.
EOF
cat > "${PROJECT_DIR}/设定/创作设定.md" <<'EOF'
# 创作设定

类型: 玄幻
主角: 林雷
风格: 升级流
EOF
cat > "${PROJECT_DIR}/设定/世界观/地图.md" <<'EOF'
# 地图

九州大陆.
EOF
cat > "${PROJECT_DIR}/大纲/细纲_第001章.md" <<'EOF'
# 第 1 章 细纲

林雷被师尊逐出山门, 意外获得禁忌功法.
EOF

# 跑短篇 (CompileShortStory 8 节 pipeline)
if [ "$MODE" = "all" ] || [ "$MODE" = "short" ]; then
    echo "==> 2. 跑短篇 (CompileShortStory, 8 节 pipeline)..."
    timeout "$TIMEOUT" go run ./cmd/cli/ stories short "林雷被逐出山门, 获得禁忌功法" \
        --project-root "${PROJECT_DIR}" \
        --provider "${E2E_PROVIDER:-deepseek}" 2>&1 | tee "${PROJECT_DIR}/short_run.log" || {
        echo "WARN: 短篇跑失败 (timeout 或 LLM 错误), 检查 ${PROJECT_DIR}/short_run.log"
    }
fi

# 跑长篇 (handleStream 1 章)
if [ "$MODE" = "all" ] || [ "$MODE" = "long" ]; then
    echo "==> 3. 跑长篇第 1 章 (handleStream + memory)..."
    timeout "$TIMEOUT" curl -sN "http://localhost:8000/api/write/stream?chapter=1&project_root=${PROJECT_DIR}&min_chars=500&skill=story-long-write" \
        --max-time 120 2>&1 | tee "${PROJECT_DIR}/long_run.log" || {
        echo "WARN: 长篇需要先启动 server (make web 或 ./novel2all web), 否则此步会失败"
        echo "启动方法: cd ${PROJECT_DIR} && ../novel2all web &"
    }
fi

echo "==> 4. 报告:"
echo "    Project: ${PROJECT_DIR}"
echo "    文风: ${PROJECT_DIR}/设定/文风.md"
echo "    跟踪状态: ${PROJECT_DIR}/.novel2all/_tracking-state.json (如更新)"
ls -la "${PROJECT_DIR}" 2>&1 || true
echo ""
echo "Sprint 36 E2E real LLM 跑完成"
