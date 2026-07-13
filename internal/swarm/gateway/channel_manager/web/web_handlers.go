package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RPCDispatcher RPC 方法注册与分发。
//
// 对齐 Python app_web_handlers.py 中的方法注册模式：
// 所有 RPC 方法通过 Register 注册，Dispatch 按 method 查找并调用。
type RPCDispatcher struct {
	// handlers 已注册的 RPC 方法处理函数
	handlers map[string]RPCHandlerFunc
	// mu 保护 handlers 的并发访问
	mu sync.RWMutex
}

// MessageHandler 消息处理器接口。
//
// 用于 connection.ack 等消息的推送，对齐 Python MessageHandler.publish_robot_messages。
type MessageHandler interface {
	// PublishRobotMessages 通过消息管道推送机器人消息
	PublishRobotMessages(ctx context.Context, msg *schema.Message) error
}

// WebHandlersBindParams WebHandlers 绑定参数。
//
// 对齐 Python WebHandlersBindParams (app_web_handlers.py L512-522)，
// 将 channel、agent_client、message_handler、channel_manager、回调等统一注入。
type WebHandlersBindParams struct {
	// Channel Web 通道实例
	Channel *WebChannel
	// AgentClient AgentServer 客户端，可为 nil
	AgentClient *routing.AgentClient
	// MessageHandler 消息处理器，用于 ack 优先路径，可为 nil
	// 对齐 Python: bind.message_handler，优先用 mh.publish_robot_messages 发 ack
	MessageHandler MessageHandler
	// ChannelManager 通道管理器，可为 nil
	ChannelManager *cm.ChannelManager
	// OnConfigSaved 配置保存回调
	OnConfigSaved OnConfigSavedFunc
	// CryptoProvider 加密/解密提供者，可为 nil
	CryptoProvider CryptoProvider
	// HeartbeatService 心跳服务，可为 nil
	// ⤵️ 11.15: GatewayHeartbeatService，注入后处理 heartbeat.get_conf / heartbeat.set_conf
	HeartbeatService any
	// CronController 定时任务控制器，可为 nil
	// ⤵️ 11.16: CronController，注入后处理 schedule.* 方法
	CronController any
	// UpdaterService 更新服务，可为 nil
	// ⤵️ 11.17: UpdaterService，注入后处理 update.* 方法
	UpdaterService any
}

// ──────────────────────────── 枚举 ────────────────────────────

// OnConfigSavedFunc 配置保存回调函数类型。
// 对齐 Python: _on_config_saved(updated_env_keys, env_updates=..., config_payload=...)。
//
// 参数说明：
//   - updatedKeys: 变更的环境变量键集合
//   - envUpdates: 增量环境变量更新
//   - configPayload: 完整配置快照
type OnConfigSavedFunc func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error

// RPCHandlerFunc RPC 方法处理函数签名。
//
// 参数：
//   - ctx：请求上下文
//   - params：RPC 请求参数
//   - sessionID：会话标识
//
// 返回值：
//   - map[string]any：响应负载
//   - error：处理错误
type RPCHandlerFunc func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error)

// EventSender 事件推送回调，用于向 WebSocket 客户端推送事件帧。
type EventSender func(event string, payload map[string]any)

// ──────────────────────────── 常数 ────────────────────────────

const (
	// WsErrBadRequest 请求参数错误
	WsErrBadRequest = "BAD_REQUEST"
	// WsErrMethodNotFound 方法未找到
	WsErrMethodNotFound = "METHOD_NOT_FOUND"
	// WsErrInternalError 内部错误
	WsErrInternalError = "INTERNAL_ERROR"
	// WsErrLLMError LLM 调用错误
	WsErrLLMError = "LLM_ERROR"
	// WsErrServiceUnavailable 服务不可用
	WsErrServiceUnavailable = "SERVICE_UNAVAILABLE"
	// WsErrNotFound 资源未找到
	WsErrNotFound = "NOT_FOUND"
	// WsErrAlreadyExists 资源已存在
	WsErrAlreadyExists = "ALREADY_EXISTS"
	// WsErrConflict 冲突
	WsErrConflict = "CONFLICT"
	// WsErrAgentUnavailable Agent 不可用
	WsErrAgentUnavailable = "AGENT_UNAVAILABLE"
)

// logComponent 本包日志组件
const logComponent = logger.ComponentGateway

// maxTeamsConfigPanel 配置面板最大团队数。
// 对齐 Python _flatten_modes_team_for_config_panel 中 range(10)。
const maxTeamsConfigPanel = 10

// ──────────────────────────── 全局变量 ────────────────────────────

