package roles

import (
	"strings"
	"testing"
)

// TestInquiry_AllRolesDump 输出 7 个 vendor role 的实际加载数据
//
// 这是阶段检查测试 — 不是断言，是输出每个 role 的加载结果供人 review。
// 用途：验证 Sprint A3 完成后 7 个 role 都能正常从 embed.FS 加载，字段完整。
func TestInquiry_AllRolesDump(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping role dump in short mode")
	}
	t.Logf("\n========== Sprint A3 阶段检查：7 vendor role 加载结果 ==========\n")

	roleNames := []string{
		"story-architect", "narrative-writer", "character-designer",
		"consistency-checker", "chapter-extractor",
		"story-explorer", "story-researcher",
	}

	for _, name := range roleNames {
		spec, err := LoadRoleSpec(name)
		if err != nil {
			t.Errorf("LoadRoleSpec(%q): %v", name, err)
			continue
		}
		t.Logf("\n--- %s ---", spec.Name)
		t.Logf("  Alias:        %q", spec.Alias)
		t.Logf("  Model:        %s", spec.Model)
		t.Logf("  MaxTurns:     %d", spec.MaxTurns)
		t.Logf("  Memory:       %q", spec.Memory)
		t.Logf("  Tools:        %v", spec.Tools)
		t.Logf("  Skills:       %v", spec.Skills)
		t.Logf("  Description:  %s",
			truncateForLog(spec.Description, 80))
		t.Logf("  SystemPrompt: %d chars", len(spec.SystemPrompt))
	}
	t.Logf("\n========== 检查结束 ==========")
}

// truncateForLog 截断长字符串用于日志输出
func truncateForLog(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
