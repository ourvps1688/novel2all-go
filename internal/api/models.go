package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/ourvps1688/novel2all-go/internal/llm"
)

// ModelInfo 描述一个可用模型（与 Python V1.5.x /api/models schema 兼容）
type ModelInfo struct {
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	AnthropicCompat bool     `json:"anthropic_compat"`
	APIBase         string   `json:"api_base,omitempty"`
	APIKeyEnv       string   `json:"api_key_env,omitempty"`
	DefaultForTasks []string `json:"default_for_tasks,omitempty"`
}

// ModelsHandler 提供 /api/models + /api/model/{current,switch}
type ModelsHandler struct {
	mu     sync.RWMutex
	router *llm.Router
	// 当前激活 model（运行时切换，进程级）
	currentModel string
}

// NewModelsHandler 创建
func NewModelsHandler(r *llm.Router) *ModelsHandler {
	// 默认 = minimax-M3（与 V1.5.x 决策一致：WRITING → minimax）
	defaultModel := ""
	if r != nil {
		defaultModel = defaultModelFromRouter(r)
	}
	return &ModelsHandler{router: r, currentModel: defaultModel}
}

func defaultModelFromRouter(r *llm.Router) string {
	// 优先返回 minimax 模型
	for _, p := range []llm.ProviderName{llm.ProviderMinimax, llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderAnthropic} {
		prov := r.ProviderByName(p)
		if prov != nil && prov.Available() {
			return prov.DefaultModel()
		}
	}
	return ""
}

// ServeHTTP 分发路由
//
//	GET  /api/models/         → 列出所有可用模型
//	GET  /api/model/current/  → 当前激活模型
//	POST /api/model/switch/   → 切换模型
func (h *ModelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /api/models/ → path 是 "models"
	// /api/model/current/ → path 是 "model/current"
	// /api/model/switch/  → path 是 "model/switch"
	// 我们统一处理：
	//   - 如果 path 是 "models" → listModels
	//   - 如果 path 以 "model/" 开头 → 去掉 "model/" 前缀再 switch
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	path = strings.Trim(path, "/")
	switch {
	case path == "models":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listModels(w, r)
	case strings.HasPrefix(path, "model/"):
		sub := strings.TrimPrefix(path, "model/")
		switch sub {
		case "current":
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.getCurrent(w, r)
		case "switch":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.switchModel(w, r)
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

// listModels 列出所有已配置 provider 的可用模型
func (h *ModelsHandler) listModels(w http.ResponseWriter, _ *http.Request) {
	if h.router == nil {
		http.Error(w, `{"error":"router not configured"}`, http.StatusServiceUnavailable)
		return
	}
	models := h.collectModels()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"models": models,
		"count":  len(models),
	})
}

// getCurrent 返回当前激活模型
func (h *ModelsHandler) getCurrent(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	current := h.currentModel
	router := h.router
	h.mu.RUnlock()
	if current == "" && router != nil {
		current = defaultModelFromRouter(router)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"current_model": current,
	})
}

// switchModel 切换默认模型
func (h *ModelsHandler) switchModel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid form"}`, http.StatusBadRequest)
		return
	}
	newModel := r.FormValue("model")
	if newModel == "" {
		http.Error(w, `{"error":"missing 'model' form field"}`, http.StatusBadRequest)
		return
	}
	models := h.collectModels()
	known := false
	for _, m := range models {
		if m.Name == newModel {
			known = true
			break
		}
	}
	if !known {
		http.Error(w, fmt.Sprintf(`{"error":"unknown model: %s"}`, newModel), http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	old := h.currentModel
	h.currentModel = newModel
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"old_model": old,
		"new_model": newModel,
	})
}

// CurrentModel 暴露当前激活模型（给其他包用）
func (h *ModelsHandler) CurrentModel() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.currentModel != "" {
		return h.currentModel
	}
	if h.router != nil {
		return defaultModelFromRouter(h.router)
	}
	return ""
}

// collectModels 从 router 收集所有可用的 ModelInfo
func (h *ModelsHandler) collectModels() []ModelInfo {
	var out []ModelInfo
	if h.router == nil {
		return out
	}
	for _, p := range []llm.ProviderName{llm.ProviderDashScope, llm.ProviderDeepSeek, llm.ProviderMinimax, llm.ProviderAnthropic} {
		prov := h.router.ProviderByName(p)
		if prov == nil || !prov.Available() {
			continue
		}
		info := ModelInfo{
			Name:            prov.DefaultModel(),
			Provider:        string(p),
			AnthropicCompat: p == llm.ProviderMinimax || p == llm.ProviderAnthropic,
			APIBase:         prov.APIBase(),
			APIKeyEnv:       apiKeyEnvFor(p),
		}
		if p == llm.ProviderMinimax {
			info.DefaultForTasks = []string{"WRITING"}
		} else {
			info.DefaultForTasks = []string{"CONSISTENCY", "EXTRACTION", "SUMMARIZATION", "COVER"}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func apiKeyEnvFor(p llm.ProviderName) string {
	switch p {
	case llm.ProviderDashScope:
		return "DASHSCOPE_API_KEY"
	case llm.ProviderDeepSeek:
		return "DEEPSEEK_API_KEY"
	case llm.ProviderMinimax:
		return "MINIMAX_API_KEY"
	case llm.ProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	}
	return ""
}