// configEnvMap 前端配置键名 → 环境变量名映射。
//
// 对齐 Python _CONFIG_SET_ENV_MAP，共 47 个条目。
// config.get 通过此映射读取环境变量值返回给前端，
// config.set 通过反向映射将前端参数写入环境变量。
var configEnvMap = map[string]string{
	// default 模型（主对话）
	"model_provider": "MODEL_PROVIDER",
	"model":          "MODEL_NAME",
	"api_base":       "API_BASE",
	"api_key":        "API_KEY",
	// video 模型
	"video_api_base": "VIDEO_API_BASE",
	"video_api_key":  "VIDEO_API_KEY",
	"video_model":    "VIDEO_MODEL_NAME",
	"video_provider": "VIDEO_PROVIDER",
	// audio 模型
	"audio_api_base": "AUDIO_API_BASE",
	"audio_api_key":  "AUDIO_API_KEY",
	"audio_model":    "AUDIO_MODEL_NAME",
	"audio_provider": "AUDIO_PROVIDER",
	// vision 模型
	"vision_api_base": "VISION_API_BASE",
	"vision_api_key":  "VISION_API_KEY",
	"vision_model":    "VISION_MODEL_NAME",
	"vision_provider": "VISION_PROVIDER",
	// 其他
	"email_address":                     "EMAIL_ADDRESS",
	"email_token":                       "EMAIL_TOKEN",
	"embed_api_key":                     "EMBED_API_KEY",
	"embed_api_base":                    "EMBED_API_BASE",
	"embed_model":                       "EMBED_MODEL",
	"jina_api_key":                      "JINA_API_KEY",
	"bocha_api_key":                     "BOCHA_API_KEY",
	"serper_api_key":                    "SERPER_API_KEY",
	"perplexity_api_key":                "PERPLEXITY_API_KEY",
	"github_token":                      "GITHUB_TOKEN",
	"evolution_auto_scan":               "EVOLUTION_AUTO_SCAN",
	"skill_create":                      "SKILL_CREATE",
	"teamskills_market_url":             "TEAM_SKILLS_HUB_BASE_URL",
	"teamskills_user_token":             "TEAM_SKILLS_HUB_USER_TOKEN",
	"teamskills_system_token":           "TEAM_SKILLS_HUB_SYSTEM_TOKEN",
	"teamskills_allowed_download_hosts": "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS",
	"free_search_ddg_enabled":           "FREE_SEARCH_DDG_ENABLED",
	"free_search_bing_enabled":          "FREE_SEARCH_BING_ENABLED",
	"free_search_proxy_url":             "FREE_SEARCH_PROXY_URL",
	// 代理
	"skills":             "SKILLS",
	"max_iterations":     "MAX_ITERATIONS",
	"completion_timeout": "COMPLETION_TIMEOUT",
	// 团队
	"team_name":     "TEAM_NAME",
	"lifecycle":     "LIFECYCLE",
	"teammate_mode": "TEAMATE_MODE",
	"spawn_mode":    "SPAWN_MODE",
	"member_name":   "MEMBER_NAME",
	"display_name":  "DISPLAY_NAME",
	"persona":       "PERSONA",
	"agent_key":     "AGENT_KEY",
	"role_type":     "ROLE_TYPE",
	"prompt_hint":   "PROMPT_HINT",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRPCDispatcher 创建空的 RPC 分发器。
func NewRPCDispatcher() *RPCDispatcher {
	return &RPCDispatcher{
		handlers: make(map[string]RPCHandlerFunc),
	}
}

// RegisterWebHandlers 创建 RPC 分发器并注册所有应用方法。
//
// 对齐 Python app_web_handlers.py 中 _register_web_handlers(bind)，
// 包括本地实现方法、chat 类方法和 stub 占位方法。
// 消息转发由两层架构的第一层（normAndForward）处理，本地 handler 仅返回 ack。
func RegisterWebHandlers(bind *WebHandlersBindParams) *RPCDispatcher {
	d := NewRPCDispatcher()

	// 从 bind 中提取事件发送器和配置保存回调（bind 可为 nil）
	var sendEvent EventSender
	var onConfigSaved OnConfigSavedFunc
	var cryptoProv CryptoProvider
	var channelMgr *cm.ChannelManager
	var agentClient *routing.AgentClient
	var messageHandler MessageHandler
	var channel *WebChannel
	if bind != nil {
		if bind.Channel != nil {
			sendEvent = bind.Channel.broadcastEvent
			channel = bind.Channel
		}
		onConfigSaved = bind.OnConfigSaved
		cryptoProv = bind.CryptoProvider
		channelMgr = bind.ChannelManager
		agentClient = bind.AgentClient
		messageHandler = bind.MessageHandler
	}

	// ─── 本地实现方法 ───
	d.Register("config.get", handleConfigGet(cryptoProv))
	d.Register("config.set", handleConfigSet(sendEvent, onConfigSaved))
	d.Register("models.list", handleModelsList(cryptoProv))
	d.Register("channel.get", handleChannelGet(channelMgr))
	d.Register("session.list", handleSessionList)
	d.Register("session.create", handleSessionCreate)
	d.Register("session.delete", handleSessionDelete(agentClient))

	// ─── config 辅助方法 ───
	d.Register("config.save_all", handleConfigSaveAll(sendEvent, onConfigSaved))
	d.Register("config.validate_model", stubHandler("config.validate_model", map[string]any{"ok": true}))

	// ─── models 辅助方法 ───
	d.Register("models.replace_all", stubHandler("models.replace_all", map[string]any{"ok": true}))
	d.Register("models.validate", stubHandler("models.validate", map[string]any{"ok": true}))

	// ─── session 辅助方法 ───
	d.Register("session.switch", stubHandler("session.switch", map[string]any{"ok": true}))

	// ─── chat 类方法（本地 ack 响应，消息转发由两层架构第一层处理）───
	d.Register("chat.send", handleChatSend())
	d.Register("chat.resume", handleChatResume())
	d.Register("chat.interrupt", handleChatInterrupt())
	d.Register("chat.user_answer", handleChatUserAnswer())

	// ─── 路径配置 ───
	d.Register("path.get", stubHandler("path.get", map[string]any{"path": ""}))
	d.Register("path.set", stubHandler("path.set", map[string]any{"ok": true}))

	// ─── 内存信息 ───
	d.Register("memory.compute", stubHandler("memory.compute", map[string]any{"rss": 0, "total": 0}))

	// ─── 语言配置 ───
	d.Register("locale.get_conf", stubHandler("locale.get_conf", map[string]any{"preferred_language": "zh"}))
	d.Register("locale.set_conf", stubHandler("locale.set_conf", map[string]any{"ok": true}))

	// ─── 心跳配置 ───
	d.Register("heartbeat.get_conf", stubHandler("heartbeat.get_conf", map[string]any{
		"every": 0, "target": "", "active_hours": map[string]any{},
	}))
	d.Register("heartbeat.set_conf", stubHandler("heartbeat.set_conf", map[string]any{"ok": true}))
	d.Register("heartbeat.get_path", stubHandler("heartbeat.get_path", map[string]any{"path": ""}))

	// ─── 更新器 ───
	updaterStub := stubHandler("updater", map[string]any{"status": "up_to_date"})
	d.Register("updater.check", updaterStub)
	d.Register("updater.download", updaterStub)
	d.Register("updater.install", updaterStub)
	d.Register("updater.cancel", updaterStub)
	d.Register("updater.get_status", updaterStub)

	// ─── hooks ───
	d.Register("hooks.list", stubHandler("hooks.list", map[string]any{"hooks": []any{}}))

	// ─── 权限 ───
	permStub := stubHandler("permissions", map[string]any{})
	d.Register("permissions.owner_scopes.get", permStub)
	d.Register("permissions.owner_scopes.set", permStub)
	d.Register("permissions.tools.get", permStub)
	d.Register("permissions.tools.set", permStub)
	d.Register("permissions.rules.get", permStub)
	d.Register("permissions.rules.set", permStub)
	d.Register("permissions.approval_overrides.get", permStub)
	d.Register("permissions.approval_overrides.set", permStub)

	// ─── 禁止记忆 ───
	d.Register("memory.forbidden.get", stubHandler("memory.forbidden.get", map[string]any{}))
	d.Register("memory.forbidden.set", stubHandler("memory.forbidden.set", map[string]any{}))

	// ─── IM 平台配置 ───
	channelConfStub := stubHandler("channel.conf", map[string]any{})
	for _, platform := range []string{"feishu", "dingtalk", "telegram", "discord", "whatsapp", "wecom", "xiaoyi"} {
		d.Register("channel."+platform+".get_conf", channelConfStub)
		d.Register("channel."+platform+".set_conf", channelConfStub)
	}
	d.Register("channel.wechat.get_login_ui", stubHandler("channel.wechat.get_login_ui", map[string]any{}))
	d.Register("channel.wechat.unbind", stubHandler("channel.wechat.unbind", map[string]any{}))

	// ─── cron ───
	cronListStub := stubHandler("cron", map[string]any{"jobs": []any{}})
	cronOkStub := stubHandler("cron", map[string]any{"ok": true})
	d.Register("cron.job.list", cronListStub)
	d.Register("cron.job.create", cronOkStub)
	d.Register("cron.job.update", cronOkStub)
	d.Register("cron.job.delete", cronOkStub)
	d.Register("cron.job.enable", cronOkStub)
	d.Register("cron.job.disable", cronOkStub)
	d.Register("cron.job.run", cronOkStub)
	d.Register("cron.job.status", stubHandler("cron", map[string]any{}))

	// ─── harness ───
	harnessListStub := stubHandler("harness", map[string]any{"packages": []any{}})
	harnessOkStub := stubHandler("harness", map[string]any{"ok": true})
	d.Register("harness.list", harnessListStub)
	d.Register("harness.install", harnessOkStub)
	d.Register("harness.uninstall", harnessOkStub)
	d.Register("harness.enable", harnessOkStub)
	d.Register("harness.disable", harnessOkStub)
	d.Register("harness.get_status", stubHandler("harness", map[string]any{}))
	d.Register("harness.rebuild", harnessOkStub)

	// ─── 转发方法（全部 stub）───
	forwardStubs := map[string]map[string]any{
		"initialize":        {},
		"acp.tool_response": {},
		// 团队
		"team.delete":      {},
		"team.snapshot":    {},
		"team.history.get": {},
		// 历史
		"history.get": {},
		// 浏览器
		"browser.start": {},
		// 技能
		"skills.marketplace.list":   {"skills": []any{}},
		"skills.list":               {"skills": []any{}},
		"skills.installed":          {"skills": []any{}},
		"skills.get":                {},
		"skills.toggle":             {"ok": true},
		"skills.install":            {"ok": true},
		"skills.import_local":       {"ok": true},
		"skills.uninstall":          {"ok": true},
		"skills.marketplace.add":    {"ok": true},
		"skills.marketplace.remove": {"ok": true},
		"skills.marketplace.toggle": {"ok": true},
		// 技能网络
		"skills.skillnet.search":         {"skills": []any{}},
		"skills.skillnet.install":        {"ok": true},
		"skills.skillnet.install_status": {"status": "not_installed"},
		"skills.skillnet.evaluate":       {},
		// ClawHub
		"skills.clawhub.get_token": {"token": ""},
		"skills.clawhub.set_token": {"ok": true},
		"skills.clawhub.search":    {"skills": []any{}},
		"skills.clawhub.download":  {"ok": true},
		// 团队技能中心
		"skills.teamskillshub.info":     {},
		"skills.teamskillshub.init":     {"ok": true},
		"skills.teamskillshub.validate": {"ok": true},
		"skills.teamskillshub.pack":     {"ok": true},
		"skills.teamskillshub.search":   {"skills": []any{}},
		"skills.teamskillshub.install":  {"ok": true},
		"skills.teamskillshub.publish":  {"ok": true},
		"skills.teamskillshub.delete":   {"ok": true},
		// 进化
		"skills.evolution.status": {"status": "idle"},
		"skills.evolution.get":    {},
		"skills.evolution.save":   {"ok": true},
		// 插件
		"plugins.list":      {"plugins": []any{}},
		"plugins.install":   {"ok": true},
		"plugins.uninstall": {"ok": true},
		"plugins.enable":    {"ok": true},
		"plugins.disable":   {"ok": true},
		"plugins.reload":    {"ok": true},
		// 扩展
		"extensions.list":   {"extensions": []any{}},
		"extensions.import": {"ok": true},
		"extensions.delete": {"ok": true},
		"extensions.toggle": {"ok": true},
		// 代理
		"agents.list":       {"agents": []any{}},
		"agents.get":        {},
		"agents.create":     {"ok": true},
		"agents.update":     {"ok": true},
		"agents.delete":     {"ok": true},
		"agents.enable":     {"ok": true},
		"agents.disable":    {"ok": true},
		"agents.tools_list": {"tools": []any{}},
		// 调度
		"schedule.check_config":  {},
		"schedule.update_config": {"ok": true},
		"schedule.create":        {"ok": true},
		"schedule.run":           {"ok": true},
		"schedule.list":          {"schedules": []any{}},
		"schedule.status":        {},
		"schedule.logs":          {"logs": []any{}},
		"schedule.cancel":        {"ok": true},
		"schedule.delete":        {"ok": true},
	}
	for method, payload := range forwardStubs {
		d.Register(method, stubHandler(method, payload))
	}

	// ─── 注册 onConnect 钩子：发送 connection.ack ───
	// 对齐 Python WebChannel._connection_handler 中 _connect_hooks 逻辑
	if channel != nil {
		ch := channel // 捕获循环变量
		ac := agentClient
		mh := messageHandler
		ch.OnConnect(func(_ *websocket.Conn) error {
			// AgentClient 未就绪时跳过 ack（对齐 Python: 仅 debug 日志，不断开连接）
			if ac == nil || !ac.ServerReady() {
				logger.Debug(logComponent).Msg("AgentClient 未就绪，跳过 connection.ack")
				return nil
			}
			// 构造 connection.ack 消息（对齐 Python _on_connect）
			sid := MakeSessionID()
			ackMsg := &schema.Message{
				ID:        "ack-" + sid,
				Type:      schema.MessageTypeEvent,
				ChannelID: ch.ChannelID(),
				SessionID: sid,
				EventType: schema.EventTypeConnectionAck,
				OK:        true,
				Payload: map[string]any{
					"session_id":       sid,
					"mode":             "BUILD",
					"tools":            []any{},
					"protocol_version": protocolVersion,
				},
				Timestamp: float64(time.Now().UnixMilli()) / 1000.0,
			}
			// 对齐 Python L592-596: 优先用 mh.publish_robot_messages，否则 fallback channel.send
			if mh != nil {
				if err := mh.PublishRobotMessages(context.Background(), ackMsg); err != nil {
					logger.Warn(logComponent).Err(err).Msg("MessageHandler.PublishRobotMessages 发送 connection.ack 失败，fallback channel.Send")
					_ = ch.Send(context.Background(), ackMsg)
				}
			} else {
				if err := ch.Send(context.Background(), ackMsg); err != nil {
					logger.Warn(logComponent).Err(err).Msg("发送 connection.ack 失败")
				}
			}
			return nil
		})
	}

	// 将 dispatcher 设到 channel 上
	if channel != nil {
		channel.dispatcher = d
	}

	return d
}

// Register 注册 RPC 方法处理函数。
//
// 对齐 Python app_web_handlers.py 中 @register_rpc_method 装饰器模式。
func (d *RPCDispatcher) Register(method string, handler RPCHandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.handlers[method] = handler
	logger.Debug(logComponent).
		Str("method", method).
		Msg("已注册 RPC 方法")
}

// Dispatch 分发 RPC 请求到对应处理函数。
//
// 对齐 Python _handle_rpc_request 中 method 查找与调用逻辑。
// 方法未找到时返回 METHOD_NOT_FOUND 错误。
func (d *RPCDispatcher) Dispatch(method string, params map[string]any, sessionID string) (map[string]any, error) {
	d.mu.RLock()
	handler, ok := d.handlers[method]
	d.mu.RUnlock()

	if !ok {
		logger.Warn(logComponent).
			Str("method", method).
			Msg("RPC 方法未找到")
		return nil, fmt.Errorf("方法 %q 未找到", method)
	}

	ctx := context.Background()
	result, err := handler(ctx, params, sessionID)
	if err != nil {
		logger.Error(logComponent).
			Str("method", method).
			Err(err).
			Msg("RPC 方法处理失败")
	}
	return result, err
}

// MakeSessionID 生成会话标识。
//
// 格式：sess_{hex_timestamp}_{6_random_hex}
// 对齐 Python _make_session_id() 和前端 generateSessionId。
func MakeSessionID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("sess_%s_%x", ts, suffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strOrEmpty 将任意值转为字符串，nil 或无效值返回空串。
// 对齐 Python: str(val) or ""。
func strOrEmpty(val any) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	s := fmt.Sprintf("%v", val)
	if s == "<nil>" {
		return ""
	}
	return s
}

// stubHandler 返回固定 stub 响应的处理函数。
//
// 用于尚未实现转发的 RPC 方法，返回占位数据使前端不报错。
func stubHandler(_ string, payload map[string]any) RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		return payload, nil
	}
}

