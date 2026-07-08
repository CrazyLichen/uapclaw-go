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

// ──────────────────────────── 非导出函数 ────────────────────────────

// initContext 初始化上下文引擎。
//
// 对应 Python: ReActAgent._init_context()
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
			// 对齐 Python: self.ability_manager.add(context_reloader.card)
			am.Add(reloaderTool.Card())
		}
		// ⤵️ Runner.resource_mgr 注册（需要 Runner 集成）
	} else {
		if am != nil && reloaderTool != nil {
			// 对齐 Python: self.ability_manager.remove(context_reloader.card.name)
			am.Remove(reloaderTool.Card().Name)
		}
	}

	return modelCtx, nil
}

// getLLM 获取 LLM 实例（延迟初始化）。
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
		modelCfg := &llmschema.ModelRequestConfig{
			ModelName: a.config.ModelNameVal,
		}
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

// getTools 获取工具列表。
func (a *ReActAgent) getTools() ([]cschema.ToolInfoInterface, error) {
	am := a.getAbilityManager()
	if am == nil {
		return nil, nil
	}
	tools, _ := am.ListToolInfo(context.Background(), nil)
	return tools, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetAbilityManager 设置能力管理器，允许外部注入自定义实现。
func (a *ReActAgent) SetAbilityManager(am interfaces.AbilityManagerInterface) {
	a.abilityManager = am
}

// SetPromptBuilder 设置系统提示词构建器（由 DeepAgent 注入共享实例）。
// 对齐 Python: agent.prompt_builder = prompt_builder / agent.system_prompt_builder = prompt_builder
// 注意：Configure() 会覆盖此字段（新建 SystemPromptBuilder），
// DeepAgent 需在每次调用 Configure 后重新调用此方法覆盖回共享实例。
func (a *ReActAgent) SetPromptBuilder(pb *prompts.SystemPromptBuilder) {
	a.promptBuilder = pb
}

// SetLLM 设置预构建的 LLM 模型实例（跳过延迟初始化）。
// 对齐 Python: agent.set_llm(model)
// 注意：Configure() 会重置 llmOnce，
// DeepAgent 需在每次调用 Configure 后重新调用此方法确保注入生效。
func (a *ReActAgent) SetLLM(m *llm.Model) {
	a.llm = m
	a.llmOnce = sync.Once{}
}

// GetLLM 返回 LLM 模型实例（延迟初始化）。
// 对齐 Python: ReActAgent.get_llm()
// 导出版本，供 DeepAgent 等外部消费者调用。
func (a *ReActAgent) GetLLM() (*llm.Model, error) {
	return a.getLLM()
}

// SwitchModel 切换模型并同步配置。
// 对齐 Python: ctx.agent.set_llm(target_model) + ctx.agent.config.model_name = target_model.model_config.model_name
// 封装 SetLLM + Config.ModelNameVal 同步，供 TaskPlanningRail 通过 modelSwitcher 最小接口调用。
func (a *ReActAgent) SwitchModel(model *llm.Model) {
	a.SetLLM(model)
	if a.config != nil && model != nil && model.ModelConfig != nil {
		a.config.ModelNameVal = model.ModelConfig.ModelName
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAbilityManager 返回能力管理器。
func (a *ReActAgent) getAbilityManager() interfaces.AbilityManagerInterface {
	if a.abilityManager == nil {
		return nil
	}
	return a.abilityManager
}

// saveContexts 保存上下文。
func (a *ReActAgent) saveContexts(sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil || sess == nil {
		return
	}
	if _, err := a.contextEngine.SaveContexts(context.Background(), sess, nil); err != nil {
		logger.Warn(logComponent).Str("event_type", "save_contexts_error").Err(err).Msg("保存上下文失败")
	}
}
