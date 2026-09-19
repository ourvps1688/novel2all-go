package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// grepInput Grep 工具 input
type grepInput struct {
	Pattern string `json:"pattern" jsonschema:"description=正则表达式"`
	Path    string `json:"path" jsonschema:"description=起始搜索路径（文件或目录，相对 sandbox root 或绝对）"`
}

// grepSchema JSON Schema
var grepSchema = []byte(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "正则表达式"},
    "path": {"type": "string", "description": "起始搜索路径（文件或目录）"}
  },
  "required": ["pattern"]
}`)

// maxGrepFileSize 10MB 限制
const maxGrepFileSize = 10 * 1024 * 1024

// maxGrepResults 限制返回结果数（避免大量匹配）
const maxGrepResults = 100

// GrepTool 实现 Grep 工具（沙箱内文本搜索）
type GrepTool struct {
	sandbox Sandbox
}

// NewGrepTool 创建 Grep 工具
func NewGrepTool(sandbox Sandbox) *GrepTool {
	return &GrepTool{sandbox: sandbox}
}

// Name 工具名
func (t *GrepTool) Name() string { return "Grep" }

// Description 工具描述
func (t *GrepTool) Description() string {
	return "在沙箱内搜索文本模式（正则表达式）。返回匹配的文件路径、行号、内容。"
}

// InputSchema JSON Schema
func (t *GrepTool) InputSchema() []byte { return grepSchema }

// Execute 执行搜索
func (t *GrepTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in grepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Grep: invalid input: %v", err)), nil
	}
	if in.Pattern == "" {
		return ErrorResult("Grep: pattern is required"), nil
	}

	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Grep: invalid regex: %v", err)), nil
	}

	sb := t.sandbox
	if sb == nil && ctx != nil {
		sb = NewRootSandbox(ctx.Root)
	}
	if sb == nil {
		return ErrorResult("Grep: no sandbox configured"), nil
	}

	// 起始搜索路径
	searchPath := in.Path
	if searchPath == "" {
		searchPath = sb.Root()
	}
	absPath, err := sb.Resolve(searchPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Grep: %v", err)), nil
	}

	// 判断是文件还是目录
	info, err := os.Stat(absPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Grep: %v", err)), nil
	}

	var matches []string
	if info.IsDir() {
		matches = t.searchDir(sb, absPath, re)
	} else {
		matches = t.searchFile(sb, absPath, re)
	}

	if len(matches) == 0 {
		return SuccessResult(fmt.Sprintf("Grep: no matches for pattern %q", in.Pattern)), nil
	}
	return SuccessResult(strings.Join(matches, "\n")), nil
}

// searchDir 递归搜索目录
func (t *GrepTool) searchDir(sb Sandbox, dir string, re *regexp.Regexp) []string {
	var matches []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// 跳过二进制大文件
		if info.Size() > maxGrepFileSize {
			return nil
		}
		// 跳过隐藏目录（点开头）
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		matches = append(matches, t.searchFile(sb, path, re)...)
		if len(matches) >= maxGrepResults {
			return filepath.SkipAll
		}
		return nil
	})
	return matches
}

// searchFile 搜索单个文件
func (t *GrepTool) searchFile(sb Sandbox, path string, re *regexp.Regexp) []string {
	var matches []string
	f, err := os.Open(path)
	if err != nil {
		return matches
	}
	defer func() { _ = f.Close() }()

	rel, _ := filepath.Rel(sb.Root(), path)
	relSlash := filepath.ToSlash(rel)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), maxGrepFileSize)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, fmt.Sprintf("%s:%d:%s", relSlash, lineNo, line))
			if len(matches) >= maxGrepResults {
				break
			}
		}
	}
	return matches
}