// handleConfigGet 处理 config.get 请求。
//
// 全量实现：读取 configEnvMap 中 47 个环境变量 + config.yaml 补充字段 + app_version。
// 对齐 Python config_get_handler 中 ~65 字段的返回。
// crypto 用于解密 api_key/token 等敏感字段。
func handleConfigGet(crypto CryptoProvider) RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		result := make(map[string]any, len(configEnvMap)+10)

		// 从环境变量映射读取
		for key, envVar := range configEnvMap {
			val := os.Getenv(envVar)
			// 对齐 Python: 对包含 api_key 或 token 的键解密
			if crypto != nil && val != "" && isSensitiveKey(key) {
				val = crypto.Decrypt(val)
			}
			result[key] = val
		}

		// 应用版本
		result["app_version"] = version.Version

		// config.yaml 补充字段
		cfg, err := loadAppConfig()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("config.get 加载 config.yaml 失败，使用默认值")
		}

		// 上下文引擎启用
		result["context_engine_enabled"] = getConfigString(cfg, "react.context_engine_config.enabled", "false")
		// KV 缓存亲和启用
		result["kv_cache_affinity_enabled"] = getConfigString(cfg, "react.context_engine_config.enable_kv_cache_release", "false")
		// 权限启用
		result["permissions_enabled"] = getConfigString(cfg, "permissions.enabled", "false")
		// skill_create: 环境变量优先，fallback config.yaml
		if envVal := os.Getenv("SKILL_CREATE"); envVal != "" {
			result["skill_create"] = envVal
		} else {
			result["skill_create"] = getConfigString(cfg, "react.evolution.skill_create", "false")
		}
		// evolution_auto_scan: 环境变量优先，fallback config.yaml
		if envVal := os.Getenv("EVOLUTION_AUTO_SCAN"); envVal != "" {
			result["evolution_auto_scan"] = envVal
		} else {
			result["evolution_auto_scan"] = getConfigString(cfg, "react.evolution.auto_scan", "false")
		}
		// 记忆禁止启用
		result["memory_forbidden_enabled"] = getConfigString(cfg, "memory.forbidden_memory_definition.enabled", "false")
		// 记忆禁止描述：返回 dict（对齐 Python getConfigAny 而非 getConfigString）
		result["memory_forbidden_description"] = getConfigAny(cfg, "memory.forbidden_memory_definition.description", map[string]any{})

		// 默认值填充（仅当环境变量为空时）
		if result["free_search_ddg_enabled"] == "" {
			result["free_search_ddg_enabled"] = "false"
		}
		if result["free_search_bing_enabled"] == "" {
			result["free_search_bing_enabled"] = "false"
		}

		// 展平 team 配置
		flattenTeamConfig(cfg, result)

		return result, nil
	}
}

