package adapter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAgentRail Code 模式下的自定义 Agent 护栏。
// 对齐 Python: CodeAgentRail(DeepAgentRail) priority=90 (code_agent_rail.py L351-425)
//
// 管理 /agents 创建的自定义 Agent，通过 AgentTool 注册为统一 "Agent" 工具。
// 与 SubagentRail 共存，只管理自定义 Agent，不触碰内置 Agent。
type CodeAgentRail struct {
	rails.DeepAgentRail
	// workspaceDir 工作空间目录
	workspaceDir string
	// configLister 自定义 Agent 配置列表接口
	configLister AgentConfigLister
	// agentTool 已注册的 AgentTool 实例
	agentTool *AgentTool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// codeAgentRailPriority CodeAgentRail 优先级。
	// 对齐 Python: CodeAgentRail.priority = 90 (code_agent_rail.py L358)
	codeAgentRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// disallowedForSubagents 禁止传递给子 Agent 的工具名集合。
// 对齐 Python: DISALLOWED_FOR_SUBAGENTS (code_agent_rail.py L28-31)
// 引用 types.DisallowedForSubagents 切片构建 map，避免硬编码重复
var disallowedForSubagents map[string]bool

// displayToInternal 显示名→内部名映射。
// 对齐 Python: _DISPLAY_TO_INTERNAL (code_agent_rail.py L48-66)
//
// 定义在本地而非导入 cli.ui.tool_display，避免触发 prompt_toolkit 导入。
var displayToInternal = map[string]string{
	"Read": "read_file", "Write": "write_file", "Edit": "edit_file",
	"Bash": "bash", "Grep": "grep", "Glob": "glob",
	"LS": "ls", "ListDir": "ls",
	"TodoWrite": "todo_create", "TodoList": "todo_list",
	"WebSearch": "web_search", "WebFetch": "web_fetch",
	"ImageOCR": "image_ocr", "VisionQA": "visual_question_answering",
	"AudioTranscribe": "audio_transcription",
	"AudioQA":         "audio_question_answering",
	"AudioMetadata":   "audio_metadata",
}

// toolGroups 工具分组（用于 Agent 定义 UI）。
// 对齐 Python: TOOL_GROUPS (code_agent_rail.py L34-41)
var toolGroups = types.ToolGroups

// 编译时验证 CodeAgentRail 满足 AgentRail 接口
var _ sainterfaces.AgentRail = (*CodeAgentRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeAgentRail 创建 CodeAgentRail 实例。
// 对齐 Python: CodeAgentRail.__init__(workspace_dir=workspace_dir) (code_agent_rail.py L360-364)
func NewCodeAgentRail(workspaceDir string, configLister AgentConfigLister) *CodeAgentRail {
	r := &CodeAgentRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		workspaceDir:  workspaceDir,
		configLister:  configLister,
	}
	r.WithPriority(codeAgentRailPriority)
	return r
}

// Init 初始化 CodeAgentRail，从 AgentConfigService 加载自定义 Agent 并注册 AgentTool。
// 对齐 Python: CodeAgentRail.init(agent) (code_agent_rail.py L366-368)
//
// 步骤：
//  1. loadCustomAgents() 从 AgentConfigService 加载 enabled 的非 builtin Agent
//  2. 无自定义 Agent → 跳过注册，日志记录
//  3. buildAgentToolCard(customAgents, agent.Card().ID)
//  4. NewAgentTool(card, parentAgent, customAgents)
//  5. ResourceMgr.AddTool(agentTool) — 幂等注册
//  6. AbilityManager.Add(agentTool.Card())
func (r *CodeAgentRail) Init(agent sainterfaces.BaseAgent) error {
	// 步骤 1: 加载自定义 Agent
	customAgents := r.loadCustomAgents()
	if len(customAgents) == 0 {
		logger.Info(logComponent).
			Str("event_type", "code_agent_rail_no_custom_agents").
			Msg("无自定义 Agent，Agent 工具未注册")
		return nil
	}

	// 步骤 2: 获取 agentID
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 步骤 3: 动态构建 AgentTool 的 ToolCard
	card := buildAgentToolCard(customAgents, agentID)

	// 步骤 4: 创建 AgentTool 实例
	r.agentTool = NewAgentTool(card, agent, customAgents)

	// 步骤 5: 幂等注册到 ResourceMgr
	// 对齐 Python: Runner.resource_mgr.add_tool([self._agent_tool]) (code_agent_rail.py L387)
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		toolID := r.agentTool.Card().ID
		if toolID != "" {
			existing, err := resourceMgr.GetTool([]string{toolID})
			if err == nil && len(existing) > 0 {
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}
		}
		_ = resourceMgr.AddTool(r.agentTool)
	}

	// 步骤 6: 注册到 AbilityManager
	// 对齐 Python: self._agent.ability_manager.add(self._agent_tool.card) (code_agent_rail.py L388)
	am := agent.AbilityManager()
	if am != nil {
		am.Add(r.agentTool.Card())
	}

	names := make([]string, 0, len(customAgents))
	for _, a := range customAgents {
		names = append(names, a.Name)
	}
	logger.Info(logComponent).
		Str("event_type", "code_agent_rail_init").
		Int("custom_agent_count", len(customAgents)).
		Strs("agent_names", names).
		Msg("CodeAgentRail 已注册 Agent 工具")

	return nil
}

