// Package update tests.
//
// Phase 4 测试:
//   - compareSemver 边界 (major/minor/patch)
//   - parseSemverParts 边界 (含 pre-release suffix)
//   - LatestRelease HTTP 错误处理 (404 = 无 release)
//   - CheckForUpdates 集成: latest < current / > current / = current
package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// 基本 major/minor/patch
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.99.99", "2.0.0", -1},
		// pre-release suffix — Phase 3 简化版不处理 suffix (Phase 4+ 增强)
		{"1.0.0-rc1", "1.0.0-rc1", 0},
		{"1.0.0-rc2", "1.0.0-rc1", 0}, // 简化版忽略 suffix, 认为相等 (未来增强)
		// 0.x 与 1.x
		{"0.1.0", "1.0.0", -1},
		{"1.0.0", "0.1.0", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestParseSemverParts(t *testing.T) {
	tests := []struct {
		in  string
		out [3]int
	}{
		{"1.0.0", [3]int{1, 0, 0}},
		{"2.5.10", [3]int{2, 5, 10}},
		{"1.0.0-rc1", [3]int{1, 0, 0}}, // suffix 忽略
		// 短版本 (major only)
		{"3", [3]int{3, 0, 0}},
		// 异常
		{"", [3]int{0, 0, 0}},
		{"abc", [3]int{0, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parseSemverParts(tt.in)
			if got != tt.out {
				t.Errorf("parseSemverParts(%q) = %v, want %v", tt.in, got, tt.out)
			}
		})
	}
}

func TestParseGitHubTime(t *testing.T) {
	t.Run("RFC3339", func(t *testing.T) {
		got := parseGitHubTime("2026-09-19T07:19:05Z")
		if got.IsZero() {
			t.Error("expected non-zero time")
		}
		if got.Year() != 2026 || got.Month() != 9 || got.Day() != 19 {
			t.Errorf("unexpected parsed time: %v", got)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		got := parseGitHubTime("not-a-time")
		if !got.IsZero() {
			t.Errorf("expected zero time, got %v", got)
		}
	})
}

func TestLatestRelease_404(t *testing.T) {
	// Mock GitHub API 返 404 (无 release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// 临时替换 GitHubAPI
	orig := GitHubAPI
	GitHubAPI = server.URL
	defer func() { GitHubAPI = orig }()

	ctx := context.Background()
	release, err := LatestRelease(ctx)
	if err != nil {
		t.Errorf("404 should not return error, got %v", err)
	}
	if release != nil {
		t.Errorf("404 should return nil release, got %+v", release)
	}
}

func TestCheckForUpdates_HasUpdate(t *testing.T) {
	// Mock GitHub API 返 v1.5.0 release
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GitHubRelease{
			TagName:     "v1.5.0",
			Name:        "Novel2ALL v1.5.0",
			Body:        "更新日志",
			PublishedAt: "2026-09-19T07:19:05Z",
			Assets: []GitHubAsset{
				{Name: "Novel2ALL.exe", Size: 12000000, BrowserDownloadURL: "https://example.com/Novel2ALL.exe"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 临时替换 GitHubAPI
	orig := GitHubAPI
	GitHubAPI = server.URL
	defer func() { GitHubAPI = orig }()

	// 当前 1.0.0, 最新 1.5.0 → 应有更新
	origVer := CurrentVersion
	CurrentVersion = "1.0.0"
	defer func() { CurrentVersion = origVer }()

	ctx := context.Background()
	info, err := CheckForUpdates(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if !info.Available {
		t.Error("expected Available=true")
	}
	if info.LatestVer != "v1.5.0" {
		t.Errorf("expected LatestVer=v1.5.0, got %q", info.LatestVer)
	}
	if !strings.HasPrefix(info.DownloadURL, "https://") {
		t.Errorf("expected DownloadURL to start with https://, got %q", info.DownloadURL)
	}
}

func TestCheckForUpdates_NoUpdate(t *testing.T) {
	// Mock GitHub API 返 1.0.0 (与当前相同)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GitHubRelease{
			TagName: "v1.0.0",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	orig := GitHubAPI
	GitHubAPI = server.URL
	defer func() { GitHubAPI = orig }()

	origVer := CurrentVersion
	CurrentVersion = "1.0.0"
	defer func() { CurrentVersion = origVer }()

	ctx := context.Background()
	info, err := CheckForUpdates(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Available {
		t.Error("expected Available=false (same version)")
	}
}