// isSensitiveKey 判断配置键是否为敏感字段（需要解密）。
//
// 对齐 Python: 包含 "api_key" 或 "token" 的键视为敏感字段。
func isSensitiveKey(key string) bool {
	return strings.Contains(key, "api_key") || strings.Contains(key, "token")
}

// handleConfigSet 处理 config.set 请求。
//
// 完整实现：参数映射→provider校验→YAML键写入→agents/team写入→os.Setenv+.env持久化→回包→触发热重载。
// 对齐 Python: _config_set (app_web_handlers.py L870-895)。
func handleConfigSet(sendEvent EventSender, onConfigSaved OnConfigSavedFunc) RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return nil, fmt.Errorf("params 不能为空")
		}

		// 步骤1-5: applyConfigPayload 统一处理
		envUpdates, yamlUpdated, err := ApplyConfigPayload(params)
		if err != nil {
			// 区分 BadRequest 和 InternalError
			if _, ok := err.(*ConfigBadRequest); ok {
				return map[string]any{"ok": false, "error": err.Error(), "code": WsErrBadRequest}, nil
			}
			return map[string]any{"ok": false, "error": err.Error(), "code": WsErrInternalError}, nil
		}

		// 步骤6: 回包给前端（对齐 Python: 先回包后通知）
		appliedWithoutRestart := true
		updatedParamKeys := make([]string, 0)
		for k, e := range configEnvMap {
			if _, ok := envUpdates[e]; ok {
				updatedParamKeys = append(updatedParamKeys, k)
			}
		}
		updatedParamKeys = append(updatedParamKeys, yamlUpdated...)

		// 步骤7: 触发热重载（对齐 Python: _notify_config_saved_once）
		if len(envUpdates) > 0 || len(yamlUpdated) > 0 {
			NotifyConfigSavedOnce(onConfigSaved, envUpdates, yamlUpdated, false)
		}

		return map[string]any{
			"ok":                      true,
			"updated":                 updatedParamKeys,
			"applied_without_restart": appliedWithoutRestart,
		}, nil
	}
}

