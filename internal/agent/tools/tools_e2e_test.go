package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestToolsE2E_All6CoreTools 6 个 core tool 端到端 (Sprint A6.2)
//
// 真实 sandbox 测试：创建临时目录 + 测试文件，验证 Read/Write/Edit/Bash/Glob/Grep 端到端。
func TestToolsE2E_All6CoreTools(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	testContent := "Hello, World!\nThis is a test.\nLine three.\n"
	if err := os.WriteFile(testFile, []byte(testContent), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.Register(NewReadTool(nil))
	reg.Register(NewWriteTool(nil))
	reg.Register(NewEditTool(nil))
	reg.Register(NewGlobTool(nil))
	reg.Register(NewGrepTool(nil))
	reg.Register(NewBashTool(nil))

	execCtx := &ExecContext{Root: dir}

	t.Run("Read", func(t *testing.T) {
		res, _ := reg.Dispatch("Read", execCtx, []byte(`{"path": "test.txt"}`))
		if res.IsError {
			t.Errorf("Read 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "Hello, World!") {
			t.Errorf("Read 应含文件内容")
		}
	})

	t.Run("Write", func(t *testing.T) {
		outFile := filepath.Join(dir, "out.txt")
		res, _ := reg.Dispatch("Write", execCtx, []byte(`{"path": "out.txt", "content": "written by tool"}`))
		if res.IsError {
			t.Errorf("Write 应成功：%v", res.Content)
		}
		data, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "written by tool" {
			t.Errorf("文件内容不符，got=%q", string(data))
		}
	})

	t.Run("Edit", func(t *testing.T) {
		res, _ := reg.Dispatch("Edit", execCtx, []byte(`{"path": "test.txt", "old_string": "Line three.", "new_string": "Line 3."}`))
		if res.IsError {
			t.Errorf("Edit 应成功：%v", res.Content)
		}
		data, _ := os.ReadFile(testFile)
		if !strings.Contains(string(data), "Line 3.") {
			t.Errorf("Edit 未应用：%q", string(data))
		}
		if strings.Contains(string(data), "Line three.") {
			t.Errorf("Edit 后旧字符串仍在：%q", string(data))
		}
	})

	t.Run("Glob", func(t *testing.T) {
		res, _ := reg.Dispatch("Glob", execCtx, []byte(`{"pattern": "*.txt"}`))
		if res.IsError {
			t.Errorf("Glob 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "test.txt") {
			t.Errorf("Glob 应匹配 test.txt：%q", res.Content)
		}
		if !strings.Contains(res.Content, "out.txt") {
			t.Errorf("Glob 应匹配 out.txt：%q", res.Content)
		}
	})

	t.Run("Grep", func(t *testing.T) {
		res, _ := reg.Dispatch("Grep", execCtx, []byte(`{"pattern": "Hello", "path": "."}`))
		if res.IsError {
			t.Errorf("Grep 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "test.txt:1:Hello") {
			t.Errorf("Grep 应含 test.txt:1:Hello，实际=%q", res.Content)
		}
	})

	t.Run("Bash_echo", func(t *testing.T) {
		// echo 在 Windows PATH 缺失但 git --version 必有
		res, _ := reg.Dispatch("Bash", execCtx, []byte(`{"command": "git --version"}`))
		if res.IsError {
			t.Errorf("Bash 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "git") {
			t.Errorf("Bash 输出应含 'git'，实际=%q", res.Content)
		}
	})
}

// TestToolsE2E_DisallowedToolsVerify 3 个新 mock tool (Sprint A5.10/11/12)
func TestToolsE2E_DisallowedToolsVerify(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewWebSearchTool())
	reg.Register(NewAgentBrowserTool())
	reg.Register(NewCDPTool())
	execCtx := &ExecContext{Root: "/tmp"}

	t.Run("WebSearch", func(t *testing.T) {
		res, _ := reg.Dispatch("WebSearch", execCtx, []byte(`{"query": "Go testing", "max_results": 2}`))
		if res.IsError {
			t.Errorf("WebSearch 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "Go testing") {
			t.Errorf("WebSearch 应含 query")
		}
		if !strings.Contains(res.Content, "MOCK") {
			t.Errorf("WebSearch 应标 MOCK")
		}
	})

	t.Run("AgentBrowser", func(t *testing.T) {
		res, _ := reg.Dispatch("AgentBrowser", execCtx, []byte(`{"url": "https://example.com"}`))
		if res.IsError {
			t.Errorf("AgentBrowser 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "https://example.com") {
			t.Errorf("AgentBrowser 应含 URL")
		}
	})

	t.Run("CDP_navigate", func(t *testing.T) {
		res, _ := reg.Dispatch("CDP", execCtx, []byte(`{"command": "Page.navigate", "params": {"url": "https://example.com"}}`))
		if res.IsError {
			t.Errorf("CDP 应成功：%v", res.Content)
		}
		if !strings.Contains(res.Content, "Navigated") {
			t.Errorf("CDP navigate 应含 Navigated")
		}
	})
}

// TestToolsE2E_SandboxEnforced sandbox 拒绝 ../ 逃避
func TestToolsE2E_SandboxEnforced(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.Register(NewReadTool(nil))

	execCtx := &ExecContext{Root: dir}
	res, _ := reg.Dispatch("Read", execCtx, []byte(`{"path": "../etc/passwd"}`))
	if !res.IsError {
		t.Errorf("../ 逃避应被拒绝，实际=%q", res.Content)
	}
}
