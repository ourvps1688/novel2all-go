// Package memory — retriever 子模块 (Sprint 26)
//
// MemoryRetriever 实现与 Python V0.21 retriever.py 同等接口：
//   - 3 模式自动降级: chromadb (向量检索) → TF-IDF → keyword (朴素匹配)
//   - add_event / add_events / query (chapter_range + event_type filter)
//   - delete_chapter / count / clear / wipe_disk 维护 API
//   - force_mode 测试钩子 (chromadb / tfidf / keyword / nil=自动)
//
// 设计差异 (vs Python):
//   - 向量库用内部 internal/chroma (纯 Go, 无 cgo) 替代 chromadb
//   - embedding 用 chroma.Client.Embed (SHA256 token hash → 128 维) 替代 sentence-transformers
//   - chromem-go 需要 CGO=P1 阶段排除, 用 hash embedding 够用
//
// 失败原则: 检索层失败永远不阻断写作主流程, 自动降级.
package memory

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ourvps1688/novel2all-go/internal/chroma"
)

// RetrieverMode 当前检索模式.
type RetrieverMode string

const (
	// ModeChromaDB 向量检索 (chroma + embedding) — 最准确.
	ModeChromaDB RetrieverMode = "chroma"
	// ModeTFIDF 关键词 TF-IDF 余弦相似度 — 中等准确, 纯内存.
	ModeTFIDF RetrieverMode = "tfidf"
	// ModeKeyword 朴素关键词匹配 — 兜底, 永远可用.
	ModeKeyword RetrieverMode = "keyword"
)

