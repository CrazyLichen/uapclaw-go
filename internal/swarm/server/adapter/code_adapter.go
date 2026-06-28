package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapter Code 模式适配器，组合委托 DeepAdapter。
//
// 继承 JiuWenClawDeepAdapter 的全部接口方法，仅覆盖 CreateInstance。
// Go 中通过内嵌 *DeepAdapter 实现组合委托。
//
// Code 模式差异点（对齐 Python JiuwenClawCodeAdapter）：
//   - create_instance: 不传多模态/上下文引擎参数，使用 code system prompt
//   - rails: 加入 LspRail/CodeAgentRail/CodingMemoryRail/ProjectMemoryRail
//   - subagents: 固定 explore+plan 子代理
//   - _update_rails_for_mode: 保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail
//   - 语言: 强制英文系统提示词
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/interface_code.py (JiuwenClawCodeAdapter)
type CodeAdapter struct {
	// deep 内嵌 DeepAdapter，组合委托全部接口方法
	deep *DeepAdapter

	// ─── Code 模式专有 Rails ───

	// lspRail LSP 护栏
	// ⤵️ 10.3.7-11: LspRail
	lspRail interface{}
	// projectMemoryRail 项目记忆护栏
	// ⤵️ 10.3.7-11: ProjectMemoryRail
	projectMemoryRail interface{}
	// codingMemoryRail 编码记忆护栏
	// ⤵️ 10.3.7-11: CodingMemoryRail
	codingMemoryRail interface{}
	// worktreeRail 工作树护栏
	// ⤵️ 10.3.7-11: WorktreeRail
	worktreeRail interface{}
	// codeAgentRail 编码 Agent 护栏（管理 /agents 创建的自定义 agent）
	// ⤵️ 10.3.7-11: CodeAgentRail
	codeAgentRail interface{}

	// ─── Code 模式配置 ───

	// runtimeLanguageOverride 运行时语言覆盖
	runtimeLanguageOverride string
	// forceEnglishRuntimePrompt 强制英文运行时提示词
	forceEnglishRuntimePrompt bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeAdapter 创建 CodeAdapter 实例。
//
// 对应 Python: JiuwenClawCodeAdapter.__init__() (line 177-192)
func NewCodeAdapter() *CodeAdapter {
	deep := NewDeepAdapter()
	deep.isCodeAgent = true // 单点 source-of-truth：code-agent → project_dir
	return &CodeAdapter{
		deep:                      deep,
		forceEnglishRuntimePrompt: true,
	}
}

// CreateInstance 初始化底层 SDK Agent（code 模式）。
//
// 对应 Python: JiuwenClawCodeAdapter.create_instance() (line 221-342)
//
// 与 DeepAdapter.CreateInstance 的差异（对齐 Python）：
//  1. dreaming_mode = "code"（而非基于 mode 前缀判断）
//  2. _workspace_dir 优先使用 project_dir（LspTool sandbox 校验需要）
//  3. _agent_workspace_dir 始终指向系统 workspace（编码记忆、todo 等不应写入用户项目目录）
//  4. 不传 vision_model_config / audio_model_config / context_engine_config / completion_timeout
//  5. system_prompt 使用 build_code_system_prompt()（而非 build_agent_identity_prompt()）
//  6. rails 包含编码专有 rails（LspRail/ProjectMemoryRail/CodingMemoryRail/CodeAgentRail 等）
//  7. subagents 固定 explore_agent + plan_agent + 按配置启用 code_agent/browser_agent
func (c *CodeAdapter) CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error {
	// 步骤 1: 设置 dreaming_mode = "code"
	c.deep.dreamingMode = "code"

	// 步骤 2: workspace 语义覆写
	// ⤵️ 10.3.7-11: workspaceDir 优先使用 projectDir
	// ⤵️ 10.3.7-11: agentWorkspaceDir 始终指向系统 workspace

	// 步骤 3: instanceOverrides 初始化
	if config != nil {
		c.deep.instanceOverrides = make(map[string]any, len(config))
		for k, v := range config {
			c.deep.instanceOverrides[k] = v
		}
	} else {
		c.deep.instanceOverrides = make(map[string]any)
	}

	// 步骤 4: 从 instanceOverrides 提取 agent_name / project_dir
	if v, ok := c.deep.instanceOverrides["agent_name"]; ok {
		if s, ok := v.(string); ok {
			c.deep.agentName = s
		}
	}
	if v, ok := c.deep.instanceOverrides["project_dir"]; ok {
		if s, ok := v.(string); ok {
			c.deep.projectDir = s
		}
	}

	// ⤵️ 10.3.7-11: 步骤 5-7 基础初始化（checkpoint、dotenv、config、multimodal）
	// ⤵️ 10.3.7-11: 步骤 8  model = _create_model(configBase)  — 不传多模态配置
	// ⤵️ 10.3.7-11: 步骤 9  agentCard = AgentCard{name, id}
	// ⤵️ 10.3.7-11: 步骤 10 toolCards = _get_tool_cards("jiuwenswarm") — 编码 tools
	// ⤵️ 10.3.7-11: 步骤 11 railsList = _build_agent_rails(config, configBase, mode="code")
	//              编码专有 rails：LspRail, ProjectMemoryRail, CodingMemoryRail,
	//              CodeAgentRail, WorktreeRail, AgentModeRail, StructuredAskUserRail,
	//              ConfirmInterruptRail, FileSystemRail
	// ⤵️ 10.3.7-11: 步骤 12 sysOperation = _create_sys_operation()
	// ⤵️ 10.3.7-11: 步骤 13 subagents = _build_configured_subagents(model, config, configBase)
	//              固定: explore_agent + plan_agent
	//              按配置: code_agent + browser_agent
	// ⤵️ 10.3.7-11: 步骤 14 c.deep.instance = create_deep_agent(
	//              model, card, system_prompt=build_code_system_prompt(),
	//              tools, subagents, rails,
	//              enable_task_loop, max_iterations, workspace, sys_operation, language,
	//              // 不传: vision_model_config, audio_model_config,
	//              //       context_engine_config, completion_timeout
	//            )
	// ⤵️ 10.3.7-11: 步骤 15 instance.ensure_initialized()
	// ⤵️ 10.3.7-11: 步骤 16-20 同 DeepAdapter (seed_cwd, a2x, mcp, user_rails)

	// 存储 mode/subMode
	c.deep.mode = mode
	c.deep.subMode = subMode

	logger.Info(logComponent).
		Str("agent_name", c.deep.agentName).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Bool("is_code_agent", c.deep.isCodeAgent).
		Msg("CodeAdapter 初始化骨架完成，等待回填")
	return nil
}

// ReloadAgentConfig 委托 DeepAdapter。
func (c *CodeAdapter) ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error {
	return c.deep.ReloadAgentConfig(ctx, configBase, envOverrides)
}

// ProcessMessageImpl 委托 DeepAdapter。
func (c *CodeAdapter) ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error) {
	return c.deep.ProcessMessageImpl(ctx, req, inputs)
}

// ProcessMessageStreamImpl 委托 DeepAdapter。
func (c *CodeAdapter) ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	return c.deep.ProcessMessageStreamImpl(ctx, req, inputs)
}

// ProcessInterrupt 委托 DeepAdapter。
func (c *CodeAdapter) ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.ProcessInterrupt(ctx, req)
}

// HandleUserAnswer 委托 DeepAdapter。
func (c *CodeAdapter) HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.HandleUserAnswer(ctx, req)
}

// HandleHeartbeat 委托 DeepAdapter。
func (c *CodeAdapter) HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	return c.deep.HandleHeartbeat(ctx, req)
}

// Cleanup 委托 DeepAdapter。
func (c *CodeAdapter) Cleanup() error {
	return c.deep.Cleanup()
}
