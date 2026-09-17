// skills 子命令: 列出所有 SKILL.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/ourvps1688/novel2all-go/internal/skills"
)

// runSkills 执行 skills 子命令.
//
// flags:
//   - --format=table (默认) / json
func runSkills(stdout io.Writer, args []string) error {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 错误输出由 run() 统一处理
	format := fs.String("format", "table", "output format: table | json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("skills: %w", err)
	}

	loader, err := skills.NewLoader()
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	list := loader.ListDetailed()

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	case "table", "":
		return printSkillsTable(stdout, list)
	default:
		return fmt.Errorf("unknown format %q (use table or json)", *format)
	}
}

// printSkillsTable 输出表格形式到 w.
func printSkillsTable(w io.Writer, list []skills.Skill) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDESCRIPTION")
	fmt.Fprintln(tw, "----\t-----------")
	for _, s := range list {
		desc := s.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\n", s.Name, desc)
	}
	return tw.Flush()
}
