package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmSchema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	permrpc "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	cm "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	commandParser "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler/command_parser"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageHandler 消息处理器接口。
//
// 用于 connection.ack 等消息的推送，对齐 Python MessageHandler.publish_robot_messages。
type MessageHandler interface {
	// PublishRobotMessages 通过消息管道推送机器人消息
	PublishRobotMessages(ctx context.Context, msg *schema.Message) error
}

// WebHandlersBindParams WebHandlers 绑定参数。
//
// Python: WebHandlersBindParams (app_web_handlers.py L512-522)，
// 将 channel、agent_client、message_handler、channel_manager、回调等统一注入。
type WebHandlersBindParams struct {
	// Channel Web 通道实例
	Channel *WebChannel
	// AgentClient AgentServer 客户端，可为 nil
	AgentClient *routing.AgentClient
	// MessageHandler 消息处理器，用于 ack 优先路径，可为 nil
	// Python: bind.message_handler，优先用 mh.publish_robot_messages 发 ack
	MessageHandler MessageHandler
	// ChannelManager 通道管理器，可为 nil
	ChannelManager *cm.ChannelManager
	// OnConfigSaved 配置保存回调
	OnConfigSaved OnConfigSavedFunc
	// CryptoProvider 加密/解密提供者（注入通道，测试用；生产装配时传入全局快照）
	CryptoProvider crypto.CryptoProvider
	// HeartbeatService 心跳服务，可为 nil
	// 11.11 已回填：注入后处理 heartbeat.get_conf / heartbeat.set_conf
	// gateway/heartbeat.GatewayHeartbeatService 隐式实现
	HeartbeatService HeartbeatConfService
	// CronController 定时任务控制器，可为 nil
	// 11.10 已回填：注入后处理 cron.job.* 8 个方法
	// gateway/cron.CronController 隐式实现
	CronController CronJobController
	// UpdaterService 更新服务，可为 nil
	//
	// ⚠️ 未接线：`updater.*` 当前一律回默认（spec §10），注入本字段不会生效。
	// 恢复升级子系统（gateway/common/updater.py）时须把它改成类型化字段并真正接线，
	// 否则注入会被静默忽略。
	UpdaterService any
}

// HeartbeatConfService web 端点所需心跳服务接口。
// gateway/heartbeat.GatewayHeartbeatService 隐式实现（对齐 CronJobController 先例）。
//
// spec 接口签名细化（相对 spec §三/§六的 map[string]string → map[string]any）：
// Python set_heartbeat_conf 不做值类型转换原样存（数值 1320 存 int，
// 由 normalize_active_hours 回转 "22:00"）；若 Go 侧在 web 层字符串化会
// 破坏该回转链（"1320" 字符串直通 → parseHM 失败 → always-active 回退）。
type HeartbeatConfService interface {
	// GetHeartbeatConf 返回配置摘要（every/target/active_hours）
	GetHeartbeatConf() map[string]any
	// SetHeartbeatConf 更新配置并按需热重启
	SetHeartbeatConf(every *float64, target *string, activeHours map[string]any) error
}

// CronJobController web 端点所需 CronController 接口。
// gateway/cron.CronController 隐式实现。
// 对齐 Python: bind.cron_controller 的 list/get/create/update/delete/toggle/preview/run_now 方法面。
type CronJobController interface {
	// ListJobs 列出全部任务
	ListJobs() ([]map[string]any, error)
	// GetJob 获取单个任务
	GetJob(jobID string) (map[string]any, error)
	// CreateJob 创建任务
	CreateJob(params map[string]any) (map[string]any, error)
	// UpdateJob 更新任务
	UpdateJob(jobID string, patch map[string]any) (map[string]any, error)
	// DeleteJob 删除任务
	DeleteJob(jobID string) (bool, error)
	// ToggleJob 启停任务
	ToggleJob(jobID string, enabled bool) (map[string]any, error)
	// PreviewJob 预览下 N 次执行
	PreviewJob(jobID string, count int) ([]map[string]any, error)
	// RunNow 立即执行
	RunNow(jobID string) (string, error)
	// JobMetadata 返回任务 schema 元数据（cron.job.meta 用）
	JobMetadata() map[string]any
}

// EventSender 事件推送回调，用于向 WebSocket 客户端推送事件帧。
type EventSender func(event string, payload map[string]any)

// OnConfigSavedFunc 配置保存回调函数类型。
// Python: _on_config_saved(updated_env_keys, env_updates=..., config_payload=...)。
//
// 参数说明：
//   - updatedKeys: 变更的环境变量键集合
//   - envUpdates: 增量环境变量更新
//   - configPayload: 完整配置快照
type OnConfigSavedFunc func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error

// ──────────────────────────── 枚举 ────────────────────────────

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
	// wsErrFeishuNoTarget 飞书通道未配置推送目标（上游 error/code 同值）
	wsErrFeishuNoTarget = "feishuNoTarget"
	// WsErrUnsupported 能力不支持（如原生文件选择器不可用）
	WsErrUnsupported = "UNSUPPORTED"
)

// logComponent 本文件日志组件
const logComponent = logger.ComponentGateway

// maxTeamsConfigPanel 配置面板最大团队数。
// Python: _flatten_modes_team_for_config_panel 中 range(10)。
const maxTeamsConfigPanel = 10

// ──────────────────────────── 全局变量 ────────────────────────────

// configEnvMap 前端配置键名 → 环境变量名映射。
//
// Python: _CONFIG_SET_ENV_MAP，共 47 个条目。
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

// RegisterWebHandlers 把全部 RPC 方法注册到 WebChannel 上。
//
// Python: app_web_handlers.py 中 _register_web_handlers(bind)；
// 注册表就是 WebChannel._method_handlers（web_connect.py:227/371-387），
// 不存在独立的 dispatcher 类型。bind.Channel 为空时（测试场景）新建一个裸通道。
func RegisterWebHandlers(bind *WebHandlersBindParams) *WebChannel {
	wc := (*WebChannel)(nil)
	if bind != nil && bind.Channel != nil {
		wc = bind.Channel
	} else {
		wc = NewWebChannel(WebChannelConfig{}, nil, nil)
	}

	// 从 bind 中提取事件发送器和配置保存回调（bind 可为 nil）
	var sendEvent EventSender
	var onConfigSaved OnConfigSavedFunc
	var cryptoProv crypto.CryptoProvider
	var channelMgr *cm.ChannelManager
	var agentClient *routing.AgentClient
	var messageHandler MessageHandler
	var channel *WebChannel
	var cronController CronJobController
	var heartbeatSvc HeartbeatConfService
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
		cronController = bind.CronController
		heartbeatSvc = bind.HeartbeatService
	}

	// ─── 本地实现方法 ───
	wc.registerMethod("config.get", configGet(wc, cryptoProv), false)
	wc.registerMethod("config.set", configSet(wc, sendEvent, onConfigSaved, cryptoProv), false)
	wc.registerMethod("models.list", modelsList(wc), false)
	wc.registerMethod("channel.get", channelGet(wc, channelMgr), false)
	wc.registerMethod("session.list", sessionList(wc), false)
	wc.registerMethod("session.create", sessionCreate(wc), false)

	// ─── 历史辅助方法 ───
	wc.registerMethod("history.get", historyGet(wc), false)

	// ─── config 辅助方法 ───
	wc.registerMethod("config.save_all", configSaveAll(wc, sendEvent, onConfigSaved, cryptoProv), false)
	wc.registerMethod("config.validate_model", stubHandler(wc, map[string]any{"ok": true}), false)

	// ─── models 辅助方法 ───
	wc.registerMethod("models.replace_all", modelsReplaceAll(wc, onConfigSaved, cryptoProv), false)
	wc.registerMethod("models.validate", modelsValidate(wc), false)

	// ─── session 辅助方法 ───

	// ─── chat 类方法（本地 ack 响应，消息转发由两层架构第一层处理）───
	wc.registerMethod("chat.send", chatSend(wc), false)
	wc.registerMethod("chat.resume", chatResume(wc), false)
	wc.registerMethod("chat.interrupt", chatInterrupt(wc), false)
	wc.registerMethod("chat.user_answer", chatUserAnswer(wc), false)

	// ─── 路径配置 ───
	wc.registerMethod("path.get", stubHandler(wc, map[string]any{"path": ""}), false)
	wc.registerMethod("path.set", stubHandler(wc, map[string]any{"ok": true}), false)

	// ─── 内存信息 ───
	// Python: _memory_compute (app_web_handlers.py L1414-1427)，psutil 真实采集
	wc.registerMethod("memory.compute", memoryCompute(wc), false)

	// ─── 语言配置 ───
	wc.registerMethod("locale.get_conf", stubHandler(wc, map[string]any{"preferred_language": "zh"}), false)
	wc.registerMethod("locale.set_conf", stubHandler(wc, map[string]any{"ok": true}), false)

	// ─── 探活配置（health_check，heartbeat.* 为上游 deprecated 别名） ───
	// Python: app_web_handlers.py:5839-5842 把 health_check.* 与 heartbeat.* 注册到同一对 handler。
	if heartbeatSvc != nil {
		wc.registerMethod("health_check.get_conf", heartbeatGetConf(wc, heartbeatSvc), false)
		wc.registerMethod("health_check.set_conf", heartbeatSetConf(wc, heartbeatSvc, agentClient), false)
		wc.registerMethod("heartbeat.get_conf", heartbeatGetConf(wc, heartbeatSvc), false)
		wc.registerMethod("heartbeat.set_conf", heartbeatSetConf(wc, heartbeatSvc, agentClient), false)
	} else {
		confStub := stubHandler(wc, map[string]any{
			"every": 0, "target": "", "active_hours": map[string]any{},
		})
		setStub := stubHandler(wc, map[string]any{"ok": true})
		wc.registerMethod("health_check.get_conf", confStub, false)
		wc.registerMethod("health_check.set_conf", setStub, false)
		wc.registerMethod("heartbeat.get_conf", confStub, false)
		wc.registerMethod("heartbeat.set_conf", setStub, false)
	}

	// ─── 更新器 ───
	updaterStub := stubHandler(wc, map[string]any{"status": "up_to_date"})
	wc.registerMethod("updater.check", updaterStub, false)
	wc.registerMethod("updater.download", updaterStub, false)
	wc.registerMethod("updater.get_status", updaterStub, false)
	updaterDefaultConf := stubHandler(wc, defaultUpdaterRuntimeConfig())
	wc.registerMethod("updater.get_conf", updaterDefaultConf, false)
	wc.registerMethod("updater.set_conf", updaterDefaultConf, false)
	wc.registerMethod("updater.reset_source", updaterDefaultConf, false)
	wc.registerMethod("updater.upgrade", stubHandler(wc, map[string]any{"state": "idle"}), false)

	// ─── 外部 CLI（codex / claude，B1b-5） ───
	// Python: app_web_handlers.py:5496-5502
	wc.registerMethod("external_cli.detect", externalCLIDetect(wc), false)
	wc.registerMethod("external_cli.validate_paths", externalCLIValidatePaths(wc), false)
	wc.registerMethod("external_cli.prune_path_histories", externalCLIPrunePathHistories(wc), false)
	wc.registerMethod("external_cli.codex_install_status", stubHandler(wc, defaultExternalCLIInstallStatus()), false)
	wc.registerMethod("external_cli.install_status", externalCLIInstallStatus(wc), false)
	wc.registerMethod("external_cli.install", stubHandler(wc, map[string]any{"installing": false}), false)
	wc.registerMethod("external_cli.custom_agent.remove", stubHandler(wc, map[string]any{"cli_agent": ""}), false)

	// ─── 杂项本地方法（B1b-4） ───
	// Python: app_web_handlers.py:5676-5684 / 5775 / 5637 / 5818
	wc.registerMethod("connection.status", connectionStatus(wc, agentClient), false)
	wc.registerMethod("commands.list", commandsList(wc), false)
	wc.registerMethod("config.moa_status", configMoaStatus(wc), false)
	wc.registerMethod("chat.swarmflow_reply", chatSwarmflowReply(wc), false)

	// ─── session 薄代理（B1b-7） ───
	// Python: app_web_handlers.py:5692-5693（两个都是 proxy_unary_request 薄代理）
	wc.registerMethod("session.rename", sessionProxyForward(wc, agentClient, "session.rename"), false)
	wc.registerMethod("session.plan_status", sessionProxyForward(wc, agentClient, "session.plan_status"), false)

	// ─── 原生文件选择（B1b-6） ───
	// Python: app_web_handlers.py:3948-3990（path.select_files/select_directory 是薄代理，不在本期）
	wc.registerMethod("path.select_file", pathSelectFile(wc), false)

	// ─── 钩子 ───
	wc.registerMethod("hooks.list", stubHandler(wc, map[string]any{"hooks": []any{}}), false)

	// ─── 权限 ───
	// Python: owner_scopes.get/set 在 web_handlers 中直接处理（不经 E2A / config_rpc）
	// Python: tools/rules/approval_overrides 通过 _forward_permissions_to_agent 转发到 AgentServer
	wc.registerMethod("permissions.owner_scopes.get", permissionsOwnerScopesGet(wc), false)
	wc.registerMethod("permissions.owner_scopes.set", permissionsOwnerScopesSet(wc), false)
	wc.registerMethod("permissions.tools.get", permissionsForwardToAgent(wc, agentClient, "permissions.tools.get"), false)
	wc.registerMethod("permissions.tools.set", permissionsForwardToAgent(wc, agentClient, "permissions.tools.set"), false)
	wc.registerMethod("permissions.tools.update", permissionsForwardToAgent(wc, agentClient, "permissions.tools.update"), false)
	wc.registerMethod("permissions.tools.delete", permissionsForwardToAgent(wc, agentClient, "permissions.tools.delete"), false)
	wc.registerMethod("permissions.rules.get", permissionsForwardToAgent(wc, agentClient, "permissions.rules.get"), false)
	wc.registerMethod("permissions.rules.create", permissionsForwardToAgent(wc, agentClient, "permissions.rules.create"), false)
	wc.registerMethod("permissions.rules.update", permissionsForwardToAgent(wc, agentClient, "permissions.rules.update"), false)
	wc.registerMethod("permissions.rules.delete", permissionsForwardToAgent(wc, agentClient, "permissions.rules.delete"), false)
	wc.registerMethod("permissions.approval_overrides.get", permissionsForwardToAgent(wc, agentClient, "permissions.approval_overrides.get"), false)
	wc.registerMethod("permissions.approval_overrides.delete", permissionsForwardToAgent(wc, agentClient, "permissions.approval_overrides.delete"), false)

	// ─── 禁止记忆 ───
	wc.registerMethod("memory.forbidden.get", memoryForbiddenGet(wc), false)
	wc.registerMethod("memory.forbidden.set", memoryForbiddenSet(wc), false)

	// ─── IM 平台配置 ───
	// Python: app_web_handlers.py:5844-5863（9 组 channel.<p>.{get,set}_conf）
	registerChannelConfFamily(wc, channelMgr, agentClient)
	wc.registerMethod("channel.wechat.get_login_ui", stubHandler(wc, map[string]any{}), false)
	wc.registerMethod("channel.wechat.unbind", stubHandler(wc, map[string]any{}), false)

	// ─── 定时任务 ───
	// 11.10 已回填：8 个 cron.job.* 端点接真实 CronController
	// （对齐 Python register_method：list/get/create/update/delete/toggle/preview/run_now）
	if cronController != nil {
		cc := cronController
		wc.registerMethod("cron.job.list", cronJobList(wc, cc), false)
		wc.registerMethod("cron.job.get", cronJobGet(wc, cc), false)
		wc.registerMethod("cron.job.create", cronJobCreate(wc, cc), false)
		wc.registerMethod("cron.job.update", cronJobUpdate(wc, cc), false)
		wc.registerMethod("cron.job.delete", cronJobDelete(wc, cc), false)
		wc.registerMethod("cron.job.toggle", cronJobToggle(wc, cc), false)
		wc.registerMethod("cron.job.preview", cronJobPreview(wc, cc), false)
		wc.registerMethod("cron.job.run_now", cronJobRunNow(wc, cc), false)
		wc.registerMethod("cron.job.meta", cronJobMeta(wc, cc), false)
	} else {
		cronListStub := stubHandler(wc, map[string]any{"jobs": []any{}})
		cronOkStub := stubHandler(wc, map[string]any{"ok": true})
		wc.registerMethod("cron.job.list", cronListStub, false)
		wc.registerMethod("cron.job.create", cronOkStub, false)
		wc.registerMethod("cron.job.update", cronOkStub, false)
		wc.registerMethod("cron.job.delete", cronOkStub, false)
		wc.registerMethod("cron.job.meta", stubHandler(wc, map[string]any{}), false)
	}

	// ─── 运行时 ───

	// ─── 转发方法（全部 stub）───
	forwardStubs := map[string]map[string]any{
		"initialize":        {},
		"acp.tool_response": {},
		// 团队
		"team.delete":      {},
		"team.snapshot":    {},
		"team.history.get": {},
		// 历史：history.get 有本地 handler，不在 stub 中
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
		// ClawHub令牌
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
		wc.registerMethod(method, stubHandler(wc, payload), false)
	}

	// ─── 注册 onConnect 钩子：发送 connection.ack ───
	// Python: WebChannel._connection_handler 中 _connect_hooks 逻辑
	if channel != nil {
		ch := channel // 捕获循环变量
		ac := agentClient
		mh := messageHandler
		ch.OnConnect(func(_ *websocket.Conn) error {
			// AgentClient 未就绪时跳过 ack（对齐 Python: 仅 debug 日志，不断开连接）
			if ac == nil || !ac.ServerReady() {
				logger.Debug(logComponent).Msg("AgentClient not ready, skipping connection.ack")
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
			// Python: L592-596: 优先用 mh.publish_robot_messages，否则 fallback channel.send
			if mh != nil {
				if err := mh.PublishRobotMessages(context.Background(), ackMsg); err != nil {
					logger.Warn(logComponent).Err(err).Msg("MessageHandler.PublishRobotMessages failed to send connection.ack, fallback channel.Send")
					_ = ch.Send(context.Background(), ackMsg)
				}
			} else {
				if err := ch.Send(context.Background(), ackMsg); err != nil {
					logger.Warn(logComponent).Err(err).Msg("failed to send connection.ack")
				}
			}
			return nil
		})
	}

	return wc
}

