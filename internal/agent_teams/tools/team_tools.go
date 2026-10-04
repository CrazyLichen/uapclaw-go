package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BuildTeamTool 创建团队工具。
// Python: BuildTeamTool(TeamTool) (tools/team_tools.py L173)
type BuildTeamTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// CleanTeamTool 清理团队工具。
// Python: CleanTeamTool(TeamTool) (tools/team_tools.py L243)
type CleanTeamTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// SpawnMemberTool 创建团队成员工具。
// Python: SpawnMemberTool(TeamTool) (tools/team_tools.py L280)
type SpawnMemberTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
	// modelConfigAlloc 模型配置分配器
	modelConfigAlloc func(modelName string) *models.Allocation
}

// ShutdownMemberTool 关闭团队成员工具。
// Python: ShutdownMemberTool(TeamTool) (tools/team_tools.py L422)
type ShutdownMemberTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// ApprovePlanTool 审批计划工具。
// Python: ApprovePlanTool(TeamTool) (tools/team_tools.py L464)
type ApprovePlanTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// ApproveToolCallTool 审批工具调用工具。
// Python: ApproveToolCallTool(TeamTool) (tools/team_tools.py L514)
type ApproveToolCallTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// ListMembersTool 列出成员工具。
// Python: ListMembersTool(TeamTool) (tools/team_tools.py L568)
type ListMembersTool struct {
	TeamTool
	// team 团队后端实例
	team *TeamBackend
}

// TaskCreateTool 创建任务工具。
// Python: TaskCreateTool(TeamTool) (tools/team_tools.py L603)
type TaskCreateTool struct {
	TeamTool
	// taskManager 任务管理器
	taskManager *TeamTaskManager
}

// ViewTaskTool 查看任务工具（V2 统一）。
// Python: ViewTaskToolV2(TeamTool) (tools/team_tools.py L748)
type ViewTaskTool struct {
	TeamTool
	// taskManager 任务管理器
	taskManager *TeamTaskManager
}

// UpdateTaskTool 更新任务工具。
// Python: UpdateTaskTool(TeamTool) (tools/team_tools.py L837)
type UpdateTaskTool struct {
	TeamTool
	// agentTeam 团队后端实例
	agentTeam *TeamBackend
}

// SubmitPlanTool 提交计划工具。
// Python: SubmitPlanTool(TeamTool) (tools/team_tools.py L1034)
type SubmitPlanTool struct {
	TeamTool
	// taskManager 任务管理器
	taskManager *TeamTaskManager
}

// ClaimTaskTool 认领任务工具。
// Python: ClaimTaskTool(TeamTool) (tools/team_tools.py L1086)
type ClaimTaskTool struct {
	TeamTool
	// taskManager 任务管理器
	taskManager *TeamTaskManager
}

// MemberCompleteTaskTool 成员完成任务工具。
// Python: MemberCompleteTaskTool(TeamTool) (tools/team_tools.py L1155)
type MemberCompleteTaskTool struct {
	TeamTool
	// taskManager 任务管理器
	taskManager *TeamTaskManager
}

