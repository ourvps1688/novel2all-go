package roles

import (
	"embed"
	"fmt"
	"strings"
	"sync"
)

// Vendor role asset FS（Sprint A3.2 — 100% 复制 vendor 7 个 role .md）
//
//go:embed assets/*.md
var roleAssets embed.FS

// RoleSpec vendor role 完整规格 (Sprint A3.3 + A3.5)
//
// 对应 vendor oh-story-dsh-0.1.9 的 7 个 role .md frontmatter + body：
//   - Name:         vendor 原名 (e.g. "story-architect")
//   - Alias:        Go 端旧名字（向后兼容，如 "story_outliner"）；新 role 可为空
//   - Description:  角色职责描述 (multiline)
//   - Tools:        允许使用的工具列表 (e.g. ["Read", "Glob"])
//   - DisallowedTools: 禁止使用的工具列表 (vendor 特有)
//   - Model:        vendor 档位 (opus / sonnet / haiku)
//   - MaxTurns:     Agent.Run() 最大循环轮数
//   - Memory:       memory scope (project / user / session)
//   - Skills:       引用的 skill 名列表
//   - SystemPrompt: role .md 的 body 部分（去掉 frontmatter 后）
type RoleSpec struct {
	Name            string
	Alias           string
	Description     string
	Tools           []string
	DisallowedTools []string
	Model           string
	MaxTurns        int
	Memory          string
	Skills          []string
	SystemPrompt    string
}

// String 返回 vendor 原名（方便 print + log）
func (s *RoleSpec) String() string { return s.Name }

// Validate 校验 RoleSpec 是否合法
func (s *RoleSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("role spec: name is required")
	}
	if s.Model == "" {
		return fmt.Errorf("role spec %q: model is required", s.Name)
	}
	switch s.Model {
	case "opus", "sonnet", "haiku":
		// OK
	default:
		return fmt.Errorf("role spec %q: unknown model %q", s.Name, s.Model)
	}
	if s.MaxTurns <= 0 {
		s.MaxTurns = 30 // 默认 30
	}
	if s.Memory == "" {
		s.Memory = "project" // 默认 project scope
	}
	return nil
}

// vendorRoleMapping vendor 原名 → Go Alias 映射 (Sprint A3.5)
//
// 7 个 vendor roles 全部映射：
//   - 5 个旧 role（vendor name != Go alias）：保留向后兼容
//   - 2 个新 role（story-explorer / story-researcher）：vendor 原名直用，Alias 为空
var vendorRoleMapping = map[string]string{
	// vendor 原名 → Go alias (空 = 没旧名字)
	"story-architect":     "story_outliner",
	"narrative-writer":    "chapter_writer",
	"character-designer":  "story_reviewer",
	"consistency-checker": "consistency_checker",
	"chapter-extractor":   "character_extractor",
	"story-explorer":      "", // 新增，无 Go alias
	"story-researcher":    "", // 新增，无 Go alias
}

// inverseAliasMapping Go alias → vendor 原名 (反向索引)
// 从 vendorRoleMapping 自动生成
var inverseAliasMapping = func() map[string]string {
	out := make(map[string]string, len(vendorRoleMapping))
	for vendor, alias := range vendorRoleMapping {
		if alias != "" {
			out[alias] = vendor
		}
	}
	return out
}()

// VendorToAlias vendor 原名 → Go alias
func VendorToAlias(vendor string) (string, bool) {
	alias, ok := vendorRoleMapping[vendor]
	if !ok {
		return "", false
	}
	return alias, true
}

// AliasToVendor Go alias → vendor 原名
func AliasToVendor(alias string) (string, bool) {
	vendor, ok := inverseAliasMapping[alias]
	return vendor, ok
}

// RoleSpecCache 7 个 vendor role 的内存缓存（启动时一次性加载）
//
// 避免每次 LoadRoleSpec 都重新解析 .md frontmatter。
type RoleSpecCache struct {
	mu    sync.RWMutex
	specs map[string]*RoleSpec // key = vendor name
}

// globalRoleCache 全局缓存
var (
	globalRoleCache     *RoleSpecCache
	globalRoleCacheOnce sync.Once
)

