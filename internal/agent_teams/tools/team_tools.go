package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
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
	SharedToolsStr = "view_task,send_message,workspace_meta"
	// HumanAgentToolsStr Human-Agent 可用的工具名（逗号分隔）
	HumanAgentToolsStr = "view_task,member_complete_task,send_message"
)

// ──────────────────────────── 全局变量 ────────────────────────────

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
//   - role: "leader" / "teammate" / "human_agent"
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
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.build_team", "build_team", t("build_team"), nil, nil)),
		team:     team,
	}
}

// newCleanTeamTool 创建 CleanTeamTool 实例。
func newCleanTeamTool(team *TeamBackend, t locales.Translator) *CleanTeamTool {
	return &CleanTeamTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.clean_team", "clean_team", t("clean_team"), nil, nil)),
		team:     team,
	}
}

// newSpawnMemberTool 创建 SpawnMemberTool 实例。
func newSpawnMemberTool(team *TeamBackend, t locales.Translator, alloc func(modelName string) *models.Allocation) *SpawnMemberTool {
	return &SpawnMemberTool{
		TeamTool:         NewTeamTool(tool.NewToolCardWithID("team.spawn_member", "spawn_member", t("spawn_member"), nil, nil)),
		team:             team,
		modelConfigAlloc: alloc,
	}
}

// newShutdownMemberTool 创建 ShutdownMemberTool 实例。
func newShutdownMemberTool(team *TeamBackend, t locales.Translator) *ShutdownMemberTool {
	return &ShutdownMemberTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.shutdown_member", "shutdown_member", t("shutdown_member"), nil, nil)),
		team:     team,
	}
}

// newApprovePlanTool 创建 ApprovePlanTool 实例。
func newApprovePlanTool(team *TeamBackend, t locales.Translator) *ApprovePlanTool {
	return &ApprovePlanTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.approve_plan", "approve_plan", t("approve_plan"), nil, nil)),
		team:     team,
	}
}

// newApproveToolCallTool 创建 ApproveToolCallTool 实例。
func newApproveToolCallTool(team *TeamBackend, t locales.Translator) *ApproveToolCallTool {
	return &ApproveToolCallTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.approve_tool", "approve_tool", t("approve_tool"), nil, nil)),
		team:     team,
	}
}

// newListMembersTool 创建 ListMembersTool 实例。
func newListMembersTool(team *TeamBackend, t locales.Translator) *ListMembersTool {
	return &ListMembersTool{
		TeamTool: NewTeamTool(tool.NewToolCardWithID("team.list_members", "list_members", t("list_members"), nil, nil)),
		team:     team,
	}
}

// newTaskCreateTool 创建 TaskCreateTool 实例。
func newTaskCreateTool(agentTeam *TeamBackend, t locales.Translator) *TaskCreateTool {
	return &TaskCreateTool{
		TeamTool:    NewTeamTool(tool.NewToolCardWithID("team.create_task", "create_task", t("create_task"), nil, nil)),
		taskManager: agentTeam.TaskManager(),
	}
}

// newUpdateTaskTool 创建 UpdateTaskTool 实例。
func newUpdateTaskTool(agentTeam *TeamBackend, t locales.Translator) *UpdateTaskTool {
	return &UpdateTaskTool{
		TeamTool:  NewTeamTool(tool.NewToolCardWithID("team.update_task", "update_task", t("update_task"), nil, nil)),
		agentTeam: agentTeam,
	}
}

// newViewTaskTool 创建 ViewTaskTool 实例。
func newViewTaskTool(taskManager *TeamTaskManager, t locales.Translator) *ViewTaskTool {
	return &ViewTaskTool{
		TeamTool:    NewTeamTool(tool.NewToolCardWithID("team.view_task", "view_task", t("view_task"), nil, nil)),
		taskManager: taskManager,
	}
}

// newClaimTaskTool 创建 ClaimTaskTool 实例。
func newClaimTaskTool(taskManager *TeamTaskManager, t locales.Translator) *ClaimTaskTool {
	return &ClaimTaskTool{
		TeamTool:    NewTeamTool(tool.NewToolCardWithID("team.claim_task", "claim_task", t("claim_task"), nil, nil)),
		taskManager: taskManager,
	}
}

// newSubmitPlanTool 创建 SubmitPlanTool 实例。
func newSubmitPlanTool(taskManager *TeamTaskManager, t locales.Translator) *SubmitPlanTool {
	return &SubmitPlanTool{
		TeamTool:    NewTeamTool(tool.NewToolCardWithID("team.submit_plan", "submit_plan", t("submit_plan"), nil, nil)),
		taskManager: taskManager,
	}
}

// newMemberCompleteTaskTool 创建 MemberCompleteTaskTool 实例。
func newMemberCompleteTaskTool(taskManager *TeamTaskManager, t locales.Translator) *MemberCompleteTaskTool {
	return &MemberCompleteTaskTool{
		TeamTool:    NewTeamTool(tool.NewToolCardWithID("team.member_complete_task", "member_complete_task", t("member_complete_task"), nil, nil)),
		taskManager: taskManager,
	}
}

// newSendMessageTool 创建 SendMessageTool 实例。
func newSendMessageTool(msgMgr *TeamMessageManager, t locales.Translator, team *TeamBackend, onCreated func(ctx context.Context, memberName string) error) *SendMessageTool {
	return &SendMessageTool{
		TeamTool:          NewTeamTool(tool.NewToolCardWithID("team.send_message", "send_message", t("send_message"), nil, nil)),
		messageManager:    msgMgr,
		team:              team,
		onTeammateCreated: onCreated,
	}
}

// Invoke 实现 Tool 接口（桩实现，返回未实现错误）。
func (t *BuildTeamTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("BuildTeamTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *BuildTeamTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *CleanTeamTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("CleanTeamTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *CleanTeamTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *SpawnMemberTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("SpawnMemberTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *SpawnMemberTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ShutdownMemberTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ShutdownMemberTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ShutdownMemberTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ApprovePlanTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ApprovePlanTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ApprovePlanTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ApproveToolCallTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ApproveToolCallTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ApproveToolCallTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ListMembersTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ListMembersTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ListMembersTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *TaskCreateTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("TaskCreateTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *TaskCreateTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ViewTaskTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ViewTaskTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ViewTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *UpdateTaskTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("UpdateTaskTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *UpdateTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *SubmitPlanTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("SubmitPlanTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *SubmitPlanTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *ClaimTaskTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("ClaimTaskTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *ClaimTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *MemberCompleteTaskTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("MemberCompleteTaskTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *MemberCompleteTaskTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

func (t *SendMessageTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, fmt.Errorf("SendMessageTool.Invoke 未实现")
}

// Stream 实现 Tool 接口（桩实现，返回流不支持错误）。
func (t *SendMessageTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
