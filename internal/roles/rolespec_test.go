package roles

import (
	"strings"
	"testing"
)

func TestLoadAllRoleSpecs_Count(t *testing.T) {
	specs := AllRoleSpecs()
	if len(specs) != 7 {
		t.Errorf("应加载 7 个 vendor role，实际=%d", len(specs))
	}
	for _, s := range specs {
		if s.Name == "" {
			t.Errorf("spec Name 为空：%+v", s)
		}
		if s.Model == "" {
			t.Errorf("spec %q Model 为空", s.Name)
		}
	}
}

// TestLoadAllRoleSpecs_DisallowedTools 验证 4 个只读 role 的 DisallowedTools 字段正确解析 (A5.13)
func TestLoadAllRoleSpecs_DisallowedTools(t *testing.T) {
	specs := AllRoleSpecs()
	specMap := make(map[string]*RoleSpec)
	for _, s := range specs {
		specMap[s.Name] = s
	}

	// 4 个 role 用 disallowedTools（vendor 只读读系列）
	tests := []struct {
		name string
		want []string
	}{
		{"consistency-checker", []string{"Write", "Edit", "Bash"}},
		{"chapter-extractor", []string{"Write", "Edit", "Bash"}},
		{"story-explorer", []string{"Write", "Edit", "Bash"}},
		{"story-researcher", []string{"Edit"}},
	}
	for _, tt := range tests {
		s, exists := specMap[tt.name]
		if !exists {
			t.Errorf("role %q 不存在", tt.name)
			continue
		}
		if len(s.DisallowedTools) != len(tt.want) {
			t.Errorf("%q DisallowedTools 长度=%d, want %d (got %v)", tt.name, len(s.DisallowedTools), len(tt.want), s.DisallowedTools)
			continue
		}
		for i, want := range tt.want {
			if s.DisallowedTools[i] != want {
				t.Errorf("%q DisallowedTools[%d]=%q, want %q", tt.name, i, s.DisallowedTools[i], want)
			}
		}
	}

	// 2 个 role 不应有 DisallowedTools（full role: arch/writer/designer）
	for _, name := range []string{"story-architect", "narrative-writer", "character-designer"} {
		s := specMap[name]
		if s != nil && len(s.DisallowedTools) != 0 {
			t.Errorf("%q 不应有 DisallowedTools，实际=%v", name, s.DisallowedTools)
		}
	}
}

func TestLoadAllRoleSpecs_AllSevenNames(t *testing.T) {
	expected := map[string]bool{
		"story-architect":     false,
		"narrative-writer":    false,
		"character-designer":  false,
		"consistency-checker": false,
		"chapter-extractor":   false,
		"story-explorer":      false,
		"story-researcher":    false,
	}
	for _, s := range AllRoleSpecs() {
		if _, ok := expected[s.Name]; ok {
			expected[s.Name] = true
		} else {
			t.Errorf("unexpected role name: %q", s.Name)
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing vendor role: %q", name)
		}
	}
}

func TestLoadRoleSpec(t *testing.T) {
	tests := []string{
		"story-architect", "narrative-writer", "character-designer",
		"consistency-checker", "chapter-extractor", "story-explorer",
		"story-researcher",
	}
	for _, name := range tests {
		spec, err := LoadRoleSpec(name)
		if err != nil {
			t.Errorf("LoadRoleSpec(%q): %v", name, err)
			continue
		}
		if spec.Name != name {
			t.Errorf("Name=%q, want %q", spec.Name, name)
		}
		if spec.SystemPrompt == "" {
			t.Errorf("SystemPrompt 应非空")
		}
		if spec.MaxTurns <= 0 {
			t.Errorf("MaxTurns 应 > 0")
		}
	}
}

func TestLoadRoleSpec_NotFound(t *testing.T) {
	_, err := LoadRoleSpec("nonexistent-role")
	if err == nil {
		t.Error("不存在的 role 应报错")
	}
}

func TestLoadRoleSpecByAlias(t *testing.T) {
	tests := []struct {
		alias      string
		wantVendor string
	}{
		{"story_outliner", "story-architect"},
		{"chapter_writer", "narrative-writer"},
		{"story_reviewer", "character-designer"},
		{"consistency_checker", "consistency-checker"},
		{"character_extractor", "chapter-extractor"},
	}
	for _, tt := range tests {
		spec, err := LoadRoleSpecByAlias(tt.alias)
		if err != nil {
			t.Errorf("LoadRoleSpecByAlias(%q): %v", tt.alias, err)
			continue
		}
		if spec.Name != tt.wantVendor {
			t.Errorf("alias=%q → vendor=%q, want %q", tt.alias, spec.Name, tt.wantVendor)
		}
	}
}

