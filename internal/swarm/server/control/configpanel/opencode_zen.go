// Zen 免费模型内存缓存。
// 对应 Python 代码：jiuwenswarm/server/runtime/opencode_zen.py（627 行）。
// 免费模型开机拉取一次、仅驻内存、不写 config.yaml；缓存为空 = 不提供免费模型。
package configpanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 常量 ────────────────────────────

// ZenAPIBase Zen 网关（OpenAI 兼容）；免费模型匿名可调，bearer 键为字面量 "public"
// （Python: opencode_zen.py:45-47）。
const (
	ZenAPIBase      = "https://opencode.ai/zen/v1"
	ZenModelsURL    = ZenAPIBase + "/models"
	ZenAnonAPIKey   = "public"
	ModelsDevURL    = "https://models.opencode.ai/api.json"
	zenFetchTimeout = 10 * time.Second
	zenWarmTimeout  = 15 * time.Second
	// zenFreeContextWindow 免费模型固定上下文窗口（不从远端元数据推导）
	zenFreeContextWindow = 256 * 1024
	// DefaultFreeModelID 用户默认模型仍为 .env 占位符时的免费兜底模型
	DefaultFreeModelID = "deepseek-v4-flash-free"
)

// zenClientHeaders Zen 免费档门禁头：必须与 opencode 客户端同形，否则 429
// （Python: opencode_zen.py:54-60 ZEN_CLIENT_HEADERS）。
var zenClientHeaders = map[string]string{
	"HTTP-Referer": "https://opencode.ai/",
	"X-Title":      "opencode",
	"X-Source":     "opencode",
	"User-Agent":   "opencode",
}

