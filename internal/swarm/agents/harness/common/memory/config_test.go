package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// injectTestConfigLoader 注入测试配置加载器（审查修正：不读真实全局配置，避免 flaky）
func injectTestConfigLoader(t *testing.T, yamlContent string) {
	t.Helper()
	orig := configLoader
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	configLoader = func() (map[string]any, error) {
		cfg, err := config.New(cfgPath)
		if err != nil {
			return nil, err
		}
		return cfg.Load()
	}
	t.Cleanup(func() {
		configLoader = orig
		ClearConfigCache()
	})
}

// TestGetEmbedConfig_读取embed段 对齐 Python get_embed_config
func TestGetEmbedConfig_读取embed段(t *testing.T) {
	injectTestConfigLoader(t, `
embed:
  embed_api_key: sk-test
  embed_base_url: https://api.example.com/v1
  embed_model: text-embedding-v3
`)
	got := GetEmbedConfig()
	if got.APIKey != "sk-test" || got.BaseURL != "https://api.example.com/v1" || got.Model != "text-embedding-v3" {
		t.Fatalf("got %+v", got)
	}
}

// TestGetEmbedConfig_缺省 对齐 Python：embed 段缺失返回零值结构体
func TestGetEmbedConfig_缺省(t *testing.T) {
	injectTestConfigLoader(t, "")
	ClearConfigCache()
	got := GetEmbedConfig()
	if got.APIKey != "" || got.BaseURL != "" || got.Model != "" {
		t.Fatalf("缺失时应为零值，got %+v", got)
	}
}

// TestMemorySettings_默认值 对齐 Python MemorySettings dataclass 默认值
func TestMemorySettings_默认值(t *testing.T) {
	injectTestConfigLoader(t, "")
	s := CreateMemorySettings(".", nil)
	if s.Provider != "openai_compatible" || s.Model != "text-embedding-v3" || s.Fallback != "mock" {
		t.Errorf("provider 默认值错误: %+v", s)
	}
	if len(s.Sources) != 2 || s.Sources[0] != "memory" || s.Sources[1] != "sessions" {
		t.Errorf("sources 默认值错误")
	}
	if s.Chunking.Tokens != 256 || s.Chunking.Overlap != 32 {
		t.Errorf("chunking 默认值错误")
	}
	if s.Query.MaxResults != 10 || s.Query.MinScore != 0.3 {
		t.Errorf("query 默认值错误（minScore 应为 0.3）")
	}
	if !s.Query.Hybrid.Enabled || s.Query.Hybrid.VectorWeight != 0.7 || s.Query.Hybrid.TextWeight != 0.3 || s.Query.Hybrid.CandidateMultiplier != 2.0 {
		t.Errorf("hybrid 默认值错误")
	}
	if s.Store.Path != "memory.db" || !s.Store.VectorEnabled || !s.Store.FtsEnabled {
		t.Errorf("store 默认值错误")
	}
	if !s.Sync.Watch || s.Sync.WatchDebounceMs != 2000 || !s.Sync.OnSearch || !s.Sync.OnSessionStart || s.Sync.IntervalMinutes != 0 {
		t.Errorf("sync 默认值错误")
	}
	if !s.Cache.Enabled || s.Cache.MaxEntries != 10000 {
		t.Errorf("cache 默认值错误")
	}
}

// TestMemorySettings_memory段覆盖 对齐 Python create_memory_settings 读 config memory 段
func TestMemorySettings_memory段覆盖(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  provider: my_provider
  fallback: none
  sources: [memory]
  chunking:
    tokens: 128
    overlap: 16
embed:
  embed_model: embed-model-x
`)
	s := CreateMemorySettings(".", nil)
	if s.Provider != "my_provider" {
		t.Errorf("provider 覆盖失败")
	}
	if s.Fallback != "none" {
		t.Errorf("fallback 覆盖失败")
	}
	if len(s.Sources) != 1 {
		t.Errorf("sources 覆盖失败")
	}
	if s.Chunking.Tokens != 128 || s.Chunking.Overlap != 16 {
		t.Errorf("chunking 覆盖失败")
	}
	if s.Model != "embed-model-x" {
		t.Errorf("model 应取 embed 段 embed_model")
	}
}

// TestMemorySettings_store段 对齐 Python：overrides 无 store 时构造三键
func TestMemorySettings_store段(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  store:
    path: custom.db
    vector:
      enabled: false
`)
	s := CreateMemorySettings(".", nil)
	if s.Store.Path != "custom.db" {
		t.Errorf("store.path 覆盖失败: %s", s.Store.Path)
	}
	if s.Store.VectorEnabled {
		t.Errorf("store.vector.enabled=false 覆盖失败")
	}
	if !s.Store.FtsEnabled {
		t.Errorf("store.fts 未配置应保持默认 true")
	}
}

