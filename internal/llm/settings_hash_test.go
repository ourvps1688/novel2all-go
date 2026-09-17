// settings_hash_test.go 测试 ComputeSettingsHash.
package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeSettingsHash_Empty(t *testing.T) {
	got := ComputeSettingsHash("")
	if got != emptyHash {
		t.Errorf("empty root should return emptyHash, got %q", got)
	}

	got = ComputeSettingsHash("/nonexistent/path/that/does/not/exist")
	if got != emptyHash {
		t.Errorf("nonexistent root should return emptyHash, got %q", got)
	}
}

func TestComputeSettingsHash_StableForSameContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "创作设定.md"), []byte("magic system"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := ComputeSettingsHash(dir)
	h2 := ComputeSettingsHash(dir)
	if h1 != h2 {
		t.Errorf("same content should produce same hash: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("hash should be 16 chars, got %d (%q)", len(h1), h1)
	}
}

func TestComputeSettingsHash_ChangesWhenFileChanges(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "创作设定.md")
	if err := os.WriteFile(fp, []byte("v1 content"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := ComputeSettingsHash(dir)

	// 修改文件
	if err := os.WriteFile(fp, []byte("v2 content"), 0o644); err != nil {
		t.Fatal(err)
	}
	h2 := ComputeSettingsHash(dir)

	if h1 == h2 {
		t.Errorf("hash should change when file changes: %q == %q", h1, h2)
	}
}

func TestComputeSettingsHash_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "创作设定.md"), []byte("setting1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 需要先建 设定/ 子目录, 否则 WriteFile 在 Windows 上失败
	if err := os.MkdirAll(filepath.Join(dir, "设定"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "设定", "文风.md"), []byte("style1"), 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := ComputeSettingsHash(dir)
	if h1 == emptyHash {
		t.Errorf("with files hash should not be empty")
	}
}

func TestComputeSettingsHash_RoleFilesOrderIndependent(t *testing.T) {
	// 角色目录 glob 应按文件名排序, 与文件系统返回顺序无关
	dir := t.TempDir()
	rolesDir := filepath.Join(dir, DefaultRolesSubdir)
	if err := os.MkdirAll(rolesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"角色C.md", "角色A.md", "角色B.md"} {
		if err := os.WriteFile(filepath.Join(rolesDir, name), []byte("content-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	h1 := ComputeSettingsHash(dir)
	// 创建顺序不同, hash 应一致
	for _, name := range []string{"角色B.md", "角色C.md", "角色A.md"} {
		_ = os.WriteFile(filepath.Join(rolesDir, name), []byte("content-"+name), 0o644)
	}
	h2 := ComputeSettingsHash(dir)

	if h1 != h2 {
		t.Errorf("角色文件 hash 应按文件名排序稳定: %q != %q", h1, h2)
	}
}
