package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestYAML 写入测试配置文件。
func writeTestYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	return path
}

// TestSetRoundTrip_保留注释 M-05 核心验证：
// round-trip 写回后文件中的注释（head/line comment）不丢失。
// Python 等价物：ruamel load_yaml_round_trip + dump_yaml_round_trip 保注释。
func TestSetRoundTrip_保留注释(t *testing.T) {
	path := writeTestYAML(t, `# 顶部注释
memory:
  # 段内注释
  forbidden_memory_definition:
    enabled: false # 行内注释
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("memory.forbidden_memory_definition.enabled", true); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "# 顶部注释") {
		t.Errorf("顶部注释丢失: %s", got)
	}
	if !strings.Contains(got, "# 段内注释") {
		t.Errorf("段内注释丢失: %s", got)
	}
	if !strings.Contains(got, "# 行内注释") {
		t.Errorf("行内注释丢失: %s", got)
	}
	if !strings.Contains(got, "enabled: true") {
		t.Errorf("值未更新: %s", got)
	}
}

// TestSetRoundTrip_多级路径自动创建 验证中间节点不存在时自动创建 mapping。
func TestSetRoundTrip_多级路径自动创建(t *testing.T) {
	path := writeTestYAML(t, `logging:
  level: info
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("modes.agent.code.memory.enabled", true); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	// Load 验证值写入
	data, err := cfg.Load()
	if err != nil {
		t.Fatal(err)
	}
	modes, ok := data["modes"].(map[string]any)
	if !ok {
		t.Fatalf("modes 应为 map: %v", data)
	}
	agent, ok := modes["agent"].(map[string]any)
	if !ok {
		t.Fatalf("modes.agent 应为 map: %v", modes)
	}
	code, ok := agent["code"].(map[string]any)
	if !ok {
		t.Fatalf("modes.agent.code 应为 map: %v", agent)
	}
	mem, ok := code["memory"].(map[string]any)
	if !ok {
		t.Fatalf("modes.agent.code.memory 应为 map: %v", code)
	}
	if mem["enabled"] != true {
		t.Errorf("enabled 应为 true: %v", mem)
	}
	// 原有 logging.level 保留
	if data["logging"].(map[string]any)["level"] != "info" {
		t.Errorf("logging.level 应保留: %v", data)
	}
}

// TestSetRoundTrip_文件不存在自动创建 T-11 延续行为验证。
// Python ruamel 在文件不存在时抛 FileNotFoundError；Go 有意增强为自动创建。
func TestSetRoundTrip_文件不存在自动创建(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml") // sub 目录也不存在
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("memory.enabled", true); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("配置文件应被创建: %v", err)
	}
	data, err := cfg.Load()
	if err != nil {
		t.Fatal(err)
	}
	if data["memory"].(map[string]any)["enabled"] != true {
		t.Errorf("memory.enabled 应为 true: %v", data)
	}
}

// TestSetRoundTrip_内存缓存同步 验证写回后 Get 能读到新值。
func TestSetRoundTrip_内存缓存同步(t *testing.T) {
	path := writeTestYAML(t, `a:
  b: 1
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Load(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("a.b", 42); err != nil {
		t.Fatal(err)
	}
	if v := cfg.Get("a.b"); v != 42 {
		t.Errorf("Get(a.b) 应返回 42，实际: %v", v)
	}
}

// TestSetRoundTrip_中间节点非map覆盖 验证中间节点是标量时覆盖为 mapping。
func TestSetRoundTrip_中间节点非map覆盖(t *testing.T) {
	path := writeTestYAML(t, `a: scalar
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("a.b.c", true); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	v := cfg.Get("a.b.c")
	if v != true {
		t.Errorf("a.b.c 应为 true: %v", v)
	}
}

// TestSetRoundTrip_覆盖已有值 验证已有键值被新值替换。
func TestSetRoundTrip_覆盖已有值(t *testing.T) {
	path := writeTestYAML(t, `memory:
  forbidden_memory_definition:
    enabled: true
    description:
      zh: 原描述
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("memory.forbidden_memory_definition.enabled", false); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	if v := cfg.Get("memory.forbidden_memory_definition.enabled"); v != false {
		t.Errorf("enabled 应为 false: %v", v)
	}
	// 同级其他键保留
	if v := cfg.Get("memory.forbidden_memory_definition.description.zh"); v != "原描述" {
		t.Errorf("description.zh 应保留: %v", v)
	}
}

// TestSetRoundTrip_空文件 验证空文件/空白文件场景。
func TestSetRoundTrip_空文件(t *testing.T) {
	path := writeTestYAML(t, "\n\n")
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("x.y", "z"); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	if v := cfg.Get("x.y"); v != "z" {
		t.Errorf("x.y 应为 z: %v", v)
	}
}

// TestSetRoundTrip_写回列表值 验证 slice 值经 Node Encode 写入。
func TestSetRoundTrip_写回列表值(t *testing.T) {
	path := writeTestYAML(t, `models:
  defaults: []
`)
	cfg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetRoundTrip("models.defaults", []any{map[string]any{"name": "m1", "provider": "p1"}}); err != nil {
		t.Fatalf("SetRoundTrip 失败: %v", err)
	}
	out, _ := os.ReadFile(path)
	got := string(out)
	if !strings.Contains(got, "name: m1") {
		t.Errorf("列表值未写入: %s", got)
	}
}