// MakeSessionID 生成会话标识。
//
// 格式：sess_{hex_timestamp}_{6_random_hex}
// Python: _make_session_id() 和前端 generateSessionId。
func MakeSessionID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("sess_%s_%x", ts, suffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strOrEmpty 将任意值转为字符串，nil 或无效值返回空串。
// Python: str(val) or ""。
func strOrEmpty(val any) string {
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	s := fmt.Sprintf("%v", val)
	// Go 的 fmt.Sprintf("%v", nil指针) 会产生 "<nil>" 字符串，需要额外过滤
	if s == "<nil>" {
		return ""
	}
	return s
}

// memoryForbiddenGet 处理 memory.forbidden.get 请求。
//
// 从 config.yaml 读取 memory.forbidden_memory_definition 并返回。
// Python: _memory_forbidden_get (app_web_handlers.py:6381)。
func memoryForbiddenGet(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		cfg, err := loadAppConfig()
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("memory.forbidden.get load config failed")
			wc.sendResponse(ws, reqID, true, map[string]any{}, "", "")
			return
		}
		// Python: cfg.get("memory", {}).get("forbidden_memory_definition", {})
		payload := getConfigAny(cfg, "memory.forbidden_memory_definition", map[string]any{})
		if m, ok := payload.(map[string]any); ok {
			wc.sendResponse(ws, reqID, true, m, "", "")
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{}, "", "")
	}
}

// memoryForbiddenSet 处理 memory.forbidden.set 请求。
//
// 接收 params，调用 updateMemoryForbiddenInConfig 写回。
// Python: _memory_forbidden_set (app_web_handlers.py:6403) + update_memory_forbidden_in_config (config.py)。
func memoryForbiddenSet(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		if err := updateMemoryForbiddenInConfig(params); err != nil {
			logger.Error(logComponent).Err(err).Msg("memory.forbidden.set write back config failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"ok": true}, "", "")
	}
}

// stubHandler 返回固定 stub 响应的处理函数。
//
// 用于尚未实现转发的 RPC 方法，返回占位数据使前端不报错。
func stubHandler(wc *WebChannel, payload map[string]any) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// configGet 处理 config.get 请求。
//
// 全量实现：读取 configEnvMap 中 47 个环境变量 + config.yaml 补充字段 + app_version。
// Python: config_get_handler 中 ~65 字段的返回。
// cryptoProv 用于解密 api_key/token 等敏感字段。
func configGet(wc *WebChannel, cryptoProv crypto.CryptoProvider) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		ctx := context.Background()
		p := resolveCrypto(cryptoProv)
		result := make(map[string]any, len(configEnvMap)+10)

		// 从环境变量映射读取
		for key, envVar := range configEnvMap {
			val := os.Getenv(envVar)
			// Python: 对包含 api_key 或 token 的键解密（app_web_handlers.py L611-614）
			// 解密失败保留原值 + Warn（Go error 签名；Python 整体 except 降级 → Go 逐字段跳过）
			if p != nil && val != "" && isSensitiveKey(key) {
				if dec, decErr := p.Decrypt(ctx, val); decErr == nil {
					val = dec
				} else {
					logger.Warn(logComponent).
						Str("key", key).
						Err(decErr).
						Msg("config.get decrypt failed, keep raw value")
				}
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
				Msg("config.get failed to load config.yaml, using defaults")
		}

		if err != nil {
			// 异常兜底默认值（对齐 Python except Exception 时 payload.setdefault，仅当键不存在时设置）
			if _, ok := result["context_engine_enabled"]; !ok {
				result["context_engine_enabled"] = "false"
			}
			if _, ok := result["kv_cache_affinity_enabled"]; !ok {
				result["kv_cache_affinity_enabled"] = "false"
			}
			if _, ok := result["permissions_enabled"]; !ok {
				result["permissions_enabled"] = "false"
			}
			if _, ok := result["memory_forbidden_enabled"]; !ok {
				result["memory_forbidden_enabled"] = "false"
			}
			if _, ok := result["memory_forbidden_description"]; !ok {
				result["memory_forbidden_description"] = ""
			}
		} else {
			// 上下文引擎启用
			result["context_engine_enabled"] = getConfigString(cfg, "react.context_engine_config.enabled", "false")
			// KV 缓存亲和启用
			result["kv_cache_affinity_enabled"] = getConfigString(cfg, "react.context_engine_config.enable_kv_cache_release", "false")
			// 权限启用
			result["permissions_enabled"] = getConfigString(cfg, "permissions.enabled", "false")
			// 记忆禁止启用
			result["memory_forbidden_enabled"] = getConfigString(cfg, "memory.forbidden_memory_definition.enabled", "false")
			// 记忆禁止描述：返回 dict（对齐 Python getConfigAny 而非 getConfigString）
			result["memory_forbidden_description"] = getConfigAny(cfg, "memory.forbidden_memory_definition.description", map[string]any{})
		}
		// skill_create: 环境变量优先，fallback config.yaml（setdefault 语义：仅当键不存在时设置）
		if _, ok := result["skill_create"]; !ok {
			if envVal := os.Getenv("SKILL_CREATE"); envVal != "" {
				result["skill_create"] = normalizeTruthy(envVal)
			} else if err == nil {
				result["skill_create"] = getConfigString(cfg, "react.evolution.skill_create", "false")
			} else {
				result["skill_create"] = "false"
			}
		}
		// evolution_auto_scan: 环境变量优先，fallback config.yaml（setdefault 语义：仅当键不存在时设置）
		if _, ok := result["evolution_auto_scan"]; !ok {
			if envVal := os.Getenv("EVOLUTION_AUTO_SCAN"); envVal != "" {
				result["evolution_auto_scan"] = normalizeTruthy(envVal)
			} else if err == nil {
				result["evolution_auto_scan"] = getConfigString(cfg, "react.evolution.auto_scan", "false")
			} else {
				result["evolution_auto_scan"] = "false"
			}
		}

		// 默认值填充（setdefault 语义：仅当键不存在或为空时）
		if v, ok := result["free_search_ddg_enabled"]; !ok || v == "" {
			result["free_search_ddg_enabled"] = "false"
		}
		if v, ok := result["free_search_bing_enabled"]; !ok || v == "" {
			result["free_search_bing_enabled"] = "false"
		}

		// 展平 team 配置
		flattenTeamConfig(cfg, result)

		wc.sendResponse(ws, reqID, true, result, "", "")
	}
}

// resolveCrypto 解析 crypto 提供者：注入优先（BindParams 测试通道），未注入时查全局。
// 对齐 Python 每次请求时取 ExtensionRegistry.get_instance().get_crypto_provider()——
// 插件加载后生效的语义由全局查找路径承接。
func resolveCrypto(injected crypto.CryptoProvider) crypto.CryptoProvider {
	if injected != nil {
		return injected
	}
	return crypto.GetCryptoProvider()
}

// isSensitiveKey 判断配置键是否为敏感字段（需要解密）。
//
// Python: "api_key" in key.lower() or "token" in key.lower()（app_web_handlers.py L612）。
// 先 ToLower 再匹配，与 config_apply.go 的加密路径对称——否则大写键
// （如 OPENAI_API_KEY）会"set 时加密、get 时不解密"，产生永远解不开的密文。
func isSensitiveKey(key string) bool {
	kl := strings.ToLower(key)
	return strings.Contains(kl, "api_key") || strings.Contains(kl, "token")
}

// normalizeTruthy 对布尔型环境变量做 truthy 归一化。
// Python: val.lower() in ("true", "1", "yes") → "true"，否则 "false"。
func normalizeTruthy(val string) string {
	lower := strings.ToLower(strings.TrimSpace(val))
	if lower == "true" || lower == "1" || lower == "yes" {
		return "true"
	}
	return "false"
}

// configSet 处理 config.set 请求。
//
// 完整实现：参数映射→provider校验→YAML键写入→agents/team写入→os.Setenv+.env持久化→回包→触发热重载。
// Python: _config_set (app_web_handlers.py L870-895)。
func configSet(wc *WebChannel, sendEvent EventSender, onConfigSaved OnConfigSavedFunc, cryptoProv crypto.CryptoProvider) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		ctx := context.Background()
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		p := resolveCrypto(cryptoProv)

		// 步骤1-5: applyConfigPayload 统一处理
		envUpdates, yamlUpdated, err := ApplyConfigPayload(ctx, params, p)
		if err != nil {
			// 区分 BadRequest 和 InternalError
			if _, ok := err.(*ConfigBadRequest); ok {
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
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

		wc.sendResponse(ws, reqID, true, map[string]any{
			"updated":                 updatedParamKeys,
			"applied_without_restart": appliedWithoutRestart,
		}, "", "")
	}
}

// configSaveAll 处理 config.save_all 请求。
//
// 批量保存配置面板变更并触发热重载。
// Python: _config_save_all (app_web_handlers.py L1086-1151)。
//
// 支持子载荷：config、models、agents、team。
func configSaveAll(wc *WebChannel, sendEvent EventSender, onConfigSaved OnConfigSavedFunc, cryptoProv crypto.CryptoProvider) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		ctx := context.Background()
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		p := resolveCrypto(cryptoProv)

		envUpdates := make(map[string]string)
		yamlUpdated := make([]string, 0)
		var modelsCount any // 默认 nil，对齐 Python: null

		// models 子载荷
		var newModels []map[string]any
		if modelsVal, ok := params["models"]; ok && modelsVal != nil {
			// 将 []any 转为 []map[string]any
			parsed, err := buildModelsDefaultsFromFrontend(ctx, modelsVal, p)
			if err != nil {
				if _, ok := err.(*ConfigBadRequest); ok {
					wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
					return
				}
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
				return
			}
			newModels = parsed
		}

		// config 子载荷 + agents/team 合并
		configParams := make(map[string]any)
		if rawConfig, ok := params["config"]; ok && rawConfig != nil {
			configMap, ok := rawConfig.(map[string]any)
			if !ok {
				wc.sendResponse(ws, reqID, false, nil, "config must be object", WsErrBadRequest)
				return
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
			appliedEnv, appliedYAML, err := ApplyConfigPayload(ctx, configParams, p)
			if err != nil {
				if _, ok := err.(*ConfigBadRequest); ok {
					wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
					return
				}
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
				return
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
					Msg("failed to write models.defaults")
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
				return
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

		wc.sendResponse(ws, reqID, true, result, "", "")
	}
}

// modelsList 处理 models.list 请求。
//
// Python: _models_list (app_web_handlers.py L1023-1055)。
// 数据流对齐 Python：get_config() → get_default_models(config) → _decrypt_model_entries
// ——解密发生在 config 层（GetDefaultModels 内部经 decryptModelEntries 查全局 provider），
// handler 层零解密（对齐 Python handler 不持 crypto）。api_key 解密失败由
// decryptModelEntries 保留原值（对齐 Python except: pass）。
func modelsList(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		// 对齐 Python: config = get_config(); models = get_default_models(config)
		models := config.GetDefaultModels(nil)
		if len(models) == 0 {
			wc.sendResponse(ws, reqID, true, map[string]any{"models": []any{}, "active_model": ""}, "", "")
			return
		}

		// 展平为前端格式：从嵌套的 model_client_config/model_config_obj 提取字段
		result := make([]map[string]any, 0, len(models))
		for idx, entry := range models {
			flat := map[string]any{
				"origin_index": idx,
			}

			// 从 model_client_config 提取
			if mcc, ok := entry["model_client_config"].(map[string]any); ok {
				if v, ok := mcc["model_name"].(string); ok {
					flat["model_name"] = v
				}
				if v, ok := mcc["api_base"].(string); ok {
					flat["api_base"] = v
				}
				if v, ok := mcc["api_key"].(string); ok {
					// 已由 decryptModelEntries 解密（全局 provider），此处直接取值
					flat["api_key"] = v
				}
				if v, ok := mcc["client_provider"].(string); ok {
					flat["model_provider"] = v
				}
			}

			// 从 model_config_obj 提取（上下文窗口与输出上限都归这里，对齐上游）
			if mco, ok := entry["model_config_obj"].(map[string]any); ok {
				// 上下文窗口：支持 256k/1m 字符串形态，0 表示未配置不回填
				if n, perr := config.ParsePositiveTokens(mco["context_window"]); perr {
					flat["context_window"] = n
				}
				if n, perr := parseInt(mco["max_tokens"]); perr == nil && n > 0 {
					flat["max_tokens"] = n
				}
				// Python: mco.get("temperature", 0.95)
				temperature := 0.95
				if v, ok := mco["temperature"]; ok && v != nil {
					if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
						temperature = f
					}
				}
				flat["temperature"] = temperature
			}

			// 顶层字段
			if v, ok := entry["is_default"]; ok {
				flat["is_default"] = v
			}
			if v, ok := entry["alias"].(string); ok {
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

		wc.sendResponse(ws, reqID, true, map[string]any{"models": result, "active_model": activeModel}, "", "")
	}
}

// channelGet 处理 channel.get 请求。
//
// 从 ChannelManager 动态读取已启用的渠道列表。
// Python: channel_get_handler，返回数组 [{channel_id: cid}]。
func channelGet(wc *WebChannel, channelMgr *cm.ChannelManager) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		channels := make([]map[string]any, 0)
		if channelMgr != nil {
			for _, cid := range channelMgr.GetEnabledChannels() {
				channels = append(channels, map[string]any{"channel_id": cid})
			}
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"channels": channels}, "", "")
	}
}

