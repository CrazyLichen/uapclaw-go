package extensions

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionManager 扩展生命周期管理器，对齐 Python ExtensionManager。
//
// 与 Python 差异：_setup_search_paths 的 config 读取上移到 cmd 启动层
// （解 config↔extensions 循环依赖），经 WithSearchPaths 注入。
type ExtensionManager struct {
	// registry 扩展注册中心
	registry *ExtensionRegistry
	// loader 扩展加载器
	loader *ExtensionLoader
	// loaded 已加载扩展列表（对齐 Python _loaded_extensions）
	loaded []LoadedExtension
}

// ManagerOption manager 构造选项
type ManagerOption func(*ExtensionManager)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// managerLogComponent 日志组件
	managerLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionManager 创建 ExtensionManager，对齐 Python ExtensionManager(registry)。
// searchPaths 由 cmd 层读 config 后注入（对齐 Python _setup_search_paths 的解环版）。
func NewExtensionManager(registry *ExtensionRegistry, opts ...ManagerOption) *ExtensionManager {
	m := &ExtensionManager{
		registry: registry,
		loader:   NewExtensionLoader(registry),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithSearchPaths 注入搜索路径（对齐 Python _setup_search_paths；cmd 层读 config 后传入）
func WithSearchPaths(paths []string) ManagerOption {
	return func(m *ExtensionManager) {
		for _, p := range paths {
			m.loader.AddSearchPath(p)
		}
	}
}

// WithPluginHostForManager 注入插件工厂（测试用；与 loader 共享同一工厂）。
// 命名说明：loader 包已有 WithPluginHost（LoaderOption），Go 包内不允许同名重载，
// manager 层用 ForManager 后缀区分。
func WithPluginHostForManager(h PluginHostFace) ManagerOption {
	return func(m *ExtensionManager) {
		m.loader.pluginHost = h
	}
}

// LoadAllExtensions 加载所有扩展，对齐 Python load_all_extensions：
// 发现 → 逐个加载；单个失败 error 日志后继续；成功记入 loaded。
func (m *ExtensionManager) LoadAllExtensions(ctx context.Context) error {
	roots := m.loader.DiscoverExtensionRoots()
	logger.Info(managerLogComponent).
		Str("event_type", "extension_manager_discover").
		Any("roots", roots).
		Msg("[ExtensionManager] 发现扩展路径")
	for _, root := range roots {
		loaded, err := m.loader.LoadExtension(ctx, root)
		if err != nil {
			logger.Error(managerLogComponent).
				Str("event_type", "extension_manager_load_failed").
				Str("root", root).
				Err(err).
				Msg("[ExtensionManager] 加载扩展失败")
			continue
		}
		for _, le := range loaded {
			logger.Info(managerLogComponent).
				Str("event_type", "extension_manager_loaded").
				Str("id", le.Metadata.ID).
				Str("root", le.Root).
				Msg("[ExtensionManager] 加载成功")
			m.loaded = append(m.loaded, le)
		}
	}
	return nil
}

// ShutdownAllExtensions 关闭所有扩展，对齐 Python shutdown_all_extensions：
// 逐个 shutdown（失败仅 warn 日志）+ Kill 子进程，最后清空列表。
//
// M-02：Go 侧一个插件进程的多个声明共享同一 PluginClient（Python 侧
// _loaded_extensions 存扩展实例、每实例独立 shutdown），N 声明会对同一
// 进程发 N 次 Shutdown RPC + N 次 Kill——首次成功后其余 RPC 报错产生误导性
// warn。故按 client 指针去重，每进程一次 Shutdown+Kill。
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error {
	seen := make(map[PluginClientFace]struct{}, len(m.loaded))
	for _, le := range m.loaded {
		if le.client == nil {
			continue
		}
		if _, dup := seen[le.client]; dup {
			continue
		}
		seen[le.client] = struct{}{}
		if err := le.client.Shutdown(ctx); err != nil {
			logger.Warn(managerLogComponent).
				Str("event_type", "extension_manager_shutdown_failed").
				Str("id", le.Metadata.ID).
				Err(err).
				Msg("[ExtensionManager] 关闭扩展失败")
		}
		le.client.Kill()
	}
	m.loaded = nil
	return nil
}

// ListExtensions 列出已加载扩展，对齐 Python list_extensions：
// [{id, name, version}] 同构。Python 按 hasattr(p, "metadata") 过滤，
// Go 的 LoadedExtension 结构恒有 Metadata（该过滤条件恒真），无需过滤（T-05）。
func (m *ExtensionManager) ListExtensions() []map[string]string {
	result := make([]map[string]string, 0, len(m.loaded))
	for _, le := range m.loaded {
		result = append(result, map[string]string{
			"id":      le.Metadata.ID,
			"name":    le.Metadata.Name,
			"version": le.Metadata.Version,
		})
	}
	return result
}