// RetrievedEvent 单条检索结果 (内部存储用, 导出供测试).
type RetrievedEvent struct {
	Chapter   int                    `json:"chapter"`
	EventType string                 `json:"event_type"`
	Text      string                 `json:"text"`
	Score     float64                `json:"score"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MemoryRetriever 事件检索器.
type MemoryRetriever struct {
	mu sync.RWMutex

	chromaDir string
	mode      RetrieverMode
	chroma    *chroma.Client
	corpus    []RetrievedEvent // TF-IDF / keyword 模式用
	docs      []string         // 同步 corpus 文本 (TF-IDF 计算用)

	// chroma 模式 metadata 索引 (id → event) 便于 delete_chapter filter.
	chromaEvents map[string]RetrievedEvent
}

// NewRetriever 构造 retriever (force_mode=nil 走 chroma 自动降级).
//
// chromaDir: 向量库持久化目录 (如 projectRoot/.chroma).
//
// 持久化: 每次构造从 {chromaDir}/events.json 加载 events 到内存索引.
// 写入: AddEvent 同步追加到 events.json (atomic write via tmp+rename).
func NewRetriever(chromaDir string, forceMode *RetrieverMode) (*MemoryRetriever, error) {
	r := &MemoryRetriever{
		chromaDir:    chromaDir,
		corpus:       nil,
		chromaEvents: make(map[string]RetrievedEvent),
	}
	r.initMode(forceMode)
	// 加载持久化数据
	if err := r.loadFromDisk(); err != nil {
		// 加载失败不阻断 (V0 简化: 视为空)
		_ = err
	}
	return r, nil
}

// eventsFile 持久化文件路径.
func (r *MemoryRetriever) eventsFile() string {
	if r.chromaDir == "" {
		return ""
	}
	return r.chromaDir + string(filepath.Separator) + "events.json"
}

// loadFromDisk 加载持久化 events.
func (r *MemoryRetriever) loadFromDisk() error {
	fp := r.eventsFile()
	if fp == "" {
		return nil
	}
	data, err := readFileOS(fp)
	if err != nil {
		return err
	}
	var events []RetrievedEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range events {
		r.corpus = append(r.corpus, e)
		r.docs = append(r.docs, e.Text)
		if r.mode == ModeChromaDB {
			id := generateEventID(e.Chapter, e.EventType, e.Text)
			r.chromaEvents[id] = e
			// 重新索引 chroma (供 query 用)
			meta := map[string]string{
				"chapter":    fmt.Sprintf("%d", e.Chapter),
				"event_type": e.EventType,
			}
			for k, v := range e.Metadata {
				meta[k] = fmt.Sprintf("%v", v)
			}
			if r.chroma != nil {
				r.chroma.Upsert([]chroma.Document{{ID: id, Text: e.Text, Metadata: meta}})
			}
		}
	}
	return nil
}

// saveToDisk 持久化 events.
func (r *MemoryRetriever) saveToDisk() error {
	fp := r.eventsFile()
	if fp == "" {
		return nil
	}
	if err := ensureDir(r.chromaDir); err != nil {
		return err
	}
	// 用 read 锁拿 corpus 副本 (避免长期持有写锁)
	r.mu.RLock()
	snapshot := make([]RetrievedEvent, len(r.corpus))
	copy(snapshot, r.corpus)
	r.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	// atomic write via tmp + rename
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, fp); err != nil {
		// fallback: 直接写 (best-effort)
		return os.WriteFile(fp, data, 0o644)
	}
	return nil
}

// Mode 返回当前模式 (chromadb / tfidf / keyword).
func (r *MemoryRetriever) Mode() RetrieverMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mode
}

// initMode 按 force_mode 或自动降级初始化.
func (r *MemoryRetriever) initMode(force *RetrieverMode) {
	if force != nil {
		switch *force {
		case ModeChromaDB:
			if r.tryInitChroma() {
				r.mode = ModeChromaDB
				return
			}
			r.mode = ModeTFIDF
		case ModeTFIDF:
			r.mode = ModeTFIDF
		case ModeKeyword:
			r.mode = ModeKeyword
		default:
			r.mode = ModeTFIDF
		}
		return
	}
	// 自动降级: chroma → tfidf → keyword
	if r.tryInitChroma() {
		r.mode = ModeChromaDB
		return
	}
	r.mode = ModeTFIDF
}

// tryInitChroma 尝试初始化向量库.
func (r *MemoryRetriever) tryInitChroma() bool {
	defer func() {
		// 防止 panic (e.g. 磁盘满 / Windows 权限) 阻断降级
		if rec := recover(); rec != nil {
			r.chroma = nil
		}
	}()

	if r.chromaDir == "" {
		return false
	}
	// 确保目录存在 (chroma 内部会用)
	if err := ensureDir(filepath.Dir(r.chromaDir)); err != nil {
		return false
	}
	r.chroma = chroma.NewClient(128)
	return true
}

// ensureDir mkdir -p (test override via mkdirOS).
var ensureDir = func(dir string) error {
	return mkdirOS(dir)
}

// AddEvent 入库一条事件, 返回事件 ID.
//
// eid: 自定义 ID (为空时按 chapter/event_type/text 自动生成).
func (r *MemoryRetriever) AddEvent(chapter int, eventType, text string, metadata map[string]interface{}) string {
	r.mu.Lock()

	id := generateEventID(chapter, eventType, text)
	meta := map[string]interface{}{"chapter": chapter, "event_type": eventType}
	for k, v := range metadata {
		meta[k] = v
	}
	event := RetrievedEvent{
		Chapter:   chapter,
		EventType: eventType,
		Text:      text,
		Score:     1.0,
		Metadata:  meta,
	}

	switch r.mode {
	case ModeChromaDB:
		// chroma.Upsert 接受 []Document, metadata 转 string map
		strMeta := make(map[string]string, len(meta))
		for k, v := range meta {
			strMeta[k] = fmt.Sprintf("%v", v)
		}
		tryUpsert(r, id, text, strMeta)
		r.chromaEvents[id] = event
	default:
		// TF-IDF / keyword 模式: 内存
		r.corpus = append(r.corpus, event)
		r.docs = append(r.docs, text)
	}
	r.mu.Unlock()

	// 持久化 (锁外做, 减少锁持有)
	_ = r.saveToDisk()
	return id
}

// AddEvents 批量入库, 返回所有事件 ID.
func (r *MemoryRetriever) AddEvents(events []EventInput) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, r.AddEvent(e.Chapter, e.EventType, e.Text, e.Metadata))
	}
	return ids
}

// EventInput 批量入库单条 (避免 map[string]interface{} 在 API 层暴露).
type EventInput struct {
	Chapter   int                    `json:"chapter"`
	EventType string                 `json:"event_type"`
	Text      string                 `json:"text"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Query 检索 top_k 相关事件 (chapter_range/event_type 可选 filter).
//
// 返回 MemoryItem 列表 (layer=EVENT), Python v0.21 接口对等.
func (r *MemoryRetriever) Query(text string, topK int, chapterRange *[2]int, eventType string) []MemoryItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if topK <= 0 {
		topK = 8
	}

	switch r.mode {
	case ModeChromaDB:
		items := r.queryChroma(text, topK, chapterRange, eventType)
		if items != nil {
			return items
		}
		// 失败降级到 TF-IDF
	}
	if r.mode == ModeChromaDB || r.mode == ModeTFIDF {
		return r.queryTFIDF(text, topK, chapterRange, eventType)
	}
	return r.queryKeyword(text, topK, chapterRange, eventType)
}

