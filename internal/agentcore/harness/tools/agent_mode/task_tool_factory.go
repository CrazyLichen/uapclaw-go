package agent_mode

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateTaskTool 创建 task_tool 工具实例列表，供 AgentModeRail 动态注册。
//
// 对齐 Python: create_task_tool(parent_agent, available_agents, language, agent_id) L127-152
func CreateTaskTool(parentAgent hinterfaces.DeepAgentInterface, availableAgents, language string) []tool.Tool {
	agentID := ""
	if card := parentAgent.ReactAgent().Card(); card != nil {
		agentID = card.ID
	}
	t := subagent.NewTaskTool(parentAgent, availableAgents, language, agentID)
	if t == nil {
		return nil
	}
	return []tool.Tool{t}
}
