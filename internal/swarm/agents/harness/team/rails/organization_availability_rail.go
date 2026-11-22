package rails

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OrganizationAvailabilityBackend 组织可用性后端注入缝。
//
// Python 侧 rail 直接访问 GLOBAL_RUNNER._team_runtime_manager.pool.get(team_id)
// 与 jiuwenswarm/server/runtime/organization_packages 的 team_package_availability、
// org_task_manager.get_organization；这些本体（organization_* 运行时 663 行）
// 属 25.9 专家与组织 ☐，本批不实现。Go 侧以本窄接口注入；
// 后端缺省为 nil，rail 按「无运行时事实」语义降级运行。
//
// 回收条件：25.9 实施 organization_packages / organization_execution 时由
// 工厂装配真实后端，删除本缝登记（IMPLEMENTATION_PLAN.md 15.20/25.9 双向标注）。
type OrganizationAvailabilityBackend interface {
	// TeamPackageAvailability 返回团队包可用性状态（可能含 "available" 键）。
	// Python: team_package_availability(team_id, group)
	TeamPackageAvailability(teamID, agentGroupName string) map[string]any
	// RuntimeEntry 返回运行时池条目摘要：当前会话 ID 与 spec 元数据。
	// Python: runtime.pool.get(team_id) → entry.current_session_id / entry.agent.spec.metadata
	// 不存在条目返回 ok=false。
	RuntimeEntry(teamID string) (currentSessionID string, specMetadata map[string]any, ok bool)
	// OrganizationLeaders 返回组织 leaders 列表（Python: manager.get_organization().leaders）。
	// 无组织管理器返回空切片。
	OrganizationLeaders() []string
}

// OrganizationAvailabilityRail 保持模型可见的组织成员关系与可执行可用性分离。
//
// Python: OrganizationAvailabilityRail (organization_availability_rail.py:22-165)
type OrganizationAvailabilityRail struct {
	rails.DeepAgentRail
	// TeamID 团队 ID
	TeamID string
	// SessionID 会话 ID
	SessionID string
	// backend 组织可用性后端（25.9 注入缝；nil = 无运行时事实语义）
	backend OrganizationAvailabilityBackend
	// builder init 时捕获的提示词构建器（Python: self.builder）
	builder saprompt.SystemPromptBuilderInterface
	// agentGroups 仅保留身份提示（team_id → agent_group_name），绝不保存安装/卸载结论
	agentGroups map[string]string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// organizationAvailabilitySection 注入节名
	// Python: _SECTION = "organization_package_availability"
	organizationAvailabilitySection = "organization_package_availability"

	// organizationAvailabilityLogComponent 日志组件
	organizationAvailabilityLogComponent = logger.ComponentAgentServer
)

// organizationAvailabilityTargetFields 派发守卫覆盖的派发类工具与目标参数名。
// Python: OrganizationAvailabilityRail.before_tool_call 的 target_fields 字典
var organizationAvailabilityTargetFields = map[string]string{
	"org_delegate_task":            "to_team_id",
	"org_create_task":              "delegated_to_team_id",
	"org_send_leader_message":      "to_team_id",
	"org_invite_team":              "team_id",
	"org_activate_and_invite_team": "team_id",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOrganizationAvailabilityRail 创建组织可用性 rail。
//
// Python: OrganizationAvailabilityRail.__init__ (organization_availability_rail.py:25-30)
func NewOrganizationAvailabilityRail(teamID, sessionID string, backend OrganizationAvailabilityBackend) *OrganizationAvailabilityRail {
	return &OrganizationAvailabilityRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		TeamID:        teamID,
		SessionID:     sessionID,
		backend:       backend,
		agentGroups:   map[string]string{},
	}
}

// Init 捕获提示词构建器。
//
// Python: OrganizationAvailabilityRail.init (organization_availability_rail.py:32-33)
func (r *OrganizationAvailabilityRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.builder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 移除注入节并清空身份缓存。
//
// Python: OrganizationAvailabilityRail.uninit (organization_availability_rail.py:35-39)
func (r *OrganizationAvailabilityRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.builder != nil {
		r.builder.RemoveSection(organizationAvailabilitySection)
	}
	r.builder = nil
	r.agentGroups = map[string]string{}
	return nil
}

// BeforeToolCall 派发类工具的目标专家 Team 不可用时拦截。
//
// Python: OrganizationAvailabilityRail.before_tool_call (organization_availability_rail.py:65-94)
func (r *OrganizationAvailabilityRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	field, isTarget := organizationAvailabilityTargetFields[inputs.ToolName]
	if !isTarget {
		return nil
	}
	// Python: args 为 str 时 json.loads —— Go ToolArgs 已是 map，无字符串分支
	target, _ := inputs.ToolArgs[field].(string)
	if target == "" {
		return nil
	}
	status := r.teamAvailability(target, "")
	if available, hasKey := status["available"]; !hasKey || available != false {
		return nil
	}
	errorMsg := ("Target expert Team is unavailable or its AgentGroup identity cannot be verified. " +
		"Reinstall its package or restore its identity before retrying.")
	toolCallID := ""
	if inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}
	cbc.SetExtra("_skip_tool", true)
	inputs.ToolResult = map[string]any{"success": false, "error": errorMsg}
	inputs.ToolMsg = llmschema.NewToolMessage(toolCallID, errorMsg)
	return nil
}

