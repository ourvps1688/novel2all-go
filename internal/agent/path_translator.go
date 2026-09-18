package agent

import (
	"strings"
)

// PathTranslator 把 vendor role .md prompt 中的 Claude Code 路径翻译为 Go 路径 (Sprint A5.9)
//
// Sprint A3 阶段检查发现（commit 4f54427）：5/7 vendor role prompt 含
// Claude Code 特定路径 `.claude/skills/...`，必须在传给 LLM 之前翻译，否则
// LLM 会尝试去实际文件系统找这些路径（Go 端是 embed.FS，没有文件系统路径）。
//
// 翻译规则：
//   - `.claude/skills/X/SKILL.md`         → `internal/skills/assets/X/SKILL.md`
//   - `.claude/skills/X/references/Y.md` → `internal/skills/assets/X/references/Y.md`
//   - `.claude/skills/X/references/sub/Y.md` → `internal/skills/assets/X/references/sub/Y.md`
//   - `{项目根}/skills/X/...`            → `internal/skills/assets/X/...`（剥掉前缀）
//   - `~/.claude/skills/X/...`            → `internal/skills/assets/X/...`（home 路径）
//   - `vendor skills/X/...`              → `internal/skills/assets/X/...`（vendor 前缀）
//
// 翻译后 LLM 看到的就是 Go 项目里的路径，可以用 Read 工具读取。
type PathTranslator struct{}

// NewPathTranslator 构造 translator
func NewPathTranslator() *PathTranslator {
	return &PathTranslator{}
}

// pathPrefixes vendor 路径前缀 → Go 路径前缀映射
//
// 注意：前缀顺序很重要（长前缀/具体前缀先匹配，避免被短前缀截断）
// 例：`~/.claude/skills/` 必须先于 `.claude/skills/`，否则 `~/` 残留
var pathPrefixes = []struct {
	old string
	new string
}{
	// 1. ~/.claude/skills/ 前缀（home 路径，最长）
	{"~/.claude/skills/", "internal/skills/assets/"},
	// 2. {项目根}/skills/ 前缀（vendor prompts 的指令前缀）
	{"{项目根}/skills/", "internal/skills/assets/"},
	// 3. .claude/skills/ 前缀（普通前缀）
	{".claude/skills/", "internal/skills/assets/"},
	// 4. vendor skills/ 前缀（vendor 文档/对话中提到）
	{"vendor skills/", "internal/skills/assets/"},
}

// Translate 翻译 prompt 中所有 vendor 路径
//
// 返回翻译后的 prompt。所有 vendor 路径会被替换成 Go 项目路径。
// 不存在的路径也会被替换（保持翻译一致性）。
func (t *PathTranslator) Translate(prompt string) string {
	out := prompt
	for _, p := range pathPrefixes {
		out = strings.ReplaceAll(out, p.old, p.new)
	}
	return out
}

// NormalizeRefPath 把相对 path 转成 reference path 格式（forward slash，去掉 ./ 前缀）
//
// 例子："abc/foo.md" → "abc/foo.md"，"./foo.md" → "foo.md"，"foo.md" → "foo.md"
func NormalizeRefPath(p string) string {
	p = strings.TrimPrefix(p, "./")
	return strings.ReplaceAll(p, "\\", "/")
}
