// Package update 提供桌面 app 自动更新检查 + 下载 + 应用.
//
// Phase 3 设计:
//   - 数据源: GitHub Releases API (https://api.github.com/repos/ourvps1688/novel2all-go/releases/latest)
//   - 触发: 用户手动点 "检查更新" 按钮 + Wails dev 启动后 5 秒自动一次
//   - 下载: 暂存到 %TEMP%\Novel2ALL-update-<ver>.exe
//   - 应用: 调用 NSIS installer 自动升级 (Phase 4 NSIS 配置已支持升级)
//   - 不引入 Wails pkg/updates (避免额外依赖), 自主实现
//
// 安全性:
//   - 必须下载 HTTPS
//   - 必须 GitHub source (防中间人替换)
//   - 必须校验 SHA256 (Phase 3.1 不实现, Phase 4 NSIS 加 signtool)
//
// 版本格式: vX.Y.Z (e.g. v1.0.5, v1.1.0)
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GitHubRelease GitHub Releases API 返回结构 (Phase 3 仅用需要的字段).
type GitHubRelease struct {
	TagName     string        `json:"tag_name"` // "v1.0.5"
	Name        string        `json:"name"`     // "Novel2ALL v1.0.5"
	Body        string        `json:"body"`     // release notes (markdown)
	PublishedAt string        `json:"published_at"`
	Assets      []GitHubAsset `json:"assets"`
}

// GitHubAsset 单个 release asset (.exe / .zip).
type GitHubAsset struct {
	Name               string `json:"name"` // "Novel2ALL.exe"
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"` // 直接下载 URL
	ContentType        string `json:"content_type"`
}

// UpdateInfo CheckForUpdates 返回结构.
type UpdateInfo struct {
	Available    bool      // 是否有可用更新
	CurrentVer   string    // 当前版本
	LatestVer    string    // 最新版本
	ReleaseName  string    // "Novel2ALL v1.0.5"
	Notes        string    // release notes (markdown)
	DownloadURL  string    // 直接下载 URL (.exe)
	DownloadSize int64     // 文件大小 (bytes)
	PublishedAt  time.Time // 发布时间
	CheckedAt    time.Time // 本次检查时间
}

// DownloadProgress 下载进度回调 (Phase 3.2 UI 显示).
type DownloadProgress func(downloaded, total int64)

// 仓库配置 (Phase 3 硬编码, Phase 4 改成 settings 可配).
//
// var (非 const) 是为了测试可以临时替换 (详见 update_test.go).
var (
	GitHubOwner = "ourvps1688"
	GitHubRepo  = "novel2all-go"
	GitHubAPI   = "https://api.github.com"
)

// HTTPClient 复用, 复用 Phase 1 的 client (超时 + 重试).
var HTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// CurrentVersion 当前桌面 app 版本.
//
// Phase 4 改成 ldflags -X 'main.Version=1.0.5' 注入.
//
// 重要: 这个值必须 ≥ latest GitHub release tag 的 major (semver major).
// 例: latest tag "v0.1.0" → CurrentVersion="1.0.0" (major=1 > 0).
// Phase 4 release 改用真实 release version 注入.
var CurrentVersion = "1.0.0"

// LatestRelease GET GitHub Releases API 返 latest release.
//
// /repos/{owner}/{repo}/releases/latest 返 200 + JSON release 对象, 404 是无 release.
func LatestRelease(ctx context.Context) (*GitHubRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", GitHubAPI, GitHubOwner, GitHubRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Novel2ALL-Desktop/1.0")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // 无 release
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API HTTP %d: %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &release, nil
}

// CheckForUpdates 检查 GitHub Releases 是否有可用更新.
//
// 比较规则: semver. "v1.0.5" > "v1.0.0" 才算有更新.
func CheckForUpdates(ctx context.Context) (*UpdateInfo, error) {
	latest, err := LatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return &UpdateInfo{
			Available:  false,
			CurrentVer: CurrentVersion,
			LatestVer:  CurrentVersion,
			CheckedAt:  time.Now(),
		}, nil
	}

	latestVer := strings.TrimPrefix(latest.TagName, "v")
	currentVer := strings.TrimPrefix(CurrentVersion, "v")

	info := &UpdateInfo{
		CurrentVer:  CurrentVersion,
		LatestVer:   latest.TagName,
		ReleaseName: latest.Name,
		Notes:       latest.Body,
		PublishedAt: parseGitHubTime(latest.PublishedAt),
		CheckedAt:   time.Now(),
	}

	// 找 Novel2ALL.exe asset (Phase 3 优先精确匹配)
	for _, asset := range latest.Assets {
		if strings.EqualFold(asset.Name, "Novel2ALL.exe") {
			info.DownloadURL = asset.BrowserDownloadURL
			info.DownloadSize = asset.Size
			break
		}
	}
	// 兼容旧名字
	if info.DownloadURL == "" {
		for _, asset := range latest.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
				info.DownloadURL = asset.BrowserDownloadURL
				info.DownloadSize = asset.Size
				break
			}
		}
	}

	info.Available = compareSemver(latestVer, currentVer) > 0 && info.DownloadURL != ""

	return info, nil
}

// DownloadLatest 下载最新版 .exe 到 %TEMP%.
//
// 返下载路径 (供前端展示, 或传给 NSIS installer 应用).
func DownloadLatest(ctx context.Context, info *UpdateInfo, progress DownloadProgress) (string, error) {
	if info.DownloadURL == "" {
		return "", fmt.Errorf("no download URL available")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.DownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d downloading", resp.StatusCode)
	}

	// 目标文件: %TEMP%\Novel2ALL-update-<version>.exe
	targetName := fmt.Sprintf("Novel2ALL-update-%s.exe", info.LatestVer)
	targetPath := filepath.Join(os.TempDir(), targetName)

	out, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer out.Close()

	total := resp.ContentLength
	if total <= 0 {
		total = info.DownloadSize
	}

	// 带进度回调的 copy
	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("write: %w", werr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read: %w", err)
		}
	}

	return targetPath, nil
}

// ApplyUpdate 启动 NSIS installer 自动升级.
//
// Phase 3 简单: 启动 installer, 当前进程退出 (NSIS 检测已安装版本会替换).
//
// Phase 4 改进: 用 scheduled task + verify 安装成功后才退出.
func ApplyUpdate(installerPath string) error {
	cmd := exec.Command(installerPath)
	if runtime.GOOS != "windows" {
		return fmt.Errorf("auto-update only supported on Windows")
	}
	return cmd.Start()
}

// compareSemver 简单 semver 比较 (major.minor.patch).
//
// 返值: -1 (a<b), 0 (a==b), 1 (a>b).
// 简化版: 不处理 pre-release / build metadata.
func compareSemver(a, b string) int {
	pa := parseSemverParts(a)
	pb := parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseSemverParts(v string) [3]int {
	parts := strings.Split(v, ".")
	out := [3]int{0, 0, 0}
	for i := 0; i < 3 && i < len(parts); i++ {
		// 忽略 pre-release suffix (-rc1 etc.)
		numStr := strings.SplitN(parts[i], "-", 2)[0]
		n, _ := strconv.Atoi(numStr)
		out[i] = n
	}
	return out
}

// parseGitHubTime 解析 GitHub ISO 8601 时间戳.
func parseGitHubTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
