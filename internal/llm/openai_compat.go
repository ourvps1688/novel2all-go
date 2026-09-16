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

// OpenAICompat 是 OpenAI 兼容协议 provider 的基类。
// dashscope / deepseek / openai 自身都遵循这个协议。
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
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: HTTP %d: %s", p.name, resp.StatusCode, string(b))
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

func toOaiMessages(msgs []Message) []oaiMessage {
	out := make([]oaiMessage, len(msgs))
	for i, m := range msgs {
		out[i] = oaiMessage{Role: m.Role, Content: m.Content}
	}
	return out
}