// sessionList 处理 session.list 请求。
//
// 遍历会话目录，读取每个 session 的 metadata.json 返回列表。
// 支持分页：limit 和 offset 参数，对齐 Python (L1264-1293)。
func sessionList(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		sessionsDir := workspace.AgentSessionsDir()

		// 解析分页参数（对齐 Python L1264-1271）
		// Python 默认 limit=20，截断限制：limit = max(1, min(limit, 200))
		limit := 20
		offset := 0
		if params != nil {
			if v, ok := params["limit"]; ok {
				switch lv := v.(type) {
				case int:
					limit = lv
				case float64:
					limit = int(lv)
				}
			}
			if v, ok := params["offset"]; ok {
				switch ov := v.(type) {
				case int:
					offset = ov
				case float64:
					offset = int(ov)
				}
			}
		}
		// 截断限制：limit = max(1, min(limit, 200))
		if limit < 1 {
			limit = 1
		}
		if limit > 200 {
			limit = 200
		}

		entries, err := os.ReadDir(sessionsDir)
		if err != nil {
			if os.IsNotExist(err) {
				wc.sendResponse(ws, reqID, true, map[string]any{"sessions": []any{}, "total": 0, "limit": limit, "offset": offset}, "", "")
				return
			}
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("读取会话目录失败: %v", err), WsErrInternalError)
			return
		}

		var sessions []map[string]any
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			// heartbeat_ 前缀会话不参与列表展示
			// 对齐 Python session_metadata.py L31/L416 _HEARTBEAT_SESSION_PREFIX = "heartbeat_"
			// （偏差⑨：与 session_startup.go 既有常量 heartbeatSessionPrefix 语义一致；
			// dreaming sweeper 的无下划线 "heartbeat" 是另一处独立先例，不适用本过滤点）
			if strings.HasPrefix(entry.Name(), "heartbeat_") {
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

		// 分页截取（对齐 Python L1286-1291）
		// 审查修复 M-06：①负 offset clamp 到 0（对齐 Python max(0, offset)，
		// 原实现负值直通 sessions[offset:] 切片 panic）；②按 last_message_at
		// 降序排序（对齐 Python sessions.sort(key=lambda x: x.get(
		// "last_message_at", 0), reverse=True)——原 ReadDir 词法序每次都
		// 偏离 Python 展示序；缺键兜底 0，JSON 解码值为 float64）。
		sort.Slice(sessions, func(i, j int) bool {
			return sessionLastMessageAt(sessions[i]) > sessionLastMessageAt(sessions[j])
		})
		total := len(sessions)
		if offset < 0 {
			offset = 0
		}
		if offset > total {
			offset = total
		}
		sessions = sessions[offset:]
		if limit < len(sessions) {
			sessions = sessions[:limit]
		}

		wc.sendResponse(ws, reqID, true, map[string]any{"sessions": sessions, "total": total, "limit": limit, "offset": offset}, "", "")
	}
}

// sessionLastMessageAt 读取会话 metadata 的 last_message_at（缺键/非数值兜底 0）。
// 审查修复 M-06 辅助：对齐 Python x.get("last_message_at", 0)。
func sessionLastMessageAt(session map[string]any) float64 {
	v, ok := session["last_message_at"]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// sessionCreate 处理 session.create 请求。
//
// 创建会话目录和 metadata.json。
// Python: session_create_handler，要求 session_id 非空，已存在则 ALREADY_EXISTS。
func sessionCreate(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		// 要求 session_id（对齐 Python: 必填参数）
		sessionID, _ := params["session_id"].(string)
		if sessionID == "" {
			wc.sendResponse(ws, reqID, false, nil, "session_id 是必填项", WsErrBadRequest)
			return
		}

		sessionsDir := workspace.AgentSessionsDir()
		sessionDir := filepath.Join(sessionsDir, sessionID)

		// 检查是否已存在（对齐 Python: ALREADY_EXISTS）
		if _, err := os.Stat(sessionDir); err == nil {
			wc.sendResponse(ws, reqID, false, nil, "会话已存在", WsErrAlreadyExists)
			return
		}

		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("创建会话目录失败: %v", err), WsErrInternalError)
			return
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
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("写入 metadata.json 失败: %v", err), WsErrInternalError)
			return
		}

		wc.sendResponse(ws, reqID, true, map[string]any{"session_id": sessionID}, "", "")
	}
}

// Python: _models_replace_all_handler (app_web_handlers.py L1058-1084)。
func modelsReplaceAll(wc *WebChannel, onConfigSaved OnConfigSavedFunc, cryptoProv crypto.CryptoProvider) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		ctx := context.Background()
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		p := resolveCrypto(cryptoProv)

		modelsVal, ok := params["models"]
		if !ok || modelsVal == nil {
			wc.sendResponse(ws, reqID, false, nil, "models 是必填项", WsErrBadRequest)
			return
		}

		// 调用 buildModelsDefaultsFromFrontend（内部会调 merge + infer）
		newModels, err := buildModelsDefaultsFromFrontend(ctx, modelsVal, p)
		if err != nil {
			if _, ok := err.(*ConfigBadRequest); ok {
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}

		// 写入 config: models.defaults
		if err := updateDefaultModelsInConfig(newModels); err != nil {
			logger.Warn(logComponent).Err(err).Msg("write models.defaults failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}

		// 触发热重载（对齐 Python: _notify_config_saved_once(force=True)）
		NotifyConfigSavedOnce(onConfigSaved, nil, []string{"models.defaults"}, true)

		wc.sendResponse(ws, reqID, true, map[string]any{"count": len(newModels)}, "", "")
	}
}

// modelsValidate 处理 models.validate 请求。
//
// Python: _config_validate_model (app_web_handlers.py L897-1019)。
// 调用 LLM 验证 api_key 和 api_base 是否有效。
func modelsValidate(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params 是必填项", WsErrBadRequest)
			return
		}

		apiBase, _ := params["api_base"].(string)
		apiKey, _ := params["api_key"].(string)
		model, _ := params["model"].(string)
		modelProvider, _ := params["model_provider"].(string)

		// 校验必填字段
		if apiBase == "" {
			wc.sendResponse(ws, reqID, false, nil, "api_base 是必填项", WsErrBadRequest)
			return
		}
		if apiKey == "" {
			wc.sendResponse(ws, reqID, false, nil, "api_key 是必填项", WsErrBadRequest)
			return
		}
		if model == "" {
			wc.sendResponse(ws, reqID, false, nil, "model 是必填项", WsErrBadRequest)
			return
		}
		if modelProvider == "" {
			wc.sendResponse(ws, reqID, false, nil, "model_provider 是必填项", WsErrBadRequest)
			return
		}

		// 校验 model_provider 在可用列表中
		if modelProvider != "" && !isAvailableProvider(modelProvider) {
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("model_provider 必须是以下之一: %v", availableModelProviders), WsErrBadRequest)
			return
		}

		// api_base 归一（C-E1）：与请求构建侧共用 openai.NormalizeAPIBase
		// （strip 空白 / 尾斜杠 / 已带 /chat/completions）
		apiBase = openai.NormalizeAPIBase(apiBase)

		// verify_ssl（对齐 Python: bool(item.get("verify_ssl", False))，默认 false）
		verifySSL := false
		if v, ok := params["verify_ssl"].(bool); ok {
			verifySSL = v
		}

		// 构造 LLM 配置
		clientConfig := &llmSchema.ModelClientConfig{
			ClientProvider: modelProvider,
			APIKey:         apiKey,
			APIBase:        apiBase,
			Timeout:        60.0,
			MaxRetries:     3,
			VerifySSL:      verifySSL,
		}

		// 尝试 max_tokens=1 验证
		maxTokens1 := 1
		modelConfig := llmSchema.NewModelRequestConfig()
		modelConfig.ModelName = model
		modelConfig.MaxTokens = &maxTokens1
		zeroTemperature := 0.0
		modelConfig.Temperature = &zeroTemperature

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		modelInst, err := llm.NewModel(clientConfig, modelConfig)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("model_provider", modelProvider).Msg("models.validate createmodel failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrLLMError)
			return
		}

		messages := model_clients.NewMessagesParam(llmSchema.NewUserMessage("hi"))
		resp, err := modelInst.Invoke(ctx, messages)
		if err != nil {
			// 失败重试 max_tokens=16
			maxTokens16 := 16
			modelConfig.MaxTokens = &maxTokens16
			modelInst2, err2 := llm.NewModel(clientConfig, modelConfig)
			if err2 != nil {
				wc.sendResponse(ws, reqID, false, nil, err2.Error(), WsErrLLMError)
				return
			}
			resp, err = modelInst2.Invoke(ctx, messages)
			if err != nil {
				wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrLLMError)
				return
			}
		}

		// 检查响应内容非空
		if resp == nil || resp.Content.Text() == "" {
			wc.sendResponse(ws, reqID, false, nil, "模型返回空响应", WsErrLLMError)
			return
		}

		wc.sendResponse(ws, reqID, true, map[string]any{"ok": true, "model_provider": modelProvider}, "", "")
	}
}

// historyGet 处理 history.get 本地请求。
//
// 返回 ack 响应，实际历史数据由 AgentServer 转发路径提供。
// Python: (L1459-1466)。
func historyGet(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		payload := map[string]any{"accepted": true, "session_id": sessionID}
		if params != nil {
			// Python: if "session_id" in params → 原值透传（含空串）
			if sid, ok := params["session_id"]; ok {
				payload["session_id"] = sid
			}
			if _, ok := params["page_idx"]; ok {
				payload["page_idx"] = params["page_idx"]
			}
			if _, ok := params["cursor"]; ok {
				payload["cursor"] = params["cursor"]
			}
			if _, ok := params["limit"]; ok {
				payload["limit"] = params["limit"]
			}
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// chatSend 处理 chat.send 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
// Python: chat.send handler 仅返回 ack。
func chatSend(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, sessionID string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"accepted": true, "session_id": sessionID}, "", "")
	}
}

// chatResume 处理 chat.resume 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
func chatResume(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, sessionID string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"accepted": true, "session_id": sessionID}, "", "")
	}
}

// chatInterrupt 处理 chat.interrupt 请求。
//
// 返回即时 ack 响应，从 params 中提取 intent。
// Python: chat.interrupt handler，intent 默认 "interrupt"，可从 params 覆写。
// 消息转发由两层架构第一层（normAndForward）处理。
func chatInterrupt(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		payload := map[string]any{"accepted": true, "session_id": sessionID}
		if params != nil {
			if intent, ok := params["intent"].(string); ok && intent != "" {
				payload["intent"] = intent
			}
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// chatUserAnswer 处理 chat.user_answer 请求。
//
// 返回即时 ack 响应。消息转发由两层架构第一层（normAndForward）处理。
func chatUserAnswer(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		payload := map[string]any{"accepted": true, "session_id": sessionID}
		// Python: request_id 仅在非空字符串时透出（:4153-4155）
		if requestID, ok := params["request_id"].(string); ok && requestID != "" {
			payload["request_id"] = requestID
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// loadAppConfig 加载 config.yaml 配置。
//
// 封装 config.New + Load 流程，失败时返回空 map 而非 error，
// 调用方用默认值兜底。
// 对齐 Python get_config()（含 _normalize_config——2026-10-28 审查接线）。
func loadAppConfig() (map[string]any, error) {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
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
// Python: _flatten_modes_team_for_config_panel (app_web_handlers.py L380-482)。
// 遍历最多 10 个 team，提取 team_{idx}_* 前缀字段和 agent 详情。
// flattenTeamConfig 展平 modes.team 配置为前端 config panel 格式。
//
// Python: _flatten_modes_team_for_config_panel (app_web_handlers.py L380-482)：
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
	// Python: L390-396
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
	// Python: L398-403
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
	// Python: L405-417
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
	// Python: L419-470
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

// 5. 空值写为 KEY=
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

// ──────────────────────────── 权限 RPC 处理函数 ────────────────────────────

// permissionsOwnerScopesGet 处理 permissions.owner_scopes.get 请求。
// Python: _permissions_owner_scopes_get (app_web_handlers.py L2275-2283)
// 直接从配置读取 owner_scopes，不经 E2A / config_rpc。
func permissionsOwnerScopesGet(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		// Python: get_permissions_owner_scopes() — get_config()（normalize 后）
		cfgPath := workspace.ConfigFile()
		// 2026-10-28 审查接线：改走 Load+WithNormalize，对齐 Python get_config()
		// （此前 Raw() 为原始视图；permissions 键不受 normalize 影响，行为无差，
		// 但统一读链入口，消除与 Python 的视图分歧）
		cfg, err := config.New(cfgPath, config.WithNormalize(config.NormalizeConfig))
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("permissions.owner_scopes.get load config failed")
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("读取 owner_scopes 配置失败: %v", err), WsErrInternalError)
			return
		}
		raw, err := cfg.Load()
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("permissions.owner_scopes.get read config failed")
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("读取 owner_scopes 配置失败: %v", err), WsErrInternalError)
			return
		}
		permCfg, _ := raw["permissions"].(map[string]any)
		if permCfg == nil {
			permCfg = map[string]any{}
		}
		ownerScopes, _ := permCfg["owner_scopes"].(map[string]any)
		if ownerScopes == nil {
			ownerScopes = map[string]any{}
		}
		denyGuidance, _ := permCfg["deny_guidance_message"].(string)
		wc.sendResponse(ws, reqID, true, map[string]any{
			"owner_scopes":          ownerScopes,
			"deny_guidance_message": denyGuidance,
		}, "", "")
	}
}

// permissionsOwnerScopesSet 处理 permissions.owner_scopes.set 请求。
// Python: _permissions_owner_scopes_set (app_web_handlers.py:6221)
// 直接调用 UpdatePermissionsOwnerScopesInConfig 统一写路径，不经 E2A / config_rpc。
func permissionsOwnerScopesSet(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		if _, err := permrpc.UpdatePermissionsOwnerScopesInConfig(params); err != nil {
			wc.sendResponse(ws, reqID, false, nil, fmt.Sprintf("更新 owner_scopes 配置失败: %v", err), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"ok": true}, "", "")
	}
}

// permissionsForwardToAgent 处理权限 RPC 请求，优先经 E2A 转发到 AgentServer；
// AgentServer 未就绪时本地执行（与 config_rpc 同源）。
// Python: _forward_permissions_to_agent (app_web_handlers.py:6237)
func permissionsForwardToAgent(wc *WebChannel, ac *routing.AgentClient, method string) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		// AgentServer 已就绪 → E2A 转发
		if ac != nil && ac.ServerReady() {
			env := e2a.E2AFromAgentFields("",
				e2a.WithFieldChannelID(""),
				e2a.WithFieldSessionID(sessionID),
				e2a.WithFieldReqMethod(method),
				e2a.WithFieldParams(params),
			)
			resp, err := ac.SendRequest(context.Background(), env)
			if err != nil {
				logger.Error(logComponent).Err(err).Str("method", method).Msg("permission RPC forwarding to AgentServer failed")
				// 转发失败，回退到本地 dispatch
			} else if resp != nil {
				if resp.OK {
					wc.sendResponse(ws, reqID, true, resp.Payload, "", "")
					return
				}
				errPayload := resp.Payload
				if errPayload == nil {
					errPayload = map[string]any{"error": "request failed"}
				}
				errMsg := "request failed"
				if e, ok := errPayload["error"].(string); ok && e != "" {
					errMsg = e
				}
				code := WsErrInternalError
				if c, ok := errPayload["code"].(string); ok && c != "" {
					code = c
				}
				wc.sendResponse(ws, reqID, false, nil, errMsg, code)
				return
			}
		}

		// AgentServer 未就绪或转发失败 → 本地 dispatch（对齐 Python: dispatch_permissions_config_request）
		ok, payload := permrpc.DispatchPermissionsConfigRequest(method, params)
		if ok {
			wc.sendResponse(ws, reqID, true, payload, "", "")
			return
		}
		errMsg := "request failed"
		code := WsErrInternalError
		if payload != nil {
			if e, ok := payload["error"].(string); ok && e != "" {
				errMsg = e
			}
			if c, ok := payload["code"].(string); ok && c != "" {
				code = c
			}
		}
		wc.sendResponse(ws, reqID, false, nil, errMsg, code)
	}
}

// cronJobList 处理 cron.job.list。
// Python: _cron_job_list (app_web_handlers.py L2050-2056)。
func cronJobList(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		jobs, err := cc.ListJobs()
		if err != nil {
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"jobs": jobs}, "", "")
	}
}

// cronJobGet 处理 cron.job.get。
// Python: _cron_job_get (L2058-2074)。id 必填；job 不存在 → NOT_FOUND。
func cronJobGet(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		job, err := cc.GetJob(jobID)
		if err != nil {
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		if job == nil {
			wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"job": job}, "", "")
	}
}

// cronJobCreate 处理 cron.job.create。
// Python: _cron_job_create (L2076-2089)。异常 → BAD_REQUEST。
func cronJobCreate(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		job, err := cc.CreateJob(params)
		if err != nil {
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"job": job}, "", "")
	}
}

// cronJobUpdate 处理 cron.job.update。
// Python: _cron_job_update (L2091-2115)。job not found → NOT_FOUND，其余 → BAD_REQUEST。
func cronJobUpdate(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		patch, _ := params["patch"].(map[string]any)
		if params["patch"] != nil && patch == nil {
			wc.sendResponse(ws, reqID, false, nil, "patch must be object", WsErrBadRequest)
			return
		}
		job, err := cc.UpdateJob(jobID, patch)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"job": job}, "", "")
	}
}

