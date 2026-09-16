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
type AnthropicCompat struct {
	name    ProviderName
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
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

func (p *AnthropicCompat) Name() ProviderName { return p.name }
func (p *AnthropicCompat) Available() bool   { return p.apiKey != "" }

// antRequest Anthropic Messages API 请求
type antRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
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
	ar, err := toAntRequest(model, req)
	if err != nil {
		return nil, err
	}
	resp, err := p.do(ctx, ar, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, string(b))
	}

	var data antResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", p.name, err)
	}
	if len(data.Content) == 0 {
		return nil, fmt.Errorf("%s: no content", p.name)
	}
	return &Response{
		Content:   data.Content[0].Text,
		Provider:  p.name,
		Model:     model,
		TokensIn:  data.Usage.InputTokens,
		TokensOut: data.Usage.OutputTokens,
	}, nil
}

// ChatStream 流式
func (p *AnthropicCompat) ChatStream(ctx context.Context, req Request, ch chan<- Chunk) error {
	defer close(ch)

	model := p.model
	if req.OverrideModel != "" {
		model = req.OverrideModel
	}
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
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, string(b))
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
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return p.client.Do(req)
}

// toAntRequest 把 Request 转为 Anthropic 协议
// Anthropic 协议：system 单独字段，messages 只含 user/assistant
func toAntRequest(model string, req Request) (antRequest, error) {
	ar := antRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
		Stream:    stream(req),
		Temperature: req.Temperature,
	}
	if ar.MaxTokens == 0 {
		ar.MaxTokens = 4096
	}
	if !ar.Stream {
		ar.Stream = false
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
			msgs = append(msgs, antMessage{Role: m.Role, Content: m.Content})
		default:
			return ar, fmt.Errorf("invalid message role: %q", m.Role)
		}
	}
	ar.Messages = msgs
	return ar, nil
}

func stream(req Request) bool {
	if req.Stream {
		return true
	}
	return false
}