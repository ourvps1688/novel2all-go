// Package obs 提供可观测性原语：结构化日志 + Prometheus metrics。
//
// P0 阶段只实现 slog 封装，metrics 在 P2 阶段加入。
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger 是 novel2all-go 的统一日志器
type Logger struct {
	*slog.Logger
}

// New 创建 Logger
func New(level, format string) *Logger {
	return NewWithWriter(level, format, os.Stdout)
}

// NewWithWriter 允许注入 writer（测试用）
func NewWithWriter(level, format string, w io.Writer) *Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default: // json
		handler = slog.NewJSONHandler(w, opts)
	}

	return &Logger{Logger: slog.New(handler)}
}

// WithRequest 返回带 request_id + method + path 的子 logger
func (l *Logger) WithRequest(ctx context.Context, method, path, requestID string) *Logger {
	return &Logger{Logger: l.Logger.With(
		"request_id", requestID,
		"method", method,
		"path", path,
	)}
}

// Info 等价于 l.Logger.Info(...) 的语法糖
func (l *Logger) Info(msg string, args ...any)  { l.Logger.Info(msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.Logger.Warn(msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.Logger.Error(msg, args...) }
func (l *Logger) Debug(msg string, args ...any) { l.Logger.Debug(msg, args...) }
