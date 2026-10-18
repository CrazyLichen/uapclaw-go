package memory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// TestGetMemoryEngine_合法值 对齐 Python get_memory_engine L30-39
func TestGetMemoryEngine_合法值(t *testing.T) {
	cases := []struct {
		engine string
		want   string
	}{
		{"builtin", "builtin"},
		{"external", "external"},
		{"both", "both"},
		{"none", "none"},
		{"  External  ", "external"}, // strip + lower
	}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := GetMemoryEngine(cfg); got != c.want {
			t.Errorf("engine=%q → %q, want %q", c.engine, got, c.want)
		}
	}
}

// TestGetMemoryEngine_回落builtin 空白/缺失/非法值回落 builtin（Python: or "builtin" + in _VALID_ENGINES 检查）
func TestGetMemoryEngine_回落builtin(t *testing.T) {
	for _, cfg := range []map[string]any{
		nil,                          // 无 memory 段
		{"memory": map[string]any{}}, // engine 缺失
		{"memory": map[string]any{"engine": ""}},
		{"memory": map[string]any{"engine": "  "}},
		{"memory": map[string]any{"engine": "invalid"}}, // 非法值 → builtin
	} {
		if got := GetMemoryEngine(cfg); got != "builtin" {
			t.Errorf("cfg=%v → %q, want builtin", cfg, got)
		}
	}
}

// TestGetMemoryEngine_nil走loadConfig 对齐 Python config=None → _load_config()
func TestGetMemoryEngine_nil走loadConfig(t *testing.T) {
	injectTestConfigLoader(t, "memory:\n  engine: external\n")
	ClearConfigCache()
	if got := GetMemoryEngine(nil); got != "external" {
		t.Errorf("nil cfg → %q, want external（应走包内 loadConfig）", got)
	}
	ClearConfigCache()
}

// TestIsBuiltinMemoryAllowed 对齐 L42-44：engine ∈ {builtin, both}
func TestIsBuiltinMemoryAllowed(t *testing.T) {
	cases := []struct {
		engine string
		want   bool
	}{{"builtin", true}, {"both", true}, {"external", false}, {"none", false}}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := IsBuiltinMemoryAllowed(cfg); got != c.want {
			t.Errorf("engine=%s → %v, want %v", c.engine, got, c.want)
		}
	}
}

// TestIsExternalMemoryAllowed 对齐 L47-49：engine ∈ {external, both}（Go 新增函数）
func TestIsExternalMemoryAllowed(t *testing.T) {
	cases := []struct {
		engine string
		want   bool
	}{{"external", true}, {"both", true}, {"builtin", false}, {"none", false}}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := IsExternalMemoryAllowed(cfg); got != c.want {
			t.Errorf("engine=%s → %v, want %v", c.engine, got, c.want)
		}
	}
}

// TestGetExternalMemoryConfig_默认值填充 对齐 L52-70：缺失字段填默认
func TestGetExternalMemoryConfig_默认值填充(t *testing.T) {
	got := GetExternalMemoryConfig(map[string]any{"memory": map[string]any{}})
	if got.Provider != "" {
		t.Errorf("Provider = %q, want 空串", got.Provider)
	}
	if got.UserID != "__default__" {
		t.Errorf("UserID = %q, want __default__", got.UserID)
	}
	if got.ScopeID != "__default__" {
		t.Errorf("ScopeID = %q, want __default__", got.ScopeID)
	}
	if len(got.AllowedPlugins) != 0 {
		t.Errorf("AllowedPlugins = %v, want 空列表", got.AllowedPlugins)
	}
	if got.Mem0 == nil || got.OpenViking == nil || got.OpenJiuwen == nil {
		t.Error("三个 provider 子段缺失时应为空 map 而非 nil")
	}
}

// TestGetExternalMemoryConfig_读取配置段 对齐 L62-70
func TestGetExternalMemoryConfig_读取配置段(t *testing.T) {
	cfg := map[string]any{"memory": map[string]any{"external": map[string]any{
		"provider":        "  mem0  ", // strip 测试
		"user_id":         "u1",
		"scope_id":        "s1",
		"allowed_plugins": []any{"p1", "p2"},
		"mem0":            map[string]any{"api_key": "k"},
	}}}
	got := GetExternalMemoryConfig(cfg)
	if got.Provider != "mem0" {
		t.Errorf("Provider = %q, want mem0（应 strip）", got.Provider)
	}
	if got.UserID != "u1" || got.ScopeID != "s1" {
		t.Errorf("UserID/ScopeID = %q/%q, want u1/s1", got.UserID, got.ScopeID)
	}
	if len(got.AllowedPlugins) != 2 || got.AllowedPlugins[0] != "p1" {
		t.Errorf("AllowedPlugins = %v, want [p1 p2]", got.AllowedPlugins)
	}
	if got.Mem0["api_key"] != "k" {
		t.Errorf("Mem0.api_key = %v, want k", got.Mem0["api_key"])
	}
}

