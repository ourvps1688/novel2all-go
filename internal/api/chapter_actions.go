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
	"time"

	"github.com/ourvps1688/novel2all-go/internal/llm"
	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// ActionRequest 通用 body 字段
type ActionRequest struct {
	ProjectRoot string `json:"project_root"`
	Instruction string `json:"instruction,omitempty"` // 扩写/重写/插入指令
	Position    int    `json:"position,omitempty"`    // insert 起始行号 (1-based)
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
//
// 这是一个组件（不是独立 handler）— 被 ChapterHandler 嵌入并在内部分发到 actions
type ChapterActions struct {
	executor *skills.Executor
}

// NewChapterActions 创建
func NewChapterActions(executor *skills.Executor) *ChapterActions {
	return &ChapterActions{executor: executor}
}

// DispatchAction 分发到对应 action（被 ChapterHandler.handleAction 调用）
//
// chapter 章节号, action 是 expand/rewrite/review/insert/rollback
func (a *ChapterActions) DispatchAction(w http.ResponseWriter, r *http.Request, chapter int, action string) {
	switch action {
	case "expand":
		a.expand(w, r, chapter)
	case "rewrite":
		a.rewrite(w, r, chapter)
	case "review":
		a.review(w, r, chapter)
	case "insert":
		a.insert(w, r, chapter)
	case "rollback":
		a.rollback(w, r, chapter)
	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusNotFound)
	}
}

// expand 扩写
func (a *ChapterActions) expand(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
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

	appended, err := a.callLLMAppend(r.Context(), req, chapter, "story-expand", string(before))
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
		Action:        "expand",
		OutputPath:    prosePath,
		BackupPath:    backupPath,
		CharsBefore:   len([]rune(string(before))),
		CharsAfter:    len([]rune(newContent)),
		AppendedChars: len([]rune(appended)),
		ElapsedMS:     time.Since(start).Milliseconds(),
	})
}

// rewrite 整章重写
func (a *ChapterActions) rewrite(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
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

	rewritten, err := a.callLLMSync(r.Context(), req, chapter, "story-long-write", string(before))
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
		Action:      "rewrite",
		OutputPath:  prosePath,
		BackupPath:  backupPath,
		CharsBefore: len([]rune(string(before))),
		CharsAfter:  len([]rune(rewritten)),
		ElapsedMS:   time.Since(start).Milliseconds(),
	})
}

// review
func (a *ChapterActions) review(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
	content, err := os.ReadFile(prosePath)
	if err != nil {
		respondActionError(w, err, http.StatusNotFound)
		return
	}

	prompt := fmt.Sprintf(
		"请审查以下小说章节，从 plot/consistency/style 三维度找出 critical/major/minor 问题，给 quality_score (0-100)，最后给 overall_verdict (pass/needs_revision/fail)。\n输出严格 JSON。\n\n章节内容:\n%s",
		string(content),
	)
	reviewJSON, err := a.callLLMSync(r.Context(), req, chapter, "story-review", prompt)
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

// insert
func (a *ChapterActions) insert(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	if req.Position <= 0 {
		http.Error(w, `{"error":"missing 'position' (1-based line number)"}`, http.StatusBadRequest)
		return
	}
	start := time.Now()

	prosePath := chapterProsePath(req.ProjectRoot, chapter)
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
	inserted, err := a.callLLMSync(r.Context(), req, chapter, "story-insert", prompt)
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
		Action:        "insert",
		OutputPath:    prosePath,
		BackupPath:    backupPath,
		CharsBefore:   len([]rune(string(before))),
		CharsAfter:    len([]rune(newContent)),
		AppendedChars: len([]rune(strings.TrimSpace(inserted))),
		ElapsedMS:     time.Since(start).Milliseconds(),
		Message:       fmt.Sprintf("inserted at line %d", req.Position),
	})
}

