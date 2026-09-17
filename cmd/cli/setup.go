// setup.go — Sprint 27 CLI 子命令: setup
//
// novel2all setup --name=<项目名> [--genre=玄幻] [--style=...]
// 初始化项目目录 + 写 创作设定.md 模板 + 初始化 tracker state.
//
// 对齐 Python V1 cli/main.py setup() 命令.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ourvps1688/novel2all-go/internal/memory"
	"github.com/ourvps1688/novel2all-go/internal/project"
)

// runSetup flag 解析 + 执行初始化.
func runSetup(stdout io.Writer, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "项目名（书名）")
	genre := fs.String("genre", "", "题材（如 玄幻 / 都市 / 言情）")
	style := fs.String("style", "", "文风锚点")
	chapters := fs.Int("chapters", 0, "目标章节数")
	words := fs.Int("words", 0, "目标字数")
	projectRoot := fs.String("project", ".", "项目根目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("setup: --name is required")
	}

	ps := project.New(*projectRoot)
	if err := ps.Init(); err != nil {
		return fmt.Errorf("init dirs: %w", err)
	}

	// 写 创作设定.md 模板 (如不存在)
	if _, err := os.Stat(ps.SetupMD()); os.IsNotExist(err) {
		if err := os.WriteFile(ps.SetupMD(), []byte(generateSetupTemplate(*name, *genre, *style, *chapters, *words)), 0o644); err != nil {
			return fmt.Errorf("write setup.md: %w", err)
		}
	}

	// 初始化 tracker state (用 tracker.Init 创建新 state, 然后覆盖其他字段)
	tracker := memory.NewTracker(ps.TrackingStateFile())
	state, err := tracker.Init(*name)
	if err != nil {
		return fmt.Errorf("init tracker: %w", err)
	}
	state.Genre = *genre
	state.StyleAnchor = *style
	state.TotalChaptersTarget = *chapters
	state.TotalWordCountTarget = *words
	if err := tracker.Write(state); err != nil {
		return fmt.Errorf("write tracker: %w", err)
	}

	fmt.Fprintf(stdout, "[OK] 项目已初始化: %s\n", ps.Root)
	fmt.Fprintf(stdout, "  项目名: %s\n", state.ProjectName)
	fmt.Fprintf(stdout, "  跟踪文件: %s\n", ps.TrackingStateFile())
	fmt.Fprintf(stdout, "\n下一步:\n")
	fmt.Fprintf(stdout, "  novel2all status   # 查看状态\n")
	fmt.Fprintf(stdout, "  编辑 创作设定.md 完善设定\n")
	return nil
}

// generateSetupTemplate 生成 创作设定.md 模板内容.
//
// 与 Python V1 cli/setup() 字符串对齐.
func generateSetupTemplate(name, genre, style string, chapters, words int) string {
	var sb strings.Builder
	sb.WriteString("# 创作设定\n\n")
	fmt.Fprintf(&sb, "## 项目名\n%s\n\n", name)
	fmt.Fprintf(&sb, "## 题材\n%s\n\n", orDefault(genre, "（待填）"))
	fmt.Fprintf(&sb, "## 文风\n%s\n\n", orDefault(style, "（待填）"))
	sb.WriteString("## 目标\n")
	if chapters > 0 {
		fmt.Fprintf(&sb, "- 章节数：%d\n", chapters)
	} else {
		sb.WriteString("- 章节数：（待定）\n")
	}
	if words > 0 {
		fmt.Fprintf(&sb, "- 字数：%d\n", words)
	} else {
		sb.WriteString("- 字数：（待定）\n")
	}
	sb.WriteString("\n## 主角\n（待填）\n\n")
	sb.WriteString("## 故事梗概\n（待填）\n\n")
	sb.WriteString("## 主线冲突\n（待填）\n\n")
	sb.WriteString("## 世界观设定\n（待填）\n\n")
	sb.WriteString("## 力量体系\n（待填）\n")
	return sb.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