// Uninit 注销 CodeAgentRail，移除已注册的 AgentTool。
// 对齐 Python: CodeAgentRail.uninit(agent) (code_agent_rail.py L370-372)
//
// 步骤：
//  1. agentTool == nil → 直接返回
//  2. 从 AbilityManager 移除（name）
//  3. 从 ResourceMgr 移除（toolID）
//  4. agentTool = nil
func (r *CodeAgentRail) Uninit(agent sainterfaces.BaseAgent) error {
	if r.agentTool == nil {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()

	// 步骤 2: 从 AbilityManager 移除
	// 对齐 Python: agent.ability_manager.remove(name) (code_agent_rail.py L400-404)
	name := r.agentTool.Card().Name
	if name != "" && am != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Debug(logComponent).
						Str("tool_name", name).
						Msgf("从 ability_manager 移除失败: %v", rec)
				}
			}()
			am.Remove(name)
		}()
	}

	// 步骤 3: 从 ResourceMgr 移除
	// 对齐 Python: Runner.resource_mgr.remove_tool(tool_id) (code_agent_rail.py L405-410)
	toolID := r.agentTool.Card().ID
	if toolID != "" && resourceMgr != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Debug(logComponent).
						Str("tool_id", toolID).
						Msgf("从 resource_mgr 移除失败: %v", rec)
				}
			}()
			_, _ = resourceMgr.RemoveTool([]string{toolID})
		}()
	}

	r.agentTool = nil
	logger.Info(logComponent).
		Str("event_type", "code_agent_rail_uninit").
		Msg("CodeAgentRail 注销完成")

	return nil
}

// Reload 热重载自定义 Agent 定义。
// 对齐 Python: _get_current_agent_rails() 覆写 (interface_code.py L839-848)
//
// 逻辑：Uninit → Init，先注销旧 AgentTool，再重新加载并注册新 AgentTool。
func (r *CodeAgentRail) Reload(agent sainterfaces.BaseAgent) error {
	if err := r.Uninit(agent); err != nil {
		return fmt.Errorf("CodeAgentRail Reload Uninit 失败: %w", err)
	}
	if err := r.Init(agent); err != nil {
		return fmt.Errorf("CodeAgentRail Reload Init 失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 初始化禁止传递给子 Agent 的工具名集合
func init() {
	disallowedForSubagents = make(map[string]bool, len(types.DisallowedForSubagents))
	for _, name := range types.DisallowedForSubagents {
		disallowedForSubagents[name] = true
	}
}

// loadCustomAgents 从 AgentConfigService 加载启用的自定义 Agent。
// 对齐 Python: CodeAgentRail._load_custom_agents() (code_agent_rail.py L413-425)
//
// 步骤：
//  1. 通过 configLister 获取自定义 agent 列表
//  2. 过滤条件：source != "builtin" && enabled == true
//  3. 异常时 warn + 返回空列表
func (r *CodeAgentRail) loadCustomAgents() []*types.AgentDefinition {
	if r.configLister == nil {
		return nil
	}

	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).
				Str("event_type", "code_agent_rail_load_failed").
				Msgf("加载自定义 Agent 失败: %v", rec)
		}
	}()

	var result []*types.AgentDefinition
	for _, a := range r.configLister.ListCustomAgents() {
		// 对齐 Python: if a.source != "builtin" and a.enabled == True
		// ListCustomAgents 已过滤 builtin，只需检查 enabled
		if a.Enabled != nil && *a.Enabled {
			result = append(result, a)
		}
	}
	return result
}

