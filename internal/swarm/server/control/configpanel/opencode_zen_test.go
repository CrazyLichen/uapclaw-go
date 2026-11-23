package configpanel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// zenFixtureServer 本地 models.dev + Zen /models fixture（httptest，可 mock 服务禁 build tag）。
func zenFixtureServer(t *testing.T, devPayload, zenPayload string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/dev", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(devPayload))
	})
	mux.HandleFunc("/zen", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(zenPayload))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	SetZenHTTPBaseURL(srv.URL+"/dev", srv.URL+"/zen")
	t.Cleanup(func() {
		SetZenHTTPBaseURL(ModelsDevURL, ZenModelsURL)
	})
	return srv
}

// TestIsFreeModel cost.input 判免费（含浮点截断陷阱回归）
func TestIsFreeModel(t *testing.T) {
	if !isFreeModel(map[string]any{"cost": map[string]any{"input": 0}}) {
		t.Error("input=0 应免费")
	}
	if !isFreeModel(map[string]any{"cost": map[string]any{"input": 0.0}}) {
		t.Error("input=0.0 应免费")
	}
	if isFreeModel(map[string]any{"cost": map[string]any{"input": 0.14}}) {
		t.Error("input=0.14 应付费（浮点截断陷阱）")
	}
	if isFreeModel(map[string]any{"cost": map[string]any{"input": "x"}}) {
		t.Error("非数字应付费")
	}
	if isFreeModel(map[string]any{}) {
		t.Error("无 cost 应付费")
	}
	if isFreeModel(map[string]any{"cost": "bad"}) {
		t.Error("cost 非 dict 应付费")
	}
}

// TestBuildZenModelEntry 条目形状（public 键/头/温度/is_free）
func TestBuildZenModelEntry(t *testing.T) {
	entry := buildZenModelEntry("laguna-s-2.1-free", "Laguna S 2.1 Free", zenFreeContextWindow)
	cfg := entry["model_client_config"].(map[string]any)
	if cfg["api_base"] != ZenAPIBase || cfg["api_key"] != ZenAnonAPIKey {
		t.Errorf("api_base/api_key = %v/%v", cfg["api_base"], cfg["api_key"])
	}
	if cfg["model_name"] != "laguna-s-2.1-free" {
		t.Errorf("model_name = %v, want 完整 API id（-free 不剥离）", cfg["model_name"])
	}
	if cfg["client_provider"] != "OpenAI" {
		t.Errorf("client_provider = %v, want OpenAI", cfg["client_provider"])
	}
	if cfg["max_retries"] != 0 {
		t.Errorf("max_retries = %v, want 0（免费档 429 不重试）", cfg["max_retries"])
	}
	headers := cfg["custom_headers"].(map[string]string)
	if headers["User-Agent"] != "opencode" || headers["HTTP-Referer"] != "https://opencode.ai/" {
		t.Errorf("headers = %v, want opencode 同形", headers)
	}
	if entry["alias"] != "Laguna S 2.1" {
		t.Errorf("alias = %v, want 剥离尾部 Free", entry["alias"])
	}
	if entry["is_free"] != true {
		t.Errorf("is_free = %v", entry["is_free"])
	}
	if entry["context_window_tokens"] != zenFreeContextWindow {
		t.Errorf("context = %v", entry["context_window_tokens"])
	}
}

// TestBuildZenModelEntry_DisplaySuffixes 展示名后缀剥离边界
func TestBuildZenModelEntry_DisplaySuffixes(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Model X-free", "Model X"},
		{"Model X (free)", "Model X"},
		{"Model X Free", "Model X"},
		{"Model X", "Model X"},
		{"Free", "Free"}, // 仅后缀剥离，整名是 Free 不动
	} {
		entry := buildZenModelEntry("id", c.in, 1)
		if entry["alias"] != c.want {
			t.Errorf("alias(%q) = %q, want %q", c.in, entry["alias"], c.want)
		}
	}
}

