// chapter_lock.go 提供章节文件并发读写安全的 per-file 互斥锁.
package api

import "sync"

// chapterFileLocks per-path 互斥锁注册表.
//
// key = 章节文件的绝对路径 (chapterProsePath 返回值), value = *sync.RWMutex.
//
// 用 sync.Map 因为:
//   - 路径数有限 (一本书 ≤ 几百章节), 注册后查询密集 (每次 chapter action 都查)
//   - sync.Map 读多写少场景下查询 O(1) 且无锁, 适合 per-key 锁注册表
//   - 业界惯例: k8s backoff registry / Go 标准库 net/http conn pool 都用 sync.Map
//
// 注册惰性: 路径首次访问时创建 mutex, 后续复用.
var chapterFileLocks sync.Map

// chapterLockOf 获取路径对应的 *sync.RWMutex (惰性创建).
func chapterLockOf(path string) *sync.RWMutex {
	if v, ok := chapterFileLocks.Load(path); ok {
		return v.(*sync.RWMutex)
	}
	mu := &sync.RWMutex{}
	actual, _ := chapterFileLocks.LoadOrStore(path, mu)
	return actual.(*sync.RWMutex)
}

// LockChapterFile 取章节文件排他锁 (write lock).
//
// 返回 release 函数, 调用方用 defer 释放:
//
//	defer LockChapterFile(prosePath)()
//
// 用于写路径 (expand/rewrite/insert/rollback/save/delete) — 这些路径遵循
// read-modify-write 模式, 必须把 read + LLM call + write 串行化防止覆盖丢失.
//
// 用 sync.RWMutex (而非 sync.Mutex) 是为了将来可以加 RLock 给纯读路径 (content/export)
// 让多读并发. P2 当前只暴露 Lock (排他锁), RLock 是预留 API.
func LockChapterFile(path string) func() {
	mu := chapterLockOf(path)
	mu.Lock()
	return mu.Unlock
}
