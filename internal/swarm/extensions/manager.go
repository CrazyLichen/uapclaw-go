package extensions

import (
	"context"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionManager 扩展生命周期管理器，对齐 Python ExtensionManager
// ⤵️ 10.5.8 延后实现：依赖 ExtensionLoader
// 当前仅定义接口占位，方法返回空结果或错误
type ExtensionManager struct {
	registry *ExtensionRegistry
	loader   *ExtensionLoader
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionManager 创建 ExtensionManager，对齐 Python ExtensionManager(registry)
func NewExtensionManager(registry *ExtensionRegistry) *ExtensionManager {
	return &ExtensionManager{
		registry: registry,
		loader:   NewExtensionLoader(registry),
	}
}

// LoadAllExtensions 加载所有扩展，对齐 Python ExtensionManager.load_all_extensions()
// ⤵️ 10.5.8 延后：当前返回错误
func (m *ExtensionManager) LoadAllExtensions(ctx context.Context) error {
	return fmt.Errorf("ExtensionManager.LoadAllExtensions 尚未实现，⤵️ 10.5.8（依赖 ExtensionLoader）")
}

// ShutdownAllExtensions 关闭所有扩展，对齐 Python ExtensionManager.shutdown_all_extensions()
// ⤵️ 10.5.8 延后：当前空实现
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error { return nil }

// ListExtensions 列出已加载扩展，对齐 Python ExtensionManager.list_extensions()
// ⤵️ 10.5.8 延后：当前返回空列表
func (m *ExtensionManager) ListExtensions() []map[string]string { return nil }
