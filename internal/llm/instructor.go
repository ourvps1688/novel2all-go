// Package llm instructor.go: 结构化 JSON 输出辅助
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type JSONSchema struct {
	Name        string
	Description string
	Fields      []JSONSchemaField
}

type JSONSchemaField struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Items       string
}

type JSONSchemaError struct {
	Content string
	Err     error
}

func (e *JSONSchemaError) Error() string {
	return fmt.Sprintf("parse JSON: %v (content=%q)", e.Err, e.Content)
}

func (e *JSONSchemaError) Unwrap() error { return e.Err }

func GenerateJSON(ctx context.Context, router *Router, req Request, schema JSONSchema, target any) error {
	systemPrompt := buildJSONSystemPrompt(schema)
	req2 := req
	msgs := make([]Message, 0, len(req2.Messages)+1)
	msgs = append(msgs, Message{Role: "system", Content: systemPrompt})
	msgs = append(msgs, req2.Messages...)
	req2.Messages = msgs
	resp, err := router.Chat(ctx, req2)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	content := stripCodeBlock(resp.Content)
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return &JSONSchemaError{Content: content, Err: err}
	}
	return nil
}

func buildJSONSystemPrompt(schema JSONSchema) string {
	var sb strings.Builder
	sb.WriteString("你是 JSON 助手。请严格按照以下 schema 输出 JSON。\n\n")
	sb.WriteString("约束:\n")
	sb.WriteString("  - 仅返回 JSON 本身，不要输出 markdown 代码块\n")
	sb.WriteString("  - 不要输出解释、前后文、注释\n")
	sb.WriteString("  - 所有 required 字段必须出现\n\n")
	sb.WriteString("Schema:\n")
	sb.WriteString(schemaToPrompt(schema))
	return sb.String()
}

func schemaToPrompt(s JSONSchema) string {
	var sb strings.Builder
	sb.WriteString(s.Name)
	if s.Description != "" {
		sb.WriteString(" - ")
		sb.WriteString(s.Description)
	}
	sb.WriteString(":\n{\n")
	for _, f := range s.Fields {
		reqStr := ""
		if f.Required {
			reqStr = " (required)"
		}
		typeStr := f.Type
		if f.Type == "array" && f.Items != "" {
			typeStr = fmt.Sprintf("array<%s>", f.Items)
		}
		if f.Description != "" {
			fmt.Fprintf(&sb, "  %q: %s%s,  // %s\n", f.Name, typeStr, reqStr, f.Description)
		} else {
			fmt.Fprintf(&sb, "  %q: %s%s,\n", f.Name, typeStr, reqStr)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

// Use chr(96) to avoid bash backtick interpretation
var backtick = string([]byte{96})

func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, backtick+backtick+backtick) {
		return s
	}
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimRight(s, " \t\n")
	if strings.HasSuffix(s, backtick+backtick+backtick) {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}