// SendMessageTool 发送消息工具。
// Python: SendMessageTool(TeamTool) (tools/team_tools.py L1246)
type SendMessageTool struct {
	TeamTool
	// messageManager 消息管理器
	messageManager *TeamMessageManager
	// team 团队后端实例
	team *TeamBackend
	// onTeammateCreated 队友创建回调
	onTeammateCreated func(ctx context.Context, memberName string) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 工具权限集合，对齐 Python LEADER_ONLY_TOOLS / MEMBER_ONLY_TOOLS / SHARED_TOOLS / HUMAN_AGENT_TOOLS。
// Python: tools/team_tools.py L94-155
const (
	// LeaderOnlyToolsStr 仅 Leader 可用的工具名（逗号分隔）
	LeaderOnlyToolsStr = "build_team,clean_team,spawn_member,shutdown_member,approve_plan,approve_tool,create_task,update_task,list_members"
	// MemberOnlyToolsStr 仅 Teammate 可用的工具名（逗号分隔）
	MemberOnlyToolsStr = "claim_task,submit_plan"
	// SharedToolsStr Leader 和 Teammate 共用的工具名（逗号分隔）
	// Python: SHARED_TOOLS = {"view_task", "send_message", "workspace_meta"}
	SharedToolsStr = "view_task,send_message,workspace_meta"
	// HumanAgentToolsStr Human-Agent 可用的工具名（逗号分隔）
	HumanAgentToolsStr = "view_task,member_complete_task,send_message"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ttLogComponent 工具包日志组件标识
var ttLogComponent = logger.ComponentTeam

// memberNamePattern 成员名正则，对齐 Python _MEMBER_NAME_PATTERN。
// Python: _MEMBER_NAME_PATTERN = re.compile(r"^[a-z][a-z0-9-]*$")
var memberNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// leaderOnlySet Leader 专用工具名集合。
var leaderOnlySet = commaStrToSet(LeaderOnlyToolsStr)

// memberOnlySet Teammate 专用工具名集合。
var memberOnlySet = commaStrToSet(MemberOnlyToolsStr)

// sharedSet 共用工具名集合。
var sharedSet = commaStrToSet(SharedToolsStr)

// humanAgentSet Human-Agent 工具名集合。
var humanAgentSet = commaStrToSet(HumanAgentToolsStr)

// leaderSet Leader 可用工具名集合 = Leader 专用 + 共用。
var leaderSet = mergeSets(leaderOnlySet, sharedSet)

// memberSet Teammate 可用工具名集合 = Teammate 专用 + 共用。
var memberSet = mergeSets(memberOnlySet, sharedSet)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateTeamTools 创建角色相关的团队工具实例列表。
// Python: create_team_tools() (tools/team_tools.py L1474)
//
// 参数：
//   - teamBackend: TeamBackend 门面实例
//   - role: 角色类型，"leader" / "teammate" / "human_agent"
//   - teammateMode: "build_mode" 或 "plan_mode"
//   - lifecycle: "temporary" 或 "persistent"
//   - language: "cn" 或 "en"
//   - onTeammateCreated: Teammate 创建回调（可选）
//   - modelConfigAlloc: 模型配置分配器（可选）
//   - excludeTools: 排除的工具名集合（可选）
func CreateTeamTools(
	teamBackend *TeamBackend,
	role string,
	teammateMode string,
	lifecycle string,
	language string,
	onTeammateCreated func(ctx context.Context, memberName string) error,
	modelConfigAlloc func(modelName string) *models.Allocation,
	excludeTools map[string]struct{},
) []tool.Tool {
	t := locales.MakeTranslator(atschema.Language(language))

	// 提取子管理器（teamBackend 可能为 nil，测试场景下）
	var taskMgr *TeamTaskManager
	var msgMgr *TeamMessageManager
	if teamBackend != nil {
		taskMgr = teamBackend.TaskManager()
		msgMgr = teamBackend.MessageManager()
	}

	// 构建所有工具实例（teamBackend 为 nil 时跳过依赖后端的工具）
	allTools := make(map[string]tool.Tool)
	if teamBackend != nil {
		allTools["build_team"] = newBuildTeamTool(teamBackend, t)
		allTools["clean_team"] = newCleanTeamTool(teamBackend, t)
		allTools["spawn_member"] = newSpawnMemberTool(teamBackend, t, modelConfigAlloc)
		allTools["shutdown_member"] = newShutdownMemberTool(teamBackend, t)
		allTools["approve_plan"] = newApprovePlanTool(teamBackend, t)
		allTools["approve_tool"] = newApproveToolCallTool(teamBackend, t)
		allTools["list_members"] = newListMembersTool(teamBackend, t)
		allTools["create_task"] = newTaskCreateTool(teamBackend, t)
		allTools["update_task"] = newUpdateTaskTool(teamBackend, t)
	}
	if taskMgr != nil {
		allTools["view_task"] = newViewTaskTool(taskMgr, t)
		allTools["claim_task"] = newClaimTaskTool(taskMgr, t)
		allTools["submit_plan"] = newSubmitPlanTool(taskMgr, t)
		allTools["member_complete_task"] = newMemberCompleteTaskTool(taskMgr, t)
	}
	if msgMgr != nil {
		allTools["send_message"] = newSendMessageTool(msgMgr, t, teamBackend, onTeammateCreated)
	}

	// 角色过滤
	var allowed map[string]struct{}
	switch role {
	case "human_agent":
		allowed = copySet(humanAgentSet)
	case "leader":
		allowed = copySet(leaderSet)
	default:
		allowed = copySet(memberSet)
	}

	// plan_mode 过滤：非 plan_mode 下移除审批/提交工具
	if teammateMode != "plan_mode" {
		delete(allowed, "approve_plan")
		delete(allowed, "approve_tool")
		delete(allowed, "submit_plan")
	}

	// persistent 生命周期下移除 clean_team
	if lifecycle == "persistent" {
		delete(allowed, "clean_team")
	}

	// 排除指定工具
	for name := range excludeTools {
		delete(allowed, name)
	}

	// 按权限集合过滤
	var result []tool.Tool
	for name, tl := range allTools {
		if _, ok := allowed[name]; ok {
			result = append(result, tl)
		}
	}

	// Python: for tool in tools: _wrap_invoke_with_logging(tool)
	// 为每个工具添加日志和 map_result 包装
	for i, tl := range result {
		result[i] = WrapInvokeWithLogging(tl)
	}

	return result
}

// QualifyTeamToolIDs 为工具 ID 添加 team.member 后缀，避免 inprocess 模式下的 ID 冲突。
// Python: qualify_team_tool_ids() (rails/team_tool_rail.py L174)
func QualifyTeamToolIDs(tools []tool.Tool, teamName string, memberName string) {
	teamKey := teamName
	if teamKey == "" {
		teamKey = "default"
	}
	memberKey := memberName
	if memberKey == "" {
		memberKey = "unknown"
	}
	for _, tl := range tools {
		card := tl.Card()
		if card == nil || card.ID == "" {
			continue
		}
		qualifiedID := fmt.Sprintf("%s.%s.%s", card.ID, teamKey, memberKey)
		if card.ID != qualifiedID {
			card.ID = qualifiedID
		}
	}
}

// MemberNameRegexp 返回成员名校验正则（用于 SpawnMemberTool 校验）。
func MemberNameRegexp() *regexp.Regexp {
	return memberNamePattern
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolSuccess 构造成功的工具返回（对齐 Python ToolOutput(success=True, data=...)）。
func toolSuccess(data map[string]any) (map[string]any, error) {
	return map[string]any{"success": true, "data": data}, nil
}

// toolError 构造业务失败的工具返回（对齐 Python ToolOutput(success=False, error="...")）。
// 注意：这不是 Go error，而是正常返回 map——BuildToolMessageContent 路径 1b 自动提取。
func toolError(msg string) (map[string]any, error) {
	return map[string]any{"success": false, "error": msg}, nil
}

// commaStrToSet 将逗号分隔字符串转为 map[string]struct{} 集合。
func commaStrToSet(s string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result[item] = struct{}{}
		}
	}
	return result
}

// mergeSets 合并两个集合。
func mergeSets(a, b map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		result[k] = struct{}{}
	}
	for k := range b {
		result[k] = struct{}{}
	}
	return result
}

// copySet 复制集合。
func copySet(s map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(s))
	for k := range s {
		result[k] = struct{}{}
	}
	return result
}

// newBuildTeamTool 创建 BuildTeamTool 实例。
func newBuildTeamTool(team *TeamBackend, t locales.Translator) *BuildTeamTool {
	return &BuildTeamTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.build_team", "build_team", t("build_team"),
			[]*schema.Param{
				schema.NewStringParam("display_name", t("build_team", "display_name"), true),
				schema.NewStringParam("team_desc", t("build_team", "team_desc"), true),
				schema.NewStringParam("leader_display_name", t("build_team", "leader_display_name"), true),
				schema.NewStringParam("leader_desc", t("build_team", "leader_desc"), true),
				schema.NewBooleanParam("enable_hitt", t("build_team", "enable_hitt"), false),
			}, nil)),
		team: team,
	}
}

// newCleanTeamTool 创建 CleanTeamTool 实例。
func newCleanTeamTool(team *TeamBackend, t locales.Translator) *CleanTeamTool {
	return &CleanTeamTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.clean_team", "clean_team", t("clean_team"),
			nil, nil)),
		team: team,
	}
}

// newSpawnMemberTool 创建 SpawnMemberTool 实例。
func newSpawnMemberTool(team *TeamBackend, t locales.Translator, alloc func(modelName string) *models.Allocation) *SpawnMemberTool {
	roleTypeParam := schema.NewStringParam("role_type", t("spawn_member", "role_type"), false, "teammate")
	roleTypeParam.Enum = []any{"teammate", "human_agent"}
	return &SpawnMemberTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.spawn_member", "spawn_member", t("spawn_member"),
			[]*schema.Param{
				schema.NewStringParam("member_name", t("spawn_member", "member_name"), true),
				schema.NewStringParam("display_name", t("spawn_member", "display_name"), true),
				schema.NewStringParam("desc", t("spawn_member", "desc"), true),
				roleTypeParam,
				schema.NewStringParam("prompt", t("spawn_member", "prompt"), false),
				schema.NewStringParam("model_name", t("spawn_member", "model_name"), false),
			}, nil)),
		team:             team,
		modelConfigAlloc: alloc,
	}
}

