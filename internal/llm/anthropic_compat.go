package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicCompat 是 Anthropic 兼容协议 provider 的基类。
// minimax 必须用这个协议（其 Anthropic 端点是 /v1/messages）。
// anthropic 自身也用这个协议。
//
// Sprint A1.20 (2026-09-18): 支持 provider-specific 默认值注入
//   - MiniMax 用 UseMinimaxDefaults() 注入 temperature=1.0, top_p=0.95, thinking=off
//   - 其他 provider（dashscope / deepseek / anthropic）不注入，保持原协议行为
type AnthropicCompat struct {
	name    ProviderName
	apiKey  string
	baseURL string
	model   string
	client  *http.Client

	// Provider-specific 默认值（Sprint A1.20）
	defaultTemperature float64 // 0 = 不注入, 使用 Request.Temperature 或协议默认
	defaultTopP        float64 // 0 = 不注入
	defaultThinking    bool    // true = 注入 thinking={"type":"adaptive"}
}

func NewAnthropicCompat(name ProviderName, apiKey, baseURL, defaultModel string) *AnthropicCompat {
	return &AnthropicCompat{
		name:    name,
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   defaultModel,
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// UseMinimaxDefaults 注入 MiniMax-M3 专属默认值 (Sprint A1.20)
//
// MiniMax 官方推荐 (https://platform.minimax.cn/docs/api-reference/text-anthropic-api):
//   - temperature: 1.0  (推荐值)
//   - top_p:      0.95  (M3 默认值，M2.x 是 0.9)
//   - thinking:   默认关闭 (需显式 thinking={"type":"adaptive"} 才启用)
//
// 调用后所有通过该 provider 发出的请求都会注入这些默认值（除非 Request 显式覆盖）。
func (p *AnthropicCompat) UseMinimaxDefaults() {
	p.defaultTemperature = 1.0
	p.defaultTopP = 0.95
	p.defaultThinking = false
}

// HasMinimaxDefaults 返回是否已注入 MiniMax defaults (测试用)
func (p *AnthropicCompat) HasMinimaxDefaults() bool {
	return p.defaultTemperature == 1.0 && p.defaultTopP == 0.95
}

func (p *AnthropicCompat) Name() ProviderName { return p.name }

// Available 返回 provider 是否可用.
//
// Sprint V1.0.1 Module B.2: 优先看 context 里的 user key (per-request override),
// fallback 到 constructor key. 任一存在即视为可用.
//
// Available 不带 context 参数 — 没法检查 context. 如需精确判断, 调用方应:
//  1. Available() 检查 constructor key
//  2. middleware 已检查 header 才会进 context, Available=true 即可调用
//
// 实际生产: Available 返回 true 永远安全 (LLM 调用时会用 effective API key, 空字符串
// 会立即报错).
func (p *AnthropicCompat) Available() bool { return p.apiKey != "" }

// effectiveAPIKey 返回本次请求应使用的 API key.
//
// 优先级: context user key > constructor key (p.apiKey).
// 调用方: ChatStream / ChatWithTools 等所有需要 API key 的方法.
//
// 返回 "" 表示都没设置 — 调用 LLM provider 会立即 401/403.
func (p *AnthropicCompat) effectiveAPIKey(ctx context.Context) string {
	if userKey := APIKeyFromContext(ctx, p.name); userKey != "" {
		return userKey
	}
	return p.apiKey
}

// DefaultModel 返回 Anthropic 兼容 provider 的默认模型名
func (p *AnthropicCompat) DefaultModel() string { return p.model }

// APIBase 返回 Anthropic 兼容 provider 的 base URL
func (p *AnthropicCompat) APIBase() string { return p.baseURL }

// antRequest Anthropic Messages API 请求
type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Stream      bool         `json:"stream,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	TopP        float64      `json:"top_p,omitempty"`
}

type antMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type antResponse struct {
	Content []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// antRequestWithTools 含 tools 字段 (Sprint 34)
type antRequestWithTools struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []antToolMsg `json:"messages"`
	Tools       []antTool    `json:"tools,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	TopP        float64      `json:"top_p,omitempty"`
}

// antTool tool 定义 (Anthropic 用 input_schema 而非 parameters)
type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// antToolMsg 消息体支持 content blocks (Sprint 34)
type antToolMsg struct {
	Role    string          `json:"role"`
	Content []antContentBlk `json:"content"`
}

// antContentBlk content block (text 或 tool_use/tool_result)
type antContentBlk struct {
	Type      string          `json:"type"` // "text" | "tool_use" | "tool_result"
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`          // tool_use
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content2  any             `json:"content,omitempty"`     // tool_result
}

// antResponseWithTools response 含 tool_use (Sprint 34)
type antResponseWithTools struct {
	Content []antContentBlk `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// 流式事件分片（多 event type，我们只关心 content_block_delta）
type antStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// Chat 非流式
func (p *AnthropicCompat) Chat(ctx context.Context, req Request) (*Response, error) {
	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
	p.applyProviderDefaults(&req) // Sprint A1.20
	ar, err := toAntRequest(model, req)
	if err != nil {
		return nil, err
	}
	resp, err := p.do(ctx, ar, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, readErr := io.ReadAll(resp.Body)
		body := string(b)
		if readErr != nil {
			body = fmt.Sprintf("<read body failed: %v>", readErr)
		}
		return nil, fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, body)
	}

	var data antResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", p.name, err)
	}
	if len(data.Content) == 0 {
		return nil, fmt.Errorf("%s: no content", p.name)
	}

	// 拼接所有 type="text" 块（跳过 type="thinking" 块）
	// Sprint A1.20: MiniMax-M3 默认开启 thinking，响应可能含 thinking + text 块
	var contentText string
	for _, blk := range data.Content {
		if blk.Type == "text" {
			contentText += blk.Text
		}
	}
	return &Response{
		Content:   contentText,
		Provider:  p.name,
		Model:     model,
		TokensIn:  data.Usage.InputTokens,
		TokensOut: data.Usage.OutputTokens,
	}, nil
}

