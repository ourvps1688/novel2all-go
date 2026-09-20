// Package config 提供 novel2all-go 的配置加载与校验。
//
// 加载顺序（后者覆盖前者）：
//  1. .env 文件（路径通过 --config 指定，默认 configs/.env）
//  2. 环境变量
//
// 校验失败返回 error，调用方应优雅退出。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// 配置默认值常量（goconst: 让重复字符串提取为常量）
const (
	defaultHTTPHost    = "0.0.0.0"
	defaultHTTPPort    = 8000
	defaultLogLevel    = "info"
	defaultLogFormat   = "json"
	defaultDBDriver    = "sqlite3"
	defaultDBDSN       = "data/novel2all.db"
	defaultSkillsDir   = "internal/skills/assets"
	defaultMaxParallel = 4
	defaultRepoOwner   = "ourvps1688/novel2all-go"
)

// Config 是 novel2all-go 的完整配置
type Config struct {
	HTTP   HTTPConfig
	Log    LogConfig
	DB     DBConfig
	LLM    LLMConfig
	Skills SkillsConfig
	GitHub GitHubConfig

	// Sprint V1.0.1 P5: Redis 限流器配置.
	// REDIS_URL 空 → 用内存版 RateLimiter (单进程 OK).
	// REDIS_URL 非空 → 用 RedisLimiter (多进程共享).
	Redis RedisConfig
}

// HTTPConfig HTTP 服务配置
type HTTPConfig struct {
	Host string
	Port int
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// DBConfig 数据库配置（P0 仅 sqlite3）
type DBConfig struct {
	Driver string // sqlite3
	DSN    string // 文件路径或连接串
}

// LLMConfig LLM provider 配置（P0 仅校验密钥非空）
type LLMConfig struct {
	DashScopeAPIKey string
	DeepSeekAPIKey  string
	MinimaxAPIKey   string
	AnthropicAPIKey string // P1+ 可选
}

// SkillsConfig 技能配置
type SkillsConfig struct {
	Dir         string
	MaxParallel int
}

// GitHubConfig GitHub 集成配置
type GitHubConfig struct {
	Token string
	Repo  string // owner/repo
}

// RedisConfig Redis 限流器配置 (Sprint V1.0.1 P5).
//
// URL 格式: "host:port" 或 "redis://user:pass@host:port/db".
// URL 为空时 server.Run 用内存版 RateLimiter (单进程 OK, 多进程需各自限流).
type RedisConfig struct {
	URL string
}

// Load 从指定路径加载 .env + 环境变量，返回校验后的 Config
func Load(envPath string) (*Config, error) {
	// 1. 先加载 .env 文件（如果存在），把里面的值注入到环境变量
	//    注意：环境变量优先级更高（loadEnvFile 内部只设未设置过的）
	if envPath != "" {
		if err := loadEnvFile(envPath); err != nil {
			return nil, fmt.Errorf("load env file %s: %w", envPath, err)
		}
	}

	// 2. 再用环境变量（可能来自 .env 或外部注入）初始化 Config
	cfg := &Config{
		HTTP: HTTPConfig{
			Host: getEnv("HTTP_HOST", defaultHTTPHost),
			Port: getEnvInt("HTTP_PORT", defaultHTTPPort),
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", defaultLogLevel),
			Format: getEnv("LOG_FORMAT", defaultLogFormat),
		},
		DB: DBConfig{
			Driver: getEnv("DB_DRIVER", defaultDBDriver),
			DSN:    getEnv("DB_DSN", defaultDBDSN),
		},
		LLM: LLMConfig{
			// Sprint V1.0.1 (2026-09-20): 默认不再从 env 读 admin LLM key.
			// 所有 LLM 调用应通过 X-LLM-Key-{provider} header (desktop Module B 提供).
			// 设 LLM_ALLOW_ADMIN_FALLBACK=true 才读 (向后兼容旧部署).
			DashScopeAPIKey: llmAdminFallbackKey("DASHSCOPE_API_KEY"),
			DeepSeekAPIKey:  llmAdminFallbackKey("DEEPSEEK_API_KEY"),
			MinimaxAPIKey:   llmAdminFallbackKey("MINIMAX_API_KEY"),
			AnthropicAPIKey: llmAdminFallbackKey("ANTHROPIC_API_KEY"),
		},
		Skills: SkillsConfig{
			Dir:         getEnv("SKILLS_DIR", defaultSkillsDir),
			MaxParallel: getEnvInt("SKILLS_MAX_PARALLEL", defaultMaxParallel),
		},
		GitHub: GitHubConfig{
			Token: getEnv("GHCR_TOKEN", ""),
			Repo:  getEnv("NOVEL2ALL_REPO", defaultRepoOwner),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", ""),
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate 校验配置合法性
func (c *Config) Validate() error {
	var errs []string

	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		errs = append(errs, fmt.Sprintf("HTTP_PORT 必须在 1-65535 之间，当前=%d", c.HTTP.Port))
	}

	switch c.Log.Level {
	case "debug", defaultLogLevel, "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("LOG_LEVEL 必须是 debug/info/warn/error 之一，当前=%q", c.Log.Level))
	}

	switch c.Log.Format {
	case defaultLogFormat, "text":
	default:
		errs = append(errs, fmt.Sprintf("LOG_FORMAT 必须是 json/text 之一，当前=%q", c.Log.Format))
	}

	switch c.DB.Driver {
	case defaultDBDriver:
	default:
		errs = append(errs, fmt.Sprintf("DB_DRIVER 必须是 sqlite3（P0 阶段），当前=%q", c.DB.Driver))
	}

	// GitHub repo 格式校验
	if c.GitHub.Repo != "" && !strings.Contains(c.GitHub.Repo, "/") {
		errs = append(errs, fmt.Sprintf("NOVEL2ALL_REPO 必须是 owner/repo 格式，当前=%q", c.GitHub.Repo))
	}

	// LLM keys 在 P0 是 warn（允许 P0 启动但 P1 强制要求）
	// P1 阶段改为 errors.New

	if len(errs) > 0 {
		return errors.New("config validation failed:\n  - " + strings.Join(errs, "\n  - "))
	}

	return nil
}

// loadEnvFile 加载 .env 文件（简单的 KEY=VALUE 格式），把 KEY=VALUE 注入到环境变量
// 注意：只注入未设置过的 key，让外部环境变量优先级更高
func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // .env 不存在不算错（环境变量可能已注入）
		}
		return err
	}

	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return fmt.Errorf("第 %d 行格式错误（缺少 =）：%q", i+1, line)
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		// 去除引号
		value = unquote(value)
		// 只在环境变量未设置时设置（让环境变量优先级更高）
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// llmAdminFallbackKey Sprint V1.0.1 (2026-09-20):
// 默认空字符串 (admin key 关闭). 仅当 LLM_ALLOW_ADMIN_FALLBACK=true 时读 env var.
//
// 设计原因:
// 1. server admin key 增加泄露风险 (systemd Environment 全员可见)
// 2. 集中付费 (admin 付所有用户费用)
// 3. 隐私 (所有用户数据过 admin key)
// 4. 桌面 Module B 已实现 user key 加密存储 + Module B.2 per-request header 注入
//
// 桌面 app 应在 SettingsPage 配置自己的 API key (dashscope/deepseek/minimax).
// 没配 user key 时, LLM 调用返 503 'user API key required, please configure in SettingsPage'.
func llmAdminFallbackKey(envKey string) string {
	if os.Getenv("LLM_ALLOW_ADMIN_FALLBACK") == "true" {
		return os.Getenv(envKey)
	}
	return ""
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
