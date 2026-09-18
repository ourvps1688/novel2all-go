package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadAgentSpec 从单个 role .md 文件加载 AgentSpec (Sprint A1.4)
//
// 文件格式：
//   ---
//   name: story-architect
//   description: |
//     多行描述
//   tools: [Read, Glob, Grep, Write, Edit]
//   model: opus
//   maxTurns: 30
//   memory: project
//   skills: [story-deslop, story-setup]
//   ---
//
//   # Role Body
//   ... 实际 prompt 内容 ...
//
// 返回：解析后的 AgentSpec + error
func LoadAgentSpec(path string) (*AgentSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load agent spec %q: %w", path, err)
	}

	fm, body, err := ParseFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("load agent spec %q: %w", path, err)
	}

	spec := &AgentSpec{
		Name:         fm.Name,
		Description:  fm.Description,
		Tools:        fm.Tools,
		Model:        fm.Model,
		MaxTurns:     fm.MaxTurns,
		Memory:       fm.Memory,
		Skills:       fm.Skills,
		SystemPrompt: strings.TrimSpace(body),
		SourcePath:   path,
	}

	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("load agent spec %q: %w", path, err)
	}

	return spec, nil
}

// LoadAgentSpecFromDir 从目录加载所有 role .md 文件 (Sprint A1.4 helper)
//
// 返回：name → AgentSpec 映射
func LoadAgentSpecFromDir(dir string) (map[string]*AgentSpec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %q: %w", dir, err)
	}

	out := make(map[string]*AgentSpec)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		spec, err := LoadAgentSpec(fullPath)
		if err != nil {
			return nil, err
		}
		out[spec.Name] = spec
	}
	return out, nil
}

// LoadAgent 从 role name 加载并构造 Agent 实例 (Sprint A1.4)
//
// dirs 按顺序搜索，找到第一个匹配 name 的 .md 文件。
// 典型用法：LoadAgent("story-architect", "internal/agent/assets/roles")
func LoadAgent(name string, dirs ...string) (*Agent, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err == nil {
			spec, err := LoadAgentSpec(path)
			if err != nil {
				return nil, err
			}
			return NewAgent(spec), nil
		}
	}
	return nil, fmt.Errorf("load agent %q: not found in dirs %v", name, dirs)
}

// ListAvailableRoles 列出目录中所有可用的 role names (调试用)
func ListAvailableRoles(dir string) ([]string, error) {
	specs, err := LoadAgentSpecFromDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	return names, nil
}