// 后台重试间隔（Python: opencode_zen.py:109-118）。
const (
	zenBackgroundRetryInterval    = 30 * time.Second
	zenBackgroundRetryMaxDuration = 180 * time.Second
	zenBackgroundRetryLowInterval = 60 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// zenFreeEntries 进程内免费模型缓存（Python: :102 _zen_free_entries）
	zenFreeEntries []map[string]any
	// zenFreeMu 缓存读写锁（Python: :103 _zen_free_lock）
	zenFreeMu sync.RWMutex
	// zenPopulateMu populate 并发保护（非阻塞 TryLock，Python: :111 _populate_lock）
	zenPopulateMu sync.Mutex
	// zenBackgroundStarted 防重复启动后台重试（Python: :114）
	zenBackgroundStarted bool
	// zenLastFetchError 最后一次拉取失败原因（Python: :120）
	zenLastFetchError string
	// zenModelsReadyCallbacks 免费模型就绪回调（Python: :126）
	zenModelsReadyCallbacks []func()
	// zenHTTPClient 拉取用的 HTTP 客户端（超时 10s；测试可替换 BaseURL）
	zenHTTPClient = &http.Client{Timeout: zenFetchTimeout}
	// zenModelsDevURL / zenModelsURL 可在测试中改指 httptest 服务
	zenModelsDevURL = ModelsDevURL
	zenModelsURL    = ZenModelsURL
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// zenFreeModelsEnabledFn 免费拉取开关（包级函数变量承载 Python 模块级函数
// _zen_free_models_enabled；生产实现恒 false——登录模型替代后硬关闭，
// 前端/config 无法重新打开探测）。
var zenFreeModelsEnabledFn = func() bool { return false }

func zenFreeModelsEnabled() bool { return zenFreeModelsEnabledFn() }

// isFreeModel cost.input == 0 即免费（Python: :139 _is_free_model）。
// 对齐 opencode provider.ts 判定；-free 后缀只是命名惯例（big-pickle/grok-code
// 无后缀也免费）。cost.input 按浮点比较——int(0.14) 截断会误判付费模型。
func isFreeModel(modelMeta map[string]any) bool {
	cost, ok := modelMeta["cost"].(map[string]any)
	if !ok {
		return false
	}
	input := cost["input"]
	switch v := input.(type) {
	case int:
		return float64(v) == 0.0
	case int64:
		return float64(v) == 0.0
	case float64:
		return v == 0.0
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err != nil {
			return false
		}
		return f == 0.0
	}
	return false
}

// buildZenModelEntry 构造一条 models.defaults 形状的内存条目
// （Python: :160 _build_zen_model_entry）。display 名剥离尾部 " Free" 族后缀；
// is_free 标记供前端分组；免费档 429 不重试（max_retries=0）。
func buildZenModelEntry(modelID, name string, context int) map[string]any {
	displayName := name
	for _, sep := range []string{" Free", "-free", "-Free", " (free)", " (Free)"} {
		if len(displayName) >= len(sep) && displayName[len(displayName)-len(sep):] == sep {
			displayName = strings.TrimRight(displayName[:len(displayName)-len(sep)], " ")
			break
		}
	}
	headers := map[string]string{}
	for k, v := range zenClientHeaders {
		headers[k] = v
	}
	return map[string]any{
		"model_client_config": map[string]any{
			"api_base":                   ZenAPIBase,
			"api_key":                    ZenAnonAPIKey,
			"model_name":                 modelID,
			"client_provider":            "OpenAI",
			"timeout":                    360,
			"stream_first_chunk_timeout": 300,
			"stream_idle_timeout":        120,
			"verify_ssl":                 true,
			"custom_headers":             headers,
			"max_retries":                0,
		},
		"model_config_obj": map[string]any{
			"temperature": 0.95,
		},
		"alias":                 displayName,
		"context_window_tokens": context,
		"is_free":               true,
	}
}

// fetchModelsDevCatalog 拉 models.dev 目录并返回 opencode 供应商模型表
// （Python: :214 _fetch_models_dev_catalog）。任何失败回空表（本轮无法判定免费）。
func fetchModelsDevCatalog() map[string]map[string]any {
	zenLastFetchError = ""
	resp, err := zenHTTPClient.Get(zenModelsDevURL)
	if err != nil {
		zenLastFetchError = fmt.Sprintf("models.dev: %v", err)
		logger.Warn(logComponent).Err(err).Msg("[OpencodeZen] failed to fetch models.dev catalog; no free models available this run")
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		zenLastFetchError = fmt.Sprintf("models.dev: HTTP %d", resp.StatusCode)
		logger.Warn(logComponent).Int("status", resp.StatusCode).Msg("[OpencodeZen] failed to fetch models.dev catalog; no free models available this run")
		return nil
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		zenLastFetchError = fmt.Sprintf("models.dev: %v", err)
		logger.Warn(logComponent).Err(err).Msg("[OpencodeZen] failed to fetch models.dev catalog; no free models available this run")
		return nil
	}
	opencode, _ := payload["opencode"].(map[string]any)
	models, _ := opencode["models"].(map[string]any)
	if len(models) == 0 {
		zenLastFetchError = "models.dev: catalog has no opencode models"
		logger.Warn(logComponent).Msg("[OpencodeZen] models.dev catalog has no opencode models; no free models available this run")
		return nil
	}
	result := map[string]map[string]any{}
	for mid, meta := range models {
		if m, ok := meta.(map[string]any); ok {
			result[mid] = m
		}
	}
	return result
}

// fetchZenFreeModels 判定当前真实可服务的免费模型
// （Python: :246 _fetch_zen_free_models）。models.dev cost 判免费 → 与 Zen
// 实时 /models 求交（免费档轮换，只报 Zen 正在服务的）；任一源不可达回空。
func fetchZenFreeModels() []map[string]any {
	devCatalog := fetchModelsDevCatalog()
	if len(devCatalog) == 0 {
		return nil
	}
	// cost 元数据判免费
	freeIDs := map[string]map[string]any{}
	for mid, meta := range devCatalog {
		mid = strings.TrimSpace(mid)
		if mid != "" && isFreeModel(meta) {
			freeIDs[mid] = meta
		}
	}
	// Zen 实时目录：只保留 Zen 正在服务的
	liveIDs := map[string]bool{}
	resp, err := zenHTTPClient.Get(zenModelsURL)
	if err != nil {
		zenLastFetchError = fmt.Sprintf("Zen /models: %v", err)
		logger.Warn(logComponent).Err(err).Msg("[OpencodeZen] failed to fetch live Zen model list; no free models available this run")
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
			for _, item := range payload.Data {
				if id, ok := item["id"].(string); ok {
					liveIDs[strings.TrimSpace(id)] = true
				}
			}
		}
	} else {
		zenLastFetchError = fmt.Sprintf("Zen /models: HTTP %d", resp.StatusCode)
		logger.Warn(logComponent).Int("status", resp.StatusCode).Msg("[OpencodeZen] failed to fetch live Zen model list; no free models available this run")
		return nil
	}
	if len(liveIDs) == 0 {
		zenLastFetchError = "Zen /models: live model list is empty"
		logger.Warn(logComponent).Msg("[OpencodeZen] live Zen model list is empty; no free models available this run")
		return nil
	}
	free := []map[string]any{}
	for mid, meta := range freeIDs {
		if !liveIDs[mid] {
			continue // 目录免费但 Zen 当前未服务
		}
		name := strings.TrimSpace(strOrDefault(meta["name"], mid))
		if name == "" {
			name = mid
		}
		free = append(free, map[string]any{"id": mid, "name": name, "context": zenFreeContextWindow})
	}
	if len(free) == 0 {
		logger.Info(logComponent).Msg("[OpencodeZen] no free models currently served by Zen; no free models available this run")
	} else {
		logger.Info(logComponent).Int("count", len(free)).Msg("[OpencodeZen] identified free models served by Zen")
	}
	return free
}