// handleConfigSaveAll 处理 config.save_all 请求。
//
// 批量保存配置面板变更并触发热重载。
// 对齐 Python: _config_save_all (app_web_handlers.py L1086-1151)。
//
// 支持子载荷：config、models、agents、team。
func handleConfigSaveAll(sendEvent EventSender, onConfigSaved OnConfigSavedFunc) RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
		if params == nil {
			return map[string]any{"ok": false, "error": "params must be object", "code": WsErrBadRequest}, nil
		}

		envUpdates := make(map[string]string)
		yamlUpdated := make([]string, 0)
		modelsCount := 0

		// models 子载荷
		var newModels []map[string]any
		if modelsVal, ok := params["models"]; ok && modelsVal != nil {
			// 将 []any 转为 []map[string]any
			parsed, err := buildModelsDefaultsFromFrontend(modelsVal, nil)
			if err != nil {
				if _, ok := err.(*ConfigBadRequest); ok {
					return map[string]any{"ok": false, "error": err.Error(), "code": WsErrBadRequest}, nil
				}
				return map[string]any{"ok": false, "error": err.Error(), "code": WsErrInternalError}, nil
			}
			newModels = parsed
		}

		// config 子载荷 + agents/team 合并
		configParams := make(map[string]any)
		if rawConfig, ok := params["config"]; ok && rawConfig != nil {
			configMap, ok := rawConfig.(map[string]any)
			if !ok {
				return map[string]any{"ok": false, "error": "config must be object", "code": WsErrBadRequest}, nil
			}
			for k, v := range configMap {
				configParams[k] = v
			}
		}
		if agents, ok := params["agents"]; ok {
			configParams["agents"] = agents
		}
		if team, ok := params["team"]; ok {
			configParams["team"] = team
		}

		// 应用 config 子载荷
		if len(configParams) > 0 {
			appliedEnv, appliedYAML, err := ApplyConfigPayload(configParams)
			if err != nil {
				if _, ok := err.(*ConfigBadRequest); ok {
					return map[string]any{"ok": false, "error": err.Error(), "code": WsErrBadRequest}, nil
				}
				return map[string]any{"ok": false, "error": err.Error(), "code": WsErrInternalError}, nil
			}
			for k, v := range appliedEnv {
				envUpdates[k] = v
			}
			yamlUpdated = append(yamlUpdated, appliedYAML...)
		}

		// 应用 models 子载荷
		if newModels != nil {
			if err := updateDefaultModelsInConfig(newModels); err != nil {
				logger.Warn(logComponent).
					Err(err).
					Msg("写入 models.defaults 失败")
				return map[string]any{"ok": false, "error": err.Error(), "code": WsErrInternalError}, nil
			}
			yamlUpdated = append(yamlUpdated, "models.defaults")
			modelsCount = len(newModels)
		}

		// 回包给前端（始终设置 models_count 键，对齐 Python）
		updatedParamKeys := make([]string, 0)
		for k, e := range configEnvMap {
			if _, ok := envUpdates[e]; ok {
				updatedParamKeys = append(updatedParamKeys, k)
			}
		}
		updatedParamKeys = append(updatedParamKeys, yamlUpdated...)

		result := map[string]any{
			"ok":                      true,
			"updated":                 updatedParamKeys,
			"applied_without_restart": true,
			"models_count":            modelsCount,
		}

		// 触发热重载（force=true，对齐 Python: _notify_config_saved_once(force=True)）
		if len(envUpdates) > 0 || len(yamlUpdated) > 0 {
			NotifyConfigSavedOnce(onConfigSaved, envUpdates, yamlUpdated, true)
		}

		return result, nil
	}
}

// handleModelsList 处理 models.list 请求。
//
// 从 config.yaml 读取 models.defaults 配置，展平为前端格式。
// 对齐 Python: _models_list_handler (app_web_handlers.py)。
// crypto 用于解密 api_key 字段。
func handleModelsList(crypto CryptoProvider) RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		cfg, err := loadAppConfig()
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("models.list 加载配置失败")
			return map[string]any{"models": []any{}}, nil
		}

		// 读取嵌套格式的 models.defaults
		rawModels := getConfigAny(cfg, "models.defaults", nil)
		if rawModels == nil {
			return map[string]any{"models": []any{}}, nil
		}
		modelsList, ok := rawModels.([]any)
		if !ok || len(modelsList) == 0 {
			return map[string]any{"models": []any{}}, nil
		}

		// 展平为前端格式：从嵌套的 model_client_config/model_config_obj 提取字段
		result := make([]map[string]any, 0, len(modelsList))
		for idx, item := range modelsList {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}

			flat := map[string]any{
				"origin_index": idx,
			}

			// 从 model_client_config 提取
			if mcc, ok := itemMap["model_client_config"].(map[string]any); ok {
				if v, ok := mcc["model_name"].(string); ok {
					flat["model_name"] = v
				}
				if v, ok := mcc["api_base"].(string); ok {
					flat["api_base"] = v
				}
				if v, ok := mcc["api_key"].(string); ok {
					// 解密 api_key
					if crypto != nil && v != "" {
						flat["api_key"] = crypto.Decrypt(v)
					} else {
						flat["api_key"] = v
					}
				}
				if v, ok := mcc["client_provider"].(string); ok {
					flat["model_provider"] = v
				}
			}

			// 从 model_config_obj 提取
			if mco, ok := itemMap["model_config_obj"].(map[string]any); ok {
				// 对齐 Python: mco.get("temperature", 0.95)
				temperature := 0.95
				if v, ok := mco["temperature"]; ok && v != nil {
					if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
						temperature = f
					}
				}
				flat["temperature"] = temperature
			}

			// 顶层字段
			if v, ok := itemMap["is_default"]; ok {
				flat["is_default"] = v
			}
			if v, ok := itemMap["alias"].(string); ok {
				flat["alias"] = v
			}

			result = append(result, flat)
		}

		// active_model: 列表首位的模型名称（对齐 Python L1049）
		activeModel := ""
		if len(result) > 0 {
			if name, ok := result[0]["model_name"].(string); ok {
				activeModel = name
			}
		}

		return map[string]any{"models": result, "active_model": activeModel}, nil
	}
}

