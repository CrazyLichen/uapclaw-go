package extensions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoadedExtension 已加载的扩展记录（Manager 的 loaded 列表元素）
type LoadedExtension struct {
	// Metadata 扩展元数据
	Metadata plugin.Metadata
	// Root 扩展根目录
	Root string
	// client 插件客户端（shutdown 时 Shutdown + Kill 子进程）
	client PluginClientFace
}

// ExtensionLoader 扩展加载器，对齐 Python ExtensionLoader。
//
// 发现 extension_dirs 下的扩展根（extension.yaml 或 ext-plugin 存在），
// 通过 PluginHostFace 启动插件子进程，Register 声明后代为注册进 Registry：
//   - 事件订阅 → registry.Register（事件桥闭包）
//   - provides_transport 声明 → registry.RegisterAgentServerClient（GRPCPluginTransport 代理）
//   - provides_crypto 声明 → registry.RegisterCryptoUtility + 覆盖全局 provider（最后加载者胜）
//
// 与 Python 差异：Python 用 importlib 加载 extension.py 并调 register_extensions(registry)；
// Go 为静态编译，改用 go-plugin 子进程（协议见 plugin 包），
// 声明由插件 Register RPC 返回、宿主代为注册；依赖安装删除（Go 无运行时 pip 语义）。
type ExtensionLoader struct {
	// registry 扩展注册中心
	registry *ExtensionRegistry
	// searchPaths 搜索路径列表（对齐 Python _search_paths）
	searchPaths []string
	// pluginHost 插件启动工厂（生产 GoPluginHost，测试注入 fake）
	pluginHost PluginHostFace
}

// PluginHostFace loader 所需的插件工厂最小接口（打破对 plugin.PluginClient 具体类型的硬依赖，测试可注入）
type PluginHostFace interface {
	// Load 启动 root 下的插件并返回客户端
	Load(root string) (PluginClientFace, error)
}

// PluginClientFace loader 所需的插件客户端最小接口（测试可注入内存实现）
type PluginClientFace interface {
	// SetExtensionDir 设置扩展目录
	SetExtensionDir(ctx context.Context, path string) error
	// HandleEvent 事件桥
	HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error)
	// Shutdown 关闭
	Shutdown(ctx context.Context) error
	// Kill 杀掉插件子进程
	Kill()
	// Transport 返回 transport 代理（ProvidesTransport 声明的扩展）
	Transport() transport.AgentTransport
	// Crypto 返回 crypto 提供者代理（ProvidesCrypto 声明的扩展；不支持时返回 nil）
	Crypto() crypto.CryptoProvider
}

// RegisterCapable 支持声明获取的插件客户端（plugin.PluginClient 满足）
type RegisterCapable interface {
	// RegisterDeclarations 获取扩展声明列表
	RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error)
}

// LoaderOption loader 构造选项
type LoaderOption func(*ExtensionLoader)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ManifestFilename 扩展清单文件名，对齐 Python MANIFEST_FILENAME
	ManifestFilename = "extension.yaml"
	// EntryFilename 扩展入口二进制名（对齐 Python ENTRY_FILENAME 语义；值与 plugin.EntryFilename 一致）
	EntryFilename = plugin.EntryFilename
	// logComponent 日志组件（基础设施层）
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionLoader 创建 ExtensionLoader，对齐 Python ExtensionLoader(registry)。
// options 可注入插件工厂（测试）。
func NewExtensionLoader(registry *ExtensionRegistry, opts ...LoaderOption) *ExtensionLoader {
	l := &ExtensionLoader{registry: registry, pluginHost: &pluginHostAdapter{}}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// WithPluginHost 注入插件工厂（测试用；生产默认 GoPluginHost 适配）
func WithPluginHost(h PluginHostFace) LoaderOption {
	return func(l *ExtensionLoader) { l.pluginHost = h }
}

// AddSearchPath 添加搜索路径，对齐 Python add_search_path（目录存在才加入）
func (l *ExtensionLoader) AddSearchPath(path string) {
	if _, err := os.Stat(path); err == nil {
		l.searchPaths = append(l.searchPaths, path)
	}
}

// DiscoverExtensionRoots 发现扩展目录，对齐 Python discover_extension_roots：
// 遍历搜索路径一级子目录，extension.yaml 或 ext-plugin 存在即为扩展根。
func (l *ExtensionLoader) DiscoverExtensionRoots() []string {
	roots := make([]string, 0)
	logger.Info(logComponent).
		Str("event_type", "extension_loader_discover").
		Any("search_paths", l.searchPaths).
		Msg("[ExtensionLoader] 开始搜索扩展路径")
	for _, basePath := range l.searchPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			subdir := filepath.Join(basePath, entry.Name())
			if isExtensionRoot(subdir) {
				roots = append(roots, subdir)
			}
		}
	}
	return roots
}

