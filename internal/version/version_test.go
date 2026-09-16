package version

import (
	"encoding/json"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()
	if info.Version == "" {
		t.Error("Version 不应为空")
	}
	if info.Commit == "" {
		t.Error("Commit 不应为空")
	}
	if info.GoVersion == "" {
		t.Error("GoVersion 不应为空")
	}
}

func TestGet_JSON(t *testing.T) {
	info := Get()
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("JSON 序列化失败：%v", err)
	}
	if len(data) == 0 {
		t.Error("JSON 输出不应为空")
	}
}