// TestPopulateZenFreeEntries_Disabled 硬关闭：开关恒 false，缓存清空
func TestPopulateZenFreeEntries_Disabled(t *testing.T) {
	zenFixtureServer(t, `{"opencode":{"models":{"a-free":{"name":"A","cost":{"input":0}}}}}`, `{"data":[{"id":"a-free"}]}`)
	if zenFreeModelsEnabled() {
		t.Fatal("开关应恒 false（硬关闭）")
	}
	n := populateZenFreeEntries()
	if n != 0 {
		t.Errorf("populate = %d, want 0（硬关闭）", n)
	}
	if len(GetZenFreeModelEntries()) != 0 {
		t.Error("缓存应为空")
	}
}

// TestFetchZenFreeModels 交集聚合（dev 免费 ∩ Zen 实时服务）
func TestFetchZenFreeModels(t *testing.T) {
	zenFixtureServer(t,
		`{"opencode":{"models":{
			"a-free":{"name":"A Free","cost":{"input":0}},
			"b-paid":{"name":"B","cost":{"input":0.14}},
			"c-free":{"name":"C Free","cost":{"input":0}}
		}}}`,
		`{"data":[{"id":"a-free"},{"id":"b-paid"}]}`)
	free := fetchZenFreeModels()
	if len(free) != 1 || free[0]["id"] != "a-free" {
		t.Errorf("free = %v, want 仅 a-free（b 付费、c 未服务）", free)
	}
}

// TestFetchZenFreeModels_CatalogUnavailable 源不可达回空并记错误
func TestFetchZenFreeModels_CatalogUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	SetZenHTTPBaseURL(srv.URL+"/dev", srv.URL+"/zen")
	t.Cleanup(func() { SetZenHTTPBaseURL(ModelsDevURL, ZenModelsURL) })
	if free := fetchZenFreeModels(); len(free) != 0 {
		t.Errorf("free = %v, want 空", free)
	}
	if !strings.Contains(ZenLastFetchError(), "models.dev") {
		t.Errorf("last error = %q, want 含 models.dev", ZenLastFetchError())
	}
}

// TestWarmZenFreeModels_Disabled 开关关闭时预热直跳（reason=config-toggle 路径）
func TestWarmZenFreeModels_Disabled(t *testing.T) {
	got := WarmZenFreeModels("config-toggle", false)
	if got != nil {
		t.Errorf("report_status=false 时 = %v, want nil", got)
	}
	if got := WarmZenFreeModels("config-toggle", true); got != true {
		t.Errorf("report_status=true 时 = %v, want true（跳过即成功）", got)
	}
}

// TestGetZenFreeModelEntries_Disabled 开关关闭恒回空
func TestGetZenFreeModelEntries_Disabled(t *testing.T) {
	zenFreeMu.Lock()
	zenFreeEntries = []map[string]any{{"alias": "stale"}}
	zenFreeMu.Unlock()
	t.Cleanup(func() {
		zenFreeMu.Lock()
		zenFreeEntries = nil
		zenFreeMu.Unlock()
	})
	if GetZenFreeModelEntries() != nil {
		t.Error("开关关闭应回空（残留缓存不报）")
	}
	if GetZenDefaultFreeModelEntry() != nil {
		t.Error("默认免费模型应为 nil")
	}
	if MatchZenFreeModelName("whatever") != nil {
		t.Error("匹配应回 nil")
	}
	if got := MatchZenFreeModelName(nil); got != nil {
		t.Errorf("nil 输入 = %v, want nil", got)
	}
	if GetZenFreeContextWindow() != zenFreeContextWindow {
		t.Errorf("context window = %d", GetZenFreeContextWindow())
	}
	if rows := PublicZenFreeModelRows(); len(rows) != 0 {
		t.Errorf("public rows = %v, want 空", rows)
	}
}