// newShutdownMemberTool 创建 ShutdownMemberTool 实例。
func newShutdownMemberTool(team *TeamBackend, t locales.Translator) *ShutdownMemberTool {
	return &ShutdownMemberTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.shutdown_member", "shutdown_member", t("shutdown_member"),
			[]*schema.Param{
				schema.NewStringParam("member_name", t("shutdown_member", "member_name"), true),
				schema.NewBooleanParam("force", t("shutdown_member", "force"), false),
			}, nil)),
		team: team,
	}
}

// newApprovePlanTool 创建 ApprovePlanTool 实例。
func newApprovePlanTool(team *TeamBackend, t locales.Translator) *ApprovePlanTool {
	return &ApprovePlanTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.approve_plan", "approve_plan", t("approve_plan"),
			[]*schema.Param{
				schema.NewStringParam("plan_id", t("approve_plan", "plan_id"), true),
				schema.NewBooleanParam("approved", t("approve_plan", "approved"), true),
				schema.NewStringParam("feedback", t("approve_plan", "feedback"), false),
			}, nil)),
		team: team,
	}
}

// newApproveToolCallTool 创建 ApproveToolCallTool 实例。
func newApproveToolCallTool(team *TeamBackend, t locales.Translator) *ApproveToolCallTool {
	return &ApproveToolCallTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.approve_tool", "approve_tool", t("approve_tool"),
			[]*schema.Param{
				schema.NewStringParam("member_name", t("approve_tool", "member_name"), true),
				schema.NewStringParam("tool_call_id", t("approve_tool", "tool_call_id"), true),
				schema.NewBooleanParam("approved", t("approve_tool", "approved"), true),
				schema.NewStringParam("feedback", t("approve_tool", "feedback"), false),
				schema.NewBooleanParam("auto_confirm", t("approve_tool", "auto_confirm"), false),
			}, nil)),
		team: team,
	}
}

// newListMembersTool 创建 ListMembersTool 实例。
func newListMembersTool(team *TeamBackend, t locales.Translator) *ListMembersTool {
	return &ListMembersTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.list_members", "list_members", t("list_members"),
			nil, nil)),
		team: team,
	}
}

// newTaskCreateTool 创建 TaskCreateTool 实例。
func newTaskCreateTool(agentTeam *TeamBackend, t locales.Translator) *TaskCreateTool {
	taskSchema := &schema.Param{
		Name: "task", Type: schema.ParamTypeObject,
		Properties: []*schema.Param{
			schema.NewStringParam("task_id", t("create_task", "task.task_id"), false),
			schema.NewStringParam("title", t("create_task", "task.title"), true),
			schema.NewStringParam("content", t("create_task", "task.content"), true),
			schema.NewArrayParam("depends_on", t("create_task", "task.depends_on"), false,
				schema.NewStringParam("item", "", false)),
			schema.NewArrayParam("depended_by", t("create_task", "task.depended_by"), false,
				schema.NewStringParam("item", "", false)),
		},
		AdditionalProperties: true,
	}
	return &TaskCreateTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.create_task", "create_task", t("create_task"),
			[]*schema.Param{
				schema.NewArrayParam("tasks", t("create_task", "tasks"), true, taskSchema),
			}, nil)),
		taskManager: agentTeam.TaskManager(),
	}
}

// newUpdateTaskTool 创建 UpdateTaskTool 实例。
func newUpdateTaskTool(agentTeam *TeamBackend, t locales.Translator) *UpdateTaskTool {
	statusParam := schema.NewStringParam("status", t("update_task", "status"), false)
	statusParam.Enum = []any{"cancelled"}
	return &UpdateTaskTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.update_task", "update_task", t("update_task"),
			[]*schema.Param{
				schema.NewStringParam("task_id", t("update_task", "task_id"), true),
				statusParam,
				schema.NewStringParam("title", t("update_task", "title"), false),
				schema.NewStringParam("content", t("update_task", "content"), false),
				schema.NewStringParam("assignee", t("update_task", "assignee"), false),
				schema.NewArrayParam("add_blocked_by", t("update_task", "add_blocked_by"), false,
					schema.NewStringParam("item", "", false)),
			}, nil)),
		agentTeam: agentTeam,
	}
}

// newViewTaskTool 创建 ViewTaskTool 实例。
func newViewTaskTool(taskManager *TeamTaskManager, t locales.Translator) *ViewTaskTool {
	actionParam := schema.NewStringParam("action", t("view_task", "action"), false, "list")
	actionParam.Enum = []any{"get", "list", "claimable"}
	return &ViewTaskTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.view_task", "view_task", t("view_task"),
			[]*schema.Param{
				actionParam,
				schema.NewStringParam("task_id", t("view_task", "task_id"), false),
				schema.NewStringParam("status", t("view_task", "status"), false),
			}, nil)),
		taskManager: taskManager,
	}
}

// newClaimTaskTool 创建 ClaimTaskTool 实例。
func newClaimTaskTool(taskManager *TeamTaskManager, t locales.Translator) *ClaimTaskTool {
	statusParam := schema.NewStringParam("status", t("claim_task", "status"), true)
	statusParam.Enum = []any{"claimed", "completed"}
	return &ClaimTaskTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.claim_task", "claim_task", t("claim_task"),
			[]*schema.Param{
				schema.NewStringParam("task_id", t("claim_task", "task_id"), true),
				statusParam,
			}, nil)),
		taskManager: taskManager,
	}
}

// newSubmitPlanTool 创建 SubmitPlanTool 实例。
func newSubmitPlanTool(taskManager *TeamTaskManager, t locales.Translator) *SubmitPlanTool {
	return &SubmitPlanTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.submit_plan", "submit_plan", t("submit_plan"),
			[]*schema.Param{
				schema.NewStringParam("task_id", t("submit_plan", "task_id"), true),
				schema.NewStringParam("plan_id", t("submit_plan", "plan_id"), false),
				schema.NewStringParam("plan_path", t("submit_plan", "plan_path"), true),
			}, nil)),
		taskManager: taskManager,
	}
}

