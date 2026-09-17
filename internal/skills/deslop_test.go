package skills

import (
	"context"
	"strings"
	"testing"
)

func TestDeslopPromptTemplate_ReplacesPhrases(t *testing.T) {
	input := "在当今时代, 这是一个值得注意的事情. 不可否认, 故事很有趣."
	out := DeslopPromptTemplate(input)
	// 检查 "原文" 这一行的内容 (prompt 规则列表里包含原短语是正常的)
	// 用 marker "原文:" 之后的内容来检查替换
	idx := strings.Index(out, "原文:")
	if idx < 0 {
		t.Fatal("prompt should have 原文: marker")
	}
	originals := out[idx+len("原文:"):]

	// 在 原文 区域不应再出现 "在当今时代" / "不可否认" (已被替换)
	if strings.Contains(originals, "在当今时代") {
		t.Errorf("AI phrase %q not replaced in 原文 section", "在当今时代")
	}
	if strings.Contains(originals, "不可否认") {
		t.Errorf("AI phrase %q not replaced in 原文 section", "不可否认")
	}
}

func TestDeslopPromptTemplate_PreservesContent(t *testing.T) {
	input := "普通的句子没有 AI 味道."
	out := DeslopPromptTemplate(input)
	// 普通文字应保留
	if !strings.Contains(out, "普通的句子") {
		t.Error("regular content should be preserved")
	}
}

func TestDeslopPromptTemplate_HasRules(t *testing.T) {
	out := DeslopPromptTemplate("测试")
	if !strings.Contains(out, "改写") {
		t.Error("prompt should mention 改写")
	}
	if !strings.Contains(out, "原文") {
		t.Error("prompt should mention 原文")
	}
}

func TestApplyDeslop_Empty(t *testing.T) {
	_, err := ApplyDeslop(context.Background(), nil, "")
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestApplyDeslop_Replaces(t *testing.T) {
	out, err := ApplyDeslop(context.Background(), nil, "首先, 这是一个测试.")
	if err != nil {
		t.Fatalf("ApplyDeslop: %v", err)
	}
	// ApplyDeslop 输出是完整 prompt (含规则列表 + 原文回填), 原文区域 "首先" 应被替换
	idx := strings.Index(out, "原文:")
	if idx >= 0 && strings.Contains(out[idx+len("原文:"):], "首先") {
		t.Errorf("AI phrase %q not replaced in 原文 section", "首先")
	}
}

func TestDeslopStage(t *testing.T) {
	s := DeslopStage([]string{"stage1", "stage2"})
	if s.Name != "deslop" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Skill != "story-deslop" {
		t.Errorf("Skill = %q", s.Skill)
	}
	if len(s.Depends) != 2 {
		t.Errorf("depends = %v", s.Depends)
	}
}
