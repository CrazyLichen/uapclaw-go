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
