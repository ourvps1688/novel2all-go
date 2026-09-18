package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// ErrSkillNotFound 找不到 skill
var ErrSkillNotFound = errors.New("skill not found")

// ErrReferenceNotFound 找不到 reference
var ErrReferenceNotFound = errors.New("skill reference not found")

// Skill 加载后的技能定义 (Sprint A4 完整版)
type Skill struct {
	Name        string   // 来自 frontmatter name
	Description string   // 来自 frontmatter description
	Body        string   // frontmatter 之后的 markdown body
	SourcePath  string   // 原始路径（debug + 日志）
	References  []string // 该 skill 的所有 reference 路径（相对 references/）
	Skills      []string // frontmatter 引用的子 skill 列表
}

// Loader 并发安全的 skill 加载器 (Sprint A4 + 决策 3=B 按需加载)
type Loader struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	// refCache 按需缓存 reference 内容（避免重复读 embed.FS）
	refCache sync.Map // key = "skillName:refPath" → string (content)
}

// NewLoader 从 embed.FS 加载所有 13 个 SKILL.md（顶层，按需 reference）
//
// 决策 3=B：只加载 SKILL.md frontmatter + body + 列出 references 元数据。
// reference content 不在加载时读，调用 LoadReference 时才读（on-demand + L1 cache）。
func NewLoader() (*Loader, error) {
	l := &Loader{
		skills: make(map[string]*Skill),
	}

	// 读 assets/ 顶层目录（13 个 skill）
	entries, err := skillFS.ReadDir("assets")
	if err != nil {
		return nil, fmt.Errorf("read assets dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			// 跳过旧 flat 模式遗留文件
			continue
		}
		skillName := e.Name()
		skill, err := l.parseSkillFromFS(skillName)
		if err != nil {
			return nil, fmt.Errorf("parse skill %s: %w", skillName, err)
		}
		l.skills[skillName] = skill
	}

	return l, nil
}

// parseSkillFromFS 从 embed.FS 读取单个 skill（顶层 SKILL.md + 列出 references）
func (l *Loader) parseSkillFromFS(skillName string) (*Skill, error) {
	skillPath := "assets/" + skillName
	// 读 SKILL.md
	skillMDPath := skillPath + "/SKILL.md"
	data, err := skillFS.ReadFile(skillMDPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", skillMDPath, err)
	}

	name, desc, body, err := parseSkillMD(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", skillMDPath, err)
	}

	// 列出 references（递归：top-level + nested subdirs）
	refsDir := skillPath + "/references"
	refs, err := listReferences(refsDir)
	if err != nil {
		return nil, fmt.Errorf("list references %s: %w", refsDir, err)
	}

	// 提取 frontmatter 中的 skills: 字段（sub-skill references）
	subSkills := extractSkillsFromBody(body)

	return &Skill{
		Name:        name,
		Description: desc,
		Body:        body,
		SourcePath:  skillMDPath,
		References:  refs,
		Skills:      subSkills,
	}, nil
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
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get 按名获取 skill（不读 reference content）
func (l *Loader) Get(name string) (Skill, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.skills[name]
	if !ok {
		return Skill{}, fmt.Errorf("%w: %q", ErrSkillNotFound, name)
	}
	return *s, nil
}

// LoadReference 按需加载 reference 内容（带 L1 cache）
//
// 决策 3=B：按需加载，启动时不全加载所有 242 references。
// Sprint A4 实现：sync.Map 做 L1 cache，避免重复读 embed.FS。
func (l *Loader) LoadReference(skillName, refPath string) (string, error) {
	l.mu.RLock()
	s, ok := l.skills[skillName]
	l.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrSkillNotFound, skillName)
	}

	// 校验 refPath 在该 skill 的 references 列表中（防止 ../ 逃避）
	found := false
	for _, r := range s.References {
		if r == refPath {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("%w: skill %q has no reference %q", ErrReferenceNotFound, skillName, refPath)
	}

	// L1 cache 检查
	cacheKey := skillName + ":" + refPath
	if v, ok := l.refCache.Load(cacheKey); ok {
		return v.(string), nil
	}

	// 从 embed.FS 读
	fullPath := "assets/" + skillName + "/references/" + refPath
	data, err := skillFS.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read reference %s: %w", fullPath, err)
	}
	content := string(data)
	l.refCache.Store(cacheKey, content)
	return content, nil
}

