package runtime

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentManagerPort 运行时需要的 manager 能力（由 server 层实现，避免反向依赖）。
type AgentManagerPort interface {
	// GetChatAgent 按 mode/subMode/projectDir 取聊天 agent
	GetChatAgent(ctx context.Context, channelID, mode, subMode, projectDir string) (ChatAgentPort, error)
}

// ChatAgentPort 运行时需要的 agent 能力。
type ChatAgentPort interface {
	// ProcessMessage 非流式执行
	ProcessMessage(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)
	// ProcessMessageStream 流式执行
	ProcessMessageStream(ctx context.Context, req *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error)
	// PlanStateAccessor 返回 plan 状态读写入口
	PlanStateAccessor() PlanStateAccessor
}

// ResolvedMode 请求模式解析结果。
type ResolvedMode struct {
	// CanonicalMode 权威模式串
	CanonicalMode string
	// IsPlan 是否规划模式
	IsPlan bool
	// IsTeam 是否团队模式
	IsTeam bool
	// NormalMode 退出 plan 后应回到的普通模式
	NormalMode string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PlanEntrySourceSlashCommand TUI /plan 命令来源
	PlanEntrySourceSlashCommand = "slash_command"
	// PlanEntrySourcePlanToggle Web 计划开关来源
	PlanEntrySourcePlanToggle = "plan_toggle"
	// PlanEntrySourceKey 请求参数中的进入来源键
	PlanEntrySourceKey = "plan_entry_source"
	// PlanReminderOriginalQueryKey 激活提醒改写前原 query 的存放键
	PlanReminderOriginalQueryKey = "plan_reminder_original_query"
	// PlanModeExitedEvent 计划模式结束事件类型
	PlanModeExitedEvent = "plan_mode_exited"
	// PreviousSessionModeKey 上一次会话模式参数键
	PreviousSessionModeKey = "_session_previous_mode"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// chatTurnMethods 参与 plan 状态同步的请求方法（空串视为默认 chat）。
var chatTurnMethods = map[string]struct{}{
	"":            {},
	"chat.send":   {},
	"chat":        {},
	"chat.resume": {},
	"chat.answer": {},
}

// planEntrySources 显式进入计划模式的来源集合。
var planEntrySources = map[string]struct{}{
	PlanEntrySourceSlashCommand: {},
	PlanEntrySourcePlanToggle:   {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveRequestRuntimeMode 把请求 mode/subMode 解析为权威模式。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode。
// 三段 canonical 直接按串语义解析（见 schema.ResolveNewCanonicalMode）；
// 其余走归一并按后缀判定 plan/team。
func ResolveRequestRuntimeMode(mode, subMode string) ResolvedMode {
	raw := strings.TrimSpace(mode)
	if raw == "" {
		if sub := strings.TrimSpace(subMode); sub != "" {
			raw = "agent.work." + sub
		}
	}
	canonical := string(schema.NormalizeMode(raw))
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    baseModeWithoutPlan(canonical),
	}
}

// baseModeWithoutPlan 返回退出 plan 后应回到的普通模式。
//
// Python: mode_matrix.base_mode_without_plan / _PLAN_EXIT_MODES
// 新三段命名下 `x.y.plan` → `x.y.normal`；非 plan 串原样返回。
func baseModeWithoutPlan(canonical string) string {
	if strings.HasSuffix(canonical, ".plan") {
		return strings.TrimSuffix(canonical, ".plan") + ".normal"
	}
	return canonical
}

// ShouldSync 判断请求是否参与 plan 状态同步。
//
// Python: PlanModeController.should_sync（只认 chat 类方法）。
func ShouldSync(reqMethod string) bool {
	_, ok := chatTurnMethods[strings.TrimSpace(reqMethod)]
	return ok
}

// IsPlanEntrySource 判断进入来源是否属于显式进入。
//
// Python: PlanModeController.is_explicit_entry
func IsPlanEntrySource(source string) bool {
	_, ok := planEntrySources[strings.TrimSpace(source)]
	return ok
}

// ResolveAgentRequestMode 解析 mode 为 manager mode、sub-mode 与 canonical mode。
//
// Python: jiuwenswarm/runtime/request.py resolve_agent_request_mode（L266-330）。
// 与上游一致：裸 "agent"/"plan"/"fast" 不下沉 plan；只有显式 plan 串才带 plan 状态。
func ResolveAgentRequestMode(rawMode any, workMode string) (mode, subMode, canonical string) {
	text := ""
	if s, ok := rawMode.(string); ok {
		text = strings.ToLower(strings.TrimSpace(s))
	}
	// 新三段 canonical 短路：environment/state 已内嵌，不能再按第一个点拆
	if m, s, c, ok := schema.ResolveNewCanonicalMode(text); ok {
		return m, s, c
	}
	if text == "auto_harness" {
		return "auto_harness", "auto_harness", "auto_harness"
	}
	normalizedWork := strings.ToLower(strings.TrimSpace(workMode))
	if text == "plan" || text == "fast" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team.plan 属历史串，canonicalize 后落到 team.work.plan
	if canonicalMode := schema.CanonicalizeMode(text); canonicalMode != text {
		if m, s, c, ok := schema.ResolveNewCanonicalMode(canonicalMode); ok {
			return m, s, c
		}
	}
	parts := strings.SplitN(text, ".", 2)
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	if mode == "agent" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	// team 族的 subMode 不参与实例查找（上游显式置 None）
	if mode == "team" {
		return "team", "", "team"
	}
	if len(parts) > 1 {
		subMode = parts[1]
	}
	if mode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	canonical = mode
	if subMode != "" {
		canonical = mode + "." + subMode
	}
	return mode, subMode, canonical
}

// ApplyResolvedModeToRequest 解析并把 canonical mode 写回 request.Params。
//
// Python: jiuwenswarm/runtime/request.py apply_resolved_mode_to_request（L345-360）。
func ApplyResolvedModeToRequest(req *schema.AgentRequest, workMode string) (mode, subMode string) {
	params := parseRequestParams(req)
	mode, subMode, canonical := ResolveAgentRequestMode(params["mode"], workMode)
	params["mode"] = canonical
	writeRequestParams(req, params)
	return mode, subMode
}

// ResolveRequestRuntimeModeFromRequest 从请求解析权威运行时模式。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode（L331-344）。
func ResolveRequestRuntimeModeFromRequest(req *schema.AgentRequest) ResolvedMode {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	_, _, canonical := ResolveAgentRequestMode(params["mode"], workMode)
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    baseModeWithoutPlan(canonical),
	}
}

// parseRequestParams 解析 request.Params 为可写 map（nil/非法时返回空 map）。
func parseRequestParams(req *schema.AgentRequest) map[string]any {
	if req == nil || len(req.Params) == 0 {
		return map[string]any{}
	}
	params := map[string]any{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

// writeRequestParams 把 params 序列化回 request.Params。
func writeRequestParams(req *schema.AgentRequest, params map[string]any) {
	if req == nil {
		return
	}
	updated, err := json.Marshal(params)
	if err != nil {
		return
	}
	req.Params = json.RawMessage(updated)
}

// ResolveAgentRequestModeFromRequest 纯读取请求的 mode（不修改 request）。
//
// Python: _handle_cancel / AgentManager 用 resolve_agent_request_mode 的只读形态。
func ResolveAgentRequestModeFromRequest(req *schema.AgentRequest) (mode, subMode string) {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	mode, subMode, _ = ResolveAgentRequestMode(params["mode"], workMode)
	return mode, subMode
}

// ResolveRequestProjectDir 解析用于构造 agent 的稳定工程身份。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_project_dir（L44-82）。
// includeLegacyFallbacks=false 时（agent/code 的 projectless 轮次）不回落到
// cwd/trusted_dirs，保持 cwd 动态以便 adapter 分配会话任务工作区。
func ResolveRequestProjectDir(req *schema.AgentRequest, includeLegacyFallbacks bool) string {
	params := parseRequestParams(req)
	if v, ok := params["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if req != nil && req.Metadata != nil {
		if v, ok := req.Metadata["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if !includeLegacyFallbacks {
		return ""
	}
	if v, ok := params["cwd"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if req != nil && req.Metadata != nil {
		if v, ok := req.Metadata["cwd"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if dirs, ok := params["trusted_dirs"].([]any); ok && len(dirs) > 0 {
		if v, ok := dirs[0].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ResolveCancelProjectDir 解析传输无关的取消请求所需工程缓存键。
//
// Python: jiuwenswarm/runtime/request.py _resolve_cancel_project_dir（L84-149）。
// 偏差：Go 尚无 project_id → project_dir 注册表（上游 project_store.get_project_dir_by_id），
// 该分支记 ⤵️，其余分支（直接解析 + 会话 metadata）已对齐。
func ResolveCancelProjectDir(req *schema.AgentRequest) string {
	if direct := ResolveRequestProjectDir(req, true); direct != "" {
		return direct
	}
	if req == nil || req.SessionID == nil {
		return ""
	}
	sid := strings.TrimSpace(*req.SessionID)
	if sid == "" {
		return ""
	}
	meta := session.GetSessionMetadata(sid)
	if v, ok := meta["project_dir"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// PrepareChatTurn 解析会话语义并选出一个 agent。
//
// Python: jiuwenswarm/runtime/request.py prepare_chat_turn（L361-577）。
// 偏差（⤵️）：sync_metadata 写入（Go 的 SessionMetadataUpdate 尚无 project_dir/
// work_mode/model/last_user_message_at 字段）、get_agent_for_request 原子准入、
// wait_for_session_prewarm 未移植；此处按 sync_metadata=false 的只读形态执行。
func PrepareChatTurn(
	ctx context.Context,
	mgr AgentManagerPort,
	req *schema.AgentRequest,
	channelID string,
) (mode, subMode string, agent ChatAgentPort, err error) {
	params := parseRequestParams(req)
	rawMode, _ := params["mode"].(string)
	explicitModeProvided := strings.TrimSpace(rawMode) != ""

	sid := ""
	if req != nil && req.SessionID != nil {
		sid = strings.TrimSpace(*req.SessionID)
	}

	// 1. 会话已锁定的 mode/work_mode 继承（内部轮次含 heartbeat 不带显式 mode）
	runtimeWorkMode := ""
	if sid != "" {
		meta := session.GetSessionMetadata(sid)
		if storedMode, ok := meta["mode"].(string); ok && strings.TrimSpace(storedMode) != "" {
			storedMode = strings.TrimSpace(storedMode)
			params[PreviousSessionModeKey] = storedMode
			if !explicitModeProvided {
				params["mode"] = storedMode
			}
		}
		if wm, ok := meta["work_mode"].(string); ok {
			if n := strings.ToLower(strings.TrimSpace(wm)); n == "code" || n == "work" {
				runtimeWorkMode = n
			}
		}
	}
	// 2. 请求自带 work_mode
	if runtimeWorkMode == "" {
		if wm, ok := params["work_mode"].(string); ok {
			if n := strings.ToLower(strings.TrimSpace(wm)); n == "code" || n == "work" {
				runtimeWorkMode = n
			}
		}
	}
	// 3. 渠道默认（Python: default_work_mode_for_channel —— tui→code，其余 work）
	if runtimeWorkMode == "" {
		if strings.EqualFold(strings.TrimSpace(channelID), "tui") {
			runtimeWorkMode = "code"
		} else {
			runtimeWorkMode = "work"
		}
		logger.Warn(logComponent).
			Str("channel_id", channelID).
			Str("session_id", sid).
			Str("work_mode", runtimeWorkMode).
			Msg("runtime request has no work_mode; defaulting by channel")
	}
	params["work_mode"] = runtimeWorkMode
	writeRequestParams(req, params)

	// 4. 解析 mode 并回写 canonical
	mode, subMode = ApplyResolvedModeToRequest(req, runtimeWorkMode)
	agentMode := mode
	if agentMode == "auto_harness" {
		agentMode = "agent"
	}

	// 5. 工程身份（agent/code 的 projectless 轮次不回落 cwd）
	projectDir := ResolveRequestProjectDir(req, agentMode != "agent" && agentMode != "code")

	// 6. 取 agent（⤵️ 上游为 get_agent_for_request 原子准入 + 预热等待）
	agent, err = mgr.GetChatAgent(ctx, channelID, agentMode, subMode, projectDir)
	if err != nil {
		return "", "", nil, err
	}
	return mode, subMode, agent, nil
}