// populateZenFreeEntries 拉取并写入缓存（Python: :317 _populate_zen_free_entries）。
// 幂等替换；并发在途时直接回当前缓存大小（非阻塞锁）；永不 panic。
func populateZenFreeEntries() int {
	if !zenPopulateMu.TryLock() {
		zenFreeMu.RLock()
		cacheSize := len(zenFreeEntries)
		zenFreeMu.RUnlock()
		logger.Debug(logComponent).Int("cache_size", cacheSize).Msg("[OpencodeZen] populate already in progress, skipping")
		return cacheSize
	}
	defer zenPopulateMu.Unlock()

	if !zenFreeModelsEnabled() {
		logger.Info(logComponent).Msg("[OpencodeZen] fetching disabled by env; skipping")
		zenFreeMu.Lock()
		zenFreeEntries = nil
		zenFreeMu.Unlock()
		return 0
	}
	freeModels := fetchZenFreeModels()
	entries := make([]map[string]any, 0, len(freeModels))
	for _, m := range freeModels {
		entries = append(entries, buildZenModelEntry(
			fmt.Sprintf("%v", m["id"]), fmt.Sprintf("%v", m["name"]), zenFreeContextWindow))
	}
	zenFreeMu.Lock()
	zenFreeEntries = entries
	zenFreeMu.Unlock()
	if len(entries) > 0 {
		// 仅成功后清空失败原因
		zenLastFetchError = ""
		notifyModelsReady()
	}
	logger.Info(logComponent).Int("count", len(entries)).Msg("[OpencodeZen] cached free models in memory")
	return len(entries)
}

// startBackgroundRetry 启动后台重试循环（Python: :413 _start_background_retry）。
// 同一时间只有一个循环；daemon 化（Go goroutine 不阻塞退出）。
func startBackgroundRetry() {
	zenFreeMu.Lock()
	if zenBackgroundStarted {
		zenFreeMu.Unlock()
		return
	}
	zenBackgroundStarted = true
	zenFreeMu.Unlock()
	logger.Info(logComponent).
		Str("interval", zenBackgroundRetryInterval.String()).
		Str("low_interval", zenBackgroundRetryLowInterval.String()).
		Msg("[OpencodeZen] background retry scheduled")
	go backgroundRetryLoop()
}

