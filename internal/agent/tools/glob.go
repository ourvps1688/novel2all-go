package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// globInput Glob 工具 input
type globInput struct {
	Pattern string `json:"pattern" jsonschema:"description=Glob 模式（如 '**/*.md' 或 'src/**/*.go'）"`
}

// globSchema JSON Schema
var globSchema = []byte(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob 模式（如 '**/*.md' 或 'src/**/*.go'）"}
  },
  "required": ["pattern"]
}`)

// GlobTool 实现 Glob 工具（沙箱内文件 pattern 匹配）
type GlobTool struct {
	sandbox Sandbox
}

// NewGlobTool 创建 Glob 工具
func NewGlobTool(sandbox Sandbox) *GlobTool {
	return &GlobTool{sandbox: sandbox}
}

// Name 工具名
func (t *GlobTool) Name() string { return "Glob" }

// Description 工具描述
func (t *GlobTool) Description() string {
	return "按 Glob 模式匹配沙箱内的文件（如 '**/*.md' 或 'src/**/*.go'）。返回相对路径列表。"
}

// InputSchema JSON Schema
func (t *GlobTool) InputSchema() []byte { return globSchema }

// Execute 执行匹配
func (t *GlobTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in globInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Glob: invalid input: %v", err)), nil
	}
	if in.Pattern == "" {
		return ErrorResult("Glob: pattern is required"), nil
	}

	sb := t.sandbox
	if sb == nil && ctx != nil {
		sb = NewRootSandbox(ctx.Root)
	}
	if sb == nil {
		return ErrorResult("Glob: no sandbox configured"), nil
	}

	// 拆分 pattern: dir + glob
	dir, file := splitPattern(in.Pattern)
	var searchDir string
	if filepath.IsAbs(dir) {
		searchDir = filepath.Clean(dir)
	} else {
		searchDir = filepath.Join(sb.Root(), dir)
	}

	// 验证 searchDir 在 sandbox 内
	if err := sb.Validate(searchDir); err != nil {
		return ErrorResult(fmt.Sprintf("Glob: %v", err)), nil
	}

	matches, err := filepath.Glob(filepath.Join(searchDir, file))
	if err != nil {
		return ErrorResult(fmt.Sprintf("Glob: %v", err)), nil
	}

	// 转相对路径 + 排序
	relMatches := make([]string, 0, len(matches))
	for _, m := range matches {
		if rel, err := filepath.Rel(sb.Root(), m); err == nil {
			relMatches = append(relMatches, filepath.ToSlash(rel))
		}
	}
	sort.Strings(relMatches)

	if len(relMatches) == 0 {
		return SuccessResult(fmt.Sprintf("Glob: no files matched pattern %q", in.Pattern)), nil
	}

	var sb_out strings.Builder
	fmt.Fprintf(&sb_out, "Glob matched %d file(s):\n", len(relMatches))
	for _, m := range relMatches {
		sb_out.WriteString("- ")
		sb_out.WriteString(m)
		sb_out.WriteByte('\n')
	}
	return SuccessResult(sb_out.String()), nil
}

// splitPattern 拆分 pattern 为 dir + file 部分
func splitPattern(pattern string) (dir, file string) {
	idx := strings.LastIndex(pattern, "/")
	if idx < 0 {
		return ".", pattern
	}
	return pattern[:idx], pattern[idx+1:]
}
