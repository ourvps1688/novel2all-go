package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// writeInput Write 工具 input
type writeInput struct {
	Path    string `json:"path" jsonschema:"description=目标文件路径（相对 sandbox root 或绝对）"`
	Content string `json:"content" jsonschema:"description=要写入的完整内容（覆盖现有内容）"`
}

// writeSchema JSON Schema
var writeSchema = []byte(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "目标文件路径"},
    "content": {"type": "string", "description": "要写入的完整内容"}
  },
  "required": ["path", "content"]
}`)

// WriteTool 实现 Write 工具（写沙箱内文件）
type WriteTool struct {
	sandbox Sandbox
}

// NewWriteTool 创建 Write 工具
func NewWriteTool(sandbox Sandbox) *WriteTool {
	return &WriteTool{sandbox: sandbox}
}

// Name 工具名
func (t *WriteTool) Name() string { return "Write" }

// Description 工具描述
func (t *WriteTool) Description() string {
	return "写入文件到沙箱内指定路径（覆盖现有内容）。自动创建父目录。"
}

// InputSchema JSON Schema
func (t *WriteTool) InputSchema() []byte { return writeSchema }

// Execute 执行写入
func (t *WriteTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Write: invalid input: %v", err)), nil
	}
	if in.Path == "" {
		return ErrorResult("Write: path is required"), nil
	}

	sb := t.sandbox
	if sb == nil && ctx != nil {
		sb = NewRootSandbox(ctx.Root)
	}
	if sb == nil {
		return ErrorResult("Write: no sandbox configured"), nil
	}

	absPath, err := sb.Resolve(in.Path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Write: %v", err)), nil
	}

	// 自动创建父目录
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ErrorResult(fmt.Sprintf("Write: mkdir: %v", err)), nil
	}

	if err := os.WriteFile(absPath, []byte(in.Content), 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("Write: %v", err)), nil
	}

	rel, _ := filepath.Rel(sb.Root(), absPath)
	return SuccessResult(fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), rel)), nil
}
