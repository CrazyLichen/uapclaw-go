package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetEndpointProfileOverrides_读顶层键 host 必须小写、空值丢弃。
// Python: gateway/common/config.py:308-327
func TestGetEndpointProfileOverrides_读顶层键(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "endpoint_profile_overrides:\n" +
		"  My-Gateway.Internal: dashscope\n" +
		"  other.host: \"\"\n" +
		"  third.host: vllm\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)

	got := GetEndpointProfileOverrides()
	if len(got) != 2 {
		t.Fatalf("条目数 = %d, 期望 2（空 profile 应丢弃）：%v", len(got), got)
	}
	if got["my-gateway.internal"] != "dashscope" {
		t.Errorf("host 未小写化：%v", got)
	}
	if got["third.host"] != "vllm" {
		t.Errorf("third.host = %q", got["third.host"])
	}
	if _, ok := got["other.host"]; ok {
		t.Error("空 profile 条目应被丢弃")
	}
}

// TestGetEndpointProfileOverrides_缺失返回空表 键不存在时返回空 map 而非 nil。
func TestGetEndpointProfileOverrides_缺失返回空表(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("models: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)

	got := GetEndpointProfileOverrides()
	if got == nil {
		t.Fatal("应返回空 map，不能是 nil")
	}
	if len(got) != 0 {
		t.Errorf("期望空表，得 %v", got)
	}
}

// TestGetEndpointProfileOverrides_非映射值 顶层键不是映射时返回空表。
func TestGetEndpointProfileOverrides_非映射值(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("endpoint_profile_overrides: not-a-map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_CONFIG_YAML", path)

	got := GetEndpointProfileOverrides()
	if len(got) != 0 {
		t.Errorf("非映射值应返回空表，得 %v", got)
	}
}
