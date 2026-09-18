package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: 创建 sandbox + 测试目录
func setupSandbox(t *testing.T) (Sandbox, string) {
	t.Helper()
	dir := t.TempDir()
	// 写入测试文件
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("Hello, World!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.md"), []byte("# Title\n\nbody text"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 子目录
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewRootSandbox(dir), dir
}

func TestSandbox_Resolve_Relative(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	abs, err := sb.Resolve("foo/bar.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(abs, sb.Root()) {
		t.Errorf("abs=%q 应在 root=%q 下", abs, sb.Root())
	}
}

func TestSandbox_Resolve_DotDotBlocked(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	_, err := sb.Resolve("../etc/passwd")
	if err == nil {
		t.Error("'..' 路径应被拒绝")
	}
}

func TestSandbox_Resolve_AbsoluteOutsideBlocked(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	// 用 .. 逃避（跨平台）
	_, err := sb.Resolve("../../etc/passwd")
	if err == nil {
		t.Error(".. 逃避应被拒绝")
	}
}

func TestSandbox_Validate_AbsoluteInsideAllowed(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	if err := sb.Validate(sb.Root()); err != nil {
		t.Errorf("root 本身应允许：%v", err)
	}
	if err := sb.Validate(filepath.Join(sb.Root(), "sub")); err != nil {
		t.Errorf("root 子路径应允许：%v", err)
	}
}

// --- Read ---

func TestRead_BasicFile(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewReadTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, err := tool.Execute(ctx, []byte(`{"path": "hello.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError {
		t.Errorf("Read 应成功: %s", out.Content)
	}
	if !strings.Contains(out.Content, "Hello, World!") {
		t.Errorf("内容应含 'Hello, World!'，实际=%q", out.Content)
	}
}

func TestRead_FileNotFound(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewReadTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "nope.txt"}`))
	if !out.IsError {
		t.Error("不存在的文件应报错")
	}
}

func TestRead_EscapesSandbox(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewReadTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "../etc/passwd"}`))
	if !out.IsError {
		t.Error("逃逸 sandbox 的路径应被拒绝")
	}
}

// --- Glob ---

func TestGlob_MatchAllMd(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGlobTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "*.md"}`))
	if out.IsError {
		t.Errorf("Glob 应成功：%s", out.Content)
	}
	if !strings.Contains(out.Content, "data.md") {
		t.Errorf("应匹配 data.md，实际=%q", out.Content)
	}
	if strings.Contains(out.Content, "hello.txt") {
		t.Errorf("不应匹配 .txt，实际=%q", out.Content)
	}
}

func TestGlob_Recursive(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGlobTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "**/*.go"}`))
	if out.IsError {
		t.Errorf("Glob 应成功：%s", out.Content)
	}
	if !strings.Contains(out.Content, "sub/nested.go") {
		t.Errorf("应匹配 sub/nested.go，实际=%q", out.Content)
	}
}

func TestGlob_NoMatch(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGlobTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "*.py"}`))
	if out.IsError {
		t.Error("无匹配不应是 error，只是空结果")
	}
	if !strings.Contains(out.Content, "no files matched") {
		t.Errorf("应提示无匹配，实际=%q", out.Content)
	}
}

// --- Grep ---

func TestGrep_FindInFile(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGrepTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "Hello", "path": "hello.txt"}`))
	if out.IsError {
		t.Errorf("Grep 应成功：%s", out.Content)
	}
	if !strings.Contains(out.Content, "Hello, World!") {
		t.Errorf("应匹配 'Hello'，实际=%q", out.Content)
	}
}

func TestGrep_RecursiveDir(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGrepTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "package", "path": "."}`))
	if out.IsError {
		t.Errorf("Grep 应成功：%s", out.Content)
	}
	if !strings.Contains(out.Content, "sub/nested.go") {
		t.Errorf("应匹配 sub/nested.go 的 'package'，实际=%q", out.Content)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewGrepTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"pattern": "[invalid", "path": "."}`))
	if !out.IsError {
		t.Error("非法 regex 应报错")
	}
}

// --- Write ---

func TestWrite_NewFile(t *testing.T) {
	sb, dir := setupSandbox(t)
	tool := NewWriteTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "new.txt", "content": "hello"}`))
	if out.IsError {
		t.Errorf("Write 应成功：%s", out.Content)
	}
	// 验证文件确实写入
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("内容=%q, want 'hello'", string(data))
	}
}

func TestWrite_OverwriteExisting(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewWriteTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "hello.txt", "content": "NEW"}`))
	if out.IsError {
		t.Errorf("Write 应成功：%s", out.Content)
	}
}

func TestWrite_AutoCreateParentDir(t *testing.T) {
	sb, dir := setupSandbox(t)
	tool := NewWriteTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "deep/nested/file.txt", "content": "x"}`))
	if out.IsError {
		t.Errorf("Write 应自动创建父目录：%s", out.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "deep/nested/file.txt")); err != nil {
		t.Errorf("文件未创建：%v", err)
	}
}

