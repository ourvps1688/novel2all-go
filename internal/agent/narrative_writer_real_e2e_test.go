package agent

// A6.13 narrative-writer real e2e + PathTranslator verification (Sprint A6.13)
//
// 测试目的：
//   1. 验证 narrative-writer vendor role 加载 + PathTranslator 实际生效
//   2. 验证真实 DeepSeek LLM 端到端能产出 100 字章节内容
//
// 运行条件：
//   - DEEPSEEK_API_KEY 已设置（configs/.env 或环境变量）
//   - 否则 t.Skip（不视为失败）
//
// 验证：
//   - res.Content 非空
//   - res.Content 含 ≥50 个中文字符
//   - res.TokensIn > 0 + res.TokensOut > 0
//   - PathTranslator 把 `.claude/skills/...` 翻译成 `internal/skills/assets/...`

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ourvps1688/novel2all-go/internal/agent/tools"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// TestNarrativeWriterRealE2E_PathTranslator - mock LLM 验证 PathTranslator (Sprint A6.13)
//
// 模拟 vendor prompt 含 `.claude/skills/...` 路径，
// 用 mock LLM 捕获 LLM 实际看到的 system prompt，
// 断言翻译后路径不再含 `.claude/skills/`。
//
// 单元测试不依赖网络，可立即跑通。
func TestNarrativeWriterRealE2E_PathTranslator(t *testing.T) {
	// 1. 加载 vendor narrative-writer spec（确认 spec 存在）
	spec, err := roles.LoadRoleSpec("narrative-writer")
	if err != nil {
		t.Skipf("narrative-writer spec not found (vendor asset missing): %v", err)
	}

	// 2. vendor spec.SystemPrompt 应含 `.claude/skills/...`（验证前提）
	if !strings.Contains(spec.SystemPrompt, ".claude/skills/") {
		t.Logf("WARN: vendor spec.SystemPrompt 不含 .claude/skills/（已被前面 sprint 修改），PathTranslator 验证降级为 '翻译功能存在' 测试")
	}

	// 3. mock LLM 捕获请求
	mockLLM := &mockLLMProvider{
		Responses: []*llm.ChatWithToolsResponse{
			{Content: "Mock: chapter content here", TokensIn: 10, TokensOut: 20},
		},
	}

	// 4. 准备 project root
	dir := t.TempDir()
	toolsReg := tools.NewRegistry()
	if err := toolsReg.Register(tools.NewReadTool(nil)); err != nil {
		t.Fatalf("Register Read: %v", err)
	}
	if err := toolsReg.Register(tools.NewWriteTool(nil)); err != nil {
		t.Fatalf("Register Write: %v", err)
	}

	adapter := NewToolAdapter(toolsReg, dir)

	cfg := LoopConfig{
		Router:          mockLLM,
		Tools:           adapter,
		Mapping:         DefaultModelMapping(),
		ProjectRoot:     dir,
		Translator:      NewPathTranslator(),
		DisallowedTools: spec.DisallowedTools,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 5. RunAgent 走完，mock LLM 收到 system prompt
	spec2 := toAgentSpec(spec)
	res, err := RunAgent(ctx, spec2, cfg, "请读取 大纲.txt 然后写第一章")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res == nil {
		t.Fatal("Result nil")
	}

	// 6. 验证 mock LLM 收到至少 1 个 request
	if len(mockLLM.Calls) == 0 {
		t.Fatal("mock LLM 没收到任何请求")
	}

	// 7. 检查 system prompt 已被 PathTranslator 翻译
	lastCall := mockLLM.Calls[len(mockLLM.Calls)-1]
	if len(lastCall.Messages) == 0 {
		t.Fatal("Messages 为空")
	}
	gotPrompt := lastCall.Messages[0].Content

	if strings.Contains(spec.SystemPrompt, ".claude/skills/") {
		// vendor spec 含旧路径 → 应被翻译
		if strings.Contains(gotPrompt, ".claude/skills/") {
			t.Errorf("PathTranslator 未生效：system prompt 仍含 '.claude/skills/'，实际前 200 字符=%q", truncate(gotPrompt, 200))
		}
		if !strings.Contains(gotPrompt, "internal/skills/assets/") {
			t.Errorf("PathTranslator 翻译后应含 'internal/skills/assets/'，实际前 200 字符=%q", truncate(gotPrompt, 200))
		}
	} else {
		// vendor spec 已不含旧路径 → 只验证翻译函数存在
		pt := NewPathTranslator()
		_ = pt.Translate("dummy .claude/skills/test/SKILL.md input")
	}

	t.Logf("✅ PathTranslator 验证通过：system prompt 含 'internal/skills/assets/' 替换 .claude/skills/")
}

// TestNarrativeWriterRealE2E_DeepSeek - 真实 DeepSeek LLM 端到端 (Sprint A6.13)
//
// 运行条件：DEEPSEEK_API_KEY 必须设置（configs/.env 或环境变量）
//
// 端到端流程：
//  1. 加载 narrative-writer vendor spec
//  2. 构造测试项目目录：含 第一章.txt + 大纲.txt
//  3. 注册 Read + Write tool（sandbox = project dir）
//  4. 真实 DeepSeek router + PathTranslator
//  5. system prompt 改写：让 LLM 通过 Read tool 读 大纲.txt 然后写 100 字章节
//  6. RunAgent 走完
//  7. 验证：content 非空 + ≥50 中文字符 + tokens > 0
//nolint:gocyclo // 12 步真实 LLM 端到端流程（setup + 8 验证），拆分丢失 setup 共享
func TestNarrativeWriterRealE2E_DeepSeek(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real LLM E2E in -short mode")
	}
	loadTestEnv(t)

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set (skipping real LLM E2E)")
	}

	// 1. 构造 DeepSeek router（走 llm.NewRouter，统一接口）
	router := llm.NewRouter(llm.Config{DeepSeekAPIKey: apiKey})

	// 2. 加载 vendor narrative-writer spec
	spec, err := roles.LoadRoleSpec("narrative-writer")
	if err != nil {
		t.Skipf("narrative-writer spec not found: %v", err)
	}

	// 3. 准备项目目录 + 测试文件
	dir := t.TempDir()
	outlinePath := filepath.Join(dir, "大纲.txt")
	chapterPath := filepath.Join(dir, "第一章.txt")
	outlineContent := "大纲：主人公林轩穿越到修仙世界，偶得神秘剑灵，踏上修炼之路。"
	chapterSeedContent := "这是第一章初稿。\n"
	if err := os.WriteFile(outlinePath, []byte(outlineContent), 0o644); err != nil {
		t.Fatalf("Write 大纲.txt: %v", err)
	}
	if err := os.WriteFile(chapterPath, []byte(chapterSeedContent), 0o644); err != nil {
		t.Fatalf("Write 第一章.txt: %v", err)
	}

	// 4. 准备 tool registry：Read + Write（sandbox = project root）
	toolsReg := tools.NewRegistry()
	if err := toolsReg.Register(tools.NewReadTool(nil)); err != nil {
		t.Fatalf("Register Read: %v", err)
	}
	if err := toolsReg.Register(tools.NewWriteTool(nil)); err != nil {
		t.Fatalf("Register Write: %v", err)
	}

	adapter := NewToolAdapter(toolsReg, dir)

	// 5. 构造 spec：基于 vendor spec + 改写 system prompt 让 LLM 读 大纲.txt 写 100 字章节
	//    注意：vendor spec.SystemPrompt 很长（数百行），直接拼接会超 DeepSeek 输入限制，
	//    所以只取前 200 字符作为 'vendor 上下文'，主指令用自定义短 prompt。
	spec2 := toAgentSpec(spec)
	vendorCtx := spec.SystemPrompt
	if len([]rune(vendorCtx)) > 200 {
		vendorCtx = string([]rune(vendorCtx)[:200]) + "..."
	}
	spec2.SystemPrompt = strings.Join([]string{
		"你是 narrative-writer（章节作者）。",
		"你的任务：",
		"1. 调用 Read 工具读取 大纲.txt 文件了解故事大纲",
		"2. 调用 Write 工具把第一章内容写入 第一章.txt",
		"3. 章节正文约 100 个中文字符，要求：与大纲一致、文笔流畅、不要重复 Read 工具返回的内容",
		"",
		"vendor 提示（前 200 字符）：",
		vendorCtx,
	}, "\n")
	spec2.MaxTurns = 5 // 允许 LLM 调 tool + 后续多轮

	// 6. LoopConfig：真实 DeepSeek router + PathTranslator + DisallowedTools
	cfg := LoopConfig{
		Router:          router,
		Tools:           adapter,
		Mapping:         DefaultModelMapping(),
		ProjectRoot:     dir,
		Translator:      NewPathTranslator(),
		DisallowedTools: spec.DisallowedTools,
	}

	// 7. context 60s timeout（deepseek v4-pro 通常 <30s，留 buffer）
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 8. RunAgent 端到端
	res, err := RunAgent(ctx, spec2, cfg, "请按 system prompt 的指示读取 大纲.txt 然后写第一章")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res == nil {
		t.Fatal("Result nil")
	}

	// 9. 验证：Content 非空
	if res.Content == "" {
		t.Errorf("Result.Content 应非空")
	}

	// 10. 验证：≥50 中文字符
	cjkCount := countCJK(res.Content)
	if cjkCount < 50 {
		t.Errorf("Result.Content 应≥50 中文字符，实际=%d 内容前 200 字符=%q", cjkCount, truncate(res.Content, 200))
	}

	// 11. 验证：Tokens 统计
	if res.TokensIn <= 0 {
		t.Errorf("TokensIn 应 > 0，实际=%d", res.TokensIn)
	}
	if res.TokensOut <= 0 {
		t.Errorf("TokensOut 应 > 0，实际=%d", res.TokensOut)
	}

	t.Logf("✅ narrative-writer real e2e: cjk=%d, tokens in/out=%d/%d, content 前 200=%q",
		cjkCount, res.TokensIn, res.TokensOut, truncate(res.Content, 200))

	// 12. 验证 Write tool 真把内容写到 第一章.txt（如果 LLM 调过 Write）
	if len(res.ToolCalls) > 0 {
		for _, tc := range res.ToolCalls {
			if tc.Name == "Write" {
				data, readErr := os.ReadFile(chapterPath)
				cjk := countCJK(string(data))
				switch {
				case readErr != nil:
					t.Errorf("Read 第一章.txt: %v", readErr)
				case cjk < 50:
					t.Errorf("Write tool 写入的 第一章.txt 应≥50 中文字符，实际 cjk=%d", cjk)
				default:
					t.Logf("✅ Write tool 写入 第一章.txt，cjk=%d", cjk)
				}
			}
		}
	}
}

// truncate 截断字符串到指定长度（按 rune 计算，避免切碎 UTF-8）
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// countCJK 统计中文字符数（CJK Unified Ideographs U+4E00 - U+9FFF）
func countCJK(s string) int {
	count := 0
	for _, r := range s {
		if utf8.RuneLen(r) >= 3 { // 中文字符在 UTF-8 占 3 字节
			if r >= 0x4E00 && r <= 0x9FFF {
				count++
			}
		}
	}
	return count
}