// ChatStream 流式
//
//nolint:gocyclo // SSE 流处理天然多分支（line 解析 + EOF + JSON unmarshal + select）
func (p *AnthropicCompat) ChatStream(ctx context.Context, req Request, ch chan<- Chunk) error {
	defer close(ch)

	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
	p.applyProviderDefaults(&req) // Sprint A1.20
	ar, err := toAntRequest(model, req)
	if err != nil {
		ch <- Chunk{Err: err}
		return err
	}
	resp, err := p.do(ctx, ar, true)
	if err != nil {
		ch <- Chunk{Err: err}
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, readErr := io.ReadAll(resp.Body)
		body := string(b)
		if readErr != nil {
			body = fmt.Sprintf("<read body failed: %v>", readErr)
		}
		err := fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, body)
		ch <- Chunk{Err: err}
		return err
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				ch <- Chunk{Done: true}
				return nil
			}
			ch <- Chunk{Err: err}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimPrefix(line, "data:")
		payload = strings.TrimSpace(payload)
		var ev antStreamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		// 只关心 content_block_delta 事件
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" {
			content := ev.Delta.Text
			if content != "" {
				select {
				case ch <- Chunk{Content: content}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		// message_stop 表示结束
		if ev.Type == "message_stop" {
			ch <- Chunk{Done: true}
			return nil
		}
	}
}

// ChatWithTools 一次调用 LLM (Anthropic 协议), 返回 assistant 文本 + 可能调用的 tool 列表.
//
// Sprint 34: 同 OpenAI.ChatWithTools, 但用 Anthropic tool_use 协议.
// Multi-turn 循环由 Router.ChatWithTools 控制.
func (p *AnthropicCompat) ChatWithTools(ctx context.Context, req ChatWithToolsRequest) (*ChatWithToolsResponse, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("anthropic compat %s: API key not configured", p.name)
	}
	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
	// 把 Provider 默认值（MiniMax defaults 等）应用到 Request 上
	// Sprint A1.20：必须先应用，再读 req.Temperature / req.TopP
	if p.defaultTemperature > 0 && req.Temperature == 0 {
		req.Temperature = p.defaultTemperature
	}
	if p.defaultTopP > 0 && req.TopP == 0 {
		req.TopP = p.defaultTopP
	}

	body := buildToolsRequest(model, req)
	resp, err := p.doWithTools(ctx, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, string(b))
	}

	var data antResponseWithTools
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", p.name, err)
	}
	return parseToolsResponse(p.name, model, data), nil
}