// filterToolCards 按允许/禁止列表过滤 ToolCard。
// 对齐 Python: _filter_tool_cards(all_tool_cards, allowed_tools, disallowed_tools) (code_agent_rail.py L78-112)
//
// 步骤：
//  1. allowedTools == ["*"] → 返回全部（浅拷贝）
//  2. 否则按显示名和内部名双匹配过滤（target_names 集合包含 name 和 displayToInternal[name]）
//  3. disallowedTools 再从结果中移除（同样的双匹配逻辑）
func filterToolCards(
	allToolCards []*tool.ToolCard,
	allowedTools []string,
	disallowedTools []string,
) []*tool.ToolCard {
	var result []*tool.ToolCard

	// 步骤 1: allowedTools == ["*"] → 返回全部
	// 对齐 Python: if allowed_tools == ["*"]: result = list(all_tool_cards)
	if len(allowedTools) == 1 && allowedTools[0] == "*" {
		result = make([]*tool.ToolCard, len(allToolCards))
		copy(result, allToolCards)
	} else {
		// 步骤 2: 按显示名和内部名双匹配过滤
		// 对齐 Python: target_names = set(); for name in allowed_tools: target_names.add(display_to_internal.get(name, name)); target_names.add(name)
		targetNames := make(map[string]bool, len(allowedTools)*2)
		for _, name := range allowedTools {
			targetNames[name] = true
			if internal, ok := displayToInternal[name]; ok {
				targetNames[internal] = true
			}
		}
		for _, tc := range allToolCards {
			if targetNames[tc.Name] || targetNames[tc.ID] {
				result = append(result, tc)
			}
		}
	}

	// 步骤 3: disallowedTools 再从结果中移除
	// 对齐 Python: if disallowed_tools: ... result = [tc for tc in result if tc.name not in disallowed_internal]
	if len(disallowedTools) > 0 {
		disallowedSet := make(map[string]bool, len(disallowedTools)*2)
		for _, name := range disallowedTools {
			disallowedSet[name] = true
			if internal, ok := displayToInternal[name]; ok {
				disallowedSet[internal] = true
			}
		}
		filtered := make([]*tool.ToolCard, 0, len(result))
		for _, tc := range result {
			if !disallowedSet[tc.Name] && !disallowedSet[tc.ID] {
				filtered = append(filtered, tc)
			}
		}
		result = filtered
	}

	return result
}

// buildAgentToolCard 动态构建 Agent 工具的 ToolCard。
// 对齐 Python: _build_agent_tool_card(custom_agents, agent_id) (code_agent_rail.py L115-167)
//
// 步骤：
//  1. 遍历 customAgents，生成描述行 "- name: when_to_use (Tools: ...)"
//  2. 追加 Usage notes
//  3. 构建 ToolCard，name="Agent"，含 5 个输入参数
//  4. required: ["description", "prompt", "subagent_type"]
func buildAgentToolCard(customAgents []*types.AgentDefinition, agentID string) *tool.ToolCard {
	// 步骤 1: 构建描述行
	// 对齐 Python: lines = ["Launch a new agent to handle complex, multi-step tasks autonomously.", ...]
	lines := []string{
		"Launch a new agent to handle complex, multi-step tasks autonomously.",
		"",
		"Available custom agents (created via /agents):",
	}
	for _, agentDef := range customAgents {
		// 对齐 Python: desc = agent_def.when_to_use or agent_def.description
		desc := agentDef.WhenToUse
		if desc == "" {
			desc = agentDef.Description
		}
		// 对齐 Python: tools_desc = ", ".join(agent_def.tools) if agent_def.tools else "*"
		toolsDesc := "*"
		if len(agentDef.Tools) > 0 {
			toolsDesc = strings.Join(agentDef.Tools, ", ")
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (Tools: %s)", agentDef.Name, desc, toolsDesc))
	}

	// 步骤 2: 追加 Usage notes
	// 对齐 Python: lines.append("") + lines.append("Usage notes:") + ...
	lines = append(lines,
		"",
		"Usage notes:",
		"- Each agent starts fresh — provide complete context in the prompt",
		"- Clearly tell the agent whether you expect it to write code or just to do research",
		"- Never delegate understanding — write prompts that prove you understood the task",
		"- Delegate the COMPLETE task, not just the analysis portion",
		"- Use background=True for independent parallel work",
		"- You can also invoke agents via @agent-<name> syntax in user messages",
	)

	// 步骤 3: 构建 ToolCard
	// 对齐 Python: tool_id = f"agent_tool_{agent_id}" if agent_id else f"agent_tool_{uuid.uuid4().hex}"
	toolID := fmt.Sprintf("agent_tool_%s", agentID)

	description := strings.Join(lines, "\n")

	return tool.NewToolCardWithID(
		toolID,
		"Agent",
		description,
		[]*schema.Param{
			schema.NewStringParam("description", "A short (3-5 word) description of the task", true),
			schema.NewStringParam("prompt", "The task for the agent to perform", true),
			schema.NewStringParam("subagent_type", "The name of the custom agent to use", true),
			schema.NewStringParam("model", "Optional model override", false),
			schema.NewBooleanParam("background", "Run in background. You will be notified when complete.", false),
		},
		nil,
	)
}

// sortedKeys 返回 map 的排序键。
// 对齐 Python: ", ".join(sorted(self._custom_agents.keys()))
func sortedKeys(m map[string]*types.AgentDefinition) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
