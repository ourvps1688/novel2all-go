package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefault 返回值包含 DefaultProvider 空字符串.
func TestDefault(t *testing.T) {
	d := Default()
	if d.Theme != "system" {
		t.Errorf("Default().Theme = %q, want \"system\"", d.Theme)
	}
	if d.DefaultProvider != "" {
		t.Errorf("Default().DefaultProvider = %q, want \"\"", d.DefaultProvider)
	}
}

// TestLoadMissingFile 文件不存在时返 Default (无错).
func TestLoadMissingFile(t *testing.T) {
	// 临时重写 UserConfigDir 不可行, 但文件不存在时 Load 会返 Default.
	// 直接测试 round-trip: Save → Load.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	// 重命名既有文件以模拟"不存在"
	dir := filepath.Join(tmp, "novel2all-desktop")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// 删除可能的已有 settings.json
	_ = os.Remove(filepath.Join(dir, "settings.json"))

	s, err := Load()
	if err != nil {
		t.Fatalf("Load() 失败 (文件不存在时应返默认): %v", err)
	}
	if s.Theme != "system" {
		t.Errorf("Load() Theme = %q, want system", s.Theme)
	}
}

// TestSaveLoadRoundTrip 写入 + 读回 DefaultProvider 字段保持一致.
func TestSaveLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	want := Settings{
		AutoStart:       true,
		StartMinimized:  true,
		Theme:           "paper",
		DefaultProvider: "deepseek",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("round-trip 不一致:\nwant %+v\ngot  %+v", want, got)
	}
}

// TestJSONKeys 验证 JSON 输出字段名 (避免破坏 settings.json 已存用户的兼容性).
func TestJSONKeys(t *testing.T) {
	s := Settings{
		AutoStart:       true,
		StartMinimized:  false,
		Theme:           "dark",
		DefaultProvider: "dashscope",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	str := string(data)
	for _, key := range []string{
		`"auto_start":true`,
		`"start_minimized":false`,
		`"theme":"dark"`,
		`"default_provider":"dashscope"`,
	} {
		if !strings.Contains(str, key) {
			t.Errorf("JSON 缺少 %q\n实际: %s", key, str)
		}
	}
}

// TestLoadCorruptedFile 文件损坏时返错误 + 默认值.
func TestLoadCorruptedFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	dir := filepath.Join(tmp, "novel2all-desktop")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("not json{"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Load()
	if err == nil {
		t.Errorf("Load() 应返错 (文件损坏)")
	}
	if s.Theme != "system" {
		t.Errorf("损坏文件 Load().Theme = %q, want system (default fallback)", s.Theme)
	}
}