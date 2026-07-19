package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTool 自定义 Agent 调度工具。
// 对齐 Python: AgentTool(Tool) (code_agent_rail.py L170-348)
//
// 直接使用 create_deep_agent() 创建子 agent，不依赖 deep_config.subagents。
// 实现 tool.Tool 接口，invoke 时创建子 DeepAgent 执行任务。
type AgentTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// parentAgent 父 Agent 接口（用于获取 AbilityManager、DeepConfig 等）
	parentAgent sainterfaces.BaseAgent
	// customAgents 自定义 Agent 定义映射（name → AgentDefinition）
	// 对齐 Python: self._custom_agents: dict[str, object] = {a.name: a for a in custom_agents}
	customAgents map[string]*types.AgentDefinition
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTool 创建 AgentTool 实例。
// 对齐 Python: AgentTool.__init__(card, parent_agent, custom_agents) (code_agent_rail.py L176-179)
func NewAgentTool(
	card *tool.ToolCard,
	parentAgent sainterfaces.BaseAgent,
	customAgents []*types.AgentDefinition,
) *AgentTool {
	agentMap := make(map[string]*types.AgentDefinition, len(customAgents))
	for _, a := range customAgents {
		agentMap[a.Name] = a
	}
	return &AgentTool{
		card:         card,
		parentAgent:  parentAgent,
		customAgents: agentMap,
	}
}

// Card 返回工具卡片。
// 对齐 Python: Tool.card 属性
func (t *AgentTool) Card() *tool.ToolCard {
	return t.card
}

// Invoke 执行自定义 Agent 调度。
// 对齐 Python: AgentTool.invoke(inputs, **kwargs) (code_agent_rail.py L260-331)
//
// 步骤：
//  1. 从 kwargs 提取 SessionFacade（对齐 Python kwargs["session"]）
//  2. 解析 inputs: subagent_type, prompt, background
//  3. 校验 subagent_type + prompt 必填
//  4. 从 customAgents 查找 AgentDefinition
//  5. 构建 subSessionID: "{parentSessionID}_custom_{subagentType}_{randomHex8}"
//  6. 调用 createSubAgent() 创建子 DeepAgent
//  7. 根据 background 标志：
//     - false（同步）：subAgent.Invoke → 返回 {"output": output, "agent_id": subagentType}
//     - true（异步）：go subAgent.Invoke → 立即返回 {"status": "async_launched", "agent_id": ..., "prompt": ...}
func (t *AgentTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1: 从 opts 提取 SessionFacade
	// 对齐 Python: parent_session = kwargs.get("session") (code_agent_rail.py L263)
	callOpts := tool.NewToolCallOptions(opts...)

	// 步骤 2: 解析输入参数
	// 对齐 Python: subagent_type = inputs.get("subagent_type"); prompt = inputs.get("prompt"); background = inputs.get("background", False)
	var subagentType, prompt string
	var background bool
	if inputs != nil {
		subagentType, _ = inputs["subagent_type"].(string)
		prompt, _ = inputs["prompt"].(string)
		if b, ok := inputs["background"].(bool); ok {
			background = b
		}
	}

	// 步骤 3: 校验必填参数
	// 对齐 Python: if not subagent_type or not prompt: raise build_error(...)
	if subagentType == "" || prompt == "" {
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("error_msg", "Both 'subagent_type' and 'prompt' are required"),
		)
	}

	// 步骤 4: 查找自定义 Agent 定义
	// 对齐 Python: agent_def = self._custom_agents.get(subagent_type) (code_agent_rail.py L285)
	agentDef, ok := t.customAgents[subagentType]
	if !ok {
		available := sortedKeys(t.customAgents)
		return nil, exception.BuildError(
			exception.StatusAgentToolNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("Agent type '%s' not found. Available custom agents: %v", subagentType, available)),
		)
	}

	// 步骤 5: 构建 subSessionID
	// 对齐 Python: parent_session_id = parent_session.get_session_id(); sub_session_id = self._build_sub_session_id(parent_session_id, subagent_type)
	parentSessionID := "default"
	if callOpts.Session != nil {
		// 对齐 Python: parent_session.get_session_id()
		if sess, ok := callOpts.Session.(interface{ GetSessionID() string }); ok {
			parentSessionID = sess.GetSessionID()
		}
	}
	subSessionID := buildSubSessionID(parentSessionID, subagentType)

	// 步骤 6: 创建子 Agent
	// 对齐 Python: subagent = self._create_sub_agent(agent_def, sub_session_id) (code_agent_rail.py L297)
	subAgent, err := t.createSubAgent(agentDef, subSessionID)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_create_subagent_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("子 Agent 创建失败")
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("Custom agent '%s' creation failed: %v", subagentType, err)),
		)
	}

	// 步骤 7: 根据 background 标志执行
	if background {
		// 异步执行
		// 对齐 Python: asyncio.create_task(self._run_async(subagent, prompt, sub_session_id, subagent_type, parent_session))
		// 返回: {"status": "async_launched", "agent_id": subagent_type, "prompt": prompt}
		var parentSess sessioninterfaces.SessionFacade
		if callOpts.Session != nil {
			if s, ok := callOpts.Session.(sessioninterfaces.SessionFacade); ok {
				parentSess = s
			}
		}
		go t.runAsync(ctx, subAgent, prompt, subSessionID, subagentType, parentSess)
		return map[string]any{
			"status":   "async_launched",
			"agent_id": subagentType,
			"prompt":   prompt,
		}, nil
	}

	// 同步执行
	// 对齐 Python: result = await subagent.invoke({"query": prompt, "conversation_id": sub_session_id}, session=parent_session)
	var invokeOpts []sainterfaces.AgentOption
	if callOpts.Session != nil {
		if sess, ok := callOpts.Session.(sessioninterfaces.SessionFacade); ok {
			invokeOpts = append(invokeOpts, sainterfaces.WithSession(sess))
		}
	}
	result, err := subAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": subSessionID,
	}, invokeOpts...)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_invoke_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("子 Agent 执行失败")
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("Custom agent '%s' execution failed: %v", subagentType, err)),
		)
	}

	// 对齐 Python: output = result.get("output", ""); return ToolOutput(success=True, data={"output": output, "agent_id": subagent_type})
	output := ""
	if result != nil {
		if s, ok := result["output"].(string); ok {
			output = s
		}
	}

	return map[string]any{
		"output":   output,
		"agent_id": subagentType,
	}, nil
}

