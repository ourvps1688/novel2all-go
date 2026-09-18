package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// editInput Edit 工具 input
type editInput struct {
	Path      string `json:"path" jsonschema:"description=目标文件路径"`
	OldString string `json:"old_string" jsonschema:"description=要替换的字符串（必须唯一匹配）"`
	NewString string `json:"new_string" jsonschema:"description=替换后的字符串"`
}

// editSchema JSON Schema
var editSchema = []byte(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "目标文件路径"},
    "old_string": {"type": "string", "description": "要替换的字符串（必须唯一匹配）"},
    "new_string": {"type": "string", "description": "替换后的字符串"}
  },
  "required": ["path", "old_string", "new_string"]
}`)

// EditTool 实现 Edit 工具（增量替换文件中的字符串）
type EditTool struct {
	sandbox Sandbox
}

// NewEditTool 创建 Edit 工具
func NewEditTool(sandbox Sandbox) *EditTool {
	return &EditTool{sandbox: sandbox}
}

// Name 工具名
func (t *EditTool) Name() string { return "Edit" }

// Description 工具描述
func (t *EditTool) Description() string {
	return "在文件中增量替换字符串（old_string 必须唯一匹配）。返回替换结果。"
}

// InputSchema JSON Schema
func (t *EditTool) InputSchema() []byte { return editSchema }

// Execute 执行增量替换
func (t *EditTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in editInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Edit: invalid input: %v", err)), nil
	}
	if in.Path == "" || in.OldString == "" {
		return ErrorResult("Edit: path and old_string are required"), nil
	}

	sb := t.sandbox
	if sb == nil && ctx != nil {
		sb = NewRootSandbox(ctx.Root)
	}
	if sb == nil {
		return ErrorResult("Edit: no sandbox configured"), nil
	}

	absPath, err := sb.Resolve(in.Path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Edit: %v", err)), nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Edit: read: %v", err)), nil
	}
	content := string(data)

	// 检查唯一性（必须恰好 1 处匹配，避免误伤）
	count := strings.Count(content, in.OldString)
	if count == 0 {
		return ErrorResult(fmt.Sprintf("Edit: old_string not found in %s", in.Path)), nil
	}
	if count > 1 {
		return ErrorResult(fmt.Sprintf("Edit: old_string matched %d times in %s, must be unique", count, in.Path)), nil
	}

	newContent := strings.Replace(content, in.OldString, in.NewString, 1)

	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("Edit: write: %v", err)), nil
	}

	rel, _ := filepath.Rel(sb.Root(), absPath)
	return SuccessResult(fmt.Sprintf("Edited %s (replaced 1 occurrence)", rel)), nil
}
