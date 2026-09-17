// memory.go — Sprint 26 memory 子系统 CLI.
//
// 子命令:
//
//	memory stats    显示 retriever 模式 + 事件总数
//	memory query    检索 top-K 事件 (交互式查询)
//	memory add-event  入库单条事件 (调试用)
//	memory rebuild   清空 retriever 索引 (危险, 默认 dry-run)
//
// 所有子命令操作项目根目录的 .chroma 目录 (与 internal/memory/manager 一致).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/ourvps1688/novel2all-go/internal/memory"
)

// runMemory 路由 memory 子命令.
func runMemory(stdout, stderr io.Writer, args []string) error {
	if len(args) == 0 {
		printMemoryHelp(stdout)
		return nil
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "stats":
		return memoryStats(stdout, stderr, rest)
	case "query":
		return memoryQuery(stdout, stderr, rest)
	case "add-event":
		return memoryAddEvent(stdout, stderr, rest)
	case "rebuild":
		return memoryRebuild(stdout, stderr, rest)
	case "-h", "--help", "help":
		printMemoryHelp(stdout)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown memory subcommand: %q\n\n", sub)
		printMemoryHelp(stderr)
		return fmt.Errorf("unknown memory subcommand: %q", sub)
	}
}

// printMemoryHelp 输出 memory 子命令帮助.
func printMemoryHelp(w io.Writer) {
	fmt.Fprintf(w, `novel2all memory — 内存子系统 CLI (Sprint 26)

用法:
  novel2all memory <subcommand> [flags]

子命令:
  stats                    显示 retriever 模式 + 事件总数 + mode
  query --text <q>         检索 top-K 事件
  add-event                入库单条事件 (调试用)
  rebuild                  清空 retriever 索引 (默认 dry-run, --force 确认)

通用 flag:
  --project=<path>         项目根目录 (默认当前目录)
  --mode=chroma|tfidf|keyword  强制 retriever 模式 (默认自动)
  -h, --help               显示帮助

示例:
  novel2all memory stats --project=./projects/my-novel
  novel2all memory query --text="血脉觉醒" --top-k=5
  novel2all memory add-event --chapter=1 --type=general --text="主角踏入修炼之路"
  novel2all memory rebuild --force
`)
}

// memoryStats 实现 stats 子命令.
func memoryStats(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("memory stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectRoot := fs.String("project", ".", "project root directory")
	mode := fs.String("mode", "", "force mode (chroma/tfidf/keyword)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("memory stats: %w", err)
	}

	retriever, err := newRetriever(*projectRoot, *mode)
	if err != nil {
		return fmt.Errorf("new retriever: %w", err)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "MODE\tEVENTS\n")
	fmt.Fprintf(tw, "----\t------\n")
	fmt.Fprintf(tw, "%s\t%d\n", retriever.Mode(), retriever.Count())
	_ = tw.Flush()
	return nil
}

// memoryQuery 实现 query 子命令.
func memoryQuery(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("memory query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectRoot := fs.String("project", ".", "project root directory")
	mode := fs.String("mode", "", "force mode (chroma/tfidf/keyword)")
	text := fs.String("text", "", "query text (required)")
	topK := fs.Int("top-k", 8, "top-K events to retrieve")
	chapterLo := fs.Int("ch-lo", 0, "chapter range lower bound (0=any)")
	chapterHi := fs.Int("ch-hi", 0, "chapter range upper bound (0=any)")
	eventType := fs.String("type", "", "event type filter (character_change/foreshadowing/timeline/general)")
	format := fs.String("format", "table", "output format (table/json)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("memory query: %w", err)
	}
	if *text == "" {
		return fmt.Errorf("memory query: --text is required")
	}

	retriever, err := newRetriever(*projectRoot, *mode)
	if err != nil {
		return fmt.Errorf("new retriever: %w", err)
	}

	var rng *[2]int
	if *chapterLo > 0 && *chapterHi > 0 {
		rng = &[2]int{*chapterLo, *chapterHi}
	}
	results := retriever.Query(*text, *topK, rng, *eventType)

	switch *format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	default:
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "RELEVANCE\tCHAPTER\tTYPE\tCONTENT\n")
		fmt.Fprintf(tw, "---------\t-------\t----\t-------\n")
		for _, item := range results {
			// extract chapter from source "retriever#chN"
			var ch int
			fmt.Sscanf(item.Source, "retriever#ch%d", &ch)
			content := item.Content
			if len(content) > 80 {
				content = content[:77] + "..."
			}
			layer := string(item.Layer)
			fmt.Fprintf(tw, "%.3f\t%d\t%s\t%s\n", item.Relevance, ch, layer, content)
		}
		return tw.Flush()
	}
}

// memoryAddEvent 实现 add-event 子命令 (调试用).
func memoryAddEvent(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("memory add-event", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectRoot := fs.String("project", ".", "project root directory")
	mode := fs.String("mode", "", "force mode")
	chapter := fs.Int("chapter", 0, "chapter number (required)")
	eventType := fs.String("type", "general", "event type")
	text := fs.String("text", "", "event text (required)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("memory add-event: %w", err)
	}
	if *chapter == 0 {
		return fmt.Errorf("memory add-event: --chapter is required")
	}
	if *text == "" {
		return fmt.Errorf("memory add-event: --text is required")
	}

	retriever, err := newRetriever(*projectRoot, *mode)
	if err != nil {
		return fmt.Errorf("new retriever: %w", err)
	}

	id := retriever.AddEvent(*chapter, *eventType, *text, nil)
	fmt.Fprintf(stdout, "added event: %s\n", id)
	return nil
}

// memoryRebuild 实现 rebuild 子命令 (默认 dry-run).
func memoryRebuild(stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("memory rebuild", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectRoot := fs.String("project", ".", "project root directory")
	mode := fs.String("mode", "", "force mode")
	force := fs.Bool("force", false, "actually wipe (DESTRUCTIVE)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("memory rebuild: %w", err)
	}

	retriever, err := newRetriever(*projectRoot, *mode)
	if err != nil {
		return fmt.Errorf("new retriever: %w", err)
	}
	count := retriever.Count()

	if !*force {
		fmt.Fprintf(stdout, "[dry-run] would clear %d events (mode=%s)\n", count, retriever.Mode())
		fmt.Fprintf(stdout, "pass --force to actually wipe\n")
		return nil
	}

	if err := retriever.Clear(); err != nil {
		return err
	}
	retriever.WipeDisk()
	fmt.Fprintf(stdout, "cleared %d events\n", count)
	return nil
}

// newRetriever 构造 retriever (helper).
func newRetriever(projectRoot, forceMode string) (*memory.MemoryRetriever, error) {
	var mode *memory.RetrieverMode
	if forceMode != "" {
		m := memory.RetrieverMode(forceMode)
		mode = &m
	}
	chromaDir := filepath.Join(projectRoot, ".chroma")
	return memory.NewRetriever(chromaDir, mode)
}
