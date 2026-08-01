package sdk

import (
	"context"
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseExtension 扩展抽象基类接口，对齐 Python BaseExtension ABC
type BaseExtension interface {
	// Initialize 扩展初始化入口，对齐 Python initialize(config: ExtensionConfig)
	Initialize(ctx context.Context, config *extensions.ExtensionConfig) error
	// Shutdown 扩展关闭释放资源，对齐 Python shutdown()
	Shutdown(ctx context.Context) error
	// Metadata 返回扩展元数据，对齐 Python metadata @property
	Metadata() *extensions.ExtensionMetadata
	// SetExtensionDir 设置扩展目录，对齐 Python set_extension_dir(path)
	SetExtensionDir(path string)
}

// BaseExtensionImpl BaseExtension 默认实现（嵌入使用），对齐 Python BaseExtension 类字段和方法
type BaseExtensionImpl struct {
	metadataCache *extensions.ExtensionMetadata
	extensionDir  *string
	configCache   map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

// ManifestFilename 扩展清单文件名，对齐 Python MANIFEST_FILENAME
const ManifestFilename = "extension.yaml"

// ──────────────────────────── 导出函数 ────────────────────────────

// Metadata 返回扩展元数据（有缓存则返回缓存，无缓存则返回 nil），
// 对齐 Python BaseExtension.metadata @property
func (b *BaseExtensionImpl) Metadata() *extensions.ExtensionMetadata {
	return b.metadataCache
}

// SetExtensionDir 设置扩展目录，同时清除 metadata 和 config 缓存，
// 对齐 Python BaseExtension.set_extension_dir(path)
func (b *BaseExtensionImpl) SetExtensionDir(path string) {
	b.extensionDir = &path
	b.metadataCache = nil
	b.configCache = nil
}

// LoadMetadataFromYAML 从扩展目录的 extension.yaml 加载元数据，
// 对齐 Python BaseExtension._load_metadata_from_yaml()
func (b *BaseExtensionImpl) LoadMetadataFromYAML() (*extensions.ExtensionMetadata, error) {
	if b.extensionDir == nil {
		return nil, fmt.Errorf("无法确定扩展目录，请在子类中设置目录或调用 SetExtensionDir")
	}

	yamlPath := *b.extensionDir + "/" + ManifestFilename
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("扩展元数据文件不存在（期望 %s）: %w", ManifestFilename, err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", ManifestFilename, err)
	}

	m := &extensions.ExtensionMetadata{
		ID:                    strVal(raw, "id"),
		Name:                  strVal(raw, "name"),
		Version:               strVal(raw, "version"),
		Description:           strVal(raw, "description"),
		Author:                strVal(raw, "author"),
		MinJiuwenSwarmVersion: strVal(raw, "min_jiuwenswarm_version"),
	}
	if deps, ok := raw["dependencies"].(map[string]any); ok {
		m.Dependencies = make(map[string]string, len(deps))
		for k, v := range deps {
			m.Dependencies[k] = fmt.Sprintf("%v", v)
		}
	}
	if cs, ok := raw["config_schema"]; ok {
		if csMap, ok := cs.(map[string]any); ok {
			m.ConfigSchema = csMap
		}
	}

	b.metadataCache = m
	return m, nil
}

// LoadConfigFromYAML 从扩展目录的 config.yaml 加载配置，
// 对齐 Python BaseExtension._load_config_from_yaml()
// 文件不存在时返回 nil
func (b *BaseExtensionImpl) LoadConfigFromYAML() map[string]any {
	if b.configCache != nil {
		return b.configCache
	}

	if b.extensionDir == nil {
		return nil
	}

	configPath := *b.extensionDir + "/config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	b.configCache = cfg
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 从 map 中安全提取字符串值
func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