// buildToolsRequest 构造 ChatWithTools 的 Anthropic 请求体（拆分降低 ChatWithTools 圈复杂度）
func buildToolsRequest(model string, req ChatWithToolsRequest) antRequestWithTools {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	return antRequestWithTools{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      joinSystemMessages(req.Messages),
		Messages:    convertToToolMessages(req.Messages),
		Tools:       convertTools(req.Tools),
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
}

// systemRole system 角色名（goconst 3+ 复用提取）
const systemRole = "system"

// joinSystemMessages 把所有 system role 消息合并为单个 system prompt
func joinSystemMessages(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role == systemRole {
			sb.WriteString(m.Content)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// convertToToolMessages 把 user/assistant 消息转为 Anthropic tool msg 格式
func convertToToolMessages(messages []Message) []antToolMsg {
	out := make([]antToolMsg, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" {
			continue
		}
		out = append(out, antToolMsg{
			Role:    m.Role,
			Content: []antContentBlk{{Type: "text", Text: m.Content}},
		})
	}
	return out
}

// convertTools 把 Tool 定义转为 Anthropic tool 格式
func convertTools(tools []Tool) []antTool {
	out := make([]antTool, 0, len(tools))
	for _, t := range tools {
		var schema json.RawMessage = t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, antTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}

// parseToolsResponse 把 Anthropic response 转为 ChatWithToolsResponse
func parseToolsResponse(name ProviderName, model string, data antResponseWithTools) *ChatWithToolsResponse {
	out := &ChatWithToolsResponse{
		Provider:  name,
		Model:     model,
		TokensIn:  data.Usage.InputTokens,
		TokensOut: data.Usage.OutputTokens,
	}
	for _, blk := range data.Content {
		if blk.Type == "text" {
			out.Content += blk.Text
			continue
		}
		if blk.Type == "tool_use" {
			out.ToolCalls = append(out.ToolCalls, ExecutedToolCall{
				Call: ToolCall{
					ID:        blk.ID,
					Name:      blk.Name,
					Arguments: blk.Input,
				},
			})
		}
	}
	return out
}

func (p *AnthropicCompat) do(ctx context.Context, body antRequest, stream bool) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", p.name, err)
	}
	url := p.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.effectiveAPIKey(ctx))
	req.Header.Set("anthropic-version", "2023-06-01")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return p.client.Do(req)
}

// doWithTools 调 Anthropic Messages API 含 tools (Sprint 34)
func (p *AnthropicCompat) doWithTools(ctx context.Context, body antRequestWithTools) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", p.name, err)
	}
	url := p.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.effectiveAPIKey(ctx))
	req.Header.Set("anthropic-version", "2023-06-01")
	return p.client.Do(req)
}

// toAntRequest 把 Request 转为 Anthropic 协议
// Anthropic 协议：system 单独字段，messages 只含 user/assistant
func toAntRequest(model string, req Request) (antRequest, error) {
	ar := antRequest{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
	if ar.MaxTokens == 0 {
		ar.MaxTokens = 4096
	}

	var msgs []antMessage
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if ar.System != "" {
				ar.System += "\n\n"
			}
			ar.System += m.Content
		case "user", "assistant":
			msgs = append(msgs, antMessage(m))
		default:
			return ar, fmt.Errorf("invalid message role: %q", m.Role)
		}
	}
	ar.Messages = msgs
	return ar, nil
}

// applyProviderDefaults 应用 provider-specific 默认值（Sprint A1.20）
//
// 如果 provider 注入了 defaultTemperature/defaultTopP/defaultThinking，
// 且 Request 未显式设置对应字段（Temperature/TopP == 0），则应用默认值。
//
// 此函数在 Chat/ChatStream 调 do() 之前调用。
func (p *AnthropicCompat) applyProviderDefaults(req *Request) {
	if req.Temperature == 0 && p.defaultTemperature > 0 {
		req.Temperature = p.defaultTemperature
	}
	if req.TopP == 0 && p.defaultTopP > 0 {
		req.TopP = p.defaultTopP
	}
}
