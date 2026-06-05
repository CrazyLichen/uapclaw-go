package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNew_默认路径(t *testing.T) {
	cfg, err := New("")
	if err != nil {
		t.Fatalf("New('') 失败: %v", err)
	}
	if cfg.Path() == "" {
		t.Error("期望非空路径")
	}
}

func TestNew_指定路径(t *testing.T) {
	cfg, err := New("/tmp/test_config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if cfg.Path() != "/tmp/test_config.yaml" {
		t.Errorf("期望 /tmp/test_config.yaml，实际 %s", cfg.Path())
	}
}

func TestNew_环境变量覆盖路径(t *testing.T) {
	os.Setenv(EnvConfigDir, "/custom/config/dir")
	defer os.Unsetenv(EnvConfigDir)

	cfg, err := New("")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	expected := filepath.Join("/custom/config/dir", DefaultConfigFile)
	if cfg.Path() != expected {
		t.Errorf("期望 %s，实际 %s", expected, cfg.Path())
	}
}

func TestLoad_正常读取(t *testing.T) {
	cfg, err := New("testdata/config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	// 验证顶层键
	if _, ok := data["server"]; !ok {
		t.Error("期望包含 server 键")
	}
	if _, ok := data["logging"]; !ok {
		t.Error("期望包含 logging 键")
	}
	if _, ok := data["workspace"]; !ok {
		t.Error("期望包含 workspace 键")
	}
}

func TestLoad_文件不存在(t *testing.T) {
	cfg, err := New("testdata/nonexistent.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("文件不存在时应返回空配置，不应报错: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("期望空配置，实际 %v", data)
	}
}

func TestLoad_环境变量解析(t *testing.T) {
	os.Setenv("DB_HOST", "db.example.com")
	defer os.Unsetenv("DB_HOST")
	os.Setenv("DB_PASSWORD", "secret123")
	defer os.Unsetenv("DB_PASSWORD")
	os.Setenv("MY_API_KEY", "sk-abc")
	defer os.Unsetenv("MY_API_KEY")
	os.Setenv("API_HOST", "api.example.com")
	defer os.Unsetenv("API_HOST")

	cfg, err := New("testdata/envvar_config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	// 验证环境变量替换
	db := data["database"].(map[string]any)
	if db["host"] != "db.example.com" {
		t.Errorf("期望 db.example.com，实际 %v", db["host"])
	}
	if db["password"] != "secret123" {
		t.Errorf("期望 secret123，实际 %v", db["password"])
	}

	services := data["services"].(map[string]any)
	api := services["api"].(map[string]any)
	if api["url"] != "http://api.example.com:8080/v1" {
		t.Errorf("期望 http://api.example.com:8080/v1，实际 %v", api["url"])
	}
}

func TestLoad_环境变量默认值(t *testing.T) {
	// 不设置 DB_HOST，应使用默认值 localhost
	// 不设置 API_HOST，应使用空字符串

	cfg, err := New("testdata/envvar_config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	db := data["database"].(map[string]any)
	if db["host"] != "localhost" {
		t.Errorf("期望 localhost（默认值），实际 %v", db["host"])
	}
}

func TestLoad_DecryptFunc集成(t *testing.T) {
	os.Setenv("MY_API_KEY", "encrypted_sk")
	defer os.Unsetenv("MY_API_KEY")

	decryptFn := func(envName, value string) (string, bool) {
		return "decrypted_" + value, true
	}

	cfg, err := New("testdata/envvar_config.yaml", WithDecrypt(decryptFn))
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	services := data["services"].(map[string]any)
	api := services["api"].(map[string]any)
	if api["api_key"] != "decrypted_encrypted_sk" {
		t.Errorf("期望解密后的值，实际 %v", api["api_key"])
	}
}

func TestLoad_NormalizeFunc(t *testing.T) {
	cfg, err := New("testdata/config.yaml", WithNormalize(func(m map[string]any) {
		// 后处理：添加一个标记
		m["_normalized"] = true
	}))
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data, err := cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	if data["_normalized"] != true {
		t.Error("期望后处理标记 _normalized = true")
	}
}

func TestRaw_不含环境变量(t *testing.T) {
	os.Setenv("DB_HOST", "db.example.com")
	defer os.Unsetenv("DB_HOST")

	cfg, err := New("testdata/envvar_config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	raw, err := cfg.Raw()
	if err != nil {
		t.Fatalf("Raw 失败: %v", err)
	}

	// Raw 应该保留原始的 ${VAR:-default} 字符串
	db := raw["database"].(map[string]any)
	if db["host"] != "${DB_HOST:-localhost}" {
		t.Errorf("期望 ${DB_HOST:-localhost}（原始值），实际 %v", db["host"])
	}
}

func TestSave_写入后读取(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	data := map[string]any{
		"server": map[string]any{
			"host": "localhost",
			"port": 9090,
		},
	}

	err = cfg.Save(data)
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 验证文件已创建
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Fatal("配置文件未创建")
	}

	// 重新读取验证
	cfg2, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	loaded, err := cfg2.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	server := loaded["server"].(map[string]any)
	if server["host"] != "localhost" {
		t.Errorf("期望 localhost，实际 %v", server["host"])
	}
	// yaml.v3 将数字解析为 int
	if server["port"] != 9090 {
		t.Errorf("期望 9090，实际 %v", server["port"])
	}
}

func TestSave_自动创建目录(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "deep", "nested", "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	err = cfg.Save(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Save 应自动创建目录: %v", err)
	}
}

func TestGet_点分隔路径(t *testing.T) {
	cfg, err := New("testdata/config.yaml")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	_, err = cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	tests := []struct {
		key  string
		want any
	}{
		{"server.agentserver.host", "0.0.0.0"},
		{"server.agentserver.port", 8765},
		{"logging.level", "info"},
		{"workspace.path", "~/.uapclaw"},
		{"server.gateway.host", "0.0.0.0"},
		{"server.gateway.port", 8766},
		{"nonexistent.key", nil},
		{"server.nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := cfg.Get(tt.key)
			if got != tt.want {
				t.Errorf("Get(%q) = %v，期望 %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestSet_更新值(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	// 先保存初始配置
	err = cfg.Save(map[string]any{
		"server": map[string]any{
			"host": "localhost",
			"port": 8080,
		},
	})
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 加载
	_, err = cfg.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	// 更新值
	err = cfg.Set("server.host", "0.0.0.0")
	if err != nil {
		t.Fatalf("Set 失败: %v", err)
	}

	// 验证内存中的值
	if cfg.Get("server.host") != "0.0.0.0" {
		t.Errorf("期望 0.0.0.0，实际 %v", cfg.Get("server.host"))
	}

	// 验证文件中的值
	cfg2, _ := New(cfgPath)
	cfg2.Load()
	if cfg2.Get("server.host") != "0.0.0.0" {
		t.Errorf("文件中期望 0.0.0.0，实际 %v", cfg2.Get("server.host"))
	}
}

func TestSet_自动创建中间节点(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	cfg.Save(map[string]any{})
	cfg.Load()

	err = cfg.Set("deep.nested.key", "value")
	if err != nil {
		t.Fatalf("Set 失败: %v", err)
	}

	if cfg.Get("deep.nested.key") != "value" {
		t.Errorf("期望 value，实际 %v", cfg.Get("deep.nested.key"))
	}
}

func TestReload(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	cfg.Save(map[string]any{"key": "old"})

	// 外部修改文件
	os.WriteFile(cfgPath, []byte("key: new\n"), 0o644)

	// 重新加载
	err = cfg.Reload()
	if err != nil {
		t.Fatalf("Reload 失败: %v", err)
	}

	if cfg.Get("key") != "new" {
		t.Errorf("期望 new，实际 %v", cfg.Get("key"))
	}
}

func TestDeepCopyMap(t *testing.T) {
	original := map[string]any{
		"key": "value",
		"nested": map[string]any{
			"inner": "data",
		},
		"slice": []any{1, 2, 3},
	}

	copied := deepCopyMap(original)

	// 修改拷贝不影响原始
	copied["key"] = "changed"
	copied["nested"].(map[string]any)["inner"] = "modified"

	if original["key"] != "value" {
		t.Error("深拷贝应该不影响原始值")
	}
	if original["nested"].(map[string]any)["inner"] != "data" {
		t.Error("深拷贝应该不影响原始嵌套值")
	}
}

// TestGet_数据未加载 测试 Load 未调用时 Get 返回 nil
func TestGet_数据未加载(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	// 不调用 Load，直接 Get
	val := cfg.Get("server.host")
	if val != nil {
		t.Errorf("未加载时 Get 应返回 nil，实际 %v", val)
	}
}

// TestSet_未加载时自动初始化 测试 data/raw 为 nil 时 Set 正常工作
func TestSet_未加载时自动初始化(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	// 不调用 Load，直接 Set
	err = cfg.Set("server.host", "0.0.0.0")
	if err != nil {
		t.Fatalf("Set 失败: %v", err)
	}

	if cfg.Get("server.host") != "0.0.0.0" {
		t.Errorf("期望 0.0.0.0，实际 %v", cfg.Get("server.host"))
	}
}

// TestSet_覆盖非map值 测试路径中间节点是标量时被覆盖为 map
func TestSet_覆盖非map值(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	// 先设置标量值
	cfg.Save(map[string]any{"server": "scalar_value"})
	cfg.Load()

	// 设置嵌套路径（中间节点是标量，应被覆盖为 map）
	err = cfg.Set("server.host", "0.0.0.0")
	if err != nil {
		t.Fatalf("Set 失败: %v", err)
	}

	if cfg.Get("server.host") != "0.0.0.0" {
		t.Errorf("期望 0.0.0.0，实际 %v", cfg.Get("server.host"))
	}
}

// TestRaw_文件不存在 测试 Raw 在文件不存在时返回空 map
func TestRaw_文件不存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nonexistent.yaml")

	cfg, err := New(cfgPath)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}

	raw, err := cfg.Raw()
	if err != nil {
		t.Fatalf("Raw 失败: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("文件不存在时应返回空 map，实际 %v", raw)
	}
}

// TestDeepCopySlice 测试 slice 深拷贝
func TestDeepCopySlice(t *testing.T) {
	original := []any{
		1,
		map[string]any{"key": "value"},
		[]any{2, 3},
	}

	copied := deepCopySlice(original)

	// 修改拷贝不影响原始
	copied[0] = 100
	copied[1].(map[string]any)["key"] = "modified"

	if original[0] != 1 {
		t.Error("深拷贝应不影响原始值")
	}
	if original[1].(map[string]any)["key"] != "value" {
		t.Error("深拷贝应不影响原始嵌套 map")
	}
}

// TestDeepCopyMap_nil 测试 nil map 深拷贝
func TestDeepCopyMap_nil(t *testing.T) {
	result := deepCopyMap(nil)
	if result != nil {
		t.Errorf("nil map 深拷贝应返回 nil，实际 %v", result)
	}
}

// TestDeepCopySlice_nil 测试 nil slice 深拷贝
func TestDeepCopySlice_nil(t *testing.T) {
	result := deepCopySlice(nil)
	if result != nil {
		t.Errorf("nil slice 深拷贝应返回 nil，实际 %v", result)
	}
}
