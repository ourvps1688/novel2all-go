// write 子命令: 单次写作生成.
//
// 用法:
//
//	novel2all write --task=WRITING --prompt "你的写作 prompt"
//
// flags:
//   - --config   .env 路径 (默认 configs/.env, 空字符串跳过)
//   - --task     LLM TaskType (WRITING | CONSISTENCY | EXTRACTION | SUMMARIZATION | COVER), 默认 WRITING
//   - --prompt   用户 prompt (必填)
//   - --provider 强制 provider (空 = 路由)
//   - --model    强制 model (空 = 路由)
//   - --max-tokens  最大输出 token (0 = provider 默认)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/config"
	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// runWrite 执行 write 子命令.
//
// 流程:
//  1. 加载 config (.env)
//  2. 构造 llm.Router
//  3. Router.Chat 调 LLM
//  4. 输出 Response.Content 到 stdout (元信息到 stderr)
func runWrite(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("write", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "configs/.env", "path to .env file (empty to skip)")
	task := fs.String("task", "WRITING", "LLM task type (WRITING/CONSISTENCY/EXTRACTION/SUMMARIZATION/COVER)")
	prompt := fs.String("prompt", "", "user prompt (required)")
	provider := fs.String("provider", "", "force provider name (empty = use router)")
	model := fs.String("model", "", "force model name (empty = use router)")
	maxTokens := fs.Int("max-tokens", 0, "max output tokens (0 = provider default)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	if *prompt == "" {
		return fmt.Errorf("write: --prompt is required")
	}

	// 1. 加载 config
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 构造 router (无 metrics/cache, 纯 CLI)
	router := llm.NewRouter(llm.Config{
		DashScopeAPIKey: cfg.LLM.DashScopeAPIKey,
		DeepSeekAPIKey:  cfg.LLM.DeepSeekAPIKey,
		MinimaxAPIKey:   cfg.LLM.MinimaxAPIKey,
		AnthropicAPIKey: cfg.LLM.AnthropicAPIKey,
	})

	if len(router.AvailableProviders()) == 0 {
		return fmt.Errorf("no LLM provider configured (check API keys in %s)", *configPath)
	}

	// 3. 构造 Request
	req := llm.Request{
		Task: llm.TaskType(*task),
		Messages: []llm.Message{
			{Role: "user", Content: *prompt},
		},
		OverrideProvider: llm.ProviderName(*provider),
		OverrideModel:    *model,
		MaxTokens:        *maxTokens,
		Stream:           false, // CLI 默认非流式
	}

	// 4. 调 LLM
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	start := time.Now()
	resp, err := router.Chat(ctx, req)
	if err != nil {
		return fmt.Errorf("llm chat: %w", err)
	}
	duration := time.Since(start)

	// 5. 输出 (内容到 stdout, 元信息到 stderr)
	fmt.Fprintln(stdout, resp.Content)
	fmt.Fprintf(stderr, "---\nprovider=%s model=%s tokens_in=%d tokens_out=%d duration=%s\n",
		resp.Provider, resp.Model, resp.TokensIn, resp.TokensOut, duration.Round(time.Millisecond))

	return nil
}
