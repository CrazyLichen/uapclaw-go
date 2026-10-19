package team

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	skilltools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberSkillToolkitRail 为 team member 绑定成员作用域的技能管理工具。
// 对齐 Python: MemberSkillToolkitRail (rails/team_member_skill_toolkit_rail.py)
type MemberSkillToolkitRail struct {
	rails.DeepAgentRail
	// workspaceDir 成员工作区目录
	workspaceDir string
	// tools 已注册的工具列表（幂等标记）
	tools []tool.Tool
	// manager 成员 SkillManager
	manager *skillruntime.SkillManager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberSkillToolkitRail 创建 MemberSkillToolkitRail 实例。
// 对齐 Python: MemberSkillToolkitRail(workspace_dir)
func NewMemberSkillToolkitRail(workspaceDir string) *MemberSkillToolkitRail {
	r := &MemberSkillToolkitRail{workspaceDir: workspaceDir}
	r.WithPriority(95)
	return r
}

// Init 在 agent 上注册成员作用域技能工具。
// 对齐 Python: init(self, agent)
func (r *MemberSkillToolkitRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if r.tools != nil {
		return nil
	}
	if agent == nil {
		return nil
	}

	card := agent.Card()
	agentID := card.ID
	if agentID == "" {
		agentID = card.Name
	}
	// Python: str(agent.card.id or agent.card.name)

	r.manager = skillruntime.NewSkillManager(r.workspaceDir)
	toolkit := skilltools.NewSkillToolkit(r.manager)
	tools := toolkit.GetTools()

	for _, t := range tools {
		t.Card().ID = qualifyToolID(t.Card().ID, agentID)
	}

	// 同名 ToolCard 已存在则先移除（对齐 Python existing = ability_manager.get(name)）
	abilityMgr := agent.AbilityManager()
	for _, t := range tools {
		if existing := abilityMgr.Get(t.Card().Name); existing != nil {
			abilityMgr.Remove(t.Card().Name)
		}
	}

	// Runner.resource_mgr.add_tool(list(tools)) + agent.ability_manager.add(tool.card)
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		if err := resourceMgr.AddTool(t); err != nil {
			logger.Warn(logComponent).Err(err).Str("tool", t.Card().Name).
				Msg("[MemberSkillToolkitRail] add_tool failed")
		}
	}
	for _, t := range tools {
		abilityMgr.Add(t.Card())
	}

	r.tools = tools
	logger.Info(logComponent).
		Int("count", len(tools)).
		Str("workspace", r.workspaceDir).
		Str("agent_id", agentID).
		Msg("[MemberSkillToolkitRail] Registered skill tools")
	return nil
}

// Uninit 从 agent 移除成员作用域技能工具。
// 对齐 Python: uninit(self, agent)
func (r *MemberSkillToolkitRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.tools == nil {
		return nil
	}
	if agent != nil {
		abilityMgr := agent.AbilityManager()
		resourceMgr := runner.GetResourceMgr()
		for _, t := range r.tools {
			abilityMgr.Remove(t.Card().Name)
			if _, err := resourceMgr.RemoveTool([]string{t.Card().ID}); err != nil {
				logger.Debug(logComponent).Err(err).Str("tool", t.Card().ID).
					Msg("[MemberSkillToolkitRail] remove_tool failed")
			}
		}
	}
	logger.Info(logComponent).
		Int("count", len(r.tools)).
		Str("workspace", r.workspaceDir).
		Msg("[MemberSkillToolkitRail] Unregistered skill tools")
	r.tools = nil
	r.manager = nil
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// qualifyToolID 为工具 ID 追加 agent 后缀（成员作用域隔离）。
// 对齐 Python: _qualify_tool_id(tool_id, agent_id) → f"{tool_id}_{agent_id}"
func qualifyToolID(toolID string, agentID string) string {
	return toolID + "_" + agentID
}

// 编译时验证 AgentRail 接口实现
var _ agentinterfaces.AgentRail = (*MemberSkillToolkitRail)(nil)