// BeforeModelCall 每次模型调用前刷新组织可用性快照。
//
// Python: OrganizationAvailabilityRail.before_model_call (organization_availability_rail.py:96-133)
func (r *OrganizationAvailabilityRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.builder == nil {
		return nil
	}
	r.builder.RemoveSection(organizationAvailabilitySection)
	if r.backend == nil {
		return nil
	}
	// Python: entry = await runtime.pool.get(self.team_id)
	_, _, entryOK := r.backend.RuntimeEntry(r.TeamID)
	if !entryOK {
		return nil
	}
	var availability []map[string]any
	for _, leaderTeamID := range r.backend.OrganizationLeaders() {
		status := r.teamAvailability(leaderTeamID, "")
		if _, hasKey := status["available"]; hasKey {
			entry := map[string]any{"team_id": leaderTeamID}
			for k, v := range status {
				entry[k] = v
			}
			availability = append(availability, entry)
		}
	}
	if len(availability) > 0 {
		payload, err := json.Marshal(availability)
		if err != nil {
			logger.Warn(organizationAvailabilityLogComponent).
				Err(err).
				Msg("Organization availability lookup failed")
			return nil
		}
		notice := ("Current AgentGroup availability for this organization; this snapshot supersedes earlier messages. " +
			"Members with available=false must not receive new work; ask the user to reinstall their package or " +
			"restore its identity. Membership and history remain for reference. available=true means the package " +
			"is installed again, not that its Team is necessarily idle or already loaded.\n" +
			string(payload))
		r.builder.AddSection(saprompt.PromptSection{
			Name:     organizationAvailabilitySection,
			Content:  map[string]string{"en": notice, "cn": notice},
			Priority: 5,
		})
	}
	return nil
}

// AfterToolCall 丰富组织名册工具结果中的可用性事实。
//
// Python: OrganizationAvailabilityRail.after_tool_call (organization_availability_rail.py:135-165)
func (r *OrganizationAvailabilityRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	if inputs.ToolName != "org_view_organization" || cbc.Exception() != nil {
		return nil
	}
	if r.backend == nil {
		return nil
	}
	result, isMap := inputs.ToolResult.(map[string]any)
	if !isMap {
		return nil
	}
	success, _ := result["success"].(bool)
	data, isData := result["data"].(map[string]any)
	if !success || !isData {
		return nil
	}
	leaders, _ := data["leaders"].([]any)
	for _, item := range leaders {
		leader, isLeader := item.(map[string]any)
		if !isLeader {
			continue
		}
		teamID, _ := leader["team_id"].(string)
		if teamID == "" {
			continue
		}
		groupName, _ := leader["agent_group_name"].(string)
		for k, v := range r.teamAvailability(teamID, groupName) {
			leader[k] = v
		}
	}
	data["next_action"] = ("Membership is historical, not proof of availability. Teams with available=false cannot execute or claim " +
		"new work until their package is reinstalled or its identity is restored. " +
		"Use only currently available teams.")
	if inputs.ToolMsg != nil {
		body := map[string]any{"success": success, "data": data}
		if content, err := json.Marshal(body); err == nil {
			inputs.ToolMsg.Content = llmschema.NewTextContent(string(content))
		} else {
			logger.Warn(organizationAvailabilityLogComponent).
				Err(err).
				Msg("Cannot serialize organization roster tool result")
			return nil
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// teamAvailability 在 prompt、名册结果与派发守卫之间使用当前的包事实。
//
// Python: OrganizationAvailabilityRail._team_availability (organization_availability_rail.py:41-63)
func (r *OrganizationAvailabilityRail) teamAvailability(teamID, agentGroupName string) map[string]any {
	if r.backend == nil {
		return map[string]any{}
	}
	// Python: runtime = GLOBAL_RUNNER._team_runtime_manager; entry = runtime.pool.get(team_id)
	_, metadata, entryOK := r.backend.RuntimeEntry(teamID)
	metadataMap := map[string]any{}
	if entryOK && metadata != nil {
		metadataMap = metadata
	}
	group := ""
	if v, ok := metadataMap["agent_group_name"].(string); ok && v != "" {
		group = v
	} else if agentGroupName != "" {
		group = agentGroupName
	} else {
		group = r.agentGroups[teamID]
	}
	group = trimSpace(group)
	status := r.backend.TeamPackageAvailability(teamID, group)
	resolvedGroup := ""
	if v, ok := status["agent_group_name"].(string); ok {
		resolvedGroup = trimSpace(v)
	}
	_, isExpert := metadataMap["expert_team"]
	if resolvedGroup != "" || isExpert {
		// 只保留身份提示，绝不保存安装/卸载结论。
		r.agentGroups[teamID] = resolvedGroup
	}
	_, knownExpert := r.agentGroups[teamID]
	if _, hasAvailable := status["available"]; !hasAvailable && knownExpert {
		return map[string]any{"available": false, "unavailable_reason": "AGENT_GROUP_IDENTITY_UNKNOWN"}
	}
	// 普通团/Summary/遗留团缺少包身份不构成卸载证据。
	// 其既有工具/运行时校验仍然适用。
	return status
}

// trimSpace 空白裁剪（对齐 Python str.strip）。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpaceByte(s[start]) {
		start++
	}
	for end > start && isSpaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

// isSpaceByte 判断 ASCII 空白字节。
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}
