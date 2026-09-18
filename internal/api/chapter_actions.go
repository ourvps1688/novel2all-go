package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ourvps1688/novel2all-go/internal/auth"
	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/memory"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// action 名常量（避免 goconst）
const (
	actionExpand   = "expand"
	actionRewrite  = "rewrite"
	actionReview   = "review"
	actionInsert   = "insert"
	actionRollback = "rollback"
)

// skill 名常量
const (
	skillStoryExpand     = "story-expand"
	skillStoryLongWrite  = "story-long-write"
	skillStoryShortWrite = "story-short-write"
	skillStoryReview     = "story-review"
	skillStoryInsert     = "story-insert"
)

// ActionRequest 通用 body 字段
//
// Sprint V1.0.1 P0-B: ProjectID 用于 owner check (0 = 跳过 check, legacy 模式).
type ActionRequest struct {
	ProjectRoot string `json:"project_root"`
	ProjectID   int64  `json:"project_id,omitempty"`
	Instruction string `json:"instruction,omitempty"`
	Position    int    `json:"position,omitempty"`
}

// ActionResponse 通用响应
type ActionResponse struct {
	Chapter       int    `json:"chapter"`
	Action        string `json:"action"`
	OutputPath    string `json:"output_path"`
	BackupPath    string `json:"backup_path,omitempty"`
	CharsBefore   int    `json:"chars_before,omitempty"`
	CharsAfter    int    `json:"chars_after,omitempty"`
	AppendedChars int    `json:"appended_chars,omitempty"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	Message       string `json:"message,omitempty"`
	// Issues Sprint 34c: verifier post-check 报告 (consistency/style/character drift).
	// 为空时 omitempty 不序列化, V0.30 调用方 JSON 兼容.
	Issues []PostCheckIssue `json:"issues,omitempty"`
}

// PostCheckIssue verifier 报告的 1 条问题 (Sprint 34c).
//
// Severity: info/warning/critical.
// Category: consistency/character/style/plot/pacing/other.
// Message:  简短描述 (≤200 字, 不含 markdown).
type PostCheckIssue struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// Verifier action 后置一致性/文风/角色一致性检查接口 (Sprint 34c).
//
// ChapterActions.expand/rewrite/insert 完成后调用, 结果写到 ActionResponse.Issues.
// verifier=nil 时跳过检查 (向后兼容, V0.30 mock 路径).
//
// 期望行为:
//   - 不修改 content, 只读.
//   - 返回 ≤20 条 issue, 多了截断.
//   - 内部失败 (panic) 应被 recover, 不影响 action 成功.
type Verifier interface {
	Check(ctx context.Context, content string) []PostCheckIssue
}

// ReviewResult review 输出
type ReviewResult struct {
	Chapter        int          `json:"chapter"`
	CriticalIssues []ReviewItem `json:"critical_issues"`
	MajorIssues    []ReviewItem `json:"major_issues"`
	MinorIssues    []ReviewItem `json:"minor_issues"`
	QualityScore   float64      `json:"quality_score"`
	OverallVerdict string       `json:"overall_verdict"`
	ContentChars   int          `json:"content_chars"`
	ElapsedMS      int64        `json:"elapsed_ms"`
}

// ReviewItem 单条 review 反馈
type ReviewItem struct {
	Location string `json:"location,omitempty"`
	Category string `json:"category,omitempty"`
	Note     string `json:"note"`
}

// ChapterActions 提供 LLM 操作（expand/rewrite/review/insert/rollback）
type ChapterActions struct {
	executor *skills.Executor

	// Sprint 34: 注入 MemoryManager 让 callLLMSync/callLLMAppend 拼 5 层 memory + settings 到 system prompt.
	// nil = 走 V0.29 mock 路径 (向后兼容).
	memMgr *memory.MemoryManager

	// Sprint 34c: 注入 Verifier, action 完成后跑 post-check 写到 ActionResponse.Issues.
	// nil = 跳过检查 (向后兼容).
	verifier Verifier

	// Sprint 35: 注入 ReferencesLoader, 让 buildActionSystemPrompt 拼 references 段.
	// nil = 跳过 references (V0.30 mock 路径).
	refLoader ReferencesLoader

	// Sprint V1.0.1 P0-B: 注入 ProjectsRepo, 5 个 action 入口做 owner check.
	// nil = 禁用 owner check (legacy 模式, 仅用于直接单元测试).
	projects ProjectsRepo
}

// NewChapterActions 创建
func NewChapterActions(executor *skills.Executor) *ChapterActions {
	return &ChapterActions{executor: executor}
}

// NewChapterActionsWithVerifier 创建带 verifier post-check 的 ChapterActions (Sprint 34c).
//
// 用法:
//
//	actions := api.NewChapterActionsWithVerifier(executor, memMgr, verifier)
//
// 与 NewChapterActions + NewChapterActionsWithMemory 并存; 不破坏现有调用.
// memMgr=nil 跳过 memory 注入 (V0.29 mock 路径).
// verifier=nil 跳过 post-check.
func NewChapterActionsWithVerifier(executor *skills.Executor, memMgr *memory.MemoryManager, v Verifier) *ChapterActions {
	return &ChapterActions{executor: executor, memMgr: memMgr, verifier: v}
}

// NewChapterActionsFull 创建 Sprint 34+35 全功能 ChapterActions.
//
// 参数:
//   - executor: skills 执行器
//   - memMgr:   5 层 memory 注入 (nil = V0.29 mock)
//   - v:        verifier post-check (nil = 跳过)
//   - rl:       references 按 skill 自动加载 (nil = 跳过)
func NewChapterActionsFull(executor *skills.Executor, memMgr *memory.MemoryManager, v Verifier, rl ReferencesLoader) *ChapterActions {
	return &ChapterActions{executor: executor, memMgr: memMgr, verifier: v, refLoader: rl}
}

// NewChapterActionsWithMemory 创建带 memory 注入的 ChapterActions.
//
// Sprint 34 工厂方法: 与 NewChapterActions 签名不同的"with"变体.
// 不破坏现有调用方 (Sprint 18+ 测试都用 NewChapterActions).
//
// 参数:
//   - executor: skills 执行器 (调 LLM)
//   - memMgr: memory manager (5 层 memory 自动加载). 可为 nil → 走 V0.29 mock
func NewChapterActionsWithMemory(executor *skills.Executor, memMgr *memory.MemoryManager) *ChapterActions {
	return &ChapterActions{executor: executor, memMgr: memMgr}
}

// NewChapterActionsWithProjects 创建带 projects 仓库的 ChapterActions (Sprint V1.0.1 P0-B).
//
// 用于 5 个 chapter action 的 owner check (Sprint V1.0.1 P0-B 阻止越权写章节).
// projects == nil → 禁用 owner check (legacy 模式, 直接单元测试用).
//
// 用法 (router.go):
//
//	projects := deps.projectStore  // SQLite adapter
//	actions := NewChapterActionsWithProjects(executor, projects)
func NewChapterActionsWithProjects(executor *skills.Executor, projects ProjectsRepo) *ChapterActions {
	return &ChapterActions{executor: executor, projects: projects}
}

// checkProjectAccess 验证 user 对 project 有访问权 (P0-B owner check helper).
//
// 行为:
//   - user 不在 context → 401 + false
//   - req.ProjectID <= 0 OR a.projects == nil → true (legacy skip, 用于直接调用测试)
//   - project 不存在 → 404 + false
//   - admin OR user.ID == project.OwnerID → true
//   - 其他 → 403 + false
//
// 每个 action 入口调用: if !a.checkProjectAccess(w, r, req) { return }
func (a *ChapterActions) checkProjectAccess(w http.ResponseWriter, r *http.Request, req ActionRequest) bool {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if req.ProjectID <= 0 || a.projects == nil {
		// legacy: 无 project_id 或 projects 仓库未注入, 跳过 owner check
		// (直接单元测试场景, 注入 admin user + project_id=0 即可)
		return true
	}
	project, err := a.projects.Get(req.ProjectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return false
		}
		http.Error(w, fmt.Sprintf(`{"error":"%v"}`, err), http.StatusInternalServerError)
		return false
	}
	if auth.IsAdmin(user) || project.OwnerID == user.ID {
		return true
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	return false
}

// DispatchAction 分发到对应 action
func (a *ChapterActions) DispatchAction(w http.ResponseWriter, r *http.Request, chapter int, action string) {
	switch action {
	case actionExpand:
		a.expand(w, r, chapter)
	case actionRewrite:
		a.rewrite(w, r, chapter)
	case actionReview:
		a.review(w, r, chapter)
	case actionInsert:
		a.insert(w, r, chapter)
	case actionRollback:
		a.rollback(w, r, chapter)
	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusNotFound)
	}
}

func (a *ChapterActions) expand(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if !a.checkProjectAccess(w, r, req) {
		return
	}
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex 防止并发 expand/rewrite/save 覆盖丢失.
	// 取排他锁串行化 read-modify-write (LLM 调 + 写 backup + 写 prosePath).
	defer LockChapterFile(prosePath)()
	before, err := os.ReadFile(prosePath)
	if err != nil {
		respondActionError(w, err, http.StatusNotFound)
		return
	}
	backupPath, err := backupChapterFile(req.ProjectRoot, chapter, before)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	appended, err := a.callLLMAppend(r.Context(), req, chapter, skillStoryExpand, string(before))
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	newContent := string(before) + appended
	if err := os.WriteFile(prosePath, []byte(newContent), 0o644); err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	respondActionOK(w, ActionResponse{
		Chapter:       chapter,
		Action:        actionExpand,
		OutputPath:    prosePath,
		BackupPath:    backupPath,
		CharsBefore:   len([]rune(string(before))),
		CharsAfter:    len([]rune(newContent)),
		AppendedChars: len([]rune(appended)),
		ElapsedMS:     time.Since(start).Milliseconds(),
		Issues:        a.runVerifierCheck(r.Context(), newContent),
	})
}

func (a *ChapterActions) rewrite(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if !a.checkProjectAccess(w, r, req) {
		return
	}
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex (见 expand).
	defer LockChapterFile(prosePath)()
	before, err := os.ReadFile(prosePath)
	if err != nil {
		respondActionError(w, err, http.StatusNotFound)
		return
	}
	backupPath, err := backupChapterFile(req.ProjectRoot, chapter, before)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	rewritten, err := a.callLLMSync(r.Context(), req, chapter, skillStoryLongWrite, string(before))
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(prosePath, []byte(rewritten), 0o644); err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	respondActionOK(w, ActionResponse{
		Chapter:     chapter,
		Action:      actionRewrite,
		OutputPath:  prosePath,
		BackupPath:  backupPath,
		CharsBefore: len([]rune(string(before))),
		CharsAfter:  len([]rune(rewritten)),
		ElapsedMS:   time.Since(start).Milliseconds(),
		Issues:      a.runVerifierCheck(r.Context(), rewritten),
	})
}

func (a *ChapterActions) review(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if !a.checkProjectAccess(w, r, req) {
		return
	}
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex. review 不写文件, 但读取需防
	// 与并发 expand/rewrite 交错 (LLM 评估中途内容变更导致 review 结果无意义).
	defer LockChapterFile(prosePath)()
	content, err := os.ReadFile(prosePath)
	if err != nil {
		respondActionError(w, err, http.StatusNotFound)
		return
	}

	prompt := fmt.Sprintf(
		"请审查以下小说章节，从 plot/consistency/style 三维度找出 critical/major/minor 问题，给 quality_score (0-100)，最后给 overall_verdict (pass/needs_revision/fail)。\n输出严格 JSON。\n\n章节内容:\n%s",
		string(content),
	)
	reviewJSON, err := a.callLLMSync(r.Context(), req, chapter, skillStoryReview, prompt)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	result, perr := parseReviewJSON(chapter, reviewJSON, len([]rune(string(content))), time.Since(start).Milliseconds())
	if perr != nil {
		result = mockReview(chapter, len([]rune(string(content))), time.Since(start).Milliseconds())
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(result)
}

func (a *ChapterActions) insert(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if !a.checkProjectAccess(w, r, req) {
		return
	}
	if req.Position <= 0 {
		http.Error(w, `{"error":"missing 'position' (1-based line number)"}`, http.StatusBadRequest)
		return
	}
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex (见 expand).
	defer LockChapterFile(prosePath)()
	before, err := os.ReadFile(prosePath)
	if err != nil {
		respondActionError(w, err, http.StatusNotFound)
		return
	}
	backupPath, err := backupChapterFile(req.ProjectRoot, chapter, before)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	prompt := fmt.Sprintf("请根据指令插入新段落（保持 1-3 段，不要超过 200 字）：\n%s", req.Instruction)
	inserted, err := a.callLLMSync(r.Context(), req, chapter, skillStoryInsert, prompt)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(before), "\n")
	if req.Position > len(lines) {
		req.Position = len(lines)
	}
	newLines := append([]string{}, lines[:req.Position]...)
	newLines = append(newLines, strings.TrimSpace(inserted))
	newLines = append(newLines, lines[req.Position:]...)
	newContent := strings.Join(newLines, "\n")

	if err := os.WriteFile(prosePath, []byte(newContent), 0o644); err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	respondActionOK(w, ActionResponse{
		Chapter:       chapter,
		Action:        actionInsert,
		OutputPath:    prosePath,
		BackupPath:    backupPath,
		CharsBefore:   len([]rune(string(before))),
		CharsAfter:    len([]rune(newContent)),
		AppendedChars: len([]rune(strings.TrimSpace(inserted))),
		ElapsedMS:     time.Since(start).Milliseconds(),
		Message:       fmt.Sprintf("inserted at line %d", req.Position),
		Issues:        a.runVerifierCheck(r.Context(), newContent),
	})
}

func (a *ChapterActions) rollback(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if !a.checkProjectAccess(w, r, req) {
		return
	}
	projectRoot := req.ProjectRoot
	if projectRoot == "" {
		projectRoot = "."
	}
	prosePath := chapterProsePath(projectRoot, chapter)
	// Sprint V1.0.1 P2: per-file mutex (见 expand).
	// key 用 prosePath (非 backupPath) 保证与 expand/rewrite/save 互斥 — rollback 写入
	// 是覆盖 prosePath, 与其他写路径竞争同一文件. backup 文件不锁 (rollback 不写 backup).
	defer LockChapterFile(prosePath)()

	proseDir := chapterProseDir(projectRoot)
	pattern := fmt.Sprintf("第%03d章.md.bak.*", chapter)
	entries, err := filepath.Glob(filepath.Join(proseDir, pattern))
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}
	if len(entries) == 0 {
		respondActionError(w, fmt.Errorf("no backup found for chapter %d", chapter), http.StatusNotFound)
		return
	}

	var latest string
	var latestTime time.Time
	for _, e := range entries {
		fi, _ := os.Stat(e)
		if fi == nil {
			continue
		}
		if latest == "" || fi.ModTime().After(latestTime) {
			latest = e
			latestTime = fi.ModTime()
		}
	}

	backupData, err := os.ReadFile(latest)
	if err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(prosePath, backupData, 0o644); err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	respondActionOK(w, ActionResponse{
		Chapter:    chapter,
		Action:     actionRollback,
		OutputPath: prosePath,
		BackupPath: latest,
		CharsAfter: len([]rune(string(backupData))),
		Message:    fmt.Sprintf("restored from %s", filepath.Base(latest)),
	})
}

func parseActionRequest(r *http.Request) ActionRequest {
	var req ActionRequest
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = r.URL.Query().Get("project_root")
	}
	if req.ProjectID == 0 {
		if p := r.URL.Query().Get("project_id"); p != "" {
			if n, err := strconv.ParseInt(p, 10, 64); err == nil {
				req.ProjectID = n
			}
		}
	}
	if req.Instruction == "" {
		req.Instruction = r.URL.Query().Get("instruction")
	}
	if req.Position == 0 {
		if p := r.URL.Query().Get("position"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				req.Position = n
			}
		}
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	return req
}

func respondActionOK(w http.ResponseWriter, resp ActionResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func respondActionError(w http.ResponseWriter, err error, status int) {
	if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func backupChapterFile(projectRoot string, chapter int, content []byte) (string, error) {
	prosePath := chapterProsePath(projectRoot, chapter)
	// Sprint V1.0.1 P2: 用 UnixNano + atomic counter 消除同秒撞名.
	// 旧实现 time.Now().Unix() 秒级 timestamp, 同一秒内多次 backup 会撞名
	// 导致第一次 backup 静默丢失. P2 加 per-file mutex 后, 同一章节两次 backup
	// 不再并发, 但仍可能在同一秒内连续发生 (e.g. expand + insert), 用 Nano + 进程内
	// 计数器彻底消除碰撞.
	backupPath := fmt.Sprintf("%s.bak.%d.%d", prosePath, time.Now().UnixNano(), nextBackupSuffix())
	if err := os.WriteFile(backupPath, content, 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

// backupSuffix 进程内单调递增计数器, 保证同一纳秒内多次 backup 文件名唯一.
var backupSuffix uint64

// nextBackupSuffix 原子递增并返回新值 (从 1 起, 0 表示溢出但实际不可能).
func nextBackupSuffix() uint64 {
	return atomic.AddUint64(&backupSuffix, 1)
}

func (a *ChapterActions) callLLMAppend(ctx context.Context, req ActionRequest, chapter int, skill, contextText string) (string, error) {
	if a.executor == nil {
		return mockLLMOutput(skill, contextText), nil
	}
	// Sprint 34.8 流式抵消延迟: ExecuteStream 边接收边积累.
	ch := make(chan llm.Chunk, 32)
	errCh := make(chan error, 1)
	go func() {
		err := a.executor.ExecuteStream(ctx, skills.ExecuteInput{
			SkillName:   skill,
			UserInput:   buildActionUserInput(req, chapter, "continue"),
			SystemInput: a.buildActionSystemPrompt(ctx, req, chapter, contextText),
			Variables:   map[string]any{"chapter": chapter, "context": contextText},
		}, ch)
		errCh <- err
	}()

	var out strings.Builder
	for c := range ch {
		if c.Err != nil {
			return "", c.Err
		}
		out.WriteString(c.Content)
	}
	if err := <-errCh; err != nil {
		return a.callLLMSync(ctx, req, chapter, skill, contextText)
	}
	if out.Len() == 0 {
		return mockLLMOutput(skill, contextText), nil
	}
	return out.String(), nil
}

func (a *ChapterActions) callLLMSync(ctx context.Context, req ActionRequest, chapter int, skill, contextText string) (string, error) {
	if a.executor == nil {
		return mockLLMOutput(skill, contextText), nil
	}
	result, err := a.executor.Execute(ctx, skills.ExecuteInput{
		SkillName:   skill,
		UserInput:   buildActionUserInput(req, chapter, skill),
		SystemInput: a.buildActionSystemPrompt(ctx, req, chapter, contextText),
		Variables:   map[string]any{"chapter": chapter, "context": contextText},
	})
	if err != nil {
		// 失败时回退到 mock, 不阻断业务 (同 V0.29 行为)
		return mockLLMOutput(skill, contextText), nil
	}
	return result.Content, nil
}

// buildActionSystemPrompt 拼 action 的 system prompt (Sprint 34).
//
// 拼接顺序 (与 handleStream 一致):
//  1. 加载 5 层 memory (MemoryManager.LoadForWriting) → 拼到 system prompt
//  2. 加载项目 settings (设定/文风.md + 创作设定.md)
//  3. 加载细纲 (大纲/细纲_第NNN章.md) 作为 chapter context
//
// memMgr=nil 时降级到 "裸" prompt (无 memory, 同 V0.29 行为).
//
// 返回空 string 表示 memMgr 没配置 + 无 settings (executor 会单独用 skill body).
func (a *ChapterActions) buildActionSystemPrompt(ctx context.Context, req ActionRequest, chapter int, contextText string) string {
	var parts []string

	// 1. Memory 5 层 (memMgr 不为 nil 时)
	if a.memMgr != nil {
		if mc, err := a.memMgr.LoadForWriting(ctx, chapter); err == nil {
			parts = append(parts, mc.ToSystemSections()...)
		}
	}

	// 1.5 Sprint 35: References (按 skill 自动加载)
	if a.refLoader != nil {
		// 用 action 推导 skill: chapter actions (expand/rewrite/insert/review) 都用同一基础 skill set
		// V0 简化: 用 chapter 数字区分不出, 全部传 "" 让 LoadForSkill 返回 defaults
		refs := a.refLoader.LoadForSkill(req.skillForReference())
		if len(refs) > 0 {
			var refSection strings.Builder
			refSection.WriteString("# References\n")
			for _, r := range refs {
				body := a.refLoader.LoadByName(r)
				if body == "" {
					continue
				}
				refSection.WriteString("\n\n## ")
				refSection.WriteString(r)
				refSection.WriteString("\n")
				refSection.WriteString(body)
			}
			if refSection.Len() > len("# References\n") {
				parts = append(parts, refSection.String())
			}
		}
	}

	// 2. Project settings (设定/文风.md + 创作设定.md)
	settingMDs := []string{"设定/文风.md", "创作设定.md"}
	for _, rel := range settingMDs {
		fp := filepath.Join(req.ProjectRoot, rel)
		if data, err := os.ReadFile(fp); err == nil {
			parts = append(parts, "# "+rel+"\n"+string(data))
		}
	}

	// 3. 细纲 (chapter context)
	if outline, err := readOutline(req.ProjectRoot, chapter); err == nil && outline != "" {
		parts = append(parts, "# 本章细纲\n"+outline)
	}

	result := strings.Join(parts, "\n\n---\n\n")
	if len(result) > 32000 {
		result = result[:32000] + "\n\n[... truncated ...]"
	}
	return result
}

// skillForReference 从 ActionRequest 推导 reference dispatch skill 名.
//
// Sprint 35: 暂用 action 名 (expand/rewrite/review/insert) 直接映射.
// V1.0: 按 user 配置的 writing_skill 字段 + chapter action type 双维度 dispatch.
func (r ActionRequest) skillForReference() string {
	// 留空 → LoadForSkill 只返回 defaults (style-anchor + deslop-rules + platform-style).
	// action-specific references 通过 defaultSkillDispatch 的空 mapping 覆盖.
	// 后续 Sprint 可在 ActionRequest 加 SkillName 字段让 user 配置.
	return ""
}

func buildActionUserInput(req ActionRequest, chapter int, action string) string {
	if req.Instruction != "" {
		return req.Instruction
	}
	return fmt.Sprintf("章节 %d - 操作 %s", chapter, action)
}

func mockLLMOutput(skill, contextText string) string {
	switch skill {
	case skillStoryExpand:
		return "\n\n[AI 扩写] 在原有基础上，本章新增了角色互动、情节推进和场景描写，让故事更加生动。\n"
	case skillStoryLongWrite, skillStoryShortWrite:
		return fmt.Sprintf("\n\n[AI 重写] 本章重写完成（mock）。基于 %d 字原文。\n", len([]rune(contextText)))
	case skillStoryInsert:
		return "[AI 插入] 新段落补充了情节衔接。\n"
	}
	return "[AI 输出]（mock）\n"
}

func parseReviewJSON(chapter int, raw string, chars int, elapsedMS int64) (*ReviewResult, error) {
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
	matches := re.FindStringSubmatch(raw)
	if matches == nil {
		matches = []string{"", raw}
	}
	var raw2 struct {
		CriticalIssues []ReviewItem `json:"critical_issues"`
		MajorIssues    []ReviewItem `json:"major_issues"`
		MinorIssues    []ReviewItem `json:"minor_issues"`
		QualityScore   float64      `json:"quality_score"`
		OverallVerdict string       `json:"overall_verdict"`
	}
	if err := json.Unmarshal([]byte(matches[1]), &raw2); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &ReviewResult{
		Chapter:        chapter,
		CriticalIssues: raw2.CriticalIssues,
		MajorIssues:    raw2.MajorIssues,
		MinorIssues:    raw2.MinorIssues,
		QualityScore:   raw2.QualityScore,
		OverallVerdict: raw2.OverallVerdict,
		ContentChars:   chars,
		ElapsedMS:      elapsedMS,
	}, nil
}

// mockReview mock review 结果（同类型参数合并）
func mockReview(chapter, chars int, elapsedMS int64) *ReviewResult {
	return &ReviewResult{
		Chapter: chapter,
		CriticalIssues: []ReviewItem{
			{Location: "第 3 段", Category: "plot", Note: "[mock] 主线情节需要强化"},
		},
		MajorIssues: []ReviewItem{
			{Location: "第 1 段", Category: "consistency", Note: "[mock] 时间线与前章略有冲突"},
			{Location: "第 5 段", Category: "style", Note: "[mock] 描写过于冗长"},
		},
		MinorIssues: []ReviewItem{
			{Location: "第 2 段", Category: "language", Note: "[mock] 建议替换更精准的动词"},
		},
		QualityScore:   72.5,
		OverallVerdict: "needs_revision",
		ContentChars:   chars,
		ElapsedMS:      elapsedMS,
	}
}

// runVerifierCheck 安全调 verifier, 防 panic; verifier=nil 返回 nil.
//
// Sprint 34c: expand/rewrite/insert 完成后调, 结果写到 ActionResponse.Issues.
// 截断到 maxIssues (≤20) 防止 ActionResponse 太大.
func (a *ChapterActions) runVerifierCheck(ctx context.Context, content string) []PostCheckIssue {
	if a.verifier == nil {
		return nil
	}
	const maxIssues = 20
	var issues []PostCheckIssue
	func() {
		defer func() {
			if r := recover(); r != nil {
				// verifier 自身 panic → 静默吞, 不影响 action 成功.
				issues = nil
			}
		}()
		issues = a.verifier.Check(ctx, content)
	}()
	if len(issues) > maxIssues {
		issues = issues[:maxIssues]
	}
	return issues
}
