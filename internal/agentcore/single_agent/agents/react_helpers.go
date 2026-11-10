package agents

import (
	"context"
	"fmt"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetAbilityManager 设置能力管理器，允许外部注入自定义实现。
func (a *ReActAgent) SetAbilityManager(am interfaces.AbilityManagerInterface) {
	a.abilityManager = am
}

// SetPromptBuilder 设置系统提示词构建器（由 DeepAgent 注入共享实例）。
// Python: agent.prompt_builder = prompt_builder / agent.system_prompt_builder = prompt_builder
// 注意：Configure() 会覆盖此字段（新建 SystemPromptBuilder），
// DeepAgent 需在每次调用 Configure 后重新调用此方法覆盖回共享实例。
func (a *ReActAgent) SetPromptBuilder(pb *prompts.SystemPromptBuilder) {
	a.promptBuilder = pb
}

// SetLLM 设置预构建的 LLM 模型实例并同步配置字段。
// Python: agent.set_llm(model) + config.model_name/model_client_config/model_config_obj 同步。
// 注意：Configure() 会重置 llmOnce，
// DeepAgent 需在每次调用 Configure 后重新调用此方法确保注入生效。
func (a *ReActAgent) SetLLM(m *llm.Model) {
	a.llm = m
	a.llmOnce = sync.Once{}
	// 同步 config 中的模型相关字段，对齐 Python _apply_model_to_react_agent
	if a.config != nil && m != nil {
		if m.ModelConfig != nil {
			a.config.ModelNameVal = m.ModelConfig.ModelName
		}
		a.config.ModelClientConfig = m.ClientConfig
		a.config.ModelRequestConfig = m.ModelConfig
	}
}

// GetLLM 返回 LLM 模型实例（延迟初始化）。
// Python: ReActAgent.get_llm()
// 导出版本，供 DeepAgent 等外部消费者调用。
func (a *ReActAgent) GetLLM() (*llm.Model, error) {
	return a.getLLM()
}

// SetUsageBuildContext 注入团队成员的构建上下文（成员名来源）。
// 内层 ReActAgent 产生模型调用用量事件，这个字段让用量归属能认出成员身份，
// 而不必让核心用量代码依赖团队包。
// Python: self._react_agent._usage_build_context = build_context（native_harness.py L186）
func (a *ReActAgent) SetUsageBuildContext(buildContext UsageIdentityProvider) {
	a.usageBuildContext = buildContext
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initContext 按配置创建本轮的模型上下文，并同步刷新上下文重载工具。
func (a *ReActAgent) initContext(ctx context.Context, sess sessioninterfaces.SessionFacade) (ceinterface.ModelContext, error) {
	if a.contextEngine == nil {
		return nil, nil
	}

	// 1. 对齐 Python L1225-1229: 传递 context_processors
	var opts []ceinterface.CreateContextOption
	if a.config != nil && len(a.config.ContextProcessors) > 0 {
		opts = append(opts, ceinterface.WithProcessors(a.config.ContextProcessors))
	}
	modelCtx, err := a.contextEngine.CreateContext(ctx, "default_context", sess, opts...)
	if err != nil {
		return nil, fmt.Errorf("创建上下文失败: %w", err)
	}

	// 2. 对齐 Python L1234-1241: reloader tool 动态注册/注销
	reloaderTool := modelCtx.ReloaderTool()
	am := a.getAbilityManager()
	if a.config != nil && a.config.ContextEngineConfig.EnableReload {
		if am != nil && reloaderTool != nil {
			// Python: self.ability_manager.add(context_reloader.card)
			am.Add(reloaderTool.Card())
		}
		// ⤵️ Runner.resource_mgr 注册（需要 Runner 集成）
	} else {
		if am != nil && reloaderTool != nil {
			// Python: self.ability_manager.remove(context_reloader.card.name)
			am.Remove(reloaderTool.Card().Name)
		}
	}

	return modelCtx, nil
}

// getLLM 返回模型客户端，未初始化时按配置创建。
func (a *ReActAgent) getLLM() (*llm.Model, error) {
	if a.llm != nil {
		return a.llm, nil
	}
	if a.config == nil {
		return nil, fmt.Errorf("config 未设置")
	}
	var initErr error
	a.llmOnce.Do(func() {
		clientCfg := &llmschema.ModelClientConfig{
			ClientProvider: a.config.ModelProvider,
			APIKey:         a.config.APIKey,
			APIBase:        a.config.APIBase,
		}
		modelCfg := llmschema.NewModelRequestConfig()
		modelCfg.ModelName = a.config.ModelNameVal
		model, err := llm.NewModel(clientCfg, modelCfg)
		if err != nil {
			initErr = err
			return
		}
		a.llm = model
	})
	if initErr != nil {
		return nil, fmt.Errorf("LLM 初始化失败: %w", initErr)
	}
	return a.llm, nil
}

// getTools 取当前可用的工具列表，取不到时直接报错而不是返回空列表。
func (a *ReActAgent) getTools(ctx context.Context) ([]cschema.ToolInfoInterface, error) {
	am := a.getAbilityManager()
	if am == nil {
		return nil, nil
	}
	// S-54: 对齐 Python react_agent.py L1328 异常传播——
	// 工具列表获取失败时向调用方报错（fail-fast），不吞错继续空列表
	tools, err := am.ListToolInfo(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("获取工具列表失败: %w", err)
	}
	return tools, nil
}

// getAbilityManager 返回能力管理器。
func (a *ReActAgent) getAbilityManager() interfaces.AbilityManagerInterface {
	if a.abilityManager == nil {
		return nil
	}
	return a.abilityManager
}

// parallelToolCalls 返回本次工具批量是否并行执行。
// 未配置时默认 true（与上游 ReActAgentConfig.parallel_tool_calls 默认一致），
// 子代理可通过配置显式关闭，让有状态工具按序执行。
// Python: react_agent.py:2460 self._config.parallel_tool_calls
func (a *ReActAgent) parallelToolCalls() bool {
	if a.config == nil {
		return true
	}
	return a.config.ParallelToolCallsOrDefault()
}

// saveContexts 把本轮上下文写回会话存储，写失败只记日志不影响主流程。
func (a *ReActAgent) saveContexts(ctx context.Context, sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	if _, err := a.contextEngine.SaveContexts(ctx, sess, nil); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("savecontext failed")
	}
}

// saveContextsDetached 以脱离取消信号的 ctx 持久化上下文。
// 取消/异常路径必须写完：调用方 ctx 已取消时，KV/SQL 写入会因
// ExecContext(canceled) 立刻失败，导致本轮清洗结果丢失，下一轮从旧 checkpoint 重建。
// 等价 Python 的 asyncio.shield(self._save_contexts_on_cancel(session))。
func (a *ReActAgent) saveContextsDetached(ctx context.Context, sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	if _, err := a.contextEngine.SaveContexts(detached, sess, nil); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("savecontext failed")
	}
}