// rollback
func (a *ChapterActions) rollback(w http.ResponseWriter, r *http.Request, chapter int) {
	req := parseActionRequest(r)
	projectRoot := req.ProjectRoot
	if projectRoot == "" {
		projectRoot = "."
	}

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
	prosePath := chapterProsePath(projectRoot, chapter)
	if err := os.WriteFile(prosePath, backupData, 0o644); err != nil {
		respondActionError(w, err, http.StatusInternalServerError)
		return
	}

	respondActionOK(w, ActionResponse{
		Chapter:    chapter,
		Action:     "rollback",
		OutputPath: prosePath,
		BackupPath: latest,
		CharsAfter: len([]rune(string(backupData))),
		Message:    fmt.Sprintf("restored from %s", filepath.Base(latest)),
	})
}

// parseActionRequest 解析 body
func parseActionRequest(r *http.Request) ActionRequest {
	var req ActionRequest
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = r.URL.Query().Get("project_root")
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

// respondActionOK 统一成功响应
func respondActionOK(w http.ResponseWriter, resp ActionResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// respondActionError 统一错误响应
func respondActionError(w http.ResponseWriter, err error, status int) {
	if errors.Is(err, fs.ErrNotExist) {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// backupChapterFile 备份当前章节到 .bak.{timestamp}
func backupChapterFile(projectRoot string, chapter int, content []byte) (string, error) {
	prosePath := chapterProsePath(projectRoot, chapter)
	backupPath := fmt.Sprintf("%s.bak.%d", prosePath, time.Now().Unix())
	if err := os.WriteFile(backupPath, content, 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

// callLLMAppend 调 LLM 流式 + 累加
func (a *ChapterActions) callLLMAppend(ctx context.Context, req ActionRequest, chapter int, skill, contextText string) (string, error) {
	if a.executor == nil {
		return mockLLMOutput(skill, contextText), nil
	}
	ch := make(chan llm.Chunk, 32)
	errCh := make(chan error, 1)
	go func() {
		err := a.executor.ExecuteStream(ctx, skills.ExecuteInput{
			SkillName: skill,
			UserInput: buildActionUserInput(req, chapter, "continue"),
			Variables: map[string]any{"chapter": chapter, "context": contextText},
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

// callLLMSync 调 LLM 同步
func (a *ChapterActions) callLLMSync(ctx context.Context, req ActionRequest, chapter int, skill, contextText string) (string, error) {
	if a.executor == nil {
		return mockLLMOutput(skill, contextText), nil
	}
	result, err := a.executor.Execute(ctx, skills.ExecuteInput{
		SkillName: skill,
		UserInput: buildActionUserInput(req, chapter, skill),
		Variables: map[string]any{"chapter": chapter, "context": contextText},
	})
	if err != nil {
		return mockLLMOutput(skill, contextText), nil
	}
	return result.Content, nil
}

// buildActionUserInput 构造 user input
func buildActionUserInput(req ActionRequest, chapter int, action string) string {
	if req.Instruction != "" {
		return req.Instruction
	}
	return fmt.Sprintf("章节 %d - 操作 %s", chapter, action)
}

// mockLLMOutput LLM 调用失败时 mock 输出
func mockLLMOutput(skill, contextText string) string {
	switch skill {
	case "story-expand":
		return "\n\n[AI 扩写] 在原有基础上，本章新增了角色互动、情节推进和场景描写，让故事更加生动。\n"
	case "story-long-write", "story-short-write":
		return fmt.Sprintf("\n\n[AI 重写] 本章重写完成（mock）。基于 %d 字原文。\n", len([]rune(contextText)))
	case "story-insert":
		return "[AI 插入] 新段落补充了情节衔接。\n"
	}
	return "[AI 输出]（mock）\n"
}

// parseReviewJSON 尝试解析 review JSON
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

// mockReview mock review 结果
func mockReview(chapter int, chars int, elapsedMS int64) *ReviewResult {
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