// handleChannelGet 处理 channel.get 请求。
//
// 从 ChannelManager 动态读取已启用的渠道列表。
// 对齐 Python: channel_get_handler，读取 ChannelManager.enabled_channels。
func handleChannelGet(channelMgr *cm.ChannelManager) RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		channels := make(map[string]any)
		if channelMgr != nil {
			for _, cid := range channelMgr.GetEnabledChannels() {
				channels[cid] = map[string]any{"enabled": true}
			}
		}
		return map[string]any{"channels": channels}, nil
	}
}

// handleSessionList 处理 session.list 请求。
//
// 遍历会话目录，读取每个 session 的 metadata.json 返回列表。
func handleSessionList(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
	sessionsDir := workspace.AgentSessionsDir()

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"sessions": []any{}}, nil
		}
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}

	var sessions []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(sessionsDir, entry.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			// 无 metadata.json 的会话目录，用默认值
			sessions = append(sessions, map[string]any{
				"session_id": entry.Name(),
			})
			continue
		}
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err != nil {
			sessions = append(sessions, map[string]any{
				"session_id": entry.Name(),
			})
			continue
		}
		meta["session_id"] = entry.Name()
		sessions = append(sessions, meta)
	}

	return map[string]any{"sessions": sessions}, nil
}

// handleSessionCreate 处理 session.create 请求。
//
// 创建会话目录和 metadata.json。
// 对齐 Python: session_create_handler，要求 session_id 非空，已存在则 ALREADY_EXISTS。
func handleSessionCreate(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
	// 要求 session_id（对齐 Python: 必填参数）
	sessionID, _ := params["session_id"].(string)
	if sessionID == "" {
		return map[string]any{"ok": false, "error": "session_id is required", "code": WsErrBadRequest}, nil
	}

	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)

	// 检查是否已存在（对齐 Python: ALREADY_EXISTS）
	if _, err := os.Stat(sessionDir); err == nil {
		return map[string]any{"ok": false, "error": "session already exists", "code": WsErrAlreadyExists}, nil
	}

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}

	// 写入完整 metadata.json（对齐 Python: 写入 session_id, channel_id, user_id, title, mode）
	meta := map[string]any{
		"session_id": sessionID,
		"created_at": time.Now().Format(time.RFC3339),
	}
	if params != nil {
		if v, ok := params["channel_id"].(string); ok && v != "" {
			meta["channel_id"] = v
		}
		if v, ok := params["user_id"].(string); ok && v != "" {
			meta["user_id"] = v
		}
		if v, ok := params["title"].(string); ok && v != "" {
			meta["title"] = v
		}
		if v, ok := params["mode"].(string); ok && v != "" {
			meta["mode"] = v
		}
	}
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), metaData, 0o644); err != nil {
		return nil, fmt.Errorf("写入 metadata.json 失败: %w", err)
	}

	return map[string]any{"session_id": sessionID, "ok": true}, nil
}

// handleSessionDelete 处理 session.delete 请求。
//
// 对齐 Python _session_delete (app_web_handlers.py L1294-1354)：
//  1. AgentClient 可用 → 转发到 AgentServer → 成功则直接返回结果
//  2. 转发失败 → fallback 本地删除
//  3. AgentClient 不可用 → 本地删除（team 模式返回 AGENT_UNAVAILABLE）
func handleSessionDelete(agentClient *routing.AgentClient) RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
		sessionID, _ := params["session_id"].(string)
		if sessionID == "" {
			return map[string]any{"ok": false, "error": "session_id is required", "code": WsErrBadRequest}, nil
		}

		// 优先转发到 AgentServer（对齐 Python L1313-1336）
		if agentClient != nil && agentClient.ServerReady() {
			envelope := buildSessionDeleteEnvelope(sessionID, params)
			if envelope != nil {
				resp, err := agentClient.SendRequest(context.Background(), envelope)
				if err == nil && resp != nil {
					// 转发成功：直接返回 AgentServer 的响应（对齐 Python L1324-1327）
					if resp.OK {
						payload := resp.Payload
						if payload == nil {
							payload = map[string]any{}
						}
						return payload, nil
					}
					// 转发返回失败（对齐 Python L1328-1335）
					payload := resp.Payload
					if payload == nil {
						payload = map[string]any{}
					}
					errMsg := "session.delete failed"
					if v, ok := payload["error"].(string); ok && v != "" {
						errMsg = v
					}
					code := ""
					if v, ok := payload["code"].(string); ok {
						code = v
					}
					return map[string]any{"ok": false, "error": errMsg, "code": code}, nil
				}
				// 转发异常：fallback 本地删除（对齐 Python L1337-1338）
				logger.Warn(logComponent).Err(err).Msg("session.delete 转发 AgentServer 失败，fallback 本地删除")
			}
		}

		// 本地删除（对齐 Python L1340-：AgentClient 不可用或转发失败时）
		sessionsDir := workspace.AgentSessionsDir()
		sessionDir := filepath.Join(sessionsDir, sessionID)

		if _, err := os.Stat(sessionDir); os.IsNotExist(err) {
			// 判断是否 team 模式 → AGENT_UNAVAILABLE（对齐 Python L1341-1348）
			mode, _ := params["mode"].(string)
			if mode == "team" && (agentClient == nil || !agentClient.ServerReady()) {
				return map[string]any{"ok": false, "error": "team session delete requires agent server", "code": WsErrAgentUnavailable}, nil
			}
			return map[string]any{"ok": false, "error": "session not found", "code": WsErrNotFound}, nil
		}

		if err := os.RemoveAll(sessionDir); err != nil {
			return nil, fmt.Errorf("删除会话目录失败: %w", err)
		}

		return map[string]any{"ok": true}, nil
	}
}