// cronJobDelete 处理 cron.job.delete。
// Python: _cron_job_delete (L2117-2136)。未删除 → NOT_FOUND。
func cronJobDelete(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		deleted, err := cc.DeleteJob(jobID)
		if err != nil {
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		if !deleted {
			wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"deleted": true}, "", "")
	}
}

// cronJobToggle 处理 cron.job.toggle。
// Python: _cron_job_toggle (L2138-2161)。enabled 必填；job not found → NOT_FOUND。
func cronJobToggle(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		enabledRaw, hasEnabled := params["enabled"]
		if !hasEnabled || enabledRaw == nil {
			wc.sendResponse(ws, reqID, false, nil, "enabled is required", WsErrBadRequest)
			return
		}
		enabled := false
		if b, ok := enabledRaw.(bool); ok {
			enabled = b
		}
		job, err := cc.ToggleJob(jobID, enabled)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"job": job}, "", "")
	}
}

// cronJobPreview 处理 cron.job.preview。
// Python: _cron_job_preview (L2163-2180)。count 默认 5；job not found → NOT_FOUND。
func cronJobPreview(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		count := 5
		if v, ok := params["count"].(float64); ok {
			count = int(v)
		}
		rows, err := cc.PreviewJob(jobID, count)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"next": rows}, "", "")
	}
}

// cronJobRunNow 处理 cron.job.run_now。
// Python: _cron_job_run_now (L2182-2198)。job not found → NOT_FOUND，其余 → INTERNAL_ERROR。
func cronJobRunNow(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		jobID := strings.TrimSpace(cronStrVal(params["id"]))
		if jobID == "" {
			wc.sendResponse(ws, reqID, false, nil, "id is required", WsErrBadRequest)
			return
		}
		runID, err := cc.RunNow(jobID)
		if err != nil {
			if strings.Contains(err.Error(), "job not found") {
				wc.sendResponse(ws, reqID, false, nil, "job not found", WsErrNotFound)
				return
			}
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"run_id": runID}, "", "")
	}
}

// cronJobMeta 处理 cron.job.meta，返回任务 schema 元数据。
// Python: _cron_job_meta (app_web_handlers.py:5111-5117) → CronController.job_metadata()。
func cronJobMeta(wc *WebChannel, cc CronJobController) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		if cc == nil {
			wc.sendResponse(ws, reqID, false, nil, "cron not available", WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, cc.JobMetadata(), "", "")
	}
}

// cronStrVal 安全取字符串。
func cronStrVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ──────────────────────────── 心跳配置 handler（11.11） ────────────────────────────

// heartbeatGetConf 处理 heartbeat.get_conf。
// Python: _heartbeat_get_conf (app_web_handlers.py L1513-1525)。
func heartbeatGetConf(wc *WebChannel, svc HeartbeatConfService) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		if svc == nil {
			// 上游 health_check.get_conf 与 heartbeat.get_conf 共用同一 handler，
			// 文案统一为 "health_check service not available"（app_web_handlers.py:4256）。
			wc.sendResponse(ws, reqID, false, nil, "health_check service not available", WsErrServiceUnavailable)
			return
		}
		wc.sendResponse(ws, reqID, true, svc.GetHeartbeatConf(), "", "")
	}
}

// heartbeatSetConf 处理 heartbeat.set_conf。
// Python: _heartbeat_set_conf (app_web_handlers.py L1527-1563)。
func heartbeatSetConf(wc *WebChannel, svc HeartbeatConfService, ac *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		ctx := context.Background()
		if svc == nil {
			wc.sendResponse(ws, reqID, false, nil, "heartbeat service not available", WsErrServiceUnavailable)
			return
		}
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}

		// 三值提取（对齐 Python L1538-1550）
		var every *float64
		if v, ok := params["every"]; ok && v != nil {
			f, err := heartbeatAnyToFloat(v)
			if err != nil {
				wc.sendResponse(ws, reqID, false, nil, "every must be number", WsErrBadRequest)
				return
			}
			every = &f
		}
		var target *string
		if v, ok := params["target"]; ok && v != nil {
			s := fmt.Sprint(v) // 对齐 Python str(target)
			target = &s
		}
		// 审查修复 S-06：区分三分支对齐 Python 微妙三分语义：
		//   ① 空 map（len==0）→ 传非 nil 空 map（{} 为 falsy，Python elif
		//      不命中原样穿透 set_heartbeat_conf 更新为 {}——清除生效
		//      始终活跃）；NormalizeActiveHours 空 map 出空 map，isActiveNow
		//      对空配置返回始终活跃
		//   ② 非空且同时含 start/end → 原样直通
		//   ③ 非空但缺 start/end → 保持 nil（Python truthy 缺键置 None →
		//      set_heartbeat_conf 不更新保持旧值）
		// 原实现空 map 落 nil → SetHeartbeatConf updated=false 早退——注释
		// 声称的"清除语义"实际是 no-op，旧时段保留。
		var activeHours map[string]any
		if v, ok := params["active_hours"]; ok && v != nil {
			if ahMap, isMap := v.(map[string]any); isMap {
				if len(ahMap) == 0 {
					// 空 map：非 nil 空配置触发 SetHeartbeatConf 更新（清除时段）
					activeHours = map[string]any{}
				} else if _, hasStart := ahMap["start"]; hasStart {
					if _, hasEnd := ahMap["end"]; hasEnd {
						// 值原样直通（对齐 Python dict 传引用不转换：
						// 数值 1320 保留 int，由 heartbeat 侧 NormalizeActiveHours 回转）
						activeHours = ahMap
					}
				}
				// 非空但缺 start/end → nil（不更新保持旧值）
			}
			// 非 map → nil（不更新保持旧值）
		}

		// 目标渠道是飞书时先检测是否有可用推送目标（多应用要求每个 app 都有）
		if target != nil && *target == "feishu" {
			hasTarget, checkErr := feishuHasPushTarget()
			if checkErr != nil {
				logger.Debug(logComponent).
					Err(checkErr).
					Msg("[health_check.set_conf] 飞书目标检测异常")
				wc.sendResponse(ws, reqID, false, nil, checkErr.Error(), WsErrInternalError)
				return
			}
			if !hasTarget {
				wc.sendResponse(ws, reqID, false, nil, "feishuNoTarget", wsErrFeishuNoTarget)
				return
			}
		}

		if err := svc.SetHeartbeatConf(every, target, activeHours); err != nil {
			// 对齐 Python except ValueError → BAD_REQUEST
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrBadRequest)
			return
		}
		payload := svc.GetHeartbeatConf()

		// 写回 config.yaml（对齐 L1553-1557 try/Warn：失败仅 Warn 不阻断响应）
		if err := updateHealthCheckInConfig(payload); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Msg("[heartbeat.set_conf] 写回 config.yaml 失败")
		} else {
			// 清 Agent 侧配置缓存（对齐 _clear_agent_config_cache，失败静默 pass）
			clearAgentConfigCache(ac, ctx)
		}
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// heartbeatAnyToFloat every 参数数值化。
// 对齐 Python float(every)：string/int/int64/float64 → float64；
// 失败或类型不支持 → error（调用方转 BAD_REQUEST "every must be number"）。
func heartbeatAnyToFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number: %q", val)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// feishuHasPushTarget 判断飞书通道是否已配置推送目标。
//
// Python: _health_check_set_conf 的 target=="feishu" 检测（app_web_handlers.py:4290-4325）：
// 多应用模式要求每个 app 都有 last_chat_id 或 chat_id；旧平铺格式看顶层同名字段。
func feishuHasPushTarget() (bool, error) {
	cfg, err := config.New("")
	if err != nil {
		return false, fmt.Errorf("创建配置管理器失败: %w", err)
	}
	raw, err := cfg.Raw()
	if err != nil {
		return false, fmt.Errorf("加载配置失败: %w", err)
	}
	chCfg := mapSection(raw, "channels", "feishu")
	apps, _ := chCfg["apps"].([]any)
	if len(apps) > 0 {
		for _, item := range apps {
			app, isMap := item.(map[string]any)
			if !isMap {
				return false, nil
			}
			if strings.TrimSpace(strOrEmpty(app["last_chat_id"])) == "" &&
				strings.TrimSpace(strOrEmpty(app["chat_id"])) == "" {
				return false, nil
			}
		}
		return true, nil
	}
	return strings.TrimSpace(strOrEmpty(chCfg["last_chat_id"])) != "" ||
		strings.TrimSpace(strOrEmpty(chCfg["chat_id"])) != "", nil
}

// clearAgentConfigCache 写回 config.yaml 后清 Agent 侧缓存。
// 对齐 Python _clear_agent_config_cache (L485-501)：
// agentClient 非 nil → AGENT_RELOAD_CONFIG E2A（10s 超时）；
// nil → 跳过（Go 装配 web bind 总有 agentClient；Python else 分支 get_config()
// 本地重读在 Go 对应 config 全局已注册，无需动作，偏差⑦）。
// 失败静默 pass（对齐 Python except: pass）。
func clearAgentConfigCache(ac *routing.AgentClient, ctx context.Context) {
	if ac == nil {
		return
	}
	defer func() {
		_ = recover() // 静默（对齐 Python except: pass）
	}()
	reloadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	requestID := "cfg-reload-" + uuid.New().String()[:8]
	envelope := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldReqMethod(string(schema.ReqMethodAgentReloadConfig)),
	)
	_, _ = ac.SendRequest(reloadCtx, envelope)
}

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigBadRequest 配置请求参数错误。
// Python: _ConfigBadRequest(ValueError)。
type ConfigBadRequest struct {
	// Message 错误信息
	Message string
}

// ConfigInternalError 配置内部错误。
// Python: _ConfigInternalError(RuntimeError)。
type ConfigInternalError struct {
	// Message 错误信息
	Message string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentConfigApply 本文件日志组件
const logComponentConfigApply = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// configYAMLKeys 需要写入 config.yaml 的配置键集合。
// Python: _CONFIG_YAML_KEYS (app_web_handlers.py L371-377)。
var configYAMLKeys = map[string]bool{
	"context_engine_enabled":       true,
	"kv_cache_affinity_enabled":    true,
	"permissions_enabled":          true,
	"memory_forbidden_enabled":     true,
	"memory_forbidden_description": true,
}

// availableModelProviders 可用的模型服务商列表。
// Python: [provider.value for provider in ProviderType]。
var availableModelProviders = []string{
	"OpenAI",
	"OpenRouter",
	"SiliconFlow",
	"DashScope",
	"DeepSeek",
	"InferenceAffinity",
	"intelli_router",
}

// channelConfPlatforms IM 平台配置族覆盖的平台清单（注册处逐条字面量、见 registerChannelConfFamily）。
//
// Python: app_web_handlers.py:5844-5863 的 9 组 channel.<p>.get_conf / set_conf 注册。
var channelConfPlatforms = []string{
	"feishu", "xiaoyi", "telegram", "dingtalk", "whatsapp",
	"discord", "slack", "wecom", "wechat",
}

// feishuAppDefaults 飞书应用条目缺省值。
//
// Python: _FEISHU_APP_DEFAULTS (app_web_handlers.py:1586-1600)。
var feishuAppDefaults = map[string]any{
	"name":                 "默认应用",
	"is_default":           false,
	"enabled":              true,
	"app_id":               "",
	"app_secret":           "",
	"encrypt_key":          "",
	"verification_token":   "",
	"allow_from":           []any{"0.0.0.0/0"},
	"enable_streaming":     true,
	"group_digital_avatar": false,
	"my_user_id":           "",
	"bot_name":             "",
	"enable_memory":        false,
}

// xiaoyiAppDefaults 小艺应用条目缺省值。
//
// Python: _XIAOYI_APP_DEFAULTS (app_web_handlers.py:1687-1704)。
var xiaoyiAppDefaults = map[string]any{
	"name":                "默认应用",
	"is_default":          false,
	"enabled":             true,
	"ak":                  "",
	"sk":                  "",
	"app_id":              "",
	"api_id":              "",
	"agent_id":            "",
	"enable_streaming":    true,
	"mode":                "xiaoyi_channel",
	"push_id":             "",
	"ws_url1":             "wss://hag.cloud.huawei.com/openclaw/v1/ws/link",
	"ws_url2":             "wss://116.63.174.231/openclaw/v1/ws/link",
	"phone_tools_enabled": false,
	"uid":                 "",
	"api_key":             "",
	"push_url":            "",
	"file_upload_url":     "",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *ConfigBadRequest) Error() string { return e.Message }

// Error 实现 error 接口。
func (e *ConfigInternalError) Error() string { return e.Message }

// ApplyConfigPayload 将 config.set 风格的参数应用到 .env 和 config.yaml，不触发重载。
// Python: _apply_config_payload (app_web_handlers.py L701-761)。
//
// 返回值：
//   - envUpdates: 本次变更的环境变量增量
//   - yamlUpdated: 本次变更的 YAML 键列表
func ApplyConfigPayload(ctx context.Context, params map[string]any, cryptoProv crypto.CryptoProvider) (envUpdates map[string]string, yamlUpdated []string, err error) {
	// Python: _encrypt_config_params (L692-699):
	// 遍历 params，key 名包含 api_key 或 token 时加密。
	// 加密失败保留明文 + Warn（Go error 签名；对齐 Python 无 error 概念的宽容语义）
	if cryptoProv != nil {
		for key, val := range params {
			kl := strings.ToLower(key)
			if strings.Contains(kl, "api_key") || strings.Contains(kl, "token") {
				if strVal, ok := val.(string); ok && strVal != "" {
					if enc, encErr := cryptoProv.Encrypt(ctx, strVal); encErr == nil {
						params[key] = enc
					} else {
						logger.Warn(logComponentConfigApply).
							Str("key", key).
							Err(encErr).
							Msg("加密失败，保留明文写入")
					}
				}
			}
		}
	}

	envUpdates = make(map[string]string)
	yamlUpdated = make([]string, 0)

	// 环境变量映射处理
	for paramKey, envKey := range configEnvMap {
		val, ok := params[paramKey]
		if !ok {
			continue
		}
		// provider 校验（对齐 Python available_model_providers 校验，仅非空值校验）
		if strings.HasSuffix(paramKey, "_provider") {
			strVal := strings.TrimSpace(fmt.Sprintf("%v", val))
			if strVal != "" && !isAvailableProvider(strVal) {
				return nil, nil, &ConfigBadRequest{
					Message: fmt.Sprintf("Model provider must in: %v", availableModelProviders),
				}
			}
		}
		if val == nil {
			envUpdates[envKey] = ""
		} else {
			envUpdates[envKey] = strings.TrimSpace(fmt.Sprintf("%v", val))
		}
	}

	// agents/team 写入 config.yaml
	if _, hasAgents := params["agents"]; hasAgents {
		if err := replaceTeamsInConfig(params); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("failed to write back modes.team")
			return nil, nil, &ConfigInternalError{Message: "failed to update modes.team"}
		}
		yamlUpdated = append(yamlUpdated, "modes.team")
	} else if _, hasTeam := params["team"]; hasTeam {
		if err := replaceTeamsInConfig(params); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("failed to write back modes.team")
			return nil, nil, &ConfigInternalError{Message: "failed to update modes.team"}
		}
		yamlUpdated = append(yamlUpdated, "modes.team")
	}

	// YAML 键写入 config.yaml
	for paramKey := range configYAMLKeys {
		val, ok := params[paramKey]
		if !ok {
			continue
		}
		if err := updateYAMLKeyInConfig(paramKey, val); err != nil {
			logger.Warn(logComponentConfigApply).
				Str("key", paramKey).
				Err(err).
				Msg("failed to write back config.yaml")
		} else {
			yamlUpdated = append(yamlUpdated, paramKey)
		}
	}

	// 设置环境变量 + 持久化 .env
	for envKey, value := range envUpdates {
		_ = os.Setenv(envKey, value)
	}
	if len(envUpdates) > 0 {
		if err := persistEnvUpdates(envUpdates); err != nil {
			logger.Warn(logComponentConfigApply).
				Err(err).
				Msg("failed to persist .env file")
		} else {
			logger.Info(logComponentConfigApply).
				Strs("keys", sortedKeys(envUpdates)).
				Msg(".env updated")
		}
	}
	if len(yamlUpdated) > 0 {
		logger.Info(logComponentConfigApply).
			Strs("keys", yamlUpdated).
			Msg("config.yaml updated")
	}

	return envUpdates, yamlUpdated, nil
}

