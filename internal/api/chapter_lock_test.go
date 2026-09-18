// chapter_lock_test.go 验证 per-file mutex 的并发安全性.
//
// Sprint V1.0.1 P2: chapter 文件并发写不安全, 修复后必须验证:
//  1. 同一章节并发写不会覆盖丢失
//  2. 不同章节并发写仍并行 (锁隔离)
//  3. 备份文件同秒多次创建文件名唯一
//  4. save 与 expand 串行化 (不交错)
//
// 注: race detector 需要 cgo (gcc), CI 默认 `go test ./...` 不带 -race.
// 本测试通过验证串行化行为间接证明锁有效 (锁持有期间 read + LLM + backup +
// write 全部互斥; 即使无 -race, sync.RWMutex 仍提供内存可见性保证).
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ourvps1688/novel2all-go/internal/store"
)

// chapterLockPath 构造测试用的章节文件绝对路径.
func chapterLockPath(dir string, ch int) string {
	return filepath.Join(dir, "prose", chapterFilename(ch))
}

func chapterFilename(ch int) string {
	return "第" + pad3(ch) + "章.md"
}

func pad3(n int) string {
	s := []byte("000")
	i := len(s) - 1
	for n > 0 && i >= 0 {
		s[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(s)
}

// writeChapterFile 写入测试用的章节文件.
func writeChapterFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readFile 读取文件, 失败返回错误.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// adminReq 构造带 admin user context 的 request (用于直接 handler 调用).
// 当前测试不直接调 handler, 全部走 postJSON; 保留此 helper 备未来 rollback/save
// 直接 dispatch 测试使用.
//
//nolint:unused // reserved for future rollback-vs-save direct dispatch test
func adminReq(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	user := &store.User{ID: 999, Role: "admin", Username: "test-admin"}
	ctx := context.WithValue(req.Context(), userCtxValue, user)
	return req.WithContext(ctx)
}

// TestChapterLock_SameChapter_ConcurrentExpand 验证同一章节并发 expand 不覆盖丢失.
//
// Mock LLM (nil executor) 让 expand 调用极快, 但仍走过 read-backup-write 流程.
// 加锁后, 5 次并发 expand 必须按顺序执行, 每次读到上一次的写入结果再追加.
//
// 不加锁时: 同时读 before → backup 同秒撞名 → 各自写 newContent → 第二次 write
// 覆盖第一次 write, backup 静默丢失.
func TestChapterLock_SameChapter_ConcurrentExpand(t *testing.T) {
	dir, h := setupActionsTest(t)
	body := map[string]string{"project_root": dir, "instruction": "扩写一次"}

	const n = 5
	var wg sync.WaitGroup
	var successCount atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := postJSON(h, "/api/chapter/1/expand/", body)
			if rec.Code == http.StatusOK {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != int32(n) {
		t.Errorf("expected %d successful expands, got %d", n, got)
	}

	// 验证最终内容确实被扩写 n 次
	prosePath := chapterLockPath(dir, 1)
	content := readFile(t, prosePath)
	count := strings.Count(content, "[AI 扩写]")
	if count != n {
		t.Errorf("expected %d expansions in chapter file, got %d", n, count)
	}

	// 验证备份文件数 == n (无同秒撞名丢失)
	entries, err := os.ReadDir(filepath.Join(dir, "prose"))
	if err != nil {
		t.Fatalf("read prose dir: %v", err)
	}
	backupCount := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			backupCount++
		}
	}
	if backupCount != n {
		t.Errorf("expected %d backup files (no same-second collision), got %d (entries: %v)",
			n, backupCount, namesOf(entries))
	}
}

// TestChapterLock_DifferentChapters_Parallel 验证不同章节并发操作不被同一锁阻塞.
//
// 不同章节写不同路径, 锁 registry 按 key 隔离, 并发执行.
// 失败信号: 任一 chapter 失败 或 内容污染.
func TestChapterLock_DifferentChapters_Parallel(t *testing.T) {
	dir, h := setupActionsTest(t)

	// 创建 ch 1/2/3 三个文件
	for _, n := range []int{1, 2, 3} {
		writeChapterFile(t, chapterLockPath(dir, n), "# 第 "+pad3(n)+" 章\n内容\n")
	}

	const nChapters = 3
	var wg sync.WaitGroup
	errCh := make(chan string, nChapters)
	for ch := 1; ch <= nChapters; ch++ {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := map[string]string{"project_root": dir, "instruction": "扩写"}
			rec := postJSON(h, "/api/chapter/"+pad3(ch)+"/expand/", body)
			if rec.Code != http.StatusOK {
				errCh <- "ch=" + pad3(ch) + " code=" + itoa(rec.Code) + " body=" + rec.Body.String()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Error("concurrent expand different chapter failed: " + msg)
	}

	// 验证 3 个章节都被扩写
	for ch := 1; ch <= nChapters; ch++ {
		content := readFile(t, chapterLockPath(dir, ch))
		if !strings.Contains(content, "[AI 扩写]") {
			t.Errorf("ch %d not expanded: %s", ch, content)
		}
	}
}

// TestChapterLock_SaveVsExpand_Serializes 验证手动 save 与 LLM expand 串行化.
//
// 场景: 用户前端编辑时触发 expand, 两个请求并发, 加锁后必须不交错.
// 不加锁: save 写的内容被 expand 覆盖, 或 expand 写的内容被 save 覆盖.
// 验证: 最终内容只含 save OR expand 一个标记 (不会两者皆有, 也不会两者皆无).
func TestChapterLock_SaveVsExpand_Serializes(t *testing.T) {
	dir, h := setupActionsTest(t)
	prosePath := chapterLockPath(dir, 1)

	saveBody := map[string]any{
		"project_root": dir,
		"content":      "# 第 1 章\n手动保存的内容 SAVE_MARKER\n",
	}

	var wg sync.WaitGroup
	var saveOK, expandOK bool

	wg.Add(2)
	go func() {
		defer wg.Done()
		rec := postJSON(h, "/api/chapter/1/save/", saveBody)
		saveOK = rec.Code == http.StatusOK
	}()
	go func() {
		defer wg.Done()
		body := map[string]string{"project_root": dir, "instruction": "扩写"}
		rec := postJSON(h, "/api/chapter/1/expand/", body)
		expandOK = rec.Code == http.StatusOK
	}()
	wg.Wait()

	if !saveOK || !expandOK {
		t.Fatalf("saveOK=%v expandOK=%v, expected both true", saveOK, expandOK)
	}

	content := readFile(t, prosePath)
	hasSave := strings.Contains(content, "SAVE_MARKER")
	hasExpand := strings.Contains(content, "[AI 扩写]")
	if hasSave && hasExpand {
		t.Errorf("file contains BOTH save and expand markers (interleaving bug), content: %s", content)
	}
	if !hasSave && !hasExpand {
		t.Errorf("file contains NEITHER marker, lost both writes, content: %s", content)
	}
}

// TestChapterLock_BackupTimestampUnique 验证 backup 同纳秒多次文件名唯一.
//
// 直接调 backupChapterFile 100 次, 验证每个 backup 路径不同.
// 旧实现 time.Now().Unix() 秒级时间戳同秒撞名 → 第一次 backup 静默丢失.
// P2 改 UnixNano + 原子计数器彻底消除碰撞.
func TestChapterLock_BackupTimestampUnique(t *testing.T) {
	dir := t.TempDir()
	prose := filepath.Join(dir, "prose")
	if err := os.MkdirAll(prose, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create source file at prose dir (backup path derived from it).
	srcPath := filepath.Join(prose, "第001章.md")
	if err := os.WriteFile(srcPath, []byte("# ch1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	const n = 100
	paths := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		p, err := backupChapterFile(dir, 1, []byte("backup content"))
		if err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		if paths[p] {
			t.Errorf("backup path collision at i=%d: %s", i, p)
		}
		paths[p] = true
	}
	if len(paths) != n {
		t.Errorf("expected %d unique backup paths, got %d", n, len(paths))
	}
}

// TestChapterLock_LockOf_ReusesSameMutex 验证同路径返回同一 mutex.
//
// 多次调 chapterLockOf(p) 必须返回同一 *sync.RWMutex (地址相等).
// 否则锁无效.
func TestChapterLock_LockOf_ReusesSameMutex(t *testing.T) {
	p := "/tmp/test/chapter.md"
	mu1 := chapterLockOf(p)
	mu2 := chapterLockOf(p)
	if mu1 != mu2 {
		t.Errorf("expected same mutex for same path, got %p vs %p", mu1, mu2)
	}
	// 不同路径返回不同 mutex
	mu3 := chapterLockOf("/tmp/test/other.md")
	if mu1 == mu3 {
		t.Error("expected different mutex for different paths")
	}
}

// TestChapterLock_GlobPatternMatchesBackup 验证 backup 文件 glob 匹配正确.
//
// rollback handler 用 glob "第001章.md.bak.*" 找 latest backup. Windows glob
// 对中文文件名可能不支持, 此测试覆盖 glob 是否真能找到 backup 文件.
func TestChapterLock_GlobPatternMatchesBackup(t *testing.T) {
	dir := t.TempDir()
	prose := filepath.Join(dir, "prose")
	if err := os.MkdirAll(prose, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 创建源 + 3 个 backup (用 backupChapterFile)
	if err := os.WriteFile(filepath.Join(prose, "第001章.md"), []byte("# ch1\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := backupChapterFile(dir, 1, []byte("bk "+itoa(i))); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}

	// glob 找 backup
	pattern := filepath.Join(prose, "第001章.md.bak.*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(matches) != 3 {
		t.Errorf("expected 3 backup files matched by glob, got %d (matches=%v)", len(matches), matches)
	}
}

// --- helpers ---

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
