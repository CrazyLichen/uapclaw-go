package rails

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	mcptools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpRail MCP 资源浏览工具注册 Rail。
//
// 职责：
//   - Init:    创建 ListMcpResourcesTool + ReadMcpResourceTool，注册到 ResourceMgr + AbilityManager
//   - Uninit:  从 AbilityManager + ResourceMgr 注销两个工具
//
// MCP 服务器本身的注册由 DeepAgentConfig.mcps 处理（_register_pending_mcps），
// McpRail 只负责挂载资源浏览工具，使 LLM 能发现和读取已注册 MCP 服务器上的资源。
//
// 对齐 Python: openjiuwen/harness/rails/mcp_rail.py McpRail
type McpRail struct {
	DeepAgentRail
	// tools 已注册的工具列表
	tools []tool.Tool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mcpRailPriority McpRail 优先级
	// 对齐 Python: McpRail.priority = 95
	mcpRailPriority = 95
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 McpRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*McpRail)(nil)

// mcpRailLogComponent 日志组件标识
var mcpRailLogComponent = logger.ComponentAgentCore

// init 确保编译时引用 hinterfaces 包（McpRail 不直接使用但子类可能需要）
var _ hinterfaces.DeepAgentInterface

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMcpRail 创建 MCP 资源浏览 Rail 实例。
//
// 对齐 Python: McpRail.__init__()
func NewMcpRail() *McpRail {
	r := &McpRail{
		DeepAgentRail: *NewDeepAgentRail(),
	}
	r.WithPriority(mcpRailPriority)
	return r
}

// Init 注册 ListMcpResourcesTool + ReadMcpResourceTool 到 ResourceMgr + AbilityManager。
//
// 对齐 Python: McpRail.init() L25-38
func (r *McpRail) Init(agent agentinterfaces.BaseAgent) error {
	// 对齐 Python L26: 获取 language 和 agent_id
	var language string
	var agentID string

	sb := agent.SystemPromptBuilder()
	if sb != nil {
		language = sb.Language()
	} else {
		language = "cn"
	}
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 对齐 Python L28-30: list_tool = ListMcpResourcesTool(lang, agent_id)
	//                     read_tool = ReadMcpResourceTool(lang, agent_id)
	r.tools = []tool.Tool{
		mcptools.NewListMcpResourcesTool(language, agentID),
		mcptools.NewReadMcpResourceTool(language, agentID),
	}

	// 对齐 Python L32-35: Runner.resource_mgr.add_tool(self.tools)
	//                     for tool in self.tools: agent.ability_manager.add(tool.card)
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(mcpRailLogComponent).
		Str("event_type", "mcp_rail_init").
		Msg("McpRail 已注册 ListMcpResources/ReadMcpResource 工具")

	return nil
}

// Uninit 从 AbilityManager + ResourceMgr 注销两个资源浏览工具。
//
// 对齐 Python: McpRail.uninit() L40-49
func (r *McpRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if len(r.tools) == 0 {
		return nil
	}

	// 对齐 Python L42-48: for tool in self.tools:
	//   name = tool.card.name; ability_manager.remove(name)
	//   tool_id = tool.card.id; Runner.resource_mgr.remove_tool(tool_id)
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(mcpRailLogComponent).
						Str("event_type", "mcp_rail_uninit").
						Str("tool_name", t.Card().Name).
						Msgf("注销工具失败: %v", rec)
				}
			}()
			if am != nil {
				am.Remove(t.Card().Name)
			}
			if resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
			}
		}(t)
	}
	r.tools = nil

	logger.Info(mcpRailLogComponent).
		Str("event_type", "mcp_rail_uninit").
		Msg("McpRail 注销完成")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