// queryChroma 用 chroma + post-filter (因为 chroma.Query 不支持 metadata filter).
func (r *MemoryRetriever) queryChroma(text string, topK int, chapterRange *[2]int, eventType string) []MemoryItem {
	defer func() {
		if rec := recover(); rec != nil {
			// 检索失败: 返回 nil 触发降级
		}
	}()

	if r.chroma == nil {
		return nil
	}
	// 多取一些再 filter (filter 后可能少于 topK)
	expanded := topK * 4
	if expanded < 32 {
		expanded = 32
	}
	matches := r.chroma.Query(text, expanded)
	if len(matches) == 0 {
		return nil
	}

	items := make([]MemoryItem, 0, len(matches))
	for _, m := range matches {
		evt, ok := r.chromaEvents[m.ID]
		if !ok {
			continue
		}
		if chapterRange != nil {
			if evt.Chapter < chapterRange[0] || evt.Chapter > chapterRange[1] {
				continue
			}
		}
		if eventType != "" && evt.EventType != eventType {
			continue
		}
		if len(items) >= topK {
			break
		}
		items = append(items, MemoryItem{
			Content:    evt.Text,
			Source:     fmt.Sprintf("retriever#ch%d", evt.Chapter),
			Layer:      LayerEvent,
			Relevance:  m.Score,
			TokenCount: max1(len(evt.Text) / 4),
		})
	}
	return items
}

// queryTFIDF 纯 Go TF-IDF 余弦相似度 (Python _tfidf_scores 移植).
func (r *MemoryRetriever) queryTFIDF(text string, topK int, chapterRange *[2]int, eventType string) []MemoryItem {
	candidates := r.filterCorpus(chapterRange, eventType)
	if len(candidates) == 0 {
		return nil
	}
	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = c.Text
	}
	scores := tfidfScores(text, docs)
	type sc struct {
		evt   RetrievedEvent
		score float64
	}
	ranked := make([]sc, 0, len(candidates))
	for i, c := range candidates {
		if scores[i] > 0 {
			ranked = append(ranked, sc{c, scores[i]})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	items := make([]MemoryItem, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, MemoryItem{
			Content:    r.evt.Text,
			Source:     fmt.Sprintf("retriever#ch%d", r.evt.Chapter),
			Layer:      LayerEvent,
			Relevance:  r.score,
			TokenCount: max1(len(r.evt.Text) / 4),
		})
	}
	return items
}