// TestMemorySettings_overrides透传 对齐 Python for key, value in overrides.items()
func TestMemorySettings_overrides透传(t *testing.T) {
	injectTestConfigLoader(t, "")
	s := CreateMemorySettings(".", map[string]any{
		"provider": "over_provider",
		"query":    map[string]any{"minScore": 0.5},
		"sources":  []any{"memory"},
	})
	if s.Provider != "over_provider" {
		t.Errorf("overrides provider 失败")
	}
	if s.Query.MinScore != 0.5 {
		t.Errorf("overrides minScore 失败")
	}
	if len(s.Sources) != 1 || s.Sources[0] != "memory" {
		t.Errorf("overrides sources 失败: %v", s.Sources)
	}
}

// TestResolveModeMemory_三形态 对齐 Python _resolve_mode_memory
func TestResolveModeMemory_三形态(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"enabled": true}},
			},
			"code": map[string]any{"memory": map[string]any{"enabled": false}},
		},
	}
	if m := ResolveModeMemory("agent.plan", cfg); m["enabled"] != true {
		t.Errorf("agent.plan 解析失败: %v", m)
	}
	if m := ResolveModeMemory("plan", cfg); m["enabled"] != true {
		t.Errorf("bare plan 应归 agent 命名空间: %v", m)
	}
	if m := ResolveModeMemory("code", cfg); m["enabled"] != false {
		t.Errorf("code 解析失败: %v", m)
	}
	if m := ResolveModeMemory("nonexistent", cfg); len(m) != 0 {
		t.Errorf("不存在应返回空 map")
	}
	if m := ResolveModeMemory("", cfg); len(m) != 0 {
		t.Errorf("空 token 应返回空 map")
	}
}

// TestIsMemoryEnabled 对齐 Python is_memory_enabled
func TestIsMemoryEnabled(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"enabled": true}},
			},
		},
	}
	if !IsMemoryEnabled("plan", cfg) {
		t.Errorf("plan 应启用")
	}
	if IsMemoryEnabled("code", cfg) {
		t.Errorf("code 未配置应禁用")
	}
}

// TestIsProactiveMemory 对齐 Python is_proactive_memory
func TestIsProactiveMemory(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"agent": map[string]any{
				"plan": map[string]any{"memory": map[string]any{"is_proactive": true}},
			},
		},
	}
	if !IsProactiveMemory("plan", cfg) {
		t.Errorf("plan 应主动")
	}
	if IsProactiveMemory("fast", cfg) {
		t.Errorf("fast 未配置应非主动")
	}
}

// TestGetMemoryMode 对齐 Python get_memory_mode：cloud 或 local（默认）
func TestGetMemoryMode(t *testing.T) {
	if GetMemoryMode(nil) != "local" {
		t.Errorf("nil config 默认 local")
	}
	if GetMemoryMode(map[string]any{"memory": map[string]any{"mode": "CLOUD "}}) != "cloud" {
		t.Errorf("大小写+空格归一化失败")
	}
	if GetMemoryMode(map[string]any{"memory": map[string]any{"mode": "local"}}) != "local" {
		t.Errorf("local 应返回 local")
	}
}

// TestConfigCache_缓存与清理 对齐 Python _config_cache + clear_config_cache：
// 缓存生效期间换 loader 不重读；清缓存后才重新加载
func TestConfigCache_缓存与清理(t *testing.T) {
	injectTestConfigLoader(t, "embed:\n  embed_model: v1\n")
	if GetEmbedConfig().Model != "v1" {
		t.Fatalf("首次读取失败")
	}
	// 换 loader 内容：缓存仍在，应读到 v1 而非 v2（对齐 Python 缓存语义）
	configLoader = func() (map[string]any, error) {
		return map[string]any{"embed": map[string]any{"embed_model": "v2"}}, nil
	}
	if got := GetEmbedConfig().Model; got != "v1" {
		t.Fatalf("缓存生效期间不应重读，got %s", got)
	}
	ClearConfigCache()
	if got := GetEmbedConfig().Model; got != "v2" {
		t.Fatalf("清缓存后应重新加载，got %s", got)
	}
}
// ──────────────────────────── apply*Overrides 字段级测试 ────────────────────────────

