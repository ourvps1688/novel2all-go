// load_all_settings_test.go - ProjectStructure.LoadAllSettings 单元测试 (Sprint 35.6)
package project

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLoadAllSettings_NoDir(t *testing.T) {
	p := &ProjectStructure{Root: t.TempDir()}
	sections := p.LoadAllSettings()
	if len(sections) != 0 {
		t.Errorf("expected 0 sections when setting dir missing, got %d", len(sections))
	}
}

func TestLoadAllSettings_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "设定"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &ProjectStructure{Root: dir}
	sections := p.LoadAllSettings()
	if len(sections) != 0 {
		t.Errorf("expected 0 sections when setting dir empty, got %d", len(sections))
	}
}

func TestLoadAllSettings_TopLevelFiles(t *testing.T) {
	dir := t.TempDir()
	settingDir := filepath.Join(dir, "设定")
	if err := os.MkdirAll(settingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "文风.md"), []byte("# 文风\n\n短句为主."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "创作设定.md"), []byte("# 设定\n\n类型: 玄幻"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &ProjectStructure{Root: dir}
	sections := p.LoadAllSettings()
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Name != "设定/创作设定.md" {
		t.Errorf("first section = %q, want 设定/创作设定.md", sections[0].Name)
	}
	if sections[1].Name != "设定/文风.md" {
		t.Errorf("second section = %q, want 设定/文风.md", sections[1].Name)
	}
	for _, s := range sections {
		if s.Body == "" {
			t.Errorf("section %q has empty body", s.Name)
		}
	}
}

func TestLoadAllSettings_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	settingDir := filepath.Join(dir, "设定")
	worldDir := filepath.Join(settingDir, "世界观")
	charDir := filepath.Join(settingDir, "角色")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(charDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "文风.md"), []byte("# 文风"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "地图.md"), []byte("# 地图"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charDir, "主角.md"), []byte("# 主角"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &ProjectStructure{Root: dir}
	sections := p.LoadAllSettings()
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections (1 top + 2 nested), got %d", len(sections))
	}
	names := []string{}
	for _, s := range sections {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	expected := []string{
		"设定/世界观/地图.md",
		"设定/文风.md",
		"设定/角色/主角.md",
	}
	if !equalStringSlices(names, expected) {
		t.Errorf("section names mismatch: got %v, want %v", names, expected)
	}
}

func TestLoadAllSettings_SkipHiddenAndNonMd(t *testing.T) {
	dir := t.TempDir()
	settingDir := filepath.Join(dir, "设定")
	if err := os.MkdirAll(settingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "a.md"), []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "b.md"), []byte("# b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "ignore.txt"), []byte("not md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(settingDir, "_draft"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingDir, "_draft", "secret.md"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &ProjectStructure{Root: dir}
	sections := p.LoadAllSettings()
	if len(sections) != 2 {
		t.Errorf("expected 2 sections (skip .txt + _draft), got %d", len(sections))
		for _, s := range sections {
			t.Logf("  got: %s", s.Name)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