// NotifyConfigSavedOnce 在所有文件写入完成后触发一次热重载。
// Python: _notify_config_saved_once (app_web_handlers.py L763-782)。
func NotifyConfigSavedOnce(
	onConfigSaved OnConfigSavedFunc,
	envUpdates map[string]string,
	yamlUpdated []string,
	force bool,
) {
	if !force && len(envUpdates) == 0 && len(yamlUpdated) == 0 {
		return
	}
	if onConfigSaved == nil {
		// ⤵️ 对齐 Python: _clear_agent_config_cache(agent_client)
		logger.Debug(logComponentConfigApply).Msg("onConfigSaved is nil, clearAgentConfigCache pending implementation")
		return
	}

	// 获取最新配置快照
	configPayload := getConfigSnapshot()

	// 合并变更键（对齐 Python: set(env_updates.keys()) | set(yaml_updated)，使用 map 去重）
	seen := make(map[string]struct{}, len(envUpdates)+len(yamlUpdated))
	updatedKeys := make([]string, 0, len(envUpdates)+len(yamlUpdated))
	for k := range envUpdates {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			updatedKeys = append(updatedKeys, k)
		}
	}
	for _, k := range yamlUpdated {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			updatedKeys = append(updatedKeys, k)
		}
	}

	// envUpdates 转 map[string]any
	envUpdatesAny := make(map[string]any, len(envUpdates))
	for k, v := range envUpdates {
		envUpdatesAny[k] = v
	}

	// 调用回调
	if err := onConfigSaved(updatedKeys, envUpdatesAny, configPayload); err != nil {
		logger.Warn(logComponentConfigApply).
			Err(err).
			Msg("onConfigSaved callback failed")
	}
}

// ProcessFiles 处理 params 中的 files 字段，下载 URL 文件到本地 workspace。
//
// Python: _process_files (web_connect.py L189-221)。
func ProcessFiles(ctx context.Context, params map[string]any) map[string]any {
	files, ok := params["files"]
	if !ok {
		return params
	}
	filesList, ok := files.([]any)
	if !ok || len(filesList) == 0 {
		return params
	}

	workspaceDir := workspace.AgentWorkspaceDir()
	downloadedFiles := make([]any, 0, len(filesList))

	for _, fileItem := range filesList {
		fileInfo, ok := fileItem.(map[string]any)
		if !ok {
			downloadedFiles = append(downloadedFiles, fileItem)
			continue
		}

		fileURL := ""
		if u, ok := fileInfo["url"].(string); ok && u != "" {
			fileURL = u
		} else if u, ok := fileInfo["uri"].(string); ok && u != "" {
			fileURL = u
		}

		fileName := "unknown_file"
		if n, ok := fileInfo["name"].(string); ok && n != "" {
			fileName = n
		} else if n, ok := fileInfo["filename"].(string); ok && n != "" {
			fileName = n
		}

		if fileURL != "" {
			fileContent, err := downloadFile(ctx, fileURL)
			if err != nil {
				logger.Warn(logComponentConfigApply).
					Str("url", fileURL).
					Err(err).
					Msg("file download failed")
			} else if fileContent != nil {
				if err := os.MkdirAll(workspaceDir, 0o755); err == nil {
					filePath := filepath.Join(workspaceDir, fileName)
					if err := os.WriteFile(filePath, fileContent, 0o644); err != nil {
						logger.Warn(logComponentConfigApply).Err(err).Msg("file save failed")
					} else {
						fileInfo["path"] = filePath
					}
				}
			}
		}
		downloadedFiles = append(downloadedFiles, fileInfo)
	}

	params["files"] = downloadedFiles
	return params
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAvailableProvider 检查 provider 是否在可用列表中。
func isAvailableProvider(provider string) bool {
	for _, p := range availableModelProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// updateYAMLKeyInConfig 将单个 YAML 键写入 config.yaml。
// Python: update_context_engine_enabled_in_config 等系列函数。
func updateYAMLKeyInConfig(key string, value any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	// 加载当前配置
	cfgData, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	parsed := isTruthy(value)

	switch key {
	case "context_engine_enabled":
		// M-05: SetRoundTrip（yaml.v3 Node 保注释）替代 Set（yaml.Marshal 全量重写丢注释），
		// 对齐 Python 各 update_*_in_config 的 ruamel round-trip 写回
		return cfg.SetRoundTrip("react.context_engine_config.enabled", parsed)
	case "kv_cache_affinity_enabled":
		return cfg.SetRoundTrip("react.context_engine_config.enable_kv_cache_release", parsed)
	case "permissions_enabled":
		return cfg.SetRoundTrip("permissions.enabled", parsed)
	case "memory_forbidden_enabled":
		return cfg.SetRoundTrip("memory.forbidden_memory_definition.enabled", parsed)
	case "memory_forbidden_description":
		// Python: update_memory_forbidden_description_in_config
		// 先读后合并：current_desc = config.get("memory.forbidden_memory_definition.description", {})
		// Python: config.set("memory.forbidden_memory_definition.description", {**current_desc, **description})
		descMap := cfg.Get("memory.forbidden_memory_definition.description")
		currentDesc := make(map[string]any)
		if descMap != nil {
			if m, ok := descMap.(map[string]any); ok {
				currentDesc = m
			}
		}
		// 合并：新值覆盖同名 key，旧的其他 key 保留
		if descVal, ok := value.(map[string]any); ok {
			for k, v := range descVal {
				currentDesc[k] = v
			}
		} else if descStr, ok := value.(string); ok {
			// 单语言字符串：用 preferred_language 确定写入的 key
			preferredLang := "zh"
			if lang, ok := cfgData["preferred_language"].(string); ok && lang != "" {
				preferredLang = lang
			}
			currentDesc[preferredLang] = strings.TrimSpace(descStr)
		}
		return cfg.SetRoundTrip("memory.forbidden_memory_definition.description", currentDesc)
	default:
		return fmt.Errorf("未知的 YAML 配置键: %s", key)
	}
}

// replaceTeamsInConfig 替换 config.yaml 中的 modes.team 配置。
// Python: replace_teams_in_config (common/config.py L989-1038)。
func replaceTeamsInConfig(params map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// team 数据
	teamData, hasTeam := params["team"]
	if hasTeam {
		// 空数组：删除 modes.team（对齐 Python replace_teams_in_config L1017-1021）
		if teamList, ok := teamData.([]any); ok && len(teamList) == 0 {
			data, err := cfg.Load()
			if err != nil {
				return fmt.Errorf("加载配置失败: %w", err)
			}
			// Python: if "modes" in data and isinstance(data["modes"], dict) and "team" in data["modes"]: del data["modes"]["team"]
			if modes, ok := data["modes"].(map[string]any); ok {
				if _, hasTeamKey := modes["team"]; hasTeamKey {
					delete(modes, "team")
					// M-05: Save → SetRoundTrip 不可行（删除无对应 API），
					// 保留 Save 全量写回（Python ruamel del + dump 等价路径）
					if err := cfg.Save(data); err != nil {
						return fmt.Errorf("保存配置失败（删除 modes.team）: %w", err)
					}
					logger.Info(logComponentConfigApply).Msg("modes.team empty array, config node deleted")
				}
			}
			return nil
		}
		// 非空数组：写入 modes.team
		// M-05: SetRoundTrip 保注释写回
		if err := cfg.SetRoundTrip("modes.team", teamData); err != nil {
			return fmt.Errorf("写入 modes.team 失败: %w", err)
		}
	}

	// agents 数据写入 web_config_panel.agent_team_agents
	agentsData, hasAgents := params["agents"]
	if hasAgents {
		// M-05: SetRoundTrip 保注释写回
		if err := cfg.SetRoundTrip("web_config_panel.agent_team_agents", agentsData); err != nil {
			return fmt.Errorf("写入 web_config_panel.agent_team_agents 失败: %w", err)
		}
	}

	return nil
}

// updateDefaultModelsInConfig 将默认模型列表写入 config.yaml 的 models.defaults 段。
// Python: update_default_models_in_config (common/config.py L734-742)。
func updateDefaultModelsInConfig(models []map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if err := cfg.SetRoundTrip("models.defaults", models); err != nil {
		return fmt.Errorf("写入 models.defaults 失败: %w", err)
	}

	return nil
}

// updateHealthCheckInConfig 只更新 health_check 段的 every/target/active_hours 并写回。
//
// Python: update_health_check_in_config (gateway/common/config.py:451-463)。
// Python: update_heartbeat_in_config (common/config.py L152-164)。
// 逐键条件写回（if "every" in payload ...），SetRoundTrip 保注释（M-05 先例）。
func updateHealthCheckInConfig(payload map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	_, err = cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if v, ok := payload["every"]; ok {
		if err := cfg.SetRoundTrip("health_check.every", v); err != nil {
			return fmt.Errorf("写入 health_check.every 失败: %w", err)
		}
	}
	if v, ok := payload["target"]; ok {
		if err := cfg.SetRoundTrip("health_check.target", v); err != nil {
			return fmt.Errorf("写入 health_check.target 失败: %w", err)
		}
	}
	if v, ok := payload["active_hours"]; ok {
		if err := cfg.SetRoundTrip("health_check.active_hours", v); err != nil {
			return fmt.Errorf("写入 health_check.active_hours 失败: %w", err)
		}
	}
	return nil
}

// updateMemoryForbiddenInConfig 更新 memory.forbidden_memory_definition 并写回。
// Python: update_memory_forbidden_in_config (common/config.py)。
// description 字段使用先读后合并逻辑（新值覆盖同名 key，旧的其他 key 保留）。
func updateMemoryForbiddenInConfig(updates map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}

	cfgData, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 读取当前 memory.forbidden_memory_definition
	section := make(map[string]any)
	if memRaw, ok := cfgData["memory"].(map[string]any); ok {
		if fmd, ok := memRaw["forbidden_memory_definition"].(map[string]any); ok {
			section = fmd
		}
	}

	// 合并更新
	for k, v := range updates {
		if k == "description" {
			// Python: description 字段先读后合并
			// {**current_desc, **description}：新值覆盖同名 key，旧的其他 key 保留
			currentDesc := make(map[string]any)
			if existing, ok := section["description"].(map[string]any); ok {
				for dk, dv := range existing {
					currentDesc[dk] = dv
				}
			}
			if newDesc, ok := v.(map[string]any); ok {
				for dk, dv := range newDesc {
					currentDesc[dk] = dv
				}
			} else {
				// 非字典类型直接覆盖
				currentDesc = map[string]any{"_value": v}
			}
			section[k] = currentDesc
		} else {
			section[k] = v
		}
	}

	// 写回 memory.forbidden_memory_definition
	// M-05: SetRoundTrip 保注释写回（对齐 Python ruamel round-trip）
	if err := cfg.SetRoundTrip("memory.forbidden_memory_definition", section); err != nil {
		return fmt.Errorf("写入 memory.forbidden_memory_definition 失败: %w", err)
	}

	return nil
}

// buildModelsDefaultsFromFrontend 从前端 models 参数构建 models.defaults 列表。
// Python: _build_models_defaults_from_frontend (app_web_handlers.py L784-868)。
// cryptoProv 用于加密 api_key；rawModels 中的 origin_index 用于匹配原始 YAML 条目。
func buildModelsDefaultsFromFrontend(ctx context.Context, rawModels any, cryptoProv crypto.CryptoProvider) ([]map[string]any, error) {
	modelsList, ok := rawModels.([]any)
	if !ok || len(modelsList) == 0 {
		return nil, &ConfigBadRequest{Message: "models must be a non-empty list"}
	}

	parsed := make([]map[string]any, 0, len(modelsList))
	aliasesSeen := make(map[string]int)

	for idx, item := range modelsList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d] must be object", idx)}
		}

		modelNameRaw, hasModelName := itemMap["model_name"]
		if !hasModelName || modelNameRaw == nil {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d].model_name is required", idx)}
		}
		modelName := strings.TrimSpace(fmt.Sprintf("%v", modelNameRaw))
		if modelName == "" {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d].model_name is required", idx)}
		}

		apiKeyRaw, hasAPIKey := itemMap["api_key"]
		originIndex := itemMap["origin_index"] // 可为 nil
		apiKey := ""
		if hasAPIKey && apiKeyRaw != nil {
			apiKey = strings.TrimSpace(fmt.Sprintf("%v", apiKeyRaw))
		}
		if apiKey == "" && originIndex == nil {
			return nil, &ConfigBadRequest{Message: fmt.Sprintf("models[%d].api_key is required", idx)}
		}

		apiBase := strings.TrimSpace(fmt.Sprintf("%v", itemMap["api_base"]))
		modelProvider := ""
		if mp, ok := itemMap["model_provider"]; ok && mp != nil {
			modelProvider = strings.TrimSpace(fmt.Sprintf("%v", mp))
		}
		// provider 校验：仅当值非空时才检查（对齐 Python 逻辑）
		if modelProvider != "" && !isAvailableProvider(modelProvider) {
			return nil, &ConfigBadRequest{
				Message: fmt.Sprintf("models[%d].model_provider must be one of: %v", idx, availableModelProviders),
			}
		}

		temperature := 0.95
		if t, ok := itemMap["temperature"]; ok {
			if f, err := parseFloat(t); err == nil {
				temperature = f
			}
		}

		timeout := 1800
		if t, ok := itemMap["timeout"]; ok {
			if i, err := parseInt(t); err == nil {
				timeout = i
			}
		}

		alias := ""
		if a, ok := itemMap["alias"]; ok && a != nil {
			alias = strings.TrimSpace(fmt.Sprintf("%v", a))
		}
		if alias != "" {
			if prevIdx, exists := aliasesSeen[alias]; exists {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("Alias '%s' is used by both models[%d] and models[%d]", alias, prevIdx, idx),
				}
			}
			aliasesSeen[alias] = idx
		}

		isDefault := false
		if d, ok := itemMap["is_default"]; ok {
			isDefault = isTruthy(d)
		}

		verifySSL := false
		if v, ok := itemMap["verify_ssl"].(bool); ok {
			verifySSL = v
		}

		// context_window / max_tokens：归 model_config_obj（对齐上游模型条目结构）。
		// context_window 接受 256k/1m 等字符串形态；0 表示未配置，可覆盖旧值。
		contextWindow := 0
		if v, ok := itemMap["context_window"]; ok && v != nil && fmt.Sprint(v) != "" {
			n, perr := config.ParsePositiveTokens(v)
			if !perr {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("models[%d].context_window 必须是正整数或 256k/1m 形式", idx),
				}
			}
			contextWindow = n
		}

		maxTokens := 0
		if v, ok := itemMap["max_tokens"]; ok && v != nil && fmt.Sprint(v) != "" {
			n, perr := parseInt(v)
			if perr != nil || n < 0 {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("models[%d].max_tokens 必须是非负整数", idx),
				}
			}
			maxTokens = n
		}

		entry := map[string]any{
			"model_client_config": map[string]any{
				"model_name":      modelName,
				"api_base":        apiBase,
				"api_key":         apiKey,
				"client_provider": modelProvider,
				"timeout":         timeout,
				"verify_ssl":      verifySSL,
			},
			"model_config_obj": map[string]any{
				"temperature": temperature,
				// 上下文窗口（token）；0 = 未配置，回退内置表
				"context_window": contextWindow,
				// 输出上限（token）：请求 max_tokens 默认值；0 = 不限制
				"max_tokens": maxTokens,
			},
			"is_default": isDefault,
			"alias":      alias,
		}
		if originIndex != nil {
			entry["origin_index"] = originIndex
		}

		parsed = append(parsed, entry)
	}

	// alias 与其他条目的 model_name 冲突校验
	for i, p := range parsed {
		a, _ := p["alias"].(string)
		if a == "" {
			continue
		}
		for j, q := range parsed {
			if i == j {
				continue
			}
			if q["model_client_config"] != nil {
				if mcc, ok := q["model_client_config"].(map[string]any); ok {
					if mn, _ := mcc["model_name"].(string); mn == a {
						return nil, &ConfigBadRequest{
							Message: fmt.Sprintf("Alias '%s' on models[%d] conflicts with model_name on models[%d]", a, i, j),
						}
					}
				}
			}
		}
	}

	// 合并原始 YAML 条目（对齐 Python _merge_models_for_replace_all）
	rawDefaults := loadRawModelsDefaults()
	resolvedDefaults := config.GetDefaultModels(nil)
	merged := mergeModelsForReplaceAll(ctx, parsed, rawDefaults, resolvedDefaults, cryptoProv)

	// 推断 is_default（对齐 Python _infer_is_default）
	merged = inferIsDefault(merged)

	return merged, nil
}