func TestLoadRoleSpecByAlias_UnknownAlias(t *testing.T) {
	_, err := LoadRoleSpecByAlias("nonexistent_alias")
	if err == nil {
		t.Error("不存在的 alias 应报错")
	}
}

func TestVendorToAlias(t *testing.T) {
	tests := []struct {
		vendor string
		alias  string
		ok     bool
	}{
		{"story-architect", "story_outliner", true},
		{"narrative-writer", "chapter_writer", true},
		{"character-designer", "story_reviewer", true},
		{"story-explorer", "", true},   // 新 role 无 alias
		{"story-researcher", "", true}, // 新 role 无 alias
		{"unknown-role", "", false},
	}
	for _, tt := range tests {
		alias, ok := VendorToAlias(tt.vendor)
		if ok != tt.ok {
			t.Errorf("VendorToAlias(%q) ok=%v, want %v", tt.vendor, ok, tt.ok)
		}
		if alias != tt.alias {
			t.Errorf("VendorToAlias(%q)=%q, want %q", tt.vendor, alias, tt.alias)
		}
	}
}

func TestAliasToVendor(t *testing.T) {
	tests := []struct {
		alias  string
		vendor string
		ok     bool
	}{
		{"story_outliner", "story-architect", true},
		{"chapter_writer", "narrative-writer", true},
		{"consistency_checker", "consistency-checker", true},
		{"story-explorer", "", false}, // 新 role 无 alias
		{"unknown_alias", "", false},
	}
	for _, tt := range tests {
		vendor, ok := AliasToVendor(tt.alias)
		if ok != tt.ok {
			t.Errorf("AliasToVendor(%q) ok=%v, want %v", tt.alias, ok, tt.ok)
		}
		if vendor != tt.vendor {
			t.Errorf("AliasToVendor(%q)=%q, want %q", tt.alias, vendor, tt.vendor)
		}
	}
}

func TestRoleSpec_Validate_OK(t *testing.T) {
	spec := &RoleSpec{
		Name:     "test",
		Model:    "opus",
		MaxTurns: 30,
		Memory:   "project",
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate 应通过：%v", err)
	}
	if spec.MaxTurns != 30 {
		t.Errorf("MaxTurns 应保留")
	}
}

func TestRoleSpec_Validate_Defaults(t *testing.T) {
	spec := &RoleSpec{Name: "test", Model: "haiku"}
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate 应通过：%v", err)
	}
	if spec.MaxTurns != 30 {
		t.Errorf("默认 MaxTurns 应=30")
	}
	if spec.Memory != "project" {
		t.Errorf("默认 Memory 应=project")
	}
}

func TestRoleSpec_Validate_BadModel(t *testing.T) {
	spec := &RoleSpec{Name: "test", Model: "gpt-5"}
	if err := spec.Validate(); err == nil {
		t.Error("非法 model 应报错")
	}
}

func TestRoleSpec_SystemPromptContainsHeader(t *testing.T) {
	spec, _ := LoadRoleSpec("story-architect")
	if !strings.Contains(spec.SystemPrompt, "# Story Architect") {
		t.Errorf("story-architect prompt 应含 '# Story Architect' 标题")
	}
}

func TestAllRoleNames_Sorted(t *testing.T) {
	names := AllRoleNames()
	if len(names) != 7 {
		t.Errorf("AllRoleNames 长度=%d, want 7", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("未排序：%v", names)
			break
		}
	}
}

// 现有 5 个 Role const 仍然有效（向后兼容）
func TestLegacyRoleConsts_StillValid(t *testing.T) {
	// 不通过 LoadRoleSpec 测，直接用 IsValid（不会因为新 role 加进来而 break）
	legacyRoles := []Role{
		RoleStoryOutliner,
		RoleChapterWriter,
		RoleConsistencyChecker,
		RoleCharacterExtractor,
		RoleStoryReviewer,
	}
	for _, r := range legacyRoles {
		if !IsValid(r) {
			t.Errorf("legacy role %q 应仍然 valid", r)
		}
	}
}

func TestLoadRoleSpec_CachesResult(t *testing.T) {
	// 第一次加载
	spec1, err := LoadRoleSpec("story-architect")
	if err != nil {
		t.Fatal(err)
	}
	// 第二次应返回同一 spec（指针可能不同但内容相同）
	spec2, err := LoadRoleSpec("story-architect")
	if err != nil {
		t.Fatal(err)
	}
	if spec1.Name != spec2.Name {
		t.Errorf("缓存应返回相同内容：%s vs %s", spec1.Name, spec2.Name)
	}
}
