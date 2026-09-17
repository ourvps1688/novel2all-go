// status.go — Sprint 27 CLI 子命令: status
//
// novel2all status — 查看项目状态 (Tracker + tabwriter Table).
//
// 对齐 Python V1 cli/main.py status() 命令.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/ourvps1688/novel2all-go/internal/memory"
	"github.com/ourvps1688/novel2all-go/internal/project"
)

const statusDefaultProject = "."

// runStatus 列出项目状态到 stdout.
func runStatus(stdout, stderr io.Writer, args []string) error {
	projectRoot := statusDefaultProject
	format := "table"
	for i := 0; i < len(args); i++ {
		a := args[i]
		// 支持 "--key=value" 和 "--key value" 两种格式
		if v, ok := strings.CutPrefix(a, "--project="); ok {
			projectRoot = v
			continue
		}
		if v, ok := strings.CutPrefix(a, "--format="); ok {
			format = v
			continue
		}
		switch a {
		case "--project", "-p":
			if i+1 < len(args) {
				projectRoot = args[i+1]
				i++
			}
		case "--format", "-f":
			if i+1 < len(args) {
				format = args[i+1]
				i++
			}
		}
	}

	ps := project.New(projectRoot)
	if !ps.Exists() {
		fmt.Fprintf(stdout, "[未初始化] 项目目录: %s\n", ps.Root)
		fmt.Fprintf(stdout, "提示: 运行 `novel2all setup` 初始化项目\n")
		return nil
	}

	tracker := memory.NewTracker(ps.TrackingStateFile())
	state, err := tracker.Read()
	if err != nil {
		fmt.Fprintf(stderr, "read tracker: %v\n", err)
		return err
	}
	if state == nil {
		fmt.Fprintf(stdout, "[未初始化] 项目目录: %s\n", ps.Root)
		return nil
	}

	// 统计
	activeFs := 0
	for _, fs := range state.Foreshadowing {
		if fs.Status == "active" {
			activeFs++
		}
	}

	switch format {
	case "json":
		view := map[string]interface{}{
			"project_root":      ps.Root,
			"project_name":      state.ProjectName,
			"genre":             strOrDefault(state.Genre, "(未设置)"),
			"style_anchor":      strOrDefault(state.StyleAnchor, "(未设置)"),
			"total_chapters":    state.TotalChaptersTarget,
			"total_word_count":  state.TotalWordCountTarget,
			"last_chapter":      state.LastUpdatedChapter,
			"character_count":   len(state.Characters),
			"active_foreshadow": activeFs,
			"timeline_count":    len(state.Timeline),
			"summary_count":     len(state.RecentChapterSummaries),
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	default:
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "项目状态 (%s)\n", ps.Root)
		fmt.Fprintf(tw, "------\n")
		fmt.Fprintf(tw, "项目名\t%s\n", state.ProjectName)
		fmt.Fprintf(tw, "题材\t%s\n", strOrDefault(state.Genre, "(未设置)"))
		fmt.Fprintf(tw, "文风锚点\t%s\n", strOrDefault(state.StyleAnchor, "(未设置)"))
		fmt.Fprintf(tw, "目标章节\t%d\n", state.TotalChaptersTarget)
		fmt.Fprintf(tw, "目标字数\t%d\n", state.TotalWordCountTarget)
		fmt.Fprintf(tw, "当前章节\t%d\n", state.LastUpdatedChapter)
		fmt.Fprintf(tw, "角色数\t%d\n", len(state.Characters))
		fmt.Fprintf(tw, "活跃伏笔\t%d\n", activeFs)
		fmt.Fprintf(tw, "时间线条目\t%d\n", len(state.Timeline))
		fmt.Fprintf(tw, "已有摘要\t%d 章\n", len(state.RecentChapterSummaries))
		return tw.Flush()
	}
}

func strOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