// queryKeyword 朴素 bigram + unigram Jaccard.
func (r *MemoryRetriever) queryKeyword(text string, topK int, chapterRange *[2]int, eventType string) []MemoryItem {
	candidates := r.filterCorpus(chapterRange, eventType)
	if len(candidates) == 0 {
		return nil
	}
	qTokens := tokenizeForSearch(text)
	if len(qTokens) == 0 {
		return nil
	}
	type sc struct {
		evt   RetrievedEvent
		score float64
	}
	ranked := make([]sc, 0, len(candidates))
	for _, c := range candidates {
		dTokens := tokenizeForSearch(c.Text)
		if len(dTokens) == 0 {
			continue
		}
		qSet := make(map[string]struct{}, len(qTokens))
		for _, t := range qTokens {
			qSet[t] = struct{}{}
		}
		dSet := make(map[string]struct{}, len(dTokens))
		for _, t := range dTokens {
			dSet[t] = struct{}{}
		}
		var inter, union int
		for t := range qSet {
			if _, ok := dSet[t]; ok {
				inter++
			}
		}
		union = len(qSet) + len(dSet) - inter
		if union == 0 {
			continue
		}
		overlap := float64(inter) / float64(union)
		if overlap > 0 {
			ranked = append(ranked, sc{c, overlap})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	items := make([]MemoryItem, 0, len(ranked))
	for _, r := range ranked {
		items = append(items, MemoryItem{
			Content:    r.evt.Text,
			Source:     fmt.Sprintf("retriever#ch%d", r.evt.Chapter),
			Layer:      LayerEvent,
			Relevance:  r.score,
			TokenCount: max1(len(r.evt.Text) / 4),
		})
	}
	return items
}

// filterCorpus 内存模式 metadata filter.
func (r *MemoryRetriever) filterCorpus(chapterRange *[2]int, eventType string) []RetrievedEvent {
	out := make([]RetrievedEvent, 0, len(r.corpus))
	for _, e := range r.corpus {
		if chapterRange != nil {
			if e.Chapter < chapterRange[0] || e.Chapter > chapterRange[1] {
				continue
			}
		}
		if eventType != "" && e.EventType != eventType {
			continue
		}
		out = append(out, e)
	}
	return out
}

// DeleteChapter 删除某 chapter 全部事件, 返回删除条数.
func (r *MemoryRetriever) DeleteChapter(chapter int) int {
	r.mu.Lock()

	count := 0
	switch r.mode {
	case ModeChromaDB:
		for id, evt := range r.chromaEvents {
			if evt.Chapter == chapter {
				_ = r.chroma.Delete(id)
				delete(r.chromaEvents, id)
				count++
			}
		}
	default:
		newCorpus := r.corpus[:0]
		for _, e := range r.corpus {
			if e.Chapter == chapter {
				count++
				continue
			}
			newCorpus = append(newCorpus, e)
		}
		r.corpus = newCorpus
		// 同步 docs
		newDocs := make([]string, 0, len(r.corpus))
		for _, e := range r.corpus {
			newDocs = append(newDocs, e.Text)
		}
		r.docs = newDocs
	}
	r.mu.Unlock()

	// 持久化
	_ = r.saveToDisk()
	return count
}

// Count 当前事件总数.
func (r *MemoryRetriever) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.mode == ModeChromaDB {
		return len(r.chromaEvents)
	}
	return len(r.corpus)
}

// Clear 清空内存事件 (chromadb 模式删除磁盘 index; 内存模式清空 corpus).
func (r *MemoryRetriever) Clear() error {
	r.mu.Lock()
	if r.mode == ModeChromaDB && r.chroma != nil {
		r.chroma.Clear()
	}
	r.corpus = nil
	r.docs = nil
	r.chromaEvents = make(map[string]RetrievedEvent)
	r.mu.Unlock()
	// 持久化 (空 array)
	return r.saveToDisk()
}

// WipeDisk 物理删除 chromadb 目录.
func (r *MemoryRetriever) WipeDisk() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chromaDir == "" {
		return
	}
	// 用通用 helper (test 可覆盖), 但 rm 不能放 file_helpers 因为容易误删
	// 直接走 os.RemoveAll (通过 mkdirOS 同包函数无法做)
	_ = osRemoveAll(r.chromaDir)
}

// === 辅助函数 ===

// tokenRE 中英文混合分词 (Python _TOKEN_RE 移植).
var tokenRE = regexp.MustCompile(`[\w\p{Han}]+`)