// TestAppendZenFreeModels_Merge 归并去重（models.list 追加语义）。
// 临时开启开关变量以驱动归并逻辑（Python 开关为模块级函数；同包函数变量覆盖，
// 结束恢复硬关闭）。
func TestAppendZenFreeModels_Merge(t *testing.T) {
	zenFreeModelsEnabledFn = func() bool { return true }
	t.Cleanup(func() { zenFreeModelsEnabledFn = func() bool { return false } })
	zenFreeMu.Lock()
	zenFreeEntries = []map[string]any{buildZenModelEntry("m-free", "M Free", zenFreeContextWindow)}
	zenFreeMu.Unlock()
	t.Cleanup(func() {
		zenFreeMu.Lock()
		zenFreeEntries = nil
		zenFreeMu.Unlock()
	})
	models := []map[string]any{}
	models = AppendZenFreeModels(models)
	if len(models) != 1 {
		t.Fatalf("追加后 = %d 条, want 1", len(models))
	}
	row := models[0]
	if row["model_name"] != "m-free" || row["api_key"] != ZenAnonAPIKey ||
		row["model_provider"] != "OpenAI" || row["is_free"] != true || row["is_agentos"] != false {
		t.Errorf("row = %v", row)
	}
	if row["reasoning_level"] != "" {
		t.Errorf("reasoning_level = %v, want 空串（nil→空）", row["reasoning_level"])
	}
	// 再追加一次：去重
	if models = AppendZenFreeModels(models); len(models) != 1 {
		t.Errorf("去重后 = %d 条, want 1", len(models))
	}
}

// TestReasoningLevelDisplay 布尔映射回 on/off
func TestReasoningLevelDisplay(t *testing.T) {
	if ReasoningLevelDisplay(true) != "on" || ReasoningLevelDisplay(false) != "off" {
		t.Error("布尔应映射 on/off")
	}
	if ReasoningLevelDisplay(nil) != "" {
		t.Error("nil 应回空串")
	}
	if ReasoningLevelDisplay(" high ") != "high" {
		t.Errorf("字符串应去空白: %q", ReasoningLevelDisplay(" high "))
	}
}

// TestParsePositiveInt 单位解析与上限
func TestParsePositiveInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{256 * 1024, 256 * 1024},
		{"256k", 256 * 1024},
		{"256 K", 256 * 1024},
		{"1m", 1024 * 1024},
		{"128 tokens", 128},
		{"128.5", 128},
		{"abc", 0},
		{"-5", 0},
		{0, 0},
		{nil, 0},
		{"1e", 0},
	}
	for _, c := range cases {
		if got := ParsePositiveInt(c.in); got != c.want {
			t.Errorf("ParsePositiveInt(%v) = %d, want %d", c.in, got, c.want)
		}
	}
	if ParsePositiveInt("999999999999999999999") != int(MaxContextWindowTokens) {
		t.Errorf("超大值应钳到上限")
	}
}

// TestRegisterModelsReadyCallback 回调注册与通知分发
func TestRegisterModelsReadyCallback(t *testing.T) {
	var mu sync.Mutex
	called := 0
	RegisterModelsReadyCallback(func() {
		mu.Lock()
		called++
		mu.Unlock()
	})
	notifyModelsReady()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return called == 1
	})
}

// TestDispatchModelsReady_PanicRecovered 回调 panic 不外泄
func TestDispatchModelsReady_PanicRecovered(t *testing.T) {
	RegisterModelsReadyCallback(func() { panic("boom") })
	dispatchModelsReady() // 不应 panic 外泄
}

// TestStartBackgroundRetry_DoubleStart 防重复启动
func TestStartBackgroundRetry_DoubleStart(t *testing.T) {
	// 硬关闭下循环首轮即退出并重置标志；这里只验证不会 panic/死锁
	startBackgroundRetry()
	startBackgroundRetry()
}

// waitFor 轮询等待条件成立（异步回调）。
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("条件未在时限内成立")
}