// LoadExtension 加载单个扩展，对齐 Python load_extension(root)：
// manifest 解析（缺省空）→ 启动插件 → Register 声明 → 逐声明注册（SetExtensionDir + 事件 + transport）。
// 单个声明失败不影响其他声明（对齐 Python 异常仅日志）。
func (l *ExtensionLoader) LoadExtension(ctx context.Context, root string) ([]LoadedExtension, error) {
	manifest := loadManifestDict(root)
	_ = manifest // 依赖安装已删除（Go 静态编译），manifest 仅作发现与元数据参考

	pc, err := l.pluginHost.Load(root)
	if err != nil {
		return nil, fmt.Errorf("加载扩展失败 %s: %w", root, err)
	}

	// Register 声明（PluginClientFace 不含 Register——由 RegisterCapable 具体类型提供）
	decls, err := l.registerDecls(pc, root)
	if err != nil {
		if killer, ok := pc.(interface{ Kill() }); ok {
			killer.Kill()
		}
		return nil, err
	}

	return l.loadAndRegister(ctx, root, pc, decls)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isExtensionRoot 判断是否扩展根（manifest 或入口存在），对齐 Python _is_extension_root
func isExtensionRoot(path string) bool {
	return findManifest(path) != "" || hasEntry(path)
}

// findManifest 查找 extension.yaml，对齐 Python _find_manifest
func findManifest(root string) string {
	p := filepath.Join(root, ManifestFilename)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// hasEntry 查找 ext-plugin 入口
func hasEntry(root string) bool {
	_, err := os.Stat(filepath.Join(root, EntryFilename))
	return err == nil
}

// loadManifestDict 解析 manifest（缺省空 map），对齐 Python _load_manifest_dict
func loadManifestDict(root string) map[string]any {
	manifestPath := findManifest(root)
	if manifestPath == "" {
		return map[string]any{}
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return map[string]any{}
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return map[string]any{}
	}
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

// registerDecls 获取插件声明（PluginClientFace 不含 Register；由 RegisterCapable 提供具体实现）
func (l *ExtensionLoader) registerDecls(pc PluginClientFace, root string) ([]plugin.Declaration, error) {
	if rpc, ok := pc.(RegisterCapable); ok {
		return rpc.RegisterDeclarations(context.Background())
	}
	return nil, fmt.Errorf("插件客户端不支持 Register: %T", pc)
}

// loadAndRegister 编排注册：SetExtensionDir + 事件桥 + transport 注册
func (l *ExtensionLoader) loadAndRegister(ctx context.Context, root string, pc PluginClientFace, decls []plugin.Declaration) ([]LoadedExtension, error) {
	loaded := make([]LoadedExtension, 0, len(decls))
	for _, decl := range decls {
		if err := pc.SetExtensionDir(ctx, root); err != nil {
			logger.Error(logComponent).
				Str("event_type", "extension_set_dir_failed").
				Str("root", root).
				Err(err).
				Msg("[ExtensionLoader] SetExtensionDir 失败")
			continue
		}
		for _, sub := range decl.EventSubs {
			l.registry.Register(sub.Event, l.makeEventBridge(pc, sub.Event), int(sub.Priority))
		}
		if decl.ProvidesTransport {
			t := pc.Transport()
			if t != nil {
				l.registry.RegisterAgentServerClient(&pluginTransportExtension{client: pc, metadata: decl.Metadata})
			}
		}
		if decl.ProvidesCrypto {
			ext := &pluginCryptoExtension{client: pc, metadata: decl.Metadata}
			// RegisterCryptoUtility 内部双写全局（registry 与 common/crypto 两源一致）；
			// 此处显式覆盖再强调"最后加载者胜"编排语义（对齐 Python loader 顺序覆盖）
			l.registry.RegisterCryptoUtility(ext)
			if p := ext.GetCrypto(); p != nil {
				crypto.SetCryptoProvider(p)
				logger.Info(logComponent).
					Str("event_type", "extension_crypto_registered").
					Str("ext_id", decl.Metadata.ID).
					Msg("[ExtensionLoader] crypto 插件已接管全局加密提供者")
			}
		}
		loaded = append(loaded, LoadedExtension{Metadata: decl.Metadata, Root: root, client: pc})
	}
	return loaded, nil
}

// makeEventBridge 构造事件桥闭包：HandleEvent RPC → 结果原地写回宿主 data map
func (l *ExtensionLoader) makeEventBridge(pc PluginClientFace, event string) callback.CustomCallbackFunc {
	return func(ctx context.Context, data map[string]any) any {
		if data == nil {
			return nil
		}
		result, err := pc.HandleEvent(ctx, event, data)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "extension_event_bridge_error").
				Str("event", event).
				Err(err).
				Msg("[ExtensionLoader] 事件桥调用失败")
			return nil
		}
		// range nil map 安全，无需前置 nil 检查（staticcheck S1031）
		for k, v := range result {
			data[k] = v
		}
		return nil
	}
}

// pluginHostAdapter 生产适配：go-plugin 子进程加载 → PluginClientFace
type pluginHostAdapter struct{}

func (a *pluginHostAdapter) Load(root string) (PluginClientFace, error) {
	pc, err := plugin.LoadPluginClient(root)
	if err != nil {
		return nil, err
	}
	return &pluginClientAdapter{pc: pc}, nil
}

// pluginClientAdapter plugin.PluginClient → PluginClientFace + RegisterCapable
type pluginClientAdapter struct {
	pc *plugin.PluginClient
}

func (a *pluginClientAdapter) SetExtensionDir(ctx context.Context, path string) error {
	return a.pc.SetExtensionDir(ctx, path)
}

func (a *pluginClientAdapter) HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error) {
	return a.pc.HandleEvent(ctx, event, data)
}