// Stream 流式执行（不支持）。
// 对齐 Python: AgentTool.stream(inputs, **kwargs): pass (code_agent_rail.py L347-348)
func (t *AgentTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.Name)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createSubAgent 从 AgentDefinition 创建子 DeepAgent。
// 对齐 Python: AgentTool._create_sub_agent(agent_def, sub_session_id) (code_agent_rail.py L184-258)
//
// 步骤：
//  1. agentDefToSubagentConfig() 转换 AgentDefinition → SubAgentConfig
//  2. 从 parentAgent.ability_manager 获取 ToolCard 列表，过滤 disallowedForSubagents
//  3. filterToolCards() 按定义的 tools/disallowed_tools 再过滤
//  4. 构建 Workspace（复用父 workspace root_path）
//  5. 构建 CreateDeepAgentParams（对齐 Python create_kwargs 字段映射）
//  6. CreateDeepAgent(ctx, params)
func (t *AgentTool) createSubAgent(agentDef *types.AgentDefinition, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
	// 步骤 1: 将 AgentDefinition 转换为 SubAgentConfig
	// 对齐 Python: spec = _agent_def_to_subagent_config(agent_def, parent_config.model, parent_config.workspace.root_path, model_cache)
	var model *llm.Model
	var modelCache map[string]*llm.Model
	var language string
	var ws *hworkspace.Workspace
	if t.parentAgent != nil {
		if deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface); ok {
			if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
				model = deepCfg.Model
				language = deepCfg.Language
				if deepCfg.Workspace != nil {
					ws = hworkspace.NewWorkspace(deepCfg.Workspace.RootPath, deepCfg.Language)
				}
			}
		}
	}
	spec := agentDefToSubagentConfig(agentDef, model, modelCache)

	// 步骤 2: 从 parentAgent.ability_manager 获取 ToolCard 列表，过滤 disallowedForSubagents
	// 对齐 Python: all_tool_cards = [tc for tc in self._parent_agent.ability_manager.list()
	//             if isinstance(tc, ToolCard) and tc.name not in DISALLOWED_FOR_SUBAGENTS]
	var allToolCards []*tool.ToolCard
	if t.parentAgent != nil {
		am := t.parentAgent.AbilityManager()
		if am != nil {
			for _, ability := range am.List() {
				if tc, ok := ability.(*tool.ToolCard); ok && !disallowedForSubagents[tc.Name] {
					allToolCards = append(allToolCards, tc)
				}
			}
		}
	}

	// 步骤 3: 按 Agent 定义的 tools 字段进一步过滤
	// 对齐 Python: parent_tool_cards = _filter_tool_cards(all_tool_cards,
	//             allowed_tools=list(spec.tools) if spec.tools else ["*"], disallowed_tools=None)
	// 注意：_agent_def_to_subagent_config() 已将 disallowed_tools 合并进 spec.tools（Python 中是 []string），
	// 所以这里的 disallowed_tools 传 nil。
	// Go 中需要自己构建等价于 Python spec.tools 的字符串列表
	allowedTools := agentDef.Tools
	if len(allowedTools) == 0 {
		allowedTools = []string{"*"}
	}
	// 如果有 disallowed_tools 且 allowedTools 不是 ["*"]，先过滤掉 disallowed
	// 对齐 Python: if agent_def.disallowed_tools and tools != ["*"]: tools = [t for t in tools if t not in agent_def.disallowed_tools]
	if len(agentDef.DisallowedTools) > 0 && (len(allowedTools) != 1 || allowedTools[0] != "*") {
		disallowedSet := make(map[string]bool, len(agentDef.DisallowedTools))
		for _, t := range agentDef.DisallowedTools {
			disallowedSet[t] = true
		}
		filtered := make([]string, 0, len(allowedTools))
		for _, t := range allowedTools {
			if !disallowedSet[t] {
				filtered = append(filtered, t)
			}
		}
		allowedTools = filtered
	}
	parentToolCards := filterToolCards(allToolCards, allowedTools, nil)

	// 步骤 4: 构建 Workspace
	// 对齐 Python: workspace = Workspace(root_path=str(parent_config.workspace.root_path), language=parent_config.language)
	if ws == nil {
		ws = hworkspace.NewWorkspace(subSessionID, "en")
	}

	// 步骤 5: 构建 CreateDeepAgentParams
	// 对齐 Python: create_kwargs = {"model": spec.model or parent_config.model, ...} (code_agent_rail.py L234-252)
	resolvedModel := spec.Model
	if resolvedModel == nil {
		resolvedModel = model
	}

	// 对齐 Python: language=spec.language if spec.language is not None else parent_config.language
	resolvedLanguage := language
	if spec.Language != "" {
		resolvedLanguage = spec.Language
	}

	maxIter := 15
	if spec.MaxIterations > 0 {
		maxIter = spec.MaxIterations
	}

	restrictToWorkDir := true

	params := hconfig.CreateDeepAgentParams{
		Model:                  resolvedModel,
		Card:                   spec.AgentCard,
		SystemPrompt:           spec.SystemPrompt,
		ToolCards:              parentToolCards,
		EnableTaskLoop:         spec.EnableTaskLoop,
		MaxIterations:          maxIter,
		Workspace:              ws,
		Skills:                 spec.Skills,
		SysOperation:           nil, // 子 Agent 不继承 sys_operation
		RestrictToWorkDir:      &restrictToWorkDir,
		AutoCreateWorkspace:    false,
		AddGeneralPurposeAgent: false,
		Subagents:              nil,
		EnableAsyncSubagent:    false,
		Language:               resolvedLanguage,
	}

	// 步骤 6: 创建子 Agent
	// 对齐 Python: sub_agent = create_deep_agent(**create_kwargs, **factory_kwargs)
	subAgent, err := harness.CreateDeepAgent(context.Background(), params)
	if err != nil {
		return nil, fmt.Errorf("create_deep_agent 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "agent_tool_create_subagent").
		Str("subagent_type", agentDef.Name).
		Msg("子 Agent 创建成功")

	return subAgent, nil
}

// runAsync 后台异步执行子 Agent。
// 对齐 Python: AgentTool._run_async(subagent, prompt, sub_session_id, subagent_type, parent_session) (code_agent_rail.py L333-345)
//
// 关键决策：不是 fire-and-forget。子 Agent 的流式输出通过 parentSession 的
// delivery 机制（GatewayPushTransport）推送至前端，用户可以看到实时输出。
func (t *AgentTool) runAsync(
	ctx context.Context,
	subAgent hinterfaces.DeepAgentInterface,
	prompt, subSessionID, subagentType string,
	parentSession sessioninterfaces.SessionFacade,
) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("event_type", "agent_tool_async_panic").
				Str("subagent_type", subagentType).
				Any("panic", r).
				Msg("异步子 Agent 执行 panic")
		}
	}()

	var opts []sainterfaces.AgentOption
	if parentSession != nil {
		// 对齐 Python: session=parent_session
		// 将 parentSession 传递给子 Agent，使其流式输出能通过 GatewayPushTransport 推送
		opts = append(opts, sainterfaces.WithSession(parentSession))
	}

	_, err := subAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": subSessionID,
	}, opts...)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_async_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("异步子 Agent 执行失败")
	}
}

// buildSubSessionID 构建 sub session ID。
// 对齐 Python: AgentTool._build_sub_session_id(parent_session_id, subagent_type) (code_agent_rail.py L181-182)
// 格式: "{parent_session_id}_custom_{subagent_type}_{random_hex_8chars}"
func buildSubSessionID(parentSessionID, subagentType string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_custom_%s_%s", parentSessionID, subagentType, hex.EncodeToString(b))
}
