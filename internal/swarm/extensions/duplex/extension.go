package duplex

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ApplicationPluginServices 插件装配所需服务集。
// 对齐 extension.py:5 ApplicationPluginServices 的本插件子集。
type ApplicationPluginServices struct{}

// WebsocketRoute WS 代理路由声明。
// 对齐 extension.py:162-170 WebSocketRouteContribution。
type WebsocketRoute struct {
	// Path 代理路径
	Path string
	// AvailableWhenDisabled 关闭时仍可用
	AvailableWhenDisabled bool
}

// FrontendContribution 前端贡献声明。
// 对齐 extension.py:172-183 FrontendContribution。
type FrontendContribution struct {
	// ID 贡献标识
	ID string
	// NavKey 导航键
	NavKey string
	// Title 标题
	Title string
	// RenderMode 渲染模式
	RenderMode string
	// Component 组件名
	Component string
	// Position 位置
	Position int
}

// VideoDuplexApplicationPlugin video-duplex 应用插件。
// 对齐 extension.py:15-190 VideoDuplexApplicationPlugin。
// 偏差 O-2：Python 继承 ApplicationPluginExtension 基类（gateway.extensions.sdk），
// Go 用既有 swarm/extensions 插件机制，本结构体只承载本插件自己的方法。
type VideoDuplexApplicationPlugin struct {
	// searchManager 视频搜索与任务管理器（bind 时赋值）
	searchManager *VideoSearchManager
	// taskManager 持久任务服务（对齐 extension.py:40 _task_manager）
	taskManager any
	// agentClient AgentServer 客户端注入（网关装配层赋值）
	agentClient AgentClient
	// normalizeMediaAttachments 附件归一化服务注入（23.8 链）
	normalizeMediaAttachments func(params map[string]any, coreSessionID string) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PluginID 插件标识。
// 对齐 extension.py:16 plugin_id = "video-duplex"。
func (p *VideoDuplexApplicationPlugin) PluginID() string { return "video-duplex" }

// IsEnabled 返回插件启用状态。
// 对齐 extension.py:18 is_enabled → video_live.video_duplex_enabled（video_live.py:180-186）。
func (p *VideoDuplexApplicationPlugin) IsEnabled() bool { return VideoDuplexEnabled() }

// Shutdown 停机：关闭任务管理器。
// 对齐 extension.py:23-26 shutdown。
func (p *VideoDuplexApplicationPlugin) Shutdown() {
	if manager, ok := p.taskManager.(*VideoSearchManager); ok && manager != nil {
		if err := manager.Close(); err != nil {
			logger.Error(logComponent).Err(err).Msg("关闭任务管理器失败")
		}
	}
}

// InjectDependencies 装配层注入依赖。
// Python 侧无对应实现：services 鸭子注入的显式化。
func (p *VideoDuplexApplicationPlugin) InjectDependencies(client AgentClient, normalize func(map[string]any, string) error) {
	p.agentClient = client
	p.normalizeMediaAttachments = normalize
}

// BindWebChannel 向 WebChannel 注册本插件的 RPC 方法。
// 对齐 extension.py:28-160 bind_web_channel。
// WS 代理路由（:162-170 websocket_routes）由网关层消费 WebsocketRoutes 挂接。
func (p *VideoDuplexApplicationPlugin) BindWebChannel(ctx context.Context, channel DuplexChannel) {
	p.searchManager = registerVideoLiveHandler(channel, registerOptions{
		AgentClient:               p.agentClient,
		NormalizeMediaAttachments: p.normalizeMediaAttachments,
	})
	// 对齐 extension.py:40 self._task_manager = register_video_live_handler(...)
	p.taskManager = p.searchManager
}

// WebsocketRoutes 返回 WS 代理路由声明。
// 对齐 extension.py:162-170 websocket_routes。
func (p *VideoDuplexApplicationPlugin) WebsocketRoutes() []WebsocketRoute {
	return []WebsocketRoute{{
		Path:                  QwenOmniProxyPath,
		AvailableWhenDisabled: true,
	}}
}

// FrontendContributions 返回前端贡献声明（独立页签有意隐藏，配置移至设置页实验区）。
// 对齐 extension.py:172-183 frontend_contributions。
func (p *VideoDuplexApplicationPlugin) FrontendContributions() []FrontendContribution {
	return []FrontendContribution{{
		ID:         "video-live",
		NavKey:     "app:video-duplex",
		Title:      "Full-duplex",
		RenderMode: "none",
		Component:  "video-duplex",
		Position:   75,
	}}
}

// RegisterExtensions 创建插件并注册到注册表，返回插件实例。
// 对齐 extension.py:186-190 register_extensions。
func RegisterExtensions(registry interface {
	RegisterApplicationPlugin(plugin any)
}) *VideoDuplexApplicationPlugin {
	extension := &VideoDuplexApplicationPlugin{}
	registry.RegisterApplicationPlugin(extension)
	return extension
}