// LoadAllReferences 批量加载某 skill 的所有 references（不常用）
func (l *Loader) LoadAllReferences(skillName string) (map[string]string, error) {
	s, err := l.Get(skillName)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(s.References))
	for _, ref := range s.References {
		content, err := l.LoadReference(skillName, ref)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", ref, err)
		}
		out[ref] = content
	}
	return out, nil
}

// listReferences 递归列出 references/ 目录下所有 .md 文件
//
// 返回路径相对 references/ 目录，例如：
//   - "agent-references/profiles.md"
//   - "common.md"
func listReferences(refsDir string) ([]string, error) {
	var refs []string
	err := fs.WalkDir(skillFS, refsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// 去掉 refsDir + "/" 前缀，得到相对路径
		prefix := refsDir
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		rel := strings.TrimPrefix(path, prefix)
		// 用正斜杠统一（embed.FS 跨平台）
		refs = append(refs, strings.ReplaceAll(rel, "\\", "/"))
		return nil
	})
	if err != nil {
		// references 目录可能不存在（某些 skill 没有 references）
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}

// parseSkillMD 解析 SKILL.md 的 frontmatter + body
//
// 格式：
//
//	---
//	name: story-x
//	description: "..."
//	---
//
//	<body>
func parseSkillMD(content string) (name, desc, body string, err error) {
	const sep = "\n---\n"

	if !strings.HasPrefix(content, "---") {
		return "", "", "", fmt.Errorf("file must start with --- frontmatter")
	}

	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\n")
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return "", "", "", fmt.Errorf("missing closing --- for frontmatter")
	}

	front := rest[:idx]
	body = strings.TrimPrefix(rest[idx+len(sep):], "\n")

	name, desc, err = parseFrontmatter(front)
	if err != nil {
		return "", "", "", err
	}

	return name, desc, body, nil
}

// parseFrontmatter 解析简单的 YAML frontmatter（key: value 单行 + 多行 description: |）
//
// 与 agent 包 / roles 包 parser 类似但独立（避免循环依赖）。
func parseFrontmatter(s string) (name, desc string, err error) {
	var multilineBuf strings.Builder
	inMultiline := false
	var multilineKey string

	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)

		// 多行 description 处理（value: | 后跟缩进内容）
		if inMultiline {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				if multilineBuf.Len() > 0 {
					multilineBuf.WriteByte('\n')
				}
				multilineBuf.WriteString(strings.TrimSpace(line))
				continue
			}
			// 退出 multiline
			if multilineKey == "description" {
				desc = strings.TrimRight(multilineBuf.String(), "\n")
			}
			inMultiline = false
			multilineBuf.Reset()
			// fallthrough 处理当前行
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		colon := strings.Index(line, ":")
		if colon < 0 {
			return "", "", fmt.Errorf("invalid frontmatter line: %q", line)
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])

		// 多行字符串开始
		if value == "|" || value == ">" {
			inMultiline = true
			multilineKey = key
			multilineBuf.Reset()
			continue
		}

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
	// 收尾 multiline
	if inMultiline && multilineKey == "description" {
		desc = strings.TrimRight(multilineBuf.String(), "\n")
	}

	if name == "" {
		return "", "", fmt.Errorf("frontmatter missing 'name'")
	}
	return name, desc, nil
}

// extractSkillsFromBody 和 splitCSV 在 loader_helpers.go 实现
