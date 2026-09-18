package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// readInput Read 工具 input
type readInput struct {
	Path string `json:"path" jsonschema:"description=要读取的文件路径（相对 sandbox root 或绝对）"`
}

// readSchema JSON Schema
var readSchema = []byte(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "要读取的文件路径（相对 sandbox root 或绝对）"}
  },
  "required": ["path"]
}`)

// ReadTool 实现 Read 工具（读取 sandbox 内的文件）
type ReadTool struct {
	sandbox Sandbox
}

// NewReadTool 创建 Read 工具
func NewReadTool(sandbox Sandbox) *ReadTool {
	return &ReadTool{sandbox: sandbox}
}

// Name 工具名
func (t *ReadTool) Name() string { return "Read" }

// Description 工具描述
func (t *ReadTool) Description() string {
	return "读取文件内容（沙箱内）。返回文件文本内容。"
}

// InputSchema JSON Schema
func (t *ReadTool) InputSchema() []byte { return readSchema }

// Execute 执行读取
func (t *ReadTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Read: invalid input: %v", err)), nil
	}
	if in.Path == "" {
		return ErrorResult("Read: path is required"), nil
	}

	// 沙箱验证（用工具自带 sandbox 或 ctx sandbox）
	sb := t.sandbox
	if sb == nil && ctx != nil {
		sb = NewRootSandbox(ctx.Root)
	}
	if sb == nil {
		return ErrorResult("Read: no sandbox configured"), nil
	}

	absPath, err := sb.Resolve(in.Path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Read: %v", err)), nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("Read: file not found: %s", absPath)), nil
		}
		return ErrorResult(fmt.Sprintf("Read: %v", err)), nil
	}

	// 文件信息（给 LLM 参考）
	relPath, _ := filepath.Rel(sb.Root(), absPath)
	return SuccessResult(fmt.Sprintf("File: %s\n\n%s", relPath, string(data))), nil
}
