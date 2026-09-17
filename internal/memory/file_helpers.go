// file_helpers.go - 文件读取 helper (便于测试 mock)
package memory

import "os"

// readFileOS 默认 os.ReadFile (测试时可覆盖).
var readFileOS = os.ReadFile

// writeFileOS 默认 os.WriteFile (测试时可覆盖).
var writeFileOS = func(fp string, data []byte) error { return os.WriteFile(fp, data, 0o644) }

// mkdirOS 默认 os.MkdirAll (测试时可覆盖).
var mkdirOS = func(dir string) error { return os.MkdirAll(dir, 0o755) }

// DefaultSettingFiles 默认纳入 memory core 的项目级文件 (相对 project root).
//
// 注: 与 llm.settings_hash 重复定义 (memory 不依赖 llm 避免循环).
var DefaultSettingFiles = []string{
	"创作设定.md",
	"设定/文风.md",
}