// getConfigSnapshot 获取当前配置快照。
// Python: get_config()（含 _normalize_config——2026-10-28 审查接线）。
func getConfigSnapshot() map[string]any {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return make(map[string]any)
	}
	data, err := cfg.Load()
	if err != nil {
		return make(map[string]any)
	}
	return data
}

// isTruthy 判断值是否为真（对齐 Python bool 语义）。
func isTruthy(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "1" || lower == "yes"
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return fmt.Sprintf("%v", val) != ""
	}
}

// parseFloat 安全解析浮点数。
func parseFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(v, "%f", &f)
		return f, err
	default:
		return 0, fmt.Errorf("无法解析为浮点数: %v", val)
	}
}

// parseInt 安全解析整数。
func parseInt(val any) (int, error) {
	switch v := val.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		var i int
		_, err := fmt.Sscanf(v, "%d", &i)
		return i, err
	default:
		return 0, fmt.Errorf("无法解析为整数: %v", val)
	}
}

// processFiles 处理 params 中的 files 字段，下载 URL 文件到本地 workspace。
//
// Python: _process_files (web_connect.py L189-221)。
// 供 HandleWebSocket 内部调用，外部请使用 ProcessFiles。
func processFiles(ctx context.Context, params map[string]any) map[string]any {
	return ProcessFiles(ctx, params)
}

// downloadFile 下载指定 URL 的文件内容。
func downloadFile(ctx context.Context, url string) ([]byte, error) {
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(childCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// valuesMatch 比较前端发送值与解析后的值是否相同。
// Python: _values_match (app_web_handlers.py L63-79)。
func valuesMatch(parsedVal, resolvedVal any) bool {
	// bool 比较
	if _, ok := parsedVal.(bool); ok {
		return fmt.Sprintf("%v", parsedVal) == fmt.Sprintf("%v", resolvedVal)
	}
	if _, ok := resolvedVal.(bool); ok {
		return fmt.Sprintf("%v", parsedVal) == fmt.Sprintf("%v", resolvedVal)
	}
	// nil 比较
	if parsedVal == nil && resolvedVal == nil {
		return true
	}
	// 数值比较
	pF, pErr := parseFloat(parsedVal)
	rF, rErr := parseFloat(resolvedVal)
	if pErr == nil && rErr == nil {
		return pF == rF
	}
	// 字符串比较
	pStr := ""
	if parsedVal != nil {
		pStr = fmt.Sprintf("%v", parsedVal)
	}
	rStr := ""
	if resolvedVal != nil {
		rStr = fmt.Sprintf("%v", resolvedVal)
	}
	return pStr == rStr
}

// encryptAPIKey 加密条目的 api_key（cryptoProv nil 或加密失败时保留明文 + Warn）
func encryptAPIKey(ctx context.Context, mcc map[string]any, cryptoProv crypto.CryptoProvider) {
	if cryptoProv == nil {
		return
	}
	apiKey, ok := mcc["api_key"].(string)
	if !ok || apiKey == "" {
		return
	}
	enc, err := cryptoProv.Encrypt(ctx, apiKey)
	if err != nil {
		logger.Warn(logComponentConfigApply).
			Err(err).
			Msg("api_key 加密失败，保留明文")
		return
	}
	mcc["api_key"] = enc
}

// mergeModelsForReplaceAll 用 origin_index 匹配原始 YAML 条目，保留占位符和未暴露字段，仅覆写变化字段。
// Python: _merge_models_for_replace_all (app_web_handlers.py L82-156)。
//
// 对每个前端条目：
//   - 有 origin_index 指向已有原始条目：深拷贝原始条目，仅覆写与解析快照不同的字段
//   - 比较时用 resolvedDefaults[oi] 与前端值比较，匹配时保留 rawDefaults[oi] 的值
//   - api_key 变更时加密，未变更时保留原始值
//   - 新条目（无 origin_index）：加密 api_key 后原样存入
func mergeModelsForReplaceAll(ctx context.Context, parsed []map[string]any, rawDefaults []map[string]any, resolvedDefaults []map[string]any, cryptoProv crypto.CryptoProvider) []map[string]any {

	result := make([]map[string]any, 0, len(parsed))

	for _, entry := range parsed {
		originIndexVal, hasOrigin := entry["origin_index"]
		if !hasOrigin || originIndexVal == nil {
			// 新条目：加密 api_key 后原样存入
			newEntry := utils.DeepCopyMap(entry)
			if mcc, ok := newEntry["model_client_config"].(map[string]any); ok {
				encryptAPIKey(ctx, mcc, cryptoProv)
			}
			result = append(result, newEntry)
			continue
		}

		// 解析 origin_index
		oi := -1
		switch v := originIndexVal.(type) {
		case int:
			oi = v
		case float64:
			oi = int(v)
		default:
			if i, err := parseInt(v); err == nil {
				oi = i
			}
		}

		if oi < 0 || oi >= len(rawDefaults) {
			// origin_index 越界，当作新条目处理
			newEntry := utils.DeepCopyMap(entry)
			if mcc, ok := newEntry["model_client_config"].(map[string]any); ok {
				encryptAPIKey(ctx, mcc, cryptoProv)
			}
			result = append(result, newEntry)
			continue
		}

		// 深拷贝原始条目（保留占位符、custom_headers 等未暴露字段）
		merged := utils.DeepCopyMap(rawDefaults[oi])

		// 获取解析后的默认条目（用于与前端值比较）
		var resolvedEntry map[string]any
		if oi < len(resolvedDefaults) {
			resolvedEntry = resolvedDefaults[oi]
		}

		// 前端解析后的快照（即 entry 中的解析值）
		// 用 resolvedDefaults 与前端值比较，匹配时保留 rawDefaults 的值
		if frontMCC, ok := entry["model_client_config"].(map[string]any); ok {
			if mergedMCC, ok := merged["model_client_config"].(map[string]any); ok {
				// 获取解析后的 model_client_config
				var resolvedMCC map[string]any
				if resolvedEntry != nil {
					resolvedMCC, _ = resolvedEntry["model_client_config"].(map[string]any)
				}
				for k, parsedVal := range frontMCC {
					// 用 resolvedDefaults 的值比较，而非 rawDefaults
					resolvedVal := mergedMCC[k] // 默认 fallback 到 raw
					if resolvedMCC != nil {
						if rv, ok := resolvedMCC[k]; ok {
							resolvedVal = rv
						}
					}
					if !valuesMatch(parsedVal, resolvedVal) {
						// 前端值与解析值不同，覆写
						if k == "api_key" {
							// api_key 特殊处理：加密后写入（失败保留明文 + Warn）
							if apiKeyStr, ok := parsedVal.(string); ok && apiKeyStr != "" && cryptoProv != nil {
								if enc, encErr := cryptoProv.Encrypt(ctx, apiKeyStr); encErr == nil {
									mergedMCC[k] = enc
								} else {
									logger.Warn(logComponentConfigApply).Err(encErr).Msg("api_key 加密失败，保留明文")
									mergedMCC[k] = parsedVal
								}
							} else {
								mergedMCC[k] = parsedVal
							}
						} else {
							mergedMCC[k] = parsedVal
						}
					}
					// 前端值与解析值相同，保留原始值（含占位符等）
				}
			}
		}

		if frontMCO, ok := entry["model_config_obj"].(map[string]any); ok {
			if mergedMCO, ok := merged["model_config_obj"].(map[string]any); ok {
				// 获取解析后的 model_config_obj
				var resolvedMCO map[string]any
				if resolvedEntry != nil {
					resolvedMCO, _ = resolvedEntry["model_config_obj"].(map[string]any)
				}
				for k, parsedVal := range frontMCO {
					resolvedVal := mergedMCO[k] // 默认 fallback 到 raw
					if resolvedMCO != nil {
						if rv, ok := resolvedMCO[k]; ok {
							resolvedVal = rv
						}
					}
					if !valuesMatch(parsedVal, resolvedVal) {
						mergedMCO[k] = parsedVal
					}
				}
			}
		}

		// 覆写顶层字段
		for _, k := range []string{"is_default", "alias"} {
			parsedVal, hasParsed := entry[k]
			resolvedVal, hasResolved := merged[k]
			if resolvedEntry != nil {
				if rv, ok := resolvedEntry[k]; ok {
					resolvedVal = rv
					hasResolved = true
				}
			}
			if hasParsed {
				if !hasResolved || !valuesMatch(parsedVal, resolvedVal) {
					merged[k] = parsedVal
				}
			}
		}

		// 移除 origin_index（不需要持久化到 YAML）
		delete(merged, "origin_index")

		result = append(result, merged)
	}

	return result
}

// inferIsDefault 确保模型列表中恰好一个 is_default=true。
// Python: _infer_is_default。
func inferIsDefault(models []map[string]any) []map[string]any {
	if len(models) == 0 {
		return models
	}
	hasDefault := false
	for _, m := range models {
		if isTruthy(m["is_default"]) {
			if hasDefault {
				// 后续 true 设为 false，确保恰好一个
				m["is_default"] = false
			} else {
				hasDefault = true
			}
		}
	}
	if !hasDefault {
		// 无 true 时，设第一个为 true
		models[0]["is_default"] = true
	}
	return models
}

// loadRawModelsDefaults 从 config.yaml 加载原始 models.defaults 列表。
func loadRawModelsDefaults() []map[string]any {
	cfgData := getConfigSnapshot()
	rawVal := getConfigAny(cfgData, "models.defaults", nil)
	if rawVal == nil {
		return nil
	}
	rawList, ok := rawVal.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(rawList))
	for _, item := range rawList {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// ──────────────────────────── 全局变量 ────────────────────────────

// collectMemory 采集本进程与系统内存，返回字节数及系统内存使用百分比。
//
// 以包级变量暴露供测试注入：gopsutil 的错误分支无法由真实环境稳定触发。
var collectMemory = defaultCollectMemory

// processRSSBytes 采集本进程 RSS 字节数（测试注入点）。
var processRSSBytes = defaultProcessRSSBytes

// currentPID 当前进程 PID（测试注入点）。
var currentPID = os.Getpid

// systemVirtualMemory 采集系统内存（测试注入点）。
var systemVirtualMemory = mem.VirtualMemory

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// memoryCompute 处理 memory.compute 请求，返回进程与系统内存占用。
//
// Python: _memory_compute (app_web_handlers.py L1414-1427)
// 偏差：Python 计算了 mem.percent 却未放进 payload，此处补 used_percent 为超集
// （Go 前端 ToolPanel 读取该键，见 spec D1）。
func memoryCompute(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		rssBytes, totalBytes, availableBytes, usedPercent, err := collectMemory()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("method", "memory.compute").
				Msg("memory.compute collect failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{
			"rss_mb":       bytesToMB(rssBytes),
			"total_mb":     bytesToMB(totalBytes),
			"available_mb": bytesToMB(availableBytes),
			"used_percent": usedPercent,
		}, "", "")
	}
}

// defaultCollectMemory 组合进程 RSS 与系统内存采集（psutil 等价物）。
//
// Python: process = psutil.Process(); process.memory_info().rss; psutil.virtual_memory()
// 错误原样透传，对齐 Python「psutil 异常直接冒到 register_method 包装层」语义。
func defaultCollectMemory() (rssBytes, totalBytes, availableBytes uint64, usedPercent float64, err error) {
	rssBytes, err = processRSSBytes()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	vm, err := systemVirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return rssBytes, vm.Total, vm.Available, vm.UsedPercent, nil
}

// defaultProcessRSSBytes 读取本进程物理内存（RSS）。
func defaultProcessRSSBytes() (uint64, error) {
	proc, err := process.NewProcess(int32(currentPID()))
	if err != nil {
		return 0, err
	}
	procMem, err := proc.MemoryInfo()
	if err != nil {
		return 0, err
	}
	return procMem.RSS, nil
}

// bytesToMB 字节转 MB，对齐 Python bytes / (1024 * 1024)。
func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}

// ──────────────────────────── IM 平台配置族（B1b-2） ────────────────────────────

// registerChannelConfFamily 注册 9 个 IM 平台的 get_conf / set_conf。
//
// Python: app_web_handlers.py:5844-5863（9 组 channel.<p>.{get,set}_conf 注册）。
func registerChannelConfFamily(wc *WebChannel, channelMgr *cm.ChannelManager, agentClient *routing.AgentClient) {
	// 逐条显式注册（与上游 5844-5863 的 18 条 register_method 一一对应，
	// 也让 cmd/rpc-matrix 的字面量扫描能识别）。
	wc.registerMethod("channel.feishu.get_conf", channelConfGet(wc, channelMgr, "feishu"), false)
	wc.registerMethod("channel.feishu.set_conf", channelConfSet(wc, channelMgr, agentClient, "feishu"), false)
	wc.registerMethod("channel.xiaoyi.get_conf", channelConfGet(wc, channelMgr, "xiaoyi"), false)
	wc.registerMethod("channel.xiaoyi.set_conf", channelConfSet(wc, channelMgr, agentClient, "xiaoyi"), false)
	wc.registerMethod("channel.telegram.get_conf", channelConfGet(wc, channelMgr, "telegram"), false)
	wc.registerMethod("channel.telegram.set_conf", channelConfSet(wc, channelMgr, agentClient, "telegram"), false)
	wc.registerMethod("channel.dingtalk.get_conf", channelConfGet(wc, channelMgr, "dingtalk"), false)
	wc.registerMethod("channel.dingtalk.set_conf", channelConfSet(wc, channelMgr, agentClient, "dingtalk"), false)
	wc.registerMethod("channel.whatsapp.get_conf", channelConfGet(wc, channelMgr, "whatsapp"), false)
	wc.registerMethod("channel.whatsapp.set_conf", channelConfSet(wc, channelMgr, agentClient, "whatsapp"), false)
	wc.registerMethod("channel.discord.get_conf", channelConfGet(wc, channelMgr, "discord"), false)
	wc.registerMethod("channel.discord.set_conf", channelConfSet(wc, channelMgr, agentClient, "discord"), false)
	wc.registerMethod("channel.slack.get_conf", channelConfGet(wc, channelMgr, "slack"), false)
	wc.registerMethod("channel.slack.set_conf", channelConfSet(wc, channelMgr, agentClient, "slack"), false)
	wc.registerMethod("channel.wecom.get_conf", channelConfGet(wc, channelMgr, "wecom"), false)
	wc.registerMethod("channel.wecom.set_conf", channelConfSet(wc, channelMgr, agentClient, "wecom"), false)
	wc.registerMethod("channel.wechat.get_conf", channelConfGet(wc, channelMgr, "wechat"), false)
	wc.registerMethod("channel.wechat.set_conf", channelConfSet(wc, channelMgr, agentClient, "wechat"), false)
}

// channelConfGet 返回平台通道当前配置。
//
// Python: _channel_<p>_get_conf（app_web_handlers.py:4365 起，9 组同构）：
// cm 缺失 → SERVICE_UNAVAILABLE；否则回 {"config": conf}（feishu/xiaoyi 先归一化）。
func channelConfGet(wc *WebChannel, channelMgr *cm.ChannelManager, platform string) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		if channelMgr == nil {
			logger.Warn(logComponent).
				Str("method", "channel."+platform+".get_conf").
				Str("req_id", reqID).
				Msg("channel manager not available")
			wc.sendResponse(ws, reqID, false, nil, "channel manager not available", WsErrServiceUnavailable)
			return
		}
		conf := normalizeChannelConf(platform, channelMgr.GetConf(platform))
		wc.sendResponse(ws, reqID, true, map[string]any{"config": conf}, "", "")
	}
}