func TestWrite_EscapesSandbox(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewWriteTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "../escape.txt", "content": "x"}`))
	if !out.IsError {
		t.Error("逃逸 sandbox 应被拒绝")
	}
}

// --- Edit ---

func TestEdit_ReplaceOnce(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewEditTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "hello.txt", "old_string": "Hello", "new_string": "Hi"}`))
	if out.IsError {
		t.Errorf("Edit 应成功：%s", out.Content)
	}
	// 验证文件内容
	toolRead := NewReadTool(sb)
	out2, _ := toolRead.Execute(ctx, []byte(`{"path": "hello.txt"}`))
	if !strings.Contains(out2.Content, "Hi, World!") {
		t.Errorf("编辑后应含 'Hi, World!'，实际=%q", out2.Content)
	}
}

func TestEdit_OldStringNotFound(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewEditTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "hello.txt", "old_string": "NOTFOUND", "new_string": "x"}`))
	if !out.IsError {
		t.Error("找不到 old_string 应报错")
	}
}

func TestEdit_OldStringNotUnique(t *testing.T) {
	sb, dir := setupSandbox(t)
	// 写一个含多次 "abc" 的文件
	if err := os.WriteFile(filepath.Join(dir, "dup.txt"), []byte("abc\nabc\nabc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewEditTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"path": "dup.txt", "old_string": "abc", "new_string": "x"}`))
	if !out.IsError {
		t.Error("多次匹配应报错（要求唯一）")
	}
}

// --- Bash ---

func TestBash_AllowedCommand(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	// git 是跨平台命令（Windows + Linux 都在 PATH）
	out, _ := tool.Execute(ctx, []byte(`{"command": "git --version"}`))
	if out.IsError {
		t.Errorf("git --version 应允许：%s", out.Content)
	}
	if !strings.Contains(out.Content, "git version") {
		t.Errorf("git --version 输出应含 'git version'，实际=%q", out.Content)
	}
}

func TestBash_DeniedCommand(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"command": "rm -rf /tmp/foo"}`))
	if !out.IsError {
		t.Error("rm 应被黑名单拒绝")
	}
}

func TestBash_NotInWhitelist(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"command": "python --version"}`))
	if !out.IsError {
		t.Error("不在白名单的命令应被拒绝")
	}
}

func TestBash_DotDotBlocked(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"command": "ls ../../etc"}`))
	if !out.IsError {
		t.Error("参数含 '..' 应被拒绝")
	}
}

func TestBash_SensitivePathBlocked(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	out, _ := tool.Execute(ctx, []byte(`{"command": "cat /etc/passwd"}`))
	if !out.IsError {
		t.Error("cat /etc/passwd 应被敏感路径拒绝")
	}
}

func TestBash_PwdWorks(t *testing.T) {
	sb, _ := setupSandbox(t)
	tool := NewBashTool(sb)
	ctx := &ExecContext{Root: sb.Root()}
	// git --version 不依赖 git 仓库
	out, _ := tool.Execute(ctx, []byte(`{"command": "git --no-pager --version"}`))
	if out.IsError {
		t.Errorf("git --version 应允许：%s", out.Content)
	}
}

// --- Registry ---

func TestRegistry_RegisterAndDispatch(t *testing.T) {
	sb, _ := setupSandbox(t)
	reg := NewRegistry()
	if err := reg.Register(NewReadTool(sb)); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(NewWriteTool(sb)); err != nil {
		t.Fatal(err)
	}

	if reg.Count() != 2 {
		t.Errorf("Count=%d, want 2", reg.Count())
	}
	if !reg.Has("Read") || !reg.Has("Write") {
		t.Error("Has 应返回 true")
	}
	if reg.Has("NonExist") {
		t.Error("不存在的 tool Has 应返回 false")
	}

	// Dispatch 调用
	ctx := &ExecContext{Root: sb.Root()}
	out, err := reg.Dispatch("Write", ctx, []byte(`{"path": "via-dispatch.txt", "content": "ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError {
		t.Errorf("Dispatch 应成功：%s", out.Content)
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	reg := NewRegistry()
	t1 := NewReadTool(NewRootSandbox(t.TempDir()))
	if err := reg.Register(t1); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(t1); err == nil {
		t.Error("重复注册应报错")
	}
}

func TestRegistry_ListSorted(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	reg := NewRegistry()
	_ = reg.Register(NewBashTool(sb))
	_ = reg.Register(NewEditTool(sb))
	_ = reg.Register(NewReadTool(sb))

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("List=%v, want 3 items", list)
	}
	// 应按字典序
	if list[0] != "Bash" || list[1] != "Edit" || list[2] != "Read" {
		t.Errorf("List 顺序错：%v", list)
	}
}

func TestRegistry_Schemas(t *testing.T) {
	sb := NewRootSandbox(t.TempDir())
	reg := NewRegistry()
	_ = reg.Register(NewReadTool(sb))
	_ = reg.Register(NewBashTool(sb))

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("Schemas=%d, want 2", len(schemas))
	}
	for _, s := range schemas {
		if s.Name == "" || s.Description == "" || len(s.InputSchema) == 0 {
			t.Errorf("schema 字段缺失：%+v", s)
		}
	}
}