// handleChatSend 处理 chat.send 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
// 对齐 Python: chat.send handler 仅返回 ack。
func handleChatSend() RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, sessionID string) (map[string]any, error) {
		return map[string]any{"accepted": true, "session_id": sessionID}, nil
	}
}

// handleChatResume 处理 chat.resume 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
func handleChatResume() RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, sessionID string) (map[string]any, error) {
		return map[string]any{"accepted": true, "session_id": sessionID}, nil
	}
}

// handleChatInterrupt 处理 chat.interrupt 请求。
//
// 返回即时 ack 响应，从 params 中提取 intent。
// 对齐 Python: chat.interrupt handler，intent 默认 "interrupt"，可从 params 覆写。
// 消息转发由两层架构第一层（normAndForward）处理。
func handleChatInterrupt() RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, sessionID string) (map[string]any, error) {
		payload := map[string]any{"accepted": true, "session_id": sessionID}
		if params != nil {
			if intent, ok := params["intent"].(string); ok && intent != "" {
				payload["intent"] = intent
			}
		}
		return payload, nil
	}
}

// handleChatUserAnswer 处理 chat.user_answer 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
func handleChatUserAnswer() RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, sessionID string) (map[string]any, error) {
		requestID, _ := params["request_id"].(string)
		return map[string]any{"accepted": true, "session_id": sessionID, "request_id": requestID}, nil
	}
}

// loadAppConfig 加载 config.yaml 配置。
//
// 封装 config.New + Load 流程，失败时返回空 map 而非 error，
// 调用方用默认值兜底。
func loadAppConfig() (map[string]any, error) {
	cfg, err := config.New("")
	if err != nil {
		return make(map[string]any), err
	}
	data, err := cfg.Load()
	if err != nil {
		return make(map[string]any), err
	}
	return data, nil
}

// getConfigString 从配置 map 中读取点分隔路径的字符串值，不存在时返回默认值。
func getConfigString(cfg map[string]any, key string, defaultVal string) string {
	parts := strings.Split(key, ".")
	var current any = cfg
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return defaultVal
		}
		current, ok = m[part]
		if !ok {
			return defaultVal
		}
	}
	if s, ok := current.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", current)
}

// getConfigAny 从配置 map 中读取点分隔路径的值，不存在时返回默认值。
func getConfigAny(cfg map[string]any, key string, defaultVal any) any {
	parts := strings.Split(key, ".")
	var current any = cfg
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return defaultVal
		}
		current, ok = m[part]
		if !ok {
			return defaultVal
		}
	}
	return current
}

