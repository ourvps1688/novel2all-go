// roles 子命令: 列出 5 个核心角色.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/ourvps1688/novel2all-go/internal/roles"
)

// runRoles 执行 roles 子命令.
//
// flags:
//   - --format=table (默认) / json
func runRoles(stdout io.Writer, args []string) error {
	fs := flag.NewFlagSet("roles", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "table", "output format: table | json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("roles: %w", err)
	}

	all := roles.All()

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	case "table", "":
		return printRolesTable(stdout, all)
	default:
		return fmt.Errorf("unknown format %q (use table or json)", *format)
	}
}

// printRolesTable 输出表格形式到 w.
func printRolesTable(w io.Writer, list []roles.RoleMeta) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tAGENT\tSTAGE")
	fmt.Fprintln(tw, "--\t----\t-----\t-----")
	for _, r := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Name, r.Agent, r.Stage)
	}
	return tw.Flush()
}