// TestApplyQueryOverrides_全字段 对齐 Python memory 段 query 覆盖：
// maxResults/minScore + hybrid 四键
func TestApplyQueryOverrides_全字段(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  query:
    maxResults: 20
    minScore: 0.55
    hybrid:
      enabled: false
      vectorWeight: 0.9
      textWeight: 0.1
      candidateMultiplier: 3.5
`)
	s := CreateMemorySettings(".", nil)
	if s.Query.MaxResults != 20 {
		t.Errorf("query.maxResults 覆盖失败: %d", s.Query.MaxResults)
	}
	if s.Query.MinScore != 0.55 {
		t.Errorf("query.minScore 覆盖失败: %v", s.Query.MinScore)
	}
	if s.Query.Hybrid.Enabled {
		t.Errorf("query.hybrid.enabled=false 覆盖失败")
	}
	if s.Query.Hybrid.VectorWeight != 0.9 {
		t.Errorf("query.hybrid.vectorWeight 覆盖失败: %v", s.Query.Hybrid.VectorWeight)
	}
	if s.Query.Hybrid.TextWeight != 0.1 {
		t.Errorf("query.hybrid.textWeight 覆盖失败: %v", s.Query.Hybrid.TextWeight)
	}
	if s.Query.Hybrid.CandidateMultiplier != 3.5 {
		t.Errorf("query.hybrid.candidateMultiplier 覆盖失败: %v", s.Query.Hybrid.CandidateMultiplier)
	}
}

// TestApplyQueryOverrides_int类型对齐 yaml.v3 整数解析为 int
func TestApplyQueryOverrides_int类型对齐(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  query:
    maxResults: 15
`)
	s := CreateMemorySettings(".", nil)
	if s.Query.MaxResults != 15 {
		t.Errorf("yaml int 应正确解析: %d", s.Query.MaxResults)
	}
}