func (a *pluginClientAdapter) Shutdown(ctx context.Context) error {
	return a.pc.Shutdown(ctx)
}

func (a *pluginClientAdapter) Transport() transport.AgentTransport {
	return plugin.NewGRPCPluginTransport(a.pc)
}

func (a *pluginClientAdapter) Crypto() crypto.CryptoProvider {
	return plugin.NewGRPCPluginCryptoProvider(a.pc)
}

func (a *pluginClientAdapter) RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error) {
	return a.pc.Register(ctx)
}

func (a *pluginClientAdapter) Kill() {
	a.pc.Kill()
}

// pluginTransportExtension AgentServerClientExtension 的插件代理实现（registry.RegisterAgentServerClient 入参）
type pluginTransportExtension struct {
	// client 插件客户端
	client PluginClientFace
	// metadata 扩展元数据
	metadata plugin.Metadata
}

// Initialize 扩展初始化（对齐 Python 事实：host 从不调用，空实现）
func (e *pluginTransportExtension) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}

// Shutdown 代理到插件 Shutdown RPC
func (e *pluginTransportExtension) Shutdown(ctx context.Context) error {
	return e.client.Shutdown(ctx)
}

// Metadata 返回扩展元数据（Register 声明携带）
func (e *pluginTransportExtension) Metadata() *ExtensionMetadata {
	return &ExtensionMetadata{
		ID:                    e.metadata.ID,
		Name:                  e.metadata.Name,
		Version:               e.metadata.Version,
		Description:           e.metadata.Description,
		Author:                e.metadata.Author,
		MinJiuwenSwarmVersion: e.metadata.MinHostVersion,
		Dependencies:          e.metadata.Dependencies,
	}
}

// SetExtensionDir 代理到插件 SetExtensionDir RPC
func (e *pluginTransportExtension) SetExtensionDir(path string) {
	_ = e.client.SetExtensionDir(context.Background(), path)
}

// GetClient 返回 transport 代理（对齐 Python get_client()）
func (e *pluginTransportExtension) GetClient() transport.AgentTransport {
	return e.client.Transport()
}

// pluginCryptoExtension CryptoUtilityExtension 的插件代理实现（registry.RegisterCryptoUtility 入参）。
// 结构对齐 pluginTransportExtension 先例：生命周期代理到插件 RPC，GetCrypto 返回宿主侧代理。
type pluginCryptoExtension struct {
	// client 插件客户端
	client PluginClientFace
	// metadata 扩展元数据
	metadata plugin.Metadata
}

// Initialize 扩展初始化（对齐 Python 事实：host 从不调用，空实现）
func (e *pluginCryptoExtension) Initialize(ctx context.Context, config *ExtensionConfig) error {
	return nil
}

// Shutdown 代理到插件 Shutdown RPC
func (e *pluginCryptoExtension) Shutdown(ctx context.Context) error {
	return e.client.Shutdown(ctx)
}

// Metadata 返回扩展元数据（Register 声明携带）
func (e *pluginCryptoExtension) Metadata() *ExtensionMetadata {
	return &ExtensionMetadata{
		ID:                    e.metadata.ID,
		Name:                  e.metadata.Name,
		Version:               e.metadata.Version,
		Description:           e.metadata.Description,
		Author:                e.metadata.Author,
		MinJiuwenSwarmVersion: e.metadata.MinHostVersion,
		Dependencies:          e.metadata.Dependencies,
	}
}

// SetExtensionDir 代理到插件 SetExtensionDir RPC
func (e *pluginCryptoExtension) SetExtensionDir(path string) {
	_ = e.client.SetExtensionDir(context.Background(), path)
}

// GetCrypto 返回包着 PluginClient 的 GRPCPluginCryptoProvider（对齐 Python get_crypto()）
func (e *pluginCryptoExtension) GetCrypto() crypto.CryptoProvider {
	return e.client.Crypto()
}
