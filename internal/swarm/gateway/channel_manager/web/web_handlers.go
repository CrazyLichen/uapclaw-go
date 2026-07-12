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

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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

// ──────────────────────────── 常量 ────────────────────────────

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
func RegisterWebHandlers(sendEvent EventSender, onConfigSaved OnConfigSavedFunc) *RPCDispatcher {
	d := NewRPCDispatcher()

	// ─── 本地实现方法 ───
	d.Register("config.get", handleConfigGet)
	d.Register("config.set", handleConfigSet(sendEvent, onConfigSaved))
	d.Register("models.list", handleModelsList)
	d.Register("channel.get", handleChannelGet)
	d.Register("session.list", handleSessionList)
	d.Register("session.create", handleSessionCreate)
	d.Register("session.delete", handleSessionDelete)

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
func handleConfigGet(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
	result := make(map[string]any, len(configEnvMap)+10)

	// 从环境变量映射读取
	for key, envVar := range configEnvMap {
		result[key] = os.Getenv(envVar)
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
	// 记忆禁止描述
	result["memory_forbidden_description"] = getConfigString(cfg, "memory.forbidden_memory_definition.description", "")

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
		var modelsCount *int

		// models 子载荷
		var newModels []map[string]any
		if modelsVal, ok := params["models"]; ok && modelsVal != nil {
			// 将 []any 转为 []map[string]any
			parsed, err := buildModelsDefaultsFromFrontend(modelsVal)
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
			count := len(newModels)
			modelsCount = &count
		}

		// 回包给前端
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
		}
		if modelsCount != nil {
			result["models_count"] = *modelsCount
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
// 从 config.yaml 读取 models.defaults 配置返回给前端。
func handleModelsList(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
	cfg, err := loadAppConfig()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("models.list 加载配置失败")
		return map[string]any{"models": []any{}}, nil
	}

	models := getConfigAny(cfg, "models.defaults", []any{})
	return map[string]any{"models": models}, nil
}

// handleChannelGet 处理 channel.get 请求。
//
// 返回当前渠道配置，Web 渠道始终启用。
func handleChannelGet(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
	return map[string]any{
		"channels": map[string]any{
			"web": map[string]any{"enabled": true},
		},
	}, nil
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
func handleSessionCreate(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
	sessionID := MakeSessionID()
	if params != nil {
		if sid, ok := params["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
	}

	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}

	// 写入 metadata.json
	meta := map[string]any{
		"session_id": sessionID,
		"created_at": time.Now().Format(time.RFC3339),
	}
	if params != nil {
		if name, ok := params["name"].(string); ok {
			meta["name"] = name
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
// 删除会话目录。
func handleSessionDelete(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
	sessionID, _ := params["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id 不能为空")
	}

	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)

	if err := os.RemoveAll(sessionDir); err != nil {
		return nil, fmt.Errorf("删除会话目录失败: %w", err)
	}

	return map[string]any{"ok": true}, nil
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
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
func handleChatInterrupt() RPCHandlerFunc {
	return func(_ context.Context, params map[string]any, sessionID string) (map[string]any, error) {
		return map[string]any{"accepted": true, "session_id": sessionID, "intent": "interrupt"}, nil
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
// 对齐 Python _flatten_modes_team_for_config_panel(raw)。
func flattenTeamConfig(cfg map[string]any, result map[string]any) {
	team := getConfigAny(cfg, "modes.team", nil)
	if team == nil {
		return
	}
	teamMap, ok := team.(map[string]any)
	if !ok {
		return
	}
	for k, v := range teamMap {
		// 仅当结果中对应键为空时才用 team 配置填充
		if existing, exists := result[k]; !exists || existing == "" {
			result[k] = v
		}
	}
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
