package skills

import "embed"

// skillFS 嵌入 vendor oh-story-dsh-0.1.9 完整 skills (Sprint A4)
//
// 13 个 SKILL.md + 242 references (~2.62 MB) 通过 embed.FS 嵌入二进制。
// 结构：assets/<skill>/SKILL.md + assets/<skill>/references/[<subdir>/]*.md
//
// `//go:embed assets` 嵌入整个目录树（Go embed 不支持 ** glob）
//
//go:embed assets
var skillFS embed.FS
