// Package memory 提供 novel2all-go 长记忆系统.
package memory

// summarizer.go 实现 L3 章节摘要器 (Sprint 23).
//
// 设计 (对齐 Python V0.21 ChapterSummarizer):
//   - 3 个压缩档位: TIER1 (ch 1-10 全量) / TIER2 (11-30 每 5 章滚动) / TIER3 (31+ 每 10 章滚动)
//   - 单章 summarize 用 extractive 摘要 (前 3 句 + 末 1 句 + 关键句), 不依赖 LLM (可沙箱跑)
//   - token 计数: 中文约 1.5 字符/token, 英文约 4 字符/token (粗估 len(text)/3)
//   - apply_to_state 把 chapter 摘要按档位合并
//
// 参考 Python V0.21 core/memory/summarizer.py.
import (
	"fmt"
	"strings"
)

// 三个压缩档位的触发边界 (章节号).
const (
	Tier1Threshold = 11 // 1-10 全量
	Tier2Threshold = 31 // 11-30 每 5 章滚动; 31+ 每 10 章滚动
	Tier2Bucket    = 5  // 11-30 的桶大小
	Tier3Bucket    = 10 // 31+ 的桶大小
)

// TierOf 返回章节所在的摘要档位 (1/2/3).
func TierOf(chapter int) int {
	if chapter < Tier1Threshold {
		return 1
	}
	if chapter < Tier2Threshold {
		return 2
	}
	return 3
}

// BucketOf 返回章节所属的 (桶起始, 桶结束) 区间.
//
// TIER1: 每章独立 → (chapter, chapter)
// TIER2: [11, 15], [16, 20], ..., [26, 30]
// TIER3: [31, 40], [41, 50], ...
func BucketOf(chapter int) (int, int) {
	t := TierOf(chapter)
	if t == 1 {
		return chapter, chapter
	}
	bucket := Tier2Bucket
	if t == 3 {
		bucket = Tier3Bucket
	}
	// tier 2: 起始 11, 桶大小 5
	// tier 3: 起始 31, 桶大小 10
	start := Tier1Threshold
	if t == 3 {
		start = Tier2Threshold
	}
	offset := chapter - start
	bucketIdx := offset / bucket
	bucketStart := start + bucketIdx*bucket
	bucketEnd := bucketStart + bucket - 1
	return bucketStart, bucketEnd
}

// ChapterSummarizer 章节摘要器.
type ChapterSummarizer struct{}

// NewChapterSummarizer 创建.
func NewChapterSummarizer() *ChapterSummarizer {
	return &ChapterSummarizer{}
}

// SummarizeChapter 单章 extractive 摘要 (不依赖 LLM).
//
// 规则:
//   - 取前 3 句 + 末 1 句
//   - 如总长 < 200 字, 全量返回
//   - 否则按比例压缩到 ~200 字
func (s *ChapterSummarizer) SummarizeChapter(content string) string {
	if len(content) <= 200 {
		return content
	}

	// 按中文/英文句号分句
	sentences := splitSentences(content)
	if len(sentences) <= 4 {
		// 句子少, 全量
		return content
	}

	// 前 3 + 末 1
	var picked []string
	for i, s := range sentences {
		if i < 3 || i == len(sentences)-1 {
			picked = append(picked, s)
		}
	}
	result := strings.Join(picked, "")
	// 仍然太长 → 截前 200 字
	if len(result) > 300 {
		result = result[:300] + "..."
	}
	return result
}

// splitSentences 简单分句 (支持中英文 . ! ?。！？).
func splitSentences(text string) []string {
	var sentences []string
	current := strings.Builder{}
	for _, ch := range text {
		current.WriteRune(ch)
		if ch == '.' || ch == '!' || ch == '?' || ch == '。' || ch == '！' || ch == '？' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if rest := strings.TrimSpace(current.String()); rest != "" {
		sentences = append(sentences, rest)
	}
	return sentences
}

// CountTokens 估算 token 数 (中文 1.5 字符/token, 英文 4 字符/token).
func (s *ChapterSummarizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	// 简化: len/3 (Python len/3 兜底)
	return maxInt(1, len(text)/3)
}

// ApplyToState 把 chapter 摘要按档位合并到 state.
//
// 1. 生成 chapter 摘要 (extractive, 不调 LLM)
// 2. 按 BucketOf 决定写入 recent_chapter_summaries 还是合并到 bucket
// 3. bucket 摘要合并时用 "Ch X-Y: " 前缀
func (s *ChapterSummarizer) ApplyToState(state *TrackingState, chapter int, content string) *TrackingState {
	if state.RecentChapterSummaries == nil {
		state.RecentChapterSummaries = make(map[int]string)
	}

	summary := s.SummarizeChapter(content)
	bucketStart, bucketEnd := BucketOf(chapter)

	if bucketStart == bucketEnd {
		// TIER1: 单独 chapter key
		state.RecentChapterSummaries[chapter] = summary
	} else {
		// TIER2/3: 合并到 bucket key (= bucketStart)
		bucketKey := bucketStart
		existing, ok := state.RecentChapterSummaries[bucketKey]
		prefix := fmt.Sprintf("Ch %d-%d: ", bucketStart, bucketEnd)
		if !ok {
			state.RecentChapterSummaries[bucketKey] = prefix + summary
		} else {
			// 追加到 prefix 后
			state.RecentChapterSummaries[bucketKey] = existing + " | " + summary
		}
	}

	state.LastUpdatedChapter = maxInt(state.LastUpdatedChapter, chapter)
	return state
}

// maxInt 内置 max helper (Go 1.21+ built-in).
// CompressRange 合并 [startCh, endCh] 范围的章节摘要为单一字符串 (Sprint 30 helper).
//
// 用于旧摘要压缩或跨章节上下文生成. 调 apply_to_state 累积 state, 然后取 RecentChapterSummaries 拼接.
//
// V0.27 简化: 直接拼接, 不调用 LLM.
func (s *ChapterSummarizer) CompressRange(state *TrackingState, startCh, endCh int) string {
	if state == nil || startCh > endCh {
		return ""
	}
	var parts []string
	for ch := startCh; ch <= endCh; ch++ {
		if summary, ok := state.RecentChapterSummaries[ch]; ok && summary != "" {
			parts = append(parts, fmt.Sprintf("第%d章: %s", ch, summary))
		}
	}
	return strings.Join(parts, "\n\n")
}

// MergeBuckets 合并 TIER1/2/3 三档摘要 (Sprint 30 helper).
//
// buckets: [TIER1 summaries, TIER2 summaries, TIER3 summaries].
// 返回拼接字符串 (用 \n\n 分隔 tier, 内部用 | 分隔 summaries).
func (s *ChapterSummarizer) MergeBuckets(buckets [3][]string) string {
	headers := []string{"[TIER1: 近期]", "[TIER2: 中期]", "[TIER3: 远期]"}
	var parts []string
	for i, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s\n%s", headers[i], strings.Join(bucket, " | ")))
	}
	return strings.Join(parts, "\n\n")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