// newMemberCompleteTaskTool 创建 MemberCompleteTaskTool 实例。
func newMemberCompleteTaskTool(taskManager *TeamTaskManager, t locales.Translator) *MemberCompleteTaskTool {
	return &MemberCompleteTaskTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.member_complete_task", "member_complete_task", t("member_complete_task"),
			[]*schema.Param{
				schema.NewStringParam("task_id", t("member_complete_task", "task_id"), true),
				schema.NewStringParam("note", t("member_complete_task", "note"), false),
			}, nil)),
		taskManager: taskManager,
	}
}

// newSendMessageTool 创建 SendMessageTool 实例。
func newSendMessageTool(msgMgr *TeamMessageManager, t locales.Translator, team *TeamBackend, onCreated func(ctx context.Context, memberName string) error) *SendMessageTool {
	toParam := &schema.Param{
		Name: "to", Type: schema.ParamTypeString, Required: true,
		Description: t("send_message", "to"),
		AnyOf: []*schema.Param{
			schema.NewStringParam("to_str", "", false),
			schema.NewArrayParam("to_arr", "", false,
				schema.NewStringParam("item", "", false)),
		},
	}
	return &SendMessageTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.send_message", "send_message", t("send_message"),
			[]*schema.Param{
				toParam,
				schema.NewStringParam("content", t("send_message", "content"), true),
				schema.NewStringParam("summary", t("send_message", "summary"), false),
			}, nil)),
		messageManager:    msgMgr,
		team:              team,
		onTeammateCreated: onCreated,
	}
}