// backgroundRetryLoop 高频 30s → 低频 60s 持续重试，直到成功或开关关闭
// （Python: :436 _background_retry_loop）。退出时重置 started 标志。
func backgroundRetryLoop() {
	defer func() {
		zenFreeMu.Lock()
		zenBackgroundStarted = false
		zenFreeMu.Unlock()
	}()
	start := time.Now()
	for {
		elapsed := time.Since(start)
		interval := zenBackgroundRetryInterval
		if elapsed >= zenBackgroundRetryMaxDuration {
			interval = zenBackgroundRetryLowInterval
		}
		time.Sleep(interval)
		if !zenFreeModelsEnabled() {
			logger.Info(logComponent).Msg("[OpencodeZen] background retry stopped: free models disabled")
			return
		}
		if len(GetZenFreeModelEntries()) > 0 {
			logger.Info(logComponent).Msg("[OpencodeZen] background retry skipped: cache already populated")
			return
		}
		logger.Info(logComponent).Int("attempt", int(time.Since(start).Seconds())).Msg("[OpencodeZen] background retry")
		populateZenFreeEntries()
	}
}

// notifyModelsReady 通知回调（Python: :607 _notify_models_ready）。
// Python 经 call_soon_threadsafe 调度回主 loop；Go 侧等价为新 goroutine 执行
// （形态偏差 D-2b，语义一致：回调不在拉取线程上同步执行）。
func notifyModelsReady() {
	if len(zenModelsReadyCallbacks) == 0 {
		return
	}
	go dispatchModelsReady()
}

// dispatchModelsReady 执行全部回调（Python: :621 _dispatch_models_ready）。
func dispatchModelsReady() {
	for _, cb := range zenModelsReadyCallbacks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Debug(logComponent).Msg("[OpencodeZen] models ready callback failed")
				}
			}()
			cb()
		}()
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WarmZenFreeModels 预热入口（Python: :367 warm_zen_free_models）。
// 整轮限时 zenWarmTimeout，超时/失败缓存为空并调度后台重试；
// 开关关闭（恒关）时直接跳过。report_status=true 时回布尔。
func WarmZenFreeModels(reason string, reportStatus bool) any {
	if !zenFreeModelsEnabled() {
		logger.Info(logComponent).Str("reason", reason).Msg("[OpencodeZen] fetching disabled; skipping warm")
		if reportStatus {
			return true
		}
		return nil
	}
	done := make(chan struct{})
	go func() {
		populateZenFreeEntries()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(zenWarmTimeout):
		logger.Warn(logComponent).Str("timeout", zenWarmTimeout.String()).Str("reason", reason).
			Msg("[OpencodeZen] fetch round timed out; no free models available this run")
	}
	if len(GetZenFreeModelEntries()) == 0 {
		logger.Info(logComponent).Str("reason", reason).Msg("[OpencodeZen] warm failed, cache empty, scheduling background retry")
		startBackgroundRetry()
	}
	if reportStatus {
		return len(GetZenFreeModelEntries()) > 0
	}
	return nil
}

// GetZenFreeModelEntries 缓存浅拷贝（Python: :496 get_zen_free_model_entries）。
// 开关关闭恒回空（登录模型替代后即使残留缓存也不报）。
func GetZenFreeModelEntries() []map[string]any {
	if !zenFreeModelsEnabled() {
		return nil
	}
	zenFreeMu.RLock()
	defer zenFreeMu.RUnlock()
	out := make([]map[string]any, 0, len(zenFreeEntries))
	for _, e := range zenFreeEntries {
		out = append(out, e)
	}
	return out
}

// GetZenFreeContextWindow 免费模型展示用上下文窗口（Python: :512）。
func GetZenFreeContextWindow() int {
	return zenFreeContextWindow
}

// GetZenDefaultFreeModelEntry 产品默认兜底免费模型（Python: :517）。
// 优先 DefaultFreeModelID，其次第一条；不可用回 nil。
func GetZenDefaultFreeModelEntry() map[string]any {
	entries := GetZenFreeModelEntries()
	for _, entry := range entries {
		cfg, _ := entry["model_client_config"].(map[string]any)
		if cfg["model_name"] == DefaultFreeModelID {
			return entry
		}
	}
	if len(entries) > 0 {
		return entries[0]
	}
	return nil
}

