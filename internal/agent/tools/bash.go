package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// bashInput Bash 工具 input
type bashInput struct {
	Command string `json:"command" jsonschema:"description=完整 shell 命令（如 'ls -la /tmp' 或 'git status'）"`
	Timeout int    `json:"timeout" jsonschema:"description=超时秒数（默认 30，最大 300）"`
}

// bashSchema JSON Schema
var bashSchema = []byte(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "完整 shell 命令"},
    "timeout": {"type": "integer", "description": "超时秒数（默认 30，最大 300）"}
  },
  "required": ["command"]
}`)

// bashDefaultTimeout 默认超时
const bashDefaultTimeout = 30 * time.Second

// bashMaxTimeout 最大超时（5 分钟）
const bashMaxTimeout = 300 * time.Second

// bashMaxOutput 最大输出字节数（防止大量输出）
const bashMaxOutput = 100 * 1024 // 100KB

// 允许的命令白名单（决策 2=B）
//
// 加新命令必须：
//  1. 加到 allowedCommands
//  2. 评估安全性（无副作用或只读）
var allowedCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true,
	"grep": true, "find": true, "wc": true, "tree": true,
	"git": true, "python3": true, "node": true,
	"echo": true, "sort": true, "uniq": true,
	"pwd": true, "whoami": true, "date": true,
	"stat": true, "file": true,
}

// 显式拒绝的命令黑名单（无论如何都拒绝）
var deniedCommands = map[string]bool{
	"rm": true, "mv": true, "dd": true, "mkfs": true,
	"sudo": true, "su": true, "chmod": true, "chown": true,
	"curl": true, "wget": true, "nc": true, "netcat": true,
	"ssh": true, "scp": true, "rsync": true,
}

// BashTool 实现 Bash 工具（严格白名单 shell）
type BashTool struct {
	sandbox Sandbox
}

// NewBashTool 创建 Bash 工具
func NewBashTool(sandbox Sandbox) *BashTool {
	return &BashTool{sandbox: sandbox}
}

// Name 工具名
func (t *BashTool) Name() string { return "Bash" }

// Description 工具描述
func (t *BashTool) Description() string {
	return "执行白名单内的 shell 命令（ls/cat/grep/git 等）。禁止 rm/sudo/curl 等危险命令。"
}

// InputSchema JSON Schema
func (t *BashTool) InputSchema() []byte { return bashSchema }

// Execute 执行 bash 命令
func (t *BashTool) Execute(ctx *ExecContext, input []byte) (Result, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ErrorResult(fmt.Sprintf("Bash: invalid input: %v", err)), nil
	}
	if in.Command == "" {
		return ErrorResult("Bash: command is required"), nil
	}

	// 1. 解析命令（取第一个 token = 命令名）
	parts := strings.Fields(in.Command)
	if len(parts) == 0 {
		return ErrorResult("Bash: empty command"), nil
	}
	cmdName := parts[0]

	// 2. 黑名单优先检查（直接拒绝）
	if deniedCommands[cmdName] {
		return ErrorResult(fmt.Sprintf("Bash: command %q is denied (blacklist)", cmdName)), nil
	}

	// 3. 白名单检查（不在白名单 = 默认拒绝）
	if !allowedCommands[cmdName] {
		return ErrorResult(fmt.Sprintf("Bash: command %q not in whitelist. Allowed: ls, cat, head, tail, grep, find, wc, tree, git, python3, node, echo, sort, uniq, pwd, whoami, date, stat, file", cmdName)), nil
	}

	// 4. 参数检查（拒绝 ../ 路径逃避）
	if err := validateBashArgs(parts[1:]); err != nil {
		return ErrorResult(fmt.Sprintf("Bash: %v", err)), nil
	}

	// 5. 超时
	timeout := bashDefaultTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Second
		if timeout > bashMaxTimeout {
			timeout = bashMaxTimeout
		}
	}

	// 6. 执行（带超时）
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 如果有 sandbox，把 working dir 设为 sandbox root
	workDir := ""
	if t.sandbox != nil {
		workDir = t.sandbox.Root()
	} else if ctx != nil && ctx.Root != "" {
		workDir = ctx.Root
	}

	cmd := exec.CommandContext(runCtx, parts[0], parts[1:]...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Bash: %v\noutput: %s", err, truncateOutput(output))), nil
	}

	return SuccessResult(truncateOutput(output)), nil
}

// validateBashArgs 检查参数（拒绝 ../ 逃避 + 敏感路径）
func validateBashArgs(args []string) error {
	for _, arg := range args {
		// 拒绝 ../ 逃避
		if strings.Contains(arg, "..") {
			return fmt.Errorf("arg contains '..': %q", arg)
		}
		// 拒绝 /etc/passwd 等敏感路径
		if strings.HasPrefix(arg, "/etc/") || strings.HasPrefix(arg, "/root/") ||
			strings.HasPrefix(arg, "/var/log/") || arg == "/etc/passwd" ||
			arg == "/etc/shadow" {
			return fmt.Errorf("arg contains sensitive path: %q", arg)
		}
	}
	return nil
}

// truncateOutput 截断输出到 bashMaxOutput
func truncateOutput(output []byte) string {
	if len(output) > bashMaxOutput {
		return string(output[:bashMaxOutput]) + fmt.Sprintf("\n... (truncated, total %d bytes)", len(output))
	}
	return string(output)
}
