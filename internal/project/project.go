// Package project 提供项目目录结构统一管理 (Sprint 29).
//
// 对齐 Python V1 core/project.py: ProjectStructure (13 methods).
//
// 之前路径 hardcode 在 tracker.go / config.go / cmd/cli/ 等各处, 现在统一
// 走 ProjectStructure 方法, 防止新增路径字段时散落.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectStructure 项目目录结构.
//
//nolint:revive // 名称与 package 重名, 但 Python 端叫 ProjectStructure, 保留对齐
type ProjectStructure struct {
	// Root 项目根目录.
	Root string
}

// New 构造 (Root 默认 ".").
func New(root string) *ProjectStructure {
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &ProjectStructure{Root: abs}
}

// TrackingStateFile _tracking-state.json 路径.
func (p *ProjectStructure) TrackingStateFile() string {
	return filepath.Join(p.Root, "data", "_tracking-state.json")
}

// SetupMD 创作设定.md 路径.
func (p *ProjectStructure) SetupMD() string {
	return filepath.Join(p.Root, "创作设定.md")
}

// StyleMD 设定/文风.md 路径.
func (p *ProjectStructure) StyleMD() string {
	return filepath.Join(p.Root, "设定", "文风.md")
}

// WorldviewDir 设定/世界观 目录.
func (p *ProjectStructure) WorldviewDir() string {
	return filepath.Join(p.Root, "设定", "世界观")
}

// CharactersDir 设定/角色 目录.
func (p *ProjectStructure) CharactersDir() string {
	return filepath.Join(p.Root, "设定", "角色")
}

// OutlineDir 大纲/ 目录.
func (p *ProjectStructure) OutlineDir() string {
	return filepath.Join(p.Root, "大纲")
}

// ChapterOutline 大纲/细纲_第NNN章.md 路径 (n 必须 ≥ 1).
//
// 命名格式 3 位零填充, 与 Python V1 对齐: 细纲_第005章.md.
func (p *ProjectStructure) ChapterOutline(n int) string {
	return filepath.Join(p.Root, "大纲", fmt.Sprintf("细纲_第%03d章.md", n))
}

// ProseDir 正文/ 目录.
func (p *ProjectStructure) ProseDir() string {
	return filepath.Join(p.Root, "正文")
}

// ChapterProse 正文/第NNN章.md 路径.
func (p *ProjectStructure) ChapterProse(n int) string {
	return filepath.Join(p.Root, "正文", fmt.Sprintf("第%03d章.md", n))
}

// ReferenceLibDir 参考/ 目录.
func (p *ProjectStructure) ReferenceLibDir() string {
	return filepath.Join(p.Root, "参考")
}

// MetadataDir .novel2all/ 目录 (CLI 私有元数据).
func (p *ProjectStructure) MetadataDir() string {
	return filepath.Join(p.Root, ".novel2all")
}

// ChromaDir .chroma/ 向量库目录.
func (p *ProjectStructure) ChromaDir() string {
	return filepath.Join(p.Root, ".chroma")
}

// Init 创建所有项目目录 + 空 state 文件 (idempotent, 已存在跳过).
//
// V0.27.3: 也创建空的 state 文件让 Exists() 立即返回 true, 避免 CLI status
// 误报 "未初始化".
func (p *ProjectStructure) Init() error {
	dirs := []string{
		p.WorldviewDir(),
		p.CharactersDir(),
		p.OutlineDir(),
		p.ProseDir(),
		p.ReferenceLibDir(),
		p.MetadataDir(),
		p.ChromaDir(),
		filepath.Dir(p.TrackingStateFile()),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	// 创建空 state 文件 (如不存在)
	if _, err := os.Stat(p.TrackingStateFile()); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(p.TrackingStateFile(), []byte("{}"), 0o644); err != nil {
			return fmt.Errorf("init state file: %w", err)
		}
	}
	return nil
}

// Exists 检查项目是否已初始化 (state file 存在).
func (p *ProjectStructure) Exists() bool {
	_, err := os.Stat(p.TrackingStateFile())
	return !errors.Is(err, os.ErrNotExist)
}

// Subdirs 列出所有标准子目录 (用于清理 / 备份 / 诊断).
// 包含 data/ (state file parent).
func (p *ProjectStructure) Subdirs() []string {
	return []string{
		filepath.Dir(p.TrackingStateFile()),
		p.WorldviewDir(),
		p.CharactersDir(),
		p.OutlineDir(),
		p.ProseDir(),
		p.ReferenceLibDir(),
		p.MetadataDir(),
		p.ChromaDir(),
	}
}

// LoadAllSettings 加载 设定/ 下所有 .md 文件 (按字母排序), 返回 [(相对路径, 内容)].
//
// Sprint 35: 自动遍历 设定/ + 设定/世界观/ + 设定/角色/ 等子目录.
//
// 文件格式约定: 每个 .md 第一行作为标题 (# XXX), 内容包含 markdown 体.
//
// 返回空 slice 如果目录不存在或没有 .md 文件 (不报错).
//
// 典型用法 (chapter_actions / write handler 拼 system prompt):
//
//	sections := ps.LoadAllSettings()
//	for _, sec := range sections {
//	    // sec.Name = "设定/世界观/地图.md"
//	    // sec.Body = "# 地图\\n\\n..."
//	}
func (p *ProjectStructure) LoadAllSettings() []SettingSection {
	var sections []SettingSection

	settingDir := filepath.Join(p.Root, "设定")
	if !isDir(settingDir) {
		return sections
	}

	// 1. 顶层 设定/*.md (不含子目录)
	entries, err := os.ReadDir(settingDir)
	if err != nil {
		return sections
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		full := filepath.Join(settingDir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		sections = append(sections, SettingSection{
			Name: "设定/" + e.Name(),
			Body: strings.TrimSpace(string(data)),
		})
	}

	// 2. 子目录 (世界观/, 角色/, 历史/, ...) 递归一层 (Sprint 35 简化).
	//
	// 跳过 _开头/ .开头的隐藏目录; 跳过 node_modules / vendor 等.
	for _, e := range entries {
		if !e.IsDir() || skipDirName(e.Name()) {
			continue
		}
		subdir := filepath.Join(settingDir, e.Name())
		subEntries, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".md") {
				continue
			}
			full := filepath.Join(subdir, se.Name())
			data, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			sections = append(sections, SettingSection{
				Name: "设定/" + e.Name() + "/" + se.Name(),
				Body: strings.TrimSpace(string(data)),
			})
		}
	}

	// 按 Name 排序 (确保一致顺序, 便于 diff)
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Name < sections[j].Name
	})
	return sections
}

// SettingSection 一个 .md 配置 (相对路径 + 内容).
type SettingSection struct {
	Name string // e.g. "设定/世界观/地图.md"
	Body string // markdown body
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// skipDirName 判断是否应该跳过目录 (隐藏目录 + 第三方目录).
//
// 规则:
//   - 以 . 开头 (隐藏, e.g. .git, .workbuddy)
//   - 以 _ 开头 (用户标记为草稿/临时, e.g. _draft, _backup)
//   - 知名第三方目录 (node_modules, vendor)
func skipDirName(name string) bool {
	switch name {
	case "node_modules", "vendor":
		return true
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	return false
}
