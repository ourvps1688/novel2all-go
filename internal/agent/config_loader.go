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
//
//	model_mapping:
//	  opus:
//	    provider: minimax
//	    model: "MiniMax-M3"
//	  sonnet:
//	    provider: deepseek
//	    model: "claude-opus-4-5-20250929"
//	  haiku:
//	    provider: dashscope
//	    model: "qwen3.7-plus"
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
		inMapping  bool
		currentKey string // 当前正在编辑的 vendor model key
	)

	for scanner.Scan() {
		raw := scanner.Text()
		currentKey, inMapping = parseModelMappingLine(m, raw, currentKey, inMapping)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse model mapping: %w", err)
	}

	return m, nil
}

// parseModelMappingLine 处理单行 YAML（拆分降低 ParseModelMapping 圈复杂度）
func parseModelMappingLine(m *ModelMapping, raw, currentKey string, inMapping bool) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return currentKey, inMapping
	}

	// 计算缩进级别
	indent := len(raw) - len(strings.TrimLeft(raw, " \t"))

	// 顶层：model_mapping:
	if indent == 0 {
		if trimmed == "model_mapping:" {
			return currentKey, true
		}
		return currentKey, false
	}

	if !inMapping {
		return currentKey, inMapping
	}

	// vendor model (opus/sonnet/haiku) 二级缩进
	if indent == 2 && strings.HasSuffix(trimmed, ":") {
		key := strings.TrimSuffix(trimmed, ":")
		switch key {
		case vendorModelOpus, vendorModelSonnet, vendorModelHaiku:
			return key, inMapping
		}
		return currentKey, inMapping
	}

	// 字段 (provider/model) 三级缩进
	if indent >= 4 && currentKey != "" {
		newKey := applyModelMappingField(m, currentKey, trimmed)
		return newKey, inMapping
	}

	return currentKey, inMapping
}

// applyModelMappingField 处理 provider/model 字段赋值（拆分降低圈复杂度）
func applyModelMappingField(m *ModelMapping, key, trimmed string) string {
	if !strings.Contains(trimmed, ":") {
		return key
	}
	colonIdx := strings.Index(trimmed, ":")
	field := strings.TrimSpace(trimmed[:colonIdx])
	value := strings.TrimSpace(trimmed[colonIdx+1:])
	value = strings.Trim(value, `"'`)

	entry := m.Mapping[key]
	switch field {
	case "provider":
		entry.Provider = llm.ProviderName(value)
	case "model":
		entry.Model = value
	}
	m.Mapping[key] = entry
	return key
}
