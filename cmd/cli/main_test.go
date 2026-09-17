// main_test.go 测试 cmd/cli 顶层 run() 路由分发.
package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRun_NoArgs 测试无参数时显示 help (exit 0).
func TestRun_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := run(&stdout, &stderr, []string{}); err != nil {
		t.Errorf("无参数应无 error，实际=%v", err)
	}
	if !strings.Contains(stdout.String(), "novel2all CLI") {
		t.Errorf("无参数应输出 help, 实际 stdout=%q", stdout.String()[:100])
	}
}

// TestRun_HelpFlag 测试 --help 显示 help.
func TestRun_HelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := run(&stdout, &stderr, []string{"--help"}); err != nil {
		t.Errorf("--help 应无 error，实际=%v", err)
	}
	if !strings.Contains(stdout.String(), "novel2all CLI") {
		t.Errorf("--help 应输出 help, 实际 stdout=%q", stdout.String()[:100])
	}
}

// TestRun_UnknownCommand 测试未知子命令 → error + exit code 1 (via main).
func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run(&stdout, &stderr, []string{"definitely-not-a-subcommand"})
	if err == nil {
		t.Error("未知子命令应返回 error")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("error 应含 'unknown subcommand', 实际=%v", err)
	}
	// stderr 应输出 help 提示
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Errorf("stderr 应输出 'unknown subcommand' 提示, 实际=%q", stderr.String()[:200])
	}
}

// TestRun_HelpCmd 测试 help 子命令.
func TestRun_HelpCmd(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := run(&stdout, &stderr, []string{"help"}); err != nil {
		t.Errorf("help 子命令应无 error，实际=%v", err)
	}
	if !strings.Contains(stdout.String(), "novel2all CLI") {
		t.Errorf("help 子命令应输出 help, 实际 stdout=%q", stdout.String()[:100])
	}
}

// TestRun_CacheMigrate 测试 cache-migrate 子命令 (Sprint 18 stub).
//
// 不依赖 LLM/DB, 纯 flag 解析 + 输出, 必过.
func TestRun_CacheMigrate(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run(&stdout, &stderr, []string{"cache-migrate", "--dry-run"})
	if err != nil {
		t.Errorf("cache-migrate 应无 error (Sprint 18 stub), 实际=%v", err)
	}
	if !strings.Contains(stderr.String(), "cache-migrate") {
		t.Errorf("stderr 应输出 'cache-migrate' 标签, 实际=%q", stderr.String()[:200])
	}
}