// TestApplySyncOverrides_全字段 对齐 Python memory 段 sync 覆盖：五键
func TestApplySyncOverrides_全字段(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  sync:
    watch: false
    watchDebounceMs: 500
    onSearch: false
    onSessionStart: false
    intervalMinutes: 10
`)
	s := CreateMemorySettings(".", nil)
	if s.Sync.Watch {
		t.Errorf("sync.watch=false 覆盖失败")
	}
	if s.Sync.WatchDebounceMs != 500 {
		t.Errorf("sync.watchDebounceMs 覆盖失败: %d", s.Sync.WatchDebounceMs)
	}
	if s.Sync.OnSearch {
		t.Errorf("sync.onSearch=false 覆盖失败")
	}
	if s.Sync.OnSessionStart {
		t.Errorf("sync.onSessionStart=false 覆盖失败")
	}
	if s.Sync.IntervalMinutes != 10 {
		t.Errorf("sync.intervalMinutes 覆盖失败: %d", s.Sync.IntervalMinutes)
	}
}

// TestApplyCacheOverrides_全字段 对齐 Python memory 段 cache 覆盖：enabled/maxEntries
func TestApplyCacheOverrides_全字段(t *testing.T) {
	injectTestConfigLoader(t, `
memory:
  cache:
    enabled: false
    maxEntries: 512
`)
	s := CreateMemorySettings(".", nil)
	if s.Cache.Enabled {
		t.Errorf("cache.enabled=false 覆盖失败")
	}
	if s.Cache.MaxEntries != 512 {
		t.Errorf("cache.maxEntries 覆盖失败: %d", s.Cache.MaxEntries)
	}
}

// TestApplyOverrides_顶层字段 对齐 Python for key, value in overrides.items(): setattr
func TestApplyOverrides_顶层字段(t *testing.T) {
	injectTestConfigLoader(t, "")
	s := CreateMemorySettings(".", map[string]any{
		"model":      "override-model",
		"fallback":   "none",
		"extraPaths": []any{"docs", "notes"},
		"chunking":   map[string]any{"tokens": 64, "overlap": 8},
		"store": map[string]any{
			"path":   "x.db",
			"vector": map[string]any{"enabled": false},
			"fts":    map[string]any{"enabled": false},
		},
		"sync":  map[string]any{"watch": false, "watchDebounceMs": 300, "onSearch": false, "onSessionStart": false, "intervalMinutes": 5},
		"cache": map[string]any{"enabled": false, "maxEntries": 100},
	})
	if s.Model != "override-model" {
		t.Errorf("overrides.model 失败: %s", s.Model)
	}
	if s.Fallback != "none" {
		t.Errorf("overrides.fallback 失败: %s", s.Fallback)
	}
	if len(s.ExtraPaths) != 2 || s.ExtraPaths[0] != "docs" || s.ExtraPaths[1] != "notes" {
		t.Errorf("overrides.extraPaths 失败: %v", s.ExtraPaths)
	}
	if s.Chunking.Tokens != 64 || s.Chunking.Overlap != 8 {
		t.Errorf("overrides.chunking 失败: %+v", s.Chunking)
	}
	if s.Store.Path != "x.db" || s.Store.VectorEnabled || s.Store.FtsEnabled {
		t.Errorf("overrides.store 失败: %+v", s.Store)
	}
	if s.Sync.Watch || s.Sync.WatchDebounceMs != 300 || s.Sync.OnSearch || s.Sync.OnSessionStart || s.Sync.IntervalMinutes != 5 {
		t.Errorf("overrides.sync 失败: %+v", s.Sync)
	}
	if s.Cache.Enabled || s.Cache.MaxEntries != 100 {
		t.Errorf("overrides.cache 失败: %+v", s.Cache)
	}
}

// TestApplyOverrides_类型不匹配保持默认 对齐 Python：仅同类型覆盖生效
func TestApplyOverrides_类型不匹配保持默认(t *testing.T) {
	injectTestConfigLoader(t, "")
	s := CreateMemorySettings(".", map[string]any{
		"provider": 123,           // 非 string 忽略
		"chunking": "not-a-map",   // 非 map 忽略
		"sources":  []any{1, 2},   // 非字符串元素忽略 → 空结果不覆盖
	})
	if s.Provider != "openai_compatible" {
		t.Errorf("非 string provider 应保持默认: %s", s.Provider)
	}
	if s.Chunking.Tokens != 256 {
		t.Errorf("非 map chunking 应保持默认: %d", s.Chunking.Tokens)
	}
	if len(s.Sources) != 2 {
		t.Errorf("非字符串 sources 元素应忽略，保持默认: %v", s.Sources)
	}
}

// ──────────────────────────── numVal/strSlice 辅助函数测试 ────────────────────────────

// TestNumVal_各类型 对齐 yaml.v3 int / JSON float64 两种解析来源
func TestNumVal_各类型(t *testing.T) {
	cases := []struct {
		in     any
		want   float64
		wantOK bool
	}{
		{int(42), 42, true},
		{int64(42), 42, true},
		{uint64(42), 42, true},
		{float64(42.5), 42.5, true},
		{"42", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}
	for _, c := range cases {
		got, ok := numVal(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("numVal(%v) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestStrSlice_各类型 对齐 strSlice：[]string / []any / 空
func TestStrSlice_各类型(t *testing.T) {
	if got := strSlice([]string{"a", "b"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("[]string 解析失败: %v", got)
	}
	if got := strSlice([]any{"x", 1, "y"}); len(got) != 2 || got[1] != "y" {
		t.Errorf("[]any 过滤非字符串失败: %v", got)
	}
	if got := strSlice("not-a-slice"); got != nil {
		t.Errorf("非切片应返回 nil: %v", got)
	}
	if got := strSlice([]any{1, 2}); got != nil {
		t.Errorf("全非字符串元素应返回 nil: %v", got)
	}
}

// TestStrVal_默认值 对齐 strValDefault
func TestStrVal_默认值(t *testing.T) {
	if got := strValDefault("v", "d"); got != "v" {
		t.Errorf("字符串应直返: %s", got)
	}
	if got := strValDefault(123, "d"); got != "d" {
		t.Errorf("非字符串应返回默认: %s", got)
	}
}

// ──────────────────────────── mapsClone 测试 ────────────────────────────

// TestMapsClone_浅拷贝 对齐 Python dict(overrides) 浅拷贝语义
func TestMapsClone_浅拷贝(t *testing.T) {
	inner := map[string]any{"k": "v"}
	orig := map[string]any{"outer": inner}
	clone := mapsClone(orig)
	if len(clone) != 1 {
		t.Fatalf("克隆应含 1 键")
	}
	cloneInner, _ := clone["outer"].(map[string]any)
	if cloneInner["k"] != "v" {
		t.Errorf("浅拷贝内层值应可见: %v", cloneInner)
	}
	clone["added"] = true
	if _, exists := orig["added"]; exists {
		t.Errorf("克隆修改不应影响原 map")
	}
}