// MatchZenFreeModelName raw 是某免费模型 id 或 alias 时回规范 id
// （Python: :531 match_zen_free_model_name）。
func MatchZenFreeModelName(raw any) any {
	value := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(raw)))
	if value == "" {
		return nil
	}
	for _, entry := range GetZenFreeModelEntries() {
		cfg, _ := entry["model_client_config"].(map[string]any)
		modelName := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(cfg["model_name"])))
		alias := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(entry["alias"])))
		if modelName != "" && (value == modelName || value == alias) {
			return modelName
		}
	}
	return nil
}

// PublicZenFreeModelRows Gateway 可直接合并的公开行（Python: :585）。
func PublicZenFreeModelRows() []map[string]any {
	rows := []map[string]any{}
	// AppendZenFreeModels 返回扩容后的切片（append 语义），必须接住返回值
	return AppendZenFreeModels(rows)
}

// RegisterModelsReadyCallback 注册就绪回调（Python: :598）。
func RegisterModelsReadyCallback(cb func()) {
	zenModelsReadyCallbacks = append(zenModelsReadyCallbacks, cb)
}

// SetZenHTTPBaseURL 测试钩子：改指 httptest 服务。
func SetZenHTTPBaseURL(modelsDev, models string) {
	zenModelsDevURL = modelsDev
	zenModelsURL = models
}

// ZenLastFetchError 供排查读取最后失败原因（Python _last_fetch_error）。
func ZenLastFetchError() string { return zenLastFetchError }

// ──────────────────────────── 非导出函数 ────────────────────────────

// orEmptyAny Python `raw or ""`：nil/None 回空串。
func orEmptyAny(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// strOrDefault 值非空转字符串否则默认（Python str(x.get(k, default))）。
func strOrDefault(value any, defaultVal any) string {
	if value == nil {
		return fmt.Sprintf("%v", defaultVal)
	}
	return fmt.Sprintf("%v", value)
}

// boolTextStr bool → "true"/"false"。
func boolTextStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// AppendZenFreeModels 把缓存的免费模型追加进 models.list payload
// （Python: :545 append_zen_free_models）。依赖 models_handlers 的
// reasoningLevelDisplay——同包直调（Python 跨模块 import，此处同包聚合为
// configpanel 一体，归属不变）。按 model_name 去重：已存在跳过。
func AppendZenFreeModels(models []map[string]any) []map[string]any {
	existingNames := map[string]bool{}
	for _, item := range models {
		if name := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(item["model_name"]))); name != "" && name != "<nil>" {
			existingNames[name] = true
		}
	}
	for _, entry := range GetZenFreeModelEntries() {
		modelConfig, _ := entry["model_client_config"].(map[string]any)
		modelConfigObj, _ := entry["model_config_obj"].(map[string]any)
		modelName := strings.TrimSpace(fmt.Sprintf("%v", orEmptyAny(modelConfig["model_name"])))
		if modelName == "" || existingNames[modelName] {
			continue
		}
		models = append(models, map[string]any{
			"model_name":            modelName,
			"api_base":              orEmptyAny(modelConfig["api_base"]),
			"api_key":               orEmptyAny(modelConfig["api_key"]),
			"model_provider":        orEmptyAny(modelConfig["client_provider"]),
			"temperature":           modelConfigObj["temperature"],
			"reasoning_level":       ReasoningLevelDisplay(modelConfigObj["reasoning_level"]),
			"is_default":            entry["is_default"],
			"is_agentos":            false,
			"is_free":               true,
			"alias":                 orEmptyAny(entry["alias"]),
			"context_window_tokens": parsePositiveIntOr(entry["context_window_tokens"], 0),
		})
		existingNames[modelName] = true
	}
	return models
}

// SetZenFreeModelsEnabledOverride 测试接缝：覆写免费拉取开关。
// 对齐 Python 测试对模块属性 _zen_free_models_enabled 的 monkeypatch 惯例
// （Python: config_panel 各 handler「调用方可 monkeypatch 本模块属性以隔离测试」）；
// 生产代码不得调用。
func SetZenFreeModelsEnabledOverride(enabled bool) {
	zenFreeModelsEnabledFn = func() bool { return enabled }
}
