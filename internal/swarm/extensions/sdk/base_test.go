package sdk

import (
	"context"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
)

// TestBaseExtensionImpl_Metadata_缓存 测试元数据缓存机制
func TestBaseExtensionImpl_Metadata_缓存(t *testing.T) {
	impl := &BaseExtensionImpl{}
	// 无 metadata 缓存时 Metadata() 返回 nil
	m := impl.Metadata()
	if m != nil {
		t.Errorf("Metadata() = %v, want nil (no cache)", m)
	}
}

// TestBaseExtensionImpl_SetExtensionDir 测试 SetExtensionDir 清除缓存
func TestBaseExtensionImpl_SetExtensionDir(t *testing.T) {
	impl := &BaseExtensionImpl{}

	// 设置 metadata 缓存
	impl.metadataCache = &extensions.ExtensionMetadata{ID: "cached-ext"}
	m := impl.Metadata()
	if m.ID != "cached-ext" {
		t.Errorf("Metadata() = %q, want %q", m.ID, "cached-ext")
	}

	// SetExtensionDir 应清除缓存
	impl.SetExtensionDir("/test/ext/dir")
	m2 := impl.Metadata()
	if m2 != nil {
		t.Errorf("Metadata() after SetExtensionDir = %v, want nil (cache cleared)", m2)
	}
	if impl.extensionDir == nil || *impl.extensionDir != "/test/ext/dir" {
		t.Errorf("extensionDir = %v, want %q", impl.extensionDir, "/test/ext/dir")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_文件不存在 测试 YAML 文件不存在时返回错误
func TestBaseExtensionImpl_LoadMetadataFromYAML_文件不存在(t *testing.T) {
	impl := &BaseExtensionImpl{}
	impl.SetExtensionDir(t.TempDir()) // 空目录，无 extension.yaml

	_, err := impl.LoadMetadataFromYAML()
	if err == nil {
		t.Error("LoadMetadataFromYAML() should return error when file does not exist")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_成功 测试从 YAML 加载元数据
func TestBaseExtensionImpl_LoadMetadataFromYAML_成功(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入 extension.yaml
	yamlContent := `id: test-ext
name: 测试扩展
version: 1.0.0
description: 测试描述
author: test-author
min_jiuwenswarm_version: "0.1.0"
dependencies:
  core: ">=0.1.0"
`
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	if m.ID != "test-ext" {
		t.Errorf("ID = %q, want %q", m.ID, "test-ext")
	}
	if m.Name != "测试扩展" {
		t.Errorf("Name = %q, want %q", m.Name, "测试扩展")
	}
	if m.Dependencies["core"] != ">=0.1.0" {
		t.Errorf("Dependencies[core] = %q, want %q", m.Dependencies["core"], ">=0.1.0")
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_不存在 测试 config.yaml 不存在返回 nil
func TestBaseExtensionImpl_LoadConfigFromYAML_不存在(t *testing.T) {
	impl := &BaseExtensionImpl{}
	impl.SetExtensionDir(t.TempDir())

	cfg := impl.LoadConfigFromYAML()
	if cfg != nil {
		t.Errorf("LoadConfigFromYAML() = %v, want nil when file not exists", cfg)
	}
}

// TestManifestFilename 测试常量对齐
func TestManifestFilename(t *testing.T) {
	if ManifestFilename != "extension.yaml" {
		t.Errorf("ManifestFilename = %q, want %q", ManifestFilename, "extension.yaml")
	}
}

// TestBaseExtension_接口契约 测试 BaseExtension 接口方法签名
func TestBaseExtension_接口契约(t *testing.T) {
	// 验证 BaseExtension 接口包含所需方法
	var _ BaseExtension = (*mockExtension)(nil)
}

// mockExtension 用于测试 BaseExtension 接口实现的 mock
type mockExtension struct {
	BaseExtensionImpl
}

func (e *mockExtension) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

func (e *mockExtension) Shutdown(ctx context.Context) error {
	return nil
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_未设置目录 测试 extensionDir 为 nil 时返回错误
func TestBaseExtensionImpl_LoadMetadataFromYAML_未设置目录(t *testing.T) {
	impl := &BaseExtensionImpl{}
	// 不设置 extensionDir
	_, err := impl.LoadMetadataFromYAML()
	if err == nil {
		t.Error("LoadMetadataFromYAML() should return error when extensionDir is nil")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_YAML格式错误 测试 YAML 格式无效时返回错误
func TestBaseExtensionImpl_LoadMetadataFromYAML_YAML格式错误(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入无效 YAML
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte("invalid: [yaml: content"), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	_, err := impl.LoadMetadataFromYAML()
	if err == nil {
		t.Error("LoadMetadataFromYAML() should return error for invalid YAML")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_ConfigSchema 测试 ConfigSchema 分支解析
func TestBaseExtensionImpl_LoadMetadataFromYAML_ConfigSchema(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入带 config_schema 的 extension.yaml
	yamlContent := `id: ext-with-schema
name: 带配置扩展
version: "2.0.0"
description: 有配置模式的扩展
author: schema-author
min_jiuwenswarm_version: "0.1.0"
config_schema:
  type: object
  properties:
    timeout:
      type: integer
`
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	if m.ConfigSchema == nil {
		t.Error("ConfigSchema should not be nil when present in YAML")
	}
	if m.ConfigSchema["type"] != "object" {
		t.Errorf("ConfigSchema[type] = %v, want %q", m.ConfigSchema["type"], "object")
	}
	props, ok := m.ConfigSchema["properties"].(map[string]any)
	if !ok {
		t.Errorf("ConfigSchema[properties] type = %T, want map[string]any", m.ConfigSchema["properties"])
	} else {
		timeoutProps, ok := props["timeout"].(map[string]any)
		if !ok {
			t.Errorf("properties[timeout] type = %T, want map[string]any", props["timeout"])
		} else if timeoutProps["type"] != "integer" {
			t.Errorf("timeout.type = %v, want %q", timeoutProps["type"], "integer")
		}
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_无ConfigSchema 测试没有 config_schema 字段
func TestBaseExtensionImpl_LoadMetadataFromYAML_无ConfigSchema(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入不含 config_schema 的 extension.yaml
	yamlContent := `id: ext-no-schema
name: 无配置扩展
version: "1.0.0"
description: 无配置模式
author: no-schema-author
`
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	if m.ConfigSchema != nil {
		t.Errorf("ConfigSchema should be nil when not in YAML, got %v", m.ConfigSchema)
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_缓存 测试 LoadMetadataFromYAML 结果存入缓存
func TestBaseExtensionImpl_LoadMetadataFromYAML_缓存(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	yamlContent := `id: cached-ext
name: 缓存扩展
version: "1.0.0"
`
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	// 缓存应被设置
	cached := impl.Metadata()
	if cached != m {
		t.Error("Metadata() cache != LoadMetadataFromYAML() result")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_缺字段 测试 YAML 缺少部分字段
func TestBaseExtensionImpl_LoadMetadataFromYAML_缺字段(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 只写 id，其他字段缺失
	yamlContent := `id: minimal-ext
`
	yamlPath := dir + "/extension.yaml"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	if m.ID != "minimal-ext" {
		t.Errorf("ID = %q, want %q", m.ID, "minimal-ext")
	}
	// 缺失字段应为空字符串（strVal 返回 ""）
	if m.Name != "" {
		t.Errorf("Name = %q, want empty string for missing field", m.Name)
	}
	if m.Version != "" {
		t.Errorf("Version = %q, want empty string for missing field", m.Version)
	}
	if m.Dependencies != nil {
		t.Errorf("Dependencies = %v, want nil for missing field", m.Dependencies)
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_成功 测试从 config.yaml 成功加载配置
func TestBaseExtensionImpl_LoadConfigFromYAML_成功(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入 config.yaml
	configContent := `timeout: 30
retry_count: 3
debug: true
`
	configPath := dir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	impl.SetExtensionDir(dir)
	cfg := impl.LoadConfigFromYAML()
	if cfg == nil {
		t.Fatal("LoadConfigFromYAML() = nil, want non-nil config")
	}
	if cfg["timeout"] != 30 {
		t.Errorf("cfg[timeout] = %v, want 30", cfg["timeout"])
	}
	if cfg["retry_count"] != 3 {
		t.Errorf("cfg[retry_count] = %v, want 3", cfg["retry_count"])
	}
	if cfg["debug"] != true {
		t.Errorf("cfg[debug] = %v, want true", cfg["debug"])
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_缓存返回 测试缓存后重复调用返回缓存
func TestBaseExtensionImpl_LoadConfigFromYAML_缓存返回(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	configContent := `key: value
`
	configPath := dir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	impl.SetExtensionDir(dir)
	_ = impl.LoadConfigFromYAML()
	// 第二次调用应返回同一缓存指针
	cfg2 := impl.LoadConfigFromYAML()
	if cfg2["key"] != "value" {
		t.Errorf("cached cfg[key] = %v, want %q", cfg2["key"], "value")
	}
	// 验证 configCache 被填充
	if impl.configCache == nil {
		t.Error("configCache should be non-nil after LoadConfigFromYAML")
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_未设置目录 测试 extensionDir 为 nil 时返回 nil
func TestBaseExtensionImpl_LoadConfigFromYAML_未设置目录(t *testing.T) {
	impl := &BaseExtensionImpl{}
	// 不设置 extensionDir，configCache 也为 nil
	cfg := impl.LoadConfigFromYAML()
	if cfg != nil {
		t.Errorf("LoadConfigFromYAML() = %v, want nil when extensionDir is nil", cfg)
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_YAML格式错误 测试 config.yaml 格式无效时返回 nil
func TestBaseExtensionImpl_LoadConfigFromYAML_YAML格式错误(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入无效 YAML
	configPath := dir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte("invalid: [yaml: broken"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	impl.SetExtensionDir(dir)
	cfg := impl.LoadConfigFromYAML()
	if cfg != nil {
		t.Errorf("LoadConfigFromYAML() = %v, want nil for invalid YAML", cfg)
	}
}