// channelConfSet 更新平台通道配置并写回 config.yaml。
//
// Python: _channel_<p>_set_conf（app_web_handlers.py:4392 起）：
// feishu/xiaoyi 走多应用分支（必须含 apps，归一化后按 app_id 合并保留未发送的敏感字段，
// 整体替换 channels.<p>.apps 并清理旧平铺键）；其余平台整体写入后回读落盘。
func channelConfSet(wc *WebChannel, channelMgr *cm.ChannelManager, agentClient *routing.AgentClient, platform string) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		method := "channel." + platform + ".set_conf"
		if channelMgr == nil {
			logger.Warn(logComponent).
				Str("method", method).
				Str("req_id", reqID).
				Msg("channel manager not available")
			wc.sendResponse(ws, reqID, false, nil, "channel manager not available", WsErrServiceUnavailable)
			return
		}
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}

		// 多应用平台：params 必须含 apps
		if platform == "feishu" || platform == "xiaoyi" {
			appsRaw, hasApps := params["apps"]
			if !hasApps {
				// Python: params["apps"] 触发 KeyError，被 except Exception 兜成 INTERNAL_ERROR
				wc.sendResponse(ws, reqID, false, nil, "'apps'", WsErrInternalError)
				return
			}
			apps, isList := appsRaw.([]any)
			if !isList {
				logger.Error(logComponent).
					Str("method", method).
					Str("req_id", reqID).
					Msg("apps must be list")
				wc.sendResponse(ws, reqID, false, nil, "apps must be list", WsErrInternalError)
				return
			}
			normalizedConf := normalizeChannelConf(platform, map[string]any{"apps": apps})
			normalized, _ := normalizedConf["apps"].([]any)
			existing := channelMgr.GetConf(platform)
			existingApps, _ := existing["apps"].([]any)
			merged := mergeAppsByID(normalized, existingApps)
			channelMgr.SetConf(platform, map[string]any{"apps": merged})

			clearAfterRespond := false
			if err := replaceChannelSubsectionWithCleanup(platform, "apps", merged, []string{"apps", "send_file_allowed"}); err != nil {
				logger.Warn(logComponent).
					Err(err).
					Str("method", method).
					Msg("写回 config.yaml apps 失败")
			} else if platform == "feishu" {
				// 对齐 Python：feishu 在 finally 里清缓存（响应之后），xiaoyi 在响应之前
				clearAfterRespond = true
			} else {
				clearAgentConfigCache(agentClient, context.Background())
			}

			wc.sendResponse(ws, reqID, true, map[string]any{"config": map[string]any{"apps": merged}}, "", "")
			if clearAfterRespond {
				clearAgentConfigCache(agentClient, context.Background())
			}
			return
		}

		// 其余平台：整体写入 ChannelManager，回读结果写回 config.yaml 并清 Agent 配置缓存
		channelMgr.SetConf(platform, params)
		conf := channelMgr.GetConf(platform)
		if err := updateChannelInConfig(platform, conf); err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("method", method).
				Msg("写回 config.yaml 失败")
		} else {
			clearAgentConfigCache(agentClient, context.Background())
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"config": conf}, "", "")
	}
}

// normalizeChannelConf 归一化平台配置：feishu/xiaoyi 补 apps 缺省字段，其余平台原样返回。
//
// Python: _normalize_feishu_conf / _normalize_xiaoyi_conf（其余平台直接透传 cm.get_conf）。
func normalizeChannelConf(platform string, raw map[string]any) map[string]any {
	switch platform {
	case "feishu":
		return normalizeFeishuConf(raw)
	case "xiaoyi":
		return normalizeXiaoyiConf(raw)
	default:
		if raw == nil {
			return map[string]any{}
		}
		return raw
	}
}

// normalizeFeishuConf 把 channels.feishu 统一为 apps 格式并为每个 app 补缺省字段。
//
// Python: _normalize_feishu_conf (app_web_handlers.py:1646-1672)。
// 输入可以是旧平铺格式（{"app_id": ...}）或新多应用格式（{"apps": [...]}）；
// 返回结果始终带 apps 列表。
func normalizeFeishuConf(raw map[string]any) map[string]any {
	return normalizeAppsConf(raw, feishuAppDefaults, singleFeishuApp)
}

// normalizeXiaoyiConf 把 channels.xiaoyi 统一为 apps 格式并为每个 app 补缺省字段。
//
// Python: _normalize_xiaoyi_conf (app_web_handlers.py:1711-1737)。
func normalizeXiaoyiConf(raw map[string]any) map[string]any {
	return normalizeAppsConf(raw, xiaoyiAppDefaults, singleXiaoyiApp)
}

// normalizeAppsConf 多应用平台归一化的公共实现（feishu / xiaoyi 同构）。
//
// 非 dict 输入 → {"apps": []}；含 apps → 逐项补默认值；无 apps → 旧平铺格式转单 app。
func normalizeAppsConf(raw map[string]any, defaults map[string]any, single func(map[string]any) map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{"apps": []any{}}
	}
	if appsRaw, hasApps := raw["apps"]; hasApps {
		apps := make([]any, 0)
		if list, isList := appsRaw.([]any); isList {
			for _, item := range list {
				if app, isMap := item.(map[string]any); isMap {
					apps = append(apps, mergeMaps(defaults, app))
				}
				// 非对象条目：Python 展开会抛 TypeError → INTERNAL_ERROR；这里跳过以免脏数据阻断整表
			}
		}
		out := mergeMaps(raw, nil)
		out["apps"] = apps
		return out
	}
	out := mergeMaps(raw, nil)
	out["apps"] = []any{single(raw)}
	return out
}

// singleFeishuApp 把单个平铺飞书配置转成 apps 列表项。
//
// Python: _normalize_single_feishu_to_app (app_web_handlers.py:1675-1693)：
// 默认值 → is_default=True → 各字段取值 → `**raw` 覆盖，等价 默认值 ⊕ {is_default:true} ⊕ raw。
func singleFeishuApp(raw map[string]any) map[string]any {
	app := mergeMaps(feishuAppDefaults, raw)
	if _, hasDefault := raw["is_default"]; !hasDefault {
		app["is_default"] = true
	}
	return app
}

// singleXiaoyiApp 把单个平铺小艺配置转成 apps 列表项。
//
// Python: _normalize_single_xiaoyi_to_app (app_web_handlers.py:1740-1776)，语义同上。
func singleXiaoyiApp(raw map[string]any) map[string]any {
	app := mergeMaps(xiaoyiAppDefaults, raw)
	if _, hasDefault := raw["is_default"]; !hasDefault {
		app["is_default"] = true
	}
	return app
}

// mergeAppsByID 按 app_id 把新 apps 与已有 apps 合并，保留前端未发送的字段。
//
// Python: _merge_apps_by_id (app_web_handlers.py:1603-1643)：
// 各平台归一化会用默认空值填充前端未发的字段，合并时以已有值为基座、新值覆盖，
// 避免已配置的敏感字段（app_secret / sk）被空值覆盖丢失。
func mergeAppsByID(newApps, existingApps []any) []any {
	if len(existingApps) == 0 {
		return newApps
	}
	existingByID := make(map[string]map[string]any, len(existingApps))
	for _, item := range existingApps {
		app, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if appID, _ := app["app_id"].(string); appID != "" {
			existingByID[appID] = app
		}
	}
	if len(existingByID) == 0 {
		return newApps
	}
	merged := make([]any, 0, len(newApps))
	for _, item := range newApps {
		app, isMap := item.(map[string]any)
		if !isMap {
			merged = append(merged, item)
			continue
		}
		appID, _ := app["app_id"].(string)
		if existing, hit := existingByID[appID]; hit && appID != "" {
			merged = append(merged, mergeMaps(existing, app))
			continue
		}
		merged = append(merged, app)
	}
	return merged
}

// mergeMaps 返回 base 叠加 override 后的新 map（override 覆盖同名键）。base 为 nil 时只拷贝 override。
func mergeMaps(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

// updateChannelInConfig 只更新 channels[<platform>] 并写回 config.yaml。
//
// Python: update_channel_in_config (gateway/common/config.py:495-506)：
// 逐键合并进既有 section，round-trip 写回。
func updateChannelInConfig(platform string, conf map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	raw, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	section := mapSection(raw, "channels", platform)
	merged := mergeMaps(section, conf)
	return cfg.SetRoundTrip("channels."+platform, merged)
}

// replaceChannelSubsectionWithCleanup 整体替换 channels[<platform>][<subsection>] 并清理旧字段。
//
// Python: replace_channel_subsection_with_cleanup (gateway/common/config.py:600-622)：
// 写入 subsection 后删除 section 下不在 keepKeys 中的键（多应用模式替换 apps、清掉旧平铺字段）。
func replaceChannelSubsectionWithCleanup(platform, subsection string, value any, keepKeys []string) error {
	cfg, err := config.New("")
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	raw, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	section := mapSection(raw, "channels", platform)
	next := make(map[string]any, len(keepKeys)+1)
	for _, key := range keepKeys {
		if v, ok := section[key]; ok {
			next[key] = v
		}
	}
	next[subsection] = value
	return cfg.SetRoundTrip("channels."+platform, next)
}

// mapSection 按路径取嵌套 mapping，缺失或类型不符时返回空 map。
func mapSection(root map[string]any, keys ...string) map[string]any {
	cur := root
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

// defaultUpdaterRuntimeConfig 返回升级服务的默认运行时配置。
//
// ⤵️ 升级子系统（gateway/common/updater.py，578 行）本期未移植：updater.* 一律回
// 上游 UpdaterService.get_runtime_config() 的默认形态（updater.py:137-155 / 515-570），
// 前端 UpdatePanel 可正常渲染，但"检查更新/下载/升级"不会真正执行。
func defaultUpdaterRuntimeConfig() map[string]any {
	return map[string]any{
		"enabled":                    true,
		"desktop_release_api_type":   "gitcode",
		"release_api_type":           "pypi",
		"install_mode":               "pip",
		"repo_owner":                 "openJiuwen",
		"repo_name":                  "jiuwenswarm",
		"release_api_url":            "",
		"asset_name_pattern":         "",
		"asset_name_pattern_windows": "",
		"asset_name_pattern_macos":   "",
		"asset_name_pattern_linux":   "JiuwenSwarm-{version}.tar.gz",
		"timeout_seconds":            20,
		"pypi_mirror":                "https://mirrors.aliyun.com/pypi",
		"access_token":               "",
	}
}

// ──────────────────────────── 杂项本地方法（B1b-4） ────────────────────────────

// connectionStatus 返回 Gateway / AgentServer 就绪状态，供 Web 客户端探测后端链路。
//
// Python: _connection_status (app_web_handlers.py:5676-5684)。
func connectionStatus(wc *WebChannel, agentClient *routing.AgentClient) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		ready := agentClient != nil && agentClient.ServerReady()
		wc.sendResponse(ws, reqID, true, map[string]any{
			"agent_ready":      ready,
			"protocol_version": "1.0",
		}, "", "")
	}
}

// commandsList 下发快捷面板命令清单（静态元数据，本地返回不转发）。
//
// Python: _commands_list (app_web_handlers.py:4010-4019) → list_builtin_commands；
// 异常回 INTERNAL_ERROR。
func commandsList(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, commandParser.ListBuiltinCommands(params), "", "")
	}
}

// configMoaStatus 返回 MoA（多模型聚合）就绪状态。
//
// Python: config.moa_status 走 E2A 到 AgentServer 的 moa_status()
// （jiuwenswarm/server/runtime/moa_runtime.py:106-119），后者在选中的 model group
// 缺失/不可解析时固定回 {"configured": false}。
//
// ⤵️ 模型目录子系统本期未移植（见计划 Task 8），因此这里固定返回"未配置"的默认 DTO；
// 面板据此显示 MoA 未启用，不会误报可用。
func configMoaStatus(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, _ string) {
		wc.sendResponse(ws, reqID, true, map[string]any{"configured": false}, "", "")
	}
}

// chatSwarmflowReply 空 ack：三层路由把 reply 转发给 agent adapter（由其构造
// HumanAgentMessage 交给 team_manager），HTTP 层只需应答。
//
// Python: _chat_swarmflow_reply (app_web_handlers.py:4158-4163)。
func chatSwarmflowReply(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, _ map[string]any, sessionID string) {
		wc.sendResponse(ws, reqID, true, map[string]any{
			"accepted":   true,
			"session_id": sessionID,
		}, "", "")
	}
}

// ──────────────────────────── 外部 CLI（B1b-5） ────────────────────────────

// externalCLIAgentKinds 内置外部 CLI 种类（对齐 Python EXTERNAL_CLI_AGENT_KINDS）。
var externalCLIAgentKinds = []string{"claude", "codex"}

// externalCLIReferenceVersions 各内置 CLI 的参考版本（"期望版本"）。
//
// Python: external_cli_reference_version (config_set_handlers.py:330-342) 读安装包元数据
// （claude_agent_sdk._cli_version / openai-codex dist）；Go 侧无这些 Python 依赖，改用常量表，
// 取上游 uv.lock 的锁定版本。**上游升版时需同步这里**，否则会把正常版本误报为"偏旧"。
var externalCLIReferenceVersions = map[string]string{
	"claude": "0.2.115",
	"codex":  "0.144.4",
}

// externalCLIPruneMaxPaths prune 单次最多处理的路径数（对齐 _EXTERNAL_CLI_PRUNE_PATH_HISTORIES_MAX_PATHS）。
const externalCLIPruneMaxPaths = 50

// externalCLIPathHistoriesKey 路径历史在 config.yaml 中的位置。
//
// Python: EXTERNAL_CLI_PATH_HISTORIES_CONFIG_PATH (common/config.py:43)。
const externalCLIPathHistoriesKey = "modes.team.jiuwen_team.external_cli_path_histories"

// externalCLIDetect 探测外部 CLI 的可执行路径与版本。
//
// Python: _external_cli_detect (app_web_handlers.py:2268-2277) →
// detect_external_cli_agent (common/config_panel/config_set_handlers.py:464-563)。
func externalCLIDetect(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		cliAgent := strings.ToLower(strings.TrimSpace(strOrEmpty(params["cli_agent"])))
		cliPath := strings.TrimSpace(strOrEmpty(params["cli_path"]))
		// use_builtin：True 或字符串 "true"（对齐上游 raw is True or str(raw).lower()=="true"）
		useBuiltin := false
		if raw, ok := params["use_builtin"]; ok {
			if b, isBool := raw.(bool); isBool && b {
				useBuiltin = true
			} else if strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))) == "true" {
				useBuiltin = true
			}
		}
		wc.sendResponse(ws, reqID, true, detectExternalCLIAgent(cliAgent, cliPath, useBuiltin), "", "")
	}
}

// externalCLIValidatePaths 批量探测路径是否为存在的文件（三值：true/false/null）。
//
// Python: _external_cli_validate_paths (app_web_handlers.py:2280-2303)：
// 单次最多 50 条；null 表示"无法判定"（锁定/无权限/离线盘），调用方只应据 false 清理历史。
func externalCLIValidatePaths(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		rawPaths, ok := params["paths"].([]any)
		if !ok {
			wc.sendResponse(ws, reqID, false, nil, "paths must be array", WsErrBadRequest)
			return
		}
		if len(rawPaths) > externalCLIPruneMaxPaths {
			rawPaths = rawPaths[:externalCLIPruneMaxPaths]
		}
		results := make(map[string]any, len(rawPaths))
		for _, item := range rawPaths {
			path := strings.TrimSpace(strOrEmpty(item))
			if path == "" {
				continue
			}
			expanded := pathutil.ExpandHome(path)
			info, err := os.Stat(expanded)
			switch {
			case err != nil && os.IsNotExist(err):
				results[path] = false
			case err != nil:
				// 无法判定（权限/锁定/其他 IO 错误）→ null
				results[path] = nil
			default:
				results[path] = !info.IsDir()
			}
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"results": results}, "", "")
	}
}

// externalCLIPrunePathHistories 从指定种类的持久化路径历史里删除已确认缺失的路径。
//
// Python: _external_cli_prune_path_histories (app_web_handlers.py:2304-2326) →
// prune_external_cli_path_histories_in_config (common/config.py:1495-1550)。
func externalCLIPrunePathHistories(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			wc.sendResponse(ws, reqID, false, nil, "params must be object", WsErrBadRequest)
			return
		}
		cliAgent := strings.ToLower(strings.TrimSpace(strOrEmpty(params["cli_agent"])))
		rawRemove, ok := params["remove_paths"].([]any)
		if cliAgent == "" {
			wc.sendResponse(ws, reqID, false, nil, "cli_agent is required", WsErrBadRequest)
			return
		}
		if !ok {
			wc.sendResponse(ws, reqID, false, nil, "remove_paths must be array", WsErrBadRequest)
			return
		}
		if len(rawRemove) > externalCLIPruneMaxPaths {
			rawRemove = rawRemove[:externalCLIPruneMaxPaths]
		}
		paths := make([]string, 0, len(rawRemove))
		for _, item := range rawRemove {
			paths = append(paths, strOrEmpty(item))
		}
		surviving, err := pruneExternalCLIPathHistories(cliAgent, paths)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("cli_agent", cliAgent).
				Msg("[external_cli.prune_path_histories] 剪枝失败")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"histories": surviving}, "", "")
	}
}

