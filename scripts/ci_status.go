// Command ci_status 列出 novel2all-go GitHub Actions workflow 状态.
//
// 用法:
//
//	go run scripts/ci_status.go                          # 默认仓库最新 10 个 runs
//	go run scripts/ci_status.go --repo owner/repo        # 指定仓库
//	go run scripts/ci_status.go --watch --interval 30    # 轮询
//	go run scripts/ci_status.go --json                   # JSON 输出
//	go run scripts/ci_status.go --run-id 12345           # 单次 run 详情
//
// 环境变量:
//   - GHCR_TOKEN     Personal Access Token (Sprint 18 起 novel2all-go 用 GHCR_TOKEN, 不是 GITHUB_TOKEN)
//   - NOVEL2ALL_REPO owner/repo 格式 (默认 ourvps1688/novel2all-go)
//
// 设计目的:
//   - 取代 novel2all/scripts/ci_status.py (Python + httpx)
//   - Go 版本零依赖 (除 std lib), build 一次到处跑
//   - 表格 + JSON + watch 三种模式, AI 友好
//
// 编译方式:
//
//	go build -o bin/ci_status scripts/ci_status.go
//	./bin/ci_status --watch
//
//go:build ignore

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	defaultRepo     = "ourvps1688/novel2all-go"
	defaultLimit    = 10
	defaultInterval = 30 // 秒
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ci_status: %v\n", err)
		os.Exit(1)
	}
}

// RunResult 提取关键字段供 AI / JSON 解析.
type RunResult struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	Branch     string `json:"branch"`
	CommitSHA  string `json:"commit_sha"`
	CommitMsg  string `json:"commit_msg,omitempty"`
	Author     string `json:"author,omitempty"`
	Event      string `json:"event"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	DurationS  *int   `json:"duration_seconds,omitempty"`
	URL        string `json:"url"`
	Icon       string `json:"conclusion_icon"`
}

func run(args []string) error {
	fs := flag.NewFlagSet("ci_status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	repoFlag := fs.String("repo", "", "repo (owner/repo), empty = use NOVEL2ALL_REPO or default")
	limit := fs.Int("limit", defaultLimit, "number of runs to fetch")
	runID := fs.Int64("run-id", 0, "fetch single run by ID")
	watch := fs.Bool("watch", false, "poll mode (refresh every --interval seconds)")
	interval := fs.Int("interval", defaultInterval, "poll interval (seconds)")
	jsonOut := fs.Bool("json", false, "JSON output (AI-friendly)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("flag: %w", err)
	}

	repo := *repoFlag
	if repo == "" {
		repo = os.Getenv("NOVEL2ALL_REPO")
	}
	if repo == "" {
		repo = defaultRepo
	}

	if *runID > 0 {
		// 单次 run 详情
		run, err := fetchRun(repo, *runID)
		if err != nil {
			return fmt.Errorf("fetch run %d: %w", *runID, err)
		}
		result := formatRun(run)
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		printDetail(os.Stdout, result)
		return nil
	}

	if *watch {
		return watchMode(repo, *interval)
	}

	runs, err := fetchRuns(repo, *limit)
	if err != nil {
		return fmt.Errorf("fetch runs: %w", err)
	}

	if *jsonOut {
		result := map[string]any{
			"repo":        repo,
			"total_count": len(runs),
			"runs":        formatRuns(runs),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(os.Stdout, "\nnovel2all-go CI 状态 — %s\n", repo)
	printRunsTable(os.Stdout, formatRuns(runs))
	fmt.Fprintln(os.Stdout, "\n[hint] 加 --watch 轮询；加 --json 让 AI 助手解析")
	return nil
}

// fetchRuns 调 GitHub API 获取最新 N 个 workflow runs.
func fetchRuns(repo string, limit int) ([]map[string]any, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs?per_page=%d", repo, limit)
	return doFetchList(url)
}

// fetchRun 获取单次 run 详情.
func fetchRun(repo string, runID int64) (map[string]any, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs/%d", repo, runID)
	return doFetchSingle(url)
}

// doFetchList GET + 解码 {workflow_runs: [...]}.
func doFetchList(url string) ([]map[string]any, error) {
	body, err := doFetch(url)
	if err != nil {
		return nil, err
	}
	var data struct {
		WorkflowRuns []map[string]any `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	return data.WorkflowRuns, nil
}

// doFetchSingle GET + 解码单对象.
func doFetchSingle(url string) (map[string]any, error) {
	body, err := doFetch(url)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode single: %w", err)
	}
	return data, nil
}

