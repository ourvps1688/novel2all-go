package skills

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed assets/*.md
var skillFS embed.FS

// ErrSkillNotFound 找不到 skill
var ErrSkillNotFound = errors.New("skill not found")

// Loader 并发安全的 skill 加载器
type Loader struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

// NewLoader 从 embed.FS 加载所有 SKILL.md
func NewLoader() (*Loader, error) {
	l := &Loader{
		skills: make(map[string]Skill),
	}

	entries, err := skillFS.ReadDir("assets")
	if err != nil {
		return nil, fmt.Errorf("read assets dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := skillFS.ReadFile("assets/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		skill, err := parseSkill(e.Name(), string(data))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		l.skills[skill.Name] = skill
	}

	return l, nil
}

// Count 返回加载的 skill 数
func (l *Loader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.skills)
}

// List 返回所有 skill 名称（按字母排序）
func (l *Loader) List() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.skills))
	for name := range l.skills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ListDetailed 返回所有 skill 的简要描述
func (l *Loader) ListDetailed() []Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Skill, 0, len(l.skills))
	for _, s := range l.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get 按名获取 skill
func (l *Loader) Get(name string) (Skill, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.skills[name]
	if !ok {
		return Skill{}, fmt.Errorf("%w: %q", ErrSkillNotFound, name)
	}
	return s, nil
}

// parseSkill 解析 SKILL.md 内容
//
// 格式：
//   ---
//   name: <name>
//   description: "<desc>"
//   ---
//
//   <body>
func parseSkill(filename, content string) (Skill, error) {
	const sep = "\n---\n"

	// 必须以 --- 开头
	if !strings.HasPrefix(content, "---") {
		return Skill{}, fmt.Errorf("file must start with --- frontmatter")
	}

	// 找到第一个 --- 之后的下一个 ---
	rest := strings.TrimPrefix(content, "---")
	// 跳过第一行换行
	rest = strings.TrimPrefix(rest, "\n")
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return Skill{}, fmt.Errorf("missing closing --- for frontmatter")
	}

	front := rest[:idx]
	body := strings.TrimPrefix(rest[idx+len(sep):], "\n")

	name, desc, err := parseFrontmatter(front)
	if err != nil {
		return Skill{}, err
	}

	return Skill{
		Name:        name,
		Description: desc,
		Body:        body,
		SourcePath:  filename,
	}, nil
}

// parseFrontmatter 解析简单的 YAML frontmatter
// 格式（仅支持 key: value 单行）：
//   name: story-x
//   description: "..."
func parseFrontmatter(s string) (name, desc string, err error) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return "", "", fmt.Errorf("invalid frontmatter line: %q", line)
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		// 去引号
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		switch key {
		case "name":
			name = value
		case "description":
			desc = value
		}
	}
	if name == "" {
		return "", "", fmt.Errorf("frontmatter missing 'name'")
	}
	return name, desc, nil
}