// Package references 提供 reference 文档加载器 (Sprint 35).
//
// reference 是写作工作流用的"参考手册"——覆盖技巧 + 平台特征 + 文风锚定等.
// LLM 在写正文/审稿/扩写时按需加载 (load_dispatch.go 决定 skill → references 映射).
//
// 数据来源:
//  1. embed.FS (assets/*.md) - 内置 starter references, Sprint 35 提供 6 个骨架
//  2. 用户自定义 (user dir) - 未来 Sprint 通过 ReloadFromDisk() 热加载
//
// 设计:
//   - Loader duck-type 满足 api.ReferencesLoader interface (避免 import 循环)
//   - LoadByName(name) string: 单个 reference 内容
//   - LoadForSkill(skill) []string: skill 关联的所有 reference name
//
// 不破坏: V0.30 WriteHandler.refLoader=nil 走 V0.30 mock 路径.
package references

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed assets/*.md
var assetsFS embed.FS

// Loader 加载 reference 文档 (Sprint 35).
//
// 线程安全: loadByNameCache 用 sync.Map 缓存 (避免每次 LLM 调用都读 FS).
type Loader struct {
	// skillDispatch skill → reference names 映射 (Sprint 35 dispatch.go)
	skillDispatch map[string][]string

	// defaultRefs 所有 skill 都加载的 base references
	defaultRefs []string

	// mu 保护 loadByNameCache 写入
	mu              sync.RWMutex
	loadByNameCache map[string]string // name → content (trimmed)
	namesLoaded     bool
}

// NewLoader 构造 + 预热 (cache 所有 embed.FS 文件).
//
// 用法:
//
//	loader := references.NewLoader()
//	contents := loader.LoadForSkill("story-long-write")
//	for _, name := range contents {
//	    body := loader.LoadByName(name)
//	    // 拼到 system prompt
//	}
func NewLoader() *Loader {
	l := &Loader{
		skillDispatch:   defaultSkillDispatch(),
		defaultRefs:     defaultRefs(),
		loadByNameCache: make(map[string]string),
	}
	l.warmup()
	return l
}

// warmup 一次性把 embed.FS 全部 .md 文件读到内存 (省 IO).
//
// 失败: 文件读不出 → cache miss, LoadByName 返回 "" + 不报错 (graceful).
func (l *Loader) warmup() {
	entries, err := assetsFS.ReadDir("assets")
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		data, err := assetsFS.ReadFile("assets/" + e.Name())
		if err != nil {
			continue
		}
		l.loadByNameCache[name] = strings.TrimSpace(string(data))
	}
	l.namesLoaded = true
}

// LoadByName 返回 reference name 的 markdown 内容.
//
// 不存在: 返回 "" (空字符串, 调用方判断).
func (l *Loader) LoadByName(name string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loadByNameCache[name]
}

// LoadForSkill 返回 skill 关联的所有 reference names (按字母排序去重).
//
// 包含: defaultRefs + skillDispatch[skill] (如果有).
// skill="" 或未注册: 只返回 defaultRefs.
//
// 返回的 slice 是 copy, 可安全修改.
func (l *Loader) LoadForSkill(skill string) []string {
	seen := make(map[string]struct{})
	var out []string

	// 1. default refs
	for _, r := range l.defaultRefs {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}

	// 2. skill-specific refs
	if skill != "" {
		for _, r := range l.skillDispatch[skill] {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				out = append(out, r)
			}
		}
	}

	sort.Strings(out)
	return out
}

// AllNames 返回所有已加载 reference names (调试/CLI 用).
func (l *Loader) AllNames() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	names := make([]string, 0, len(l.loadByNameCache))
	for name := range l.loadByNameCache {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListSkills 返回所有已注册 skill (调试/CLI 用).
func (l *Loader) ListSkills() []string {
	skills := make([]string, 0, len(l.skillDispatch))
	for s := range l.skillDispatch {
		skills = append(skills, s)
	}
	sort.Strings(skills)
	return skills
}

// Stats 返回加载统计 (调试/CLI 用).
func (l *Loader) Stats() (loaded, skills, defaultCount int) {
	l.mu.RLock()
	loaded = len(l.loadByNameCache)
	l.mu.RUnlock()
	skills = len(l.skillDispatch)
	defaultCount = len(l.defaultRefs)
	return
}

// ErrReferenceNotFound reference 不存在的 error (LoadByName 调用方判断).
//
// 调用方可用 errors.Is(err, ErrReferenceNotFound) 判断.
var ErrReferenceNotFound = fmt.Errorf("reference not found")