// TestGetExternalMemoryConfig_nil走loadConfig 对齐 Python config=None → _load_config()
func TestGetExternalMemoryConfig_nil走loadConfig(t *testing.T) {
	injectTestConfigLoader(t, "memory:\n  external:\n    provider: mem0\n")
	ClearConfigCache()
	got := GetExternalMemoryConfig(nil)
	if got.Provider != "mem0" {
		t.Errorf("nil cfg Provider = %q, want mem0", got.Provider)
	}
	ClearConfigCache()
}

// TestIsExternalMemoryEnabled_组合矩阵 对齐 L73-77：engine 放行 && provider 非空
// 回归偏差 #5：此前 Go 只查 engine，provider 清空后 rail 不注销
func TestIsExternalMemoryEnabled_组合矩阵(t *testing.T) {
	cases := []struct {
		name     string
		engine   string
		provider string
		want     bool
	}{
		{"external+provider", "external", "mem0", true},
		{"both+provider", "both", "mem0", true},
		{"both无provider", "both", "", false}, // ★ 修正点：provider 空则禁用
		{"builtin+provider", "builtin", "mem0", false},
		{"none", "none", "mem0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ext := map[string]any{}
			if c.provider != "" {
				ext["provider"] = c.provider
			}
			cfg := map[string]any{"memory": map[string]any{
				"engine": c.engine, "external": ext,
			}}
			if got := IsExternalMemoryEnabled(cfg); got != c.want {
				t.Errorf("engine=%s provider=%q → %v, want %v", c.engine, c.provider, got, c.want)
			}
		})
	}
}

// TestResolveLtmDir_创建目录 对齐 Python _resolve_ltm_dir (L80-84)：mkdir parents
func TestResolveLtmDir_创建目录(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())

	dir, err := ResolveLtmDir()
	if err != nil {
		t.Fatalf("ResolveLtmDir 失败: %v", err)
	}
	want := filepath.Join(tmpDir, "memory", "ltm")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	// 目录应已创建
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("ltm 目录未创建: %v", err)
	}
	// 幂等：再次调用不报错
	if _, err := ResolveLtmDir(); err != nil {
		t.Errorf("二次调用失败: %v", err)
	}
}

// TestBuildOpenJiuwenProviderConfig_默认值 对齐 L87-126
// 回归偏差 #6：kv 默认 shelve（jiuwenswarm builder 基线），非 memory
func TestBuildOpenJiuwenProviderConfig_默认值(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())
	t.Setenv("EMBED_MODEL", "env-model")
	t.Setenv("EMBED_BASE_URL", "http://env-base")
	t.Setenv("EMBED_API_KEY", "env-key")

	extCfg := ExternalMemoryConfig{OpenJiuwen: map[string]any{}}
	got, err := BuildOpenJiuwenProviderConfig(extCfg, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	ltmDir := filepath.Join(tmpDir, "memory", "ltm")

	kv := got["kv"].(map[string]any)
	if kv["backend"] != "shelve" {
		t.Errorf("kv.backend = %v, want shelve（回归偏差 #6）", kv["backend"])
	}
	if kv["path"] != filepath.Join(ltmDir, "kv") {
		t.Errorf("kv.path = %v, want %q", kv["path"], filepath.Join(ltmDir, "kv"))
	}
	vector := got["vector"].(map[string]any)
	if vector["backend"] != "chroma" || vector["persist_directory"] != filepath.Join(ltmDir, "chroma") {
		t.Errorf("vector = %v", vector)
	}
	db := got["db"].(map[string]any)
	if db["backend"] != "sqlite" || db["path"] != filepath.Join(ltmDir, "ltm.db") {
		t.Errorf("db = %v", db)
	}
	embedding := got["embedding"].(map[string]any)
	if embedding["model_name"] != "env-model" || embedding["base_url"] != "http://env-base" {
		t.Errorf("embedding = %v（应读 EMBED_* env 回退）", embedding)
	}
}

