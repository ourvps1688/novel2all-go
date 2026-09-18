package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// LoadModelMapping 从 configs/agent-models.yaml 加载 ModelMapping (Sprint A1.10)
//
// YAML 格式：
//   model_mapping:
//     opus:
//       provider: minimax
//       model: "MiniMax-M3"
//     sonnet:
//       provider: deepseek
//       model: "claude-opus-4-5-20250929"
//     haiku:
//       provider: dashscope
//       model: "qwen3.7-plus"
//
// 如果文件不存在，返回 DefaultModelMapping()（向后兼容）。
// 如果文件存在但部分字段缺失，对缺失字段用默认值。
func LoadModelMapping(path string) (*ModelMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在 → 默认配置
			return DefaultModelMapping(), nil
		}
		return nil, fmt.Errorf("load model mapping %q: %w", path, err)
	}

	return ParseModelMapping(string(data))
}

// ParseModelMapping 从 YAML 内容解析 ModelMapping
func ParseModelMapping(content string) (*ModelMapping, error) {
	// 从默认值开始（缺失字段用默认补）
	m := DefaultModelMapping()

	scanner := bufio.NewScanner(strings.NewReader(content))
	var (
		inMapping bool
		currentKey string // 当前正在编辑的 vendor model key
	)

	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// 计算缩进级别
		indent := len(raw) - len(strings.TrimLeft(raw, " \t"))

		// 顶层：model_mapping:
		if indent == 0 && trimmed == "model_mapping:" {
			inMapping = true
			continue
		}

		if !inMapping {
			continue
		}

		// vendor model (opus/sonnet/haiku) 二级缩进
		if indent == 2 && strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(trimmed, ":")
			switch key {
			case "opus", "sonnet", "haiku":
				currentKey = key
				continue
			}
		}

		// 字段 (provider/model) 三级缩进
		if indent >= 4 && currentKey != "" && strings.Contains(trimmed, ":") {
			colonIdx := strings.Index(trimmed, ":")
			field := strings.TrimSpace(trimmed[:colonIdx])
			value := strings.TrimSpace(trimmed[colonIdx+1:])
			value = strings.Trim(value, `"'`)

			entry := m.Mapping[currentKey]
			switch field {
			case "provider":
				entry.Provider = llm.ProviderName(value)
			case "model":
				entry.Model = value
			}
			m.Mapping[currentKey] = entry
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse model mapping: %w", err)
	}

	return m, nil
}