// externalCLIInstallStatus 返回外部 CLI 依赖安装状态。
//
// Python: _external_cli_install_status (app_web_handlers.py:2330-2340)：未知种类 → BAD_REQUEST；
// 否则回安装状态快照（冻结运行时再叠加 managed runtime 状态）。
//
// ⤵️ 安装器子系统（gateway/common/external_cli_runtime.py，1,121 行）本期未移植，
// 这里只做参数校验 + 默认快照（未安装/未在跑）。
func externalCLIInstallStatus(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		cliAgent := strings.ToLower(strings.TrimSpace(strOrEmpty(params["cli_agent"])))
		if !isKnownExternalCLIAgent(cliAgent) {
			wc.sendResponse(ws, reqID, false, nil, "unsupported external cli agent", WsErrBadRequest)
			return
		}
		payload := defaultExternalCLIInstallStatus()
		payload["cli_agent"] = cliAgent
		wc.sendResponse(ws, reqID, true, payload, "", "")
	}
}

// defaultExternalCLIInstallStatus 返回安装状态默认快照（未安装、无进行中任务）。
func defaultExternalCLIInstallStatus() map[string]any {
	return map[string]any{
		"installing": false,
		"installed":  false,
		"state":      "idle",
		"progress":   0,
		"error":      "",
	}
}

// isKnownExternalCLIAgent 判断是否为内置外部 CLI 种类。
func isKnownExternalCLIAgent(agent string) bool {
	for _, kind := range externalCLIAgentKinds {
		if kind == agent {
			return true
		}
	}
	return false
}

// detectExternalCLIAgent 探测外部 CLI 的路径/版本，返回上游同形状的 DTO。
//
// Python: detect_external_cli_agent / _detect_cli_at
// (config_set_handlers.py:464-563)。本仓差异：
//   - 无自定义 agent 注册表，未知种类直接回 status=unavailable；
//   - reference_version 取 Python 侧包元数据（claude_agent_sdk / openai-codex），本仓无此依赖，恒回空串；
//   - 不做 use_builtin 的托管 CLI 解析（managed runtime 未移植），退回 PATH 搜索。
func detectExternalCLIAgent(cliAgent, cliPath string, useBuiltin bool) map[string]any {
	if !isKnownExternalCLIAgent(cliAgent) {
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            "unavailable",
			"path":              "",
			"version":           "",
			"reference_version": "",
			"message":           "unsupported cli_agent: " + cliAgent,
		}
	}
	_ = useBuiltin
	referenceVersion := externalCLIReferenceVersions[cliAgent]

	resolved, reason, message := resolveExternalCLIPath(cliAgent, cliPath)
	if resolved == "" {
		status := "missing"
		if reason == "directory" || reason == "windows_script" {
			status = "unsupported"
		}
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            status,
			"path":              "",
			"version":           "",
			"reference_version": referenceVersion,
			"reason":            reason,
			"message":           message,
		}
	}

	versionOutput, versionErr := runExternalCLIVersionCommand(resolved)
	version := ""
	if match := externalCLIVersionRe.FindStringSubmatch(versionOutput); match != nil {
		version = match[1]
	}
	if strings.TrimSpace(versionOutput) == "" {
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            "unavailable",
			"path":              resolved,
			"version":           "",
			"reference_version": referenceVersion,
			"message":           firstNonEmpty(versionErr, "version command failed"),
		}
	}
	status := "ok"
	message = ""
	if version == "" {
		status = "warning"
		message = "version output could not be parsed"
	} else if detected, okDetected := parseVersionTuple(version); okDetected {
		// 只有"探测版本 < 参考版本"才降级为 warning（对齐上游 has_comparable_versions 分支）
		if reference, okReference := parseVersionTuple(referenceVersion); okReference && compareVersionTuple(detected, reference) < 0 {
			status = "warning"
			message = fmt.Sprintf("version %s may be incompatible with expected version %s", version, referenceVersion)
		}
	}
	return map[string]any{
		"cli_agent":         cliAgent,
		"status":            status,
		"path":              resolved,
		"version":           version,
		"reference_version": referenceVersion,
		"message":           message,
	}
}

// externalCLIVersionRe 从 --version 输出里提取语义版本号。
//
// Python: re.search(r"(\d+\.\d+\.\d+)", version_output) (config_set_handlers.py:537)。
var externalCLIVersionRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// resolveExternalCLIPath 解析外部 CLI 可执行文件路径。
//
// 显式路径：必须是存在的文件且不是目录（Python resolve_external_cli_path 的
// directory / windows_script 两个 reason 在此覆盖）；空路径回退 PATH 搜索。
func resolveExternalCLIPath(cliAgent, cliPath string) (resolved, reason, message string) {
	if p := strings.TrimSpace(cliPath); p != "" {
		expanded := pathutil.ExpandHome(p)
		info, err := os.Stat(expanded)
		if err != nil {
			if os.IsNotExist(err) {
				return "", "not_found", "not found: " + p
			}
			return "", "unreadable", err.Error()
		}
		if info.IsDir() {
			return "", "directory", "path is a directory: " + p
		}
		if runtime.GOOS == "windows" && isWindowsScriptPath(expanded) {
			return "", "windows_script", "windows_script"
		}
		return expanded, "", ""
	}
	found, err := exec.LookPath(cliAgent)
	if err != nil {
		return "", "not_found", err.Error()
	}
	if runtime.GOOS == "windows" && isWindowsScriptPath(found) {
		return "", "windows_script", "windows_script"
	}
	return found, "", ""
}

// isWindowsScriptPath 判断是否是不受支持的 Windows 脚本（.bat/.cmd/.ps1）。
func isWindowsScriptPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd", ".ps1":
		return true
	}
	return false
}

// runExternalCLIVersionCommand 执行 `<cli> --version` 并返回合并输出。
//
// 失败时返回的错误信息会作为 message 透传给前端（对齐上游 version_error）。
func runExternalCLIVersionCommand(resolved string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolved, "--version")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return "", err.Error()
	}
	return text, ""
}

// pruneExternalCLIPathHistories 从 config.yaml 的路径历史里删除已确认缺失的项。
//
// Python: prune_external_cli_path_histories_in_config (common/config.py:1495-1550)：
// 只删除「既有历史 ∩ removePaths」，去重保序；删完为空则移除该 key，整段为空则移除父键；
// 无变化时不写盘。
func pruneExternalCLIPathHistories(cliAgent string, removePaths []string) ([]string, error) {
	name := strings.TrimSpace(cliAgent)
	removeSet := make(map[string]struct{}, len(removePaths))
	for _, raw := range removePaths {
		if v := strings.TrimSpace(raw); v != "" {
			removeSet[v] = struct{}{}
		}
	}
	if name == "" || len(removeSet) == 0 {
		return []string{}, nil
	}

	cfg, err := config.New("")
	if err != nil {
		return nil, fmt.Errorf("创建配置管理器失败: %w", err)
	}
	if _, err := cfg.Load(); err != nil {
		return nil, fmt.Errorf("加载配置失败: %w", err)
	}
	histories, _ := cfg.Get(externalCLIPathHistoriesKey).(map[string]any)
	current, _ := histories[name].([]any)
	if histories == nil || current == nil {
		return []string{}, nil
	}

	surviving := make([]string, 0, len(current))
	seen := make(map[string]struct{}, len(current))
	original := make([]string, 0, len(current))
	for _, item := range current {
		value := strings.TrimSpace(strOrEmpty(item))
		if value == "" {
			continue
		}
		original = append(original, value)
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		if _, drop := removeSet[value]; !drop {
			surviving = append(surviving, value)
		}
	}
	if len(surviving) == len(original) {
		return surviving, nil
	}

	next := make(map[string]any, len(histories))
	for k, v := range histories {
		next[k] = v
	}
	if len(surviving) > 0 {
		next[name] = toAnySlice(surviving)
	} else {
		delete(next, name)
	}
	if len(next) == 0 {
		// 整段清空：写空 mapping（对读取等价于上游 pop）
		if err := cfg.SetRoundTrip(externalCLIPathHistoriesKey, map[string]any{}); err != nil {
			return nil, fmt.Errorf("写回路径历史失败: %w", err)
		}
		return surviving, nil
	}
	if err := cfg.SetRoundTrip(externalCLIPathHistoriesKey, next); err != nil {
		return nil, fmt.Errorf("写回路径历史失败: %w", err)
	}
	return surviving, nil
}

// toAnySlice 把 []string 转成 []any（写入 YAML 前统一形状）。
func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// firstNonEmpty 返回第一个非空字符串，全空时返回 fallback。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ──────────────────────────── 原生文件选择（B1b-6） ────────────────────────────

// pathSelectFile 弹出原生文件选择框，返回选中的文件路径。
//
// Python: _path_select_file (app_web_handlers.py:3948-3990)：
// 参数类型校验（initial_dir / initial_path / title 必须为字符串）→ 解析起始目录 →
// select_file_native 打开原生对话框；选择器不可用回 UNSUPPORTED；取消回
// {path: null, cancelled: true}；选中回 {path: 绝对路径, cancelled: false}。
func pathSelectFile(wc *WebChannel) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, _ string) {
		if params == nil {
			params = map[string]any{}
		}
		if v, has := params["initial_dir"]; has && v != nil {
			if _, ok := v.(string); !ok {
				wc.sendResponse(ws, reqID, false, nil, "initial_dir must be string", WsErrBadRequest)
				return
			}
		}
		if v, has := params["initial_path"]; has && v != nil {
			if _, ok := v.(string); !ok {
				wc.sendResponse(ws, reqID, false, nil, "initial_path must be string", WsErrBadRequest)
				return
			}
		}
		if v, has := params["title"]; has && v != nil {
			if _, ok := v.(string); !ok {
				wc.sendResponse(ws, reqID, false, nil, "title must be string", WsErrBadRequest)
				return
			}
		}

		resolvedDir := ""
		if v, ok := params["initial_dir"].(string); ok {
			resolvedDir = strings.TrimSpace(v)
		}
		// initial_path 优先：用其父目录作为起始目录（对齐上游 Path(initial_path).parent）
		if v, ok := params["initial_path"].(string); ok && strings.TrimSpace(v) != "" {
			resolvedDir = filepath.Dir(pathutil.ExpandHome(strings.TrimSpace(v)))
		}
		title, _ := params["title"].(string)

		selected, err := selectFileNativeFn(resolvedDir, strings.TrimSpace(title))
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("[path.select_file] picker unavailable")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrUnsupported)
			return
		}
		if selected == "" {
			wc.sendResponse(ws, reqID, true, map[string]any{"path": nil, "cancelled": true}, "", "")
			return
		}
		wc.sendResponse(ws, reqID, true, map[string]any{"path": selected, "cancelled": false}, "", "")
	}
}

// selectFileNativeFn 原生选择器入口（变量形式便于测试注入；生产值为 selectFileNative）。
var selectFileNativeFn = selectFileNative

// selectFileNative 调用平台原生文件选择器，返回选中路径（空串=用户取消）。
//
// Python: directory_picker.select_file_native（tkinter/zenity 等）。
// Go 侧按平台可用的对话框程序依次尝试：zenity → kdialog（Linux）；osascript（macOS）。
// 都不可用时返回错误，调用方回 UNSUPPORTED（与上游 headless 下 picker 抛异常一致）。
func selectFileNative(initialDir, title string) (string, error) {
	if title == "" {
		title = "选择文件"
	}
	switch runtime.GOOS {
	case "darwin":
		script := `POSIX path of (choose file with prompt "` + escapeAppleScript(title) + `")`
		return runPickerCommand("osascript", "-e", script)
	case "windows":
		return "", fmt.Errorf("native file picker is not available on windows builds")
	default:
		for _, candidate := range [][]string{
			{"zenity", "--file-selection", "--title", title, "--filename", pickerInitialPath(initialDir)},
			{"kdialog", "--getopenfilename", pickerInitialPath(initialDir), title},
		} {
			out, err := runPickerCommand(candidate[0], candidate[1:]...)
			if err == nil {
				return out, nil
			}
			if !isCommandMissing(err) {
				return "", err
			}
		}
		return "", fmt.Errorf("native file picker is not available (zenity/kdialog not found)")
	}
}

// pickerInitialPath 把起始目录转成对话框可用的初始路径（空则留空由对话框决定）。
func pickerInitialPath(initialDir string) string {
	if initialDir == "" {
		return ""
	}
	return initialDir
}

// runPickerCommand 执行对话框命令并返回去空白后的输出（用户取消 → 空串 + nil）。
func runPickerCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	text := strings.TrimSpace(string(out))
	if err != nil {
		// 用户取消在多数对话框里是非 0 退出且无输出，按取消处理
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && text == "" {
			return "", nil
		}
		return "", err
	}
	return text, nil
}

// isCommandMissing 判断错误是否为"命令不存在"（用于回退到下一个对话框程序）。
func isCommandMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// escapeAppleScript 转义 AppleScript 字符串里的双引号与反斜杠。
func escapeAppleScript(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	return strings.ReplaceAll(text, `"`, `\"`)
}

// ──────────────────────────── session 薄代理（B1b-7） ────────────────────────────

// sessionProxyForward 把会话类请求经 E2A 原样转发给 AgentServer。
//
// Python: _session_rename / _session_plan_status (app_web_handlers.py:5692-5693) →
// proxy_unary_request (gateway/routing/e2a_proxy.py:99-175)：
// 客户端不可达 → SERVICE_UNAVAILABLE「AgentServer is unavailable」；失败回包的 code 缺省
// 用 BAD_REQUEST（default_error_code）。
func sessionProxyForward(wc *WebChannel, ac *routing.AgentClient, method string) methodHandler {
	return func(ws wsSender, reqID string, params map[string]any, sessionID string) {
		if ac == nil || !ac.ServerReady() {
			logger.Warn(logComponent).
				Str("method", method).
				Str("req_id", reqID).
				Msg("AgentServer is unavailable")
			wc.sendResponse(ws, reqID, false, nil, "AgentServer is unavailable", WsErrServiceUnavailable)
			return
		}

		env := e2a.E2AFromAgentFields(reqID,
			e2a.WithFieldChannelID(wc.ChannelID()),
			e2a.WithFieldSessionID(sessionID),
			e2a.WithFieldReqMethod(method),
			e2a.WithFieldParams(params),
		)
		resp, err := ac.SendRequest(context.Background(), env)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("method", method).
				Msg("session proxy forwarding to AgentServer failed")
			wc.sendResponse(ws, reqID, false, nil, err.Error(), WsErrInternalError)
			return
		}
		if resp == nil {
			wc.sendResponse(ws, reqID, false, nil, "AgentServer returned empty response", WsErrInternalError)
			return
		}
		if resp.OK {
			wc.sendResponse(ws, reqID, true, resp.Payload, "", "")
			return
		}
		errMsg := "request failed"
		if e, ok := resp.Payload["error"].(string); ok && e != "" {
			errMsg = e
		}
		code := WsErrBadRequest
		if c, ok := resp.Payload["code"].(string); ok && c != "" {
			code = c
		}
		wc.sendResponse(ws, reqID, false, nil, errMsg, code)
	}
}

// parseVersionTuple 把 "x.y.z" 解析成三段整数元组；缺段按 0 补，非数字段视为不可解析。
//
// Python: parse_version_tuple (config_set_handlers.py 内部助手)：解析失败返回 None，
// 上游据此跳过"版本偏旧"比较。
func parseVersionTuple(version string) ([3]int, bool) {
	var out [3]int
	text := strings.TrimSpace(version)
	if text == "" {
		return out, false
	}
	parts := strings.Split(text, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// compareVersionTuple 比较两个三段版本元组：a<b 返回 -1，相等 0，a>b 返回 1。
func compareVersionTuple(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
