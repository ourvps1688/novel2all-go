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

// Config 是 novel2all-go 的完整配置
type Config struct {
	HTTP   HTTPConfig
	Log    LogConfig
	DB     DBConfig
	LLM    LLMConfig
	Skills SkillsConfig
	GitHub GitHubConfig
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
}

// SkillsConfig 技能配置
type SkillsConfig struct {
	Dir          string
	MaxParallel  int
}

// GitHubConfig GitHub 集成配置
type GitHubConfig struct {
	Token string
	Repo  string // owner/repo
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
			Host: getEnv("HTTP_HOST", "0.0.0.0"),
			Port: getEnvInt("HTTP_PORT", 8000),
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		DB: DBConfig{
			Driver: getEnv("DB_DRIVER", "sqlite3"),
			DSN:    getEnv("DB_DSN", "data/novel2all.db"),
		},
		LLM: LLMConfig{
			DashScopeAPIKey: getEnv("DASHSCOPE_API_KEY", ""),
			DeepSeekAPIKey:  getEnv("DEEPSEEK_API_KEY", ""),
			MinimaxAPIKey:   getEnv("MINIMAX_API_KEY", ""),
		},
		Skills: SkillsConfig{
			Dir:         getEnv("SKILLS_DIR", "internal/skills/assets"),
			MaxParallel: getEnvInt("SKILLS_MAX_PARALLEL", 4),
		},
		GitHub: GitHubConfig{
			Token: getEnv("GHCR_TOKEN", ""),
			Repo:  getEnv("NOVEL2ALL_REPO", "ourvps1688/novel2all-go"),
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
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("LOG_LEVEL 必须是 debug/info/warn/error 之一，当前=%q", c.Log.Level))
	}

	switch c.Log.Format {
	case "json", "text":
	default:
		errs = append(errs, fmt.Sprintf("LOG_FORMAT 必须是 json/text 之一，当前=%q", c.Log.Format))
	}

	switch c.DB.Driver {
	case "sqlite3":
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

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}