// TestBuildOpenJiuwenProviderConfig_配置覆盖 对齐 kv_type/vector_type 等覆盖
func TestBuildOpenJiuwenProviderConfig_配置覆盖(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())

	extCfg := ExternalMemoryConfig{OpenJiuwen: map[string]any{
		"kv_type":     "sqlite",
		"kv_path":     "/custom/kv",
		"vector_type": "milvus",
		"db_path":     "/custom/db",
	}}
	got, err := BuildOpenJiuwenProviderConfig(extCfg, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	kv := got["kv"].(map[string]any)
	if kv["backend"] != "sqlite" || kv["path"] != "/custom/kv" {
		t.Errorf("kv = %v", kv)
	}
	if got["vector"].(map[string]any)["backend"] != "milvus" {
		t.Errorf("vector.backend 应支持覆盖")
	}
	if got["db"].(map[string]any)["path"] != "/custom/db" {
		t.Errorf("db.path 应支持覆盖")
	}
}

// TestBuildOpenJiuwenProviderConfig_embed配置段覆盖env 对齐：get_embed_config 优先于 EMBED_* env
// 回归偏差 #7：键名 embed_base_url（包内 GetEmbedConfig），非 embed_api_base
func TestBuildOpenJiuwenProviderConfig_embed配置段覆盖env(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())
	t.Setenv("EMBED_MODEL", "env-model-should-lose")

	// 注入 config.yaml embed 段（GetEmbedConfig 走包内 loadConfig）
	injectTestConfigLoader(t, `
embed:
  embed_api_key: cfg-key
  embed_base_url: http://cfg-base
  embed_model: cfg-model
`)
	ClearConfigCache()
	defer ClearConfigCache()

	got, err := BuildOpenJiuwenProviderConfig(ExternalMemoryConfig{}, nil)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	embedding := got["embedding"].(map[string]any)
	if embedding["model_name"] != "cfg-model" {
		t.Errorf("model_name = %v, want cfg-model（config 优先于 env）", embedding["model_name"])
	}
	if embedding["base_url"] != "http://cfg-base" {
		t.Errorf("base_url = %v, want http://cfg-base（键名 embed_base_url，回归偏差 #7）", embedding["base_url"])
	}
	if embedding["api_key"] != "cfg-key" {
		t.Errorf("api_key = %v, want cfg-key", embedding["api_key"])
	}
}

// TestBuildOpenJiuwenProviderConfig_显式cfg读取embed段
// 调用方持有配置缓存时（cfg 非 nil）直接读 cfg["embed"]，不走包内 loadConfig
func TestBuildOpenJiuwenProviderConfig_显式cfg读取embed段(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())
	t.Setenv("EMBED_MODEL", "env-model-should-lose")

	cfg := map[string]any{"embed": map[string]any{
		"embed_model":    "cache-model",
		"embed_base_url": "http://cache-base",
		"embed_api_key":  "cache-key",
	}}
	got, err := BuildOpenJiuwenProviderConfig(ExternalMemoryConfig{}, cfg)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	embedding := got["embedding"].(map[string]any)
	if embedding["model_name"] != "cache-model" {
		t.Errorf("model_name = %v, want cache-model（cfg embed 段优先于 env）", embedding["model_name"])
	}
	if embedding["base_url"] != "http://cache-base" {
		t.Errorf("base_url = %v, want http://cache-base", embedding["base_url"])
	}
	if embedding["api_key"] != "cache-key" {
		t.Errorf("api_key = %v, want cache-key", embedding["api_key"])
	}
}

// TestBuildOpenJiuwenProviderConfig_显式cfg无embed段回落env
func TestBuildOpenJiuwenProviderConfig_显式cfg无embed段回落env(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	workspace.SetUserHome(workspace.UserHomeDir())
	defer workspace.SetUserHome(workspace.UserHomeDir())
	t.Setenv("EMBED_MODEL", "")
	t.Setenv("EMBED_BASE_URL", "")
	t.Setenv("EMBED_API_KEY", "")

	// 显式 cfg 无 embed 段 + env 全空 → 走 warn 分支，embedding 全空串
	got, err := BuildOpenJiuwenProviderConfig(ExternalMemoryConfig{}, map[string]any{})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	embedding := got["embedding"].(map[string]any)
	if embedding["model_name"] != "" || embedding["base_url"] != "" || embedding["api_key"] != "" {
		t.Errorf("embedding = %v, want 全空串（触发 warn 分支）", embedding)
	}
}
