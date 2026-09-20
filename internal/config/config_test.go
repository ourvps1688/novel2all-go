package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// 清空可能的环境变量
	for _, k := range []string{"HTTP_PORT", "LOG_LEVEL", "LOG_FORMAT", "DB_DRIVER", "NOVEL2ALL_REPO"} {
		_ = os.Unsetenv(k)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}

	if cfg.HTTP.Port != 8000 {
		t.Errorf("默认 HTTP_PORT 应为 8000，实际=%d", cfg.HTTP.Port)
	}
	if cfg.Log.Level != defaultLogLevel {
		t.Errorf("默认 LOG_LEVEL 应为 info，实际=%q", cfg.Log.Level)
	}
	if cfg.Log.Format != defaultLogFormat {
		t.Errorf("默认 LOG_FORMAT 应为 json，实际=%q", cfg.Log.Format)
	}
	if cfg.DB.Driver != defaultDBDriver {
		t.Errorf("默认 DB_DRIVER 应为 sqlite3，实际=%q", cfg.DB.Driver)
	}
	if cfg.GitHub.Repo != "ourvps1688/novel2all-go" {
		t.Errorf("默认 NOVEL2ALL_REPO 应为 ourvps1688/novel2all-go，实际=%q", cfg.GitHub.Repo)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("HTTP_PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}

	if cfg.HTTP.Port != 9000 {
		t.Errorf("HTTP_PORT 应为 9000，实际=%d", cfg.HTTP.Port)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("LOG_LEVEL 应为 debug，实际=%q", cfg.Log.Level)
	}
}

func TestLoad_DotEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := `HTTP_PORT=8765
LOG_LEVEL=warn
NOVEL2ALL_REPO=test/repo
# 这是注释

INVALID_LINE_NO_EQUAL
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时 .env 失败：%v", err)
	}

	_, err := Load(envPath)
	if err == nil {
		t.Error("应该返回错误（INVALID_LINE_NO_EQUAL 格式错误）")
	}
}

func TestLoad_DotEnvValid(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := `HTTP_PORT=8888
LOG_LEVEL=warn
NOVEL2ALL_REPO=test/repo
# 注释
DASHSCOPE_API_KEY=test-dashscope-key
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时 .env 失败：%v", err)
	}

	for _, k := range []string{"HTTP_PORT", "LOG_LEVEL", "NOVEL2ALL_REPO", "DASHSCOPE_API_KEY"} {
		_ = os.Unsetenv(k)
	}

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}

	if cfg.HTTP.Port != 8888 {
		t.Errorf("HTTP_PORT 应为 8888，实际=%d", cfg.HTTP.Port)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("LOG_LEVEL 应为 warn，实际=%q", cfg.Log.Level)
	}
	if cfg.GitHub.Repo != "test/repo" {
		t.Errorf("NOVEL2ALL_REPO 应为 test/repo，实际=%q", cfg.GitHub.Repo)
	}
	// Sprint V1.0.1 (2026-09-20): DASHSCOPE_API_KEY 默认应为空 (admin key 关闭)
	if cfg.LLM.DashScopeAPIKey != "" {
		t.Errorf("DASHSCOPE_API_KEY 应默认空字符串 (admin key fallback 关闭)，实际=%q", cfg.LLM.DashScopeAPIKey)
	}
}

// TestLoad_DotEnvWithAdminFallback 测试显式开启 LLM_ALLOW_ADMIN_FALLBACK=true 时 .env DASHSCOPE_API_KEY 会被加载.
func TestLoad_DotEnvWithAdminFallback(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	content := `DASHSCOPE_API_KEY=admin-dashscope-fallback-key
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时 .env 失败：%v", err)
	}
	defer os.Unsetenv("LLM_ALLOW_ADMIN_FALLBACK")
	defer os.Unsetenv("DASHSCOPE_API_KEY")

	for _, k := range []string{"LLM_ALLOW_ADMIN_FALLBACK", "DASHSCOPE_API_KEY"} {
		_ = os.Unsetenv(k)
	}
	os.Setenv("LLM_ALLOW_ADMIN_FALLBACK", "true")

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if cfg.LLM.DashScopeAPIKey != "admin-dashscope-fallback-key" {
		t.Errorf("LLM_ALLOW_ADMIN_FALLBACK=true 时 DASHSCOPE_API_KEY 应被加载，实际=%q", cfg.LLM.DashScopeAPIKey)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := &Config{}
	cfg.HTTP.Port = 0
	cfg.Log.Level = defaultLogLevel
	cfg.Log.Format = defaultLogFormat
	cfg.DB.Driver = defaultDBDriver
	cfg.Skills.MaxParallel = 4

	err := cfg.Validate()
	if err == nil {
		t.Error("port=0 应该报错")
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	cfg := &Config{}
	cfg.HTTP.Port = 8000
	cfg.Log.Level = "invalid"
	cfg.Log.Format = defaultLogFormat
	cfg.DB.Driver = defaultDBDriver

	err := cfg.Validate()
	if err == nil {
		t.Error("LOG_LEVEL=invalid 应该报错")
	}
}

func TestValidate_InvalidRepo(t *testing.T) {
	cfg := &Config{}
	cfg.HTTP.Port = 8000
	cfg.Log.Level = defaultLogLevel
	cfg.Log.Format = defaultLogFormat
	cfg.DB.Driver = defaultDBDriver
	cfg.GitHub.Repo = "no-slash"

	err := cfg.Validate()
	if err == nil {
		t.Error("NOVEL2ALL_REPO 缺斜杠应该报错")
	}
}