// doFetch 通用 GET + 鉴权 + 状态码检查.
func doFetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "novel2all-go-ci-status/0.22")
	if tok := os.Getenv("GHCR_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "token "+tok)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// continue
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("401 Unauthorized — set GHCR_TOKEN env var")
	case http.StatusNotFound:
		return nil, fmt.Errorf("404 Not Found — repo or workflow not found")
	default:
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// formatRuns 批量转换 runs.
func formatRuns(raws []map[string]any) []RunResult {
	out := make([]RunResult, 0, len(raws))
	for _, r := range raws {
		out = append(out, formatRun(r))
	}
	return out
}

// formatRun 提取关键字段.
func formatRun(r map[string]any) RunResult {
	res := RunResult{
		Status:    stringOf(r["status"]),
		URL:       stringOf(r["html_url"]),
		Branch:    stringOf(r["head_branch"]),
		CommitSHA: stringOf(r["head_sha"]),
		Event:     stringOf(r["event"]),
		CreatedAt: stringOf(r["created_at"]),
		UpdatedAt: stringOf(r["updated_at"]),
	}
	if v, ok := r["id"].(float64); ok {
		res.ID = int64(v)
	}
	if v, ok := r["name"].(string); ok {
		res.Name = v
	}
	if v, ok := r["conclusion"].(string); ok {
		res.Conclusion = v
	}
	if hc, ok := r["head_commit"].(map[string]any); ok {
		if msg, ok := hc["message"].(string); ok {
			// 取第一行, 限 80 字符
			if idx := strings.Index(msg, "\n"); idx >= 0 {
				msg = msg[:idx]
			}
			if len(msg) > 80 {
				msg = msg[:80]
			}
			res.CommitMsg = msg
		}
		if author, ok := hc["author"].(map[string]any); ok {
			if name, ok := author["name"].(string); ok {
				res.Author = name
			}
		}
	}
	res.DurationS = calcDuration(r)
	res.Icon = iconOf(res.Status, res.Conclusion)
	return res
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// calcDuration 计算 run 耗时（秒）.
func calcDuration(r map[string]any) *int {
	started, _ := r["run_started_at"].(string)
	updated, _ := r["updated_at"].(string)
	if started == "" || updated == "" {
		return nil
	}
	t1, err1 := time.Parse(time.RFC3339, started)
	t2, err2 := time.Parse(time.RFC3339, updated)
	if err1 != nil || err2 != nil {
		return nil
	}
	d := int(t2.Sub(t1).Seconds())
	return &d
}

// iconOf 状态图标.
func iconOf(status, conclusion string) string {
	if status == "completed" {
		switch conclusion {
		case "success":
			return "OK"
		case "failure":
			return "FAIL"
		case "cancelled":
			return "CANCEL"
		case "skipped":
			return "SKIP"
		case "neutral":
			return "NEUTRAL"
		}
	}
	switch status {
	case "queued":
		return "QUEUED"
	case "in_progress":
		return "RUN"
	case "waiting":
		return "WAIT"
	}
	return "?"
}

// printRunsTable 表格输出.
func printRunsTable(w io.Writer, runs []RunResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tNAME\tBRANCH\tCOMMIT\tDURATION\tWHEN")
	fmt.Fprintln(tw, "------\t----\t------\t------\t--------\t----")
	for _, r := range runs {
		dur := "-"
		if r.DurationS != nil {
			dur = fmt.Sprintf("%ds", *r.DurationS)
		}
		when := r.CreatedAt
		if len(when) > 19 {
			when = when[:19]
		}
		when = strings.ReplaceAll(when, "T", " ")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Icon, trunc(r.Name, 28), trunc(r.Branch, 15), r.CommitSHA[:min(7, len(r.CommitSHA))], dur, when)
	}
	_ = tw.Flush()
}

// trunc 截断字符串.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "."
}

// min 返回较小整数.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// printDetail 单 run 详情.
func printDetail(w io.Writer, r RunResult) {
	fmt.Fprintln(w, strings.Repeat("=", 70))
	fmt.Fprintf(w, "  Run #%d — %s\n", r.ID, r.Name)
	fmt.Fprintln(w, strings.Repeat("=", 70))
	// 状态行: completed 显示 conclusion, 其他显示 status
	if r.Status == "completed" {
		fmt.Fprintf(w, "  Status      : %s %s\n", r.Icon, r.Conclusion)
	} else {
		fmt.Fprintf(w, "  Status      : %s %s\n", r.Icon, r.Status)
	}
	fmt.Fprintf(w, "  Branch      : %s\n", r.Branch)
	fmt.Fprintf(w, "  Commit      : %s — %s\n", r.CommitSHA[:min(7, len(r.CommitSHA))], r.CommitMsg)
	fmt.Fprintf(w, "  Author      : %s\n", r.Author)
	fmt.Fprintf(w, "  Event       : %s\n", r.Event)
	fmt.Fprintf(w, "  Created     : %s\n", r.CreatedAt)
	fmt.Fprintf(w, "  Updated     : %s\n", r.UpdatedAt)
	if r.DurationS != nil {
		fmt.Fprintf(w, "  Duration    : %ds\n", *r.DurationS)
	}
	fmt.Fprintf(w, "  URL         : %s\n", r.URL)
}

// watchMode 轮询模式 (Ctrl+C 退出).
func watchMode(repo string, interval int) error {
	fmt.Fprintf(os.Stderr, "[watch] 轮询 %s, 每 %ds 刷新一次, Ctrl+C 退出\n", repo, interval)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// 立即打印一次
	if err := printOnce(repo, 5); err != nil {
		fmt.Fprintf(os.Stderr, "[watch] error: %v\n", err)
	}

	for range ticker.C {
		fmt.Fprintln(os.Stderr, "---")
		if err := printOnce(repo, 5); err != nil {
			fmt.Fprintf(os.Stderr, "[watch] error: %v\n", err)
			continue
		}
	}
	return nil
}

// printOnce 单次打印 (watch helper).
func printOnce(repo string, limit int) error {
	runs, err := fetchRuns(repo, limit)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "\nnovel2all-go CI 状态 — %s (%s)\n", repo, time.Now().Format("15:04:05"))
	printRunsTable(os.Stdout, formatRuns(runs))
	return nil
}
