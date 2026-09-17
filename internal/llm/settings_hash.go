// Package llm 提供 LLM 多 provider 路由 + 流式响应.
package llm

// settings_hash.go 实现项目设定文件 hash (Sprint 21 补全).
//
// 背景:
//   - 用户修改 创作设定.md 或 设定/文风.md 后, 旧 cache 条目应该失效
//   - 否则会用旧设定生成的 cache 响应 (不符合新风格)
//   - 解法: 每次生成 cache key 时计算 settings hash, 纳入 key
//
// 设计:
//   - 收集 创作设定.md + 设定/文风.md + 设定/角色/*.md 内容
//   - 拼接后 sha256 截前 16 字符 (64-bit)
//   - 任何文件不存在 → 跳过 (不参与 hash)
//   - 角色文件按文件名排序 (保证 hash 稳定)
//
// 参考 Python V1.0.2 B2 core/settings_hash.py.
import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultSettingFiles 默认纳入 hash 的项目级文件 (相对 project root).
var DefaultSettingFiles = []string{
	"创作设定.md",
	"设定/文风.md",
}

// DefaultRolesSubdir 角色设定子目录.
const DefaultRolesSubdir = "设定/角色"

// DefaultRolesGlob 角色文件 glob 模式.
const DefaultRolesGlob = "*.md"

// emptyHash 是 16 个 '0' (空项目/None).
const emptyHash = "0000000000000000"

// ComputeSettingsHash 计算项目设定文件 hash.
//
// projectRoot 为空或目录不存在 → 返回 emptyHash (向后兼容).
// 任何文件读取错误 → 跳过该文件.
func ComputeSettingsHash(projectRoot string) string {
	if projectRoot == "" {
		return emptyHash
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return emptyHash
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return emptyHash
	}

	var parts []string

	// 默认设定文件
	for _, rel := range DefaultSettingFiles {
		path := filepath.Join(root, rel)
		if data, err := os.ReadFile(path); err == nil {
			parts = append(parts, string(data))
		}
	}

	// 角色目录 (排序保证 hash 稳定)
	rolesDir := filepath.Join(root, DefaultRolesSubdir)
	if info, err := os.Stat(rolesDir); err == nil && info.IsDir() {
		matches, err := filepath.Glob(filepath.Join(rolesDir, DefaultRolesGlob))
		if err == nil {
			// sort by basename for stability
			sort.Slice(matches, func(i, j int) bool {
				return filepath.Base(matches[i]) < filepath.Base(matches[j])
			})
			for _, m := range matches {
				if data, err := os.ReadFile(m); err == nil {
					parts = append(parts, string(data))
				}
			}
		}
	}

	if len(parts) == 0 {
		return emptyHash
	}

	// SHA256 截前 8 字节 (16 hex chars)
	h := sha256.Sum256([]byte(strings.Join(parts, "\n---\n")))
	return hex.EncodeToString(h[:8])
}
