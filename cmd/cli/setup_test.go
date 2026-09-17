package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/project"
)

func TestRunSetup_MissingName(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := runSetup(stdout, stderr, []string{"--project=" + dir})
	if err == nil {
		t.Error("expected error for missing --name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected name error, got: %v", err)
	}
}

func TestRunSetup_Success(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	if err := runSetup(stdout, &bytes.Buffer{}, []string{
		"--project=" + dir,
		"--name=测试书",
		"--genre=玄幻",
		"--style=热血",
		"--chapters=100",
		"--words=2000000",
	}); err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	if !strings.Contains(stdout.String(), "项目已初始化") {
		t.Errorf("expected init confirmation, got: %s", stdout.String())
	}
	ps := project.New(dir)
	// 检查目录 + state file
	for _, sub := range ps.Subdirs() {
		if _, err := os.Stat(sub); err != nil {
			t.Errorf("subdir %s missing: %v", sub, err)
		}
	}
	if !ps.Exists() {
		t.Error("Exists() should be true after setup")
	}
	// 检查 创作设定.md
	setupMD := filepath.Join(dir, "创作设定.md")
	data, err := os.ReadFile(setupMD)
	if err != nil {
		t.Fatalf("read setup.md: %v", err)
	}
	if !strings.Contains(string(data), "测试书") {
		t.Error("setup.md should contain project name")
	}
	if !strings.Contains(string(data), "玄幻") {
		t.Error("setup.md should contain genre")
	}
	if !strings.Contains(string(data), "100") {
		t.Error("setup.md should contain chapter target")
	}
}

func TestRunSetup_Idempotent(t *testing.T) {
	dir := t.TempDir()
	args := []string{"--project=" + dir, "--name=test"}
	if err := runSetup(&bytes.Buffer{}, &bytes.Buffer{}, args); err != nil {
		t.Fatal(err)
	}
	// 第二次不应报错 (覆盖 state 但保留 setup.md)
	if err := runSetup(&bytes.Buffer{}, &bytes.Buffer{}, args); err != nil {
		t.Errorf("second setup should not fail: %v", err)
	}
}

func TestGenerateSetupTemplate_AllFields(t *testing.T) {
	tpl := generateSetupTemplate("书名", "genre1", "style1", 10, 100000)
	for _, want := range []string{"书名", "genre1", "style1", "10", "100000", "主角", "故事梗概", "世界观"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("template missing %q", want)
		}
	}
}

func TestGenerateSetupTemplate_DefaultPlaceholders(t *testing.T) {
	tpl := generateSetupTemplate("书名", "", "", 0, 0)
	if !strings.Contains(tpl, "（待填）") {
		t.Error("expected '（待填）' placeholder for empty fields")
	}
	if !strings.Contains(tpl, "（待定）") {
		t.Error("expected '（待定）' placeholder for empty target")
	}
}