// tokenizeForSearch bigram + unigram 混合 (Python _tokenize 移植).
func tokenizeForSearch(text string) []string {
	out := []string{}
	for _, piece := range tokenRE.FindAllString(strings.ToLower(text), -1) {
		isChinese := true
		for _, r := range piece {
			if r < 0x4e00 || r > 0x9fff {
				isChinese = false
				break
			}
		}
		if isChinese {
			// bigram + unigram
			for _, c := range piece {
				out = append(out, string(c))
			}
			for i := 0; i < len(piece)-3; i += 2 { // 3-byte UTF-8 safe step
				// simple bigram on runes
			}
			// proper rune-level bigram
			runes := []rune(piece)
			for i := 0; i < len(runes)-1; i++ {
				out = append(out, string(runes[i:i+2]))
			}
		} else {
			out = append(out, piece)
		}
	}
	return out
}

// tfidfScores TF-IDF 余弦相似度 (Python _tfidf_scores 移植).
func tfidfScores(query string, docs []string) []float64 {
	if len(docs) == 0 {
		return nil
	}
	qTokens := tokenizeForSearch(query)
	if len(qTokens) == 0 {
		scores := make([]float64, len(docs))
		return scores
	}

	docTokensList := make([][]string, len(docs))
	df := make(map[string]int)
	for i, d := range docs {
		toks := tokenizeForSearch(d)
		docTokensList[i] = toks
		seen := make(map[string]struct{})
		for _, t := range toks {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}
	nDocs := len(docs)

	vector := func(tokens []string) map[string]float64 {
		if len(tokens) == 0 {
			return nil
		}
		tf := make(map[string]int)
		for _, t := range tokens {
			tf[t]++
		}
		vec := make(map[string]float64, len(tf))
		for term, count := range tf {
			idf := math.Log(float64(nDocs+1)/float64(df[term]+1)) + 1
			vec[term] = float64(count) / float64(len(tokens)) * idf
		}
		return vec
	}

	qVec := vector(qTokens)
	if len(qVec) == 0 {
		scores := make([]float64, len(docs))
		return scores
	}

	qNorm := 0.0
	for _, v := range qVec {
		qNorm += v * v
	}
	qNorm = math.Sqrt(qNorm)

	scores := make([]float64, len(docs))
	for i, dTokens := range docTokensList {
		dVec := vector(dTokens)
		if len(dVec) == 0 {
			continue
		}
		dNorm := 0.0
		for _, v := range dVec {
			dNorm += v * v
		}
		dNorm = math.Sqrt(dNorm)

		dot := 0.0
		for t, qv := range qVec {
			if dv, ok := dVec[t]; ok {
				dot += qv * dv
			}
		}
		denom := qNorm * dNorm
		if denom > 0 {
			scores[i] = dot / denom
		}
	}
	return scores
}

// generateEventID 与 Python md5(text)[:8] 一致.
func generateEventID(chapter int, eventType, text string) string {
	h := md5.Sum([]byte(fmt.Sprintf("ch%d-%s-%s", chapter, eventType, text)))
	return fmt.Sprintf("ch%d-%s-%s", chapter, eventType, hex.EncodeToString(h[:])[:8])
}

// max1 max(1, n) (避免 import "max" 内置).
func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// tryUpsert 包装 chroma.Upsert, panic-safe (失败降级到 TF-IDF).
func tryUpsert(r *MemoryRetriever, id, text string, meta map[string]string) {
	defer func() {
		if rec := recover(); rec != nil {
			r.chroma = nil
			r.mode = ModeTFIDF
			r.corpus = append(r.corpus, RetrievedEvent{
				Chapter:   parseChapterFromID(id),
				EventType: meta["event_type"],
				Text:      text,
				Score:     1.0,
				Metadata:  nil,
			})
			r.docs = append(r.docs, text)
		}
	}()
	r.chroma.Upsert([]chroma.Document{{
		ID:       id,
		Text:     text,
		Metadata: meta,
	}})
}

// parseChapterFromID 从 "chN-type-hash" 解析 chapter (降级 fallback 用).
func parseChapterFromID(id string) int {
	var n int
	fmt.Sscanf(id, "ch%d-", &n)
	if n < 0 {
		return 0
	}
	return n
}

// osRemoveAll 生产 = os.RemoveAll (test 可覆盖).
var osRemoveAll = func(p string) error {
	return os.RemoveAll(p)
}
