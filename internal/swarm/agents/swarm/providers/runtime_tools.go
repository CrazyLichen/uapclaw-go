package providers

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	agentcron "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	teamtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// runtime_tools.go —— 成员运行时工具工厂（组织/cron/send_file/session_messaging）。
//
// Python: jiuwenswarm/agents/swarm/providers/runtime_tools.py（333 行）。
// 常量值由 registry.go 常量层承载。org 两工具（expert_org 域）基座缺
// ⤴️15.20；session_messaging ⤴️25.6（F2）；cron（11.10 基座）与 send_file
// （10.6.24b 基座）全量真实实现。

// ──────────────────────────── 常量 ────────────────────────────

const (
	constOrgOwnerInboxTools   = "swarm.organization_owner_inbox_tools"
	constOrgModelTools        = "swarm.organization_model_tools"
	constSessionMessagingTools = "swarm.session_messaging_tools"
	constCronTools            = "swarm.cron_tools"
	constSendFile             = "swarm.send_file"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildOrganizationOwnerInboxTools leader-only Owner 结果确认工具。
// 基座 OrgAckOwnerResultTool（expert_org/inbox_tool.py）Go 未实施 ⤴️15.20。
// Python: build_organization_owner_inbox_tools (runtime_tools.py:56-62)
func buildOrganizationOwnerInboxTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	if view.Role != "leader" {
		return nil
	}
	// Python: [OrgAckOwnerResultTool(ctx.team_id, ctx.session_id)]
	return nil
}

// buildOrganizationModelTools leader-only 持久专家 Team 模型控制工具。
// 基座 OrgSetTeamModelTool（expert_org/model_tool.py）Go 未实施 ⤴️15.20。
// Python: build_organization_model_tools (runtime_tools.py:65-71)
func buildOrganizationModelTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	if view.Role != "leader" {
		return nil
	}
	return nil
}

// buildSessionMessagingTools leader-only 跨会话持久消息工具。
// 分支体（SessionMessagingToolkit）⤴️25.6 群聊（缝 F2）。
// Python: build_session_messaging_tools (runtime_tools.py:79-88)
func buildSessionMessagingTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	if view.Role != "leader" {
		return nil
	}
	return nil
}

// buildCronTools 每成员 cron 工具（team 成员作用域）。
//
// 镜像 register_member_runtime_tools 的 cron 分支：构造 team 模式 cron 上下文
// 并委托 CronRuntimeBridge.BuildTools。cron 执行会话跳过（三个判定信号与
// swarm.heartbeat / code_rails permission rail 对齐）。
// Python: build_cron_tools (runtime_tools.py:122-216)
func buildCronTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	agentID := view.MemberCardID
	metadata := copyMapOrEmpty(view.RequestMetadata)
	sessionID := trimSpaceOrEmpty(view.SessionID)
	// Python :152-176：session 元数据补齐（project_id/project_dir/work_mode/
	// model_name 缺位时从 session_metadata 读，cache_bust=True，失败 debug 跳过）。
	// Go 基座 session_metadata 读取段 ⤴️15.20（lifecycle GetSessionMetadata）。
	_ = sessionID

	cronCtx := &agentcron.CronToolContext{
		ChannelID: orEmpty(view.ChannelID, "web"),
		SessionID: sessionID,
		Metadata:  metadata,
		Mode:      orEmpty(view.Mode, "team"),
	}
	if agentID != "" {
		cronCtx.ToolScopeOverride = "team_member_" + agentID
	} else {
		cronCtx.ToolScopeOverride = "team_member_unknown"
	}
	cronExecutionSession := mapKeyTruthy(view.RequestMetadata, "cron") ||
		trimSpaceOrEmpty(view.ChannelID) == "__cron__" ||
		isCronExecutionSession(sessionID)
	if cronExecutionSession {
		logger.Info(logger.ComponentAgentServer).
			Str("agent_id", agentID).
			Msg("[swarm.cron_tools] skip cron tools for cron execution session")
		return nil
	}
	bridge := cronbridge.NewCronRuntimeBridge()
	tools := bridge.BuildTools(cronCtx, agentID, stringFieldOrEmpty(view.Language, "cn"))
	logger.Info(logger.ComponentAgentServer).
		Int("count", len(tools)).
		Str("agent_id", agentID).
		Msg("[swarm.cron_tools] built cron tools")
	return tools
}

