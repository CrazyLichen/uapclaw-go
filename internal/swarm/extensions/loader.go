package extensions

import (
	"context"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionLoader 扩展加载器，对齐 Python ExtensionLoader
// ⤵️ 10.5.7 延后实现：Go 插件加载机制待定（Python 用 importlib 动态加载 extension.py）
// 当前仅定义接口占位，方法返回空结果或错误
type ExtensionLoader struct {
	registry *ExtensionRegistry
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionLoader 创建 ExtensionLoader，对齐 Python ExtensionLoader(registry)
func NewExtensionLoader(registry *ExtensionRegistry) *ExtensionLoader {
	return &ExtensionLoader{registry: registry}
}

// AddSearchPath 添加搜索路径，对齐 Python ExtensionLoader.add_search_path(path)
// ⤵️ 10.5.7 延后：当前空实现
func (l *ExtensionLoader) AddSearchPath(path string) {}

// DiscoverExtensionRoots 发现扩展目录，对齐 Python ExtensionLoader.discover_extension_roots()
// ⤵️ 10.5.7 延后：当前返回空列表
func (l *ExtensionLoader) DiscoverExtensionRoots() []string { return nil }

// LoadExtension 加载单个扩展，对齐 Python ExtensionLoader.load_extension(root)
// ⤵️ 10.5.7 延后：当前返回错误
func (l *ExtensionLoader) LoadExtension(ctx context.Context, root string) (any, error) {
	return nil, fmt.Errorf("ExtensionLoader 尚未实现，⤵️ 10.5.7（Go 插件加载机制待定）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
