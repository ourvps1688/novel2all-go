package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunStatus_NotInitialized(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := runStatus(stdout, stderr, []string{"--project=" + dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "未初始化") {
		t.Errorf("expected '未初始化' message, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "novel2all setup") {
		t.Errorf("expected setup hint, got: %s", stdout.String())
	}
}

func TestRunStatus_TableFormat(t *testing.T) {
	dir := t.TempDir()
	// 先 setup 创建完整 state
	if err := runSetup(&bytes.Buffer{}, &bytes.Buffer{}, []string{
		"--project=" + dir, "--name=测试书",
	}); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	if err := runStatus(stdout, &bytes.Buffer{}, []string{"--project=" + dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "项目状态") {
		t.Errorf("expected '项目状态' header, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "项目名") {
		t.Error("expected '项目名' row")
	}
	if !strings.Contains(stdout.String(), "测试书") {
		t.Error("expected project name '测试书' in output")
	}
}

func TestRunStatus_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	if err := runSetup(&bytes.Buffer{}, &bytes.Buffer{}, []string{
		"--project=" + dir, "--name=JSON测试",
	}); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	if err := runStatus(stdout, &bytes.Buffer{}, []string{
		"--project=" + dir, "--format=json",
	}); err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}
	if _, ok := data["project_root"]; !ok {
		t.Error("JSON missing 'project_root' field")
	}
	if data["project_name"] != "JSON测试" {
		t.Errorf("expected project_name=JSON测试, got: %v", data["project_name"])
	}
}