// buildSendFileTools send_file_to_user 工具组。
//
// 需要 request_id + channel_id，并被渠道 send_file_allowed 配置门控。
// Python: build_send_file_tools (runtime_tools.py:275-333)
func buildSendFileTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	requestID := view.RequestID
	channelID := view.ChannelID
	if requestID == "" || channelID == "" {
		logger.Info(logger.ComponentAgentServer).
			Msg("[swarm.send_file] skipped: missing request_id or channel_id")
		return nil
	}
	channelsConfig, _ := params["channels_config"].(map[string]any)
	if channelsConfig == nil {
		channelsConfig = map[string]any{}
	}
	if !isSendFileEnabled(map[string]any{"channels": channelsConfig}, channelID) {
		logger.Info(logger.ComponentAgentServer).
			Str("channel", channelID).
			Msg("[swarm.send_file] skipped: send_file_allowed=False")
		return nil
	}
	metadata := copyMapOrEmpty(view.RequestMetadata)
	toolkit := teamtools.NewSendFileToolkit(requestID, sessionOrEmpty(view), channelID, teamtools.SendFileRuntimeContext{
		Metadata:          metadata,
		UserID:            view.UserID,
		ProjectDir:        view.ProjectDir,
		TeamWorkspaceRoot: view.TeamWsRoot,
	})
	return toolkit.GetTools()
}

// sessionOrEmpty 会话 id（空串安全）。
func sessionOrEmpty(view *buildctx.SwarmBuildContext) string { return view.SessionID }

// isSendFileEnabled 渠道是否允许发文件。
// 读 channels.<channel_id>.send_file_allowed；全双工内部委托未配置时继承
// Web 策略。
// Python: _is_send_file_enabled → is_send_file_enabled（config 段）；
// Go 基座 IsSendFileEnabled（10.6.24b）。缺渠道键 → 继承 web 渠道开关。
func isSendFileEnabled(config map[string]any, channelID string) bool {
	channels, _ := config["channels"].(map[string]any)
	entry, _ := channels[channelID].(map[string]any)
	if allowed, ok := entry["send_file_allowed"]; ok {
		v, _ := allowed.(bool)
		return v
	}
	// 全双工委托继承：channel_id 以 "__full_duplex" 委托前缀时回落 web 渠道
	// （对齐 is_send_file_enabled 的 delegate 继承段）。
	if strings.Contains(channelID, "__full_duplex") {
		web, _ := channels["web"].(map[string]any)
		if allowed, ok := web["send_file_allowed"]; ok {
			v, _ := allowed.(bool)
			return v
		}
	}
	return true
}

// ──────────────────────────── 导出函数（注册） ────────────────────────────

// RegisterRuntimeToolProviders 注册 runtime_tools 全部 tool provider。
// Python: runtime_tools.py @harness_element(kind=TOOL) 段（:55-333）。
func RegisterRuntimeToolProviders() {
	hschema.RegisterToolProvider(constOrgOwnerInboxTools, toolProviderAdapter(buildOrganizationOwnerInboxTools))
	hschema.RegisterToolProvider(constOrgModelTools, toolProviderAdapter(buildOrganizationModelTools))
	hschema.RegisterToolProvider(constSessionMessagingTools, toolProviderAdapter(buildSessionMessagingTools))
	hschema.RegisterToolProvider(constCronTools, toolProviderAdapter(buildCronTools))
	hschema.RegisterToolProvider(constSendFile, toolProviderAdapter(buildSendFileTools))
}
