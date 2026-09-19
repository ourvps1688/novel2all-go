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

// DEPRECATED (Sprint A1.17, 2026-09-18): OpenAI 兼容协议基类。
//
// 历史背景：
//   - Sprint 21 引入，dashscope / deepseek / openai 都走这个协议
//   - Sprint 34 仍用于 dashscope / deepseek (直到 Sprint A1.13/A1.14)
//
// 当前状态（V2.0.0 起）：
//   - dashscope 改用 Anthropic 兼容（/apps/anthropic）
//   - deepseek 改用 Anthropic 兼容（/anthropic）+ claude-* 自动映射
//   - 4 家 provider 全部统一走 AnthropicCompat
//   - OpenAICompat 仍保留实现（向后兼容），但不再有 default route 使用
//
// Sprint 34 的 tool call 实现也保留（如果未来需要 OpenAI 协议兼容 provider）
// 实际新增 provider 优先用 AnthropicCompat。
//
// OpenAICompat 是 OpenAI 兼容协议 provider 的基类。
// 历史用途：dashscope / deepseek / openai 自身都遵循这个协议。
// 当前 (Sprint A1) 不再作为默认 provider 使用，仅作向后兼容保留。
type OpenAICompat struct {
	name    ProviderName
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewOpenAICompat 创建 OpenAI 兼容 provider
func NewOpenAICompat(name ProviderName, apiKey, baseURL, defaultModel string) *OpenAICompat {
	return &OpenAICompat{
		name:    name,
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   defaultModel,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *OpenAICompat) Name() ProviderName { return p.name }
func (p *OpenAICompat) Available() bool    { return p.apiKey != "" }

// DefaultModel 返回 OpenAI 兼容 provider 的默认模型名
func (p *OpenAICompat) DefaultModel() string { return p.model }

// APIBase 返回 OpenAI 兼容 provider 的 base URL
func (p *OpenAICompat) APIBase() string { return p.baseURL }

// oaiChatRequest OpenAI 协议请求体
type oaiChatRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Stream      bool         `json:"stream"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiChatResponse struct {
	Choices []struct {
		Message oaiMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// oaiToolCall OpenAI tool call 字段
type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// oaiToolDefinition OpenAI tool 定义 (Sprint 34)
type oaiToolDefinition struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// oaiMessageWithToolCalls message 支持 tool_calls 字段 (Sprint 34)
type oaiMessageWithToolCalls struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // for tool role messages
}

// oaiChatRequestWithTools request body 含 tools (Sprint 34)
type oaiChatRequestWithTools struct {
	Model       string                    `json:"model"`
	Messages    []oaiMessageWithToolCalls `json:"messages"`
	Tools       []oaiToolDefinition       `json:"tools,omitempty"`
	ToolChoice  any                       `json:"tool_choice,omitempty"`
	MaxTokens   int                       `json:"max_tokens,omitempty"`
	Temperature float64                   `json:"temperature,omitempty"`
}

// oaiChatResponseWithTools response 含 tool_calls (Sprint 34)
type oaiChatResponseWithTools struct {
	Choices []struct {
		Message oaiMessageWithToolCalls `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// oaiStreamChunk OpenAI 流式响应的一个分片
type oaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat 非流式
func (p *OpenAICompat) Chat(ctx context.Context, req Request) (*Response, error) {
	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
	body := oaiChatRequest{
		Model:       model,
		Messages:    toOaiMessages(req.Messages),
		Stream:      false,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	resp, err := p.do(ctx, body, false)
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

	var data oaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", p.name, err)
	}
	if len(data.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices", p.name)
	}

	return &Response{
		Content:   data.Choices[0].Message.Content,
		Provider:  p.name,
		Model:     model,
		TokensIn:  data.Usage.PromptTokens,
		TokensOut: data.Usage.CompletionTokens,
	}, nil
}

// ChatStream 流式
//
//nolint:gocyclo // SSE 流处理天然多分支（line 解析 + EOF + JSON unmarshal + select）
func (p *OpenAICompat) ChatStream(ctx context.Context, req Request, ch chan<- Chunk) error {
	defer close(ch)

	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
	body := oaiChatRequest{
		Model:       model,
		Messages:    toOaiMessages(req.Messages),
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	resp, err := p.do(ctx, body, true)
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
		if payload == "[DONE]" {
			ch <- Chunk{Done: true}
			return nil
		}
		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 跳过非 JSON 行（如注释）
		}
		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				select {
				case ch <- Chunk{Content: content}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

// ChatWithTools 一次调用 LLM, 返回 assistant 文本 + 可能调用的 tool 列表.
//
// Sprint 34: LLM 决定是否调 tool (返回 []ToolCall) 或直接答 (返回 Content).
// Multi-turn 循环由 Router.ChatWithTools 控制, provider 只暴露单次 raw 调用.
func (p *OpenAICompat) ChatWithTools(ctx context.Context, req ChatWithToolsRequest) (*ChatWithToolsResponse, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("openai compat %s: API key not configured", p.name)
	}
	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}

	// 1. 转换 Tools → oaiToolDefinition
	tools := make([]oaiToolDefinition, 0, len(req.Tools))
	for _, t := range req.Tools {
		var params json.RawMessage = t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, oaiToolDefinition{
			Type: "function",
			Function: struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			}{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}

	// 2. 转换 Messages
	messages := make([]oaiMessageWithToolCalls, 0, len(req.Messages))
	for _, m := range req.Messages {
		messages = append(messages, oaiMessageWithToolCalls{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	// 3. tool_choice
	toolChoice := any("auto")
	switch req.ToolChoice {
	case "", "auto":
		// already "auto"
	case "any":
		toolChoice = "any"
	case "none":
		toolChoice = "none"
	default:
		toolChoice = map[string]any{
			"type":     "function",
			"function": map[string]string{"name": req.ToolChoice},
		}
	}

	body := oaiChatRequestWithTools{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  toolChoice,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	resp, err := p.doWithTools(ctx, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai compat %s: HTTP %d: %s", p.name, resp.StatusCode, string(b))
	}

	var oaiResp oaiChatResponseWithTools
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai compat %s: no choices in response", p.name)
	}

	choice := oaiResp.Choices[0].Message
	out := &ChatWithToolsResponse{
		Content:   choice.Content,
		Provider:  p.name,
		Model:     model,
		TokensIn:  oaiResp.Usage.PromptTokens,
		TokensOut: oaiResp.Usage.CompletionTokens,
	}

	// 解析 tool_calls
	for _, tc := range choice.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ExecutedToolCall{
			Call: ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			},
		})
	}

	return out, nil
}

func (p *OpenAICompat) do(ctx context.Context, body oaiChatRequest, stream bool) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", p.name, err)
	}
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return p.client.Do(req)
}

// doWithTools 调 OpenAI 兼容 API (Sprint 34).
// 与 do 区别: request body 含 tools 字段.
func (p *OpenAICompat) doWithTools(ctx context.Context, body oaiChatRequestWithTools) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", p.name, err)
	}
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	return p.client.Do(req)
}

func toOaiMessages(msgs []Message) []oaiMessage {
	out := make([]oaiMessage, len(msgs))
	for i, m := range msgs {
		out[i] = oaiMessage(m)
	}
	return out
}