// Invoke 执行 BuildTeamTool，对齐 Python BuildTeamTool.invoke。
func (t *BuildTeamTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	displayName, _ := inputs["display_name"].(string)
	leaderDisplayName, _ := inputs["leader_display_name"].(string)
	leaderDesc, _ := inputs["leader_desc"].(string)
	teamDesc, _ := inputs["team_desc"].(string)
	var enableHITT *bool
	if v, ok := inputs["enable_hitt"]; ok {
		if b, ok := v.(bool); ok {
			enableHITT = &b
		}
	}
	err := t.team.BuildTeam(ctx, displayName, teamDesc, leaderDisplayName, leaderDesc, enableHITT)
	if err != nil {
		return toolError(err.Error())
	}
	return toolSuccess(map[string]any{
		"team_name":           t.team.TeamName(),
		"display_name":        displayName,
		"leader_member_name":  t.team.MemberName(),
		"leader_display_name": leaderDisplayName,
		"enable_hitt":         t.team.HITTEnabled(),
	})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *BuildTeamTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 CleanTeamTool，对齐 Python CleanTeamTool.invoke。
func (t *CleanTeamTool) Invoke(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	teamName := t.team.TeamName()
	success, err := t.team.CleanTeam(ctx)
	if err != nil {
		logger.Error(ttLogComponent).Err(err).Msg("clean_team 失败")
		return toolError(fmt.Sprintf("Internal error: %s", err))
	}
	if !success {
		return toolError("Active members remain. Use shutdown_member to close all members first.")
	}
	return toolSuccess(map[string]any{"team_name": teamName})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *CleanTeamTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 SpawnMemberTool，对齐 Python SpawnMemberTool.invoke。
func (t *SpawnMemberTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	displayName, _ := inputs["display_name"].(string)
	desc, _ := inputs["desc"].(string)
	roleType := "teammate"
	if v, ok := inputs["role_type"].(string); ok && v != "" {
		roleType = strings.ToLower(v)
	}

	// 校验 member_name 格式（对齐 Python _MEMBER_NAME_PATTERN）
	if memberName == "" || !memberNamePattern.MatchString(memberName) {
		return toolError(fmt.Sprintf(
			"Invalid member_name %q: must start with a lowercase ASCII letter (a-z), "+
				"followed by lowercase letters, digits (0-9) or hyphen (-); no uppercase, "+
				"underscore, whitespace, or non-ASCII characters", memberName))
	}

	if roleType != "teammate" && roleType != "human_agent" {
		return toolError(fmt.Sprintf("Invalid role_type %q; expected 'teammate' or 'human_agent'", roleType))
	}

	// human_agent 路径
	if roleType == "human_agent" {
		if !t.team.HITTEnabled() {
			return toolError("Cannot spawn human agent: HITT capability is disabled " +
				"(enable_hitt=False on TeamAgentSpec or build_team). " +
				"Either enable HITT in the team spec or use role_type='teammate'.")
		}
		if _, ok := inputs["model_name"]; ok {
			return toolError("role_type='human_agent' does not accept 'model_name'; " +
				"human members use the framework template — remove this field")
		}
		if _, ok := inputs["prompt"]; ok {
			return toolError("role_type='human_agent' does not accept 'prompt'; " +
				"human members use the framework template — remove this field")
		}
		result := t.team.SpawnHumanAgent(ctx, memberName, displayName, desc, "")
		if !result.OK {
			return toolError(result.Reason)
		}
		return toolSuccess(map[string]any{
			"member_name": memberName, "display_name": displayName, "role_type": "human_agent",
		})
	}

	// teammate 路径
	mode := atschema.MemberMode(t.team.TeammateMode())
	modelName, _ := inputs["model_name"].(string)
	var allocation *models.Allocation
	if t.modelConfigAlloc != nil {
		allocation = t.modelConfigAlloc(modelName)
	}
	cardID := fmt.Sprintf("%s_%s", t.team.TeamName(), memberName)
	agentCard := &agentschema.AgentCard{}
	agentCard.ID = cardID
	agentCard.Name = displayName
	agentCard.Description = desc
	prompt, _ := inputs["prompt"].(string) // 安全断言，prompt 为可选参数，对齐 Python inputs.get("prompt")
	result := t.team.SpawnMember(ctx, memberName, displayName, agentCard, string(mode), desc,
		prompt, modelName,
		WithAllocation(allocation))
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{
		"member_name": memberName, "display_name": displayName, "role_type": "teammate",
	})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *SpawnMemberTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ShutdownMemberTool，对齐 Python ShutdownMemberTool.invoke。
func (t *ShutdownMemberTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	force := false
	if v, ok := inputs["force"].(bool); ok {
		force = v
	}
	result := t.team.ShutdownMember(ctx, memberName, WithForce(force))
	if !result.OK {
		return toolError(result.Reason)
	}
	return toolSuccess(map[string]any{"member_name": memberName})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ShutdownMemberTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ApprovePlanTool，对齐 Python ApprovePlanTool.invoke。
func (t *ApprovePlanTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	planID, _ := inputs["plan_id"].(string)
	approved := false
	if v, ok := inputs["approved"].(bool); ok {
		approved = v
	}
	feedback, _ := inputs["feedback"].(string)
	result := t.team.ApprovePlan(ctx, planID, WithApproved(approved), WithFeedback(feedback))
	if !result.OK {
		return toolError("Failed to approve/reject plan")
	}
	return toolSuccess(map[string]any{"plan_id": planID, "approved": approved})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ApprovePlanTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ApproveToolCallTool，对齐 Python ApproveToolCallTool.invoke。
func (t *ApproveToolCallTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	memberName, _ := inputs["member_name"].(string)
	toolCallID, _ := inputs["tool_call_id"].(string)
	approved := false
	if v, ok := inputs["approved"].(bool); ok {
		approved = v
	}
	feedback, _ := inputs["feedback"].(string)
	autoConfirm := false
	if v, ok := inputs["auto_confirm"].(bool); ok {
		autoConfirm = v
	}
	result := t.team.ApproveTool(ctx, memberName, toolCallID, approved, feedback, autoConfirm)
	if !result.OK {
		return toolError("Failed to approve/reject tool call")
	}
	return toolSuccess(map[string]any{"member_name": memberName, "tool_call_id": toolCallID, "approved": approved})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ApproveToolCallTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ListMembersTool，对齐 Python ListMembersTool.invoke。
func (t *ListMembersTool) Invoke(ctx context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	members, err := t.team.ListMembers(ctx)
	if err != nil {
		return toolError(fmt.Sprintf("Failed to list members: %s", err))
	}
	memberList := make([]map[string]any, len(members))
	for i, m := range members {
		// 对齐 Python: member.model_dump() 返回完整字段
		memberList[i] = map[string]any{
			"member_name":      m.MemberName,
			"team_name":        m.TeamName,
			"display_name":     m.DisplayName,
			"desc":             m.Desc,
			"agent_card":       m.AgentCard,
			"status":           m.Status,
			"execution_status": m.ExecutionStatus,
			"mode":             m.Mode,
			"role":             m.Role,
			"prompt":           m.Prompt,
			"model_ref_json":   m.ModelRefJSON,
			"updated_at":       m.UpdatedAt,
		}
	}
	return toolSuccess(map[string]any{"members": memberList, "count": len(members)})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ListMembersTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 TaskCreateTool，对齐 Python TaskCreateTool.invoke。
func (t *TaskCreateTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	tasksRaw, ok := inputs["tasks"].([]any)
	if !ok || len(tasksRaw) == 0 {
		return toolError("'tasks' is required")
	}

	// 单任务快速路径
	if len(tasksRaw) == 1 {
		spec, _ := tasksRaw[0].(map[string]any)
		if spec == nil {
			return toolError("Invalid task spec")
		}
		title, _ := spec["title"].(string)
		content, _ := spec["content"].(string)
		if title == "" || content == "" {
			return toolError(fmt.Sprintf("Task %q missing required title/content", specLabel(spec)))
		}
		result, err := t.createOne(ctx, spec)
		if err != nil {
			return toolError(err.Error())
		}
		if !result.Ok() {
			return toolError(result.Reason)
		}
		return toolSuccess(taskBrief(result.Task))
	}

	// 批量路径
	var created []*database.TeamTaskBase
	var failures []map[string]any
	for _, raw := range tasksRaw {
		spec, _ := raw.(map[string]any)
		if spec == nil {
			continue
		}
		title, _ := spec["title"].(string)
		content, _ := spec["content"].(string)
		if title == "" || content == "" {
			failures = append(failures, map[string]any{
				"spec":   specLabel(spec),
				"reason": "missing required title/content",
			})
			continue
		}
		result, err := t.createOne(ctx, spec)
		if err != nil || !result.Ok() {
			reason := "unknown error"
			if err != nil {
				reason = err.Error()
			} else {
				reason = result.Reason
			}
			failures = append(failures, map[string]any{"spec": specLabel(spec), "reason": reason})
			continue
		}
		created = append(created, result.Task)
	}

	if len(created) == 0 && len(failures) > 0 {
		var msgs []string
		for _, f := range failures {
			msgs = append(msgs, fmt.Sprintf("%s: %s", f["spec"], f["reason"]))
		}
		return toolError(fmt.Sprintf("All %d task creations failed: %s", len(failures), strings.Join(msgs, "; ")))
	}

	briefs := make([]map[string]any, len(created))
	for i, task := range created {
		briefs[i] = taskBrief(task)
	}
	return toolSuccess(map[string]any{
		"tasks":    briefs,
		"count":    len(created),
		"skipped":  len(failures),
		"failures": failures,
	})
}

// createOne 创建单个任务，对齐 Python TaskCreateTool._create_one。
func (t *TaskCreateTool) createOne(ctx context.Context, spec map[string]any) (*TaskCreateResult, error) {
	title, _ := spec["title"].(string)
	content, _ := spec["content"].(string)
	taskID, _ := spec["task_id"].(string)

	// depended_by 路径（对齐 Python: add_with_priority）
	if dependedBy := extractStringSlice(spec, "depended_by"); len(dependedBy) > 0 {
		dependsOn := extractStringSlice(spec, "depends_on")
		task, err := t.taskManager.AddWithPriority(ctx, title, content,
			WithPriorityTaskID(taskID), WithPriorityDependencies(dependsOn), WithPriorityDependentTaskIDs(dependedBy))
		if err != nil {
			return nil, err
		}
		return &TaskCreateResult{Task: task}, nil
	}

	// 普通路径
	dependsOn := extractStringSlice(spec, "depends_on")
	task, err := t.taskManager.Add(ctx, title, content,
		WithTaskID(taskID), WithDependencies(dependsOn))
	if err != nil {
		return nil, err
	}
	return &TaskCreateResult{Task: task}, nil
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *TaskCreateTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ViewTaskTool，对齐 Python ViewTaskToolV2.invoke。
func (t *ViewTaskTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	action, _ := inputs["action"].(string)
	if action == "" {
		action = "list"
	}

	if action == "get" {
		taskID, _ := inputs["task_id"].(string)
		if taskID == "" {
			return toolError("task_id required for get action")
		}
		detail, err := t.taskManager.GetTaskDetail(ctx, taskID)
		if err != nil || detail == nil || detail.Task == nil {
			return toolError("Task not found")
		}
		blockedBy := make([]string, len(detail.BlockedBy))
		for i, b := range detail.BlockedBy {
			blockedBy[i] = b.TaskID
		}
		blocks := make([]string, len(detail.Blocks))
		for i, b := range detail.Blocks {
			blocks[i] = b.TaskID
		}
		// M-26: 对齐 Python — assignee 为 nil 时显示 "<unassigned>"
		assignee := "<unassigned>"
		if detail.Task.Assignee != nil && *detail.Task.Assignee != "" {
			assignee = *detail.Task.Assignee
		}
		data := map[string]any{
			"task_id":    detail.Task.TaskID,
			"title":      detail.Task.Title,
			"content":    detail.Task.Content,
			"status":     detail.Task.Status,
			"assignee":   assignee,
			"blocked_by": blockedBy,
			"blocks":     blocks,
		}
		return toolSuccess(data)
	}

	// 列表 / 可认领
	// 对齐 Python: claimable 传 status=PENDING，list 传 inputs["status"]
	var summaries []*TaskSummary
	var err error
	if action == "claimable" {
		summaries, err = t.taskManager.ListTasksWithDeps(ctx, "pending")
	} else {
		statusVal, _ := inputs["status"].(string)
		summaries, err = t.taskManager.ListTasksWithDeps(ctx, statusVal)
	}
	if err != nil {
		return toolError(fmt.Sprintf("Failed to list tasks: %s", err))
	}
	taskList := make([]map[string]any, len(summaries))
	for i, s := range summaries {
		blockedBy := make([]string, len(s.BlockedBy))
		copy(blockedBy, s.BlockedBy)
		// M-26: 对齐 Python — assignee 为空时显示 "<unassigned>"
		assignee := "<unassigned>"
		if s.Assignee != "" {
			assignee = s.Assignee
		}
		taskList[i] = map[string]any{
			"task_id":    s.TaskID,
			"title":      s.Title,
			"status":     s.Status,
			"assignee":   assignee,
			"blocked_by": blockedBy,
		}
	}
	return toolSuccess(map[string]any{"tasks": taskList})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ViewTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 UpdateTaskTool，对齐 Python UpdateTaskTool.invoke。
func (t *UpdateTaskTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	taskID, _ := inputs["task_id"].(string)
	if taskID == "" {
		return toolError("'task_id' is required")
	}
	status, _ := inputs["status"].(string)
	title, _ := inputs["title"].(string)
	content, _ := inputs["content"].(string)
	assignee, _ := inputs["assignee"].(string)
	addBlockedBy := extractStringSlice(inputs, "add_blocked_by")

	// 批量取消：task_id="*" + status="cancelled"
	if taskID == "*" && status == "cancelled" {
		// 取消所有 claimed 的非 human-agent 成员
		if err := t.cancelClaimedMembers(ctx); err != nil {
			return toolError(fmt.Sprintf("Failed to cancel claimed members: %s", err))
		}
		skip := t.agentTeam.HumanAgentNames()
		count, err := t.agentTeam.CancelAllTasks(ctx, skip)
		if err != nil {
			return toolError(fmt.Sprintf("Failed to cancel all tasks: %s", err))
		}
		return toolSuccess(map[string]any{"cancelled_count": count})
	}

	task, err := t.agentTeam.TaskManager().Get(ctx, taskID)
	if err != nil || task == nil {
		return toolError("Task not found")
	}

	// 取消单个任务
	if status == "cancelled" {
		if isHumanAgentLocked(t.agentTeam, task) {
			return toolError(fmt.Sprintf("Task '%s' is held by a human-agent member and cannot be cancelled by the leader", taskID))
		}
		if err := cancelMemberIfClaimed(ctx, t.agentTeam, taskID); err != nil {
			return toolError(fmt.Sprintf("Failed to cancel member for task %s: %s", taskID, err))
		}
		result := t.agentTeam.CancelTask(ctx, taskID)
		if !result.OK {
			return toolError("Failed to cancel task")
		}
		return toolSuccess(map[string]any{"task_id": taskID, "status": "cancelled"})
	}

	// 收集字段更新
	var updated []string

	// 内容更新（title 和/或 content）
	if title != "" || content != "" {
		if err := cancelMemberIfClaimed(ctx, t.agentTeam, taskID); err != nil {
			logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("cancelMemberIfClaimed 失败")
		}
		if err := t.agentTeam.TaskManager().UpdateTask(ctx, taskID, title, content); err != nil {
			return toolError(err.Error())
		}
		if title != "" {
			updated = append(updated, "title")
		}
		if content != "" {
			updated = append(updated, "content")
		}
	}

	// 分配任务
	if assignee != "" {
		assigneePtr := task.Assignee
		if assigneePtr != nil && *assigneePtr != assignee {
			if isHumanAgentLocked(t.agentTeam, task) {
				return toolError(fmt.Sprintf("Task '%s' is held by a human-agent member and cannot be reassigned", taskID))
			}
			if result := t.agentTeam.CancelMember(ctx, *assigneePtr); !result.OK {
				logger.Warn(logComponent).Str("member", *assigneePtr).Str("reason", result.Reason).Msg("CancelMember 失败")
			}
			resetResult, err := t.agentTeam.TaskManager().Reset(ctx, taskID)
			if err != nil {
				logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("Reset 失败")
				return toolError(fmt.Sprintf("Failed to reset task before reassigning from %s to %s: %s", *assigneePtr, assignee, err))
			}
			if !resetResult.OK {
				return toolError(fmt.Sprintf("Failed to reset task before reassigning from %s to %s: %s", *assigneePtr, assignee, resetResult.Reason))
			}
		}
		assignResult, err := t.agentTeam.TaskManager().Assign(ctx, taskID, assignee)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("task_id", taskID).Str("assignee", assignee).Msg("Assign 失败")
			return toolError(fmt.Sprintf("Failed to assign task: %s", err))
		}
		if !assignResult.OK {
			return toolError(assignResult.Reason)
		}
		updated = append(updated, "assignee")
	}

	// 添加依赖
	if len(addBlockedBy) > 0 {
		depsResult, depsErr := t.agentTeam.TaskManager().AddDependencies(ctx, taskID, addBlockedBy)
		if depsErr != nil {
			logger.Warn(logComponent).Err(depsErr).Str("task_id", taskID).Strs("add_blocked_by", addBlockedBy).Msg("AddDependencies 失败")
			return toolError(fmt.Sprintf("Failed to add dependencies: %s", depsErr))
		}
		if !depsResult.OK {
			return toolError(depsResult.Reason)
		}
		updated = append(updated, "blocked_by")
	}

	if len(updated) == 0 {
		return toolError("No update specified — provide status, title, content, assignee, or add_blocked_by")
	}
	return toolSuccess(map[string]any{"task_id": taskID, "status": "updated", "updated_fields": updated})
}

// isHumanAgentLocked 检查任务是否被 human-agent 持有（对齐 Python _is_human_agent_locked）。
func isHumanAgentLocked(team *TeamBackend, task *database.TeamTaskBase) bool {
	return task.Assignee != nil && team.IsHumanAgent(*task.Assignee) && task.Status == "claimed"
}

// cancelMemberIfClaimed 如果任务处于 claimed 状态则取消认领者（对齐 Python _cancel_member_if_claimed）。
func cancelMemberIfClaimed(ctx context.Context, team *TeamBackend, taskID string) error {
	task, err := team.TaskManager().Get(ctx, taskID)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("task_id", taskID).Msg("cancelMemberIfClaimed: Get 失败")
		return err
	}
	if task == nil || task.Status != "claimed" || task.Assignee == nil {
		return nil
	}
	if !team.IsHumanAgent(*task.Assignee) {
		if result := team.CancelMember(ctx, *task.Assignee); !result.OK {
			return fmt.Errorf("CancelMember 失败: %s", result.Reason)
		}
	}
	return nil
}

// cancelClaimedMembers 取消所有 claimed 状态的非 human-agent 成员（对齐 Python _cancel_claimed_members）。
func (t *UpdateTaskTool) cancelClaimedMembers(ctx context.Context) error {
	claimedTasks, err := t.agentTeam.TaskManager().ListTasks(ctx, "claimed")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("cancelClaimedMembers: ListTasks 失败")
		return err
	}
	cancelled := make(map[string]struct{})
	var firstErr error
	for _, task := range claimedTasks {
		if task.Assignee == nil {
			continue
		}
		assignee := *task.Assignee
		if _, ok := cancelled[assignee]; ok {
			continue
		}
		if t.agentTeam.IsHumanAgent(assignee) {
			continue
		}
		if result := t.agentTeam.CancelMember(ctx, assignee); !result.OK {
			logger.Warn(logComponent).Str("member", assignee).Str("reason", result.Reason).Msg("CancelMember 失败")
			if firstErr == nil {
				firstErr = fmt.Errorf("CancelMember(%s) 失败: %s", assignee, result.Reason)
			}
		}
		cancelled[assignee] = struct{}{}
	}
	return firstErr
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *UpdateTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 SubmitPlanTool，对齐 Python SubmitPlanTool.invoke。
func (t *SubmitPlanTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	taskID, _ := inputs["task_id"].(string)
	planID, _ := inputs["plan_id"].(string)
	planPath, _ := inputs["plan_path"].(string)

	result, err := t.taskManager.SubmitPlan(ctx, taskID, planPath, planID)
	if err != nil {
		return toolError(fmt.Sprintf("Failed to submit plan: %s", err))
	}
	// 将 PlanRecord 转为 map
	resultMap := map[string]any{
		"plan_id":        result.PlanID,
		"task_id":        result.TaskID,
		"member_name":    result.MemberName,
		"status":         result.Status,
		"team_plan_id":   result.TeamPlanID,
		"member_plan_md": result.MemberPlanMD,
		// 对齐 Python: "Member plan submitted. Wait for leader approval before execution."
		"message": "Member plan submitted. Wait for leader approval before execution.",
	}
	return toolSuccess(resultMap)
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *SubmitPlanTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 ClaimTaskTool，对齐 Python ClaimTaskTool.invoke。
func (t *ClaimTaskTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	taskID, _ := inputs["task_id"].(string)
	status, _ := inputs["status"].(string)

	task, err := t.taskManager.Get(ctx, taskID)
	if err != nil || task == nil {
		return toolError("Task not found")
	}

	var statusChange map[string]any
	switch status {
	case "claimed":
		result, err := t.taskManager.Claim(ctx, taskID)
		if err != nil {
			logger.Warn(ttLogComponent).Err(err).Str("task_id", taskID).Msg("Claim 失败")
			return toolError(fmt.Sprintf("Failed to claim task: %s", err))
		}
		if !result.OK {
			return toolError(result.Reason)
		}
		statusChange = map[string]any{"from": task.Status, "to": "claimed"}
	case "completed":
		result, err := t.taskManager.Complete(ctx, taskID)
		if err != nil {
			logger.Warn(ttLogComponent).Err(err).Str("task_id", taskID).Msg("Complete 失败")
			return toolError(fmt.Sprintf("Failed to complete task: %s", err))
		}
		if !result.OK {
			return toolError(result.Reason)
		}
		statusChange = map[string]any{"from": task.Status, "to": "completed"}
	default:
		return toolError(fmt.Sprintf("Invalid status: %s", status))
	}
	return toolSuccess(map[string]any{
		"task_id":        taskID,
		"updated_fields": []string{"status"},
		"status_change":  statusChange,
	})
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *ClaimTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 MemberCompleteTaskTool，对齐 Python MemberCompleteTaskTool.invoke。
func (t *MemberCompleteTaskTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	taskID := strings.TrimSpace(fmt.Sprintf("%v", inputs["task_id"]))
	if taskID == "" {
		return toolError("'task_id' is required")
	}

	task, err := t.taskManager.Get(ctx, taskID)
	if err != nil {
		logger.Error(ttLogComponent).Err(err).Str("task_id", taskID).Msg("member_complete_task: get 失败")
		return toolError(fmt.Sprintf("Internal error: %s", err))
	}
	if task == nil {
		return toolError(fmt.Sprintf("Task '%s' not found", taskID))
	}

	// 校验调用者是任务的认领人（对齐 Python: task.assignee != self.task_manager.member_name）
	caller := t.taskManager.MemberName()
	taskAssignee := ""
	if task.Assignee != nil {
		taskAssignee = *task.Assignee
	}
	if taskAssignee != caller {
		return toolError(fmt.Sprintf("Task '%s' is assigned to '%s', not '%s'; you can only complete tasks assigned to yourself",
			taskID, taskAssignee, caller))
	}

	result, err := t.taskManager.Complete(ctx, taskID)
	if err != nil {
		logger.Warn(ttLogComponent).Err(err).Str("task_id", taskID).Msg("Complete 失败")
		return toolError(fmt.Sprintf("Failed to complete task: %s", err))
	}
	if !result.OK {
		return toolError(result.Reason)
	}

	note := ""
	if v, ok := inputs["note"].(string); ok {
		note = strings.TrimSpace(v)
	}
	data := map[string]any{"task_id": taskID, "status": "completed"}
	if note != "" {
		data["note"] = note
	}
	return toolSuccess(data)
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *MemberCompleteTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// Invoke 执行 SendMessageTool，对齐 Python SendMessageTool.invoke。
func (t *SendMessageTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	toRaw := inputs["to"]
	content, _ := inputs["content"].(string)
	summary, _ := inputs["summary"].(string)
	content = strings.TrimSpace(content)

	if content == "" {
		return toolError("'content' is required")
	}

	// 根据 to 的类型分派
	switch v := toRaw.(type) {
	case []any:
		return t.multicast(ctx, v, content, summary)
	case string:
		to := strings.TrimSpace(v)
		if to == "" {
			return toolError("'to' is required")
		}
		if to == "*" {
			return t.broadcast(ctx, content, summary)
		}
		return t.send(ctx, to, content, summary)
	default:
		return toolError("'to' must be a string or an array of strings")
	}
}

// broadcast 广播消息（对齐 Python SendMessageTool._broadcast）。
func (t *SendMessageTool) broadcast(ctx context.Context, content, summary string) (map[string]any, error) {
	t.autoStartMembers(ctx)
	msgID, err := t.messageManager.BroadcastMessage(ctx, content, "")
	if err != nil || msgID == "" {
		return toolError("Failed to broadcast message")
	}
	data := map[string]any{"type": "broadcast", "from": t.messageManager.MemberName()}
	if summary != "" {
		data["summary"] = summary
	}
	return toolSuccess(data)
}

// send 点对点发送（对齐 Python SendMessageTool._send）。
func (t *SendMessageTool) send(ctx context.Context, to, content, summary string) (map[string]any, error) {
	// "user" 是伪成员，跳过 roster 校验
	if t.team != nil && to != "user" {
		member, err := t.team.GetMember(ctx, to)
		if err != nil || member == nil {
			return toolError(fmt.Sprintf("Member '%s' not found", to))
		}
	}
	t.autoStartMembers(ctx)
	msgID, err := t.messageManager.SendMessage(ctx, content, to, "")
	if err != nil || msgID == "" {
		return toolError(fmt.Sprintf("Failed to send message to '%s'", to))
	}
	data := map[string]any{"type": "message", "from": t.messageManager.MemberName(), "to": to}
	if summary != "" {
		data["summary"] = summary
	}
	return toolSuccess(data)
}

// multicast 群发消息（对齐 Python SendMessageTool._multicast）。
func (t *SendMessageTool) multicast(ctx context.Context, targets []any, content, summary string) (map[string]any, error) {
	// 清洗：去空白、去重
	var cleaned []string
	seen := make(map[string]struct{})
	for _, raw := range targets {
		s, ok := raw.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		cleaned = append(cleaned, s)
	}

	if len(cleaned) == 0 {
		return toolError("'to' list must contain at least one member name")
	}
	for _, name := range cleaned {
		if name == "*" {
			return toolError("Cannot mix broadcast '*' with member names; use to='*' for broadcast")
		}
		if name == "user" {
			return toolError("'user' cannot be combined in multicast; send to user separately")
		}
	}

	// 修复 S-30: 全员覆盖检查，对齐 Python team_tools.py:1384-1393
	// 如果 multicast 目标恰好覆盖所有 roster 成员，引导使用 to='*' 广播
	if t.team != nil {
		roster, _ := t.team.ListMembers(ctx)
		if len(roster) > 0 {
			rosterSet := make(map[string]struct{}, len(roster))
			for _, m := range roster {
				rosterSet[m.MemberName] = struct{}{}
			}
			if len(cleaned) == len(rosterSet) {
				allMatch := true
				for _, name := range cleaned {
					if _, ok := rosterSet[name]; !ok {
						allMatch = false
						break
					}
				}
				if allMatch {
					return toolError("Multicast targets cover every other team member; use to='*' to broadcast instead — same delivery, lower cost.")
				}
			}
		}
	}

	t.autoStartMembers(ctx)

	var delivered []string
	var failed []map[string]any
	for _, name := range cleaned {
		if t.team != nil {
			member, err := t.team.GetMember(ctx, name)
			if err != nil || member == nil {
				failed = append(failed, map[string]any{"to": name, "reason": fmt.Sprintf("Member '%s' not found", name)})
				continue
			}
		}
		msgID, err := t.messageManager.SendMessage(ctx, content, name, "")
		if err != nil || msgID == "" {
			failed = append(failed, map[string]any{"to": name, "reason": fmt.Sprintf("Failed to send message to '%s'", name)})
			continue
		}
		delivered = append(delivered, name)
	}

	ok := len(failed) == 0
	data := map[string]any{
		"type":      "multicast",
		"from":      t.messageManager.MemberName(),
		"delivered": delivered,
		"failed":    failed,
	}
	if summary != "" {
		data["summary"] = summary
	}
	if ok {
		return toolSuccess(data)
	}
	return map[string]any{
		"success": false,
		"error":   fmt.Sprintf("Multicast partially failed: %d/%d target(s) failed", len(failed), len(cleaned)),
		"data":    data,
	}, nil
}

// autoStartMembers Leader 自动启动未启动的成员（对齐 Python SendMessageTool._auto_start_members）。
func (t *SendMessageTool) autoStartMembers(ctx context.Context) {
	if t.team == nil || t.onTeammateCreated == nil || !t.team.IsLeader() {
		return
	}
	started, err := t.team.Startup(ctx, t.onTeammateCreated)
	if err != nil {
		logger.Warn(ttLogComponent).Err(err).Msg("auto_start_members 失败")
	}
	if len(started) > 0 {
		logger.Info(ttLogComponent).Strs("started", started).Msg("Auto-started members")
	}
}

// Stream 实现 Tool 接口（不支持流式调用）。
func (t *SendMessageTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// specLabel 返回任务规格的标签（对齐 Python TaskCreateTool._spec_label）。
func specLabel(spec map[string]any) string {
	if id, ok := spec["task_id"].(string); ok && id != "" {
		return id
	}
	if title, ok := spec["title"].(string); ok && title != "" {
		return title
	}
	return "<unnamed>"
}

// taskBrief 返回任务的简要信息 map。
func taskBrief(task *database.TeamTaskBase) map[string]any {
	if task == nil {
		return nil
	}
	assignee := ""
	if task.Assignee != nil {
		assignee = *task.Assignee
	}
	if assignee == "" {
		assignee = "<unassigned>"
	}
	return map[string]any{
		"task_id":   task.TaskID,
		"title":     task.Title,
		"status":    task.Status,
		"assignee":  assignee,
		"team_name": task.TeamName,
	}
}

// extractStringSlice 从 map 中提取字符串切片。
func extractStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	// 可能是 []any 或 []string
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		result := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		// 尝试 JSON 序列化/反序列化
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		var result []string
		if err := json.Unmarshal(b, &result); err != nil {
			return nil
		}
		return result
	}
}