// getRoleCache 获取全局缓存（lazy init）
func getRoleCache() *RoleSpecCache {
	globalRoleCacheOnce.Do(func() {
		globalRoleCache = &RoleSpecCache{specs: make(map[string]*RoleSpec)}
		// 启动时加载所有 .md
		all, err := loadAllRoleSpecs()
		if err != nil {
			// 加载失败 → cache 仍初始化但 specs 为空
			return
		}
		globalRoleCache.specs = all
	})
	return globalRoleCache
}

// loadAllRoleSpecs 加载所有 embed.FS 里的 vendor role .md
func loadAllRoleSpecs() (map[string]*RoleSpec, error) {
	entries, err := roleAssets.ReadDir("assets")
	if err != nil {
		return nil, fmt.Errorf("read embedded roles: %w", err)
	}
	out := make(map[string]*RoleSpec, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		// asset 名 = "<vendor-name>.md"（如 "story-architect.md"）
		vendorName := strings.TrimSuffix(name, ".md")
		path := "assets/" + name
		spec, err := parseRoleMD(vendorName, path)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out[spec.Name] = spec
	}
	return out, nil
}

// LoadRoleSpec 加载单个 vendor role (Sprint A3.3)
//
// 优先从 cache 取；cache miss 才解析 .md。
func LoadRoleSpec(vendorName string) (*RoleSpec, error) {
	cache := getRoleCache()
	cache.mu.RLock()
	spec, ok := cache.specs[vendorName]
	cache.mu.RUnlock()
	if ok {
		return spec, nil
	}
	return nil, fmt.Errorf("role spec %q not found in embedded assets", vendorName)
}

// LoadRoleSpecByAlias 按 Go alias 加载（向后兼容）
func LoadRoleSpecByAlias(alias string) (*RoleSpec, error) {
	vendor, ok := AliasToVendor(alias)
	if !ok {
		return nil, fmt.Errorf("role alias %q not recognized (no vendor mapping)", alias)
	}
	return LoadRoleSpec(vendor)
}

// AllRoleSpecs 返回所有 vendor role specs（按 vendor name）
func AllRoleSpecs() []*RoleSpec {
	cache := getRoleCache()
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	out := make([]*RoleSpec, 0, len(cache.specs))
	for _, s := range cache.specs {
		out = append(out, s)
	}
	// 按 name 排序（稳定输出）
	sortSpecsByName(out)
	return out
}

// AllRoleNames 返回所有 vendor role names（按字典序）
func AllRoleNames() []string {
	specs := AllRoleSpecs()
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

// parseRoleMD 解析单个 role .md（拆 frontmatter + body）
func parseRoleMD(vendorName, assetPath string) (*RoleSpec, error) {
	data, err := roleAssets.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", assetPath, err)
	}
	return parseRoleSpecFromMD(vendorName, string(data))
}

// parseRoleSpecFromMD 解析 .md 内容为 RoleSpec
func parseRoleSpecFromMD(defaultName, content string) (*RoleSpec, error) {
	// 复用 agent 包的 Frontmatter 解析逻辑（不依赖 agent 包，避免循环导入）
	fm, body, err := parseYAMLFrontmatter(content)
	if err != nil {
		return nil, err
	}
	spec := &RoleSpec{
		Name:         fm.Name,
		Alias:        vendorRoleMapping[fm.Name],
		Description:  fm.Description,
		Tools:        fm.Tools,
		Model:        fm.Model,
		MaxTurns:     fm.MaxTurns,
		Memory:       fm.Memory,
		Skills:       fm.Skills,
		SystemPrompt: strings.TrimSpace(body),
	}
	// vendor name 为空时用 defaultName（来自文件名）
	if spec.Name == "" {
		spec.Name = defaultName
		spec.Alias = vendorRoleMapping[defaultName]
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// sortSpecsByName 按 Name 排序（稳定输出）
func sortSpecsByName(specs []*RoleSpec) {
	for i := 1; i < len(specs); i++ {
		for j := i; j > 0 && specs[j-1].Name > specs[j].Name; j-- {
			specs[j-1], specs[j] = specs[j], specs[j-1]
		}
	}
}