// flattenTeamConfig 展平 modes.team 配置到结果 map。
//
// 对齐 Python _flatten_modes_team_for_config_panel (app_web_handlers.py L380-482)。
// 遍历最多 10 个 team，提取 team_{idx}_* 前缀字段和 agent 详情。
// flattenTeamConfig 展平 modes.team 配置为前端 config panel 格式。
//
// 对齐 Python _flatten_modes_team_for_config_panel (app_web_handlers.py L380-482)：
//   - teams_raw 为 dict（key=team_name, value=team_spec），最多 10 个 team
//   - 每个 team 展平 team_0_name/lifecycle/teammate_mode/spawn_mode 等字段
//   - leader 信息 + agent_key 推导（缺省时用 "{team_name}_leader"）
//   - teammate 从 agents.teammate 取（不是 teammates 数组）
//   - predefined_members JSON 序列化
//   - agent 详情从 agent_specs dict 按 agent_key 匹配（不是按位置）
//   - agent 输出格式：agent_name_{idx}/agent_model_{idx}/agent_skills_{idx} 等
func flattenTeamConfig(cfg map[string]any, result map[string]any) {
	// 读取 modes.team
	modes := getConfigAny(cfg, "modes", nil)
	var teamsRaw map[string]any
	if m, ok := modes.(map[string]any); ok {
		if t, ok := m["team"].(map[string]any); ok {
			teamsRaw = t
		}
	}
	if teamsRaw == nil {
		return
	}

	// 读取 web_config_panel.agent_team_agents 用于 agent 详情补充
	// 对齐 Python L390-396
	agentSpecs := make(map[string]map[string]any)
	panelCfg := getConfigAny(cfg, "web_config_panel", nil)
	if panel, ok := panelCfg.(map[string]any); ok {
		if registry, ok := panel["agent_team_agents"].(map[string]any); ok {
			for agentKey, spec := range registry {
				if s, ok := spec.(map[string]any); ok {
					agentSpecs[agentKey] = s
				}
			}
		}
	}

	// addAgent 辅助函数：将 agent_key 加入 agentSpecs（如果尚未存在）
	// 对齐 Python L398-403
	addAgent := func(agentKey string, spec any) string {
		if agentKey == "" {
			return ""
		}
		if s, ok := spec.(map[string]any); ok {
			if _, exists := agentSpecs[agentKey]; !exists {
				agentSpecs[agentKey] = s
			}
		}
		return agentKey
	}

	// modelNameFromSpec 辅助函数：从 agent spec 中提取 model name
	// 对齐 Python L405-417
	modelNameFromSpec := func(spec map[string]any) string {
		modelCfg, _ := spec["model"].(map[string]any)
		if modelCfg == nil {
			return ""
		}
		if v := modelCfg["model"]; v != nil {
			return fmt.Sprintf("%v", v)
		}
		if requestCfg, ok := modelCfg["model_request_config"].(map[string]any); ok {
			if v := requestCfg["model"]; v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
		if clientCfg, ok := modelCfg["model_client_config"].(map[string]any); ok {
			if v := clientCfg["model_name"]; v != nil {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}

	// 遍历 teams（dict，key=team_name，value=team_spec）
	// 对齐 Python L419-470
	teamIdx := 0
	for teamName, teamSpecRaw := range teamsRaw {
		if teamIdx >= maxTeamsConfigPanel {
			break
		}
		teamSpec, ok := teamSpecRaw.(map[string]any)
		if !ok {
			continue
		}
		prefix := fmt.Sprintf("team_%d_", teamIdx)

		// 基础字段
		name := strOrEmpty(teamSpec["team_name"])
		if name == "" {
			name = teamName
		}
		result[prefix+"name"] = name
		result[prefix+"lifecycle"] = strOrEmpty(teamSpec["lifecycle"])
		result[prefix+"teammate_mode"] = strOrEmpty(teamSpec["teammate_mode"])
		result[prefix+"spawn_mode"] = strOrEmpty(teamSpec["spawn_mode"])

		// agents 字典
		agents, _ := teamSpec["agents"].(map[string]any)
		if agents == nil {
			agents = make(map[string]any)
		}

		// leader 信息（对齐 Python L432-439）
		if leader, ok := teamSpec["leader"].(map[string]any); ok {
			for _, key := range []string{"member_name", "display_name", "persona"} {
				result[prefix+"leader_"+key] = strOrEmpty(leader[key])
			}
			leaderKey := strOrEmpty(leader["agent_key"])
			if leaderKey == "" {
				leaderKey = teamName + "_leader"
			}
			result[prefix+"leader_agent_key"] = addAgent(leaderKey, agents["leader"])
		}

		// teammate 信息（对齐 Python L441-449）
		// 从 agents.teammate 取，不是从 teammates 数组
		if teammateSpec, ok := agents["teammate"].(map[string]any); ok {
			teammateKey := ""
			if teammate, ok := teamSpec["teammate"].(map[string]any); ok {
				teammateKey = strOrEmpty(teammate["agent_key"])
			}
			if teammateKey == "" {
				teammateKey = teamName + "_teammate"
			}
			result[prefix+"teammate_agent_key"] = addAgent(teammateKey, teammateSpec)
		} else {
			result[prefix+"teammate_agent_key"] = ""
		}

		// predefined_members JSON 序列化（对齐 Python L451-470）
		membersOut := []map[string]string{}
		if members, ok := teamSpec["predefined_members"].([]any); ok {
			for _, memberRaw := range members {
				member, ok := memberRaw.(map[string]any)
				if !ok {
					continue
				}
				memberName := strOrEmpty(member["member_name"])
				agentKey := strOrEmpty(member["agent_key"])
				if agentKey == "" {
					if memberName != "" {
						agentKey = teamName + "_" + memberName
					}
				}
				if agentKey != "" {
					addAgent(agentKey, agents[memberName])
				}
				membersOut = append(membersOut, map[string]string{
					"member_name":  memberName,
					"display_name": strOrEmpty(member["display_name"]),
					"persona":      strOrEmpty(member["persona"]),
					"prompt_hint":  strOrEmpty(member["prompt_hint"]),
					"agent_key":    agentKey,
				})
			}
		}
		if membersJSON, err := json.Marshal(membersOut); err == nil {
			result[prefix+"predefined_members"] = string(membersJSON)
		}

		teamIdx++
	}

	// agent 详情（对齐 Python L472-480）
	agentIdx := 0
	for agentKey, spec := range agentSpecs {
		if agentIdx >= maxTeamsConfigPanel {
			break
		}
		result[fmt.Sprintf("agent_name_%d", agentIdx)] = agentKey
		result[fmt.Sprintf("agent_model_%d", agentIdx)] = modelNameFromSpec(spec)
		// skills: 逗号分隔
		skillsStr := ""
		if skills, ok := spec["skills"].([]any); ok {
			parts := make([]string, 0, len(skills))
			for _, s := range skills {
				parts = append(parts, fmt.Sprintf("%v", s))
			}
			skillsStr = strings.Join(parts, ",")
		}
		result[fmt.Sprintf("agent_skills_%d", agentIdx)] = skillsStr
		// max_iterations 默认 200（对齐 Python）
		maxIter := 200
		if v, ok := spec["max_iterations"]; ok && v != nil {
			if n, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil {
				maxIter = n
			}
		}
		result[fmt.Sprintf("agent_max_iterations_%d", agentIdx)] = strconv.Itoa(maxIter)
		// completion_timeout 默认 600（对齐 Python）
		timeout := 600
		if v, ok := spec["completion_timeout"]; ok && v != nil {
			if n, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil {
				timeout = n
			}
		}
		result[fmt.Sprintf("agent_completion_timeout_%d", agentIdx)] = strconv.Itoa(timeout)
		agentIdx++
	}
}

// buildSessionDeleteEnvelope 构建 session.delete 的 E2A envelope。
//
// 用于转发到 AgentServer，对齐 Python 中 session_delete_handler 的转发逻辑。
func buildSessionDeleteEnvelope(sessionID string, params map[string]any) *e2a.E2AEnvelope {
	envelope := e2a.NewE2AEnvelope()
	envelope.Method = "session.delete"
	envelope.SessionID = sessionID
	envelope.Params = params
	return envelope
}

// persistEnvUpdates 将更新的环境变量持久化到 .env 文件。
//
// 对齐 Python _persist_env_updates：
//  1. 读取现有 .env 所有行
//  2. 对每个更新的 key，找到 KEY=... 行替换
//  3. 未找到的 key 追加到文件末尾
//  4. 值用双引号包裹：KEY="value"
//  5. 空值写为 KEY=
func persistEnvUpdates(updated map[string]string) error {
	envPath := workspace.EnvFile()

	// 读取现有 .env 文件
	var lines []string
	data, err := os.ReadFile(envPath)
	if err == nil {
		lines = strings.Split(string(data), "\n")
	}

	// 构建更新集合
	updatedSet := make(map[string]string, len(updated))
	for k, v := range updated {
		updatedSet[k] = v
	}

	// 替换已有行
	for i, line := range lines {
		// 跳过注释和空行
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 提取 KEY= 部分
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			continue
		}
		key := line[:eqIdx]
		if newVal, ok := updatedSet[key]; ok {
			if newVal == "" {
				lines[i] = key + "="
			} else {
				lines[i] = fmt.Sprintf(`%s="%s"`, key, newVal)
			}
			delete(updatedSet, key)
		}
	}

	// 追加新增的 key
	for key, val := range updatedSet {
		if val == "" {
			lines = append(lines, key+"=")
		} else {
			lines = append(lines, fmt.Sprintf(`%s="%s"`, key, val))
		}
	}

	// 写回文件
	content := strings.Join(lines, "\n")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(envPath, []byte(content), 0o644)
}

// sortedKeys 返回 map 的有序键列表。
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
